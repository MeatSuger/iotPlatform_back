package com.yu.iotplatform.entity;

import com.baomidou.mybatisplus.annotation.FieldFill;
import com.baomidou.mybatisplus.annotation.TableField;
import com.fasterxml.jackson.annotation.JsonFormat;
import lombok.Data;

import java.time.LocalDateTime;
import java.util.List;

// ----------------- 统一设备状态对象 -----------------
@Data
public  class DeviceStatus {
    private Long id;                    // 设备主键ID
    private String deviceId;            // 设备编号
    private Long ownerId;               // 所有者ID
    private String status;              // 设备状态
    @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss", timezone = "GMT+8")
    @TableField(fill = FieldFill.INSERT_UPDATE)
    private LocalDateTime lastActiveTime; // 最后活跃时间
    private List<SensorData> sensors;   // 传感器数据列表
}
