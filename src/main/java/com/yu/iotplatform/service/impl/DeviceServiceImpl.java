package com.yu.iotplatform.service.impl;

import com.alibaba.fastjson2.JSON;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.yu.iotplatform.Util.RedisUtil;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.mapper.DeviceMapper;
import com.yu.iotplatform.service.DeviceService;
import org.springframework.stereotype.Service;

import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.UUID;
import java.util.concurrent.TimeUnit;

import static com.yu.iotplatform.control.DeviceController.DEVICE_CACHE_KEY;

@Service
public class DeviceServiceImpl extends ServiceImpl<DeviceMapper, Device> implements DeviceService {

    @Override
    public Device getDeviceById(String deviceId) {
        String key = DEVICE_CACHE_KEY + deviceId;

        // 1. 从缓存读取
        String cached = RedisUtil.StringOps.get(key);
        if (cached != null) {
            return JSON.parseObject(cached, Device.class);
        }

        // 2. 数据库读取（按 device_id 查询，避免把 String 传给主键 id）
        Device device = this.lambdaQuery()
            .eq(Device::getDeviceId, deviceId)
            .one();
        if (device != null) {
            // 写回缓存，设置过期时间
            RedisUtil.StringOps.setEx(key, JSON.toJSONString(device), 10, TimeUnit.MINUTES);
        } else {
            // 数据库没找到 → 设备已删除，直接返回 null
            return null;
        }

        return device;
    }


    @Override
    public String generateDeviceId() {
        String uuid = UUID.randomUUID().toString();
        String hash = md5Hash(uuid);
        return hash.substring(0, 6);
    }

    private String md5Hash(String input) {
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

}
