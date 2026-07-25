package mqtt

import (
	"time"
)

// MessageView MQTT消息视图
type MessageView struct {
	Topic      string    `json:"topic" binding:"required"`
	Payload    string    `json:"payload" binding:"required"`
	Qos        int       `json:"qos" binding:"min=0,max=2"`
	Retained   bool      `json:"retained"`
	Duplicate  bool      `json:"duplicate"`
	ReceivedAt time.Time `json:"receivedAt"`
}

// NewMessageView 创建消息视图
func NewMessageView(topic, payload string, qos int, retained, duplicate bool) MessageView {
	return MessageView{
		Topic:      topic,
		Payload:    payload,
		Qos:        qos,
		Retained:   retained,
		Duplicate:  duplicate,
		ReceivedAt: time.Now(),
	}
}
