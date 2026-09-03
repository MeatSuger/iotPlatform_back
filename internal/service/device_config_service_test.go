package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
)

// buildConfigSvc 构造「真实 sqlite 仓库 + miniredis 命令队列」的配置服务
func buildConfigSvc(t *testing.T) (*DeviceConfigService, *ent.Client, *repository.DeviceRepo, *repository.DeviceConfigRepo) {
	client := newTestEnt(t)
	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)
	_, rdb := newTestRedis(t)

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil)
	return NewDeviceConfigService(configRepo, downlinkSvc), client, deviceRepo, configRepo
}

func TestDeviceConfigService_SaveAndGet(t *testing.T) {
	svc, client, deviceRepo, _ := buildConfigSvc(t)
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")

	// 首次保存：version=1，并下发 config 命令
	cfg, err := svc.Save(context.Background(), "dev1", map[string]any{"sensor": map[string]any{"reportInterval": 60}})
	assert.NoError(t, err)
	assert.Equal(t, uint(1), cfg.Version)
	assert.Equal(t, "pending", cfg.Status)

	// 再次保存：version 递增为 2
	cfg2, err := svc.Save(context.Background(), "dev1", map[string]any{"sensor": map[string]any{"reportInterval": 120}})
	assert.NoError(t, err)
	assert.Equal(t, uint(2), cfg2.Version)

	// Get 返回最新快照
	got, err := svc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, uint(2), got.Version)
}

func TestDeviceConfigService_GetNotExist(t *testing.T) {
	svc, client, deviceRepo, _ := buildConfigSvc(t)
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")

	got, err := svc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeviceConfigService_SaveEnqueuesCommand(t *testing.T) {
	client := newTestEnt(t)
	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)
	mr, rdb := newTestRedis(t)

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil)
	svc := NewDeviceConfigService(configRepo, downlinkSvc)

	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")

	_, err := svc.Save(context.Background(), "dev1", map[string]any{"camera": map[string]any{"protocol": "smtp"}})
	assert.NoError(t, err)

	// Redis 命令队列中存在 type=config 的下发命令
	vals, err := mr.List(cmdQueuePrefix + "dev1")
	assert.NoError(t, err)
	assert.Len(t, vals, 1)
	assert.Contains(t, vals[0], `"type":"config"`)
	assert.Contains(t, vals[0], `"version":1`)
}

func TestDeviceConfigService_Report(t *testing.T) {
	svc, client, deviceRepo, configRepo := buildConfigSvc(t)
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")

	_, err := svc.Save(context.Background(), "dev1", map[string]any{"actuator": map[string]any{"mode": "auto"}})
	assert.NoError(t, err)

	err = svc.Report(context.Background(), "dev1", entity.DeviceConfigReport{
		Version: 1,
		Config:  map[string]any{"actuator": map[string]any{"mode": "auto"}},
	})
	assert.NoError(t, err)

	got, err := configRepo.GetByDeviceID(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "acked", got.Status)
	assert.Equal(t, uint(1), got.ReportedVersion)
	assert.JSONEq(t, `{"actuator":{"mode":"auto"}}`, got.ReportedPayload)
}

func TestDeviceConfigService_DefaultConfigRoundtrip(t *testing.T) {
	// 默认协议样例可序列化并整体保存
	svc, client, deviceRepo, _ := buildConfigSvc(t)
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")

	cfg, err := svc.Save(context.Background(), "dev1", entity.DefaultDeviceConfig())
	assert.NoError(t, err)
	assert.Equal(t, uint(1), cfg.Version)

	got, err := svc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Contains(t, got.Payload, `"camera"`)
	assert.Contains(t, got.Payload, `"ota"`)
}
