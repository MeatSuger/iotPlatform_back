//package com.yu.iotplatform.control;
//
//
//import cn.dev33.satoken.stp.StpUtil;
//import com.influxdb.client.InfluxDBClient;
//import com.yu.iotplatform.common.ApiResponse;
//import com.yu.iotplatform.service.InfluxDBService;
//import jakarta.annotation.Resource;
//import org.springframework.web.bind.annotation.*;
//
//import java.util.List;
//import java.util.Map;
//
//@RestController
//@RequestMapping("/data")
//public class DataController {
//    @Resource
//    private InfluxDBService influxDBService;
//    @Resource
//    private InfluxDBClient influxDBClient;
//
//
//    @PostMapping("/ping")
//    public ApiResponse<String> ping() {
//        return ApiResponse.success(String.valueOf(influxDBClient.ping()));
//    }
//
//    @PostMapping("/devices/{deviceId}/sensors/{apiTag}")
//    public ApiResponse<String> postDeviceData(
//            @PathVariable String deviceId,
//            @PathVariable String apiTag,
//            @RequestBody DeviceController.DeviceDetailDTO data) {
//        StpUtil.checkLogin();
//        // 假设 apiTag 作为测量值 (measurement)，deviceId 作为标签值 (tag)，data 作为实际数据
//        influxDBService.save(data);
//        return ApiResponse.success("成功");
//    }
//
////    @GetMapping("/devices/{deviceId}/sensors/{apiTag}")
////    public ApiResponse<List<Map<String, Object>>> getDeviceSensorData(
////            @PathVariable String deviceId,
////            @PathVariable String apiTag,
////            @RequestParam(required = false) Long startTime,
////            @RequestParam(required = false) Long endTime,
////            @RequestParam(required = false, defaultValue = "100") Integer limit) {
////        StpUtil.checkLogin();
////        // 假设 InfluxDBService 有一个 readData 方法来根据 measurement, deviceId, 时间范围和限制获取数据
////        List<Map<String, Object>> data = influxDBService.readData(apiTag, deviceId, startTime, endTime, limit);
////        return ApiResponse.success(data);
////    }
//
//}
