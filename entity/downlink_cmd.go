package entity

import (
	"encoding/json"

	"github.com/yu/iot-platform-go/common"
)

// DownlinkCmd 下放命令实体，对应表 iot_downlink_cmd
type DownlinkCmd struct {
	ID        uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID  string          `gorm:"column:device_id;size:50;index;not null" json:"deviceId"`
	Type      string          `gorm:"size:50" json:"type"`                     // 命令类型: config / control / ota / message
	Payload   string          `gorm:"type:text" json:"-"`                      // JSON 载荷（DB 存字符串，API 层用 json.RawMessage）
	Status    string          `gorm:"size:20;default:'pending'" json:"status"` // pending / sent / delivered
	CreatedAt common.DateTime `gorm:"column:created_at" json:"createdAt"`
}

// TableName 指定表名
func (DownlinkCmd) TableName() string {
	return "iot_downlink_cmd"
}

// CmdStatus 命令状态常量
const (
	CmdStatusPending   = "pending"
	CmdStatusSent      = "sent"
	CmdStatusDelivered = "delivered"
)

// DownlinkCmdRequest 下发命令请求（HTTP POST body）
type DownlinkCmdRequest struct {
	Type    string          `json:"type" binding:"required"`    // config / control / ota / message
	Payload json.RawMessage `json:"payload" binding:"required"` // JSON 对象
}

// DownlinkCmdResponse 设备拉取命令响应
type DownlinkCmdResponse struct {
	ID        uint            `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"` // JSON 对象
	CreatedAt common.DateTime `json:"createdAt"`
}
