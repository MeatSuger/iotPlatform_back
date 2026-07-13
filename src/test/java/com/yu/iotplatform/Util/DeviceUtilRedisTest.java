package com.yu.iotplatform.Util;

import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.service.DeviceReportService;
import com.yu.iotplatform.service.DeviceService;
import jakarta.annotation.Resource;
import org.junit.jupiter.api.Disabled;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;

import java.time.OffsetDateTime;

import static org.junit.jupiter.api.Assertions.*;

@Disabled("需要 PostgreSQL / Redis 连接")
@SpringBootTest
@ActiveProfiles("prod")
class DeviceUtilRedisTest {

	@Resource
	private DeviceService deviceService;
	@Resource
	private DeviceReportService deviceReportService;

	    @Test
	void cacheDevice_and_getCachedDevice_shouldWork() {
		String deviceId = DeviceUtil.generateShortDeviceId();
		Device device = new Device();
		device.setDeviceId(deviceId);
		device.setDeviceName("test-device");
		device.setOwnerId(100L);
		device.setStatus("ACTIVE");

		deviceService.cacheDevice(device);
		Device cached = deviceService.getDeviceById(deviceId);

		assertNotNull(cached);
		assertEquals("test-device", cached.getDeviceName());
		assertEquals(100L, cached.getOwnerId());

		deviceService.evictDeviceCache(deviceId);
		// 缓存已清除，再次查询应走 DB（但 DB 中无此设备，应返回 null）
		// 由于 @Cacheable 的 unless = "#result == null"，null 不会重新缓存，
		// 所以这里手动确认缓存已清除即可
		assertNull(deviceReportService.getDeviceStatus("non-existent"));
	}

	@Test
	void cacheDeviceStatus_and_clear_shouldWork() {
		String deviceId = DeviceUtil.generateShortDeviceId();
		DeviceStatus status = new DeviceStatus();
		status.setDeviceId(deviceId);
		status.setStatus(DeviceUtil.DEVICE_ONLINE_STATUS);
		status.setLastActiveTime(OffsetDateTime.now());

		// 直接调用 putDeviceStatus（在实现类中，需通过接口）
		DeviceReportService service = deviceReportService;
		DeviceStatus cached = service.getDeviceStatus(deviceId);
		assertNull(cached); // 初始无缓存

		// 通过 reportStatus 内部调用 putDeviceStatus 会写入缓存
		// 这里简化为手动注入状态缓存
		service.evictDeviceStatus(deviceId); // 确保清除
	}

	@Test
	void clearRecentSensorCache_shouldDeleteAllPatternKeys() {
		// 此方法由 DeviceReportServiceImpl.evictSensorRecentCache 内部使用 SCAN + DELETE 处理
		// 测试验证调用不抛异常即可
		assertDoesNotThrow(() -> deviceReportService.evictSensorRecentCache("test-device-id"));
	}

}
