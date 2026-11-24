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
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/common"
	"iot-platform.local/pkg/util"
)

// DeviceReportService 设备数据上报服务
// 实现 BatchWriter 接口，支持 Redis 缓冲 + 批量刷盘
type DeviceReportService struct {
	deviceRepo *repository.DeviceRepo
	influxSvc  *InfluxDBService
	cache      *cache.RedisCache
	deviceSvc  *DeviceService
	sensorSvc  *DeviceSensorService // 上报值类型校验用（可空：nil = 跳过校验）
	buffer     *DeviceDataBuffer    // Redis 写缓冲（可选）

	// L1 本地内存缓存：消除热点设备的 Redis 往返延迟
	localCache *cache.LocalCache

	// PostgreSQL 批量更新（debounced，避免每个请求 spawn goroutine）
	pgUpdateMu       sync.Mutex
	pgUpdatePending  map[string]time.Time // deviceID → 最新活跃时间
	pgUpdateTimer    *time.Timer
	pgFlushing       bool                                        // 是否有 flush 正在执行（防止并发 flush 叠加）
	updateLastActive func(context.Context, string, string) error // 可注入，生产指向 deviceRepo.UpdateLastActive

	// 每设备的「id/名称 → 物模型定义」映射（进程内缓存，上报校验用）
	sensorDefMu    sync.Mutex
	sensorDefCache map[string]sensorDefEntry
}

type sensorDefEntry struct {
	byName    map[string]entity.Sensor
	expiresAt time.Time
}

// sensorDefCacheTTL 与物模型定义的 L1 TTL 对齐：定义变更最多滞后这么久生效
const sensorDefCacheTTL = cache.TTLDefsLocal

// maxSensorDefCacheEntries 映射缓存条数上限（每设备一条，超出先清过期再整体丢弃）
const maxSensorDefCacheEntries = 1024

const (
	pgDebounceInterval = 5 * time.Second  // PostgreSQL 活跃时间更新防抖间隔
	pgFlushTimeout     = 30 * time.Second // 单次批量更新总超时（PG 不可用时不无限挂住）

	// degradePushTimeout 降级写回队列的超时：Redis 已不健康时不能让请求跟着等满客户端超时
	degradePushTimeout = 200 * time.Millisecond
)

func NewDeviceReportService(
	deviceRepo *repository.DeviceRepo,
	influxSvc *InfluxDBService,
	redisCache *cache.RedisCache,
	deviceSvc *DeviceService,
) *DeviceReportService {
	svc := &DeviceReportService{
		deviceRepo:      deviceRepo,
		influxSvc:       influxSvc,
		cache:           redisCache,
		deviceSvc:       deviceSvc,
		localCache:      cache.NewLocalCache(5 * time.Second), // L1 5s TTL，覆盖高频上报
		pgUpdatePending: make(map[string]time.Time),
		sensorDefCache:  make(map[string]sensorDefEntry),
	}
	if deviceRepo != nil {
		svc.updateLastActive = deviceRepo.UpdateLastActive
	}
	return svc
}

// SetBuffer 注入缓冲器（由 main 初始化后调用）
func (s *DeviceReportService) SetBuffer(buf *DeviceDataBuffer) {
	s.buffer = buf
}

// SetSensorSvc 注入传感器定义服务（由 main 初始化后调用，nil 时跳过上报值类型校验）
func (s *DeviceReportService) SetSensorSvc(svc *DeviceSensorService) {
	s.sensorSvc = svc
}

