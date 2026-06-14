package com.yu.iotplatform.entity.mqtt;

import com.fasterxml.jackson.annotation.JsonFormat;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.NotNull;

import java.time.LocalDateTime;

/**
 * MQTT 消息视图（不可变）
 *
 * @param topic      主题
 * @param payload    消息体
 * @param qos        服务质量等级 0/1/2
 * @param retained   是否为保留消息
 * @param duplicate  是否为重复投递（QoS1/2 重试）
 * @param receivedAt 接收时间戳
 */
public record MqttMessageView(@NotBlank String topic,
							  @NotNull String payload,
							  int qos,                // 基本类型，默认 0
							  boolean retained,
							  boolean duplicate,
							  @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss")
							  @NotNull LocalDateTime receivedAt) {
	// 紧凑构造器添加校验和默认值
	public MqttMessageView {
		java.util.Objects.requireNonNull(receivedAt, "receivedAt must not be null");
		if (qos < 0 || qos > 2) {
			throw new IllegalArgumentException("QoS must be 0, 1, or 2");
		}
	}

	// 工厂方法：快速创建带当前时间的消息视图
	public static MqttMessageView of(String topic, String payload, int qos, boolean retained, boolean duplicate) {
		return new MqttMessageView(topic, payload, qos, retained, duplicate, LocalDateTime.now());
	}

	// 工厂方法：默认当前时间，duplicate=false
	public static MqttMessageView of(String topic, String payload, int qos, boolean retained) {
		return new MqttMessageView(topic, payload, qos, retained, false, LocalDateTime.now());
	}
}