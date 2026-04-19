package com.yu.iotplatform.Util;

import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;

import java.time.LocalDateTime;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest
@ActiveProfiles("prod")
class DeviceUtilRedisTest {

    @Test
    void cacheDevice_and_getCachedDevice_shouldWork() {
        String deviceId = DeviceUtil.generateShortDeviceId();
        Device device = new Device();
        device.setDeviceId(deviceId);
        device.setDeviceName("test-device");
        device.setOwnerId(100L);
        device.setStatus("ACTIVE");

        DeviceUtil.cacheDevice(device, 60, TimeUnit.SECONDS);
        Device cached = DeviceUtil.getCachedDevice(deviceId);

        assertNotNull(cached);
        assertEquals("test-device", cached.getDeviceName());
        assertEquals(100L, cached.getOwnerId());

        assertTrue(DeviceUtil.clearDeviceCache(deviceId));
        assertNull(DeviceUtil.getCachedDevice(deviceId));
    }

    @Test
    void cacheDeviceStatus_and_clear_shouldWork() {
        String deviceId = DeviceUtil.generateShortDeviceId();
        DeviceStatus status = new DeviceStatus();
        status.setDeviceId(deviceId);
        status.setStatus(DeviceUtil.DEVICE_ONLINE_STATUS);
        status.setLastActiveTime(LocalDateTime.now());

        DeviceUtil.cacheDeviceStatus(status, 60, TimeUnit.SECONDS);
        DeviceStatus cached = DeviceUtil.getCachedDeviceStatus(deviceId);

        assertNotNull(cached);
        assertEquals(DeviceUtil.DEVICE_ONLINE_STATUS, cached.getStatus());
        assertNotNull(cached.getLastActiveTime());

        assertTrue(DeviceUtil.clearDeviceStatusCache(deviceId));
        assertNull(DeviceUtil.getCachedDeviceStatus(deviceId));
    }

    @Test
    void clearRecentSensorCache_shouldDeleteAllPatternKeys() {
        String deviceId = DeviceUtil.generateShortDeviceId();
        String k1 = DeviceUtil.sensorRecentCacheKey(deviceId, 10);
        String k2 = DeviceUtil.sensorRecentCacheKey(deviceId, 20);
        String isolateKey = DeviceUtil.sensorRecentCacheKey(DeviceUtil.generateShortDeviceId(), 10);

        RedisUtil.StringOps.set(k1, "v1");
        RedisUtil.StringOps.set(k2, "v2");
        RedisUtil.StringOps.set(isolateKey, "v3");

        long deleted = DeviceUtil.clearRecentSensorCache(deviceId);
        assertTrue(deleted >= 2);
        assertNull(RedisUtil.StringOps.get(k1));
        assertNull(RedisUtil.StringOps.get(k2));
        assertEquals("v3", RedisUtil.StringOps.get(isolateKey));

        RedisUtil.KeyOps.delete(isolateKey);
    }

}
