package controller

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/config"
)

// ============ 构造 MQTT CONNECT 包 ============

func encodeStr(s string) []byte {
	b := make([]byte, 2+len(s))
	binary.BigEndian.PutUint16(b, uint16(len(s)))
	copy(b[2:], s)
	return b
}

func encodeRemaining(n int) []byte {
	var out []byte
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

// buildConnectPacket 构造 MQTT CONNECT 包（默认 keepalive=60s）
func buildConnectPacket(protocolLevel byte, clientID, username, password string) []byte {
	return buildConnectPacketKeepAlive(protocolLevel, clientID, username, password, 60)
}

// buildConnectPacketKeepAlive 构造带指定 keepalive 的 MQTT CONNECT 包（心跳看护测试用）
func buildConnectPacketKeepAlive(protocolLevel byte, clientID, username, password string, keepAlive uint16) []byte {
	vh := []byte{}
	vh = append(vh, encodeStr("MQTT")...)
	vh = append(vh, protocolLevel)
	flags := byte(0x02) // clean session
	if username != "" {
		flags |= 0x80
	}
	if password != "" {
		flags |= 0x40
	}
	vh = append(vh, flags, byte(keepAlive>>8), byte(keepAlive))

	// MQTT 5.0: connect properties（0 长度表示无属性）
	if protocolLevel == 5 {
		vh = append(vh, 0x00) // properties length = 0
	}

	payload := []byte{}
	payload = append(payload, encodeStr(clientID)...)
	if username != "" {
		payload = append(payload, encodeStr(username)...)
	}
	if password != "" {
		payload = append(payload, encodeStr(password)...)
	}

	body := append(vh, payload...)
	frame := []byte{0x10}
	frame = append(frame, encodeRemaining(len(body))...)
	return append(frame, body...)
}

// ============ 解析测试 ============

func TestParseConnectCredentials(t *testing.T) {
	// MQTT 3.1.1（默认 keepalive=60）
	pkt311 := buildConnectPacket(4, "dev-001", "dev-001", "token-abc")
	_, u, p, level, ka, ok := parseConnectCredentials(pkt311)
	assert.True(t, ok)
	assert.Equal(t, "dev-001", u)
	assert.Equal(t, "token-abc", p)
	assert.Equal(t, byte(4), level)
	assert.Equal(t, uint16(60), ka)

	// MQTT 5.0 + 自定义 keepalive
	pkt5 := buildConnectPacketKeepAlive(5, "dev-002", "dev-002", "token-xyz", 120)
	_, u, p, level, ka, ok = parseConnectCredentials(pkt5)
	assert.True(t, ok)
	assert.Equal(t, "dev-002", u)
	assert.Equal(t, "token-xyz", p)
	assert.Equal(t, byte(5), level)
	assert.Equal(t, uint16(120), ka)

	// keepalive=0（禁用心跳）
	pktNoKa := buildConnectPacketKeepAlive(4, "dev-003", "dev-003", "token-no-ka", 0)
	_, _, _, _, ka, ok = parseConnectCredentials(pktNoKa)
	assert.True(t, ok)
	assert.Equal(t, uint16(0), ka)

	// 非 CONNECT 包
	_, _, _, _, _, ok = parseConnectCredentials([]byte{0x30, 0x02, 0x00, 0x00})
	assert.False(t, ok)

	// 空包
	_, _, _, _, _, ok = parseConnectCredentials(nil)
	assert.False(t, ok)
}

// ============ 端到端桥接测试 ============
// mockMqttBroker 模拟外部 MQTT broker：记录收到的帧，可按需回包
type mockMqttBroker struct {
	ln     net.Listener
	mu     sync.Mutex
	frames [][]byte
	conn   net.Conn
}

func newMockMqttBroker(t *testing.T) *mockMqttBroker {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	m := &mockMqttBroker{ln: ln}
	go m.acceptLoop()
	return m
}

func (m *mockMqttBroker) addr() string { return m.ln.Addr().String() }

func (m *mockMqttBroker) acceptLoop() {
	conn, err := m.ln.Accept()
	if err != nil {
		return
	}
	m.mu.Lock()
	m.conn = conn
	m.mu.Unlock()

	reader := &mqttFrameReader{r: bufio.NewReader(conn)}
	first := true
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			return
		}
		m.mu.Lock()
		m.frames = append(m.frames, frame)
		m.mu.Unlock()

		// 收到 CONNECT 后自动回 CONNACK success（模拟真实 broker）
		if first && len(frame) > 0 && frame[0] == 0x10 {
			_, _ = conn.Write([]byte{0x20, 0x02, 0x00, 0x00})
			first = false
		}

		// 收到 SUBSCRIBE 后回 SUBACK success（否则 paho 的 Subscribe token 会卡住）
		if len(frame) > 0 && frame[0]&0xF0 == 0x80 {
			topics := parseSubscribeTopics(frame, 4)
			if len(topics) > 0 {
				pos := 1
				for pos < len(frame) && frame[pos]&0x80 != 0 {
					pos++
				}
				pos++
				if pos+2 <= len(frame) {
					body := append([]byte{}, frame[pos:pos+2]...) // packet id
					for range topics {
						body = append(body, 0x00)
					}
					ack := []byte{0x90}
					ack = append(ack, encodeRemaining(len(body))...)
					ack = append(ack, body...)
					_, _ = conn.Write(ack)
				}
			}
		}

		// 收到 QoS>0 的 PUBLISH 后回 PUBACK（模拟真实 broker，否则 paho 的 Publish token 会卡住）
		// 规范格式: fixed header | remaining len | topic len(2B) | topic | packet id(2B) | payload
		if len(frame) > 0 && frame[0]&0xF0 == 0x30 && frame[0]&0x06 != 0 {
			pos := 1
			for pos < len(frame) && frame[pos]&0x80 != 0 {
				pos++
			}
			pos++
			if pos+2 > len(frame) {
				continue
			}
			topicLen := int(binary.BigEndian.Uint16(frame[pos : pos+2]))
			pos += 2 + topicLen
			if pos+2 <= len(frame) {
				ack := append([]byte{0x40, 0x02}, frame[pos:pos+2]...)
				_, _ = conn.Write(ack)
			}
		}
	}
}

