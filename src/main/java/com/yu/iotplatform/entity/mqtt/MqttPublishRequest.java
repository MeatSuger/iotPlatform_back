package com.yu.iotplatform.entity.mqtt;

import lombok.Data;

@Data
public class MqttPublishRequest {
	private String topic;
	private String payload;
	private Integer qos;
	private Boolean retained;
}
