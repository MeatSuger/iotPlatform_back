package entity

import (
	"github.com/yu/iot-platform-go/common"
)

// Device 设备实体，对应表 iot_device
type Device struct {
	ID              uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID        string          `gorm:"column:device_id;size:50;uniqueIndex;not null" json:"deviceId"`
	DeviceName      string          `gorm:"size:100" json:"deviceName"`
	DeviceType      string          `gorm:"size:50" json:"deviceType"`
	FirmwareVersion string          `gorm:"size:50" json:"firmwareVersion"`
	IPAddress       string          `gorm:"size:45" json:"ipAddress"`
	MacAddress      string          `gorm:"size:17" json:"macAddress"`
	Location        string          `gorm:"size:255" json:"location"`
	OwnerID         uint            `json:"ownerId"`
	Status          string          `gorm:"size:50" json:"status"`
	LastActiveTime  common.DateTime `gorm:"column:last_active_time" json:"lastActiveTime"`
	CreatedAt       common.DateTime `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt       common.DateTime `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 指定表名
func (Device) TableName() string {
	return "iot_device"
}

// DeviceStatus 设备状态常量
const (
	DeviceStatusOnline  = "ONLINE"
	DeviceStatusOffline = "OFFLINE"
)