// validateSensorValues 按物模型定义校验上报值类型与 dataType 对齐（JSON 格式统一）。
//
// 定义列表走缓存（L1/L2），name→def 映射另做进程内缓存：上报热路径每条都要用它，
// 逐条 unmarshal 全量定义 + 重建 map 曾占上报处理的约 23%（pprof 实测）。
// 定义缺失/加载失败时放行（fail-open，保持上报链路可用）。非法数据点逐条丢弃并告警。
func (s *DeviceReportService) validateSensorValues(ctx context.Context, deviceID string, dto *entity.DeviceStatusDTO) {
	if s.sensorSvc == nil || len(dto.Sensors) == 0 {
		return
	}
	byName, err := s.sensorDefMap(ctx, deviceID)
	if err != nil {
		zap.L().Warn("[DeviceReport] 物模型定义加载失败，跳过值校验",
			zap.String("deviceID", deviceID), zap.Error(err))
		return
	}
	if len(byName) == 0 {
		return
	}

	valid := dto.Sensors[:0]
	dropped := 0
	for _, sd := range dto.Sensors {
		if def, ok := byName[sd.Name]; ok {
			if err := entity.ValidateSensorValue(def.DataType, sd.Value, def.Specs); err != nil {
				zap.L().Warn("[DeviceReport] 丢弃类型错乱的上报数据点",
					zap.String("deviceID", deviceID),
					zap.String("sensor", sd.Name),
					zap.String("dataType", def.DataType),
					zap.Any("value", sd.Value),
					zap.Error(err))
				dropped++
				continue
			}
		}
		valid = append(valid, sd)
	}
	dto.Sensors = valid
	if dropped > 0 {
		zap.L().Warn("[DeviceReport] 上报值校验",
			zap.String("deviceID", deviceID),
			zap.Int("dropped", dropped),
			zap.Int("kept", len(valid)))
	}
}

// sensorDefMap 取该设备的「id/名称 → 物模型定义」映射（进程内缓存，TTL 同物模型 L1）。
// 返回的 map 只读，调用方不得修改。
func (s *DeviceReportService) sensorDefMap(ctx context.Context, deviceID string) (map[string]entity.Sensor, error) {
	now := time.Now()
	s.sensorDefMu.Lock()
	if e, ok := s.sensorDefCache[deviceID]; ok && now.Before(e.expiresAt) {
		s.sensorDefMu.Unlock()
		return e.byName, nil
	}
	s.sensorDefMu.Unlock()

	defs, err := s.sensorSvc.List(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]entity.Sensor, len(defs)*2)
	for _, def := range defs {
		byName[def.ID] = def
		byName[def.Name] = def // 上报 name 兼容 id / 名称两种写法
	}

	s.sensorDefMu.Lock()
	if len(s.sensorDefCache) >= maxSensorDefCacheEntries {
		s.pruneSensorDefCacheLocked(now)
	}
	s.sensorDefCache[deviceID] = sensorDefEntry{byName: byName, expiresAt: now.Add(sensorDefCacheTTL)}
	s.sensorDefMu.Unlock()
	return byName, nil
}

// pruneSensorDefCacheLocked 先删过期项，仍然满则整体丢弃（极端设备数下宁可直接重建）；调用方持锁
func (s *DeviceReportService) pruneSensorDefCacheLocked(now time.Time) {
	for k, e := range s.sensorDefCache {
		if now.After(e.expiresAt) {
			delete(s.sensorDefCache, k)
		}
	}
	if len(s.sensorDefCache) >= maxSensorDefCacheEntries {
		s.sensorDefCache = make(map[string]sensorDefEntry)
	}
}

// schedulePGUpdate 防抖批量更新 PostgreSQL 设备活跃时间（合并 5s 窗口内多次上报为一次批量更新）
func (s *DeviceReportService) schedulePGUpdate(deviceID string) {
	s.pgUpdateMu.Lock()
	defer s.pgUpdateMu.Unlock()

	s.pgUpdatePending[deviceID] = time.Now()

	// 已有 flush 在执行时不再排新定时器：否则 PG 变慢期间每次上报都会叠加一个
	// 并发 flush，goroutine 与连接等待无上限增长。flush 结束时统一补排。
	if s.pgUpdateTimer == nil && !s.pgFlushing {
		s.pgUpdateTimer = time.AfterFunc(pgDebounceInterval, s.flushPGUpdates)
	}
}

