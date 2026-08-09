package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3/batching"
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
//
// 参考官方 examples: https://github.com/InfluxCommunity/influxdb3-go/tree/main/examples
type InfluxDBService struct {
	client   *influxdb3.Client
	database string

	// Redis 缓存（查询结果缓存，减少 InfluxDB 压力）
	cache *cache.RedisCache

	// batcher 用于批量攒批写入（参考 Batching example）
	batcher *batching.Batcher
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

	// 批量写入参数（参考 Batching example）
	BatchSize int // 攒批大小，默认 1000；0 表示不启用攒批
}

// NewInfluxDBService 创建InfluxDB v3服务
//
// 参考 examples/Basic 和 examples/WithPreConfigHttp：
//   - Host / Token / Database 为必填
//   - 支持通过 InfluxDBConfig 调优连接池和超时参数
func NewInfluxDBService(cfg InfluxDBConfig) *InfluxDBService {
	config := influxdb3.ClientConfig{
		Host:       cfg.URL,
		Token:      cfg.Token,
		Database:   cfg.Database,
		AuthScheme: cfg.AuthScheme,
	}

	// 连接调优：设置写入超时（参考 Basic example 的 WriteTimeout）
	if cfg.WriteTimeout > 0 {
		config.WriteTimeout = cfg.WriteTimeout
	} else {
		config.WriteTimeout = 10 * time.Second
	}

	// 查询超时（参考 Basic example 的 QueryTimeout）
	if cfg.QueryTimeout > 0 {
		config.QueryTimeout = cfg.QueryTimeout
	} else {
		config.QueryTimeout = 2 * time.Minute
	}

	// 连接池调优（参考 WithPreConfigHttp example）
	if cfg.IdleConnectionTimeout > 0 {
		config.IdleConnectionTimeout = cfg.IdleConnectionTimeout
	} else {
		config.IdleConnectionTimeout = 90 * time.Second
	}
	if cfg.MaxIdleConnections > 0 {
		config.MaxIdleConnections = cfg.MaxIdleConnections
	} else {
		config.MaxIdleConnections = 10
	}

	client, err := influxdb3.New(config)
	if err != nil {
		zap.L().Error("[InfluxDB] 创建客户端失败", zap.Error(err))
		return &InfluxDBService{database: cfg.Database}
	}

	svc := &InfluxDBService{
		client:   client,
		database: cfg.Database,
	}

	// 初始化攒批器（参考 Batching example）
	if cfg.BatchSize > 0 {
		svc.batcher = batching.NewBatcher(
			batching.WithSize(cfg.BatchSize),
			batching.WithEmitCallback(func(points []*influxdb3.Point) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := client.WritePoints(ctx, points); err != nil {
					zap.L().Warn("[InfluxDB] 批量写入失败", zap.Int("count", len(points)), zap.Error(err))
				}
			}),
		)
	}

	return svc
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

// WriteSensors 写入传感器数据（同步）
//
// 使用 WriteData + lp 结构体标签，参考 examples/Basic
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

	zap.S().Infof("[InfluxDB] 写入 %d 条传感器数据", len(data))
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

// WriteSensorsBatched 使用攒批器写入（高吞吐场景推荐）
//
// 参考 examples/Batching：攒批器达到 BatchSize 后自动触发 EmitCallback 写入
// 注意：调用方需要确保 FlushBatched() 被调用以写入末尾批次
func (s *InfluxDBService) WriteSensorsBatched(data []SensorData) {
	if s.batcher == nil {
		zap.L().Warn("[InfluxDB] 攒批器未初始化，回退到异步写入")
		s.WriteSensorsAsync(data)
		return
	}

	for _, d := range data {
		ts := d.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		p := influxdb3.NewPointWithMeasurement("device_sensors").
			SetTag("deviceID", d.DeviceID).
			SetTag("sensorName", d.SensorName).
			SetTag("type", d.Type).
			SetField("value", d.Value).
			SetTimestamp(ts)
		s.batcher.Add(p)
	}
}

// FlushBatched 将攒批器中剩余的 points 写入 InfluxDB
func (s *InfluxDBService) FlushBatched() {
	if s.batcher == nil || s.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.client.WritePoints(ctx, s.batcher.Emit()); err != nil {
		zap.L().Warn("[InfluxDB] Flush 攒批器失败", zap.Error(err))
	}
}

// QueryRecentDeviceSensors 查询设备最近的传感器数据（Redis缓存 → InfluxDB回源）
//
// start / end 指定时间范围，零值时默认 end=now, start=3天前。
// 两层缓存策略（仅默认 3 天范围生效）：
//  1. limit ≤ 10：直接从 Redis 传感器最新缓存返回（0次 InfluxDB 查询）
//  2. limit > 10：先查 Redis 查询缓存（30s TTL），miss 则查 InfluxDB 并回填
func (s *InfluxDBService) QueryRecentDeviceSensors(ctx context.Context, deviceID string, limit int, start, end time.Time) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}

	// 默认时间范围：最近 3 天
	now := time.Now()
	if end.IsZero() {
		end = now
	}
	if start.IsZero() {
		start = now.Add(-72 * time.Hour)
	}

	// 仅默认时间范围走 Redis 缓存（自定义时间范围直接查 InfluxDB）
	isDefaultRange := start.Equal(now.Add(-72*time.Hour)) && end.Equal(now)

	if isDefaultRange {
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
	}

	// 缓存 miss → 查 InfluxDB
	records, err := s.queryRecentDeviceSensors(ctx, deviceID, limit, start, end)
	if err != nil {
		return nil, err
	}

	// 回填 Redis 查询缓存（仅默认范围）
	if isDefaultRange && s.cache != nil && len(records) > 0 {
		_ = s.cache.CacheSensorQuery(ctx, deviceID, limit, records)
	}

	return records, nil
}

