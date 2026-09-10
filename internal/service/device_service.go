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
	// 子资源配置仓库：设备删除时显式级联清理（DB 层 FK ON DELETE CASCADE 兜底，
	// 此处保证即使 FK 约束缺失也不会留下孤儿数据）。任意可为 nil（走 DB 级联）。
	thingRepo  *repository.DeviceThingRepo
	configRepo *repository.DeviceConfigRepo
}

func NewDeviceService(repo *repository.DeviceRepo, cache *cache.RedisCache, thingRepo *repository.DeviceThingRepo, configRepo *repository.DeviceConfigRepo) *DeviceService {
	return &DeviceService{repo: repo, cache: cache, thingRepo: thingRepo, configRepo: configRepo}
}

// DeviceParameters 设备创建/更新请求体
// 注意：deviceId 与 ownerId 不在请求体中，由服务端从路径参数/登录态推导，
// 客户端无法通过请求体篡改归属或设备ID。
type DeviceParameters struct {
	DeviceName      string `json:"deviceName" binding:"required"`
	DeviceType      string `json:"deviceType"`
	FirmwareVersion string `json:"firmwareVersion"`
	IPAddress       string `json:"ipAddress"`
	MacAddress      string `json:"macAddress"`
	Location        string `json:"location"`
}

// DeviceUpdateParameters 设备增量更新请求体
// 全部字段为指针：仅出现在请求体中的字段会被更新，未传字段保持数据库原值。
// 与 DeviceParameters（注册，全量必填）区分，避免空串覆盖已有数据。
type DeviceUpdateParameters struct {
	DeviceName      *string `json:"deviceName"`
	DeviceType      *string `json:"deviceType"`
	FirmwareVersion *string `json:"firmwareVersion"`
	IPAddress       *string `json:"ipAddress"`
	MacAddress      *string `json:"macAddress"`
	Location        *string `json:"location"`
}

type DeviceRegisterResponse struct {
	DeviceID    string `json:"deviceId"`
	DeviceToken string `json:"deviceToken"`
}

// Register 注册设备（Write-Through：写DB后同步预热缓存）
func (s *DeviceService) Register(ctx context.Context, ownerID uint, req DeviceParameters) (*DeviceRegisterResponse, error) {
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

// Update 增量更新设备（Write-Through：写DB后同步预热缓存）
// 仅更新请求体中出现的字段，未传字段保持数据库原值。
// ownerID>0 表示用户操作（校验归属）；ownerID=0 表示设备自更新（跳过归属校验）。
func (s *DeviceService) Update(ctx context.Context, deviceID string, ownerID uint, req DeviceUpdateParameters) (*ent.Device, error) {
	deviceID = util.NormalizeDeviceID(deviceID)

	fields := repository.DeviceUpdateFields{
		DeviceName:      req.DeviceName,
		DeviceType:      req.DeviceType,
		FirmwareVersion: req.FirmwareVersion,
		IPAddress:       req.IPAddress,
		MACAddress:      req.MacAddress,
		Location:        req.Location,
	}
	if fields.IsEmpty() {
		return nil, fmt.Errorf("无更新字段")
	}

	// 归属校验（仅用户操作）：先查该用户的设备列表缓存，命中即有权；
	// 缓存未命中（列表缓存陈旧/未包含）时回源 DB 点查，避免误拒。
	if ownerID > 0 {
		zap.L().Debug("user info",
			zap.Uint("id", ownerID),
			zap.String("deviceID", deviceID),
		)
		devices, err := s.ListByOwnerID(ctx, ownerID)
		if err != nil {
			return nil, fmt.Errorf("查询设备列表失败: %w", err)
		}
		var owned bool
		for _, d := range devices {
			if d != nil && d.ID == deviceID {
				owned = true
				break
			}
		}
		if !owned {
			// DB 回源兜底：设备确实存在但属于他人 → 无权；不存在 → 设备不存在
			dev, err := s.repo.GetByDeviceID(ctx, deviceID)
			if err != nil {
				return nil, fmt.Errorf("设备不存在")
			}
			if dev.OwnerID != ownerID {
				return nil, fmt.Errorf("无权操作该设备")
			}
		}
	}

	// 仅更新可编辑字段；ID 用于定位且不可被修改（schema 中 Immutable），OwnerID 不传入 → 归属不可篡改
	if err := s.repo.Update(ctx, deviceID, fields); err != nil {
		return nil, fmt.Errorf("更新设备失败: %w", err)
	}

	// 回读最新数据并预热缓存（Write-Through）
	dev, err := s.repo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("获取设备失败: %w", err)
	}
	if dev == nil {
		return nil, fmt.Errorf("设备不存在")
	}
	_ = s.cache.CacheDevice(ctx, deviceID, dev)
	_ = s.cache.EvictDeviceListCache(ctx, dev.OwnerID)

	return dev, nil
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

	// 显式级联删除子资源（配置快照/传感器定义/执行器定义）。
	// 先删子资源再删设备：任何一步失败仅告警，由 DB 层 FK CASCADE 兜底。
	s.deleteCascades(ctx, deviceID)

	if err := s.repo.Delete(ctx, device.ID); err != nil {
		return err
	}

	// 同步失效本实例所有相关缓存，并通过 Pub-Sub 通知其他实例失效其 L1
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

// deleteCascades 显式级联删除设备子资源配置（未注入的 repo 降级为依赖 DB FK）
func (s *DeviceService) deleteCascades(ctx context.Context, deviceID string) {
	type cascader struct {
		name string
		fn   func() error
	}
	cascades := make([]cascader, 0, 2)
	if s.configRepo != nil {
		cascades = append(cascades, cascader{"config", func() error { return s.configRepo.Delete(ctx, deviceID) }})
	}
	if s.thingRepo != nil {
		cascades = append(cascades, cascader{"thing", func() error { return s.thingRepo.DeleteByDeviceID(ctx, deviceID) }})
	}
	for _, c := range cascades {
		if err := c.fn(); err != nil {
			zap.L().Warn("[Device] 子资源级联删除失败（由 DB FK 兜底）",
				zap.String("deviceID", deviceID), zap.String("resource", c.name), zap.Error(err))
		}
	}
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

// UpdateStatus 更新设备在线状态（Write-Through 同步轻量状态 Hash）
func (s *DeviceService) UpdateStatus(ctx context.Context, deviceID, status string) error {
	deviceID = util.NormalizeDeviceID(deviceID)
	if err := s.repo.UpdateLastActive(ctx, deviceID, status); err != nil {
		return err
	}
	// 同步状态 Hash（不失效设备 JSON 缓存，避免下次读取回源 DB）；
	// GetDeviceStatus 读取时以 Hash 覆盖 JSON 中的旧状态，离线后立即反映
	nowMs := time.Now().UnixMilli()
	_ = s.cache.CacheDeviceStatus(ctx, deviceID, status, nowMs)
	return nil
}
