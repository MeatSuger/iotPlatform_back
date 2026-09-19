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
	"fmt"

	"go.uber.org/zap"

	"iot-platform.local/internal/ent"
	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"
)

// MqttPublisher MQTT 下行发布器（MQTT 网关未启用时为 nil，仅走 HTTP/WS/队列通道）
type MqttPublisher interface {
	// PublishConfig 发布配置快照到 retained 主题 iot/{deviceId}/config
	PublishConfig(deviceID string, env entity.ConfigEnvelope) error
	// PublishCommand 实时下行命令到 iot/{deviceId}/cmd（config 类型除外，由 retained 专管）
	PublishCommand(deviceID string, payload []byte) error
}

// DeviceConfigService 设备配置服务：配置快照持久化 + 复用下行命令队列下发 + MQTT retained 发布
type DeviceConfigService struct {
	configRepo  *repository.DeviceConfigRepo
	downlinkSvc *DownlinkService
	mqttPub     MqttPublisher
}

// NewDeviceConfigService 创建设备配置服务
// mqttPub 可为 nil（MQTT 网关未启用时只走 HTTP/WS/命令队列通道）
func NewDeviceConfigService(configRepo *repository.DeviceConfigRepo, downlinkSvc *DownlinkService, mqttPub MqttPublisher) *DeviceConfigService {
	return &DeviceConfigService{
		configRepo:  configRepo,
		downlinkSvc: downlinkSvc,
		mqttPub:     mqttPub,
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

// Save 整体设置设备配置：version 原子递增落库并复用下行命令队列（type=config）下发。
//
// 下发失败时返回错误（配置已持久化，status=pending，设备可经 GET /config 兜底拉取）。
func (s *DeviceConfigService) Save(ctx context.Context, deviceID string, config map[string]any) (*ent.DeviceConfig, error) {
	payloadBytes, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("序列化配置失败: %w", err)
	}

	// 原子 Upsert：version 在数据库内递增，并发 Save/Apply 不会产生重复版本
	if err := s.configRepo.Upsert(ctx, deviceID, string(payloadBytes)); err != nil {
		return nil, fmt.Errorf("保存配置失败: %w", err)
	}

	// 落库后读取最新版本（原子递增的最终值由数据库决定）
	cfg, err := s.configRepo.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	// 复用下行命令队列下发：在线设备经 WS 实时推送，离线设备下次轮询 /commands 拉取
	envelope := entity.ConfigEnvelope{Version: cfg.Version, Config: config}
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

	// MQTT retained 发布：设备订阅 iot/{deviceId}/config 即拉取到最新版本。
	// 发布失败仅告警（retained 可能丢失），设备仍可经 HTTP GET /config 兜底。
	if s.mqttPub != nil {
		if err := s.mqttPub.PublishConfig(deviceID, envelope); err != nil {
			zap.S().Warnf("[DeviceConfig] MQTT retained 发布失败 [device=%s]: %v", deviceID, err)
		}
	}

	return cfg, nil
}

// Report 设备配置回执：回写实际生效版本与配置，并置状态为已确认
func (s *DeviceConfigService) Report(ctx context.Context, deviceID string, rep entity.DeviceConfigReport) error {
	reportedBytes, err := json.Marshal(rep.Config)
	if err != nil {
		return fmt.Errorf("序列化上报配置失败: %w", err)
	}
	return s.configRepo.UpdateReported(ctx, deviceID, rep.Version, string(reportedBytes))
}
