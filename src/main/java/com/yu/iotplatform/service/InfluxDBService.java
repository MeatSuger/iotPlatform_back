package com.yu.iotplatform.service;


import com.yu.iotplatform.entity.SensorData;

import java.time.LocalDateTime;
import java.util.List;
import java.util.Map;



public interface InfluxDBService {


    void writeDeviceSensers(String deviceID, List<SensorData> sensers);

    /**
     * 查询设备最近 N 条数据
     */
    List<SensorData> queryRecentDeviceSensors(String deviceID, int limit);

     List<SensorData> queryRecentDeviceSensors(String deviceID, int limit, LocalDateTime start);

    /**
     * 按时间区间和 sensorName 查询设备数据
     */
    List<SensorData> queryDeviceSensorsByTime(String deviceID, String sensorName,
                                              LocalDateTime start, LocalDateTime end);

    /**
     * 聚合统计：平均值、最大值、最小值
     */
    Map<String, Double> aggregateDeviceSensor(String deviceID, String sensorName,
                                              LocalDateTime start, LocalDateTime end,
                                              String field);


}
