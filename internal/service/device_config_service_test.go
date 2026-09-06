package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	return NewDeviceConfigService(configRepo, downlinkSvc, nil), client, deviceRepo, configRepo
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

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	svc := NewDeviceConfigService(configRepo, downlinkSvc, nil)

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

// fakeConfigPublisher 记录 MQTT 发布调用的测试替身
type fakeConfigPublisher struct {
	configCalls  []entity.ConfigEnvelope
	commandCalls [][]byte
}

func (f *fakeConfigPublisher) PublishConfig(deviceID string, env entity.ConfigEnvelope) error {
	f.configCalls = append(f.configCalls, env)
	return nil
}

func (f *fakeConfigPublisher) PublishCommand(deviceID string, payload []byte) error {
	f.commandCalls = append(f.commandCalls, append([]byte(nil), payload...))
	return nil
}

// errConfigPublisher 模拟 Broker 不可达的发布器
type errConfigPublisher struct{}

func (errConfigPublisher) PublishConfig(_ string, _ entity.ConfigEnvelope) error {
	return fmt.Errorf("MQTT 未连接")
}

func (errConfigPublisher) PublishCommand(_ string, _ []byte) error {
	return fmt.Errorf("MQTT 未连接")
}

// buildConfigSvcWithPublisher 构造注入发布器的配置服务（发布器与下行服务共享，镜像生产装配）
func buildConfigSvcWithPublisher(t *testing.T, pub MqttPublisher) (*DeviceConfigService, *ent.Client, *repository.DeviceRepo) {
	client := newTestEnt(t)
	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	cmdRepo := repository.NewDownlinkCmdRepo(client)
	_, rdb := newTestRedis(t)

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, pub)
	return NewDeviceConfigService(configRepo, downlinkSvc, pub), client, deviceRepo
}

func TestDeviceConfigService_SavePublishesMQTTConfig(t *testing.T) {
	// Save 后应把最新快照（version/config）发布给 MQTT 发布器
	pub := &fakeConfigPublisher{}
	svc, client, deviceRepo := buildConfigSvcWithPublisher(t, pub)
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")

	_, err := svc.Save(context.Background(), "dev1", map[string]any{"sensor": map[string]any{"reportInterval": 30}})
	assert.NoError(t, err)
	require.Len(t, pub.configCalls, 1)
	assert.Equal(t, uint(1), pub.configCalls[0].Version)
	assert.Contains(t, pub.configCalls[0].Config, "sensor")

	// 再次保存 version 递增，发布器收到新版本
	_, err = svc.Save(context.Background(), "dev1", map[string]any{"sensor": map[string]any{"reportInterval": 60}})
	assert.NoError(t, err)
	require.Len(t, pub.configCalls, 2)
	assert.Equal(t, uint(2), pub.configCalls[1].Version)
}

func TestDeviceConfigService_SaveToleratesPublisherFailure(t *testing.T) {
	// Broker 不可达时发布失败只告警，配置保存与命令下发流程不受影响
	svc, client, deviceRepo := buildConfigSvcWithPublisher(t, errConfigPublisher{})
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")

	cfg, err := svc.Save(context.Background(), "dev1", map[string]any{"camera": map[string]any{"protocol": "smtp"}})
	assert.NoError(t, err)
	assert.Equal(t, uint(1), cfg.Version)
	assert.Equal(t, "pending", cfg.Status)
}
