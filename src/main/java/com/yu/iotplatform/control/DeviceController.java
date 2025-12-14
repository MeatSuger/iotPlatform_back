package com.yu.iotplatform.control;

import cn.dev33.satoken.annotation.SaIgnore;
import cn.dev33.satoken.stp.StpUtil;
import com.alibaba.fastjson2.JSON;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.service.DeviceService;
import jakarta.annotation.Resource;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.util.List;

import static com.yu.iotplatform.control.UserController.USER_STATUS_ACTIVE;

@RestController
@RequestMapping("/device")
public class DeviceController {

    @Resource
    private DeviceService deviceService;

    @Resource
    private RedisTemplate<String, Object> redisTemplate;

    public static final String DEVICE_CACHE_KEY = "iot:device:";
    private static final String DEVICE_STATUS_KEY = "iot:device:status:";

    // ----------------- 设备注册 -----------------
    @PostMapping("/register")
    public ApiResponse<?> registerDevice(@RequestBody Device device) {
        if (device.getDeviceName().isBlank()) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "devicesName不能为空");
        }
        if (deviceService.lambdaQuery().eq(Device::getDeviceName, device.getDeviceName()).count() > 0) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "devicesName重复");
        }
        device.setDeviceId(deviceService.generateDeviceId());
        // 检查设备是否已存在
        Device exist = deviceService.lambdaQuery().eq(Device::getDeviceId, device.getDeviceId()).one();
        if (exist != null) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "设备已存在");
        }
        // 当前登录用户 ID 作为设备 owner
        device.setOwnerId(StpUtil.getLoginIdAsLong());
        device.setStatus(USER_STATUS_ACTIVE);
        deviceService.save(device);
        redisTemplate.opsForValue().set(DEVICE_CACHE_KEY + device.getDeviceId(), JSON.toJSONString(device));
        return ApiResponse.success(device);
    }


    // ----------------- 获取设备列表 -----------------
    @GetMapping("/list")
    public ApiResponse<?> listDevices() {
        Long ownerId = StpUtil.getLoginIdAsLong();
        List<Device> devices = deviceService.lambdaQuery().eq(Device::getOwnerId, ownerId).orderByDesc(Device::getId).list();
        return ApiResponse.success(devices);
    }

    // ----------------- 获取设备详情 -----------------
    @GetMapping("/{deviceId}/Data")
    public ApiResponse<DeviceStatus> getDeviceDetail(@PathVariable String deviceId) {
        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) return ApiResponse.fail(404, "未找到设备");

        String statusJson = (String) redisTemplate.opsForHash().get(DEVICE_CACHE_KEY, deviceId);
        DeviceStatus status = statusJson == null ? new DeviceStatus() : JSON.parseObject(statusJson, DeviceStatus.class);

        // 补充设备基本信息
        status.setId(device.getId());
        status.setDeviceId(device.getDeviceId());
        status.setOwnerId(device.getOwnerId());
        status.setStatus(device.getStatus());

        return ApiResponse.success(status);
    }

    //--获取设备token--
    @GetMapping("/{deviceId}/token")
    public ApiResponse<?> getDevicesToken(@PathVariable String deviceId) {
        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) return ApiResponse.fail(404, "未找到设备");
        if (!device.getOwnerId().equals(StpUtil.getLoginIdAsLong())) return ApiResponse.fail(404, "设备id未找到");
        return ApiResponse.success(StpUtil.getTokenValue());
    }

    // ----------------- 删除设备 -----------------
    @PostMapping("/{deviceId}/delete")
    public ApiResponse<?> deleteDevice(@PathVariable String deviceId) {
        boolean removed = deviceService.remove(new QueryWrapper<Device>().eq("device_id", deviceId));
        // 删除 Redis 缓存
        boolean cacheDeleted = redisTemplate.delete(DEVICE_CACHE_KEY + deviceId);
        redisTemplate.opsForValue().set(DEVICE_CACHE_KEY + deviceId, JSON.toJSONString(cacheDeleted));

        if (removed && cacheDeleted) {
            return ApiResponse.success("删除成功");
        } else {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "删除目标不存在或错误");
        }
    }
}