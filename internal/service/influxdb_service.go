package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"iot-platform.local/pkg/cache"
)

// InfluxDBService InfluxDB时序数据服务
type InfluxDBService struct {
	client influxdb2.Client
	org    string
	bucket string

	// 异步批量写入
	writeAPI     api.WriteAPI
	writeAPILock sync.Mutex
	writeAPIOnce sync.Once

	// Redis 缓存（查询结果缓存，减少 InfluxDB 压力）
	cache *cache.RedisCache
}

// NewInfluxDBService 创建InfluxDB服务
func NewInfluxDBService(url, token, org, bucket string) *InfluxDBService {
	client := influxdb2.NewClient(url, token)
	return &InfluxDBService{
		client: client,
		org:    org,
		bucket: bucket,
	}
}

// SetCache 注入 Redis 缓存（由 Wire 初始化后调用）
func (s *InfluxDBService) SetCache(c *cache.RedisCache) {
	s.cache = c
}

// Close 关闭InfluxDB连接
func (s *InfluxDBService) Close() {
	s.writeAPILock.Lock()
	if s.writeAPI != nil {
		s.writeAPI.Flush()
	}
	s.writeAPILock.Unlock()
	s.client.Close()
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
	writeAPI := s.client.WriteAPIBlocking(s.org, s.bucket)

	for _, sensor := range sensors {
		p := influxdb2.NewPointWithMeasurement("device_sensors").
			AddTag("deviceID", deviceID).
			AddTag("sensorName", sensor.SensorName).
			AddTag("type", sensor.Type).
			AddField("value", sensor.Value).
			SetTime(sensor.Timestamp)

		if err := writeAPI.WritePoint(ctx, p); err != nil {
			return fmt.Errorf("写入传感器数据失败 [device=%s, sensor=%s]: %w", deviceID, sensor.SensorName, err)
		}
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
			zap.S().Infof("[InfluxDB] 异步写入失败 [device=%s]: %v", deviceID, err)
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
	records, err := s.queryInfluxDB(ctx, deviceID, limit)
	if err != nil {
		return nil, err
	}

	// 回填 Redis 查询缓存
	if s.cache != nil && len(records) > 0 {
		_ = s.cache.CacheSensorQuery(ctx, deviceID, limit, records)
	}

	return records, nil
}

// queryInfluxDB 直接查询 InfluxDB（原始查询逻辑）
func (s *InfluxDBService) queryInfluxDB(ctx context.Context, deviceID string, limit int) ([]map[string]any, error) {
	queryAPI := s.client.QueryAPI(s.org)
	flux := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -7d)
			|> filter(fn: (r) => r["_measurement"] == "device_sensors")
			|> filter(fn: (r) => r["deviceID"] == "%s")
			|> filter(fn: (r) => r["_field"] == "value")
			|> sort(columns: ["_time"], desc: true)
			|> limit(n: %d)
	`, s.bucket, deviceID, limit)

	result, err := queryAPI.Query(ctx, flux)
	if err != nil {
		return nil, fmt.Errorf("查询传感器数据失败: %w", err)
	}

	var records []map[string]any
	for result.Next() {
		record := result.Record()
		row := map[string]any{
			"name":      record.ValueByKey("sensorName"),
			"type":      record.ValueByKey("type"),
			"value":     record.Value(),
			"timestamp": record.Time().Format("2006-01-02T15:04:05.000"),
		}
		records = append(records, row)
	}

	if result.Err() != nil {
		return nil, fmt.Errorf("查询结果解析错误: %w", result.Err())
	}

	return records, nil
}

// QueryDeviceSensorsByTime 按时间范围查询传感器数据
func (s *InfluxDBService) QueryDeviceSensorsByTime(ctx context.Context, deviceID, sensorName string, start, end time.Time) ([]map[string]any, error) {
	queryAPI := s.client.QueryAPI(s.org)
	flux := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: %s, stop: %s)
			|> filter(fn: (r) => r["_measurement"] == "device_sensors")
			|> filter(fn: (r) => r["deviceID"] == "%s")
			|> filter(fn: (r) => r["sensorName"] == "%s")
			|> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
	`, s.bucket, start.Format(time.RFC3339), end.Format(time.RFC3339), deviceID, sensorName)

	result, err := queryAPI.Query(ctx, flux)
	if err != nil {
		return nil, fmt.Errorf("查询传感器数据失败: %w", err)
	}

	var records []map[string]any
	for result.Next() {
		record := result.Record()
		row := map[string]any{
			"time":  record.Time(),
			"value": record.Value(),
		}
		records = append(records, row)
	}

	if result.Err() != nil {
		return nil, fmt.Errorf("查询结果解析错误: %w", result.Err())
	}

	return records, nil
}

