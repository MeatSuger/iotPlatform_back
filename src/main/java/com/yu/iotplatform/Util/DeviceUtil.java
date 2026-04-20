package com.yu.iotplatform.Util;

import cn.dev33.satoken.stp.StpLogic;
import com.alibaba.fastjson2.JSON;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.LocalDateTime;
import java.util.Locale;
import java.util.Objects;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.TimeUnit;
import java.util.regex.Pattern;

public class DeviceUtil {

	private DeviceUtil() {
		throw new IllegalStateException("Utility class");
	}

	/** 设备基础缓存 key 前缀 */
	public static final String DEVICE_CACHE_KEY_PREFIX = "iot:device:";

	/** 设备状态缓存 key 前缀 */
	public static final String DEVICE_STATUS_KEY_PREFIX = "iot:device:status:";

	/** Sa-Token 设备登录体系（与用户体系隔离） */
	public static final StpLogic DEVICE_STP = new StpLogic("device");

	/** 设备最近传感器缓存 key 前缀 */
	public static final String SENSOR_RECENT_CACHE_PREFIX = "iot:cache:sensor:recent:";

	/** 默认设备缓存时长（分钟） */
	public static final long DEFAULT_DEVICE_CACHE_MINUTES = 10L;

	/** 默认设备状态缓存时长（秒） */
	public static final long DEFAULT_DEVICE_STATUS_CACHE_SECONDS = 120L;

	/** 设备在线状态值 */
	public static final String DEVICE_ONLINE_STATUS = "ONLINE";

	/** 心跳建议间隔（秒） */
	public static final long HEARTBEAT_INTERVAL_SECONDS = 60L;

	/** 设备ID规则：6位十六进制（兼容大小写） */
	private static final Pattern DEVICE_ID_PATTERN = Pattern.compile("^[a-fA-F0-9]{6}$");

	/**
	 * 规范化设备ID（去空白 + 小写）
	 */
	public static String normalizeDeviceId(String deviceId) {
		if (deviceId == null) {
			return null;
		}
		return deviceId.trim().toLowerCase(Locale.ROOT);
	}

	/**
	 * 判断设备ID是否符合规则
	 */
	public static boolean isValidDeviceId(String deviceId) {
		String normalized = normalizeDeviceId(deviceId);
		return normalized != null && DEVICE_ID_PATTERN.matcher(normalized).matches();
	}

	/**
	 * 生成简短设备ID（6位十六进制）
	 */
	public static String generateShortDeviceId() {
		String uuid = UUID.randomUUID().toString();
		String hash = md5Hash(uuid);
		return hash.substring(0, 6).toLowerCase(Locale.ROOT);
	}

	private static String md5Hash(String input) {
		try {
			MessageDigest md = MessageDigest.getInstance("MD5");
			byte[] hash = md.digest(input.getBytes());

			StringBuilder hexString = new StringBuilder();
			for (byte b : hash) {
				String hex = Integer.toHexString(0xff & b);
				if (hex.length() == 1) hexString.append('0');
				hexString.append(hex);
			}
			return hexString.toString();
		} catch (NoSuchAlgorithmException e) {
			throw new RuntimeException("MD5 algorithm not found", e);
		}
	}

	/**
	 * 设备缓存key
	 */
	public static String deviceCacheKey(String deviceId) {
		return DEVICE_CACHE_KEY_PREFIX + normalizeDeviceId(deviceId);
	}

	/**
	 * 设备状态缓存key
	 */
	public static String deviceStatusKey(String deviceId) {
		return DEVICE_STATUS_KEY_PREFIX + normalizeDeviceId(deviceId);
	}

	/**
	 * 设备最近传感器缓存key
	 */
	public static String sensorRecentCacheKey(String deviceId, int limit) {
		return SENSOR_RECENT_CACHE_PREFIX + normalizeDeviceId(deviceId) + ":" + limit;
	}

	/**
	 * 获取设备 token
	 */
	public static String getDeviceToken(String deviceId) {
		String normalizedDeviceId = normalizeDeviceId(deviceId);
		if (normalizedDeviceId == null) {
			return null;
		}
		return DEVICE_STP.getTokenValueByLoginId(normalizedDeviceId);
	}

	/**
	 * 获取设备 token（不存在则自动生成）
	 */
	public static String getOrCreateDeviceToken(String deviceId) {
		String token = getDeviceToken(deviceId);
		if (token != null && !token.isBlank()) {
			return token;
		}
		String normalizedDeviceId = normalizeDeviceId(deviceId);
		DEVICE_STP.login(normalizedDeviceId);
		return DEVICE_STP.getTokenValueByLoginId(normalizedDeviceId);
	}

