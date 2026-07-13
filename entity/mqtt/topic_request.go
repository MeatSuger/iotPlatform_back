package mqtt

// TopicRequest MQTT主题请求（用于取消订阅等操作）
type TopicRequest struct {
	Topic string `json:"topic" binding:"required"`
}
