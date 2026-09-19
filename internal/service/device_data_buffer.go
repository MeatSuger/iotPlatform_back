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
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"iot-platform.local/pkg/cache"
)

// BufferedReport 缓冲队列中的单条上报
type BufferedReport struct {
	DeviceID  string          `json:"device_id"`
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

// DeviceDataBuffer Redis 写缓冲 — 消峰填谷，支撑 1000+ 并发上报
//
// 架构：
//   - 设备上报 → LPush 到 Redis List（内存级速度）
//   - 后台 worker 定期批量 RPop 取出 → 批量写 InfluxDB + 批量更新 PostgreSQL
//
// 上报入口线程只做一次 Redis LPush 就返回，后端慢速写入（InfluxDB / PG）由 worker 异步消化。
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
	defaultBufferKey     = cache.BufferDeviceReportsKey
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
		count := b.batchSize
		results, err := b.rpopCount(ctx, b.bufferKey, count)
		if err != nil {
			if !errors.Is(err, redis.Nil) {
				zap.L().Warn("[DataBuffer] RPOP 失败", zap.Error(err))
			}
			return
		}
		if len(results) == 0 {
			return
		}

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

		// 批量写入失败不丢数据：重新推回队列
		if err := b.writer.FlushReports(ctx, reports); err != nil {
			zap.L().Error("[DataBuffer] 批量写入 InfluxDB 失败", zap.Error(err))
			b.requeue(ctx, reports)
			return
		}

		zap.L().Debug("[DataBuffer] 批量刷盘完成",
			zap.Int("reports", len(reports)))
	}
}

// rpopCount 批量 RPOP，兼容 Redis < 6.2（RPopCount 不可用时循环 RPOP）
func (b *DeviceDataBuffer) rpopCount(ctx context.Context, key string, count int) ([]string, error) {
	results, err := b.rdb.RPopCount(ctx, key, count).Result()
	if err != nil {
		// 检测是否为 Redis < 6.2 不支持 RPopCount 命令
		if strings.Contains(err.Error(), "unknown command") ||
			strings.Contains(err.Error(), "ERR unknown") ||
			strings.Contains(err.Error(), "syntax error") {
			return b.rpopFallback(ctx, key, count)
		}
		return results, err
	}
	return results, nil
}

// rpopFallback Redis < 6.2 降级方案：循环 RPOP
func (b *DeviceDataBuffer) rpopFallback(ctx context.Context, key string, count int) ([]string, error) {
	var results []string
	for i := 0; i < count; i++ {
		val, err := b.rdb.RPop(ctx, key).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				break
			}
			return results, err
		}
		results = append(results, val)
	}
	if len(results) == 0 {
		return nil, redis.Nil
	}
	return results, nil
}

// requeue 失败重试：推回队列左侧（头部，给其他消息机会，避免紧循环）
// 带 LTrim 上限保护：InfluxDB 持续故障时队列不会无限增长耗尽内存
func (b *DeviceDataBuffer) requeue(ctx context.Context, reports []BufferedReport) {
	for i := len(reports) - 1; i >= 0; i-- {
		data, _ := json.Marshal(reports[i])
		b.rdb.LPush(ctx, b.bufferKey, data) // 推回左侧头部，防止立即被 RPOP 取出
	}
	// 裁剪到上限（保留头部最新数据）
	_ = b.rdb.LTrim(ctx, b.bufferKey, 0, cache.BufferQueueMaxLen-1).Err()
	// 短暂休眠，避免紧循环耗尽 CPU
	time.Sleep(50 * time.Millisecond)
}
