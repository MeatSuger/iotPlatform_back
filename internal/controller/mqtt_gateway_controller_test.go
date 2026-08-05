package controller

import (
	"bufio"
	"encoding/binary"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform.local/internal/middleware"
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

// buildConnectPacket 构造 MQTT CONNECT 包
func buildConnectPacket(protocolLevel byte, clientID, username, password string) []byte {
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
	vh = append(vh, flags, 0x00, 0x3C) // flags + keepalive(60)

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
	// MQTT 3.1.1
	pkt311 := buildConnectPacket(4, "dev-001", "dev-001", "token-abc")
	_, u, p, level, ok := parseConnectCredentials(pkt311)
	assert.True(t, ok)
	assert.Equal(t, "dev-001", u)
	assert.Equal(t, "token-abc", p)
	assert.Equal(t, byte(4), level)

	// MQTT 5.0
	pkt5 := buildConnectPacket(5, "dev-002", "dev-002", "token-xyz")
	_, u, p, level, ok = parseConnectCredentials(pkt5)
	assert.True(t, ok)
	assert.Equal(t, "dev-002", u)
	assert.Equal(t, "token-xyz", p)
	assert.Equal(t, byte(5), level)

	// 非 CONNECT 包
	_, _, _, _, ok = parseConnectCredentials([]byte{0x30, 0x02, 0x00, 0x00})
	assert.False(t, ok)

	// 空包
	_, _, _, _, ok = parseConnectCredentials(nil)
	assert.False(t, ok)
}

// ============ 端到端桥接测试 ============
// mockMqttBroker 模拟外部 MQTT Docker：记录收到的帧，可按需回包
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
	gateway := NewMqttGatewayController(nil, nil)
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
	gw := NewMqttGatewayController(nil, nil)
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
	gw := NewMqttGatewayController(nil, nil)
	e := gin.New()
	e.GET("/api/ws/mqtt/broker", gw.HandleWebSocket)
	ts := httptest.NewServer(e)
	defer ts.Close()

	// 标准 MQTT 客户端：CONNECT username=设备ID / password=设备Token（框架签发的 Sa-Token）
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