func (m *mockMqttBroker) received() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.frames...)
}

func (m *mockMqttBroker) writePublish(topic, payload string) {
	// 规范格式: fixed header | remaining len | topic len(2B) | topic | payload（无长度字段）
	body := encodeStr(topic)
	body = append(body, []byte(payload)...)
	frame := []byte{0x30}
	frame = append(frame, encodeRemaining(len(body))...)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		_, _ = m.conn.Write(append(frame, body...))
	}
}

func (m *mockMqttBroker) close() { m.ln.Close() }

// deviceLogin 通过框架 Sa-Token（内存存储）为设备签发 Token
func deviceLogin(t *testing.T, deviceID string) string {
	token, err := middleware.GetDeviceManager().Login(deviceID, "device")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	return token
}

// setupGatewayTest 起 gin + httptest + 网关控制器（路由不挂 HTTP 中间件，纯 MQTT 透传）
func setupGatewayTest(t *testing.T) (*httptest.Server, *MqttGatewayController) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })

	gin.SetMode(gin.TestMode)
	gateway := NewMqttGatewayController(nil, nil, nil, nil, nil)
	e := gin.New()
	e.GET("/api/ws/mqtt/broker", gateway.HandleWebSocket)
	ts := httptest.NewServer(e)
	t.Cleanup(ts.Close)
	return ts, gateway
}

