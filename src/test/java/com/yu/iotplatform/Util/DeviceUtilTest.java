package com.yu.iotplatform.Util;

import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import org.junit.jupiter.api.Test;

import java.time.LocalDateTime;

import static org.junit.jupiter.api.Assertions.*;

class DeviceUtilTest {

	@Test
	void normalizeDeviceId_shouldTrimAndLowercase() {
		String result = DeviceUtil.normalizeDeviceId("  AbC123  ");
		assertEquals("abc123", result);
	}

	@Test
	void isValidDeviceId_shouldValidateHex6() {
		assertTrue(DeviceUtil.isValidDeviceId("A1b2C3"));
		assertTrue(DeviceUtil.isValidDeviceId("  a1B2c3  "));

		assertFalse(DeviceUtil.isValidDeviceId(null));
		assertFalse(DeviceUtil.isValidDeviceId("abc12"));
		assertFalse(DeviceUtil.isValidDeviceId("abc1234"));
		assertFalse(DeviceUtil.isValidDeviceId("abz123"));
		assertFalse(DeviceUtil.isValidDeviceId("12-abc"));
	}

	@Test
	void generateShortDeviceId_shouldBeLowerHexWithLength6() {
		String deviceId = DeviceUtil.generateShortDeviceId();
		assertNotNull(deviceId);
		assertEquals(6, deviceId.length());
		assertTrue(deviceId.matches("^[a-f0-9]{6}$"));
	}

	@Test
	void mergeDeviceWithStatus_shouldCreateWhenStatusNull() {
		Device device = new Device();
		device.setId(1L);
		device.setDeviceId("abc123");
		device.setOwnerId(100L);
		device.setStatus("ACTIVE");
		LocalDateTime lastActiveTime = LocalDateTime.now().minusMinutes(1);
		device.setLastActiveTime(lastActiveTime);

		DeviceStatus merged = DeviceUtil.mergeDeviceWithStatus(device, null);

		assertNotNull(merged);
		assertEquals(1L, merged.getId());
		assertEquals("abc123", merged.getDeviceId());
		assertEquals(100L, merged.getOwnerId());
		assertEquals("ACTIVE", merged.getStatus());
		assertEquals(lastActiveTime, merged.getLastActiveTime());
	}

	@Test
	void mergeDeviceWithStatus_shouldKeepStatusLastActiveWhenPresent() {
		Device device = new Device();
		device.setId(2L);
		device.setDeviceId("def456");
		device.setOwnerId(200L);
		device.setStatus("ONLINE");
		device.setLastActiveTime(LocalDateTime.now().minusHours(2));

		DeviceStatus status = new DeviceStatus();
		LocalDateTime statusTime = LocalDateTime.now().minusMinutes(5);
		status.setLastActiveTime(statusTime);

		DeviceStatus merged = DeviceUtil.mergeDeviceWithStatus(device, status);

		assertSame(status, merged);
		assertEquals(statusTime, merged.getLastActiveTime());
		assertEquals("def456", merged.getDeviceId());
		assertEquals("ONLINE", merged.getStatus());
	}
}
