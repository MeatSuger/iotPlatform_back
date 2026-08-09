package service

import (
	"context"
	"fmt"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"go.uber.org/zap"

	"iot-platform.local/pkg/cache"
)

// InfluxDBService InfluxDB v3 时序数据服务
//
// 使用 InfluxDB v3 官方 Go 客户端（influxdb3-go/v2），基于 Flight SQL API。
// 与 v2 的主要差异：
//   - 无 org/bucket 概念，改用 database
//   - 查询语言从 Flux 切换到 SQL（或 InfluxQL）
//   - 写入 API 使用 WritePoints / Write / WriteData
//   - 查询返回 Arrow 列式迭代器
type InfluxDBService struct {
	client   *influxdb3.Client
	database string

	// Redis 缓存（查询结果缓存，减少 InfluxDB 压力）
	cache *cache.RedisCache
}

// NewInfluxDBService 创建InfluxDB v3服务
func NewInfluxDBService(url, token, database, authScheme string) *InfluxDBService {
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:       url,
		Token:      token,
		Database:   database,
		AuthScheme: authScheme,
	})
	if err != nil {
		zap.L().Error("[InfluxDB] 创建客户端失败", zap.Error(err))
		return &InfluxDBService{database: database}
	}
	return &InfluxDBService{
		client:   client,
		database: database,
	}
}

// SetCache 注入 Redis 缓存（由 Wire 初始化后调用）
func (s *InfluxDBService) SetCache(c *cache.RedisCache) {
	s.cache = c
}

// IsConnected 检查客户端是否已成功连接
func (s *InfluxDBService) IsConnected() bool {
	return s.client != nil
}

// Close 关闭InfluxDB连接
func (s *InfluxDBService) Close() {
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			zap.L().Warn("[InfluxDB] 关闭客户端失败", zap.Error(err))
		}
	}
}

// SensorPoint 传感器数据点
type SensorPoint struct {
	DeviceID   string
	SensorName string
	Type       string
	Value      any
	Timestamp  time.Time
}

// WriteDeviceSensors 同步写入设备传感器数据
func (s *InfluxDBService) WriteDeviceSensors(ctx context.Context, deviceID string, sensors []SensorPoint) error {
	if s.client == nil {
		return fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	points := make([]*influxdb3.Point, 0, len(sensors))
	for _, sensor := range sensors {
		ts := sensor.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		p := influxdb3.NewPoint(
			"device_sensors",
			map[string]string{
				"deviceID":   deviceID,
				"sensorName": sensor.SensorName,
				"type":       sensor.Type,
			},
			map[string]any{
				"value": sensor.Value,
			},
			ts,
		)
		points = append(points, p)
	}

	if err := s.client.WritePoints(ctx, points); err != nil {
		return fmt.Errorf("写入传感器数据失败 [device=%s, count=%d]: %w", deviceID, len(sensors), err)
	}

	zap.S().Infof("[InfluxDB] 同步写入 %d 条传感器数据 [device=%s]", len(sensors), deviceID)
	return nil
}

// WriteDeviceSensorsAsync 异步写入设备传感器数据
func (s *InfluxDBService) WriteDeviceSensorsAsync(deviceID string, sensors []SensorPoint) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.WriteDeviceSensors(ctx, deviceID, sensors); err != nil {
			zap.S().Warnf("[InfluxDB] 异步写入失败 [device=%s]: %v", deviceID, err)
		}
	}()
}

// QueryRecentDeviceSensors 查询设备最近的传感器数据（Redis缓存 → InfluxDB回源）
//
// 两层缓存策略：
//  1. limit ≤ 10：直接从 Redis 传感器最新缓存返回（0次 InfluxDB 查询）
//  2. limit > 10：先查 Redis 查询缓存（30s TTL），miss 则查 InfluxDB 并回填
func (s *InfluxDBService) QueryRecentDeviceSensors(ctx context.Context, deviceID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}

	// 小 limit：直接从传感器最新缓存返回（上报时已写入，0 次 InfluxDB）
	if limit <= 10 && s.cache != nil {
		var sensors []map[string]any
		if err := s.cache.GetCachedSensorRecent(ctx, deviceID, &sensors); err == nil && len(sensors) > 0 {
			if len(sensors) > limit {
				sensors = sensors[:limit]
			}
			return sensors, nil
		}
	}

	// 大 limit：先查 Redis 查询缓存（Cache-Aside）
	if s.cache != nil {
		var cached []map[string]any
		if err := s.cache.GetCachedSensorQuery(ctx, deviceID, limit, &cached); err == nil && len(cached) > 0 {
			return cached, nil
		}
	}

	// 缓存 miss → 查 InfluxDB
	records, err := s.queryRecentDeviceSensors(ctx, deviceID, limit)
	if err != nil {
		return nil, err
	}

	// 回填 Redis 查询缓存
	if s.cache != nil && len(records) > 0 {
		_ = s.cache.CacheSensorQuery(ctx, deviceID, limit, records)
	}

	return records, nil
}