func TestMqttGatewayControllerBridge(t *testing.T) {
	// 外部 broker mock
	mock := newMockMqttBroker(t)
	defer mock.close()

	config.Cfg = &config.Config{
		MQTT:        config.MQTTConfig{BrokerURL: "tcp://" + mock.addr()},
		MqttGateway: config.MqttGatewayConfig{Enabled: true},
	}

	ts, _ := setupGatewayTest(t)
	// 标准 MQTT 客户端：CONNECT username=设备ID / password=设备Token（框架签发的 Sa-Token）
	token := deviceLogin(t, "dev-001")
	wsURL := "ws" + ts.URL[len("http"):] + "/api/ws/mqtt/broker"

	// 设备客户端（paho，走 ws://）
	gotPub := make(chan string, 1)
	opts := mqtt.NewClientOptions().
		AddBroker(wsURL).
		SetClientID("test-device").
		SetUsername("dev-001").
		SetPassword(token).
		SetDefaultPublishHandler(func(_ mqtt.Client, msg mqtt.Message) {
			gotPub <- string(msg.Payload())
		})
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	require.True(t, tok.WaitTimeout(8*time.Second), "设备连接失败: %v", tok.Error())
	require.NoError(t, tok.Error())
	defer client.Disconnect(100)

	// mock 收到 CONNECT（网关透明转发）
	require.Eventually(t, func() bool {
		for _, f := range mock.received() {
			if len(f) > 0 && f[0] == 0x10 {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "外部Broker未收到 CONNECT")

	// mock 回 CONNACK success（模拟 broker 放行）—— acceptLoop 已自动回复，此处不再手动调用
	// 设备发布消息 → 应被网关转发到外部 broker
	pubTok := client.Publish("iot/device/telemetry", 1, false, `{"token":"token-abc","sensors":[]}`)
	require.True(t, pubTok.WaitTimeout(5*time.Second))
	require.NoError(t, pubTok.Error())

	require.Eventually(t, func() bool {
		for _, f := range mock.received() {
			// 0x30 = PUBLISH（QoS0/1/2 前 4 位相同），payload 含 token-abc（设备发布的 JSON）
			if len(f) > 0 && f[0]&0xF0 == 0x30 && strings.Contains(string(f), "token-abc") {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "外部Broker未收到设备消息")

	// 外部 broker 下发消息 → 设备应收到（TCP→WS 帧切分）
	mock.writePublish("iot/echo", "pong-from-broker")
	select {
	case got := <-gotPub:
		assert.Equal(t, "pong-from-broker", got)
	case <-time.After(5 * time.Second):
		t.Fatal("设备未收到外部Broker下发的消息")
	}
}

// TestMqttGatewayControllerAuthReject CONNECT 凭证错误：应回 CONNACK not authorized(0x05)，
// 外部 broker 不应收到任何帧
func TestMqttGatewayControllerAuthReject(t *testing.T) {
	mock := newMockMqttBroker(t)
	defer mock.close()

	config.Cfg = &config.Config{
		MQTT:        config.MQTTConfig{BrokerURL: "tcp://" + mock.addr()},
		MqttGateway: config.MqttGatewayConfig{Enabled: true},
	}

	ts, _ := setupGatewayTest(t)
	wsURL := "ws" + ts.URL[len("http"):] + "/api/ws/mqtt/broker"

	opts := mqtt.NewClientOptions().
		AddBroker(wsURL).
		SetClientID("bad-device").
		SetUsername("dev-001").
		SetPassword("wrong-token")
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	// Connect 应失败（网关回 CONNACK not authorized）
	require.True(t, tok.WaitTimeout(8*time.Second))
	require.Error(t, tok.Error())
	defer client.Disconnect(100)

	// 外部 broker 不应收到任何帧（CONNECT 未转发）
	time.Sleep(300 * time.Millisecond)
	assert.Empty(t, mock.received(), "鉴权失败时不应转发 CONNECT 到外部Broker")
}

// TestMqttGatewayControllerAuthViaQueryToken mqtt.js 风格：URL 携带 ?X-Device-Token=，
// CONNECT 不带凭证也能通过鉴权并透传
func TestMqttGatewayControllerAuthViaQueryToken(t *testing.T) {
	mock := newMockMqttBroker(t)
	defer mock.close()

	config.Cfg = &config.Config{
		MQTT:        config.MQTTConfig{BrokerURL: "tcp://" + mock.addr()},
		MqttGateway: config.MqttGatewayConfig{Enabled: true},
	}

	ts, _ := setupGatewayTest(t)
	token := deviceLogin(t, "dev-002")
	wsURL := "ws" + ts.URL[len("http"):] + "/api/ws/mqtt/broker?X-Device-Token=" + token

	opts := mqtt.NewClientOptions().
		AddBroker(wsURL).
		SetClientID("query-device")
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	require.True(t, tok.WaitTimeout(8*time.Second), "连接失败: %v", tok.Error())
	require.NoError(t, tok.Error())
	defer client.Disconnect(100)

	// 外部 broker 收到 CONNECT（透传成功）
	require.Eventually(t, func() bool {
		for _, f := range mock.received() {
			if len(f) > 0 && f[0] == 0x10 {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "外部Broker未收到 CONNECT")
}

// TestMqttGatewayControllerRejectBadQueryToken URL 携带无效 Token：
// WebSocket 可升级（不挂 HTTP 中间件），但 CONNECT 后应回 CONNACK not authorized(0x05)
func TestMqttGatewayControllerRejectBadQueryToken(t *testing.T) {
	mock := newMockMqttBroker(t)
	defer mock.close()

	config.Cfg = &config.Config{
		MQTT:        config.MQTTConfig{BrokerURL: "tcp://" + mock.addr()},
		MqttGateway: config.MqttGatewayConfig{Enabled: true},
	}

	gin.SetMode(gin.TestMode)
	gw := NewMqttGatewayController(nil, nil, nil, nil, nil)
	e := gin.New()
	e.GET("/api/ws/mqtt/broker", gw.HandleWebSocket)
	ts := httptest.NewServer(e)
	defer ts.Close()

	wsURL := "ws" + ts.URL[len("http"):] + "/api/ws/mqtt/broker?X-Device-Token=bogus-token"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err, "WebSocket 应可升级（透传入口不挂 HTTP 中间件）")
	defer ws.Close()

	// 发送 CONNECT（MQTT 3.1.1）
	full := buildConnectPacket(4, "dev-x", "dev-x", "pw")
	require.NoError(t, ws.WriteMessage(websocket.BinaryMessage, full))

	// 应收到 CONNACK not authorized（0x20 0x02 0x00 0x05）
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, []byte{0x20, 0x02, 0x00, 0x05}, data)

	// 外部 broker 不应收到任何帧
	time.Sleep(200 * time.Millisecond)
	assert.Empty(t, mock.received(), "鉴权失败时不应转发任何帧到外部Broker")
}

// TestMqttGatewayControllerFragmentedConnect 模拟 mqtt.js 分片发送 CONNECT（固定头一个 WS 消息，body 一个 WS 消息）
func TestMqttGatewayControllerFragmentedConnect(t *testing.T) {
	mock := newMockMqttBroker(t)
	defer mock.close()

	config.Cfg = &config.Config{
		MQTT:        config.MQTTConfig{BrokerURL: "tcp://" + mock.addr()},
		MqttGateway: config.MqttGatewayConfig{Enabled: true},
	}

	gin.SetMode(gin.TestMode)
	gw := NewMqttGatewayController(nil, nil, nil, nil, nil)
	e := gin.New()
	e.GET("/api/ws/mqtt/broker", gw.HandleWebSocket)
	ts := httptest.NewServer(e)
	defer ts.Close()

	// 构造带 username/password 的 CONNECT（框架签发的 Sa-Token），经原始 WebSocket 发送
	token := deviceLogin(t, "dev-003")
	wsURL := "ws" + ts.URL[len("http"):] + "/api/ws/mqtt/broker"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// 构造完整 CONNECT（MQTT 5.0，带 username/password）
	full := buildConnectPacket(5, "dev-003", "dev-003", token)
	hdrLen := 2 // 0x10 + 1 字节 remaining（小包）
	frag1 := full[:hdrLen]
	frag2 := full[hdrLen:]

	// 模拟 mqtt.js 分片发送
	require.NoError(t, ws.WriteMessage(websocket.BinaryMessage, frag1))
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, ws.WriteMessage(websocket.BinaryMessage, frag2))

	// mock 应收到完整 CONNECT（网关累积后转发）
	require.Eventually(t, func() bool {
		for _, f := range mock.received() {
			if len(f) > 0 && f[0] == 0x10 && strings.Contains(string(f), "dev-003") {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "分片累积失败，mock 未收到完整 CONNECT")
}

func TestMqttPacketTotalLength(t *testing.T) {
	tests := []struct {
		packet []byte
		total  int
		ok     bool
	}{
		{[]byte{0x20, 0x02, 0x00, 0x00}, 4, true},
		{[]byte{0x10, 0x10}, 18, true},
		{[]byte{0x10, 0x80, 0x01}, 131, true}, // 2字节varint，完整=131，buf不够长由调用方判断
		{[]byte{0x10}, 0, false},
		{[]byte{0x00, 0x01}, 0, false},
	}
	for _, tt := range tests {
		total, ok := mqttPacketTotalLength(tt.packet)
		assert.Equal(t, tt.total, total)
		assert.Equal(t, tt.ok, ok)
	}
}

// ============ 设备配置回执上行 (iot/{deviceId}/config/report) ============

// newGatewayConfigEnv 构造「真实 sqlite + miniredis」的网关配置回执环境，
// 预置设备 gwdev1 及一条已保存配置（status=pending, version=1）
func newGatewayConfigEnv(t *testing.T) (*service.DeviceConfigService, *repository.DeviceConfigRepo, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := ent.Open("sqlite3", "file:"+dbPath+"?_fk=1")
	require.NoError(t, err)
	require.NoError(t, client.Schema.Create(context.Background()))
	t.Cleanup(func() { _ = client.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)
	configSvc := service.NewDeviceConfigService(configRepo, service.NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil), nil)

	now := time.Now()
	owner, err := repository.NewUserRepo(client).Create(context.Background(), &ent.User{
		Account: "gw_owner", Passwd: "h", Role: "user", Status: "ACTIVE",
		CreateTime: now, UpdateTime: now,
	})
	require.NoError(t, err)
	_, err = deviceRepo.Create(context.Background(), &ent.Device{
		ID: "gwdev1", DeviceName: "网关测试设备", OwnerID: owner.ID, Status: "ONLINE",
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	_, err = configSvc.Save(context.Background(), "gwdev1", map[string]any{"sensor": map[string]any{"reportInterval": 30}})
	require.NoError(t, err)
	return configSvc, configRepo, "gwdev1"
}

func TestMqttGatewayController_ConfigReportAck(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{MQTT: config.MQTTConfig{BrokerURL: "tcp://127.0.0.1:1883"}}

	configSvc, configRepo, deviceID := newGatewayConfigEnv(t)
	gw := NewMqttGatewayController(nil, nil, configSvc, nil, nil)

	// 设备向 iot/{id}/config/report 发布回执（连接鉴权设备与话题设备一致）
	report := `{"version":1,"config":{"sensor":{"reportInterval":30}}}`
	gw.onPublish("iot/"+deviceID+"/config/report", []byte(report), deviceID)

	require.Eventually(t, func() bool {
		got, err := configRepo.GetByDeviceID(context.Background(), deviceID)
		return err == nil && got.Status == "acked" && got.ReportedVersion == 1
	}, 5*time.Second, 50*time.Millisecond, "MQTT 配置回执未置为 acked")
}

func TestMqttGatewayController_ConfigReportRejectCrossDevice(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{MQTT: config.MQTTConfig{BrokerURL: "tcp://127.0.0.1:1883"}}

	configSvc, configRepo, deviceID := newGatewayConfigEnv(t)
	gw := NewMqttGatewayController(nil, nil, configSvc, nil, nil)

	// 其他设备伪造本设备话题 → 应拒绝，配置保持 pending
	report := `{"version":1,"config":{"sensor":{"reportInterval":30}}}`
	gw.onPublish("iot/"+deviceID+"/config/report", []byte(report), "evil-device")

	time.Sleep(300 * time.Millisecond)
	got, err := configRepo.GetByDeviceID(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "pending", got.Status)
	assert.Equal(t, uint(0), got.ReportedVersion)
}

func TestMqttGatewayController_ConfigReportBadPayload(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{MQTT: config.MQTTConfig{BrokerURL: "tcp://127.0.0.1:1883"}}

	configSvc, configRepo, deviceID := newGatewayConfigEnv(t)
	gw := NewMqttGatewayController(nil, nil, configSvc, nil, nil)

	// 缺少 version / 非 JSON → 丢弃，配置保持 pending
	gw.onPublish("iot/"+deviceID+"/config/report", []byte(`{"config":{}}`), deviceID)
	gw.onPublish("iot/"+deviceID+"/config/report", []byte(`not-json`), deviceID)

	time.Sleep(300 * time.Millisecond)
	got, err := configRepo.GetByDeviceID(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "pending", got.Status)
}

// ============ SUBSCRIBE 上线标记（iot/{deviceId}/config 等） ============

// buildSubscribePacket 构造 MQTT SUBSCRIBE 包（packet id=1，QoS0）
func buildSubscribePacket(protocolLevel byte, topics ...string) []byte {
	var vh []byte
	vh = append(vh, 0x00, 0x01) // packet identifier = 1
	if protocolLevel == 5 {
		vh = append(vh, 0x00) // MQTT 5.0: properties length = 0
	}
	body := vh
	for _, t := range topics {
		body = append(body, encodeStr(t)...)
		body = append(body, 0x00) // QoS 0
	}
	frame := []byte{0x82}
	frame = append(frame, encodeRemaining(len(body))...)
	return append(frame, body...)
}

func TestParseSubscribeTopics(t *testing.T) {
	// MQTT 3.1.1：单一主题
	pkt311 := buildSubscribePacket(4, "iot/dev001/config")
	assert.Equal(t, []string{"iot/dev001/config"}, parseSubscribeTopics(pkt311, 4))

	// MQTT 3.1.1：多主题
	pktMulti := buildSubscribePacket(4, "iot/dev001/config", "iot/dev001/cmd", "iot/dev001/#")
	got := parseSubscribeTopics(pktMulti, 4)
	assert.Equal(t, []string{"iot/dev001/config", "iot/dev001/cmd", "iot/dev001/#"}, got)

	// MQTT 5.0：带 properties 段也应正确跳过
	pkt5 := buildSubscribePacket(5, "iot/dev002/config")
	assert.Equal(t, []string{"iot/dev002/config"}, parseSubscribeTopics(pkt5, 5))

	// 非 SUBSCRIBE 帧（PUBLISH）→ nil
	assert.Nil(t, parseSubscribeTopics([]byte{0x30, 0x02, 0x00, 0x00}, 4))

	// 空包 / 不完整 → nil
	assert.Nil(t, parseSubscribeTopics(nil, 4))
	assert.Nil(t, parseSubscribeTopics([]byte{0x82, 0x05, 0x00, 0x01}, 4)) // 声明剩余长度但无 payload
}

func TestIsOwnSubscribeTopic(t *testing.T) {
	assert.True(t, isOwnSubscribeTopic("iot/dev001/config", "dev001"))
	assert.True(t, isOwnSubscribeTopic("iot/dev001/cmd", "dev001"))
	assert.True(t, isOwnSubscribeTopic("iot/dev001/#", "dev001"))
	assert.True(t, isOwnSubscribeTopic("iot/DEV001/config", "dev001")) // 大小写不敏感

	assert.False(t, isOwnSubscribeTopic("iot/other/config", "dev001"))     // 非本设备
	assert.False(t, isOwnSubscribeTopic("iot/dev001/telemetry", "dev001")) // 非配置/命令通道
	assert.False(t, isOwnSubscribeTopic("iot/dev001", "dev001"))           // 段数不足
	assert.False(t, isOwnSubscribeTopic("dev001/config", "dev001"))        // 无前缀，设备ID不在第2段
}

// newSubscribeEnv 构造「真实 sqlite + miniredis」网关环境，预置设备 gwsub1（OFFLINE）
func newSubscribeEnv(t *testing.T) (*MqttGatewayController, *cache.RedisCache, string) {
	t.Helper()
	// 显式设置配置：NewMqttGatewayController 读取 MqttGateway.IdleTimeoutSec。
	// 仅当全局配置为空时兜底（单测独立运行不 panic）；
	// 不覆盖调用方已设置的 config.Cfg（如 E2E 测试注入的 mock BrokerURL）。
	if config.Cfg == nil {
		oldCfg := config.Cfg
		t.Cleanup(func() { config.Cfg = oldCfg })
		config.Cfg = &config.Config{}
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := ent.Open("sqlite3", "file:"+dbPath+"?_fk=1")
	require.NoError(t, err)
	require.NoError(t, client.Schema.Create(context.Background()))
	t.Cleanup(func() { _ = client.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	rcache := cache.NewRedisCache(rdb)
	t.Cleanup(func() { _ = rcache.Close() })

	now := time.Now()
	owner, err := repository.NewUserRepo(client).Create(context.Background(), &ent.User{
		Account: "gwsub_owner", Passwd: "h", Role: "user", Status: "ACTIVE",
		CreateTime: now, UpdateTime: now,
	})
	require.NoError(t, err)

	deviceRepo := repository.NewDeviceRepo(client)
	_, err = deviceRepo.Create(context.Background(), &ent.Device{
		ID: "gwsub1", DeviceName: "订阅上线测试设备", OwnerID: owner.ID, Status: "OFFLINE",
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	// 预置 Redis 状态缓存为 OFFLINE（模拟真实设备：注册后状态缓存已存在）
	require.NoError(t, rcache.CacheDeviceStatus(context.Background(), "gwsub1", "OFFLINE", now.UnixMilli()))

	deviceSvc := service.NewDeviceService(deviceRepo, rcache, nil, nil, nil)
	gw := NewMqttGatewayController(nil, nil, nil, deviceSvc, rcache)
	return gw, rcache, "gwsub1"
}

func TestMqttGatewayController_SubscribeMarksOnline(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t)

	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/config"), 4, &mqttConn{deviceID: deviceID})

	// Redis 缓存设备状态应改为 ONLINE
	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "ONLINE", status)
}

func TestMqttGatewayController_SubscribeCmdTopicMarksOnline(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t)

	// 只订阅命令主题也应视为上线（MQTT 5.0 帧）
	gw.onSubscribe(buildSubscribePacket(5, "iot/"+deviceID+"/cmd"), 5, &mqttConn{deviceID: deviceID})

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "ONLINE", status)
}

func TestMqttGatewayController_SubscribeCrossDeviceNoMark(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t)

	// 其他连接伪造订阅本设备主题 → 不应标记上线
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/config"), 4, &mqttConn{deviceID: "evil-device"})

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status)
}

func TestMqttGatewayController_SubscribeOwnOnlyOnce(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t)

	// 同一连接多次订阅：仅第一次生效（每连接一次上线标记）
	conn := &mqttConn{deviceID: deviceID}
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/config"), 4, conn)
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/cmd"), 4, conn)
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/#"), 4, conn)

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "ONLINE", status)
}

// ============ 在线/离线状态机（连接登记 / 断连置离线 / 空闲超时） ============

func TestComputeIdleTimeout(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{}

	gw := NewMqttGatewayController(nil, nil, nil, nil, nil)

	// keepalive 正常（60s）→ 1.5 倍 = 90s
	assert.Equal(t, 90*time.Second, gw.computeIdleTimeout(60))
	// keepalive 病态小 → 夹到最小 15s
	assert.Equal(t, 15*time.Second, gw.computeIdleTimeout(1))
	// keepalive 病态大 → 夹到最大 30min
	assert.Equal(t, 30*time.Minute, gw.computeIdleTimeout(36000))
	// keepalive=0（禁用）→ 未配置兜底 5min
	assert.Equal(t, 5*time.Minute, gw.computeIdleTimeout(0))

	// 配置了 idle-timeout-sec → keepalive=0 时使用配置；keepalive 存在时优先级更高
	config.Cfg = &config.Config{MqttGateway: config.MqttGatewayConfig{IdleTimeoutSec: 300}}
	gwCfg := NewMqttGatewayController(nil, nil, nil, nil, nil)
	assert.Equal(t, 300*time.Second, gwCfg.computeIdleTimeout(0))
	assert.Equal(t, 90*time.Second, gwCfg.computeIdleTimeout(60))
}

func TestMqttGatewayController_ConnClosedMarksOffline(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t)

	// 连接建立 → 订阅自身主题上线 → 登记
	conn := &mqttConn{deviceID: deviceID}
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/config"), 4, conn)
	gw.registerConn(conn, nil, nil)
	assert.True(t, gw.IsConnected(deviceID), "连接应已登记")

	// 连接关闭 → 立即置 OFFLINE 并清除登记
	gw.unregisterConn(conn)
	assert.False(t, gw.IsConnected(deviceID), "连接关闭后应移出登记")

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status)
}

func TestMqttGatewayController_DisconnectWithoutSubscribeNoOffline(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t)

	// 连接建立但从未订阅自身主题 → 断开时不应修改业务状态（保持 OFFLINE）
	conn := &mqttConn{deviceID: deviceID}
	gw.registerConn(conn, nil, nil)
	gw.unregisterConn(conn)
	assert.False(t, gw.IsConnected(deviceID))

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status)
}

func TestMqttGatewayController_ReconnectNewConnWins(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t)

	// 旧连接已订阅上线并登记
	oldConn := &mqttConn{deviceID: deviceID}
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/config"), 4, oldConn)
	gw.registerConn(oldConn, nil, nil)

	// 新连接到来顶替旧连接（踢下线）
	newConn := &mqttConn{deviceID: deviceID}
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/config"), 4, newConn)
	gw.registerConn(newConn, nil, nil)
	assert.True(t, gw.IsConnected(deviceID))

	// 旧连接收尾 → 不应误伤新连接（设备保持 ONLINE）
	gw.unregisterConn(oldConn)
	assert.True(t, gw.IsConnected(deviceID), "旧连接关闭不应影响新连接登记")
	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "ONLINE", status)

	// 新连接收尾 → 设备真正离线
	gw.unregisterConn(newConn)
	assert.False(t, gw.IsConnected(deviceID))
	status, _, err = rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status)
}

func TestMqttGatewayController_PresenceCallbacks(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{}

	gw, rcache, deviceID := newSubscribeEnv(t)

	var mu sync.Mutex
	var onlineEvents, offlineEvents []string
	gw.SetPresenceCallbacks(
		func(deviceID string, ownerID uint) {
			mu.Lock()
			onlineEvents = append(onlineEvents, deviceID)
			mu.Unlock()
		},
		func(deviceID string, ownerID uint) {
			mu.Lock()
			offlineEvents = append(offlineEvents, deviceID)
			mu.Unlock()
		},
	)

	conn := &mqttConn{deviceID: deviceID, ownerID: 7}
	gw.registerConn(conn, nil, nil)
	gw.onSubscribe(buildSubscribePacket(4, "iot/"+deviceID+"/config"), 4, conn)
	gw.unregisterConn(conn)

	mu.Lock()
	assert.Equal(t, []string{deviceID}, onlineEvents)
	assert.Equal(t, []string{deviceID}, offlineEvents)
	mu.Unlock()

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status)
}

