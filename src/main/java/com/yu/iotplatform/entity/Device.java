package com.yu.iotplatform.entity;

import com.baomidou.mybatisplus.annotation.*;
import com.fasterxml.jackson.annotation.JsonFormat;
import lombok.Data;

import java.time.LocalDateTime;

@Data
@TableName("`iot_device`")
public class Device {

    @TableId(type = IdType.AUTO)
    private Long id;

    @TableField("device_id")
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

    @TableField(value = "created_at",fill = FieldFill.INSERT)
    @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss")
    private LocalDateTime createdAt;

    @TableField(value = "updated_at", fill = FieldFill.UPDATE)
    @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss")
    private LocalDateTime updatedAt;
}
