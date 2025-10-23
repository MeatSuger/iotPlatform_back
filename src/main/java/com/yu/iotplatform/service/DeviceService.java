package com.yu.iotplatform.service;

import com.baomidou.mybatisplus.extension.service.IService;
import com.yu.iotplatform.entity.Device;

public interface DeviceService extends IService<Device> {
    Device getDeviceById(String deviceId);
    String generateDeviceId();
}