// TestMqttGatewayController_SubscribeOnlineE2E 端到端：真实 MQTT 客户端（paho, ws://）
// 经网关订阅 iot/{deviceId}/config → Redis 缓存设备状态应变为 ONLINE
func TestMqttGatewayController_SubscribeOnlineE2E(t *testing.T) {
	mock := newMockMqttBroker(t)
	defer mock.close()

	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{
		MQTT:        config.MQTTConfig{BrokerURL: "tcp://" + mock.addr()},
		MqttGateway: config.MqttGatewayConfig{Enabled: true},
	}

	gin.SetMode(gin.TestMode)
	gw, rcache, deviceID := newSubscribeEnv(t)
	e := gin.New()
	e.GET("/api/ws/mqtt/broker", gw.HandleWebSocket)
	ts := httptest.NewServer(e)
	defer ts.Close()

	token := deviceLogin(t, deviceID)
	wsURL := "ws" + ts.URL[len("http"):] + "/api/ws/mqtt/broker"

	opts := mqtt.NewClientOptions().
		AddBroker(wsURL).
		SetClientID("sub-online-device").
		SetUsername(deviceID).
		SetPassword(token)
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	require.True(t, tok.WaitTimeout(8*time.Second), "连接失败: %v", tok.Error())
	require.NoError(t, tok.Error())
	defer client.Disconnect(100)

	// 设备订阅自身配置主题（网关应标记上线）
	subTok := client.Subscribe("iot/"+deviceID+"/config", 1, nil)
	require.True(t, subTok.WaitTimeout(8*time.Second), "订阅超时: %v", subTok.Error())
	require.NoError(t, subTok.Error())

	// Redis 缓存设备状态应为 ONLINE
	require.Eventually(t, func() bool {
		status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
		return err == nil && status == "ONLINE"
	}, 5*time.Second, 50*time.Millisecond, "设备订阅后 Redis 状态未变为 ONLINE")
	client.Disconnect(100)

	// 设备断开（网关瞬时感知，无需等离线同步器）→ 设备状态应立即变为 OFFLINE
	require.Eventually(t, func() bool {
		status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
		return err == nil && status == "OFFLINE"
	}, 5*time.Second, 50*time.Millisecond, "设备断开后 Redis 状态未变为 OFFLINE")
}

