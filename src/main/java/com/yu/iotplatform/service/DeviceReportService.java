package com.yu.iotplatform.service;

import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.entity.DeviceStatusDTO;

public interface DeviceReportService {
	ApiResponse<String> reportStatus(String deviceId,
									 String deviceTokenHeader,
									 DeviceStatusDTO statusDTO);

	ApiResponse<?> heartbeat(String deviceId,
							 String deviceTokenHeader);

	/**
	 * 读取设备状态缓存（miss 返回 null）
	 */
	DeviceStatus getDeviceStatus(String deviceId);

	/**
	 * 清除设备状态缓存
	 */
	void evictDeviceStatus(String deviceId);

	/**
	 * 清除传感器查询缓存
	 */
	void evictSensorRecentCache(String deviceId);
}
