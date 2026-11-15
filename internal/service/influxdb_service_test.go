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
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/stretchr/testify/assert"
)

// testConfig 创建测试用 InfluxDBConfig
func testConfig(url string) InfluxDBConfig {
	return InfluxDBConfig{
		URL:                   url,
		Token:                 "token",
		Database:              "my-database",
		AuthScheme:            "Bearer",
		WriteTimeout:          10 * time.Second,
		QueryTimeout:          2 * time.Minute,
		IdleConnectionTimeout: 90 * time.Second,
		MaxIdleConnections:    10,
	}
}

func TestSensorData_Fields(t *testing.T) {
	d := SensorData{
		DeviceID:   "abc123",
		SensorName: "temperature",
		Type:       "number",
		Value:      26.5,
	}
	assert.Equal(t, "abc123", d.DeviceID)
	assert.Equal(t, "temperature", d.SensorName)
	assert.Equal(t, "number", d.Type)
	assert.Equal(t, 26.5, d.Value)
	assert.True(t, d.Timestamp.IsZero())
}

func TestNewInfluxDBService(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"), nil)
	assert.NotNil(t, svc)
	assert.Equal(t, "my-database", svc.database)
	defer svc.Close()
}

func TestInfluxDBService_Close(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"), nil)
	// 关闭不应 panic
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_DoubleClose(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:8086"), nil)
	svc.Close()
	// 重复关闭应安全
	assert.NotPanics(t, func() {
		svc.Close()
	})
}

func TestInfluxDBService_PingNoConnection(t *testing.T) {
	svc := NewInfluxDBService(testConfig("http://localhost:9999"), nil)
	defer svc.Close()
	// 无真实 InfluxDB 时 Ping 应返回错误
	err := svc.Ping(t.Context())
	assert.Error(t, err)
}

// ========================================
// 错误处理/未连接/异步路径（无需真实 InfluxDB）
// ========================================

func TestInfluxDBService_WriteSensors_NotConnected(t *testing.T) {
	svc := &InfluxDBService{database: "iot"} // client nil
	err := svc.WriteSensors(context.Background(), []SensorData{{DeviceID: "d1"}})
	assert.ErrorContains(t, err, "客户端未连接")
}

func TestInfluxDBService_WriteSensors_ConnectionRefused(t *testing.T) {
	// 指向不可达地址：WriteData 快速失败（连接拒绝）
	svc := NewInfluxDBService(InfluxDBConfig{
		URL:          "http://127.0.0.1:1",
		Token:        "t",
		Database:     "iot",
		WriteTimeout: 500 * time.Millisecond,
	}, nil)

	// 零值 Timestamp/Measurement 在写失败路径前已被规范化（不 panic）
	data := []SensorData{{DeviceID: "d1", SensorName: "t", Value: 1.0}}
	err := svc.WriteSensors(context.Background(), data)
	assert.Error(t, err)
}

func TestInfluxDBService_WriteSensorsAsync_NotConnected(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}
	svc.WriteSensorsAsync([]SensorData{{DeviceID: "d1"}}) // goroutine 内报错并退出，不 panic
	time.Sleep(30 * time.Millisecond)
}

func TestInfluxDBService_Ping_NotConnected(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}
	err := svc.Ping(context.Background())
	assert.ErrorContains(t, err, "客户端未连接")
}

func TestInfluxDBService_HandleWriteError(t *testing.T) {
	svc := &InfluxDBService{database: "iot"}

	t.Run("nil 错误", func(t *testing.T) {
		assert.NoError(t, svc.handleWriteError(nil))
	})

	t.Run("普通错误透传", func(t *testing.T) {
		err := errors.New("boom")
		assert.ErrorIs(t, svc.handleWriteError(err), err)
	})

	t.Run("PartialWriteError 分类", func(t *testing.T) {
		perr := &influxdb3.PartialWriteError{
			ServerError: influxdb3.ServerError{Message: "partial"},
			LineErrors: []influxdb3.PartialWriteLineError{
				{LineNumber: 3, ErrorMessage: "bad line", OriginalLine: "x"},
			},
		}
		out := svc.handleWriteError(perr)
		assert.ErrorContains(t, out, "partial write: 1 lines failed")
	})

	t.Run("ServerError 分类", func(t *testing.T) {
		serr := &influxdb3.ServerError{StatusCode: 429, Message: "rate limited", Code: "slow_down"}
		out := svc.handleWriteError(serr)
		assert.ErrorContains(t, out, "server error [429]: rate limited")
	})
}

