package com.yu.iotplatform.control;


import com.influxdb.client.InfluxDBClient;
import com.yu.iotplatform.common.ApiResponse;
import com.yu.iotplatform.entity.DeviceStatusDTO;
import com.yu.iotplatform.entity.SensorData;
import com.yu.iotplatform.service.DeviceReportService;
import com.yu.iotplatform.service.InfluxDBService;
import jakarta.annotation.Resource;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.util.List;

@RestController
@RequestMapping("/data")
public class DataController {
    @Resource
    private InfluxDBService influxDBService;
    @Resource
    private InfluxDBClient influxDBClient;
    @Resource
    private DeviceReportService deviceReportService;

    @PostMapping("/ping")
    public ApiResponse<String> ping() {
        return ApiResponse.success(String.valueOf(influxDBClient.ping()));
    }

    /**
     * 上传传感器数据。
     * 设备 Token 优先从 X-Device-Token 请求头获取，其次从同名 Cookie 获取。
     */
    @PostMapping("/{deviceId}/Data")
    public ApiResponse<String> reportStatus(@PathVariable String deviceId,
                                            @RequestHeader(value = "X-Device-Token", required = false) String deviceTokenHeader,
                                            @CookieValue(value = "X-Device-Token", required = false) String deviceTokenCookie,
                                            @RequestBody DeviceStatusDTO statusDTO) {
        String token = resolveToken(deviceTokenHeader, deviceTokenCookie);
        return deviceReportService.reportStatus(deviceId, token, statusDTO);
    }

    /**
     * 设备心跳：维持在线状态。
     * 设备 Token 优先从 X-Device-Token 请求头获取，其次从同名 Cookie 获取。
     */
    @PostMapping({"/{deviceId}/ping", "/{deviceId}/heartbeat"})
    public ApiResponse<?> heartbeat(@PathVariable String deviceId,
                                    @RequestHeader(value = "X-Device-Token", required = false) String deviceTokenHeader,
                                    @CookieValue(value = "X-Device-Token", required = false) String deviceTokenCookie) {
        String token = resolveToken(deviceTokenHeader, deviceTokenCookie);
        return deviceReportService.heartbeat(deviceId, token);
    }

    /** 缓存由 InfluxDBServiceImpl 上的 @Cacheable 自动处理 */
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

    /** 合并 Header 和 Cookie 中的 Token，Header 优先 */
    private String resolveToken(String headerToken, String cookieToken) {
        if (headerToken != null && !headerToken.isBlank()) {
            return headerToken.trim();
        }
        if (cookieToken != null && !cookieToken.isBlank()) {
            return cookieToken.trim();
        }
        return null;
    }
}
