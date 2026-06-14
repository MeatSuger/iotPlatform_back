package com.yu.iotplatform.entity.mqtt;

import com.fasterxml.jackson.annotation.JsonFormat;

import java.time.Instant;
import java.util.Map;

public record MqttClientStatus(boolean connected,
							   String brokerUrl,
							   String clientId,
							   Map<String, Integer> subscriptions,
							   int bufferedMessages,
							   @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss")
							   Instant lastStateChange,
							   long reconnectCount,
							   long totalMessagesSent,
							   long totalMessagesReceived) {
	public MqttClientStatus {
		if (connected) {

			if (brokerUrl == null || brokerUrl.isBlank()) {
				throw new IllegalArgumentException("brokerUrl must not be blank");
			}
			if (clientId == null || clientId.isBlank()) {
				throw new IllegalArgumentException("clientId must not be blank");
			}
		} else {

			if (brokerUrl == null) brokerUrl = "null";
			if (clientId == null) clientId = "null";
		}    // 防御性复制并包装为不可变
		if (subscriptions == null) {
			subscriptions = Map.of();
		} else {
			subscriptions = Map.copyOf(subscriptions);
		}
		if (lastStateChange == null) {
			lastStateChange = Instant.now();
		}
		if (bufferedMessages < 0) {
			bufferedMessages = 0;
		}
	}

	public static MqttClientStatus connected(String brokerUrl, String clientId, Map<String, Integer> subscriptions, int bufferedMessages) {
		return new MqttClientStatus(true, brokerUrl, clientId, subscriptions, bufferedMessages, Instant.now(), 0, 0, 0);
	}

	public static MqttClientStatus disconnected() {
		return new MqttClientStatus(false, null, null, Map.of(), 0, Instant.now(), 0, 0, 0);
	}

	// 辅助方法
	public boolean hasSubscriptions() {
		return !subscriptions.isEmpty();
	}

	public MqttClientStatus withConnected(boolean newConnected) {
		return new MqttClientStatus(newConnected, brokerUrl, clientId, subscriptions, bufferedMessages, Instant.now(), newConnected ? reconnectCount : reconnectCount + 1, totalMessagesSent, totalMessagesReceived);
	}
}