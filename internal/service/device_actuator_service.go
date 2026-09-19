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
	"fmt"

	"go.uber.org/zap"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
)

// ErrActuatorNotFound 执行器定义不存在
var ErrActuatorNotFound = errors.New("执行器不存在")

// DeviceActuatorService 设备执行器定义服务：物模型 CRUD + Apply 编译进 DeviceConfig 下发。
// 存储统一走 iot_device_thing（kind=actuator），driver 等专有字段序列化进 specs JSON。
type DeviceActuatorService struct {
	thingRepo *repository.DeviceThingRepo
	configSvc *DeviceConfigService
	cache     *cache.RedisCache
}

// NewDeviceActuatorService 创建执行器定义服务
func NewDeviceActuatorService(thingRepo *repository.DeviceThingRepo, configSvc *DeviceConfigService, rcache *cache.RedisCache) *DeviceActuatorService {
	return &DeviceActuatorService{thingRepo: thingRepo, configSvc: configSvc, cache: rcache}
}

// List 查询设备全部执行器定义（整列表缓存：Cache-Aside，写路径显式失效）
func (s *DeviceActuatorService) List(ctx context.Context, deviceID string) ([]entity.Actuator, error) {
	out := []entity.Actuator{}
	err := s.cache.GetCachedActuatorDefsWithLoader(ctx, deviceID, &out, func(ctx context.Context) (any, error) {
		rows, err := s.thingRepo.ListByDeviceID(ctx, deviceID, ThingKindActuator)
		if err != nil {
			return nil, err
		}
		defs := make([]entity.Actuator, 0, len(rows))
		for _, row := range rows {
			defs = append(defs, thingToActuator(row))
		}
		return defs, nil
	})
	return out, err
}

// Get 查询单个执行器定义；不存在返回 (nil, nil)
func (s *DeviceActuatorService) Get(ctx context.Context, deviceID, actuatorID string) (*entity.Actuator, error) {
	row, err := s.thingRepo.Get(ctx, deviceID, ThingKindActuator, actuatorID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	dto := thingToActuator(row)
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

	body := actuatorThingBody{Driver: req.Driver, Specs: req.Specs}

	row, err := s.thingRepo.Create(ctx, &ent.DeviceThing{
		DeviceID: deviceID,
		Kind:     ThingKindActuator,
		ThingID:  req.ID,
		Name:     req.Name,
		Specs:    marshalActuatorBody(body),
		Enabled:  enabled,
	})
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("执行器标识符已存在: %s", req.ID)
		}
		return nil, fmt.Errorf("创建执行器失败: %w", err)
	}
	s.evictDefsCache(ctx, deviceID)
	dto := thingToActuator(row)
	return &dto, nil
}

// Update 增量更新执行器定义；不存在返回 (nil, nil)
func (s *DeviceActuatorService) Update(ctx context.Context, deviceID, actuatorID string, req entity.ActuatorUpdateRequest) (*entity.Actuator, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	row, err := s.thingRepo.Get(ctx, deviceID, ThingKindActuator, actuatorID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	body := parseActuatorBody(row.Specs)
	fields := repository.ThingUpdateFields{}

	if req.Name != nil {
		fields.Name = req.Name
	}
	if req.Driver != nil {
		body.Driver = *req.Driver
	}
	if req.Specs != nil {
		var specs map[string]any
		if err := json.Unmarshal(*req.Specs, &specs); err != nil {
			return nil, fmt.Errorf("specs 非法: %w", err)
		}
		body.Specs = specs // {} 解析为空 map → 序列化时省略
	}
	if req.Enabled != nil {
		fields.Enabled = req.Enabled
	}
	specs := marshalActuatorBody(body)
	fields.Specs = &specs

	row, err = s.thingRepo.Update(ctx, deviceID, ThingKindActuator, actuatorID, fields)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	s.evictDefsCache(ctx, deviceID)
	dto := thingToActuator(row)
	return &dto, nil
}

// Delete 删除执行器定义；不存在时返回 ErrActuatorNotFound
func (s *DeviceActuatorService) Delete(ctx context.Context, deviceID, actuatorID string) error {
	n, err := s.thingRepo.Delete(ctx, deviceID, ThingKindActuator, actuatorID)
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
	rows, err := s.thingRepo.ListByDeviceID(ctx, deviceID, ThingKindActuator)
	if err != nil {
		return nil, err
	}

	actuators := make([]entity.ActuatorWire, 0, len(rows))
	for _, row := range rows {
		actuators = append(actuators, thingToActuatorWire(row))
	}

	cfg, err := buildConfigWithThingModel(ctx, s.configSvc, deviceID, "actuators", actuators)
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
