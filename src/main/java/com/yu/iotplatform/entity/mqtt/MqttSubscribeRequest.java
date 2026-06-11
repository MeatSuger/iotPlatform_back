package com.yu.iotplatform.entity.mqtt;

import lombok.Data;

@Data
public class MqttSubscribeRequest {
	private String topic;
	private Integer qos;
}
