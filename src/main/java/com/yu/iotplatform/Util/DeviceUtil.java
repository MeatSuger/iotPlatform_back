package com.yu.iotplatform.Util;

import cn.dev33.satoken.config.SaTokenConfig;
import cn.dev33.satoken.stp.StpLogic;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Locale;
import java.util.Objects;
import java.util.UUID;
import java.util.regex.Pattern;

/**
 * 设备工具类 —— 纯工具方法，不涉及 Redis 操作。
 * 缓存操作已迁移至 Spring Cache 注解（参见 CacheConfig / DeviceServiceImpl / DeviceReportServiceImpl）。
 */
public class DeviceUtil {

	private DeviceUtil() {
		throw new IllegalStateException("Utility class");
	}

	/**
	 * Sa-Token 设备登录体系（与用户体系隔离）
	 */
	private static final StpLogic DEVICE_STP;

	static {
		DEVICE_STP = new StpLogic("device");
		// token 名称 → 自动 Set-Cookie: X-Device-Token，同时支持从 Header/Cookie 读取
		DEVICE_STP.setConfig(new SaTokenConfig()
				.setTokenName("X-Device-Token")
				.setIsLog(true)
				.setIsShare(true)
				.setTimeout(-1)
				.setTokenStyle("uuid"));
	}

	/**
	 * 获取设备 StpLogic 实例
	 */
	public static StpLogic getDeviceStp() {
		return DEVICE_STP;
	}


	/**
	 * 设备在线状态值
	 */
	public static final String DEVICE_ONLINE_STATUS = "ONLINE";

	/**
	 * 心跳建议间隔（秒）
	 */
	public static final long HEARTBEAT_INTERVAL_SECONDS = 60L;

	/**
	 * 设备ID规则：6位十六进制（兼容大小写）
	 */
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
	 * 将设备基础信息合并到状态对象中（不涉及缓存）
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
}
