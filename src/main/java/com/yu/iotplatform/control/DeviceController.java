package com.yu.iotplatform.control;

import cn.dev33.satoken.annotation.SaCheckLogin;
import cn.dev33.satoken.stp.StpUtil;
import com.alibaba.fastjson2.JSON;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.service.DeviceService;
import jakarta.annotation.Resource;
import lombok.Data;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.time.LocalDateTime;
import java.util.List;
import java.util.concurrent.TimeUnit;

@RestController
@RequestMapping("/device")
public class DeviceController {

    @Resource
    private DeviceService deviceService;

    @Resource
    private RedisTemplate<String, Object> redisTemplate;

    private static final String DEVICE_CACHE_KEY = "iot:device:";
    private static final String DEVICE_STATUS_KEY = "iot:device:status:";

    // ----------------- 设备注册 -----------------
    @PostMapping("/register")
    public ResponseEntity<?> registerDevice(@RequestBody Device device) {
        if (device.getDeviceId() == null || device.getDeviceId().isBlank()) {
            return ResponseEntity.badRequest().body("deviceId不能为空");
        }

        // 检查设备是否已存在
        Device exist = deviceService.lambdaQuery().eq(Device::getDeviceId, device.getDeviceId()).one();
        if (exist != null) {
            return ResponseEntity.badRequest().body("设备已存在");
        }
        // 当前登录用户 ID 作为设备 owner
        device.setOwnerId(StpUtil.getLoginIdAsLong());
        device.setStatus("active");
        deviceService.save(device);
        return ResponseEntity.ok(device);
    }

    // ----------------- 设备登录 -----------------
    // 设备用硬件 deviceId 登录，返回 token
    @PostMapping("/login")
    public ResponseEntity<?> deviceLogin(@RequestParam String deviceId) {
        Device device = deviceService.lambdaQuery().eq(Device::getDeviceId, deviceId).one();

        if (device == null) return ResponseEntity.badRequest().body("设备不存在");

        if (!"active".equals(device.getStatus())) return ResponseEntity.badRequest().body("设备未激活");

        // 登录 Sa-Token，使用设备表主键 ID 作为登录 ID
        StpUtil.login(device.getId());
        return ResponseEntity.ok(StpUtil.getTokenInfo());
    }

//    // ----------------- 设备状态上报 -----------------
//    @PostMapping("/status/report")
//    public ResponseEntity<?> reportStatus(@RequestBody DeviceStatusDTO statusDTO) {
//        Long deviceId = StpUtil.getLoginIdAsLong();
//
//        DeviceStatusRedis status = new DeviceStatusRedis();
//        status.setDeviceId(deviceId);
//        status.setTemperature(statusDTO.getTemperature());
//        status.setHumidity(statusDTO.getHumidity());
//        status.setBatteryLevel(statusDTO.getBatteryLevel());
//        status.setSignalStrength(statusDTO.getSignalStrength());
//        status.setLastActiveTime(LocalDateTime.now());
//
//        // JSON 存 Redis
//        redisTemplate.opsForValue().set(DEVICE_STATUS_KEY + deviceId, JSON.toJSONString(status), 5, TimeUnit.MINUTES);
//
//        return ResponseEntity.ok("上报成功");
//    }


    // ----------------- 获取设备列表 -----------------
    @GetMapping("/list")
    public ResponseEntity<?> listDevices() {
        Long ownerId = StpUtil.getLoginIdAsLong();
        List<Device> devices = deviceService.lambdaQuery().eq(Device::getOwnerId, ownerId).list();
        return ResponseEntity.ok(devices);
    }

    // ----------------- 获取设备详情 -----------------
    @GetMapping("/{id}")
    public ResponseEntity<?> getDeviceDetail(@PathVariable Long id) {
        Device device = deviceService.getById(id);
        if (device == null) return ResponseEntity.badRequest().body("未找到设备");

        String statusJson = (String) redisTemplate.opsForValue().get(DEVICE_STATUS_KEY + id);
        DeviceStatusRedis status = statusJson == null ? null : JSON.parseObject(statusJson, DeviceStatusRedis.class);

        DeviceDetailDTO detailDTO = new DeviceDetailDTO();
        detailDTO.setId(device.getId());
        detailDTO.setDeviceId(device.getDeviceId());
        detailDTO.setOwnerId(device.getOwnerId());
        detailDTO.setStatus(device.getStatus());

        if (status != null) {
            detailDTO.setTemperature(status.getTemperature());
            detailDTO.setHumidity(status.getHumidity());
            detailDTO.setBatteryLevel(status.getBatteryLevel());
            detailDTO.setSignalStrength(status.getSignalStrength());
            detailDTO.setLastActiveTime(status.getLastActiveTime());
        }

        return ResponseEntity.ok(detailDTO);
    }

    // ----------------- 删除设备 -----------------
    @DeleteMapping("/{id}")
    public ResponseEntity<?> deleteDevice(@PathVariable Long id) {
        deviceService.removeById(id);
        // 删除 Redis 缓存
        redisTemplate.delete(DEVICE_STATUS_KEY + id);
        return ResponseEntity.ok("删除成功");
    }

//
//    @PostMapping("/heartbeat")
//    public ResponseEntity<?> heartbeat() {
//        Long deviceId = StpUtil.getLoginIdAsLong();
//
//        String key = DEVICE_STATUS_KEY + deviceId;
//        String statusJson = (String) redisTemplate.opsForValue().get(key);
//
//        DeviceStatusRedis status;
//        if (statusJson != null) {
//            status = JSON.parseObject(statusJson, DeviceStatusRedis.class);
//        } else {
//            status = new DeviceStatusRedis();
//            status.setDeviceId(deviceId);
//        }
//
//        // 只更新时间戳
//        status.setLastActiveTime(LocalDateTime.now());
//
//        // 写回 Redis，延长 5 分钟过期
//        redisTemplate.opsForValue().set(key, JSON.toJSONString(status), 5, TimeUnit.MINUTES);
//
//        return ResponseEntity.ok("心跳更新成功");
//    }


    // ----------------- DTO: 设备状态上报 -----------------
    @Data
    public static class DeviceStatusDTO {
        private Double temperature;
        private Double humidity;
        private Integer batteryLevel;
        private Integer signalStrength;
    }

    // ----------------- Redis 存储对象 -----------------
    @Data
    public static class DeviceStatusRedis {
        private Long deviceId;
        private Double temperature;
        private Double humidity;
        private Integer batteryLevel;
        private Integer signalStrength;
        private LocalDateTime lastActiveTime;
    }

    // ----------------- DTO: 设备详情 -----------------
    @Data
    public static class DeviceDetailDTO {
        private Long id;
        private String deviceId;
        private Long ownerId;
        private String status;

        private Double temperature;
        private Double humidity;
        private Integer batteryLevel;
        private Integer signalStrength;
        private LocalDateTime lastActiveTime;
    }

}
