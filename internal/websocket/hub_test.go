package websocket

import (
	"testing"

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
	assert.Equal(t, 0, h.ClientCount())
	assert.Equal(t, 0, h.DeviceClientCount())
	assert.Equal(t, 0, h.UserClientCount())
}

func TestHub_RegisterAndUnregister(t *testing.T) {
	h := NewHub()

	c := newTestClient("dev-001", 1)
	h.Register(c)
	assert.Equal(t, 1, h.ClientCount())
	assert.Equal(t, 1, h.DeviceClientCount())
	assert.True(t, h.IsDeviceOnline("dev-001"))

	h.Unregister(c)
	assert.Equal(t, 0, h.ClientCount())
	assert.False(t, h.IsDeviceOnline("dev-001"))
}

func TestHub_DuplicateRegister(t *testing.T) {
	h := NewHub()

	c1 := newTestClient("dev-001", 1)
	h.Register(c1)

	c2 := newTestClient("dev-001", 2) // 同设备不同 owner
	h.Register(c2)
	assert.Equal(t, 1, h.ClientCount()) // 旧连接被踢

	// 旧连接 channel 已关闭
	_, ok := <-c1.Send
	assert.False(t, ok)
}

func TestHub_RegisterUser(t *testing.T) {
	h := NewHub()

	h.RegisterUser(newTestUserClient(100))
	assert.Equal(t, 1, h.UserClientCount())

	h.RegisterUser(newTestUserClient(100)) // 同 owner 第二个连接
	assert.Equal(t, 2, h.UserClientCount())
}

func TestHub_UnregisterUser(t *testing.T) {
	h := NewHub()

	c := newTestUserClient(200)
	h.RegisterUser(c)
	assert.Equal(t, 1, h.UserClientCount())

	h.Unregister(c)
	assert.Equal(t, 0, h.UserClientCount())
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

func TestHub_SendToDeviceOwner(t *testing.T) {
	h := NewHub()

	dev := newTestClient("dev-001", 400)
	h.Register(dev)

	user := newTestUserClient(400)
	h.RegisterUser(user)

	h.SendToDeviceOwner("dev-001", []byte(`{"type":"cmdSent"}`))
	select {
	case msg := <-user.Send:
		assert.Contains(t, string(msg), "cmdSent")
	default:
		t.Error("expected message")
	}
}

func TestHub_Broadcast(t *testing.T) {
	h := NewHub()
	c1 := newTestClient("d1", 1)
	c2 := newTestClient("d2", 2)
	h.Register(c1)
	h.Register(c2)

	h.Broadcast("hello")
	for _, c := range []*Client{c1, c2} {
		select {
		case msg := <-c.Send:
			assert.Equal(t, "hello", string(msg))
		default:
			t.Error("expected broadcast message")
		}
	}
}

func TestHub_GetOnlineDevices(t *testing.T) {
	h := NewHub()
	assert.Empty(t, h.GetOnlineDevices())

	h.Register(newTestClient("a1b2c3", 1))
	h.Register(newTestClient("d4e5f6", 2))
	assert.Len(t, h.GetOnlineDevices(), 2)
}

func TestHub_IsDeviceOnline(t *testing.T) {
	h := NewHub()
	assert.False(t, h.IsDeviceOnline("dev-x"))
	h.Register(newTestClient("dev-x", 1))
	assert.True(t, h.IsDeviceOnline("dev-x"))
}
