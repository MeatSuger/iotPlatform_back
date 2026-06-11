package com.yu.iotplatform.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.yu.iotplatform.Util.DeviceUtil;
import com.yu.iotplatform.config.CacheConfig;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.mapper.DeviceMapper;
import com.yu.iotplatform.service.DeviceService;
import org.springframework.cache.annotation.CacheEvict;
import org.springframework.cache.annotation.CachePut;
import org.springframework.cache.annotation.Cacheable;
import org.springframework.stereotype.Service;

@Service
public class DeviceServiceImpl extends ServiceImpl<DeviceMapper, Device> implements DeviceService {

	@Override
	@Cacheable(value = CacheConfig.CACHE_DEVICE, key = "#deviceId", unless = "#result == null")
	public Device getDeviceById(String deviceId) {
		return this.lambdaQuery()
				.eq(Device::getDeviceId, deviceId)
				.one();
	}

	@Override
	public String generateDeviceId() {
		return DeviceUtil.generateShortDeviceId();
	}

	/**
	 * 注册后缓存设备
	 */
	@CachePut(value = CacheConfig.CACHE_DEVICE, key = "#device.deviceId", unless = "#result == null")
	public void cacheDevice(Device device) {
	}

	/**
	 * 删除时清除设备缓存
	 */
	@CacheEvict(value = CacheConfig.CACHE_DEVICE, key = "#deviceId")
	public void evictDeviceCache(String deviceId) {
		// 注解自动处理
	}
}
