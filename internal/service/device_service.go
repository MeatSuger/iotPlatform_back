package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
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

// Register 注册设备（Write-Through：写DB后同步预热缓存）
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

	// Write-Through：预热缓存
	dev, _ := s.repo.GetByDeviceID(ctx, deviceID)
	if dev != nil {
		_ = s.cache.CacheDevice(ctx, deviceID, dev)
	}
	// 失效用户设备列表缓存（新设备加入）
	_ = s.cache.EvictDeviceListCache(ctx, ownerID)

	return &DeviceRegisterResponse{DeviceID: deviceID, DeviceToken: token}, nil
}

// GetByDeviceID 获取设备（Cache-Aside：L1本地 → L2 Redis → PostgreSQL回源）
// 参考：seaguest/cache 的 loader 模式 + go-redis/cache 的 Cache-Aside 模式
func (s *DeviceService) GetByDeviceID(ctx context.Context, deviceID string) (*ent.Device, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	var device ent.Device
	err := s.cache.GetCachedDeviceWithLoader(ctx, deviceID, &device, func(ctx context.Context) (any, error) {
		// L1/L2 均 miss，从 PostgreSQL 回源（带 singleflight 防击穿）
		d, err := s.repo.GetByDeviceID(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		return d, nil
	})
	if err != nil {
		return nil, err
	}
	return &device, nil
}

// ListByOwnerID 获取用户设备列表（Cache-Aside，2分钟缓存）
func (s *DeviceService) ListByOwnerID(ctx context.Context, ownerID uint) ([]*ent.Device, error) {
	var devices []*ent.Device
	err := s.cache.GetCachedDeviceList(ctx, ownerID, &devices, func(ctx context.Context) (any, error) {
		return s.repo.ListByOwnerID(ctx, ownerID)
	})
	if err != nil {
		return nil, err
	}
	return devices, nil
}

// Delete 删除设备（Write-Invalidate：删DB后同步失效所有相关缓存）
func (s *DeviceService) Delete(ctx context.Context, deviceID string) error {
	deviceID = util.NormalizeDeviceID(deviceID)
	device, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, device.ID); err != nil {
		return err
	}

	// 同步失效所有相关缓存（不阻塞响应，异步发布 Pub-Sub 通知其他实例）
	_ = s.cache.EvictDeviceCache(ctx, deviceID)
	_ = s.cache.EvictSensorRecentCache(ctx, deviceID)
	_ = s.cache.EvictDeviceStatusCache(ctx, deviceID)
	// 清理该设备的查询缓存/命令队列/MQTT 消息
	_ = s.cache.EvictDeviceAllCaches(ctx, deviceID)
	// 失效该用户设备列表缓存
	for _, ownerID := range []uint{device.OwnerID} {
		_ = s.cache.EvictDeviceListCache(ctx, ownerID)
	}

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

// DeviceTokenInactiveTTL 设备离线多久后清理其 Token（不删除设备）
const DeviceTokenInactiveTTL = 30 * 24 * time.Hour

// CleanupInactiveDeviceTokens 清理长时间未上线设备的 Token（不删除设备记录）
// 返回清理数量。设备 30 天未上线（含从未上线但注册超 30 天）即删除其 Token。
func (s *DeviceService) CleanupInactiveDeviceTokens(ctx context.Context, inactiveBefore time.Time) (int, error) {
	devices, err := s.repo.ListInactiveBefore(ctx, inactiveBefore)
	if err != nil {
		return 0, fmt.Errorf("查询离线设备失败: %w", err)
	}

	deviceMgr := middleware.GetDeviceManager()
	if deviceMgr == nil {
		return 0, fmt.Errorf("设备Token管理器未初始化")
	}

	cleaned := 0
	for _, d := range devices {
		if err := deviceMgr.Logout(d.ID, "device"); err != nil {
			zap.S().Warnf("[Device] 清理设备Token失败 [deviceId=%s]: %v", d.ID, err)
			continue
		}
		cleaned++
	}
	if cleaned > 0 {
		zap.S().Infof("[Device] 已清理 %d 个离线设备的Token", cleaned)
	}
	return cleaned, nil
}

func (s *DeviceService) UpdateStatus(ctx context.Context, deviceID, status string) error {
	deviceID = util.NormalizeDeviceID(deviceID)
	if err := s.repo.UpdateLastActive(ctx, deviceID, status); err != nil {
		return err
	}
	// 同步更新设备缓存中的状态（Write-Through）：
	// 不失效整个设备缓存（避免下一次读取回源 DB），而是同步状态 Hash
	// 与设备 JSON 缓存，确保离线后状态立即反映（否则最长 10 分钟显示 ONLINE）
	nowMs := time.Now().UnixMilli()
	_ = s.cache.CacheDeviceStatus(ctx, deviceID, status, nowMs)
	return nil
}
