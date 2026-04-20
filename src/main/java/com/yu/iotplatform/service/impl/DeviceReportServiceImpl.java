package com.yu.iotplatform.service.impl;

import cn.dev33.satoken.exception.NotLoginException;
import com.yu.iotplatform.Util.DeviceUtil;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.entity.DeviceStatusDTO;
import com.yu.iotplatform.service.DeviceReportService;
import com.yu.iotplatform.service.DeviceService;
import com.yu.iotplatform.service.InfluxDBService;
import jakarta.annotation.Resource;
import org.springframework.http.HttpStatus;
import org.springframework.stereotype.Service;

import java.time.LocalDateTime;
import java.util.Map;

@Service
public class DeviceReportServiceImpl implements DeviceReportService {
    @Resource
    private InfluxDBService influxDBService;
    @Resource
    private DeviceService deviceService;

    @Override
    public ApiResponse<String> reportStatus(String deviceId,
                                            String authorization,
                                            String deviceTokenHeader,
                                            DeviceStatusDTO statusDTO) {
        if (statusDTO == null || statusDTO.getSensors() == null || statusDTO.getSensors().isEmpty()) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "上传数据不能为空");
        }

        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) {
            return ApiResponse.fail(HttpStatus.NOT_FOUND.value(), "设备不存在");
        }

        String deviceToken = resolveDeviceToken(authorization, deviceTokenHeader);
        if (!isValidDeviceToken(deviceId, deviceToken)) {
            return ApiResponse.fail(HttpStatus.UNAUTHORIZED.value(), "设备token无效");
        }

        DeviceStatus status = new DeviceStatus();
        status.setDeviceId(deviceId);
        status.setStatus(DeviceUtil.DEVICE_ONLINE_STATUS);
        status.setSensors(statusDTO.getSensors());
        status.setLastActiveTime(LocalDateTime.now());
        DeviceUtil.cacheDeviceStatus(status);
        influxDBService.writeDeviceSensersAsync(deviceId, statusDTO.getSensors());
        DeviceUtil.clearRecentSensorCache(deviceId);
        return ApiResponse.success("状态上报已接收", null);
    }

    @Override
    public ApiResponse<?> heartbeat(String deviceId,
                                    String authorization,
                                    String deviceTokenHeader) {
        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) {
            return ApiResponse.fail(HttpStatus.NOT_FOUND.value(), "设备不存在");
        }

        String deviceToken = resolveDeviceToken(authorization, deviceTokenHeader);
        if (!isValidDeviceToken(deviceId, deviceToken)) {
            return ApiResponse.fail(HttpStatus.UNAUTHORIZED.value(), "设备token无效");
        }

        DeviceUtil.markDeviceOnline(deviceId);
        return ApiResponse.success(Map.of(
                "serverTime", LocalDateTime.now(),
                "nextInterval", DeviceUtil.HEARTBEAT_INTERVAL_SECONDS
        ));
    }

    private String resolveDeviceToken(String authorization, String deviceTokenHeader) {
        if (deviceTokenHeader != null && !deviceTokenHeader.isBlank()) {
            return deviceTokenHeader.trim();
        }
        if (authorization == null || authorization.isBlank()) {
            return null;
        }
        String prefix = "Bearer ";
        if (authorization.startsWith(prefix)) {
            return authorization.substring(prefix.length()).trim();
        }
        return authorization.trim();
    }

    private boolean isValidDeviceToken(String deviceId, String token) {
        if (deviceId == null || deviceId.isBlank() || token == null || token.isBlank()) {
            return false;
        }
        try {
            Object loginId = DeviceUtil.DEVICE_STP.getLoginIdByToken(token);
            String tokenDeviceId = DeviceUtil.normalizeDeviceId(String.valueOf(loginId));
            return DeviceUtil.normalizeDeviceId(deviceId).equals(tokenDeviceId);
        } catch (NotLoginException e) {
            return false;
        }
    }
}
