package com.yu.iotplatform.service;

import com.baomidou.mybatisplus.extension.service.IService;
import com.yu.iotplatform.entity.Device;

public interface DeviceService extends IService<Device> {
    Device getDeviceById(String deviceId);
    String generateDeviceId();

    /**
     * 注册后缓存设备
     */
    void cacheDevice(Device device);

    /** 删除时清除设备缓存 */
    void evictDeviceCache(String deviceId);
}
