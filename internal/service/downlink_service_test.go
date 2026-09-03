package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/websocket"
)

func TestDownlinkCmdRequest_Fields(t *testing.T) {
	req := DownlinkCmdRequest{
		Type:    "control",
		Payload: []byte(`{"action":"reboot"}`),
	}
	assert.Equal(t, "control", req.Type)
	assert.Equal(t, `{"action":"reboot"}`, string(req.Payload))
}

func TestDownlinkCmdResponse_Fields(t *testing.T) {
	resp := DownlinkCmdResponse{
		ID:      1,
		Type:    "config",
		Payload: []byte(`{"interval":60}`),
	}
	assert.Equal(t, uint(1), resp.ID)
	assert.Equal(t, "config", resp.Type)
	assert.Equal(t, `{"interval":60}`, string(resp.Payload))
}

func TestCmdQueueConstants(t *testing.T) {
	assert.Equal(t, "cmd:queue:", cmdQueuePrefix)
	assert.Equal(t, 200, cmdQueueMaxLen)
}

func TestNewDownlinkService(t *testing.T) {
	svc := NewDownlinkService(nil, nil, nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.cmdRepo)
	assert.Nil(t, svc.deviceRepo)
	assert.Nil(t, svc.rdb)
	assert.Nil(t, svc.wsHub)
}

// ========================================
// 业务逻辑：EnqueueCmd / PollCmd / AckCmd / NotifyOwnerCmd
// 真实 sqlite 仓库 + miniredis 队列 + 真实 WS Hub
// ========================================

func buildDownlinkSvc(t *testing.T) (*DownlinkService, *miniredisSrv, *ent.Client) {
	client := newTestEnt(t)
	cmdRepo := repository.NewDownlinkCmdRepo(client)
	deviceRepo := repository.NewDeviceRepo(client)
	mr, rdb := newTestRedis(t)
	svc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil)
	return svc, mr, client
}

// seedCmdDevice 预置 owner 用户 + 设备行（DownlinkCmd 有 FK），返回 owner ID
func seedCmdDevice(t *testing.T, client *ent.Client, id string) uint {
	t.Helper()
	owner := newOwner(t, client)
	repo := repository.NewDeviceRepo(client)
	seedDevice(t, repo, id, owner, "ONLINE")
	return owner
}

func TestEnqueueCmd_Success(t *testing.T) {
	svc, mr, client := buildDownlinkSvc(t)
	seedCmdDevice(t, client, "dev1")

	cmd, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "control",
		Payload: json.RawMessage(`{"action":"reboot"}`),
	})
	assert.NoError(t, err)
	assert.NotZero(t, cmd.ID)
	assert.Equal(t, "pending", cmd.Status)

	// Redis 队列中存在对应 JSON（DownlinkCmdResponse 结构）
	vals, err := mr.List(cmdQueuePrefix + "dev1")
	assert.NoError(t, err)
	assert.Len(t, vals, 1)
	var resp DownlinkCmdResponse
	assert.NoError(t, json.Unmarshal([]byte(vals[0]), &resp))
	assert.Equal(t, cmd.ID, resp.ID)
	assert.Equal(t, "control", resp.Type)
}

func TestEnqueueCmd_DBCreateError(t *testing.T) {
	svc, _, client := buildDownlinkSvc(t)
	seedCmdDevice(t, client, "dev1")
	assert.NoError(t, client.Close())

	_, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{Type: "x", Payload: []byte(`{}`)})
	assert.ErrorContains(t, err, "保存命令失败")
}

func TestEnqueueCmd_RedisPushError(t *testing.T) {
	svc, mr, client := buildDownlinkSvc(t)
	seedCmdDevice(t, client, "dev1")
	// 关闭 Redis → RPush 失败 → 返回错误（此时 PG 中命令已创建）
	mr.Close()

	_, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{Type: "x", Payload: []byte(`{}`)})
	assert.ErrorContains(t, err, "命令入队失败")
}

func TestEnqueueCmd_WebSocketPush(t *testing.T) {
	client := newTestEnt(t)
	seedCmdDevice(t, client, "dev1")
	mr, rdb := newTestRedis(t)
	hub := websocket.NewHub()

	deviceCh := make(chan []byte, 4)
	hub.Register(&websocket.Client{DeviceID: "dev1", Send: deviceCh})

	cmdRepo := repository.NewDownlinkCmdRepo(client)
	svc := NewDownlinkService(cmdRepo, repository.NewDeviceRepo(client), rdb, hub)

	cmd, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "control",
		Payload: []byte(`{"action":"reboot"}`),
	})
	assert.NoError(t, err)

	select {
	case msg := <-deviceCh:
		var pushed map[string]any
		assert.NoError(t, json.Unmarshal(msg, &pushed))
		assert.Equal(t, "cmd", pushed["type"])
		assert.Equal(t, float64(cmd.ID), pushed["id"])
	case <-time.After(time.Second):
		t.Fatal("未收到 WS 推送")
	}
	_ = mr
}

