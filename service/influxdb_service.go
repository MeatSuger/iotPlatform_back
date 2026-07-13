package service

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

// InfluxDBService InfluxDB时序数据服务
type InfluxDBService struct {
	client influxdb2.Client
	org    string
	bucket string
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

// Close 关闭InfluxDB连接
func (s *InfluxDBService) Close() {
	s.client.Close()
}

// SensorPoint 传感器数据点
type SensorPoint struct {
	DeviceID   string
	SensorName string
	Type       string
	Value      interface{}
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

// QueryRecentDeviceSensors 查询设备最近的传感器数据（默认近 7 天）
func (s *InfluxDBService) QueryRecentDeviceSensors(ctx context.Context, deviceID string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 50
	}

	queryAPI := s.client.QueryAPI(s.org)
	// 不使用 pivot，直接从 _field=value 的行中读取 _value 和标签
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

	var records []map[string]interface{}
	for result.Next() {
		record := result.Record()
		// 字段名对齐 API 文档：name, type, value, timestamp
		row := map[string]interface{}{
			"name":      record.ValueByKey("sensorName"),
			"type":      record.ValueByKey("type"),
			"value":     record.Value(),
			"timestamp": record.Time().Format("2006-01-02 15:04:05"),
		}
		records = append(records, row)
	}

	if result.Err() != nil {
		return nil, fmt.Errorf("查询结果解析错误: %w", result.Err())
	}

	return records, nil
}

// QueryDeviceSensorsByTime 按时间范围查询传感器数据
func (s *InfluxDBService) QueryDeviceSensorsByTime(ctx context.Context, deviceID, sensorName string, start, end time.Time) ([]map[string]interface{}, error) {
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

	var records []map[string]interface{}
	for result.Next() {
		record := result.Record()
		row := map[string]interface{}{
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
