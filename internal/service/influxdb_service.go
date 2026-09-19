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
	"fmt"
	"os"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"go.uber.org/zap"

	"iot-platform.local/pkg/cache"
)

// InfluxDBService InfluxDB v3 时序数据服务
//
// 基于 influxdb3-go/v2 官方客户端（Flight SQL API）。与 v2 的差异：
// 无 org/bucket 概念（改用 database）、查询用 SQL、写入用 WritePoints/WriteData。
// 参考官方 examples: https://github.com/InfluxCommunity/influxdb3-go/tree/main/examples
type InfluxDBService struct {
	client   *influxdb3.Client
	database string
	cache    *cache.RedisCache // 历史查询缓存（可为 nil，自动降级直查）
}

// InfluxDBConfig 暴露 ClientConfig 的常用调优参数
type InfluxDBConfig struct {
	URL        string
	Token      string
	Database   string
	AuthScheme string

	// 连接池参数（参考 WithPreConfigHttp example）
	WriteTimeout          time.Duration // 写入 HTTP 超时，默认 10s
	QueryTimeout          time.Duration // 查询 gRPC 超时，默认无限
	IdleConnectionTimeout time.Duration // 空闲连接超时，默认 90s
	MaxIdleConnections    int           // 最大空闲连接数，默认 100
}

// NewInfluxDBService 创建 InfluxDB v3 服务
// Host / Token / Database 为必填；连接池与超时参数可经 InfluxDBConfig 调优
func NewInfluxDBService(cfg InfluxDBConfig, redisCache *cache.RedisCache) *InfluxDBService {
	config := influxdb3.ClientConfig{
		Host:       cfg.URL,
		Token:      cfg.Token,
		Database:   cfg.Database,
		AuthScheme: cfg.AuthScheme,
	}

	// 写入超时：默认 10s
	if cfg.WriteTimeout > 0 {
		config.WriteTimeout = cfg.WriteTimeout
	} else {
		config.WriteTimeout = 10 * time.Second
	}

	// 查询超时：默认 2 分钟
	if cfg.QueryTimeout > 0 {
		config.QueryTimeout = cfg.QueryTimeout
	} else {
		config.QueryTimeout = 2 * time.Minute
	}

	// 连接池调优
	if cfg.IdleConnectionTimeout > 0 {
		config.IdleConnectionTimeout = cfg.IdleConnectionTimeout
	} else {
		config.IdleConnectionTimeout = 90 * time.Second
	}
	if cfg.MaxIdleConnections > 0 {
		config.MaxIdleConnections = cfg.MaxIdleConnections
	} else {
		config.MaxIdleConnections = 50
	}

	client, err := influxdb3.New(config)
	if err != nil {
		zap.L().Error("[InfluxDB] 创建客户端失败", zap.Error(err))
		return &InfluxDBService{database: cfg.Database, cache: redisCache}
	}

	svc := &InfluxDBService{
		client:   client,
		database: cfg.Database,
		cache:    redisCache,
	}

	return svc
}

// Close 关闭 InfluxDB 连接
func (s *InfluxDBService) Close() {
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			zap.L().Warn("[InfluxDB] 关闭客户端失败", zap.Error(err))
		}
	}
}

// SensorData 传感器数据——使用 lp: 标签映射到 Line Protocol
// 参考 examples/Basic 中的 WriteData + lp struct tags
type SensorData struct {
	Measurement string    `lp:"measurement"`
	DeviceID    string    `lp:"tag,deviceID"`
	SensorName  string    `lp:"tag,sensorName"`
	Type        string    `lp:"tag,type"`
	Value       any       `lp:"field,value"`
	Timestamp   time.Time `lp:"timestamp"`
}

// WriteSensors 同步写入传感器数据（lp 结构体标签映射到 Line Protocol）
func (s *InfluxDBService) WriteSensors(ctx context.Context, data []SensorData) error {
	if s.client == nil {
		return fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	items := make([]any, len(data))
	for i, d := range data {
		if d.Timestamp.IsZero() {
			d.Timestamp = time.Now()
		}
		if d.Measurement == "" {
			d.Measurement = "device_sensors"
		}
		items[i] = d
	}

	if err := s.client.WriteData(ctx, items); err != nil {
		return fmt.Errorf("写入传感器数据失败 [count=%d]: %w", len(data), s.handleWriteError(err))
	}

	zap.S().Debugf("[InfluxDB] 写入 %d 条传感器数据", len(data))
	return nil
}

// WriteSensorsAsync 异步写入传感器数据
func (s *InfluxDBService) WriteSensorsAsync(data []SensorData) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.WriteSensors(ctx, data); err != nil {
			zap.S().Warnf("[InfluxDB] 异步写入失败: %v", err)
		}
	}()
}

