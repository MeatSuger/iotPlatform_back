package com.yu.iotplatform.entity.mqtt;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;
import org.hibernate.validator.constraints.Range;

/**
 * MQTT 发布请求 DTO（不可变）
 */
public record MqttPublishRequest(
		@NotBlank(message = "topic 不能为空")
		String topic,

		@Range(min = 0, max = 2, message = "QoS 必须是 0,1,2")
		Integer qos,      // 允许 null，业务层默认 0

		@NotNull(message = "payload 不能为空")
		String payload,

		Boolean retained  // 允许 null，业务层默认 false
) {
}