func TestEnqueueCmd_InvalidPayloadWithHub(t *testing.T) {
	client := newTestEnt(t)
	seedCmdDevice(t, client, "dev1")
	_, rdb := newTestRedis(t)
	hub := websocket.NewHub()
	svc := NewDownlinkService(repository.NewDownlinkCmdRepo(client), repository.NewDeviceRepo(client), rdb, hub)

	// 有 hub 时 payload 必须为合法 JSON，否则返回错误
	_, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "control",
		Payload: []byte(`not-json`),
	})
	assert.Error(t, err)
}

func TestPollCmd_EmptyQueue(t *testing.T) {
	svc, _, _ := buildDownlinkSvc(t)
	cmds, err := svc.PollCmd(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Empty(t, cmds)
}

func TestPollCmd_FullFlow(t *testing.T) {
	svc, _, client := buildDownlinkSvc(t)
	seedCmdDevice(t, client, "dev1")

	// 真实入队 → 真实轮询
	cmd, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "reboot",
		Payload: []byte(`{"p":1}`),
	})
	assert.NoError(t, err)

	cmds, err := svc.PollCmd(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, cmds, 1)
	assert.Equal(t, cmd.ID, cmds[0].ID)
	assert.Equal(t, "reboot", cmds[0].Type)

	// 队列已清空；PG 中命令已标记 sent
	rest, err := svc.PollCmd(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Empty(t, rest)

	pending, err := repository.NewDownlinkCmdRepo(client).ListPending(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Empty(t, pending)
}

func TestPollCmd_SkipMalformedEntries(t *testing.T) {
	svc, mr, client := buildDownlinkSvc(t)
	seedCmdDevice(t, client, "dev1")

	cmd, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "config",
		Payload: []byte(`{"interval":60}`),
	})
	assert.NoError(t, err)
	// 追加一条损坏 JSON
	_, err = mr.RPush(cmdQueuePrefix+"dev1", "not-json{{")
	assert.NoError(t, err)

	cmds, err := svc.PollCmd(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, cmds, 1)
	assert.Equal(t, cmd.ID, cmds[0].ID)
}

func TestPollCmd_RedisError(t *testing.T) {
	svc, mr, _ := buildDownlinkSvc(t)
	mr.SetError("connection refused")
	_, err := svc.PollCmd(context.Background(), "dev1")
	assert.ErrorContains(t, err, "轮询命令失败")
}

func TestAckCmd_FullFlow(t *testing.T) {
	svc, _, client := buildDownlinkSvc(t)
	seedCmdDevice(t, client, "dev1")

	cmd, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "control",
		Payload: []byte(`{"a":1}`),
	})
	assert.NoError(t, err)
	assert.NoError(t, svc.AckCmd(context.Background(), cmd.ID))

	// 已送达：不再处于 pending
	pending, err := repository.NewDownlinkCmdRepo(client).ListPending(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Empty(t, pending)
}

func TestNotifyOwnerCmd(t *testing.T) {
	t.Run("hub 为空直接返回", func(t *testing.T) {
		svc, _, client := buildDownlinkSvc(t)
		seedCmdDevice(t, client, "dev1")
		cmd, _ := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{Type: "t", Payload: []byte(`{"a":1}`)})
		svc.NotifyOwnerCmd("dev1", cmd) // 不 panic
	})

	t.Run("payload 非法 JSON 静默返回", func(t *testing.T) {
		client := newTestEnt(t)
		_, rdb := newTestRedis(t)
		svc := NewDownlinkService(repository.NewDownlinkCmdRepo(client), repository.NewDeviceRepo(client), rdb, websocket.NewHub())
		svc.NotifyOwnerCmd("dev1", &ent.DownlinkCmd{Payload: `oops`}) // 不 panic
	})

	t.Run("owner 在线收到 cmdSent", func(t *testing.T) {
		client := newTestEnt(t)
		seedCmdDevice(t, client, "dev1")
		_, rdb := newTestRedis(t)
		hub := websocket.NewHub()

		ownerCh := make(chan []byte, 4)
		hub.Register(&websocket.Client{DeviceID: "dev1", OwnerID: 42, Send: make(chan []byte, 4)})
		hub.RegisterUser(&websocket.Client{OwnerID: 42, Send: ownerCh})

		cmdRepo := repository.NewDownlinkCmdRepo(client)
		svc := NewDownlinkService(cmdRepo, repository.NewDeviceRepo(client), rdb, hub)
		cmd, _ := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{Type: "t", Payload: []byte(`{"a":1}`)})

		svc.NotifyOwnerCmd("dev1", cmd)
		select {
		case msg := <-ownerCh:
			var pushed map[string]any
			assert.NoError(t, json.Unmarshal(msg, &pushed))
			assert.Equal(t, "cmdSent", pushed["type"])
			assert.Equal(t, float64(cmd.ID), pushed["cmdId"])
			assert.Equal(t, "dev1", pushed["deviceId"])
		case <-time.After(time.Second):
			t.Fatal("未收到 owner 通知")
		}
	})
}
