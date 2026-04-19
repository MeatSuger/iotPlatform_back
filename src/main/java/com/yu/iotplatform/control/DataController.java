package com.yu.iotplatform.control;


import com.alibaba.fastjson2.JSON;
import com.influxdb.client.InfluxDBClient;
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
import java.util.Set;
import java.util.List;
import java.util.concurrent.TimeUnit;

import static com.yu.iotplatform.control.DeviceController.DEVICE_CACHE_KEY;

@RestController
@RequestMapping("/data")
public class DataController {
    private static final String SENSOR_RECENT_CACHE_PREFIX = "iot:cache:sensor:recent:";
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
    public ApiResponse<String> reportStatus(@PathVariable String deviceId, @RequestBody DeviceStatusDTO statusDTO) {
        if (statusDTO == null || statusDTO.getSensors() == null || statusDTO.getSensors().isEmpty()) {
            return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "上传数据不能为空");
        }

        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) return ApiResponse.fail(404, "设备不存在");

        DeviceStatus status = new DeviceStatus();
        status.setDeviceId(deviceId);
        status.setSensors(statusDTO.getSensors());
        status.setLastActiveTime(LocalDateTime.now());
        RedisUtil.HashOps.hPut(DEVICE_CACHE_KEY, deviceId, JSON.toJSONString(status));
        influxDBService.writeDeviceSensers(deviceId, statusDTO.getSensors());
        clearRecentSensorCache(deviceId);
        return ApiResponse.success("状态上报成功", null);
    }

    @GetMapping("/{deviceId}/Data/list")
    public ApiResponse<List<SensorData>> queryDeviceSensors(@PathVariable String deviceId, @RequestParam int limit) {
        String cacheKey = SENSOR_RECENT_CACHE_PREFIX + deviceId + ":" + limit;
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

    private void clearRecentSensorCache(String deviceId) {
        Set<String> keys = RedisUtil.KeyOps.keys(SENSOR_RECENT_CACHE_PREFIX + deviceId + ":*");
        if (keys != null && !keys.isEmpty()) {
            RedisUtil.KeyOps.delete(keys);
        }
    }

}
