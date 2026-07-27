package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// DeviceDataBuffer Redis 写缓冲 — 消峰填谷，支撑 1000+ 并发上报
//
// 架构：
//   - 设备上报 → LPush 到 Redis List（内存级速度）
//   - 后台 worker 定期 BRPop 批量取出 → 批量写 InfluxDB + 批量更新 PostgreSQL
//
// 这样即便 1000 并发同时打进来，HTTP/UDP/WS 线程只做一次 Redis LPush 就返回，
// 后端慢速写入（InfluxDB / PG）由 worker 异步消化。

// BufferedReport 缓冲队列中的单条上报
type BufferedReport struct {
	DeviceID  string          `json:"device_id"`
	Token     string          `json:"token"`
	Sensors   []SensorDataDTO `json:"sensors"`
	Timestamp int64           `json:"ts"` // 上报时的毫秒时间戳
}

// SensorDataDTO 传感器数据（避免循环依赖，独立定义）
type SensorDataDTO struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Value     any    `json:"value"`
	Timestamp int64  `json:"ts"` // 毫秒时间戳，零值时用 BufferedReport.Timestamp
}

// BatchWriter 批量写入接口
type BatchWriter interface {
	// FlushReports 批量写入传感器数据到 InfluxDB
	FlushReports(ctx context.Context, reports []BufferedReport) error
}

// DeviceDataBuffer 设备数据缓冲器
type DeviceDataBuffer struct {
	rdb    *redis.Client
	writer BatchWriter

	bufferKey     string        // Redis List key
	batchSize     int           // 每批最大条数
	flushInterval time.Duration // 刷新间隔

	stopCh chan struct{}
	wg     sync.WaitGroup
}

const (
	defaultBufferKey     = "buffer:device_reports"
	defaultBatchSize     = 200
	defaultFlushInterval = 200 * time.Millisecond
)

// NewDeviceDataBuffer 创建设备数据缓冲器
func NewDeviceDataBuffer(rdb *redis.Client, writer BatchWriter) *DeviceDataBuffer {
	return &DeviceDataBuffer{
		rdb:           rdb,
		writer:        writer,
		bufferKey:     defaultBufferKey,
		batchSize:     defaultBatchSize,
		flushInterval: defaultFlushInterval,
		stopCh:        make(chan struct{}),
	}
}

// Start 启动后台 worker
func (b *DeviceDataBuffer) Start() {
	b.wg.Add(1)
	go b.worker()
	zap.L().Info("[DataBuffer] 后台缓冲 worker 已启动",
		zap.Int("batchSize", b.batchSize),
		zap.Duration("flushInterval", b.flushInterval))
}

// Stop 优雅停止
func (b *DeviceDataBuffer) Stop() {
	close(b.stopCh)
	b.wg.Wait()
	zap.L().Info("[DataBuffer] 后台缓冲 worker 已停止")
}

// Enqueue 将设备上报推入 Redis 缓冲队列（快速路径，仅一次 LPush）
func (b *DeviceDataBuffer) Enqueue(ctx context.Context, report BufferedReport) error {
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return b.rdb.LPush(ctx, b.bufferKey, data).Err()
}

// QueueLen 当前队列长度
func (b *DeviceDataBuffer) QueueLen(ctx context.Context) (int64, error) {
	return b.rdb.LLen(ctx, b.bufferKey).Result()
}

// worker 后台消费者
func (b *DeviceDataBuffer) worker() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()

	ctx := context.Background()

	for {
		select {
		case <-b.stopCh:
			// 停止前最后一次排空
			b.drain(ctx)
			return
		case <-ticker.C:
			b.drain(ctx)
		}
	}
}

// drain 从 Redis 中批量取出数据并写入持久层
func (b *DeviceDataBuffer) drain(ctx context.Context) {
	for {
		// 使用 RPOP 批量取出（右侧弹出，保持 FIFO）
		count := b.batchSize
		results, err := b.rdb.RPopCount(ctx, b.bufferKey, count).Result()
		if err != nil {
			if err != redis.Nil {
				zap.L().Warn("[DataBuffer] RPOP 失败", zap.Error(err))
			}
			return
		}
		if len(results) == 0 {
			return
		}

		// 反序列化
		reports := make([]BufferedReport, 0, len(results))
		for _, raw := range results {
			var r BufferedReport
			if err := json.Unmarshal([]byte(raw), &r); err != nil {
				zap.L().Warn("[DataBuffer] 反序列化失败", zap.Error(err))
				continue
			}
			reports = append(reports, r)
		}

		if len(reports) == 0 {
			continue
		}

		// 批量写入 InfluxDB
		if err := b.writer.FlushReports(ctx, reports); err != nil {
			zap.L().Error("[DataBuffer] 批量写入 InfluxDB 失败", zap.Error(err))
			// 失败不丢数据：重新推回队列
			b.requeue(ctx, reports)
			return
		}

		zap.L().Info("[DataBuffer] 批量刷盘完成",
			zap.Int("reports", len(reports)))
	}
}

// requeue 失败重试：推回队列头部
func (b *DeviceDataBuffer) requeue(ctx context.Context, reports []BufferedReport) {
	type item struct {
		Data string
	}
	for i := len(reports) - 1; i >= 0; i-- {
		data, _ := json.Marshal(reports[i])
		b.rdb.RPush(ctx, b.bufferKey, data) // 推回右侧，下次 RPOP 先取
	}
}
