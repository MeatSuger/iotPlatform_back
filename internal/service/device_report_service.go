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
type DeviceReportService struct {
	deviceRepo *repository.DeviceRepo
	influxSvc  *InfluxDBService
	cache      *cache.RedisCache
	deviceSvc  *DeviceService
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

func (s *DeviceReportService) ReportStatus(ctx context.Context, deviceID, token string, dto entity.DeviceStatusDTO) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}

	now := time.Now()
	if err := s.deviceRepo.UpdateLastActive(ctx, deviceID, "ONLINE"); err != nil {
		return err
	}

	// InfluxDB
	sensorPoints := make([]SensorPoint, len(dto.Sensors))
	for i, sensor := range dto.Sensors {
		sensorPoints[i] = SensorPoint{
			DeviceID:   deviceID,
			SensorName: sensor.Name,
			Type:       sensor.Type,
			Value:      sensor.Value,
			Timestamp:  now,
		}
	}
	if err := s.influxSvc.WriteDeviceSensors(ctx, deviceID, sensorPoints); err != nil {
		zap.S().Errorf("[DeviceReport] 写入InfluxDB失败 [device=%s]: %v", deviceID, err)
	}

	// 缓存
	deviceStatus := entity.DeviceStatus{
		ID:             device.ID,
		DeviceID:       device.ID,
		OwnerID:        device.OwnerID,
		Status:         "ONLINE",
		LastActiveTime: common.DateTimeFrom(now),
		Sensors:        dto.Sensors,
	}
	s.cache.CacheDeviceStatus(ctx, deviceID, deviceStatus)
	s.cache.EvictSensorRecentCache(ctx, deviceID)

	zap.S().Infof("[DeviceReport] 设备 %s 上报 %d 条传感器数据", deviceID, len(dto.Sensors))
	return nil
}

func (s *DeviceReportService) Heartbeat(ctx context.Context, deviceID, token string) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	deviceMgr := middleware.GetDeviceManager()
	tokenDeviceID, err := deviceMgr.GetLoginID(token)
	if err != nil {
		return ErrInvalidDeviceToken
	}
	if tokenDeviceID != deviceID {
		return ErrDeviceTokenMismatch
	}

	now := time.Now()
	if err := s.deviceRepo.UpdateLastActive(ctx, deviceID, "ONLINE"); err != nil {
		return err
	}

	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err == nil {
		device.Status = "ONLINE"
		device.LastActiveTime = now
		s.cache.CacheDevice(ctx, deviceID, device)
	}

	deviceStatus := entity.DeviceStatus{
		DeviceID:       deviceID,
		Status:         "ONLINE",
		LastActiveTime: common.DateTimeFrom(now),
	}
	s.cache.CacheDeviceStatus(ctx, deviceID, deviceStatus)

	zap.S().Infof("[DeviceReport] 设备 %s 心跳", deviceID)
	return nil
}

func (s *DeviceReportService) GetDeviceStatus(ctx context.Context, deviceID string) (*entity.DeviceStatus, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	var status entity.DeviceStatus
	if err := s.cache.GetCachedDeviceStatus(ctx, deviceID, &status); err == nil {
		return &status, nil
	}

	device, err := s.deviceSvc.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	return &entity.DeviceStatus{
		ID:             device.ID,
		DeviceID:       device.ID,
		OwnerID:        device.OwnerID,
		Status:         device.Status,
		LastActiveTime: common.DateTimeFrom(device.LastActiveTime),
	}, nil
}

func (s *DeviceReportService) EvictDeviceStatus(ctx context.Context, deviceID string) error {
	return s.cache.EvictDeviceStatus(ctx, util.NormalizeDeviceID(deviceID))
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
