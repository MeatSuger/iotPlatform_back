package com.yu.iotplatform.entity;

import lombok.Data;

import java.util.List;

// ----------------- 设备状态上报DTO -----------------
@Data
public class DeviceStatusDTO {
	private List<SensorData> sensors;   // 传感器数据
}
