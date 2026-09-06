package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/common"
)

// ErrActuatorNotFound 执行器定义不存在
var ErrActuatorNotFound = errors.New("执行器不存在")

// DeviceActuatorService 设备执行器定义服务：物模型 CRUD + Apply 编译进 DeviceConfig 下发
type DeviceActuatorService struct {
	actuatorRepo *repository.DeviceActuatorRepo
	configSvc    *DeviceConfigService
	cache        *cache.RedisCache
}

// NewDeviceActuatorService 创建执行器定义服务
func NewDeviceActuatorService(actuatorRepo *repository.DeviceActuatorRepo, configSvc *DeviceConfigService, rcache *cache.RedisCache) *DeviceActuatorService {
	return &DeviceActuatorService{actuatorRepo: actuatorRepo, configSvc: configSvc, cache: rcache}
}

// List 查询设备全部执行器定义（整列表缓存：Cache-Aside，写路径显式失效）
func (s *DeviceActuatorService) List(ctx context.Context, deviceID string) ([]entity.Actuator, error) {
	out := []entity.Actuator{}
	err := s.cache.GetCachedActuatorDefsWithLoader(ctx, deviceID, &out, func(ctx context.Context) (any, error) {
		rows, err := s.actuatorRepo.ListByDeviceID(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		defs := make([]entity.Actuator, 0, len(rows))
		for _, row := range rows {
			defs = append(defs, actuatorToDTO(row))
		}
		return defs, nil
	})
	return out, err
}

// Get 查询单个执行器定义；不存在返回 (nil, nil)
func (s *DeviceActuatorService) Get(ctx context.Context, deviceID, actuatorID string) (*entity.Actuator, error) {
	row, err := s.actuatorRepo.Get(ctx, deviceID, actuatorID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	dto := actuatorToDTO(row)
	return &dto, nil
}

// Create 创建执行器定义（仅持久化，下发由 Apply 触发）
func (s *DeviceActuatorService) Create(ctx context.Context, deviceID string, req entity.ActuatorCreateRequest) (*entity.Actuator, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	row, err := s.actuatorRepo.Create(ctx, &ent.DeviceActuator{
		DeviceID:   deviceID,
		ActuatorID: req.ID,
		Name:       req.Name,
		Driver:     req.Driver,
		Params:     marshalJSONField(req.Config),
		Enabled:    enabled,
	})
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("执行器标识符已存在: %s", req.ID)
		}
		return nil, fmt.Errorf("创建执行器失败: %w", err)
	}
	s.evictDefsCache(ctx, deviceID)
	dto := actuatorToDTO(row)
	return &dto, nil
}

// Update 增量更新执行器定义；不存在返回 (nil, nil)
func (s *DeviceActuatorService) Update(ctx context.Context, deviceID, actuatorID string, req entity.ActuatorUpdateRequest) (*entity.Actuator, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	fields := repository.ActuatorUpdateFields{
		Name:    req.Name,
		Driver:  req.Driver,
		Enabled: req.Enabled,
	}
	if req.Config != nil {
		v := string(*req.Config)
		fields.Params = &v
	}

	row, err := s.actuatorRepo.Update(ctx, deviceID, actuatorID, fields)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	s.evictDefsCache(ctx, deviceID)
	dto := actuatorToDTO(row)
	return &dto, nil
}

// Delete 删除执行器定义；不存在时返回 ErrActuatorNotFound
func (s *DeviceActuatorService) Delete(ctx context.Context, deviceID, actuatorID string) error {
	n, err := s.actuatorRepo.Delete(ctx, deviceID, actuatorID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrActuatorNotFound
	}
	s.evictDefsCache(ctx, deviceID)
	return nil
}

// evictDefsCache 失效整设备执行器定义缓存。失效失败仅告警：
// 缓存有 TTL 兜底自愈，不应让主写操作因缓存失效失败而失败。
func (s *DeviceActuatorService) evictDefsCache(ctx context.Context, deviceID string) {
	if err := s.cache.EvictActuatorDefsCache(ctx, deviceID); err != nil {
		zap.L().Warn("[DeviceActuator] 失效定义缓存失败",
			zap.String("deviceID", deviceID), zap.Error(err))
	}
}

// Apply 将设备全部执行器定义编译进 DeviceConfig.payload.actuators 并版本化下发。
//
// 复用现有配置快照通道（type=config + MQTT retained）：设备端据此 diff 实例化/
// 卸载执行器；运行期动作经 type=control 命令按 id 路由执行。
func (s *DeviceActuatorService) Apply(ctx context.Context, deviceID string) (*entity.ActuatorApplyResponse, error) {
	rows, err := s.actuatorRepo.ListByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{}
	if existing, err := s.configSvc.Get(ctx, deviceID); err != nil {
		return nil, err
	} else if existing != nil && existing.Payload != "" {
		if err := json.Unmarshal([]byte(existing.Payload), &payload); err != nil {
			return nil, fmt.Errorf("解析现有配置失败: %w", err)
		}
	}

	actuators := make([]entity.Actuator, 0, len(rows))
	for _, row := range rows {
		actuators = append(actuators, actuatorToDTO(row))
	}
	payload["actuators"] = actuators

	cfg, err := s.configSvc.Save(ctx, deviceID, payload)
	if err != nil {
		return nil, err
	}

	return &entity.ActuatorApplyResponse{
		DeviceID: cfg.DeviceID,
		Version:  cfg.Version,
		Status:   cfg.Status,
		Count:    len(actuators),
	}, nil
}

// actuatorToDTO ent 实体 → DTO（params JSON 文本解析为 config 对象，空串输出空对象）
func actuatorToDTO(row *ent.DeviceActuator) entity.Actuator {
	return entity.Actuator{
		ID:        row.ActuatorID,
		Name:      row.Name,
		Driver:    row.Driver,
		Config:    parseJSONField(row.Params),
		Enabled:   row.Enabled,
		CreatedAt: common.DateTimeFrom(row.CreatedAt),
		UpdatedAt: common.DateTimeFrom(row.UpdatedAt),
	}
}