	/**
	 * 缓存设备（默认 10 分钟）
	 */
	public static void cacheDevice(Device device) {
		cacheDevice(device, DEFAULT_DEVICE_CACHE_MINUTES, TimeUnit.MINUTES);
	}

	/**
	 * 缓存设备（可指定过期时间）
	 */
	public static void cacheDevice(Device device, long timeout, TimeUnit unit) {
		if (device == null || device.getDeviceId() == null) {
			return;
		}
		String key = deviceCacheKey(device.getDeviceId());
		if (timeout > 0 && unit != null) {
			RedisUtil.StringOps.setEx(key, JSON.toJSONString(device), timeout, unit);
		} else {
			RedisUtil.StringOps.set(key, JSON.toJSONString(device));
		}
	}

	/**
	 * 读取设备缓存
	 */
	public static Device getCachedDevice(String deviceId) {
		String cache = RedisUtil.StringOps.get(deviceCacheKey(deviceId));
		if (cache == null || cache.isBlank()) {
			return null;
		}
		return JSON.parseObject(cache, Device.class);
	}

	/**
	 * 删除设备缓存
	 */
	public static boolean clearDeviceCache(String deviceId) {
		return RedisUtil.KeyOps.delete(deviceCacheKey(deviceId));
	}

	/**
	 * 缓存设备状态（默认 120 秒）
	 */
	public static void cacheDeviceStatus(DeviceStatus status) {
		cacheDeviceStatus(status, DEFAULT_DEVICE_STATUS_CACHE_SECONDS, TimeUnit.SECONDS);
	}

	/**
	 * 缓存设备状态（可指定过期时间）
	 */
	public static void cacheDeviceStatus(DeviceStatus status, long timeout, TimeUnit unit) {
		if (status == null || status.getDeviceId() == null) {
			return;
		}
		if (status.getLastActiveTime() == null) {
			status.setLastActiveTime(LocalDateTime.now());
		}
		String key = deviceStatusKey(status.getDeviceId());
		if (timeout > 0 && unit != null) {
			RedisUtil.StringOps.setEx(key, JSON.toJSONString(status), timeout, unit);
		} else {
			RedisUtil.StringOps.set(key, JSON.toJSONString(status));
		}
	}

	/**
	 * 读取设备状态缓存
	 */
	public static DeviceStatus getCachedDeviceStatus(String deviceId) {
		String cache = RedisUtil.StringOps.get(deviceStatusKey(deviceId));
		if (cache == null || cache.isBlank()) {
			return null;
		}
		return JSON.parseObject(cache, DeviceStatus.class);
	}

	/**
	 * 删除设备状态缓存
	 */
	public static boolean clearDeviceStatusCache(String deviceId) {
		return RedisUtil.KeyOps.delete(deviceStatusKey(deviceId));
	}

	/**
	 * 删除设备 token 缓存
	 */
	public static boolean clearDeviceTokenCache(String deviceId) {
		String normalizedDeviceId = normalizeDeviceId(deviceId);
		if (normalizedDeviceId == null) {
			return false;
		}
		DEVICE_STP.logout(normalizedDeviceId);
		return true;
	}

	/**
	 * 清理某设备的最近传感器缓存
	 */
	public static long clearRecentSensorCache(String deviceId) {
		Set<String> keys = RedisUtil.KeyOps.keys(SENSOR_RECENT_CACHE_PREFIX + normalizeDeviceId(deviceId) + ":*");
		if (keys == null || keys.isEmpty()) {
			return 0L;
		}
		return RedisUtil.KeyOps.delete(keys);
	}

	/**
	 * 将设备基础信息合并到状态对象中
	 */
	public static DeviceStatus mergeDeviceWithStatus(Device device, DeviceStatus status) {
		if (device == null) {
			return null;
		}
		DeviceStatus result = Objects.requireNonNullElseGet(status, DeviceStatus::new);
		result.setId(device.getId());
		result.setDeviceId(device.getDeviceId());
		result.setOwnerId(device.getOwnerId());
		result.setStatus(device.getStatus());
		if (result.getLastActiveTime() == null) {
			result.setLastActiveTime(device.getLastActiveTime());
		}
		return result;
	}

	/**
	 * 标记设备在线（心跳或上报数据时调用）
	 */
	public static DeviceStatus markDeviceOnline(String deviceId) {
		DeviceStatus status = getCachedDeviceStatus(deviceId);
		if (status == null) {
			status = new DeviceStatus();
			status.setDeviceId(normalizeDeviceId(deviceId));
		}
		status.setStatus(DEVICE_ONLINE_STATUS);
		status.setLastActiveTime(LocalDateTime.now());
		cacheDeviceStatus(status);
		return status;
	}
}
