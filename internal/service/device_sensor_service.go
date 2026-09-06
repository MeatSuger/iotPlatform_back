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

// ErrSensorNotFound 传感器定义不存在
var ErrSensorNotFound = errors.New("传感器不存在")

// DeviceSensorService 设备传感器定义服务：物模型 CRUD + Apply 编译进 DeviceConfig 下发
type DeviceSensorService struct {
	sensorRepo *repository.DeviceSensorRepo
	configSvc  *DeviceConfigService
	cache      *cache.RedisCache
}

// NewDeviceSensorService 创建传感器定义服务
func NewDeviceSensorService(sensorRepo *repository.DeviceSensorRepo, configSvc *DeviceConfigService, rcache *cache.RedisCache) *DeviceSensorService {
	return &DeviceSensorService{sensorRepo: sensorRepo, configSvc: configSvc, cache: rcache}
}

// List 查询设备全部传感器定义（整列表缓存：Cache-Aside，写路径显式失效）
func (s *DeviceSensorService) List(ctx context.Context, deviceID string) ([]entity.Sensor, error) {
	out := []entity.Sensor{}
	err := s.cache.GetCachedSensorDefsWithLoader(ctx, deviceID, &out, func(ctx context.Context) (any, error) {
		rows, err := s.sensorRepo.ListByDeviceID(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		defs := make([]entity.Sensor, 0, len(rows))
		for _, row := range rows {
			defs = append(defs, sensorToDTO(row))
		}
		return defs, nil
	})
	return out, err
}

// Get 查询单个传感器定义；不存在返回 (nil, nil)
func (s *DeviceSensorService) Get(ctx context.Context, deviceID, sensorID string) (*entity.Sensor, error) {
	row, err := s.sensorRepo.Get(ctx, deviceID, sensorID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	dto := sensorToDTO(row)
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

	row, err := s.sensorRepo.Create(ctx, &ent.DeviceSensor{
		DeviceID:       deviceID,
		SensorID:       req.ID,
		Name:           req.Name,
		Type:           req.Type,
		DataType:       dataType,
		Unit:           req.Unit,
		Specs:          marshalJSONField(req.Specs),
		ReportInterval: req.ReportInterval,
		Thresholds:     marshalJSONField(req.Thresholds),
		Attrs:          marshalJSONField(req.Attrs),
		Enabled:        enabled,
	})
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, fmt.Errorf("传感器标识符已存在: %s", req.ID)
		}
		return nil, fmt.Errorf("创建传感器失败: %w", err)
	}
	s.evictDefsCache(ctx, deviceID)
	dto := sensorToDTO(row)
	return &dto, nil
}

// Update 增量更新传感器定义；不存在返回 (nil, nil)
func (s *DeviceSensorService) Update(ctx context.Context, deviceID, sensorID string, req entity.SensorUpdateRequest) (*entity.Sensor, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	fields := repository.SensorUpdateFields{
		Name:           req.Name,
		Type:           req.Type,
		DataType:       req.DataType,
		Unit:           req.Unit,
		ReportInterval: req.ReportInterval,
		Enabled:        req.Enabled,
	}
	if req.Specs != nil {
		v := string(*req.Specs)
		fields.Specs = &v
	}
	if req.Thresholds != nil {
		v := string(*req.Thresholds)
		fields.Thresholds = &v
	}
	if req.Attrs != nil {
		v := string(*req.Attrs)
		fields.Attrs = &v
	}

	row, err := s.sensorRepo.Update(ctx, deviceID, sensorID, fields)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	s.evictDefsCache(ctx, deviceID)
	dto := sensorToDTO(row)
	return &dto, nil
}

// Delete 删除传感器定义；不存在时返回 ErrSensorNotFound
func (s *DeviceSensorService) Delete(ctx context.Context, deviceID, sensorID string) error {
	n, err := s.sensorRepo.Delete(ctx, deviceID, sensorID)
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
	rows, err := s.sensorRepo.ListByDeviceID(ctx, deviceID)
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

	sensors := make([]entity.Sensor, 0, len(rows))
	for _, row := range rows {
		sensors = append(sensors, sensorToDTO(row))
	}
	payload["sensors"] = sensors

	cfg, err := s.configSvc.Save(ctx, deviceID, payload)
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

// sensorToDTO ent 实体 → DTO（JSON 文本字段解析为对象，空串输出空对象）
func sensorToDTO(row *ent.DeviceSensor) entity.Sensor {
	return entity.Sensor{
		ID:             row.SensorID,
		Name:           row.Name,
		Type:           row.Type,
		DataType:       row.DataType,
		Unit:           row.Unit,
		Specs:          parseJSONField(row.Specs),
		ReportInterval: row.ReportInterval,
		Thresholds:     parseJSONField(row.Thresholds),
		Attrs:          parseJSONField(row.Attrs),
		Enabled:        row.Enabled,
		CreatedAt:      common.DateTimeFrom(row.CreatedAt),
		UpdatedAt:      common.DateTimeFrom(row.UpdatedAt),
	}
}

// marshalJSONField map → JSON 文本；nil / 空 map 存空串
// 序列化失败时告警并降级为空串（输入均经模型层校验，此处为异常兜底，不应静默）
func marshalJSONField(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		zap.L().Warn("[物模型] JSON 字段序列化失败，落库为空串",
			zap.String("field", "specs/thresholds/attrs/config"), zap.Error(err))
		return ""
	}
	return string(b)
}

// parseJSONField JSON 文本 → map；空串 / 非法时返回空对象（保持响应结构稳定）
func parseJSONField(s string) map[string]any {
	out := map[string]any{}
	if s == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