// QueryRecentDeviceSensors 查询设备最近的传感器数据
//
// start / end 指定时间范围，零值时默认 end=now, start=3天前。
func (s *InfluxDBService) QueryRecentDeviceSensors(ctx context.Context, deviceID string, limit int, start, end time.Time) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}

	// 先记录是否为"默认时间窗"——缓存键需要区分默认路径与显式时间窗，
	// 下面的默认值填充会覆盖 start/end，所以必须在此之前判断。
	isDefaultRange := start.IsZero() && end.IsZero()

	// 默认时间范围：最近 3 天
	now := time.Now()
	if end.IsZero() {
		end = now
	}
	if start.IsZero() {
		start = now.Add(-72 * time.Hour)
	}

	// Redis 未就绪时降级直查，保证可用性（不因缓存故障阻塞业务）
	if s.cache == nil {
		return s.queryRecentDeviceSensors(ctx, deviceID, limit, start, end)
	}

	// Cache-Aside：L1 → L2 → singleflight 回源 → 回填
	// singleflight 保证缓存过期瞬间的并发 miss 只有一个请求真正打到 InfluxDB，
	// 避免"缓存击穿"——这正是压测中把 InfluxDB 打满的直接原因之一。
	var records []map[string]any
	err := s.cache.GetCachedSensorHistoryWithLoader(
		ctx, deviceID, limit, start, end, isDefaultRange, &records,
		func(loadCtx context.Context) (any, error) {
			return s.queryRecentDeviceSensors(loadCtx, deviceID, limit, start, end)
		})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// queryRecentDeviceSensors 直接查询 InfluxDB v3
