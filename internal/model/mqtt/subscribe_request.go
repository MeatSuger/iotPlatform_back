package mqtt

// SubscribeRequest MQTT订阅请求
type SubscribeRequest struct {
	Topic string `json:"topic" binding:"required"`
	Qos   *int   `json:"qos" binding:"omitempty,min=0,max=2"` // 可选，默认0
}

// GetQos 获取QoS值，若未设置则返回默认值0
func (r *SubscribeRequest) GetQos() byte {
	if r.Qos != nil {
		return byte(*r.Qos)
	}
	return 0
}