// TestInfluxTime 查询结果的 time 列归一化：
// 不同客户端/列类型可能给 time.Time、epoch 数字（纳秒/微秒/毫秒/秒）或字符串，
// 必须都能解析成正确时刻，否则原值会直接漏进响应（前端表现为时间列解析不出）。
func TestInfluxTime(t *testing.T) {
	want := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name   string
		in     any
		want   time.Time
		wantOK bool
	}{
		{"time.Time（原始查询分支）", want, want, true},
		{"arrow.Timestamp 命名类型（降采样分支）", arrowLikeTimestamp(want.UnixNano()), want, true},
		{"epoch 秒", want.Unix(), want, true},
		{"epoch 毫秒", want.UnixMilli(), want, true},
		{"epoch 微秒", want.UnixMicro(), want, true},
		{"epoch 纳秒", want.UnixNano(), want, true},
		{"epoch 毫秒 float64", float64(want.UnixMilli()), want, true},
		{"epoch 毫秒数字串", strconv.FormatInt(want.UnixMilli(), 10), want, true},
		{"RFC3339 字符串", "2024-01-15T10:30:00Z", want, true},
		{"无时区毫秒字符串", "2024-01-15T10:30:00.000", want, true},
		{"nil", nil, time.Time{}, false},
		{"bool", true, time.Time{}, false},
		{"垃圾字符串", "not-a-time", time.Time{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := influxTime(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.True(t, tt.want.Equal(got), "want %v, got %v", tt.want, got)
			}
		})
	}
}

// TestNormalizeSensorRow 固定历史曲线响应的时间字段形态：
// time → timestamp，且必须是 API.md §2.3 约定的「毫秒精度 + 时区偏移」。
func TestNormalizeSensorRow(t *testing.T) {
	t.Run("time.Time 归一化为带偏移的毫秒时间", func(t *testing.T) {
		row := normalizeSensorRow(map[string]any{
			"time":  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			"name":  "temp",
			"value": 25.5,
		})
		assert.Equal(t, "2024-01-15T10:30:00.000+00:00", row["timestamp"])
		_, hasTime := row["time"]
		assert.False(t, hasTime, "time 字段应被 timestamp 取代")
		assert.Equal(t, "temp", row["name"], "其它字段不应被改动")
		assert.Equal(t, 25.5, row["value"])
	})

	t.Run("epoch 毫秒数字（客户端不返回 time.Time 的情况）", func(t *testing.T) {
		row := normalizeSensorRow(map[string]any{"time": int64(1705314600000)})
		assert.Equal(t, "2024-01-15T10:30:00.000+00:00", row["timestamp"])
	})

	t.Run("线上实际返回的纳秒 int64", func(t *testing.T) {
		// 实测 /api/devices/{id}/sensorData 返回过 {"time": 1790053200000000000}（纳秒 int64），
		// 旧代码的 `.(time.Time)` 断言失败导致原值直接漏出，前端时间列空白。
		row := normalizeSensorRow(map[string]any{
			"name": "gpu_temperature", "time": int64(1790053200000000000),
			"type": "temperature", "value": 36.111,
		})
		assert.Equal(t, "2026-09-22T05:00:00.000+00:00", row["timestamp"])
		assert.Equal(t, "gpu_temperature", row["name"], "其它字段不应被改动")
		assert.Equal(t, 36.111, row["value"])
		_, hasTime := row["time"]
		assert.False(t, hasTime, "time 字段应被 timestamp 取代")
	})

	t.Run("无时区字符串按 UTC 补齐偏移", func(t *testing.T) {
		row := normalizeSensorRow(map[string]any{"time": "2024-01-15T10:30:00.000"})
		assert.Equal(t, "2024-01-15T10:30:00.000+00:00", row["timestamp"])
	})

	t.Run("无法解析时保留原始 time 字段", func(t *testing.T) {
		row := normalizeSensorRow(map[string]any{"time": "not-a-time", "name": "temp"})
		assert.Equal(t, "not-a-time", row["time"], "解析不了也不能丢时间")
		_, hasTs := row["timestamp"]
		assert.False(t, hasTs)
	})

	t.Run("无 time 字段的行不受影响", func(t *testing.T) {
		row := normalizeSensorRow(map[string]any{"name": "temp"})
		assert.Equal(t, "temp", row["name"])
	})
}

