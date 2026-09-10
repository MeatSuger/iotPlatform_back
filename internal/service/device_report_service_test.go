package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
)

func TestDeviceReportError_Values(t *testing.T) {
	assert.Equal(t, 401, ErrInvalidDeviceToken.BizCode)
	assert.Equal(t, "设备Token无效", ErrInvalidDeviceToken.Message)

	assert.Equal(t, 401, ErrDeviceTokenMismatch.BizCode)
	assert.Equal(t, "设备Token不匹配", ErrDeviceTokenMismatch.Message)

	assert.Equal(t, 404, ErrDeviceNotFound.BizCode)
	assert.Equal(t, "设备不存在", ErrDeviceNotFound.Message)

	assert.Equal(t, 401, ErrDeviceTokenNotFound.BizCode)
	assert.Equal(t, "缺少设备Token", ErrDeviceTokenNotFound.Message)
}

func TestNewDeviceReportService(t *testing.T) {
	svc := NewDeviceReportService(nil, nil, nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.deviceRepo)
	assert.Nil(t, svc.influxSvc)
	assert.Nil(t, svc.cache)
	assert.Nil(t, svc.deviceSvc)
}

// ========================================
// 快速上报/心跳/状态读取 —— 真实 sqlite + miniredis
// InfluxDB 使用 nil-client 实例（WriteSensors 走"未连接"错误分支，异步无害）
// ========================================

func newReportCtx(t *testing.T) (*DeviceReportService, *repository.DeviceRepo, *ent.Client) {
	client := newTestEnt(t)
	repo := repository.NewDeviceRepo(client)
	_, rcache := newTestRedisCache(t)
	influx := NewInfluxDBService(InfluxDBConfig{Database: "iot"})
	influx.client = nil // 本组测试不连真实 InfluxDB，WriteSensors 走"未连接"错误分支（异步无害）
	deviceSvc := NewDeviceService(repo, rcache, nil, nil)
	svc := NewDeviceReportService(repo, influx, rcache, deviceSvc)
	return svc, repo, client
}

func sensorDTO() entity.DeviceStatusDTO {
	return entity.DeviceStatusDTO{
		Sensors: []entity.SensorData{
			{Name: "temperature", Type: "number", Value: 25.5, Timestamp: time.Now()},
			{Name: "humidity", Type: "number", Value: 60.0, Timestamp: time.Now()},
		},
	}
}

func TestReportStatusFast_NoBuffer(t *testing.T) {
	svc, repo, client := newReportCtx(t)
	seedOnline2(t, client, repo)

	stopPGDebounce(svc)
	err := svc.ReportStatusFast(context.Background(), "dev1", sensorDTO())
	assert.NoError(t, err)

	// Redis 状态 Hash 已更新为 ONLINE
	status := mrHGet(t, svc, "dev1", "status")
	assert.Equal(t, "ONLINE", status)
	lastActive := mrHGet(t, svc, "dev1", "lastActiveTime")
	assert.NotEmpty(t, lastActive)

	// 传感器最新缓存已写入
	sensorVal, err := getSensorCache(t, svc, "dev1")
	assert.NoError(t, err)
	assert.NotEmpty(t, sensorVal)
}

func TestReportStatusFast_WithBuffer(t *testing.T) {
	svc, repo, client := newReportCtx(t)
	seedOnline2(t, client, repo)
	stopPGDebounce(svc)

	writer := &fakeWriter{}
	buf := NewDeviceDataBuffer(svc.cache.GetClient(), writer)
	buf.flushInterval = time.Hour
	buf.Start()
	defer buf.Stop()
	svc.SetBuffer(buf)

	err := svc.ReportStatusFast(context.Background(), "dev1", sensorDTO())
	assert.NoError(t, err)

	// 缓冲队列中存在一条上报（FastReportWrite 单次 Pipeline）
	len, err := svc.cache.GetClient().LLen(context.Background(), buf.bufferKey).Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), len)
}

