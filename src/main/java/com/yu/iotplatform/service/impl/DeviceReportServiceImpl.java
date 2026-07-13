package com.yu.iotplatform.service.impl;

import cn.dev33.satoken.exception.NotLoginException;
import com.yu.iotplatform.Util.DeviceUtil;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.config.CacheConfig;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.entity.DeviceStatusDTO;
import com.yu.iotplatform.service.DeviceReportService;
import com.yu.iotplatform.service.DeviceService;
import com.yu.iotplatform.service.InfluxDBService;
import jakarta.annotation.Resource;
import org.springframework.cache.annotation.CacheEvict;
import org.springframework.cache.annotation.CachePut;
import org.springframework.cache.annotation.Cacheable;
import org.springframework.data.redis.core.Cursor;
import org.springframework.data.redis.core.ScanOptions;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;

import java.time.OffsetDateTime;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

@Service
public class DeviceReportServiceImpl implements DeviceReportService {
	@Resource
	private InfluxDBService influxDBService;
	@Resource
	private DeviceService deviceService;
	@Resource
	private StringRedisTemplate stringRedisTemplate;

	@Override
	public ApiResponse<String> reportStatus(String deviceId, String deviceTokenHeader, DeviceStatusDTO statusDTO) {
		Device device = deviceService.getDeviceById(deviceId);
		if (device == null) {
			return ApiResponse.fail(HttpStatus.NOT_FOUND.value(), "设备不存在");
		}

		// 设备必须属于某个用户，否则 token 不生效
		if (device.getOwnerId() == null) {
			return ApiResponse.fail(HttpStatus.UNAUTHORIZED.value(), "设备未绑定用户，token无效");
		}

		if (!isValidDeviceToken(deviceId, deviceTokenHeader)) {
			return ApiResponse.fail(HttpStatus.UNAUTHORIZED.value(), "设备token无效");
		}

		OffsetDateTime now = OffsetDateTime.now();

		DeviceStatus status = new DeviceStatus();
		status.setDeviceId(deviceId);
		status.setStatus(DeviceUtil.DEVICE_ONLINE_STATUS);
		status.setLastActiveTime(now);

		boolean hasSensors = statusDTO != null && statusDTO.getSensors() != null && !statusDTO.getSensors().isEmpty();
		if (hasSensors) {
			status.setSensors(statusDTO.getSensors());
			influxDBService.writeDeviceSensersAsync(deviceId, statusDTO.getSensors());
			evictSensorRecentCache(deviceId);
		}

		putDeviceStatus(status);
		return ApiResponse.success("状态上报已接收", now.toString());
	}

	@Override
	public ApiResponse<?> heartbeat(String deviceId, String deviceTokenHeader) {
		ApiResponse<String> result = reportStatus(deviceId, deviceTokenHeader, null);
		if (result.getCode() != HttpStatus.OK.value()) {
			return result;
		}
		return ApiResponse.success(Map.of("serverTime", result.getData(), "nextInterval", DeviceUtil.HEARTBEAT_INTERVAL_SECONDS));
	}

	/**
	 * 写入设备状态缓存
	 */
	@CachePut(value = CacheConfig.CACHE_DEVICE_STATUS, key = "#status.deviceId", unless = "#result == null")
	public DeviceStatus putDeviceStatus(DeviceStatus status) {
		return status;
	}

	/**
	 * 读取设备状态缓存（miss 时返回 null，不缓存 null）
	 */
	@Cacheable(value = CacheConfig.CACHE_DEVICE_STATUS, key = "#deviceId", unless = "#result == null")
	public DeviceStatus getDeviceStatus(String deviceId) {
		return null;
	}

	/**
	 * 清除设备状态缓存
	 */
	@CacheEvict(value = CacheConfig.CACHE_DEVICE_STATUS, key = "#deviceId")
	public void evictDeviceStatus(String deviceId) {
		// 注解自动处理
	}

	/**
	 * 清除传感器查询缓存（pattern 删除，需手动 SCAN）
	 */
	public void evictSensorRecentCache(String deviceId) {
		Set<String> keys = new HashSet<>();
		String pattern = CacheConfig.CACHE_SENSOR_RECENT + "::" + DeviceUtil.normalizeDeviceId(deviceId) + ":*";
		ScanOptions options = ScanOptions.scanOptions().match(pattern).count(100).build();
		try (Cursor<String> cursor = stringRedisTemplate.scan(options)) {
			cursor.forEachRemaining(keys::add);
		}
		if (!keys.isEmpty()) {
			stringRedisTemplate.delete(keys);
		}
	}

	private boolean isValidDeviceToken(String deviceId, String token) {
		if (deviceId == null || deviceId.isBlank() || token == null || token.isBlank()) {
			return false;
		}
		try {
			Object loginId = DeviceUtil.getDeviceStp().getLoginIdByToken(token);
			String tokenDeviceId = DeviceUtil.normalizeDeviceId(String.valueOf(loginId));
			return DeviceUtil.normalizeDeviceId(deviceId).equals(tokenDeviceId);
		} catch (NotLoginException e) {
			return false;
		}
	}
}
