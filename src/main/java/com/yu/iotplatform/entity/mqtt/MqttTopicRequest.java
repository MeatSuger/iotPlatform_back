package com.yu.iotplatform.entity.mqtt;

import jakarta.validation.constraints.NotBlank;

/**
 * 通用主题请求 DTO（用于取消订阅等）
 */
public record MqttTopicRequest(
		@NotBlank(message = "topic 不能为空")
		String topic
) {
}