// ============ onPublish 上报入库链路（telemetry → FastReportWrite → 缓冲 worker → writer） ============

// recordingBatchWriter 记录缓冲 worker 刷出的上报（DeviceDataBuffer 的 BatchWriter 替身）
type recordingBatchWriter struct {
	mu      sync.Mutex
	reports []service.BufferedReport
}

func (w *recordingBatchWriter) FlushReports(_ context.Context, reports []service.BufferedReport) error {
	w.mu.Lock()
	w.reports = append(w.reports, reports...)
	w.mu.Unlock()
	return nil
}

func (w *recordingBatchWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.reports)
}

func (w *recordingBatchWriter) first() service.BufferedReport {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.reports) == 0 {
		return service.BufferedReport{}
	}
	return w.reports[0]
}

// newPublishEnv 构造「真实 sqlite + miniredis + 缓冲 worker」的 onPublish 全链路环境：
// onPublish → ReportStatus(token→FastReportWrite) → DeviceDataBuffer worker → writer
// influxSvc 传 nil 安全：带缓冲器时走快速路径，不触达同步写 InfluxDB
func newPublishEnv(t *testing.T) (*MqttGatewayController, *cache.RedisCache, string, *recordingBatchWriter) {
	t.Helper()
	if config.Cfg == nil {
		oldCfg := config.Cfg
		t.Cleanup(func() { config.Cfg = oldCfg })
		config.Cfg = &config.Config{}
	}

	dbPath := filepath.Join(t.TempDir(), "publish.db")
	client, err := ent.Open("sqlite3", "file:"+dbPath+"?_fk=1")
	require.NoError(t, err)
	require.NoError(t, client.Schema.Create(context.Background()))
	t.Cleanup(func() { _ = client.Close() })

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	rcache := cache.NewRedisCache(rdb)
	t.Cleanup(func() { _ = rcache.Close() })

	now := time.Now()
	owner, err := repository.NewUserRepo(client).Create(context.Background(), &ent.User{
		Account: "pub_owner", Passwd: "h", Role: "user", Status: "ACTIVE",
		CreateTime: now, UpdateTime: now,
	})
	require.NoError(t, err)

	deviceRepo := repository.NewDeviceRepo(client)
	_, err = deviceRepo.Create(context.Background(), &ent.Device{
		ID: "pubdev1", DeviceName: "上报链路设备", OwnerID: owner.ID, Status: "OFFLINE",
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)

	// 预置 Redis 状态缓存为 OFFLINE（模拟真实设备：注册后状态缓存已存在）
	require.NoError(t, rcache.CacheDeviceStatus(context.Background(), "pubdev1", "OFFLINE", now.UnixMilli()))

	deviceSvc := service.NewDeviceService(deviceRepo, rcache, nil, nil, nil)

	writer := &recordingBatchWriter{}
	buf := service.NewDeviceDataBuffer(rdb, writer)
	buf.Start()
	t.Cleanup(buf.Stop)

	reportSvc := service.NewDeviceReportService(deviceRepo, nil, rcache, deviceSvc)
	reportSvc.SetBuffer(buf)

	gw := NewMqttGatewayController(nil, reportSvc, nil, deviceSvc, rcache)
	return gw, rcache, "pubdev1", writer
}

// TestMqttGatewayController_OnPublishIngestsData 端到端：合法 telemetry
// → 入库（缓冲 worker 收到）+ 设备状态刷新 ONLINE
func TestMqttGatewayController_OnPublishIngestsData(t *testing.T) {
	gw, rcache, deviceID, writer := newPublishEnv(t)
	token := deviceLogin(t, deviceID)
	gw.registerDeviceToken(deviceID, token)

	payload := `{"sensors":[{"name":"temp","type":"temperature","value":25.5},{"name":"humi","type":"humidity","value":60}]}`
	gw.onPublish("iot/"+deviceID+"/telemetry", []byte(payload), deviceID)

	// 缓冲 worker 把上报刷出到 writer（完整入库链路）
	require.Eventually(t, func() bool { return writer.count() == 1 }, 5*time.Second, 20*time.Millisecond, "缓冲队列未刷出上报")
	first := writer.first()
	assert.Equal(t, deviceID, first.DeviceID)
	assert.Len(t, first.Sensors, 2)
	assert.Equal(t, "temp", first.Sensors[0].Name)

	// 上报即刷新在线状态（FastReportWrite 置 ONLINE）
	require.Eventually(t, func() bool {
		status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
		return err == nil && status == "ONLINE"
	}, 5*time.Second, 20*time.Millisecond, "上报后状态未置 ONLINE")
}

// TestMqttGatewayController_OnPublishSkipsBadInput 非法输入静默丢弃：
// 主题段数不足 / payload 非 JSON / sensors 为空 → 不入库不改状态
func TestMqttGatewayController_OnPublishSkipsBadInput(t *testing.T) {
	gw, rcache, deviceID, writer := newPublishEnv(t)
	token := deviceLogin(t, deviceID)
	gw.registerDeviceToken(deviceID, token)

	gw.onPublish("iot/"+deviceID, []byte(`{"sensors":[{"name":"t","type":"temp","value":1}]}`), deviceID)       // 主题段数不足
	gw.onPublish("iot/"+deviceID+"/telemetry", []byte("not-json"), deviceID)                                    // payload 非 JSON
	gw.onPublish("iot/"+deviceID+"/telemetry", []byte(`{"sensors":[]}`), deviceID)                              // sensors 为空
	gw.onPublish("iot/other/telemetry", []byte(`{"sensors":[{"name":"t","type":"temp","value":1}]}`), deviceID) // 其他设备发送
	gw.onPublish("iot/ghost/telemetry", []byte(`{"sensors":[{"name":"t","type":"temp","value":1}]}`), deviceID) // 未注册设备ID

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 0, writer.count(), "非法输入不应入库")

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status, "丢弃的报文不得刷新在线状态")
}

