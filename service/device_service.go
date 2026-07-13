package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yu/iot-platform-go/cache"
	"github.com/yu/iot-platform-go/common"
	"github.com/yu/iot-platform-go/entity"
	"github.com/yu/iot-platform-go/middleware"
	"github.com/yu/iot-platform-go/repository"
	"github.com/yu/iot-platform-go/util"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// DeviceService 设备服务
type DeviceService struct {
	repo  *repository.DeviceRepo
	cache *cache.RedisCache
}

// NewDeviceService 创建设备服务
func NewDeviceService(repo *repository.DeviceRepo, cache *cache.RedisCache) *DeviceService {
	return &DeviceService{repo: repo, cache: cache}
}

// DeviceRegisterRequest 设备注册请求
type DeviceRegisterRequest struct {
	DeviceName      string `json:"deviceName" binding:"required"`
	DeviceType      string `json:"deviceType"`
	FirmwareVersion string `json:"firmwareVersion"`
	IPAddress       string `json:"ipAddress"`
	MacAddress      string `json:"macAddress"`
	Location        string `json:"location"`
}

// DeviceRegisterResponse 设备注册响应
type DeviceRegisterResponse struct {
	DeviceID    string `json:"deviceId"`
	DeviceToken string `json:"deviceToken"`
}

// Register 注册设备（使用 Sa-Token 设备 Manager 生成设备 Token）
func (s *DeviceService) Register(ctx context.Context, ownerID uint, req DeviceRegisterRequest) (*DeviceRegisterResponse, error) {
	var device *entity.Device
	var deviceID string

	// 生成唯一设备ID，插入失败自动重试
	for {
		deviceID = util.GenerateShortDeviceID()

		// 先检查是否已存在
		exist, err := s.repo.GetByDeviceID(ctx, deviceID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("检查设备ID失败: %w", err)
		}
		if exist != nil {
			continue // 碰撞，重新生成
		}

		device = &entity.Device{
			DeviceID:        deviceID,
			DeviceName:      req.DeviceName,
			DeviceType:      req.DeviceType,
			FirmwareVersion: req.FirmwareVersion,
			IPAddress:       req.IPAddress,
			MacAddress:      req.MacAddress,
			Location:        req.Location,
			OwnerID:         ownerID,
			Status:          entity.DeviceStatusOffline,
			CreatedAt:       common.DateTimeNow(),
			UpdatedAt:       common.DateTimeNow(),
		}

		if err := s.repo.Create(ctx, device); err != nil {
			// 并发冲突（SQLSTATE 23505），重试
			if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key") {
				continue
			}
			return nil, fmt.Errorf("创建设备失败: %w", err)
		}
		break
	}

	// Sa-Token 设备登录
	deviceMgr := middleware.GetDeviceManager()
	token, err := deviceMgr.Login(deviceID, "device")
	if err != nil {
		return nil, fmt.Errorf("生成设备Token失败: %w", err)
	}

	// 缓存设备信息
	if err := s.cache.CacheDevice(ctx, deviceID, device); err != nil {
		zap.S().Infof("[DeviceService] 缓存设备信息失败: %v", err)
	}

	return &DeviceRegisterResponse{
		DeviceID:    deviceID,
		DeviceToken: token,
	}, nil
}

// GetByDeviceID 根据设备ID查询设备（优先缓存）
func (s *DeviceService) GetByDeviceID(ctx context.Context, deviceID string) (*entity.Device, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 尝试从缓存获取
	var device entity.Device
	if err := s.cache.GetCachedDevice(ctx, deviceID, &device); err == nil {
		return &device, nil
	}

	// 从数据库查询
	deviceEntity, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	// 写入缓存
	if err := s.cache.CacheDevice(ctx, deviceID, deviceEntity); err != nil {
		zap.S().Infof("[DeviceService] 缓存设备失败: %v", err)
	}

	return deviceEntity, nil
}

// ListByOwnerID 查询用户的设备列表
func (s *DeviceService) ListByOwnerID(ctx context.Context, ownerID uint) ([]entity.Device, error) {
	return s.repo.ListByOwnerID(ctx, ownerID)
}

// Delete 删除设备
func (s *DeviceService) Delete(ctx context.Context, deviceID string) error {
	deviceID = util.NormalizeDeviceID(deviceID)

	device, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, device.ID); err != nil {
		return err
	}

	// 清除所有相关缓存
	go func() {
		bgCtx := context.Background()
		s.cache.EvictDeviceCache(bgCtx, deviceID)
		s.cache.EvictDeviceStatus(bgCtx, deviceID)
		s.cache.EvictSensorRecentCache(bgCtx, deviceID)
	}()

	return nil
}

// GetDeviceToken 获取设备Token（sa-token-go Redis 自动管理）
func (s *DeviceService) GetDeviceToken(ctx context.Context, deviceID string, ownerID uint) (string, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	// 验证设备归属
	device, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return "", fmt.Errorf("设备不存在: %w", err)
	}
	if ownerID > 0 && device.OwnerID != ownerID {
		return "", errors.New("无权获取该设备Token")
	}

	// Sa-Token 设备登录（token 由 sa-token-go Redis 存储管理）
	deviceMgr := middleware.GetDeviceManager()
	token, err := deviceMgr.Login(deviceID, "device")
	if err != nil {
		return "", err
	}

	return token, nil
}