// queryRecentDeviceSensors 直接查询 InfluxDB v3
func (s *InfluxDBService) queryRecentDeviceSensors(ctx context.Context, deviceID string, limit int, start, end time.Time) ([]map[string]any, error) {
	if s.client == nil {
		return nil, fmt.Errorf("[InfluxDB] 客户端未连接")
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

// AggregateDeviceSensor 聚合查询设备传感器数据
// aggregateFn: MEAN, MAX, MIN, SUM, COUNT 等 SQL 聚合函数
func (s *InfluxDBService) AggregateDeviceSensor(ctx context.Context, deviceID, sensorName string, start, end time.Time, aggregateFn string) (float64, error) {
	if s.client == nil {
		return 0, fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	query := fmt.Sprintf(`
		SELECT %s("value") AS agg_value
		FROM "device_sensors"
		WHERE "deviceID" = $deviceID
		  AND "sensorName" = $sensorName
		  AND time >= TIMESTAMP '%s'
		  AND time <= TIMESTAMP '%s'
	`, aggregateFn, start.Format(time.RFC3339), end.Format(time.RFC3339))

	iterator, err := s.client.QueryWithParameters(ctx, query,
		influxdb3.QueryParameters{
			"deviceID":   deviceID,
			"sensorName": sensorName,
		})
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

// DownsampleAndWrite 降采样查询 + 写回（参考 examples/Downsampling）
//
// 使用 DATE_BIN 窗口函数进行降采样，然后通过 AsPointWithMeasurement 写回新表。
// 典型场景：将原始高频数据聚合为 5 分钟均值写入 downsampled 表。
func (s *InfluxDBService) DownsampleAndWrite(ctx context.Context, deviceID string, windowInterval string) error {
	if s.client == nil {
		return fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	// DATE_BIN 降采样查询（参考 Downsampling example）
	query := fmt.Sprintf(`
		SELECT
			DATE_BIN(INTERVAL '%s', time) AS window_start,
			"deviceID",
			"sensorName",
			AVG("value") AS avg,
			MAX("value") AS max,
			MIN("value") AS min
		FROM "device_sensors"
		WHERE
			"deviceID" = $deviceID
			AND time >= NOW() - INTERVAL '1 hour'
		GROUP BY window_start, "deviceID", "sensorName"
		ORDER BY window_start
	`, windowInterval)

	iterator, err := s.client.QueryPointValueWithParameters(ctx, query,
		influxdb3.QueryParameters{"deviceID": deviceID})
	if err != nil {
		return fmt.Errorf("降采样查询失败: %w", err)
	}

	var downsampledPoints []*influxdb3.Point
	for {
		row, err := iterator.Next()
		if err != nil {
			if err == influxdb3.Done {
				break
			}
			return fmt.Errorf("降采样迭代失败: %w", err)
		}

		// 使用 AsPointWithMeasurement 将查询结果转为 Point 并写回（参考 Downsampling example）
		p, err := row.AsPointWithMeasurement("device_sensors_downsampled")
		if err != nil {
			zap.L().Warn("[InfluxDB] 降采样 Point 转换失败", zap.Error(err))
			continue
		}
		// 移除 window_start（它不是 field/tag，只是查询分组列）
		p = p.RemoveField("window_start").RemoveTag("window_start")
		downsampledPoints = append(downsampledPoints, p)
	}

	if len(downsampledPoints) > 0 {
		if err := s.client.WritePoints(ctx, downsampledPoints); err != nil {
			return fmt.Errorf("降采样写入失败: %w", err)
		}
		zap.S().Infof("[InfluxDB] 降采样写入 %d 条 [device=%s]", len(downsampledPoints), deviceID)
	}

	return nil
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

// logWriteError 记录写入错误日志
func (s *InfluxDBService) logWriteError(msg string, count int, err error) {
	if err == nil {
		return
	}

	var partialErr *influxdb3.PartialWriteError
	var svErr *influxdb3.ServerError

	switch {
	case errors.As(err, &partialErr):
		zap.L().Warn("[InfluxDB] "+msg,
			zap.Int("totalPoints", count),
			zap.Int("failedLines", len(partialErr.LineErrors)),
		)
	case errors.As(err, &svErr):
		zap.L().Warn("[InfluxDB] "+msg,
			zap.Int("count", count),
			zap.Int("statusCode", svErr.StatusCode),
			zap.String("message", svErr.Message),
		)
	default:
		zap.L().Warn("[InfluxDB] "+msg,
			zap.Int("count", count),
			zap.Error(err),
		)
	}
}
