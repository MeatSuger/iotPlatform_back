package mqtt

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	mqttpaho "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	entity "iot-platform.local/internal/model"
	"iot-platform.local/pkg/config"
)

// ============ mock MQTT broker（记录帧 + 回 CONNACK/PUBACK/PINGRESP） ============

type mockBroker struct {
	ln     net.Listener
	mu     sync.Mutex
	frames [][]byte
}

func newMockBroker(t *testing.T) *mockBroker {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	m := &mockBroker{ln: ln}
	go m.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return m
}

func (m *mockBroker) addr() string { return m.ln.Addr().String() }

func (m *mockBroker) serve() {
	conn, err := m.ln.Accept()
	if err != nil {
		return
	}
	br := bufio.NewReader(conn)
	for {
		header := make([]byte, 1)
		if _, err := io.ReadFull(br, header); err != nil {
			return
		}
		// remaining length（可变字节整数）——同时收集长度字节，保证记录的是完整帧
		rl, mult := 0, 1
		rlBytes := []byte{}
		for i := 0; i < 4; i++ {
			b, err := br.ReadByte()
			if err != nil {
				return
			}
			rlBytes = append(rlBytes, b)
			rl += int(b&0x7f) * mult
			mult *= 128
			if b&0x80 == 0 {
				break
			}
		}
		body := make([]byte, rl)
		if _, err := io.ReadFull(br, body); err != nil {
			return
		}
		frame := append(append([]byte{}, header...), rlBytes...)
		frame = append(frame, body...)

		m.mu.Lock()
		m.frames = append(m.frames, frame)
		m.mu.Unlock()

		switch header[0] & 0xF0 {
		case 0x10: // CONNECT → CONNACK 成功
			_, _ = conn.Write([]byte{0x20, 0x02, 0x00, 0x00})
		case 0x30: // PUBLISH QoS>0 → PUBACK（否则 paho token 卡住）
			if header[0]&0x06 != 0 {
				if id := publishPacketID(frame); id != nil {
					_, _ = conn.Write(append([]byte{0x40, 0x02}, id...))
				}
			}
		case 0xC0: // PINGREQ → PINGRESP
			_, _ = conn.Write([]byte{0xD0, 0x00})
		}
	}
}

// publishPacketID 提取 QoS>0 PUBLISH 帧的 packet identifier（topic 之后的 2 字节）
func publishPacketID(frame []byte) []byte {
	if len(frame) < 7 {
		return nil
	}
	pos := 1
	for pos < len(frame) && frame[pos]&0x80 != 0 {
		pos++
	}
	pos++
	if pos+2 > len(frame) {
		return nil
	}
	tl := int(binary.BigEndian.Uint16(frame[pos : pos+2]))
	pid := pos + 2 + tl
	if pid+2 > len(frame) {
		return nil
	}
	return frame[pid : pid+2]
}

// parseTopic 提取 PUBLISH 帧 topic
func parseTopic(frame []byte) string {
	if len(frame) < 4 {
		return ""
	}
	pos := 1
	for pos < len(frame) && frame[pos]&0x80 != 0 {
		pos++
	}
	pos++
	if pos+2 > len(frame) {
		return ""
	}
	tl := int(binary.BigEndian.Uint16(frame[pos : pos+2]))
	if pos+2+tl > len(frame) {
		return ""
	}
	return string(frame[pos+2 : pos+2+tl])
}

// waitFrame 轮询等待满足条件的帧（或超时）
func (m *mockBroker) waitFrame(t *testing.T, timeout time.Duration, pred func([]byte) bool) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		frames := append([][]byte(nil), m.frames...)
		m.mu.Unlock()
		for _, f := range frames {
			if pred(f) {
				return f
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待帧超时")
	return nil
}

func (m *mockBroker) received() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]byte(nil), m.frames...)
}

// ============ 测试 ============

// TestNewPublisher_Uninitialized 未初始化发布器（nil client）→ 返回错误而非 panic
func TestNewPublisher_Uninitialized(t *testing.T) {
	p := &Publisher{}
	err := p.PublishConfig("dev-001", entity.ConfigEnvelope{})
	assert.ErrorContains(t, err, "未初始化")
	err = p.PublishCommand("dev-001", []byte(`{}`))
	assert.ErrorContains(t, err, "未初始化")
}

// TestPublisher_Disconnected client 存在但未连接 → 跳过发布
func TestPublisher_Disconnected(t *testing.T) {
	client := mqttpaho.NewClient(mqttpaho.NewClientOptions().AddBroker("tcp://127.0.0.1:1"))
	p := &Publisher{client: client} // 未调用 Connect

	err := p.PublishConfig("dev-001", entity.ConfigEnvelope{})
	assert.ErrorContains(t, err, "未连接")
	err = p.PublishCommand("dev-001", []byte(`{}`))
	assert.ErrorContains(t, err, "未连接")
}

// TestPublisher_PublishConfigAndCommand 端到端：真实 paho 客户端 → mock broker
// 验证 topic/QoS/retained 位与 payload 与下行主题约定一致
func TestPublisher_PublishConfigAndCommand(t *testing.T) {
	mock := newMockBroker(t)

	oldCfg := config.Cfg
	t.Cleanup(func() { config.Cfg = oldCfg })
	config.Cfg = &config.Config{MQTT: config.MQTTConfig{BrokerURL: "tcp://" + mock.addr()}}

	p := NewPublisher()

	// 等 CONNECT 帧到达 broker，且 paho 已完成 CONNACK 处理（避免 IsConnected 竞态）
	mock.waitFrame(t, 5*time.Second, func(f []byte) bool { return len(f) > 0 && f[0]&0xF0 == 0x10 })
	require.Eventually(t, func() bool { return p.client.IsConnected() }, 5*time.Second, 10*time.Millisecond, "paho 未连上 mock broker")

	// PublishConfig：retained + QoS1 → iot/{id}/config
	err := p.PublishConfig("dev-001", entity.ConfigEnvelope{
		Version: 3,
		Config:  map[string]any{"sensor": map[string]any{"reportInterval": 30}},
	})
	require.NoError(t, err)

	cfgFrame := mock.waitFrame(t, 5*time.Second, func(f []byte) bool {
		return len(f) > 0 && f[0]&0xF0 == 0x30 && strings.Contains(string(f), "/config")
	})
	assert.Equal(t, byte(0x33), cfgFrame[0]&0xFF, "config 应 QoS1+retained (0x33)")
	assert.Equal(t, "iot/dev-001/config", parseTopic(cfgFrame))
	assert.Contains(t, string(cfgFrame), `"version":3`)

	// PublishCommand：QoS1 非 retained → iot/{id}/cmd，payload 原样透传
	err = p.PublishCommand("dev-001", []byte(`{"id":1,"type":"control","payload":{}}`))
	require.NoError(t, err)

	cmdFrame := mock.waitFrame(t, 5*time.Second, func(f []byte) bool {
		return len(f) > 0 && f[0]&0xF0 == 0x30 && strings.Contains(string(f), "/cmd")
	})
	assert.Equal(t, byte(0x32), cmdFrame[0]&0xFF, "cmd 应 QoS1 非 retained (0x32)")
	assert.Equal(t, "iot/dev-001/cmd", parseTopic(cmdFrame))
	assert.Contains(t, string(cmdFrame), `"type":"control"`)

	// 未发布任何 retained 到 cmd（PublishCommand retained=false 已验证）
	assert.NotEmpty(t, mock.received())
}
