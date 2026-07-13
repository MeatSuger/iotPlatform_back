package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/yu/iot-platform-go/cache"
	"github.com/yu/iot-platform-go/common"
	"github.com/yu/iot-platform-go/entity"
	"github.com/yu/iot-platform-go/middleware"
	"github.com/yu/iot-platform-go/repository"
	"github.com/yu/iot-platform-go/util"
)

// DeviceReportService 设备数据上报服务
type DeviceReportService struct {
	deviceRepo *repository.DeviceRepo
	influxSvc  *InfluxDBService
	cache      *cache.RedisCache
	deviceSvc  *DeviceService
}

// NewDeviceReportService 创建设备上报服务
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

// ReportStatus 上报设备传感器数据
func (s *DeviceReportService) ReportStatus(ctx context.Context, deviceID, token string, dto entity.DeviceStatusDTO) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 验证设备Token（Sa-Token）
	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	// 查询设备信息
	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}

	// 更新设备状态（直接 UPDATE，不用 Save 避免 INSERT 误判）
	now := common.DateTimeNow()
	if err := s.deviceRepo.UpdateLastActive(ctx, deviceID, util.DeviceOnlineStatus); err != nil {
		return err
	}

	// 同步写入InfluxDB（确保数据立即可查）
	sensorPoints := make([]SensorPoint, len(dto.Sensors))
	for i, sensor := range dto.Sensors {
		sensorPoints[i] = SensorPoint{
			DeviceID:   deviceID,
			SensorName: sensor.Name,
			Type:       sensor.Type,
			Value:      sensor.Value,
			Timestamp:  now.Time, // 服务器生成时间，不信任前端
		}
	}
	if err := s.influxSvc.WriteDeviceSensors(ctx, deviceID, sensorPoints); err != nil {
		zap.S().Errorf("[DeviceReport] 写入InfluxDB失败 [device=%s]: %v", deviceID, err)
	}

	// 更新设备状态缓存
	deviceStatus := entity.DeviceStatus{
		ID:             device.ID,
		DeviceID:       deviceID,
		OwnerID:        device.OwnerID,
		Status:         util.DeviceOnlineStatus,
		LastActiveTime: now,
		Sensors:        dto.Sensors,
	}
	if err := s.cache.CacheDeviceStatus(ctx, deviceID, deviceStatus); err != nil {
		zap.S().Infof("[DeviceReport] 缓存设备状态失败: %v", err)
	}

	// 清除传感器近期缓存
	if err := s.cache.EvictSensorRecentCache(ctx, deviceID); err != nil {
		zap.S().Infof("[DeviceReport] 清除传感器缓存失败: %v", err)
	}

	zap.S().Infof("[DeviceReport] 设备 %s 上报 %d 条传感器数据", deviceID, len(dto.Sensors))
	return nil
}

// Heartbeat 设备心跳
func (s *DeviceReportService) Heartbeat(ctx context.Context, deviceID, token string) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 验证设备Token（Sa-Token）
	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	// 更新设备状态
	now := common.DateTimeNow()
	if err := s.deviceRepo.UpdateStatus(ctx, deviceID, util.DeviceOnlineStatus); err != nil {
		return err
	}

	// 更新设备缓存
	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err == nil {
		device.Status = util.DeviceOnlineStatus
		device.LastActiveTime = now
		s.cache.CacheDevice(ctx, deviceID, device)
	}

	// 更新状态缓存
	deviceStatus := entity.DeviceStatus{
		DeviceID:       deviceID,
		Status:         util.DeviceOnlineStatus,
		LastActiveTime: now,
	}
	if err := s.cache.CacheDeviceStatus(ctx, deviceID, deviceStatus); err != nil {
		zap.S().Infof("[DeviceReport] 缓存心跳状态失败: %v", err)
	}

	zap.S().Infof("[DeviceReport] 设备 %s 心跳", deviceID)
	return nil
}

// GetDeviceStatus 获取设备状态（优先缓存）
func (s *DeviceReportService) GetDeviceStatus(ctx context.Context, deviceID string) (*entity.DeviceStatus, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 尝试从缓存获取
	var status entity.DeviceStatus
	if err := s.cache.GetCachedDeviceStatus(ctx, deviceID, &status); err == nil {
		return &status, nil
	}

	// 从数据库获取设备信息
	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	status = entity.DeviceStatus{
		ID:             device.ID,
		DeviceID:       device.DeviceID,
		OwnerID:        device.OwnerID,
		Status:         device.Status,
		LastActiveTime: device.LastActiveTime,
	}

	return &status, nil
}

// EvictDeviceStatus 清除设备状态缓存
func (s *DeviceReportService) EvictDeviceStatus(ctx context.Context, deviceID string) error {
	return s.cache.EvictDeviceStatus(ctx, util.NormalizeDeviceID(deviceID))
}

// EvictSensorRecentCache 清除传感器近期缓存
func (s *DeviceReportService) EvictSensorRecentCache(ctx context.Context, deviceID string) error {
	return s.cache.EvictSensorRecentCache(ctx, util.NormalizeDeviceID(deviceID))
}

// 错误定义（使用统一的 common.AppError）
var (
	ErrInvalidDeviceToken  = &common.AppError{HTTPCode: 401, BizCode: 401, Message: "设备Token无效"}
	ErrDeviceTokenMismatch = &common.AppError{HTTPCode: 401, BizCode: 401, Message: "设备Token不匹配"}
	ErrDeviceNotFound      = &common.AppError{HTTPCode: 404, BizCode: 404, Message: "设备不存在"}
	ErrDeviceTokenNotFound = &common.AppError{HTTPCode: 401, BizCode: 401, Message: "缺少设备Token"}
)
