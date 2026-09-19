// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
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
	svc := NewDownlinkService(nil, nil, nil, nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.msgRepo)
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
	cmdRepo := repository.NewMessageLogRepo(client)
	deviceRepo := repository.NewDeviceRepo(client)
	mr, rdb := newTestRedis(t)
	svc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
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

	cmdRepo := repository.NewMessageLogRepo(client)
	svc := NewDownlinkService(cmdRepo, repository.NewDeviceRepo(client), rdb, hub, nil)

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
	svc := NewDownlinkService(repository.NewMessageLogRepo(client), repository.NewDeviceRepo(client), rdb, hub, nil)

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

	sentCmd, err := client.MessageLog.Get(context.Background(), cmd.ID)
	assert.NoError(t, err)
	assert.Equal(t, "sent", sentCmd.Status)
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

	// 已送达
	delivered, err := client.MessageLog.Get(context.Background(), cmd.ID)
	assert.NoError(t, err)
	assert.Equal(t, "delivered", delivered.Status)
}

// capturePublisher 记录 MQTT 命令发布调用（实现 MqttPublisher，配置发布忽略）
type capturePublisher struct {
	payloads [][]byte
}

func (c *capturePublisher) PublishConfig(_ string, _ entity.ConfigEnvelope) error { return nil }

func (c *capturePublisher) PublishCommand(_ string, payload []byte) error {
	c.payloads = append(c.payloads, append([]byte(nil), payload...))
	return nil
}

func TestEnqueueCmd_PublishesMQTTForNonConfig(t *testing.T) {
	client := newTestEnt(t)
	cmdRepo := repository.NewMessageLogRepo(client)
	_, rdb := newTestRedis(t)
	pub := &capturePublisher{}
	svc := NewDownlinkService(cmdRepo, repository.NewDeviceRepo(client), rdb, nil, pub)
	seedCmdDevice(t, client, "dev1")

	// type=control → 发布到 MQTT（payload 与 GET /commands 返回项同构）
	_, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "control",
		Payload: []byte(`{"action":"servo1","value":{"angle":90}}`),
	})
	assert.NoError(t, err)
	require.Len(t, pub.payloads, 1)
	var published DownlinkCmdResponse
	assert.NoError(t, json.Unmarshal(pub.payloads[0], &published))
	assert.Equal(t, "control", published.Type)
	assert.Contains(t, string(published.Payload), `"servo1"`)
}

func TestEnqueueCmd_SkipsMQTTForConfig(t *testing.T) {
	client := newTestEnt(t)
	cmdRepo := repository.NewMessageLogRepo(client)
	_, rdb := newTestRedis(t)
	pub := &capturePublisher{}
	svc := NewDownlinkService(cmdRepo, repository.NewDeviceRepo(client), rdb, nil, pub)
	seedCmdDevice(t, client, "dev1")

	// type=config → 不发布命令主题（由 retained 配置主题专管）
	_, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "config",
		Payload: []byte(`{"version":1,"config":{}}`),
	})
	assert.NoError(t, err)
	assert.Empty(t, pub.payloads)
}

func TestEnqueueCmd_PublisherFailureTolerated(t *testing.T) {
	client := newTestEnt(t)
	cmdRepo := repository.NewMessageLogRepo(client)
	_, rdb := newTestRedis(t)
	svc := NewDownlinkService(cmdRepo, repository.NewDeviceRepo(client), rdb, nil, errConfigPublisher{})
	seedCmdDevice(t, client, "dev1")

	// Broker 不可达：仅告警，命令保存/入队不受影响
	cmd, err := svc.EnqueueCmd(context.Background(), "dev1", DownlinkCmdRequest{
		Type:    "control",
		Payload: []byte(`{"action":"led1","value":{"r":1}}`),
	})
	assert.NoError(t, err)
	assert.Equal(t, "pending", cmd.Status)
}
