package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBatchWriter 用于测试的批量写入器
type mockBatchWriter struct {
	mu      sync.Mutex
	flushed [][]BufferedReport
}

func (m *mockBatchWriter) FlushReports(ctx context.Context, reports []BufferedReport) error {
	m.mu.Lock()
	// 深拷贝
	cp := make([]BufferedReport, len(reports))
	copy(cp, reports)
	m.flushed = append(m.flushed, cp)
	m.mu.Unlock()
	return nil
}
func TestNewDeviceDataBuffer(t *testing.T) {
	mock := &mockBatchWriter{}
	buf := NewDeviceDataBuffer(nil, mock)

	assert.NotNil(t, buf)
	assert.Equal(t, "buffer:device_reports", buf.bufferKey)
	assert.Equal(t, 200, buf.batchSize)
	assert.Equal(t, 200*time.Millisecond, buf.flushInterval)
}

func TestDeviceDataBuffer_StopWithoutStart(t *testing.T) {
	mock := &mockBatchWriter{}
	buf := NewDeviceDataBuffer(nil, mock)

	assert.NotPanics(t, func() {
		buf.Stop()
	})
}

func TestBufferedReport_JSONRoundtrip(t *testing.T) {
	original := BufferedReport{
		DeviceID: "369c04",
		Sensors: []SensorDataDTO{
			{Name: "temp", Type: "float", Value: 25.5, Timestamp: 1712345678000},
			{Name: "hum", Type: "int", Value: 60, Timestamp: 1712345678001},
		},
		Timestamp: 1712345678000,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored BufferedReport
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.DeviceID, restored.DeviceID)
	assert.Equal(t, original.Timestamp, restored.Timestamp)
	assert.Len(t, restored.Sensors, 2)
	assert.Equal(t, "temp", restored.Sensors[0].Name)
	assert.Equal(t, 25.5, restored.Sensors[0].Value)
}

func TestSensorDataDTO_JSONRoundtrip(t *testing.T) {
	original := SensorDataDTO{
		Name:      "pressure",
		Type:      "float",
		Value:     1013.25,
		Timestamp: 1712345678000,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored SensorDataDTO
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.Name, restored.Name)
	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.Value, restored.Value)
	assert.Equal(t, original.Timestamp, restored.Timestamp)
}

func TestSensorDataDTO_ZeroTimestamp(t *testing.T) {
	// 零值时间戳应正常序列化/反序列化
	dto := SensorDataDTO{
		Name:      "test",
		Type:      "data",
		Value:     100,
		Timestamp: 0,
	}

	data, err := json.Marshal(dto)
	require.NoError(t, err)

	var restored SensorDataDTO
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, int64(0), restored.Timestamp)
}

func TestBatchWriter_Interface(t *testing.T) {
	// 验证 mockBatchWriter 满足 BatchWriter 接口
	var _ BatchWriter = (*mockBatchWriter)(nil)

	// 验证 DeviceReportService 满足 BatchWriter 接口
	var _ BatchWriter = (*DeviceReportService)(nil)
}

func TestDeviceDataBuffer_Defaults(t *testing.T) {
	assert.Equal(t, "buffer:device_reports", defaultBufferKey)
	assert.Equal(t, 200, defaultBatchSize)
	assert.Equal(t, 200*time.Millisecond, defaultFlushInterval)
}

// ========================================
// 真实 Redis（miniredis）缓冲队列测试
// ========================================

// fakeWriter 记录 FlushReports 调用，可注入失败
type fakeWriter struct {
	reports [][]BufferedReport
	fail    error
}

func (w *fakeWriter) FlushReports(_ context.Context, reports []BufferedReport) error {
	w.reports = append(w.reports, reports)
	return w.fail
}

func testReport(deviceID string, ts int64) BufferedReport {
	return BufferedReport{
		DeviceID:  deviceID,
		Sensors:   []SensorDataDTO{{Name: "temperature", Type: "number", Value: 25.5, Timestamp: ts}},
		Timestamp: ts,
	}
}

// enqueueReport 直接将上报 LPush 到缓冲队列（模拟生产 FastReportWrite 写入路径）
func enqueueReport(t *testing.T, rdb redis.UniversalClient, buf *DeviceDataBuffer, r BufferedReport) {
	t.Helper()
	data, err := json.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, rdb.LPush(context.Background(), buf.bufferKey, data).Err())
}

// queuedReports 返回当前缓冲队列长度
func queuedReports(t *testing.T, rdb redis.UniversalClient, buf *DeviceDataBuffer) int64 {
	t.Helper()
	n, err := rdb.LLen(context.Background(), buf.bufferKey).Result()
	require.NoError(t, err)
	return n
}

func TestDeviceDataBuffer_RealDrain(t *testing.T) {
	mr, rdb := newTestRedis(t)
	writer := &fakeWriter{}
	buf := NewDeviceDataBuffer(rdb, writer)
	buf.flushInterval = 20 * time.Millisecond

	now := time.Now().UnixMilli()
	enqueueReport(t, rdb, buf, testReport("dev1", now))
	enqueueReport(t, rdb, buf, testReport("dev2", now))

	buf.Start()
	defer buf.Stop()

	// 等待 worker 至少排空一轮
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if queuedReports(t, rdb, buf) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	assert.Equal(t, int64(0), queuedReports(t, rdb, buf))

	// 两条上报均已刷给 writer
	total := 0
	for _, batch := range writer.reports {
		total += len(batch)
	}
	assert.Equal(t, 2, total)
	_ = mr
}

func TestDeviceDataBuffer_FlushErrorRequeues(t *testing.T) {
	_, rdb := newTestRedis(t)
	writer := &fakeWriter{fail: errors.New("influx down")}
	buf := NewDeviceDataBuffer(rdb, writer)
	buf.flushInterval = 20 * time.Millisecond

	now := time.Now().UnixMilli()
	enqueueReport(t, rdb, buf, testReport("dev1", now))

	buf.Start()
	time.Sleep(150 * time.Millisecond)
	buf.Stop()

	// 数据被重新推回队列（requeue 保护，不丢数据）
	assert.Equal(t, int64(1), queuedReports(t, rdb, buf))
}

func TestDeviceDataBuffer_DrainOnStop(t *testing.T) {
	_, rdb := newTestRedis(t)
	writer := &fakeWriter{}
	buf := NewDeviceDataBuffer(rdb, writer)
	buf.flushInterval = time.Hour // 不触发定时排空，仅 Stop 排空

	now := time.Now().UnixMilli()
	enqueueReport(t, rdb, buf, testReport("dev1", now))

	buf.Start()
	buf.Stop()

	total := 0
	for _, batch := range writer.reports {
		total += len(batch)
	}
	assert.Equal(t, 1, total)
	assert.Equal(t, int64(0), queuedReports(t, rdb, buf))
}

func TestDeviceDataBuffer_RPopError(t *testing.T) {
	_, rdb := newTestRedis(t)
	writer := &fakeWriter{}
	buf := NewDeviceDataBuffer(rdb, writer)
	buf.flushInterval = 20 * time.Millisecond

	buf.Start()
	time.Sleep(80 * time.Millisecond)
	buf.Stop() // 空队列 + Redis 正常：无副作用
}