// AggregateDeviceSensor 聚合查询设备传感器数据
func (s *InfluxDBService) AggregateDeviceSensor(ctx context.Context, deviceID, sensorName string, start, end time.Time, aggregateFn string) (float64, error) {
	queryAPI := s.client.QueryAPI(s.org)

	// aggregateFn: mean, max, min, sum, count 等
	flux := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: %s, stop: %s)
			|> filter(fn: (r) => r["_measurement"] == "device_sensors")
			|> filter(fn: (r) => r["deviceID"] == "%s")
			|> filter(fn: (r) => r["sensorName"] == "%s")
			|> filter(fn: (r) => r["_field"] == "value")
			|> %s()
	`, s.bucket, start.Format(time.RFC3339), end.Format(time.RFC3339), deviceID, sensorName, aggregateFn)

	result, err := queryAPI.Query(ctx, flux)
	if err != nil {
		return 0, fmt.Errorf("聚合查询失败: %w", err)
	}

	for result.Next() {
		val, ok := result.Record().Value().(float64)
		if ok {
			return val, nil
		}
	}

	if result.Err() != nil {
		return 0, fmt.Errorf("聚合查询结果解析错误: %w", result.Err())
	}

	return 0, nil
}

// Ping 测试InfluxDB连接
func (s *InfluxDBService) Ping(ctx context.Context) error {
	health, err := s.client.Health(ctx)
	if err != nil {
		return fmt.Errorf("InfluxDB健康检查失败: %w", err)
	}
	if health.Status != "pass" {
		return fmt.Errorf("InfluxDB状态异常: %s", health.Status)
	}
	return nil
}

// WritePoint 写入单个数据点（通用方法）
func (s *InfluxDBService) WritePoint(ctx context.Context, point *write.Point) error {
	writeAPI := s.client.WriteAPIBlocking(s.org, s.bucket)
	return writeAPI.WritePoint(ctx, point)
}

// getWriteAPI 获取或创建异步 WriteAPI（线程安全，懒初始化）
// 异步 WriteAPI 内部有批量缓冲，自动攒批写入，远比逐条同步写入高效
func (s *InfluxDBService) getWriteAPI() api.WriteAPI {
	s.writeAPIOnce.Do(func() {
		// 使用异步 WriteAPI：每 5000 条或每 1 秒自动 flush
		s.writeAPI = s.client.WriteAPI(s.org, s.bucket)
		// 监听异步错误
		go func() {
			for err := range s.writeAPI.Errors() {
				zap.L().Warn("[InfluxDB] 异步写入错误", zap.Error(err))
			}
		}()
	})
	return s.writeAPI
}

// WriteDeviceSensorsBatch 批量异步写入传感器数据（高并发场景推荐）
// 内部使用 InfluxDB 异步 WriteAPI，自动攒批
// deviceID 为空时使用每个 point 自带的 DeviceID（跨设备批量场景）
func (s *InfluxDBService) WriteDeviceSensorsBatch(deviceID string, sensors []SensorPoint) {
	_api := s.getWriteAPI()
	for _, sensor := range sensors {
		did := deviceID
		if did == "" {
			did = sensor.DeviceID
		}
		p := influxdb2.NewPointWithMeasurement("device_sensors").
			AddTag("deviceID", did).
			AddTag("sensorName", sensor.SensorName).
			AddTag("type", sensor.Type).
			AddField("value", sensor.Value).
			SetTime(sensor.Timestamp)
		_api.WritePoint(p)
	}
}