// flushPGUpdates 批量执行 PostgreSQL 活跃时间更新。
// 同一时刻只允许一个 flush；带总超时，PG 故障时不会无限挂住。
func (s *DeviceReportService) flushPGUpdates() {
	s.pgUpdateMu.Lock()
	if s.pgFlushing {
		s.pgUpdateMu.Unlock()
		return
	}
	pending := s.pgUpdatePending
	s.pgUpdatePending = make(map[string]time.Time)
	s.pgUpdateTimer = nil
	s.pgFlushing = true
	s.pgUpdateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), pgFlushTimeout)
	defer cancel()

	update := s.updateLastActive
	if update != nil {
		for deviceID := range pending {
			if err := update(ctx, deviceID, "ONLINE"); err != nil {
				zap.S().Warnf("[DeviceReport] 批量更新活跃时间失败 [device=%s]: %v", deviceID, err)
			}
		}
	}

	s.pgUpdateMu.Lock()
	s.pgFlushing = false
	// flush 期间新到的上报攒在新 pending 里：补排下一轮，不能丢。
	if s.pgUpdateTimer == nil && len(s.pgUpdatePending) > 0 {
		s.pgUpdateTimer = time.AfterFunc(pgDebounceInterval, s.flushPGUpdates)
	}
	s.pgUpdateMu.Unlock()
}

// ReportStatus 设备上报传感器数据（带 Token 校验，供非 HTTP 入口调用）
func (s *DeviceReportService) ReportStatus(ctx context.Context, deviceID, token string, dto entity.DeviceStatusDTO) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// Token 校验（仅非 HTTP 入口需要，HTTP 入口由中间件完成）。
	// 空 Token 一律拒绝：UDP 等入口若放行空 Token，任何人都能用设备 ID 写入数据。
	if token == "" {
		return ErrDeviceTokenNotFound
	}
	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	return s.reportStatusFast(ctx, deviceID, dto)
}

// ReportStatusFast 设备上报传感器数据（跳过 Token 校验，HTTP 中间件已验证）
// 优化后的快速路径（目标 <5ms）：
//  1. L1 本地缓存检查设备存在性（0 次 Redis 读）
//  2. Pipeline 合并：更新设备状态 Hash + 缓存传感器数据 + 推入缓冲队列（1 次 Redis 往返）
//
// 慢速路径（后台 worker 异步）：
//   - 批量写入 InfluxDB
//   - 批量更新 PostgreSQL 设备活跃时间
func (s *DeviceReportService) ReportStatusFast(ctx context.Context, deviceID string, dto entity.DeviceStatusDTO) error {
	return s.reportStatusFast(ctx, util.NormalizeDeviceID(deviceID), dto)
}

