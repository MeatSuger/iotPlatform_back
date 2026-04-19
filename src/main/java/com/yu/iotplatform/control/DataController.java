package com.yu.iotplatform.control;


import com.alibaba.fastjson2.JSON;
import com.influxdb.client.InfluxDBClient;
import com.yu.iotplatform.Util.DeviceUtil;
import com.yu.iotplatform.Util.RedisUtil;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.entity.DeviceStatusDTO;
import com.yu.iotplatform.entity.SensorData;
import com.yu.iotplatform.service.DeviceService;
import com.yu.iotplatform.service.InfluxDBService;
import jakarta.annotation.Resource;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.time.LocalDateTime;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

@RestController
@RequestMapping("/data")
public class DataController {
    private static final long SENSOR_RECENT_CACHE_TTL_SECONDS = 30;

    @Resource
    private InfluxDBService influxDBService;
    @Resource
    private InfluxDBClient influxDBClient;
    @Resource
    private DeviceService deviceService;

    @PostMapping("/ping")
    public ApiResponse<String> ping() {
        return ApiResponse.success(String.valueOf(influxDBClient.ping()));
    }

    /**
     * 上传数据
     *
     * @param deviceId  上传的设备id
     * @param statusDTO 上传的数据list集合
     * @return 上传返回的状态
     */
    @PostMapping("/{deviceId}/Data")
    public ApiResponse<String> reportStatus(@PathVariable String deviceId,
                                            @RequestHeader(value = "Authorization", required = false) String authorization,
                                            @RequestHeader(value = "X-Device-Token", required = false) String deviceTokenHeader,
                                            @RequestBody DeviceStatusDTO statusDTO) {
        if (statusDTO == null || statusDTO.getSensors() == null || statusDTO.getSensors().isEmpty()) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "上传数据不能为空");
        }

        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) return ApiResponse.fail(404, "设备不存在");

        String deviceToken = resolveDeviceToken(authorization, deviceTokenHeader);
        if (deviceToken == null || !DeviceUtil.validateDeviceToken(deviceId, deviceToken)) {
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

    /**
     * 设备心跳：仅表示设备在线
     */
    @PostMapping({"/{deviceId}/ping", "/{deviceId}/heartbeat"})
    public ApiResponse<?> heartbeat(@PathVariable String deviceId,
                                    @RequestHeader(value = "Authorization", required = false) String authorization,
                                    @RequestHeader(value = "X-Device-Token", required = false) String deviceTokenHeader) {
        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) {
            return ApiResponse.fail(HttpStatus.NOT_FOUND.value(), "设备不存在");
        }

        String deviceToken = resolveDeviceToken(authorization, deviceTokenHeader);
        if (deviceToken == null || !DeviceUtil.validateDeviceToken(deviceId, deviceToken)) {
            return ApiResponse.fail(HttpStatus.UNAUTHORIZED.value(), "设备token无效");
        }

        DeviceUtil.markDeviceOnline(deviceId);
        return ApiResponse.success(Map.of(
                "serverTime", LocalDateTime.now(),
                "nextInterval", DeviceUtil.HEARTBEAT_INTERVAL_SECONDS
        ));
    }

    @GetMapping("/{deviceId}/Data/list")
    public ApiResponse<List<SensorData>> queryDeviceSensors(@PathVariable String deviceId, @RequestParam int limit) {
        String cacheKey = DeviceUtil.sensorRecentCacheKey(deviceId, limit);
        String cached = RedisUtil.StringOps.get(cacheKey);
        if (cached != null && !cached.isBlank()) {
            List<SensorData> cachedList = JSON.parseArray(cached, SensorData.class);
            if (cachedList != null && !cachedList.isEmpty()) {
                return ApiResponse.success(cachedList);
            }
        }

        List<SensorData> sensorDataList = influxDBService.queryRecentDeviceSensors(deviceId, limit);
        if (sensorDataList.isEmpty()) return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "未找到设备传感器");
        RedisUtil.StringOps.setEx(
                cacheKey,
                JSON.toJSONString(sensorDataList),
                SENSOR_RECENT_CACHE_TTL_SECONDS,
                TimeUnit.SECONDS
        );
        return ApiResponse.success(sensorDataList);

    }

    @GetMapping("/list")
    public ApiResponse<String> listDevices() {
        return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "暂未实现");
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

}