func (s *InfluxDBService) queryRecentDeviceSensors(ctx context.Context, deviceID string, limit int, start, end time.Time) ([]map[string]any, error) {
	if s.client == nil {
		return nil, fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	// 大跨度降采样：3 天窗口的原始数据点扫描代价高（是全部接口里唯一的重查询）。
	// 先用时间桶聚合；任何失败都回退原始查询，保证功能不退化。
	//
	// 用 first_value() 而非 avg() —— 物模型含 bool/enum/string 类型，avg 只能作用于数值。
	// （已实测：InfluxDB3 DataFusion 不支持 last()，first_value() 可用）
	if bucket := downsampleBucket(end.Sub(start)); bucket != "" {
		aggQuery := fmt.Sprintf(`
			SELECT date_bin(INTERVAL '%s', time, TIMESTAMP '1970-01-01T00:00:00Z') AS time,
			       "sensorName" AS name, "type", first_value("value") AS value
			FROM "device_sensors"
			WHERE "deviceID" = $deviceID
			  AND time >= TIMESTAMP '%s'
			  AND time <= TIMESTAMP '%s'
			GROUP BY 1, "sensorName", "type"
			ORDER BY time DESC
			LIMIT %d
		`, bucket, start.Format(time.RFC3339), end.Format(time.RFC3339), limit)

		if records, err := s.scanSensorRows(ctx, aggQuery, deviceID); err == nil {
			return records, nil
		} else {
			zap.L().Warn("[InfluxDB] 降采样查询失败，回退原始查询",
				zap.String("bucket", bucket), zap.Error(err))
		}
	}

	// RFC3339 + TIMESTAMP 关键字（DataFusion 标准语法）
	// 参考 https://docs.influxdata.com/influxdb3/core/reference/sql/
	query := fmt.Sprintf(`
		SELECT "sensorName" AS name, "type", "value", time
		FROM "device_sensors"
		WHERE "deviceID" = $deviceID
		  AND time >= TIMESTAMP '%s'
		  AND time <= TIMESTAMP '%s'
		ORDER BY time DESC
		LIMIT %d
	`, start.Format(time.RFC3339), end.Format(time.RFC3339), limit)

	return s.scanSensorRows(ctx, query, deviceID)
}

// scanSensorRows 执行查询并把 time 格式化为字符串（兼容旧 API 返回格式）
func (s *InfluxDBService) scanSensorRows(ctx context.Context, query, deviceID string) ([]map[string]any, error) {
	iterator, err := s.client.QueryWithParameters(ctx, query,
		influxdb3.QueryParameters{
			"deviceID": deviceID,
		})
	if err != nil {
		return nil, fmt.Errorf("查询传感器数据失败: %w", err)
	}

	var records []map[string]any
	for iterator.Next() {
		value := iterator.Value()
		if t, ok := value["time"].(time.Time); ok {
			value["timestamp"] = t.Format("2006-01-02T15:04:05.000")
			delete(value, "time")
		}
		records = append(records, value)
	}

	if iterator.Err() != nil {
		return nil, fmt.Errorf("查询结果解析错误: %w", iterator.Err())
	}
	return records, nil
}

// downsampleBucket 按查询跨度选择降采样桶大小；返回空串表示不降采样。
//
// 阈值取 24h：小窗口（实时/当日曲线）保持原始分辨率，
// 大跨度（默认 3 天）才聚合，避免改变用户高频场景的返回语义。
func downsampleBucket(span time.Duration) string {
	// 可用 HISTORY_DOWNSAMPLE=off 关闭降采样（用于 A/B 与线上回退，无需重新编译）
	if v := os.Getenv("HISTORY_DOWNSAMPLE"); v == "off" || v == "false" || v == "0" {
		return ""
	}
	switch {
	case span >= 72*time.Hour:
		return "1 hour"
	case span >= 24*time.Hour:
		return "30 minutes"
	default:
		return ""
	}
}

// QueryDeviceSensorsByTime 按时间范围查询传感器数据
func (s *InfluxDBService) QueryDeviceSensorsByTime(ctx context.Context, deviceID, sensorName string, start, end time.Time) ([]map[string]any, error) {
	if s.client == nil {
		return nil, fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	query := fmt.Sprintf(`
		SELECT time, "value"
		FROM "device_sensors"
		WHERE "deviceID" = $deviceID
		  AND "sensorName" = $sensorName
		  AND time >= TIMESTAMP '%s'
		  AND time <= TIMESTAMP '%s'
		ORDER BY time ASC
	`, start.Format(time.RFC3339), end.Format(time.RFC3339))

	iterator, err := s.client.QueryWithParameters(ctx, query,
		influxdb3.QueryParameters{
			"deviceID":   deviceID,
			"sensorName": sensorName,
		})
	if err != nil {
		return nil, fmt.Errorf("查询传感器数据失败: %w", err)
	}

	var records []map[string]any
	for iterator.Next() {
		value := iterator.Value()
		records = append(records, value)
	}

	if iterator.Err() != nil {
		return nil, fmt.Errorf("查询结果解析错误: %w", iterator.Err())
	}

	return records, nil
}

// Ping 测试 InfluxDB 连接（HTTP /ping 端点，不依赖 gRPC Flight SQL）
func (s *InfluxDBService) Ping(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	// 使用 HTTP /ping 端点检查连通性，避免依赖 gRPC Flight SQL（HTTP/2）
	version, err := s.client.GetServerVersion()
	if err != nil {
		return fmt.Errorf("InfluxDB HTTP ping 失败: %w", err)
	}
	zap.L().Debug("[InfluxDB] 服务版本", zap.String("version", version))
	return nil
}

// handleWriteError 处理写入错误，区分 ServerError、PartialWriteError（参考 examples/HTTPErrorHandled）
func (s *InfluxDBService) handleWriteError(err error) error {
	if err == nil {
		return nil
	}

	// 检查是否为 PartialWriteError（部分写入失败，v3 write_lp endpoint）
	var partialErr *influxdb3.PartialWriteError
	if errors.As(err, &partialErr) {
		zap.L().Warn("[InfluxDB] 部分写入失败",
			zap.Int("failedLines", len(partialErr.LineErrors)),
			zap.String("message", partialErr.Message),
		)
		for _, le := range partialErr.LineErrors {
			zap.L().Warn("[InfluxDB] 失败行详情",
				zap.Int("line", le.LineNumber),
				zap.String("error", le.ErrorMessage),
				zap.String("originalLine", le.OriginalLine),
			)
		}
		return fmt.Errorf("partial write: %d lines failed", len(partialErr.LineErrors))
	}

	// 检查是否为 ServerError
	var svErr *influxdb3.ServerError
	if errors.As(err, &svErr) {
		zap.L().Error("[InfluxDB] 服务端错误",
			zap.Int("statusCode", svErr.StatusCode),
			zap.String("code", svErr.Code),
			zap.String("message", svErr.Message),
			zap.Int("retryAfter", svErr.RetryAfter),
		)
		return fmt.Errorf("server error [%d]: %s", svErr.StatusCode, svErr.Message)
	}

	return err
}