// reportStatusFast 核心快速路径实现
func (s *DeviceReportService) reportStatusFast(ctx context.Context, deviceID string, dto entity.DeviceStatusDTO) error {
	now := time.Now()
	nowMs := now.UnixMilli()

	// 0. 上报值类型校验（与物模型 dataType 对齐；丢弃类型错乱的数据点）
	s.validateSensorValues(ctx, deviceID, &dto)

	// 1. L1 本地缓存：快速确认设备存在（避免 Redis GET + JSON Unmarshal）
	localKey := "dev:" + deviceID
	if _, ok := s.localCache.Get(localKey); !ok {
		// 缓存 miss：经 GetByDeviceID 全链路加载（L1/L2 → DB）
		device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
		if err != nil {
			return err
		}
		// 缓存到 L1（仅存轻量标记）
		s.localCache.Set(localKey, device.Status)
	}

	// 2. 准备缓冲数据（提前序列化，避免在 Pipeline 中序列化）
	sensorDTOs := make([]SensorDataDTO, len(dto.Sensors))
	for i, sensor := range dto.Sensors {
		sensorDTOs[i] = SensorDataDTO{
			Name:      sensor.Name,
			Type:      sensor.Type,
			Value:     sensor.Value,
			Timestamp: sensor.Timestamp.UnixMilli(),
		}
	}

	bufferData, _ := json.Marshal(BufferedReport{
		DeviceID:  deviceID,
		Sensors:   sensorDTOs,
		Timestamp: nowMs,
	})

	// 传感器最新值的 JSON：在调用方序列化一次，供 FastReportWrite 直接使用。
	// 必须是 entity 形态（含 time.Time），与回退路径的 CacheSensorRecent 保持一致，
	// 否则设备详情接口读回缓存时字段格式会变化。
	sensorsJSON, _ := json.Marshal(dto.Sensors)

	// 3. 一次 Pipeline 完成所有 Redis 写操作（1 次往返！）
	if s.buffer != nil {
		if err := s.cache.FastReportWrite(ctx, deviceID, "ONLINE", nowMs, sensorsJSON, bufferData); err != nil {
			// 客户端断开导致的 ctx 取消失败：直接放弃。降级路径反而会多打三次 Redis，
			// 而结果没人读（请求方已经走了）；设备侧会重发或下一拍补上。
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			zap.S().Warnf("[DeviceReport] 快速写入失败 [device=%s]: %v", deviceID, err)
			// 降级：把原始 payload 写回缓冲队列，交给后台 worker 落库。
			// 不能在请求线程里同步写 InfluxDB：8 个写入槽位满时调用方被阻塞，单次写入
			// 最长 10s，3000 VUs 压测的超时雪崩就是这条路径（见 docs/influxdb-load-test-findings.md）。
			// 状态/传感器缓存由下一次成功上报补齐，此处不再额外打 Redis。
			pushCtx, cancel := context.WithTimeout(ctx, degradePushTimeout)
			pushErr := s.buffer.Push(pushCtx, bufferData)
			cancel()
			if pushErr != nil {
				// 队列也写不进（Redis 整体不可用）：退到进程内异步写，槽位满则丢弃，同样不阻塞请求。
				s.writeSensorsSync(deviceID, dto)
			}
		}
	} else {
		// 无缓冲器：逐条更新状态与传感器缓存，并同步写 InfluxDB。
		// 缓存写同样是尽力而为，失败不影响上报成功。
		_ = s.cache.CacheDeviceStatus(ctx, deviceID, "ONLINE", nowMs)
		_ = s.cache.CacheSensorRecent(ctx, deviceID, dto.Sensors)
		s.writeSensorsSync(deviceID, dto)
	}

	// 4. 防抖批量更新 PostgreSQL（合并 5s 窗口内的所有更新为一次）
	s.schedulePGUpdate(deviceID)

	zap.S().Debugf("[DeviceReport] 设备 %s 上报 %d 条传感器数据", deviceID, len(dto.Sensors))
	return nil
}

// writeSensorsSync 降级写入传感器数据到 InfluxDB（Redis 队列也不可用时的最后一级兑底）。
// WriteSensorsAsync 不阻塞：写入槽位满时直接丢弃，不会把上报请求拖成超时。
func (s *DeviceReportService) writeSensorsSync(deviceID string, dto entity.DeviceStatusDTO) {
	data := make([]SensorData, len(dto.Sensors))
	for i, sensor := range dto.Sensors {
		ts := sensor.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		data[i] = SensorData{
			DeviceID:   deviceID,
			SensorName: sensor.Name,
			Type:       sensor.Type,
			Value:      sensor.Value,
			Timestamp:  ts,
		}
	}
	s.influxSvc.WriteSensorsAsync(data)
}

// ============================================================
// BatchWriter 接口实现（供 DeviceDataBuffer 后台 worker 调用）
// ============================================================

// FlushReports 批量写入传感器数据到 InfluxDB
//
// 必须同步返回写入结果：DeviceDataBuffer 靠这个 error 决定是否把整批数据重新入队。
// 早期版本这里调用 WriteSensorsAsync（fire-and-forget 后立即 return nil），
// 导致 drain 的"写入失败重新入队"永远不会触发——InfluxDB 故障时数据已被 RPOP 出队，
// 每批 200 条上报就这样静默丢弃。
func (s *DeviceReportService) FlushReports(ctx context.Context, reports []BufferedReport) error {
	if len(reports) == 0 {
		return nil
	}

	// 收集所有传感器数据点（优先用传感器自身时间戳，再用 report 级别时间戳）
	var allData []SensorData
	for _, report := range reports {
		for _, sensor := range report.Sensors {
			allData = append(allData, SensorData{
				DeviceID:   report.DeviceID,
				SensorName: sensor.Name,
				Type:       sensor.Type,
				Value:      sensor.Value,
				Timestamp:  resolveReportTime(sensor.Timestamp, report.Timestamp),
			})
		}
	}

	// 同步写：失败要让调用方（drain）看到并重新入队，不能异步后静默报成功
	return s.influxSvc.WriteSensors(ctx, allData)
}

