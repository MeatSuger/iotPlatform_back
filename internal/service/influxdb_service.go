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
	"strings"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"go.uber.org/zap"

	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/common"
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

	// writeSem 异步写入的并发令牌（容量 inflightWrites）。
	// InfluxDB 变慢时由调用方阻塞形成背压，避免每次上报 spawn 一个 goroutine 把连接池和 InfluxDB 一起打满。
	writeSem chan struct{}
}

// inflightWrites 异步写入的最大并发数
const inflightWrites = 8

// InfluxDBConfig 暴露 ClientConfig 的常用调优参数
type InfluxDBConfig struct {
	URL        string
	Token      string
	Database   string
	AuthScheme string

	// 连接池参数（参考 WithPreConfigHttp example）
	WriteTimeout          time.Duration // 写入 HTTP 超时，默认 10s
	QueryTimeout          time.Duration // 查询 gRPC 超时，默认 10s
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

	// 查询超时：默认 10s
	//
	// 历史查询走后台刷新，没人等它，但也不能让一次挂死的查询占着（否则这个键
	// 整个 refresh 周期的数据都不更新）。实测正常查询 ~200ms，10s 足够宽松。
	if cfg.QueryTimeout > 0 {
		config.QueryTimeout = cfg.QueryTimeout
	} else {
		config.QueryTimeout = 10 * time.Second
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
		return &InfluxDBService{database: cfg.Database, cache: redisCache, writeSem: make(chan struct{}, inflightWrites)}
	}

	svc := &InfluxDBService{
		client:   client,
		database: cfg.Database,
		cache:    redisCache,
		writeSem: make(chan struct{}, inflightWrites),
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

	// V3 接口 + no_sync：v2 同步写要等一轮 WAL flush（生产 flush 间隔 10s ⇒ 每次写都撞 10s 超时，
	// 上报缓冲队列因此打满并丢数据）。no_sync 写入 ~2ms，代价是宕机丢 ≤1 个 flush 周期的数据。
	if err := s.client.WriteData(ctx, items,
		influxdb3.WithUseV2Api(false), influxdb3.WithNoSync(true)); err != nil {
		return fmt.Errorf("写入传感器数据失败 [count=%d]: %w", len(data), s.handleWriteError(err))
	}

	zap.S().Debugf("[InfluxDB] 写入 %d 条传感器数据", len(data))
	return nil
}

// WriteSensorsAsync 异步写入传感器数据（并发受 writeSem 限制）。
//
// 仅用于降级路径（Redis 缓冲队列也不可用时的兜底）。槽位满时直接丢弃并告警，
// 而不是阻塞调用方：本函数跑在 HTTP 上报线程上，阻塞会把整条上报链路拖成超时
// （3000 VUs 压测时就是这里雪崩的，见 docs/influxdb-load-test-findings.md）。
func (s *InfluxDBService) WriteSensorsAsync(data []SensorData) {
	write := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.WriteSensors(ctx, data); err != nil {
			zap.S().Warnf("[InfluxDB] 异步写入失败: %v", err)
		}
	}

	if s.writeSem == nil { // 未经构造函数创建（测试/降级构造）：同步写，行为一致且不堆 goroutine
		write()
		return
	}

	select {
	case s.writeSem <- struct{}{}:
		go func() {
			defer func() { <-s.writeSem }()
			write()
		}()
	default:
		zap.S().Warnf("[InfluxDB] 写入并发已满(%d)，降级丢弃 %d 条数据", inflightWrites, len(data))
	}
}

// 历史查询的时间粒度与缓存延迟契约。
//
// 查询窗口按整分钟取整、结果按分钟入键缓存：同一分钟内的请求拿到同一份结果，
// 因此不必在设备上报时去增量更新缓存（分钟一到自然换键）。
// 本分钟还未查询完时用上一分钟的旧值顶位（stale-while-revalidate），
// 所以最新上报的数据最多晚 HistoryMaxDelay 才能被查到。前端据此提示延迟。
const (
	HistoryGranularity = time.Minute
	HistoryMaxDelay    = 2 * HistoryGranularity
)

