package com.yu.iotplatform.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.yu.iotplatform.Util.DeviceUtil;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.mapper.DeviceMapper;
import com.yu.iotplatform.service.DeviceService;
import org.springframework.stereotype.Service;

@Service
public class DeviceServiceImpl extends ServiceImpl<DeviceMapper, Device> implements DeviceService {

    @Override
    public Device getDeviceById(String deviceId) {
        // 1. 从缓存读取
        Device cached = DeviceUtil.getCachedDevice(deviceId);
        if (cached != null) {
            return cached;
        }

        // 2. 数据库读取（按 device_id 查询，避免把 String 传给主键 id）
        Device device = this.lambdaQuery()
            .eq(Device::getDeviceId, deviceId)
            .one();
        if (device != null) {
            // 写回缓存，设置过期时间
            DeviceUtil.cacheDevice(device);
        } else {
            // 数据库没找到 → 设备已删除，直接返回 null
            return null;
        }

        return device;
    }


    @Override
    public String generateDeviceId() {
        return DeviceUtil.generateShortDeviceId();
    }

}
