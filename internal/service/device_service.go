package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/util"
)

// DeviceService 设备服务
type DeviceService struct {
	repo  *repository.DeviceRepo
	cache *cache.RedisCache
}

func NewDeviceService(repo *repository.DeviceRepo, cache *cache.RedisCache) *DeviceService {
	return &DeviceService{repo: repo, cache: cache}
}

type DeviceRegisterRequest struct {
	DeviceName      string `json:"deviceName" binding:"required"`
	DeviceType      string `json:"deviceType"`
	FirmwareVersion string `json:"firmwareVersion"`
	IPAddress       string `json:"ipAddress"`
	MacAddress      string `json:"macAddress"`
	Location        string `json:"location"`
}

type DeviceRegisterResponse struct {
	DeviceID    string `json:"deviceId"`
	DeviceToken string `json:"deviceToken"`
}

func (s *DeviceService) Register(ctx context.Context, ownerID uint, req DeviceRegisterRequest) (*DeviceRegisterResponse, error) {
	var deviceID string
	now := time.Now()

	for {
		deviceID = util.GenerateShortDeviceID()
		exist, err := s.repo.GetByDeviceID(ctx, deviceID)
		if err != nil && !ent.IsNotFound(err) {
			return nil, fmt.Errorf("检查设备ID失败: %w", err)
		}
		if exist != nil {
			continue
		}

		_, err = s.repo.Create(ctx, &ent.Device{
			ID:              deviceID,
			DeviceName:      req.DeviceName,
			DeviceType:      req.DeviceType,
			FirmwareVersion: req.FirmwareVersion,
			IPAddress:       req.IPAddress,
			MACAddress:      req.MacAddress,
			Location:        req.Location,
			OwnerID:         ownerID,
			Status:          "OFFLINE",
			CreatedAt:       now,
			UpdatedAt:       now,
		})
		if err != nil {
			if strings.Contains(err.Error(), "duplicate key") {
				continue
			}
			return nil, fmt.Errorf("创建设备失败: %w", err)
		}
		break
	}

	deviceMgr := middleware.GetDeviceManager()
	token, err := deviceMgr.Login(deviceID, "device")
	if err != nil {
		return nil, fmt.Errorf("生成设备Token失败: %w", err)
	}

	// 缓存
	dev, _ := s.repo.GetByDeviceID(ctx, deviceID)
	if dev != nil {
		s.cache.CacheDevice(ctx, deviceID, dev)
	}

	return &DeviceRegisterResponse{DeviceID: deviceID, DeviceToken: token}, nil
}

func (s *DeviceService) GetByDeviceID(ctx context.Context, deviceID string) (*ent.Device, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	var device ent.Device
	if err := s.cache.GetCachedDevice(ctx, deviceID, &device); err == nil {
		return &device, nil
	}

	d, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	s.cache.CacheDevice(ctx, deviceID, d)
	return d, nil
}

func (s *DeviceService) ListByOwnerID(ctx context.Context, ownerID uint) ([]*ent.Device, error) {
	return s.repo.ListByOwnerID(ctx, ownerID)
}

func (s *DeviceService) Delete(ctx context.Context, deviceID string) error {
	deviceID = util.NormalizeDeviceID(deviceID)
	device, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, device.ID); err != nil {
		return err
	}

	go func() {
		bgCtx := context.Background()
		s.cache.EvictDeviceCache(bgCtx, deviceID)
		s.cache.EvictDeviceStatus(bgCtx, deviceID)
		s.cache.EvictSensorRecentCache(bgCtx, deviceID)
	}()
	return nil
}

func (s *DeviceService) GetDeviceToken(ctx context.Context, deviceID string, ownerID uint) (string, error) {
	deviceID = util.NormalizeDeviceID(deviceID)
	device, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return "", fmt.Errorf("设备不存在: %w", err)
	}
	if ownerID > 0 && device.OwnerID != ownerID {
		return "", fmt.Errorf("无权获取该设备Token")
	}

	deviceMgr := middleware.GetDeviceManager()
	return deviceMgr.Login(deviceID, "device")
}

func (s *DeviceService) UpdateStatus(ctx context.Context, deviceID, status string) error {
	deviceID = util.NormalizeDeviceID(deviceID)
	return s.repo.UpdateLastActive(ctx, deviceID, status)
}