// QueryRecentDeviceSensors 查询设备最近的传感器数据。
//
// start / end 指定时间范围，零值时默认 end=now, start=3天前；两个时间点都会向下
// 取整到整分钟（HistoryGranularity），既让滚动窗口命中缓存，也把“实时性”降级成
// 可承诺的分钟粒度。
//
// mode/bucket 控制降采样：默认 DownsampleRaw（返回原始采样点，尊重设备上报频率）；
// DownsampleAuto 或显式桶才会走 date_bin 聚合（聚合会丢失采样点，必须调用方显式要求）。
func (s *InfluxDBService) QueryRecentDeviceSensors(ctx context.Context, deviceID string, limit int, start, end time.Time, mode DownsampleMode, bucket string) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}

	// 默认时间范围：最近 3 天
	start, end = historyWindow(start, end, time.Now())

	// Redis 未就绪时降级直查，保证可用性（不因缓存故障阻塞业务）
	if s.cache == nil {
		return s.queryRecentDeviceSensors(ctx, deviceID, limit, start, end, mode, bucket)
	}

	// Cache-Aside：L1 → L2 → singleflight 回源 → 回填
	// singleflight 保证缓存过期瞬间的并发 miss 只有一个请求真正打到 InfluxDB，
	// 避免"缓存击穿"——这正是压测中把 InfluxDB 打满的直接原因之一。
	// 缓存变体：降采样模式必须进 key，否则原始点请求会命中聚合缓存（反之亦然）
	variant := string(mode)
	if variant == "" {
		variant = string(DownsampleRaw)
	}

	var records []map[string]any
	err := s.cache.GetCachedSensorHistoryWithLoader(
		ctx, deviceID, limit, start, end, variant, &records,
		func(loadCtx context.Context) (any, error) {
			return s.queryRecentDeviceSensors(loadCtx, deviceID, limit, start, end, mode, bucket)
		})
	if err != nil {
		return nil, err
	}
	return records, nil
}

// historyWindow 填充默认时间窗（最近 3 天）并向下取整到整分钟。
//
// 取整是“分钟粒度”的兑现：end 取整后，当前分钟的增量不会返回，
// 因此同一分钟内的请求结果完全一致，可以直接命中缓存。
func historyWindow(start, end, now time.Time) (time.Time, time.Time) {
	if end.IsZero() {
		end = now
	}
	if start.IsZero() {
		start = now.Add(-72 * time.Hour)
	}
	return start.Truncate(HistoryGranularity), end.Truncate(HistoryGranularity)
}

