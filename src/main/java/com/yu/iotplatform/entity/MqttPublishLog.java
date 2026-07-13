package com.yu.iotplatform.entity;

import com.baomidou.mybatisplus.annotation.*;
import com.fasterxml.jackson.annotation.JsonFormat;
import lombok.Data;

import java.time.OffsetDateTime;

@Data
@TableName("\"mqtt_publish_log\"")
public class MqttPublishLog {
	@TableId(type = IdType.AUTO)
	private Long id;

	@TableField("topic")
	private String topic;

	@TableField("payload")
	private String payload;

	@TableField("qos")
	private Integer qos;

	@TableField("retained")
	private Boolean retained;

	@TableField("client_id")
	private String clientId;

	@TableField("broker_url")
	private String brokerUrl;

	@TableField(value = "create_time", fill = FieldFill.INSERT)
	@JsonFormat(pattern = "yyyy-MM-dd HH:mm:ssXXX", timezone = "GMT+8")
	private OffsetDateTime createTime;
}