// TestMqttGatewayController_OnPublishWithoutTokenSkips 连接未鉴权（无 token）→ 跳过入库
func TestMqttGatewayController_OnPublishWithoutTokenSkips(t *testing.T) {
	gw, rcache, deviceID, writer := newPublishEnv(t)
	// 不注册设备 token，模拟连接未建立/鉴权未通过

	gw.onPublish("iot/"+deviceID+"/telemetry", []byte(`{"sensors":[{"name":"temp","type":"temperature","value":1}]}`), deviceID)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 0, writer.count(), "无 token 时不得入库")

	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status, "无 token 时不得修改在线状态")
}

// TestMqttGatewayController_OnPublishNilReportSvc 上报服务未注入 → 跳过入库（状态不变）
func TestMqttGatewayController_OnPublishNilReportSvc(t *testing.T) {
	gw, rcache, deviceID := newSubscribeEnv(t) // newSubscribeEnv 的网关 reportSvc=nil、deviceSvc=有
	token := deviceLogin(t, deviceID)
	gw.registerDeviceToken(deviceID, token)

	gw.onPublish("iot/"+deviceID+"/telemetry", []byte(`{"sensors":[{"name":"t","type":"temp","value":1}]}`), deviceID)

	time.Sleep(300 * time.Millisecond)
	status, _, err := rcache.GetCachedDeviceStatus(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status, "reportSvc 未注入时不得改变状态")
}

// TestMqttGatewayController_OnPublishConfigReportBad 配置回执缺 version / 非 JSON → 保持 pending
func TestMqttGatewayController_OnPublishConfigReportBad(t *testing.T) {
	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{MQTT: config.MQTTConfig{BrokerURL: "tcp://127.0.0.1:1883"}}

	configSvc, configRepo, deviceID := newGatewayConfigEnv(t)
	gw := NewMqttGatewayController(nil, nil, configSvc, nil, nil)

	gw.onPublish("iot/"+deviceID+"/config/report", []byte(`{"config":{}}`), deviceID) // version=0 → 缺少版本
	gw.onPublish("iot/"+deviceID+"/config/report", []byte("not-json"), deviceID)      // 非 JSON → 解析失败

	time.Sleep(300 * time.Millisecond)
	got, err := configRepo.GetByDeviceID(context.Background(), deviceID)
	require.NoError(t, err)
	assert.Equal(t, "pending", got.Status, "非法配置回执不得置为 acked")
}
