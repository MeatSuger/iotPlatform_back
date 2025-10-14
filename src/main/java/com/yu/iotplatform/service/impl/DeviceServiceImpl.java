package com.yu.iotplatform.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.mapper.DeviceMapper;
import com.yu.iotplatform.service.DeviceService;
import jakarta.annotation.Resource;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.stereotype.Service;

import java.util.concurrent.TimeUnit;

@Service
public class DeviceServiceImpl extends ServiceImpl<DeviceMapper, Device> implements DeviceService {

    @Resource
    private RedisTemplate<String, Object> redisTemplate;

    private static final String DEVICE_CACHE_KEY = "iot:device:";

    @Override
    public Device getDeviceById(Long id) {
        String key = DEVICE_CACHE_KEY + id;

        // 1. 尝试从 Redis 读取
        Device cached = (Device) redisTemplate.opsForValue().get(key);
        if (cached != null) {
            return cached;
        }

        // 2. 查数据库
        Device device = this.getById(id);
        if (device != null) {
            redisTemplate.opsForValue().set(key, device, 5, TimeUnit.MINUTES);
        }
        return device;
    }
}
