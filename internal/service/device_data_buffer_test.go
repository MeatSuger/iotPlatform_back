package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

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

func (m *mockBatchWriter) flushedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, batch := range m.flushed {
		n += len(batch)
	}
	return n
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

func TestDeviceDataBuffer_QueueLen_NilClient(t *testing.T) {
	mock := &mockBatchWriter{}
	buf := NewDeviceDataBuffer(nil, mock)

	assert.Panics(t, func() {
		_, err := buf.QueueLen(context.Background())
		if err != nil {
			return
		}
	})
}

func TestBufferedReport_JSONRoundtrip(t *testing.T) {
	original := BufferedReport{
		DeviceID: "369c04",
		Token:    "test-token",
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
	assert.Equal(t, original.Token, restored.Token)
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
