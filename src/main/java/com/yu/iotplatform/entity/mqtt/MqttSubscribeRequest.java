package com.yu.iotplatform.entity.mqtt;

import jakarta.validation.constraints.NotBlank;
import org.hibernate.validator.constraints.Range; /**
 * MQTT 订阅请求 DTO
 */
public record MqttSubscribeRequest(
		@NotBlank(message = "topic 不能为空")
		String topic,

		@Range(min = 0, max = 2, message = "QoS 必须是 0,1,2")
		Integer qos       // 允许 null，默认 0
) {
}
