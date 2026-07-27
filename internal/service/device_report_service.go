package service

import (
	"context"
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
}

func NewDeviceReportService(
	deviceRepo *repository.DeviceRepo,
	influxSvc *InfluxDBService,
	cache *cache.RedisCache,
	deviceSvc *DeviceService,
) *DeviceReportService {
	return &DeviceReportService{
		deviceRepo: deviceRepo,
		influxSvc:  influxSvc,
		cache:      cache,
		deviceSvc:  deviceSvc,
	}
}

// SetBuffer 注入缓冲器（由 main 初始化后调用）
func (s *DeviceReportService) SetBuffer(buf *DeviceDataBuffer) {
	s.buffer = buf
}

// ReportStatus 设备上报传感器数据（1000 并发优化版）
//
// 快速路径（~1ms）：
//  1. Token 校验（Redis 查询，极快）
//  2. 更新 Redis 设备状态缓存（立即生效）
//  3. 将数据推入 Redis List 缓冲队列（1 次 LPush）
//
// 慢速路径（后台 worker 异步）：
//   - 批量写入 InfluxDB
//   - 批量更新 PostgreSQL 设备活跃时间（30s 防抖）
func (s *DeviceReportService) ReportStatus(ctx context.Context, deviceID, token string, dto entity.DeviceStatusDTO) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 1. Token 校验（Redis-backed Sa-Token，极快）
	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	// 2. 获取设备信息（优先 Redis 缓存）
	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}

	now := time.Now()
	nowMs := now.UnixMilli()

	// 3. 更新 Device 缓存中的运行时状态（Status + LastActiveTime），不再另存 DeviceStatus
	device.Status = "ONLINE"
	device.LastActiveTime = now
	s.cache.CacheDevice(ctx, deviceID, device)

	// 同时缓存最新传感器数据（用于快速查询）
	s.cache.CacheSensorRecent(ctx, deviceID, dto.Sensors)

	// 4. 防抖更新 PostgreSQL（30s 内同一设备只写一次）
	shouldUpdate, _ := s.cache.ShouldUpdateActive(ctx, deviceID)
	if shouldUpdate {
		go func() {
			bgCtx := context.Background()
			if err := s.deviceRepo.UpdateLastActive(bgCtx, deviceID, "ONLINE"); err != nil {
				zap.S().Warnf("[DeviceReport] 更新活跃时间失败 [device=%s]: %v", deviceID, err)
			}
		}()
	}

	// 5. 推入 Redis 缓冲队列 → 后台 worker 批量写 InfluxDB
	if s.buffer != nil {
		sensorDTOs := make([]SensorDataDTO, len(dto.Sensors))
		for i, sensor := range dto.Sensors {
			sensorDTOs[i] = SensorDataDTO{
				Name:      sensor.Name,
				Type:      sensor.Type,
				Value:     sensor.Value,
				Timestamp: sensor.Timestamp.UnixMilli(),
			}
		}
		report := BufferedReport{
			DeviceID:  deviceID,
			Token:     token,
			Sensors:   sensorDTOs,
			Timestamp: nowMs,
		}
		if err := s.buffer.Enqueue(ctx, report); err != nil {
			zap.S().Warnf("[DeviceReport] 入队失败 [device=%s]: %v", deviceID, err)
			// 降级：同步写入 InfluxDB
			s.writeSensorsSync(ctx, deviceID, dto)
		}
	} else {
		// 无缓冲器：同步写入（兼容旧逻辑）
		s.writeSensorsSync(ctx, deviceID, dto)
	}

	zap.S().Infof("[DeviceReport] 设备 %s 上报 %d 条传感器数据", deviceID, len(dto.Sensors))
	return nil
}

// writeSensorsSync 同步写入传感器数据到 InfluxDB（降级路径）
func (s *DeviceReportService) writeSensorsSync(ctx context.Context, deviceID string, dto entity.DeviceStatusDTO) {
	sensorPoints := make([]SensorPoint, len(dto.Sensors))
	for i, sensor := range dto.Sensors {
		ts := sensor.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		sensorPoints[i] = SensorPoint{
			DeviceID:   deviceID,
			SensorName: sensor.Name,
			Type:       sensor.Type,
			Value:      sensor.Value,
			Timestamp:  ts,
		}
	}
	// 使用异步批量写入替代逐条同步写入
	s.influxSvc.WriteDeviceSensorsBatch(deviceID, sensorPoints)
}

// ============================================================
// BatchWriter 接口实现（供 DeviceDataBuffer 后台 worker 调用）
// ============================================================

// FlushReports 批量写入传感器数据到 InfluxDB
func (s *DeviceReportService) FlushReports(ctx context.Context, reports []BufferedReport) error {
	if len(reports) == 0 {
		return nil
	}

	// 收集所有传感器数据点
	var allPoints []SensorPoint
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
			allPoints = append(allPoints, SensorPoint{
				DeviceID:   report.DeviceID,
				SensorName: sensor.Name,
				Type:       sensor.Type,
				Value:      sensor.Value,
				Timestamp:  t,
			})
		}
	}

	// 使用异步批量写入
	s.influxSvc.WriteDeviceSensorsBatch("", allPoints)
	return nil
}

func (s *DeviceReportService) Heartbeat(ctx context.Context, deviceID, token string) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// Token 校验（Redis-backed，极快）
	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	now := time.Now()

	// 更新 Device 缓存中的运行时状态
	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err == nil {
		device.Status = "ONLINE"
		device.LastActiveTime = now
		s.cache.CacheDevice(ctx, deviceID, device)
	}

	// 防抖更新 PostgreSQL（30s 内同一设备只写一次）
	shouldUpdate, _ := s.cache.ShouldUpdateActive(ctx, deviceID)
	if shouldUpdate {
		go func() {
			bgCtx := context.Background()
			if err := s.deviceRepo.UpdateLastActive(bgCtx, deviceID, "ONLINE"); err != nil {
				zap.S().Warnf("[DeviceReport] 心跳更新活跃时间失败 [device=%s]: %v", deviceID, err)
			}
		}()
	}

	zap.S().Infof("[DeviceReport] 设备 %s 心跳", deviceID)
	return nil
}

func (s *DeviceReportService) GetDeviceStatus(ctx context.Context, deviceID string) (*entity.DeviceStatus, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 从 Device 缓存获取基础信息
	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	// 从传感器缓存获取最新数据（SensorData.UnmarshalJSON 自动修正零值时间戳）
	var sensors []entity.SensorData
	s.cache.GetCachedSensorRecent(ctx, deviceID, &sensors)

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
