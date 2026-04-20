package com.yu.iotplatform.entity;

import com.baomidou.mybatisplus.annotation.FieldFill;
import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.annotation.TableId;
import com.baomidou.mybatisplus.annotation.TableName;
import com.fasterxml.jackson.annotation.JsonFormat;
import lombok.Data;

import java.time.LocalDateTime;

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
    @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss")
    private LocalDateTime createTime;
}
