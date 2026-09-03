package service

import (
	"context"
	"encoding/json"
	"fmt"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
)

// DeviceConfigService 设备配置服务：配置快照持久化 + 复用下行命令队列下发
type DeviceConfigService struct {
	configRepo  *repository.DeviceConfigRepo
	downlinkSvc *DownlinkService
}

// NewDeviceConfigService 创建设备配置服务
func NewDeviceConfigService(configRepo *repository.DeviceConfigRepo, downlinkSvc *DownlinkService) *DeviceConfigService {
	return &DeviceConfigService{
		configRepo:  configRepo,
		downlinkSvc: downlinkSvc,
	}
}

// Get 查询设备配置快照；不存在时返回 (nil, nil)
func (s *DeviceConfigService) Get(ctx context.Context, deviceID string) (*ent.DeviceConfig, error) {
	cfg, err := s.configRepo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return cfg, nil
}

// Save 整体设置设备配置：version+1 落库并复用下行命令队列（type=config）下发。
//
// 下发失败时返回错误（配置已持久化，status=pending，设备可经 GET /config 兜底拉取）。
func (s *DeviceConfigService) Save(ctx context.Context, deviceID string, config map[string]any) (*ent.DeviceConfig, error) {
	payloadBytes, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("序列化配置失败: %w", err)
	}

	existing, err := s.configRepo.GetByDeviceID(ctx, deviceID)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}

	newVersion := uint(1)
	if existing != nil {
		newVersion = existing.Version + 1
	}

	if err := s.configRepo.Upsert(ctx, deviceID, string(payloadBytes), newVersion); err != nil {
		return nil, fmt.Errorf("保存配置失败: %w", err)
	}

	// 复用下行命令队列下发：在线设备经 WS 实时推送，离线设备下次轮询 /commands 拉取
	envelope := entity.ConfigEnvelope{Version: newVersion, Config: config}
	envBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("序列化下发载荷失败: %w", err)
	}
	if _, err := s.downlinkSvc.EnqueueCmd(ctx, deviceID, DownlinkCmdRequest{
		Type:    "config",
		Payload: envBytes,
	}); err != nil {
		return nil, fmt.Errorf("配置已保存但下发失败: %w", err)
	}

	return s.configRepo.GetByDeviceID(ctx, deviceID)
}

// Report 设备配置回执：回写实际生效版本与配置，并置状态为已确认
func (s *DeviceConfigService) Report(ctx context.Context, deviceID string, rep entity.DeviceConfigReport) error {
	reportedBytes, err := json.Marshal(rep.Config)
	if err != nil {
		return fmt.Errorf("序列化上报配置失败: %w", err)
	}
	return s.configRepo.UpdateReported(ctx, deviceID, rep.Version, string(reportedBytes))
}