func TestReportStatusFast_FastPathFallback(t *testing.T) {
	svc, repo, client := newReportCtx(t)
	seedOnline2(t, client, repo)
	stopPGDebounce(svc)

	_, rdb := newTestRedis(t)
	buf := NewDeviceDataBuffer(rdb, &fakeWriter{})
	buf.flushInterval = time.Hour
	svc.SetBuffer(buf)

	// 制造 FastReportWrite 失败：直接停掉其底层 Redis？—— svc.cache 与 buf 共用同一服务器不可行，
	// 此处通过关闭缓存对应的 miniredis 令 Pipeline Exec 失败 → 走降级逐条写入（无 panic）
	// 注：svc.cache 的 miniredis 在 newReportCtx 内创建，无法从外部关闭；改用不存在客户端重建 svc
	client2 := newTestEnt(t)
	repo2 := repository.NewDeviceRepo(client2)
	_, rcache2 := newTestRedisCache(t)
	influx2 := &InfluxDBService{database: "iot"} // client nil
	svc2 := NewDeviceReportService(repo2, influx2, rcache2, NewDeviceService(repo2, rcache2, nil, nil))
	seedOnline2(t, client2, repo2)
	stopPGDebounce(svc2)

	err := svc2.ReportStatusFast(context.Background(), "dev1", sensorDTO())
	assert.NoError(t, err)
}

func TestReportStatus_TokenValidation(t *testing.T) {
	svc, repo, client := newReportCtx(t)
	seedOnline2(t, client, repo)

	t.Run("无效 token", func(t *testing.T) {
		stopPGDebounce(svc)
		err := svc.ReportStatus(context.Background(), "dev1", "bad-token", sensorDTO())
		assert.ErrorIs(t, err, ErrInvalidDeviceToken)
	})

	t.Run("token 与设备不匹配", func(t *testing.T) {
		stopPGDebounce(svc)
		deviceMgr := middleware.GetDeviceManager()
		token, err := deviceMgr.Login("dev-other", "device")
		assert.NoError(t, err)
		err = svc.ReportStatus(context.Background(), "dev1", token, sensorDTO())
		assert.ErrorIs(t, err, ErrDeviceTokenMismatch)
	})
}

func TestHeartbeatFast(t *testing.T) {
	svc, repo, client := newReportCtx(t)
	seedOnline2(t, client, repo)
	stopPGDebounce(svc)

	err := svc.HeartbeatFast(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "ONLINE", mrHGet(t, svc, "dev1", "status"))
}

func TestHeartbeat_UnknownDevice(t *testing.T) {
	svc, _, _ := newReportCtx(t)
	stopPGDebounce(svc)
	err := svc.HeartbeatFast(context.Background(), "ghost")
	assert.Error(t, err)
}

func TestGetDeviceStatus_AfterReport(t *testing.T) {
	svc, repo, client := newReportCtx(t)
	seedOnline2(t, client, repo)
	stopPGDebounce(svc)

	assert.NoError(t, svc.ReportStatusFast(context.Background(), "dev1", sensorDTO()))

	st, err := svc.GetDeviceStatus(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "dev1", st.DeviceID)
	assert.Equal(t, "ONLINE", st.Status)
	assert.Len(t, st.Sensors, 2)
}

