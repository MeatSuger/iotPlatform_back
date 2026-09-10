package service

import (
	"context"
	"encoding/json"
	"fmt"

	"iot-platform.local/internal/ent"
)

// buildConfigWithThingModel 读取设备现有配置 payload，写入指定物模型列表（key=sensors/actuators）
// 后版本化保存，供传感器/执行器 Apply 复用。
func buildConfigWithThingModel(ctx context.Context, configSvc *DeviceConfigService, deviceID, key string, items any) (*ent.DeviceConfig, error) {
	payload := map[string]any{}
	if existing, err := configSvc.Get(ctx, deviceID); err != nil {
		return nil, err
	} else if existing != nil && existing.Payload != "" {
		if err := json.Unmarshal([]byte(existing.Payload), &payload); err != nil {
			return nil, fmt.Errorf("解析现有配置失败: %w", err)
		}
	}
	payload[key] = items
	return configSvc.Save(ctx, deviceID, payload)
}
