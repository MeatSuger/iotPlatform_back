package websocket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newTestClient(deviceID string, ownerID uint) *Client {
	return &Client{Conn: nil, Send: make(chan []byte, 256), DeviceID: deviceID, OwnerID: ownerID}
}

func newTestUserClient(ownerID uint) *Client {
	return &Client{Conn: nil, Send: make(chan []byte, 256), OwnerID: ownerID}
}

func TestNewHub(t *testing.T) {
	h := NewHub()
	assert.NotNil(t, h)
	assert.False(t, h.IsDeviceOnline("dev-none"))
}

func TestHub_RegisterAndUnregister(t *testing.T) {
	h := NewHub()

	c := newTestClient("dev-001", 1)
	h.Register(c)
	assert.True(t, h.IsDeviceOnline("dev-001"))

	h.Unregister(c)
	assert.False(t, h.IsDeviceOnline("dev-001"))
}

func TestHub_DuplicateRegister(t *testing.T) {
	h := NewHub()

	c1 := newTestClient("dev-001", 1)
	h.Register(c1)

	c2 := newTestClient("dev-001", 2) // 同设备不同 owner
	h.Register(c2)
	assert.True(t, h.IsDeviceOnline("dev-001"))

	// 旧连接 channel 已关闭（被踢下线）
	_, ok := <-c1.Send
	assert.False(t, ok)
}

func TestHub_RegisterUser(t *testing.T) {
	h := NewHub()

	// 同一 owner 允许多个管理端连接，且都能收到推送
	u1 := newTestUserClient(100)
	u2 := newTestUserClient(100)
	h.RegisterUser(u1)
	h.RegisterUser(u2)

	h.SendToOwner(100, []byte(`{"type":"deviceOnline"}`))
	for _, u := range []*Client{u1, u2} {
		select {
		case msg := <-u.Send:
			assert.Contains(t, string(msg), "deviceOnline")
		case <-time.After(time.Second):
			t.Error("owner 连接未收到推送")
		}
	}
}

func TestHub_UnregisterUser(t *testing.T) {
	h := NewHub()

	c := newTestUserClient(200)
	h.RegisterUser(c)

	h.Unregister(c)
	// 注销后 channel 关闭：不再投递任何消息
	_, ok := <-c.Send
	assert.False(t, ok, "注销后 Send channel 应已关闭")
}

func TestHub_SendToDevice(t *testing.T) {
	h := NewHub()
	c := newTestClient("dev-001", 1)
	h.Register(c)

	h.SendToDevice("dev-001", []byte(`{"type":"cmd"}`))
	select {
	case msg := <-c.Send:
		assert.Equal(t, `{"type":"cmd"}`, string(msg))
	default:
		t.Error("expected message")
	}

	// 离线设备不 panic
	assert.NotPanics(t, func() { h.SendToDevice("dev-nonexist", []byte("test")) })
}

func TestHub_SendToOwner(t *testing.T) {
	h := NewHub()
	c := newTestUserClient(300)
	h.RegisterUser(c)

	h.SendToOwner(300, []byte(`{"type":"deviceOnline"}`))
	select {
	case msg := <-c.Send:
		assert.Contains(t, string(msg), "deviceOnline")
	default:
		t.Error("expected message")
	}
}

func TestHub_IsDeviceOnline(t *testing.T) {
	h := NewHub()
	assert.False(t, h.IsDeviceOnline("dev-x"))
	h.Register(newTestClient("dev-x", 1))
	assert.True(t, h.IsDeviceOnline("dev-x"))
}