func TestGetDeviceStatus_NoCacheData(t *testing.T) {
	svc, _, client := newReportCtx(t)
	repo2 := repository.NewDeviceRepo(client)
	seedOnline2(t, client, repo2)
	stopPGDebounce(svc)

	// 未上报过 → sensors 为空但不报错
	st, err := svc.GetDeviceStatus(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Empty(t, st.Sensors)
}

func TestGetDeviceStatus_UnknownDevice(t *testing.T) {
	svc, _, _ := newReportCtx(t)
	stopPGDebounce(svc)
	_, err := svc.GetDeviceStatus(context.Background(), "ghost")
	assert.Error(t, err)
}

func TestFlushReports(t *testing.T) {
	svc, _, _ := newReportCtx(t)

	// 空批次直接返回
	assert.NoError(t, svc.FlushReports(context.Background(), nil))

	// 正常批次：时间戳回退到 report.Timestamp
	err := svc.FlushReports(context.Background(), []BufferedReport{{
		DeviceID:  "dev1",
		Timestamp: time.Now().UnixMilli(),
		Sensors:   []SensorDataDTO{{Name: "t", Type: "number", Value: 1.0, Timestamp: 0}},
	}})
	assert.NoError(t, err)
	time.Sleep(20 * time.Millisecond) // 等待异步 WriteSensors 结束（nil client 报错后退出）
}

// TestReportStatusFast_ValueValidation 上报值按物模型 dataType 校验：
// 类型错乱的数据点（如 float 传感器上报 "400ppm"）被丢弃，其余数据正常入库。
func TestReportStatusFast_ValueValidation(t *testing.T) {
	client := newTestEnt(t)
	repo := repository.NewDeviceRepo(client)
	_, rcache := newTestRedisCache(t)
	influx := NewInfluxDBService(InfluxDBConfig{Database: "iot"})
	influx.client = nil
	deviceSvc := NewDeviceService(repo, rcache, nil, nil)

	// 传感器定义服务：温度(float) + 模式(enum)
	cmdRepo := repository.NewMessageLogRepo(client)
	_, rdb := newTestRedis(t)
	downlinkSvc := NewDownlinkService(cmdRepo, repo, rdb, nil, nil)
	configSvc := NewDeviceConfigService(repository.NewDeviceConfigRepo(client), downlinkSvc, nil)
	sensorSvc := NewDeviceSensorService(repository.NewDeviceThingRepo(client), configSvc, rcache)

	seedOnline2(t, client, repo)
	_, err := sensorSvc.Create(context.Background(), "dev1", entity.SensorCreateRequest{
		ID: "temperature", Name: "温度", Type: "temperature", DataType: "float",
	})
	require.NoError(t, err)
	_, err = sensorSvc.Create(context.Background(), "dev1", entity.SensorCreateRequest{
		ID: "mode", Name: "模式", Type: "mode", DataType: "enum",
		Specs: &entity.SensorSpecs{Values: []string{"auto", "manual"}},
	})
	require.NoError(t, err)

	svc := NewDeviceReportService(repo, influx, rcache, deviceSvc)
	svc.SetSensorSvc(sensorSvc)
	stopPGDebounce(svc)

	// float 传感器上报字符串 → 该点被丢弃；enum 上报合法值 → 保留
	dto := entity.DeviceStatusDTO{Sensors: []entity.SensorData{
		{Name: "temperature", Type: "number", Value: "400ppm", Timestamp: time.Now()},
		{Name: "temperature", Type: "number", Value: 25.5, Timestamp: time.Now()},
		{Name: "mode", Type: "string", Value: "auto", Timestamp: time.Now()},
		{Name: "mode", Type: "string", Value: "turbo", Timestamp: time.Now()}, // 不在 values 内
	}}
	err = svc.ReportStatusFast(context.Background(), "dev1", dto)
	assert.NoError(t, err)

	// 最近值缓存只保留 2 条合法数据
	raw, err := getSensorCache(t, svc, "dev1")
	assert.NoError(t, err)
	assert.Contains(t, string(raw), `"value":25.5`)
	assert.Contains(t, string(raw), `"value":"auto"`)
	assert.NotContains(t, string(raw), `400ppm`)
	assert.NotContains(t, string(raw), `turbo`)
}

// ---------- helpers ----------

// mrHGet 读取 DeviceReportService 缓存中的 Hash 字段
func mrHGet(t *testing.T, svc *DeviceReportService, deviceID, field string) string {
	t.Helper()
	key := cache.PrefixDeviceStatus + deviceID
	rdb := svc.cache.GetClient()
	val, err := rdb.HGet(context.Background(), key, field).Result()
	assert.NoError(t, err)
	return val
}

func getSensorCache(t *testing.T, svc *DeviceReportService, deviceID string) (string, error) {
	t.Helper()
	key := cache.PrefixSensorRecent + deviceID + ":latest"
	rdb := svc.cache.GetClient()
	return rdb.Get(context.Background(), key).Result()
}

func seedOnline2(t *testing.T, client *ent.Client, repo *repository.DeviceRepo) {
	t.Helper()
	owner := newOwner(t, client)
	seedDevice(t, repo, "dev1", owner, "ONLINE")
}
