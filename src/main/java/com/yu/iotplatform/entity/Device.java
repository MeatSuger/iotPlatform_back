package com.yu.iotplatform.entity;

import com.baomidou.mybatisplus.annotation.*;
import lombok.Data;

import java.time.LocalDateTime;

@Data
@TableName("iot_device")
public class Device {

    @TableId(type = IdType.AUTO)
    private Long id;

    private String deviceId;
    private String deviceName;
    private String deviceType;
    private String firmwareVersion;
    private String ipAddress;
    private String macAddress;
    private String location;
    private Long ownerId;
    private String status;
    private LocalDateTime lastActiveTime;

    @TableField(fill = FieldFill.INSERT)
    private LocalDateTime createdAt;

    @TableField(fill = FieldFill.INSERT_UPDATE)
    private LocalDateTime updatedAt;
}
