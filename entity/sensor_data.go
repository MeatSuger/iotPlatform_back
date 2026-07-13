package entity

import (
	"github.com/yu/iot-platform-go/common"
)

// SensorData 传感器数据（DTO，非数据库实体；Timestamp 由服务器生成，忽略前端传入值）
type SensorData struct {
	Name      string          `json:"name" binding:"required"`
	Type      string          `json:"type" binding:"required"`
	Value     any             `json:"value" binding:"required"`
	Timestamp common.DateTime `json:"timestamp"`
}

// DeviceStatus 设备状态（DTO，包含传感器数据）
type DeviceStatus struct {
	ID             uint            `json:"id"`
	DeviceID       string          `json:"deviceId"`
	OwnerID        uint            `json:"ownerId"`
	Status         string          `json:"status"`
	LastActiveTime common.DateTime `json:"lastActiveTime"`
	Sensors        []SensorData    `json:"sensors"`
}

// DeviceStatusDTO 设备状态上报请求
type DeviceStatusDTO struct {
	Sensors []SensorData `json:"sensors" binding:"required"`
}
