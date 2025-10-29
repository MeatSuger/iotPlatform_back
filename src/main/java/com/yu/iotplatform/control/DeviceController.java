package com.yu.iotplatform.control;

import cn.dev33.satoken.stp.StpUtil;
import com.alibaba.fastjson2.JSON;
import com.baomidou.mybatisplus.annotation.FieldFill;
import com.baomidou.mybatisplus.annotation.TableField;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.fasterxml.jackson.annotation.JsonFormat;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.service.DeviceService;
import com.yu.iotplatform.service.InfluxDBService;
import jakarta.annotation.Resource;
import lombok.Data;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.time.LocalDateTime;
import java.util.List;

import static com.yu.iotplatform.control.UserController.USER_STATUS_ACTIVE;

@RestController
@RequestMapping("/device")
public class DeviceController {

    @Resource
    private DeviceService deviceService;

    @Resource
    private RedisTemplate<String, Object> redisTemplate;

    @Resource
    private InfluxDBService influxDBService;

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


    // ----------------- 设备状态上报 -----------------
    @PostMapping("/{deviceId}/Data")
    public ApiResponse<String> reportStatus(@PathVariable String deviceId, @RequestBody DeviceStatusDTO statusDTO) {
        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) return ApiResponse.fail(404, "设备不存在");

        DeviceStatus status = new DeviceStatus();
        status.setDeviceId(deviceId);
        status.setSensors(statusDTO.getSensors());
        status.setLastActiveTime(LocalDateTime.now());
        redisTemplate.opsForHash().put(DEVICE_CACHE_KEY, deviceId, JSON.toJSONString(status));
        influxDBService.writeDeviceSensers(deviceId, statusDTO.sensors);
        return ApiResponse.success("状态上报成功", null);
    }

    // ----------------- 获取设备列表 -----------------
    @GetMapping("/list")
    public ApiResponse<?> listDevices() {
        Long ownerId = StpUtil.getLoginIdAsLong();
        List<Device> devices = deviceService.lambdaQuery().eq(Device::getOwnerId, ownerId).list();
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

    // ----------------- 统一设备状态对象 -----------------
    @Data
    public static class DeviceStatus {
        private Long id;                    // 设备主键ID
        private String deviceId;            // 设备编号
        private Long ownerId;               // 所有者ID
        private String status;              // 设备状态
        @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss", timezone = "GMT+8")
        @TableField(fill = FieldFill.INSERT_UPDATE)
        private LocalDateTime lastActiveTime; // 最后活跃时间
        private List<SensorData> sensors;   // 传感器数据列表
    }

    // ----------------- 设备状态上报DTO -----------------
    @Data
    public static class DeviceStatusDTO {
        private List<SensorData> sensors;   // 传感器数据
    }

    // ----------------- 传感器数据 -----------------
    @Data
    public static class SensorData {
        private String name;        // 传感器名称
        private String type;        // 数据类型
        private Object value;       // 传感器数值
        @JsonFormat(pattern = "yyyy-MM-dd HH:mm:ss", timezone = "GMT+8")
        @TableField(fill = FieldFill.INSERT_UPDATE)
        private LocalDateTime timestamp; // 采集时间
    }
}