// resolveReportTime 决定数据点写入时间：传感器时间戳 → report 时间戳 → 服务端当前时间。
//
// 不能写成 `time.UnixMilli(ts).IsZero()`：Unix 0 是 1970-01-01（不是零值时间），
// 一旦两个时间戳都缺，数据点会被写成 1970 年而在曲线上“消失”，必须显式拦下。
func resolveReportTime(sensorTs, reportTs int64) time.Time {
	ts := sensorTs
	if ts <= 0 {
		ts = reportTs
	}
	if ts <= 0 {
		return time.Now()
	}
	return time.UnixMilli(ts)
}

// Heartbeat 设备心跳（带 Token 校验，供非 HTTP 入口调用）
func (s *DeviceReportService) Heartbeat(ctx context.Context, deviceID, token string) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// Token 校验（仅非 HTTP 入口需要）。空 Token 一律拒绝，理由同 ReportStatus。
	if token == "" {
		return ErrDeviceTokenNotFound
	}
	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	return s.heartbeatFast(ctx, deviceID)
}

// HeartbeatFast 设备心跳（跳过 Token 校验，HTTP 中间件已验证）
func (s *DeviceReportService) HeartbeatFast(ctx context.Context, deviceID string) error {
	return s.heartbeatFast(ctx, util.NormalizeDeviceID(deviceID))
}

// heartbeatFast 核心心跳逻辑
func (s *DeviceReportService) heartbeatFast(ctx context.Context, deviceID string) error {
	nowMs := time.Now().UnixMilli()

	// L1 本地缓存检查设备存在
	localKey := "dev:" + deviceID
	if _, ok := s.localCache.Get(localKey); !ok {
		_, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
		if err != nil {
			return err
		}
		s.localCache.Set(localKey, "ONLINE")
	}

	// 仅更新设备状态 Hash（轻量级，无需完整 Device JSON）
	if err := s.cache.CacheDeviceStatus(ctx, deviceID, "ONLINE", nowMs); err != nil {
		return err
	}

	// 防抖批量更新 PostgreSQL
	s.schedulePGUpdate(deviceID)

	zap.S().Debugf("[DeviceReport] 设备 %s 心跳", deviceID)
	return nil
}

// GetDeviceStatus 获取设备最新状态（设备基础信息 + 状态 Hash 覆盖 + 传感器缓存）
func (s *DeviceReportService) GetDeviceStatus(ctx context.Context, deviceID string) (*entity.DeviceStatus, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 从 Device 缓存获取基础信息
	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	// 优先从 Hash 读取最新状态（避免完整 JSON 反序列化）
	status, lastActiveMs, err := s.cache.GetCachedDeviceStatus(ctx, deviceID)
	if err == nil && status != "" {
		device.Status = status
		if lastActiveMs > 0 {
			device.LastActiveTime = time.UnixMilli(lastActiveMs)
		}
	}

	// 从传感器缓存获取最新数据（SensorData.UnmarshalJSON 自动修正零值时间戳）
	// redis.Nil = 无缓存数据（设备未上报过），属正常情况，返回空列表而非错误
	var sensors []entity.SensorData
	err = s.cache.GetCachedSensorRecent(ctx, deviceID, &sensors)
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			// Redis 故障等真实错误才上报；无数据时继续返回空 sensors
			zap.L().Warn("[DeviceReport] 读取传感器缓存失败",
				zap.String("deviceID", deviceID), zap.Error(err))
		}
		sensors = nil
	}

	return &entity.DeviceStatus{
		ID:             device.ID,
		DeviceID:       device.ID,
		OwnerID:        device.OwnerID,
		Status:         device.Status,
		LastActiveTime: common.DateTimeFrom(device.LastActiveTime),
		Sensors:        sensors,
	}, nil
}

var (
	ErrInvalidDeviceToken  = &common.AppError{BizCode: 401, Message: "设备Token无效"}
	ErrDeviceTokenMismatch = &common.AppError{BizCode: 401, Message: "设备Token不匹配"}
	ErrDeviceNotFound      = &common.AppError{BizCode: 404, Message: "设备不存在"}
	ErrDeviceTokenNotFound = &common.AppError{BizCode: 401, Message: "缺少设备Token"}
)
