package mqtt

// PublishRequest MQTT发布请求
type PublishRequest struct {
	Topic    string `json:"topic" binding:"required"`
	Qos      *int   `json:"qos" binding:"omitempty,min=0,max=2"` // 可选，默认0
	Payload  string `json:"payload" binding:"required"`
	Retained *bool  `json:"retained"` // 可选，默认false
}

// GetQos 获取QoS值，若未设置则返回默认值0
func (r *PublishRequest) GetQos() byte {
	if r.Qos != nil {
		return byte(*r.Qos)
	}
	return 0
}

// GetRetained 获取Retained值，若未设置则返回默认值false
func (r *PublishRequest) GetRetained() bool {
	if r.Retained != nil {
		return *r.Retained
	}
	return false
}
