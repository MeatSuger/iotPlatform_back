package entity

import (
	"github.com/yu/iot-platform-go/common"
)

// MqttPublishLog MQTT发布日志，对应表 mqtt_publish_log
type MqttPublishLog struct {
	ID         uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	Topic      string          `gorm:"column:topic;size:255;not null" json:"topic"`
	Payload    string          `gorm:"column:payload;type:text" json:"payload"`
	Qos        int             `gorm:"column:qos;not null;default:0" json:"qos"`
	Retained   bool            `gorm:"column:retained;not null;default:false" json:"retained"`
	ClientID   string          `gorm:"column:client_id;size:128" json:"clientId"`
	BrokerURL  string          `gorm:"column:broker_url;size:255" json:"brokerUrl"`
	CreateTime common.DateTime `gorm:"column:create_time" json:"createTime"`
}

// TableName 指定表名
func (MqttPublishLog) TableName() string {
	return "mqtt_publish_log"
}
