package com.yu.iotplatform.entity.mqtt;

import lombok.Builder;
import lombok.Data;

import java.util.Map;

@Data
@Builder
public class MqttClientStatus {
	private boolean connected;
	private String brokerUrl;
	private String clientId;
	private Map<String, Integer> subscriptions;
	private int bufferedMessages;
}
