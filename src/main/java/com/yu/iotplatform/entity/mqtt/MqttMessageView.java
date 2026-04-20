package com.yu.iotplatform.entity.mqtt;

import lombok.Builder;
import lombok.Data;

import java.time.LocalDateTime;

@Data
@Builder
public class MqttMessageView {
    private String topic;
    private String payload;
    private int qos;
    private boolean retained;
    private boolean duplicate;
    private LocalDateTime receivedAt;
}
