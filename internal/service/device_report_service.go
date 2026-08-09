package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

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
	buffer     *DeviceDataBuffer // Redis 写缓冲（可选）

	// L1 本地内存缓存：消除热点设备的 Redis 往返延迟
	localCache *cache.LocalCache

	// PostgreSQL 批量更新（debounced，避免每个请求 spawn goroutine）
	pgUpdateMu      sync.Mutex
	pgUpdatePending map[string]time.Time // deviceID → 最新活跃时间
	pgUpdateTimer   *time.Timer
}

const pgDebounceInterval = 5 * time.Second // PostgreSQL 活跃时间更新防抖间隔

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
	}
	return svc
}

// SetBuffer 注入缓冲器（由 main 初始化后调用）
func (s *DeviceReportService) SetBuffer(buf *DeviceDataBuffer) {
	s.buffer = buf
}

// schedulePGUpdate 防抖批量更新 PostgreSQL 设备活跃时间
// 替代原先每个请求 spawn 一个 goroutine 的做法
// 高并发下多个请求的活跃时间更新被合并为一次批量执行
func (s *DeviceReportService) schedulePGUpdate(deviceID string) {
	now := time.Now()

	s.pgUpdateMu.Lock()
	s.pgUpdatePending[deviceID] = now

	if s.pgUpdateTimer == nil {
		s.pgUpdateTimer = time.AfterFunc(pgDebounceInterval, func() {
			s.flushPGUpdates()
		})
	}
	s.pgUpdateMu.Unlock()
}

// flushPGUpdates 批量执行 PostgreSQL 活跃时间更新
func (s *DeviceReportService) flushPGUpdates() {
	s.pgUpdateMu.Lock()
	pending := s.pgUpdatePending
	s.pgUpdatePending = make(map[string]time.Time)
	s.pgUpdateTimer = nil
	s.pgUpdateMu.Unlock()

	ctx := context.Background()
	for deviceID := range pending {
		if err := s.deviceRepo.UpdateLastActive(ctx, deviceID, "ONLINE"); err != nil {
			zap.S().Warnf("[DeviceReport] 批量更新活跃时间失败 [device=%s]: %v", deviceID, err)
		}
	}
}

// ReportStatus 设备上报传感器数据（带 Token 校验，供非 HTTP 入口调用）
func (s *DeviceReportService) ReportStatus(ctx context.Context, deviceID, token string, dto entity.DeviceStatusDTO) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// Token 校验（仅非 HTTP 入口需要，HTTP 入口由中间件完成）
	if token != "" {
		deviceMgr := middleware.GetDeviceManager()
		tokenDeviceID, err := deviceMgr.GetLoginID(token)
		if err != nil {
			return ErrInvalidDeviceToken
		}
		if tokenDeviceID != deviceID {
			return ErrDeviceTokenMismatch
		}
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

	// 1. L1 本地缓存：快速确认设备存在（避免 Redis GET + JSON Unmarshal）
	localKey := "dev:" + deviceID
	if _, ok := s.localCache.Get(localKey); !ok {
		// 首次或缓存过期：从 Redis 读取设备信息（仅一次）
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

	// 序列化缓冲报告
	bufferData, _ := json.Marshal(BufferedReport{
		DeviceID:  deviceID,
		Sensors:   sensorDTOs,
		Timestamp: nowMs,
	})

	// 3. 一次 Pipeline 完成所有 Redis 写操作（1 次往返！）
	if s.buffer != nil {
		if err := s.cache.FastReportWrite(ctx, deviceID, "ONLINE", nowMs, dto.Sensors, bufferData); err != nil {
			zap.S().Warnf("[DeviceReport] 快速写入失败 [device=%s]: %v", deviceID, err)
			// 降级：逐条写入
			s.cache.CacheDeviceStatus(ctx, deviceID, "ONLINE", nowMs)
			s.cache.CacheSensorRecent(ctx, deviceID, dto.Sensors)
			s.writeSensorsSync(deviceID, dto)
		}
	} else {
		// 无缓冲器：Pipeline 仅更新状态 + 传感器
		s.cache.CacheDeviceStatus(ctx, deviceID, "ONLINE", nowMs)
		s.cache.CacheSensorRecent(ctx, deviceID, dto.Sensors)
		s.writeSensorsSync(deviceID, dto)
	}

	// 失效查询缓存（新数据入库，旧查询结果过期）
	// 异步执行，不阻塞响应
	go func() {
		_ = s.cache.EvictSensorQueryCache(context.Background(), deviceID)
	}()

	// 4. 防抖批量更新 PostgreSQL（合并 5s 窗口内的所有更新为一次）
	s.schedulePGUpdate(deviceID)

	zap.S().Debugf("[DeviceReport] 设备 %s 上报 %d 条传感器数据", deviceID, len(dto.Sensors))
	return nil
}

// writeSensorsSync 同步写入传感器数据到 InfluxDB（降级路径）
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
func (s *DeviceReportService) FlushReports(_ context.Context, reports []BufferedReport) error {
	if len(reports) == 0 {
		return nil
	}

	// 收集所有传感器数据点
	var allData []SensorData
	for _, report := range reports {
		for _, sensor := range report.Sensors {
			// 优先用传感器自身时间戳，再用 report 级别时间戳
			ts := sensor.Timestamp
			if ts == 0 {
				ts = report.Timestamp
			}
			t := time.UnixMilli(ts)
			if t.IsZero() {
				t = time.Now()
			}
			allData = append(allData, SensorData{
				DeviceID:   report.DeviceID,
				SensorName: sensor.Name,
				Type:       sensor.Type,
				Value:      sensor.Value,
				Timestamp:  t,
			})
		}
	}

	s.influxSvc.WriteSensorsAsync(allData)
	return nil
}

func (s *DeviceReportService) Heartbeat(ctx context.Context, deviceID, token string) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// Token 校验（仅非 HTTP 入口需要）
	if token != "" {
		deviceMgr := middleware.GetDeviceManager()
		tokenDeviceID, err := deviceMgr.GetLoginID(token)
		if err != nil {
			return ErrInvalidDeviceToken
		}
		if tokenDeviceID != deviceID {
			return ErrDeviceTokenMismatch
		}
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
	var sensors []entity.SensorData
	err = s.cache.GetCachedSensorRecent(ctx, deviceID, &sensors)
	if err != nil {
		return nil, err
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

func (s *DeviceReportService) EvictSensorRecentCache(ctx context.Context, deviceID string) error {
	return s.cache.EvictSensorRecentCache(ctx, util.NormalizeDeviceID(deviceID))
}

var (
	ErrInvalidDeviceToken  = &common.AppError{HTTPCode: 401, BizCode: 401, Message: "设备Token无效"}
	ErrDeviceTokenMismatch = &common.AppError{HTTPCode: 401, BizCode: 401, Message: "设备Token不匹配"}
	ErrDeviceNotFound      = &common.AppError{HTTPCode: 404, BizCode: 404, Message: "设备不存在"}
	ErrDeviceTokenNotFound = &common.AppError{HTTPCode: 401, BizCode: 401, Message: "缺少设备Token"}
)
