package service

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
)

// DeviceOfflineSyncer 设备离线检测与 PostgreSQL 同步器
//
// 设计原则：设备实时状态以 Redis Hash（cache:device_status:{deviceID}）为准，
// 上报/心跳会持续刷新其中的 lastActiveTime。设备停止上报后，该时间戳过期。
//
// 本组件周期性扫描 PostgreSQL 中 status=ONLINE 的设备：
//  1. 若 WS 长连接仍在线（isOnlineFn），跳过（防止长连接但不发心跳的设备误判）
//  2. 若 Redis 状态键缺失/过期，或 lastActiveTime 超过阈值 → 判定离线
//  3. 先将 Redis 状态置为 OFFLINE（缓存为准），再同步到 PostgreSQL（持久层）
//
// 这样覆盖了 WS 断开回调之外的离线场景（HTTP/MQTT 上报设备静默失联、
// 进程重启丢失 Hub 状态等），保证 PG 中的设备状态最终一致。
type DeviceOfflineSyncer struct {
	deviceRepo *repository.DeviceRepo
	cache      *cache.RedisCache

	interval  time.Duration // 扫描间隔
	threshold time.Duration // 离线判定阈值（lastActiveTime 超过该时长未刷新 → 离线）

	isOnlineFn func(deviceID string) bool // 可选：WS 长连接在线判定
	onOffline  func(deviceID string)      // 可选：检测到离线后的通知回调

	stopCh chan struct{}
	done   chan struct{}
	once   sync.Once
}

// NewDeviceOfflineSyncer 创建离线同步器
// interval/threshold 传入 0 时使用默认值（30s / 120s，阈值为心跳周期 60s 的 2 倍）
func NewDeviceOfflineSyncer(deviceRepo *repository.DeviceRepo, c *cache.RedisCache, interval, threshold time.Duration) *DeviceOfflineSyncer {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if threshold <= 0 {
		threshold = 120 * time.Second
	}
	return &DeviceOfflineSyncer{
		deviceRepo: deviceRepo,
		cache:      c,
		interval:   interval,
		threshold:  threshold,
		stopCh:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// SetOnlineChecker 设置 WS 长连接在线判定（可选，防止长连接设备误报离线）
func (s *DeviceOfflineSyncer) SetOnlineChecker(fn func(deviceID string) bool) {
	s.isOnlineFn = fn
}

// SetOfflineCallback 设置离线通知回调（可选，如推送 WS 消息给 owner）
func (s *DeviceOfflineSyncer) SetOfflineCallback(fn func(deviceID string)) {
	s.onOffline = fn
}

// Start 启动后台扫描循环
func (s *DeviceOfflineSyncer) Start() {
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.syncOnce()
			}
		}
	}()
	zap.L().Info("[OfflineSyncer] 设备离线检测已启动",
		zap.Duration("interval", s.interval),
		zap.Duration("threshold", s.threshold))
}

// Stop 停止后台扫描（幂等）
func (s *DeviceOfflineSyncer) Stop() {
	s.once.Do(func() {
		close(s.stopCh)
		<-s.done
	})
	zap.L().Info("[OfflineSyncer] 设备离线检测已停止")
}

// syncOnce 执行一轮离线检测与同步
func (s *DeviceOfflineSyncer) syncOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 从 PG 取出所有 ONLINE 设备（Redis 为准、PG 为待修正方）
	devices, err := s.deviceRepo.ListByStatus(ctx, "ONLINE")
	if err != nil {
		zap.L().Warn("[OfflineSyncer] 查询在线设备失败", zap.Error(err))
		return
	}
	if len(devices) == 0 {
		return
	}

	// 2. Pipeline 批量读取 Redis 状态 Hash（1 次往返，避免 N 次 HMGET）
	pipe := s.cache.Pipeline()
	cmds := make([]*redis.SliceCmd, 0, len(devices))
	deviceIDs := make([]string, 0, len(devices))
	for _, d := range devices {
		// WS 长连接仍在线的设备跳过（可能不发应用层心跳但连接健康）
		if s.isOnlineFn != nil && s.isOnlineFn(d.ID) {
			continue
		}
		deviceIDs = append(deviceIDs, d.ID)
		cmds = append(cmds, pipe.HMGet(ctx, cache.PrefixDeviceStatus+d.ID, "status", "lastActiveTime"))
	}
	if len(cmds) == 0 {
		return
	}
	if _, err := pipe.Exec(ctx); err != nil {
		zap.L().Warn("[OfflineSyncer] 批量读取Redis状态失败", zap.Error(err))
		return
	}

	// 3. 判定离线并同步
	nowMs := time.Now().UnixMilli()
	thresholdMs := s.threshold.Milliseconds()
	marked := 0

	for i, cmd := range cmds {
		deviceID := deviceIDs[i]
		vals, err := cmd.Result()
		if err != nil {
			continue // 单设备读取失败跳过，不影响其他设备
		}

		offline := false
		switch {
		case len(vals) == 0 || vals[1] == nil:
			// Redis 状态键缺失/字段不存在（TTL 过期或从未上报）→ 离线
			offline = true
		case vals[0] == "OFFLINE":
			// Redis 已标记 OFFLINE，PG 滞后 → 同步 PG
			offline = true
		default:
			lastActiveMs := parseInt64(vals[1])
			if lastActiveMs <= 0 || nowMs-lastActiveMs > thresholdMs {
				offline = true
			}
		}
		if !offline {
			continue
		}

		// 3.1 先将 Redis 置为 OFFLINE（缓存为准）
		if err := s.cache.CacheDeviceStatus(ctx, deviceID, "OFFLINE", nowMs); err != nil {
			zap.L().Warn("[OfflineSyncer] Redis置离线失败",
				zap.String("deviceID", deviceID), zap.Error(err))
		}

		// 3.2 再同步到 PostgreSQL（用 UpdateStatus，不刷新 last_active_time）
		if err := s.deviceRepo.UpdateStatus(ctx, deviceID, "OFFLINE"); err != nil {
			zap.L().Warn("[OfflineSyncer] 同步PG离线状态失败",
				zap.String("deviceID", deviceID), zap.Error(err))
			continue
		}
		marked++

		if s.onOffline != nil {
			s.onOffline(deviceID)
		}
	}

	if marked > 0 {
		zap.L().Info("[OfflineSyncer] 离线设备已同步到PostgreSQL", zap.Int("count", marked))
	}
}

// parseInt64 解析 Redis Hash 字段值（HSet 写入的 int64 读回为 string）
func parseInt64(v any) int64 {
	switch val := v.(type) {
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	case int64:
		return val
	default:
		return 0
	}
}
