package com.yu.iotplatform.control;


import com.alibaba.fastjson2.JSON;
import com.influxdb.client.InfluxDBClient;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.entity.DeviceStatusDTO;
import com.yu.iotplatform.entity.SensorData;
import com.yu.iotplatform.service.DeviceService;
import com.yu.iotplatform.service.InfluxDBService;
import jakarta.annotation.Resource;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.time.LocalDateTime;
import java.util.List;

import static com.yu.iotplatform.control.DeviceController.DEVICE_CACHE_KEY;

@RestController
@RequestMapping("/data")
public class DataController {
    @Resource
    private InfluxDBService influxDBService;
    @Resource
    private InfluxDBClient influxDBClient;
    @Resource
    private DeviceService deviceService;
    @Resource
    private RedisTemplate<String, Object> redisTemplate;

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
        Device device = deviceService.getDeviceById(deviceId);
        if (device == null) return ApiResponse.fail(404, "设备不存在");

        DeviceStatus status = new DeviceStatus();
        status.setDeviceId(deviceId);
        status.setSensors(statusDTO.getSensors());
        status.setLastActiveTime(LocalDateTime.now());
        redisTemplate.opsForHash().put(DEVICE_CACHE_KEY, deviceId, JSON.toJSONString(status));
        influxDBService.writeDeviceSensers(deviceId, statusDTO.getSensors());
        return ApiResponse.success("状态上报成功", null);
    }

    @GetMapping("/{deviceId}/Data/list")
    public ApiResponse<List<SensorData>> queryDeviceSensors(@PathVariable String deviceId, @RequestParam int limit) {
        List<SensorData> sensorDataList = influxDBService.queryRecentDeviceSensors(deviceId, limit);
        if (sensorDataList.isEmpty()) return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "未找到设备传感器");
        return ApiResponse.success(sensorDataList);

    }

    @GetMapping("/list")
    public ApiResponse<String> listDevices() {
        return ApiResponse.fail(HttpStatus.BAD_REQUEST.value(), "暂未实现");
    }

}
