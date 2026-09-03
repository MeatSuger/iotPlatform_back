package entity

import "time"

// DeviceConfig 设备配置快照（DTO，对应 ent.DeviceConfig 对外暴露）
type DeviceConfig struct {
	ID              uint      `json:"id"`
	DeviceID        string    `json:"deviceId"`
	Version         uint      `json:"version"`
	Payload         string    `json:"payload"`
	Status          string    `json:"status"`
	ReportedVersion uint      `json:"reportedVersion"`
	ReportedPayload string    `json:"reportedPayload"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// ConfigEnvelope 下发到设备端的配置命令载荷
//
// 复用现有下行命令通道：EnqueueCmd(type="config", payload=ConfigEnvelope)
type ConfigEnvelope struct {
	Version uint           `json:"version"`
	Config  map[string]any `json:"config"`
}

// DeviceConfigReport 设备配置回执（设备 → 平台）
type DeviceConfigReport struct {
	Version uint           `json:"version" binding:"required"`
	Config  map[string]any `json:"config"`
}

// DeviceConfigSaveRequest 平台设置配置请求
type DeviceConfigSaveRequest struct {
	Config map[string]any `json:"config" binding:"required"`
}

// DefaultDeviceConfig 返回一份默认协议配置样例（sensor/actuator/camera/network/ota），
// 作为协议基线文档与测试夹具。设备端与平台据此字段结构约定 JSON 协议。
func DefaultDeviceConfig() map[string]any {
	return map[string]any{
		"network": map[string]any{
			"wifi": map[string]any{
				"ssid":     "",
				"password": "",
			},
			"mqtt": map[string]any{
				"host": "",
				"port": 1883,
				"tls":  false,
			},
		},
		"sensor": map[string]any{
			"reportInterval": 60,
			"thresholds": map[string]any{
				"temperature": map[string]any{"min": 0, "max": 100},
			},
		},
		"actuator": map[string]any{
			"mode": "auto",
			"schedule": map[string]any{
				"on":  "08:00",
				"off": "20:00",
			},
			"pwm": 512,
		},
		"camera": map[string]any{
			"protocol": "smtp",
			"smtp": map[string]any{
				"host":     "smtp.example.com",
				"port":     465,
				"ssl":      true,
				"username": "",
				"password": "",
			},
			"snapshotInterval": 30,
		},
		// OTA 升级扩展点（本期仅预留结构，不实现升级流程）
		"ota": map[string]any{
			"fwUrl":     "",
			"fwVersion": "",
			"md5":       "",
		},
	}
}