// TestNormalizeSensorRow_ZoneStable 输出时区必须固定为 UTC：
// time.Unix* 返回本地时区，不显式 UTC() 会让响应在 +00:00 / +08:00 之间漂移。
func TestNormalizeSensorRow_ZoneStable(t *testing.T) {
	instant := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	viaTimeType := normalizeSensorRow(map[string]any{"time": instant})["timestamp"]
	viaEpoch := normalizeSensorRow(map[string]any{"time": instant.UnixMilli()})["timestamp"]
	viaEpochInLocal := normalizeSensorRow(map[string]any{"time": instant.In(time.FixedZone("CST", 8*3600)).UnixMilli()})["timestamp"]
	viaString := normalizeSensorRow(map[string]any{"time": "2024-01-15T18:30:00.000+08:00"})["timestamp"]

	assert.Equal(t, "2024-01-15T10:30:00.000+00:00", viaTimeType)
	assert.Equal(t, viaTimeType, viaEpoch)
	assert.Equal(t, viaTimeType, viaEpochInLocal)
	assert.Equal(t, viaTimeType, viaString)
}

// arrowLikeTimestamp 模拟 arrow.Timestamp（`type Timestamp int64` 的命名类型）：
// 降采样查询 `date_bin(...) AS time` 实际返回的就是这个类型，类型断言匹配不到内置 int64。
type arrowLikeTimestamp int64

// TestParseDownsampleMode 降采样参数解析：默认原始点，非法值必须被拒绝（不静默降级）。
func TestParseDownsampleMode(t *testing.T) {
	tests := []struct {
		in         string
		wantMode   DownsampleMode
		wantBucket string
		wantOK     bool
	}{
		{"", DownsampleRaw, "", true},
		{"raw", DownsampleRaw, "", true},
		{"off", DownsampleRaw, "", true},
		{"0", DownsampleRaw, "", true},
		{"RAW", DownsampleRaw, "", true},
		{"auto", DownsampleAuto, "", true},
		{"1m", DownsampleMode("1m"), "1 minute", true},
		{"5m", DownsampleMode("5m"), "5 minutes", true},
		{"15m", DownsampleMode("15m"), "15 minutes", true},
		{"30m", DownsampleMode("30m"), "30 minutes", true},
		{"1h", DownsampleMode("1h"), "1 hour", true},
		{"6h", DownsampleMode("6h"), "6 hours", true},
		{"1d", DownsampleMode("1d"), "1 day", true},
		// 非法值必须报错：静默降级会让调用方误以为拿到的是原始点
		{"2h", "", "", false},
		{"1 hour", "", "", false},
		{"'; DROP TABLE x; --", "", "", false},
	}

	for _, tt := range tests {
		t.Run("downsample="+tt.in, func(t *testing.T) {
			mode, bucket, ok := ParseDownsampleMode(tt.in)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.wantMode, mode)
				assert.Equal(t, tt.wantBucket, bucket)
			}
		})
	}
}

// TestDownsampleBucket 实际桶计算：默认不聚合；auto 按跨度；显式桶优先；环境变量强制关闭。
func TestDownsampleBucket(t *testing.T) {
	t.Run("默认（raw）任何跨度都不聚合", func(t *testing.T) {
		for _, span := range []time.Duration{time.Hour, 24 * time.Hour, 72 * time.Hour, 30 * 24 * time.Hour} {
			assert.Equal(t, "", downsampleBucket(span, DownsampleRaw, ""), "span=%v", span)
		}
	})

	t.Run("auto 按跨度选桶", func(t *testing.T) {
		assert.Equal(t, "", downsampleBucket(6*time.Hour, DownsampleAuto, ""))
		assert.Equal(t, "30 minutes", downsampleBucket(24*time.Hour, DownsampleAuto, ""))
		assert.Equal(t, "1 hour", downsampleBucket(72*time.Hour, DownsampleAuto, ""))
	})

	t.Run("显式桶", func(t *testing.T) {
		assert.Equal(t, "1 minute", downsampleBucket(time.Hour, DownsampleMode("1m"), "1 minute"))
		assert.Equal(t, "1 hour", downsampleBucket(time.Hour, DownsampleMode("1h"), "1 hour"))
	})

	t.Run("HISTORY_DOWNSAMPLE=off 强制关闭", func(t *testing.T) {
		t.Setenv("HISTORY_DOWNSAMPLE", "off")
		assert.Equal(t, "", downsampleBucket(72*time.Hour, DownsampleAuto, ""))
		assert.Equal(t, "", downsampleBucket(time.Hour, DownsampleMode("1m"), "1 minute"))
	})
}
