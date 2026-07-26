package entity

import (
	"time"

	"iot-platform.local/pkg/common"
)

// SensorData 传感器数据（DTO）
type SensorData struct {
	Name      string    `json:"name" binding:"required"`
	Type      string    `json:"type" binding:"required"`
	Value     any       `json:"value" binding:"required"`
	Timestamp time.Time `json:"timestamp"`
}

// DeviceStatus 设备状态（DTO）
type DeviceStatus struct {
	ID             int             `json:"id"`
	DeviceID       string          `json:"deviceId"`
	OwnerID        uint            `json:"ownerId"`
	Status         string          `json:"status"`
	LastActiveTime common.DateTime `json:"lastActiveTime"`
	Sensors        []SensorData    `json:"sensors"`
}

// DeviceStatusDTO 设备状态上报请求（DTO）
type DeviceStatusDTO struct {
	Sensors []SensorData `json:"sensors" binding:"required"`
}
