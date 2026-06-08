package com.yu.iotplatform.control;

import cn.dev33.satoken.stp.StpUtil;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.yu.iotplatform.Util.DeviceUtil;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.service.DeviceReportService;
import com.yu.iotplatform.service.DeviceService;
import com.yu.iotplatform.service.UserService;
import jakarta.annotation.Resource;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.util.HashMap;
import java.util.List;
import java.util.Map;

import static com.yu.iotplatform.control.UserController.USER_STATUS_ACTIVE;

@RestController
@RequestMapping("/device")
public class DeviceController {

    @Resource
    private DeviceService deviceService;
    @Resource
    private DeviceReportService deviceReportService;
    @Resource
    private UserService userService;

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
        deviceService.cacheDevice(device);

        DeviceUtil.getDeviceStp().login(device.getDeviceId());
        Map<String, Object> result = new HashMap<>();
        result.put("device", device);
        result.put("deviceToken", DeviceUtil.getDeviceStp().getTokenValue());
        return ApiResponse.success("设备注册成功", result);
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
        if (device == null) return ApiResponse.fail(HttpStatus.NOT_FOUND.value(), "未找到设备");

        DeviceStatus status = deviceReportService.getDeviceStatus(deviceId);
        status = DeviceUtil.mergeDeviceWithStatus(device, status);

        return ApiResponse.success(status);
    }

    //--获取设备token--
    @GetMapping("/{deviceId}/login")
    public ApiResponse<?> getDevicesToken(@PathVariable String deviceId) {
        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) return ApiResponse.fail(HttpStatus.NOT_FOUND.value(), "未找到设备");
        if (!userService.isUserExist(device.getOwnerId()))
            return ApiResponse.fail(HttpStatus.NOT_FOUND.value(), "设备id未找到或者未绑定到用户");

        DeviceUtil.getDeviceStp().login(device.getDeviceId());
        Map<String, Object> result = new HashMap<>();
        result.put("deviceId", deviceId);
        result.put("deviceToken", DeviceUtil.getDeviceStp().getTokenValue());
        return ApiResponse.success(result);
    }

    // ----------------- 删除设备 -----------------
    @PostMapping("/{deviceId}/delete")
    public ApiResponse<?> deleteDevice(@PathVariable String deviceId) {
        boolean removed = deviceService.remove(new QueryWrapper<Device>().eq("device_id", deviceId));
        deviceService.evictDeviceCache(deviceId);
        deviceReportService.evictDeviceStatus(deviceId);
        deviceReportService.evictSensorRecentCache(deviceId);
        DeviceUtil.getDeviceStp().logout();

        if (removed) {
            return ApiResponse.success("删除成功");
        } else {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "删除目标不存在或错误");
        }
    }
}