// queryRecentDeviceSensors 直接查询 InfluxDB v3（SQL）
func (s *InfluxDBService) queryRecentDeviceSensors(ctx context.Context, deviceID string, limit int) ([]map[string]any, error) {
	if s.client == nil {
		return nil, fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	query := fmt.Sprintf(`
		SELECT "sensorName" AS name, "type", "value", time
		FROM "device_sensors"
		WHERE "deviceID" = '%s'
		  AND time >= NOW() - INTERVAL '7 days'
		ORDER BY time DESC
		LIMIT %d
	`, deviceID, limit)

	iterator, err := s.client.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询传感器数据失败: %w", err)
	}

	var records []map[string]any
	for iterator.Next() {
		value := iterator.Value()
		// 将 time 格式化为字符串（兼容旧 API 返回格式）
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

// QueryDeviceSensorsByTime 按时间范围查询传感器数据
func (s *InfluxDBService) QueryDeviceSensorsByTime(ctx context.Context, deviceID, sensorName string, start, end time.Time) ([]map[string]any, error) {
	if s.client == nil {
		return nil, fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	query := fmt.Sprintf(`
		SELECT time, "value"
		FROM "device_sensors"
		WHERE "deviceID" = '%s'
		  AND "sensorName" = '%s'
		  AND time >= '%s'::TIMESTAMP
		  AND time <= '%s'::TIMESTAMP
		ORDER BY time ASC
	`, deviceID, sensorName, start.Format(time.RFC3339), end.Format(time.RFC3339))

	iterator, err := s.client.Query(ctx, query)
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

// AggregateDeviceSensor 聚合查询设备传感器数据
// aggregateFn: MEAN, MAX, MIN, SUM, COUNT 等 SQL 聚合函数
func (s *InfluxDBService) AggregateDeviceSensor(ctx context.Context, deviceID, sensorName string, start, end time.Time, aggregateFn string) (float64, error) {
	if s.client == nil {
		return 0, fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	query := fmt.Sprintf(`
		SELECT %s("value") AS agg_value
		FROM "device_sensors"
		WHERE "deviceID" = '%s'
		  AND "sensorName" = '%s'
		  AND time >= '%s'::TIMESTAMP
		  AND time <= '%s'::TIMESTAMP
	`, aggregateFn, deviceID, sensorName, start.Format(time.RFC3339), end.Format(time.RFC3339))

	iterator, err := s.client.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("聚合查询失败: %w", err)
	}

	for iterator.Next() {
		value := iterator.Value()
		if val, ok := value["agg_value"].(float64); ok {
			return val, nil
		}
		// int64 → float64 转换
		if val, ok := value["agg_value"].(int64); ok {
			return float64(val), nil
		}
	}

	if iterator.Err() != nil {
		return 0, fmt.Errorf("聚合查询结果解析错误: %w", iterator.Err())
	}

	return 0, nil
}

// Ping 测试InfluxDB连接（HTTP /ping 端点，不依赖 gRPC Flight SQL）
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

// WriteDeviceSensorsBatch 批量异步写入传感器数据（高并发场景推荐）
// 内部使用 WritePoints 批量写入，InfluxDB v3 自动攒批
// deviceID 为空时使用每个 point 自带的 DeviceID（跨设备批量场景）
func (s *InfluxDBService) WriteDeviceSensorsBatch(deviceID string, sensors []SensorPoint) {
	if s.client == nil {
		zap.L().Warn("[InfluxDB] 客户端未连接，跳过批量写入")
		return
	}

	points := make([]*influxdb3.Point, 0, len(sensors))
	for _, sensor := range sensors {
		did := deviceID
		if did == "" {
			did = sensor.DeviceID
		}
		ts := sensor.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		p := influxdb3.NewPoint(
			"device_sensors",
			map[string]string{
				"deviceID":   did,
				"sensorName": sensor.SensorName,
				"type":       sensor.Type,
			},
			map[string]any{
				"value": sensor.Value,
			},
			ts,
		)
		points = append(points, p)
	}

	// 异步批量写入
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.client.WritePoints(ctx, points); err != nil {
			zap.L().Warn("[InfluxDB] 批量异步写入失败",
				zap.Int("count", len(points)), zap.Error(err))
		}
	}()
}
