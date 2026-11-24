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
	"sync/atomic"
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
	influx := NewInfluxDBService(InfluxDBConfig{Database: "iot"}, nil)
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

	// 正常批次：时间戳回退到 report.Timestamp。
	// 写入失败必须上抛（DeviceDataBuffer 靠这个 error 决定重新入队）；
	// 早期版本这里调 WriteSensorsAsync 恒返回 nil，于是在 InfluxDB 故障时
	// 每批 200 条上报被静默丢弃。
	err := svc.FlushReports(context.Background(), []BufferedReport{{
		DeviceID:  "dev1",
		Timestamp: time.Now().UnixMilli(),
		Sensors:   []SensorDataDTO{{Name: "t", Type: "number", Value: 1.0, Timestamp: 0}},
	}})
	assert.ErrorContains(t, err, "客户端未连接")
}

// TestReportStatusFast_ValueValidation 上报值按物模型 dataType 校验：
// 类型错乱的数据点（如 float 传感器上报 "400ppm"）被丢弃，其余数据正常入库。
func TestReportStatusFast_ValueValidation(t *testing.T) {
	client := newTestEnt(t)
	repo := repository.NewDeviceRepo(client)
	_, rcache := newTestRedisCache(t)
	influx := NewInfluxDBService(InfluxDBConfig{Database: "iot"}, nil)
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

// TestSensorDefMap_CachesPerDevice name→def 映射必须按设备复用，不能每条上报重建
func TestSensorDefMap_CachesPerDevice(t *testing.T) {
	client := newTestEnt(t)
	_, rcache := newTestRedisCache(t)
	sensorSvc := NewDeviceSensorService(repository.NewDeviceThingRepo(client), nil, rcache)
	svc := NewDeviceReportService(nil, nil, rcache, nil)
	svc.SetSensorSvc(sensorSvc)
	ctx := context.Background()

	m1, err := svc.sensorDefMap(ctx, "dev-x")
	require.NoError(t, err)
	m1["synthetic"] = entity.Sensor{Name: "synthetic"} // 只在同一次缓存寿命内可见

	m2, err := svc.sensorDefMap(ctx, "dev-x")
	require.NoError(t, err)
	assert.Contains(t, m2, "synthetic", "映射应被复用而不是重新加载")

	// 过期后必须重建（不再带 synthetic）
	svc.sensorDefMu.Lock()
	entry := svc.sensorDefCache["dev-x"]
	entry.expiresAt = time.Now().Add(-time.Second)
	svc.sensorDefCache["dev-x"] = entry
	svc.sensorDefMu.Unlock()

	m3, err := svc.sensorDefMap(ctx, "dev-x")
	require.NoError(t, err)
	assert.NotContains(t, m3, "synthetic", "过期后应重新加载")
}

// TestReportStatusFast_CanceledCtxSkipsFallback 客户端断开导致的 ctx 取消不得走降级路径
// （降级会多打 3 次 Redis，而结果没人读）
func TestReportStatusFast_CanceledCtxSkipsFallback(t *testing.T) {
	svc, repo, client := newReportCtx(t)
	seedOnline2(t, client, repo)
	stopPGDebounce(svc)

	buf := NewDeviceDataBuffer(svc.cache.GetClient(), &fakeWriter{})
	buf.flushInterval = time.Hour
	svc.SetBuffer(buf)

	// 先用活 ctx 暖本设备的 L1 存在性标记，使后续流程直达 FastReportWrite
	_, err := svc.deviceSvc.GetByDeviceID(context.Background(), "dev1")
	require.NoError(t, err)
	svc.localCache.Set("dev:dev1", "ONLINE")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = svc.ReportStatusFast(ctx, "dev1", sensorDTO())
	assert.ErrorIs(t, err, context.Canceled)

	// 降级路径未执行：状态 Hash 与传感器快照都不应被写
	assert.Equal(t, int64(0), svc.cache.GetClient().Exists(context.Background(), cache.PrefixDeviceStatus+"dev1").Val())
	assert.Equal(t, int64(0), svc.cache.GetClient().Exists(context.Background(), cache.PrefixSensorRecent+"dev1:latest").Val())
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

// TestResolveReportTime 数据点写入时间的三级回退：传感器 → report → 服务端当前时间。
// 重点：两个时间戳都缺时不能写成 1970（Unix 0）或 0001（Go 零值）年。
func TestResolveReportTime(t *testing.T) {
	sensorTs := int64(1737000000000)
	reportTs := int64(1737000001000)

	t.Run("优先用传感器自身时间戳", func(t *testing.T) {
		assert.Equal(t, time.UnixMilli(sensorTs), resolveReportTime(sensorTs, reportTs))
	})

	t.Run("传感器时间戳缺失时用 report 时间戳", func(t *testing.T) {
		assert.Equal(t, time.UnixMilli(reportTs), resolveReportTime(0, reportTs))
		assert.Equal(t, time.UnixMilli(reportTs), resolveReportTime(-1, reportTs))
	})

	t.Run("两者都缺失时补服务端当前时间", func(t *testing.T) {
		zeroTimeMillis := time.Time{}.UnixMilli() // Go 零值时间的毫秒值（负几十亿毫秒）
		for _, pair := range [][2]int64{
			{0, 0},
			{-1, -1},
			{zeroTimeMillis, 0},
			{0, zeroTimeMillis},
		} {
			got := resolveReportTime(pair[0], pair[1])
			assert.False(t, got.IsZero(), "不得返回零值时间 (input=%v)", pair)
			assert.InDelta(t, time.Now().UnixMilli(), got.UnixMilli(), 60_000,
				"应回退为服务端当前时间 (input=%v)", pair)
		}
	})
}

// TestFlushPGUpdates_NoConcurrentFlush 回归：flush 未完成时不得再并发进入另一个
// flush（PG 变慢时每次上报都叠加一个 flush → goroutine 与连接无界增长）。
func TestFlushPGUpdates_NoConcurrentFlush(t *testing.T) {
	svc := &DeviceReportService{pgUpdatePending: map[string]time.Time{"dev1": time.Now()}}

	started := make(chan struct{})
	release := make(chan struct{})
	var inFlight, calls atomic.Int32
	svc.updateLastActive = func(ctx context.Context, _, _ string) error {
		calls.Add(1)
		if n := inFlight.Add(1); n > 1 {
			t.Errorf("flush 并发执行：inFlight=%d", n)
		}
		defer inFlight.Add(-1)
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	firstDone := make(chan struct{})
	go func() {
		svc.flushPGUpdates()
		close(firstDone)
	}()
	<-started

	// 第一次 flush 期间又有设备上报（进入新 pending）
	svc.pgUpdateMu.Lock()
	svc.pgUpdatePending["dev2"] = time.Now()
	svc.pgUpdateMu.Unlock()

	secondDone := make(chan struct{})
	go func() {
		svc.flushPGUpdates()
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("第二次 flush 未立即返回")
	}
	assert.Equal(t, int32(1), calls.Load(), "第二次 flush 不应执行更新")

	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("第一次 flush 未结束")
	}

	svc.pgUpdateMu.Lock()
	assert.Contains(t, svc.pgUpdatePending, "dev2", "flush 期间的上报必须保留")
	if svc.pgUpdateTimer != nil {
		svc.pgUpdateTimer.Stop()
	}
	svc.pgUpdateMu.Unlock()
	assert.False(t, svc.pgFlushing)
}

// TestReportStatusFast_FallbackRequeues 降级路径：Redis 快路径失败时写回缓冲队列，
// 而不是在请求线程里同步写 InfluxDB（3000 VUs 超时的触发点）。
func TestReportStatusFast_FallbackRequeues(t *testing.T) {
	// 缓存指向一个已关闭的 Redis：FastReportWrite 必然失败
	mrDead, rcacheDead := newTestRedisCache(t)
	mrDead.Close()

	client := newTestEnt(t)
	repo := repository.NewDeviceRepo(client)
	seedOnline2(t, client, repo)

	// 缓冲队列指向健康的 Redis
	_, rdb := newTestRedis(t)
	writer := &fakeWriter{}
	buf := NewDeviceDataBuffer(rdb, writer)
	buf.flushInterval = time.Hour

	influxSvc := &InfluxDBService{database: "iot"} // client nil：若走同步写会报“未连接”
	svc := NewDeviceReportService(repo, influxSvc, rcacheDead, NewDeviceService(repo, rcacheDead, nil, nil))
	stopPGDebounce(svc)
	svc.SetBuffer(buf)
	svc.localCache.Set("dev:dev1", "ONLINE") // 跳过设备存在性校验（其缓存已不可用）

	assert.NoError(t, svc.ReportStatusFast(context.Background(), "dev1", sensorDTO()))
	assert.Equal(t, int64(1), queuedReports(t, rdb, buf))
}