// queryRecentDeviceSensors 直接查询 InfluxDB v3
func (s *InfluxDBService) queryRecentDeviceSensors(ctx context.Context, deviceID string, limit int, start, end time.Time, mode DownsampleMode, bucket string) ([]map[string]any, error) {
	if s.client == nil {
		return nil, fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	// 降采样：仅在显式要求时聚合（默认返回原始采样点，避免“时间被取整”）。
	// 大跨度聚合曾用于降低重查询代价，但聚合并不减少扫描量，
	// 实测原始查询在 6h/24h/72h/7d 跨度下均 ~200ms 且无文件数限制错误。
	// 任何失败都回退原始查询，保证功能不退化。
	//
	// 用 first_value() 而非 avg() —— 物模型含 bool/enum/string 类型，avg 只能作用于数值。
	// （已实测：InfluxDB3 DataFusion 不支持 last()，first_value() 可用）
	if aggBucket := downsampleBucket(end.Sub(start), mode, bucket); aggBucket != "" {
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
		`, aggBucket, start.Format(time.RFC3339), end.Format(time.RFC3339), limit)

		if records, err := s.scanSensorRows(ctx, aggQuery, deviceID); err == nil {
			return records, nil
		} else {
			zap.L().Warn("[InfluxDB] 降采样查询失败，回退原始查询",
				zap.String("bucket", aggBucket), zap.Error(err))
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
		records = append(records, normalizeSensorRow(iterator.Value()))
	}

	if iterator.Err() != nil {
		return nil, fmt.Errorf("查询结果解析错误: %w", iterator.Err())
	}
	return records, nil
}

// normalizeSensorRow 把查询结果的一行归一化为响应形态：time → timestamp。
//
// 时间统一输出 common.DateTimeFormatWithZone（毫秒精度 + 时区偏移，见 API.md §2.3），
// 并固定为 UTC（`+00:00`）：InfluxDB 本就以 UTC 存储，且 time.Unix* 返回的是本地时区
// ——不显式 UTC() 的话，输出会随客户端返回类型在 `+00:00` 与 `+08:00` 之间漂移。
//
// time 列形态不可控：InfluxDB 客户端/列类型不同时可能是 time.Time、epoch 数字
// （纳秒/微秒/毫秒/秒）或字符串。只认 time.Time 会让其它形态的原值直接漏到响应里
// （表现为“查询接口返回裸毫秒时间戳”，前端时间列解析不出）。
func normalizeSensorRow(value map[string]any) map[string]any {
	raw, exists := value["time"]
	if !exists {
		return value
	}
	ts, ok := influxTime(raw)
	if !ok {
		// 解析不出来时保留原 time 字段，不静默丢时间
		zap.L().Warn("[InfluxDB] 无法解析查询结果的 time 字段，原样返回", zap.Any("time", raw))
		return value
	}
	value["timestamp"] = ts.UTC().Format(common.DateTimeFormatWithZone)
	delete(value, "time")
	return value
}

// influxTime 归一化 InfluxDB 查询结果里的时间值。
// 依次尝试：兼容布局字符串 → time.Time → epoch 数值（纳秒/微秒/毫秒/秒）。
func influxTime(v any) (time.Time, bool) {
	if s, ok := v.(string); ok {
		if t, ok := common.ParseFlexibleTime(strings.TrimSpace(s)); ok {
			return t, true
		}
	}
	return common.ParseEpochValue(v)
}

// downsampleBucket 按查询跨度选择降采样桶大小；返回空串表示不降采样。
//
// 阈值取 24h：小窗口（实时/当日曲线）保持原始分辨率，
// 大跨度（默认 3 天）才聚合，避免改变用户高频场景的返回语义。
// DownsampleMode 历史查询的降采样模式。
//
// 为什么必须可显式控制：降采样会把设备的真实采样点（如每 30 秒一条）聚合成
// 每小时一条，调用方看到的是“时间被取整了”，且无法恢复。因此默认返回**原始点**，
// 需要聚合时必须显式声明。
type DownsampleMode string

const (
	// DownsampleRaw 原始采样点（默认）—— 尊重设备真实上报频率
	DownsampleRaw DownsampleMode = "raw"
	// DownsampleAuto 按查询跨度自动选桶（≥72h → 1h，≥24h → 30m）
	DownsampleAuto DownsampleMode = "auto"
)

// downsampleBuckets 允许显式指定的桶大小白名单（值直接拼进 SQL，必须白名单校验）
var downsampleBuckets = map[string]string{
	"1m":  "1 minute",
	"5m":  "5 minutes",
	"15m": "15 minutes",
	"30m": "30 minutes",
	"1h":  "1 hour",
	"6h":  "6 hours",
	"1d":  "1 day",
}

// ParseDownsampleMode 解析 downsample 查询参数，返回模式与（可选的）显式桶。
//
// 取值：空/raw/off/0 → 原始点；auto → 自动；1m/5m/15m/30m/1h/6h/1d → 指定桶。
// 无法识别返回 ok=false，由调用方报 400（不静默降级，避免调用方以为拿到原始点）。
func ParseDownsampleMode(v string) (mode DownsampleMode, bucket string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "raw", "off", "0", "false":
		return DownsampleRaw, "", true
	case "auto":
		return DownsampleAuto, "", true
	}
	if b, exists := downsampleBuckets[strings.ToLower(strings.TrimSpace(v))]; exists {
		return DownsampleMode(strings.ToLower(strings.TrimSpace(v))), b, true
	}
	return "", "", false
}

// downsampleBucket 计算实际使用的聚合桶；返回空串表示不降采样。
//
// 优先级：环境变量强制关闭 > 显式桶 > auto 按跨度选择 > 默认不降采样。
func downsampleBucket(span time.Duration, mode DownsampleMode, explicitBucket string) string {
	// 可用 HISTORY_DOWNSAMPLE=off 强制关闭降采样（运维回退，无需重新编译）
	if v := os.Getenv("HISTORY_DOWNSAMPLE"); v == "off" || v == "false" || v == "0" {
		return ""
	}
	if mode == DownsampleAuto {
		switch {
		case span >= 72*time.Hour:
			return "1 hour"
		case span >= 24*time.Hour:
			return "30 minutes"
		default:
			return ""
		}
	}
	return explicitBucket
}

// WarmupConnection 预热 InfluxDB gRPC Flight SQL 连接。
//
// 首次 HTTP/2 握手较慢（~25s），启动时先用一次最廉价的真实查询触发握手，
// 避免第一个真实请求承担该延迟。deviceID 传空串 → 不命中任何数据，只为完成握手。
func (s *InfluxDBService) WarmupConnection(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("[InfluxDB] 客户端未连接")
	}

	// 必须带窄时间窗：InfluxDB 3 Core 按文件数限制扫描，
	// 无时间谓词的查询会跨越全部 Parquet 文件而被拒绝。
	end := time.Now()
	start := end.Add(-time.Minute)
	query := fmt.Sprintf(`SELECT time, "value" FROM "device_sensors"
		WHERE "deviceID" = $deviceID
		  AND time >= TIMESTAMP '%s'
		  AND time <= TIMESTAMP '%s'
		LIMIT 1`, start.Format(time.RFC3339), end.Format(time.RFC3339))
	iterator, err := s.client.QueryWithParameters(ctx, query,
		influxdb3.QueryParameters{"deviceID": ""})
	if err != nil {
		return fmt.Errorf("预热查询失败: %w", err)
	}
	for iterator.Next() { // 丢弃结果，只需走完 Flight 查询路径
	}
	return iterator.Err()
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
	if partialErr, ok := errors.AsType[*influxdb3.PartialWriteError](err); ok {
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
	if svErr, ok := errors.AsType[*influxdb3.ServerError](err); ok {
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
