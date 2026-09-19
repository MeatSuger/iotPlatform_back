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

// ErrSensorNotFound 传感器定义不存在
var ErrSensorNotFound = errors.New("传感器不存在")

// DeviceSensorService 设备传感器定义服务：物模型 CRUD + Apply 编译进 DeviceConfig 下发。
// 存储统一走 iot_device_thing（kind=sensor），sensor 专有字段序列化进 specs JSON。
type DeviceSensorService struct {
	thingRepo *repository.DeviceThingRepo
	configSvc *DeviceConfigService
	cache     *cache.RedisCache
}

// NewDeviceSensorService 创建传感器定义服务
func NewDeviceSensorService(thingRepo *repository.DeviceThingRepo, configSvc *DeviceConfigService, rcache *cache.RedisCache) *DeviceSensorService {
	return &DeviceSensorService{thingRepo: thingRepo, configSvc: configSvc, cache: rcache}
}

// List 查询设备全部传感器定义（整列表缓存：Cache-Aside，写路径显式失效）
func (s *DeviceSensorService) List(ctx context.Context, deviceID string) ([]entity.Sensor, error) {
	out := []entity.Sensor{}
	err := s.cache.GetCachedSensorDefsWithLoader(ctx, deviceID, &out, func(ctx context.Context) (any, error) {
		rows, err := s.thingRepo.ListByDeviceID(ctx, deviceID, ThingKindSensor)
		if err != nil {
			return nil, err
		}
		defs := make([]entity.Sensor, 0, len(rows))
		for _, row := range rows {
			defs = append(defs, thingToSensor(row))
		}
		return defs, nil
	})
	return out, err
}

// Get 查询单个传感器定义；不存在返回 (nil, nil)
func (s *DeviceSensorService) Get(ctx context.Context, deviceID, sensorID string) (*entity.Sensor, error) {
	row, err := s.thingRepo.Get(ctx, deviceID, ThingKindSensor, sensorID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	dto := thingToSensor(row)
	return &dto, nil
}

// Create 创建传感器定义（仅持久化，下发由 Apply 触发）
func (s *DeviceSensorService) Create(ctx context.Context, deviceID string, req entity.SensorCreateRequest) (*entity.Sensor, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	dataType, _ := entity.ValidateDataType(req.DataType)

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	body := sensorThingBody{
		Type:           req.Type,
		DataType:       dataType,
		Unit:           req.Unit,
		Specs:          req.Specs,
		ReportInterval: req.ReportInterval,
	}

	row, err := s.thingRepo.Create(ctx, &ent.DeviceThing{
		DeviceID: deviceID,
		Kind:     ThingKindSensor,
		ThingID:  req.ID,
		Name:     req.Name,
		Specs:    marshalSensorBody(body),
		Enabled:  enabled,
	})
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("传感器标识符已存在: %s", req.ID)
		}
		return nil, fmt.Errorf("创建传感器失败: %w", err)
	}
	s.evictDefsCache(ctx, deviceID)
	dto := thingToSensor(row)
	return &dto, nil
}

// Update 增量更新传感器定义；不存在返回 (nil, nil)
func (s *DeviceSensorService) Update(ctx context.Context, deviceID, sensorID string, req entity.SensorUpdateRequest) (*entity.Sensor, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	row, err := s.thingRepo.Get(ctx, deviceID, ThingKindSensor, sensorID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	body := parseSensorBody(row.Specs)
	fields := repository.ThingUpdateFields{}

	if req.Name != nil {
		fields.Name = req.Name
	}
	if req.Type != nil {
		body.Type = *req.Type
	}
	if req.DataType != nil {
		body.DataType = *req.DataType
	}
	if req.Unit != nil {
		body.Unit = *req.Unit
	}
	if req.Specs != nil {
		if string(*req.Specs) == "{}" {
			body.Specs = nil // 显式清空
		} else {
			var sp entity.SensorSpecs
			if err := json.Unmarshal(*req.Specs, &sp); err != nil {
				return nil, fmt.Errorf("specs 非法: %w", err)
			}
			body.Specs = &sp
		}
	}
	if req.ReportInterval != nil {
		if string(*req.ReportInterval) == "null" {
			body.ReportInterval = nil // 恢复继承全局采样周期
		} else {
			var n int
			_ = json.Unmarshal(*req.ReportInterval, &n) // Validate 已保证为合法正整数
			body.ReportInterval = &n
		}
	}
	if req.Enabled != nil {
		fields.Enabled = req.Enabled
	}
	specs := marshalSensorBody(body)
	fields.Specs = &specs

	row, err = s.thingRepo.Update(ctx, deviceID, ThingKindSensor, sensorID, fields)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	s.evictDefsCache(ctx, deviceID)
	dto := thingToSensor(row)
	return &dto, nil
}

// Delete 删除传感器定义；不存在时返回 ErrSensorNotFound
func (s *DeviceSensorService) Delete(ctx context.Context, deviceID, sensorID string) error {
	n, err := s.thingRepo.Delete(ctx, deviceID, ThingKindSensor, sensorID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSensorNotFound
	}
	s.evictDefsCache(ctx, deviceID)
	return nil
}

// evictDefsCache 失效整设备传感器定义缓存。失效失败仅告警：
// 缓存有 TTL 兜底自愈，不应让主写操作因缓存失效失败而失败。
func (s *DeviceSensorService) evictDefsCache(ctx context.Context, deviceID string) {
	if err := s.cache.EvictSensorDefsCache(ctx, deviceID); err != nil {
		zap.L().Warn("[DeviceSensor] 失效定义缓存失败",
			zap.String("deviceID", deviceID), zap.Error(err))
	}
}

// Apply 将设备全部传感器定义编译进 DeviceConfig.payload.sensors 并版本化下发。
//
// 复用现有下行命令通道（type=config）：在线设备经 WS 实时推送，
// 离线设备下次轮询 /commands 拉取；也可经 GET /config 兜底获取。
func (s *DeviceSensorService) Apply(ctx context.Context, deviceID string) (*entity.SensorApplyResponse, error) {
	rows, err := s.thingRepo.ListByDeviceID(ctx, deviceID, ThingKindSensor)
	if err != nil {
		return nil, err
	}

	sensors := make([]entity.SensorWire, 0, len(rows))
	for _, row := range rows {
		sensors = append(sensors, thingToSensorWire(row))
	}

	cfg, err := buildConfigWithThingModel(ctx, s.configSvc, deviceID, "sensors", sensors)
	if err != nil {
		return nil, err
	}

	return &entity.SensorApplyResponse{
		DeviceID: cfg.DeviceID,
		Version:  cfg.Version,
		Status:   cfg.Status,
		Count:    len(sensors),
	}, nil
}
