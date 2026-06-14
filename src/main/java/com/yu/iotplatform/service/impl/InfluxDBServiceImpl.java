package com.yu.iotplatform.service.impl;

import com.influxdb.client.InfluxDBClient;
import com.influxdb.client.WriteApiBlocking;
import com.influxdb.client.domain.WritePrecision;
import com.influxdb.client.write.Point;
import com.influxdb.query.FluxRecord;
import com.influxdb.query.FluxTable;
import com.yu.iotplatform.config.CacheConfig;
import com.yu.iotplatform.config.InfluxDBConfig;
import com.yu.iotplatform.entity.SensorData;
import com.yu.iotplatform.service.InfluxDBService;
import jakarta.annotation.Resource;
import lombok.extern.slf4j.Slf4j;
import org.springframework.cache.annotation.Cacheable;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.time.LocalDateTime;
import java.time.ZoneId;
import java.util.List;
import java.util.Map;
import java.util.Objects;

@Slf4j
@Service
public class InfluxDBServiceImpl implements InfluxDBService {
	@Resource
	private InfluxDBClient influxDBClient;

	@Resource
	private InfluxDBConfig influxDBConfig;
	private final WriteApiBlocking writeApi;


	public InfluxDBServiceImpl(InfluxDBClient influxDBClient) {
		this.writeApi = influxDBClient.getWriteApiBlocking();
	}

	@Override
	public void writeDeviceSensers(String deviceID, List<SensorData> sensers) {
		List<Point> points = sensers.stream().map(sensorData -> {
			Point point = Point.measurement("device_sensors")
					.addTag("deviceID", deviceID)
					.addTag("sensorName", sensorData.getName())
					.addTag("type", sensorData.getType())
					.time(Instant.now(), WritePrecision.MS);

			Object value = sensorData.getValue();
			if (value instanceof Number number) {
				point.addField("value", number.doubleValue());
			} else if (value instanceof Boolean bool) {
				point.addField("value", bool);
			} else {
				point.addField("value", String.valueOf(value));
			}
			return point;
		}).toList();
		writeApi.writePoints(influxDBConfig.bucket, influxDBConfig.org, points);
		log.info("批量写入 InfluxDB: deviceId={}, 数量={}", deviceID, points.size());
	}

	@Async("iotTaskExecutor")
	@Override
	public void writeDeviceSensersAsync(String deviceID, List<SensorData> sensers) {
		try {
			writeDeviceSensers(deviceID, sensers);
		} catch (Exception e) {
			log.error("异步写入 InfluxDB 失败: deviceId={}, error={}", deviceID, e.getMessage(), e);
		}
	}

	/**
	 * 默认查询最近 N 条（近 7 天），结果缓存 30 秒
	 */
	@Override
	@Cacheable(value = CacheConfig.CACHE_SENSOR_RECENT, key = "#deviceID + ':' + #limit", unless = "#result.isEmpty()")
	public List<SensorData> queryRecentDeviceSensors(String deviceID, int limit) {
		return queryRecentDeviceSensors(deviceID, limit, LocalDateTime.now().minusDays(7));
	}

	/**
	 * 查询设备最近 N 条数据再最近1个小时
	 */
	@Override
	public List<SensorData> queryRecentDeviceSensors(String deviceID, int limit, LocalDateTime start) {
		// 如果 start == null，则默认取近 7 天
		String rangeStart = (start == null)
				? "-7d"
				: start.atZone(ZoneId.systemDefault()).toOffsetDateTime().toString();

		// 构造 Flux 查询语句
		String flux = String.format("""
						from(bucket: "%s")
						  |> range(start: %s)
						  |> filter(fn: (r) => r["deviceID"] == "%s")
						  |> sort(columns: ["_time"], desc: true)
						  |> limit(n: %d)
						""",
				influxDBConfig.bucket,
				rangeStart,
				deviceID,
				limit
		);
		return parseFluxRecords(influxDBClient.getQueryApi().query(flux));
	}


	/**
	 * 按时间区间和 sensorName 查询设备数据
	 */
	@Override
	public List<SensorData> queryDeviceSensorsByTime(String deviceID, String sensorName,
													 LocalDateTime start, LocalDateTime end) {
		String flux = String.format("""
						from(bucket: "%s")
						  |> range(start: %s, stop: %s)
						  |> filter(fn: (r) => r._measurement == "device_sensors")
						  |> filter(fn: (r) => r.deviceID == "%s")
						  |> filter(fn: (r) => r.sensorName == "%s")
						  |> sort(columns: ["_time"], desc: false)
						""",
				influxDBConfig.bucket,
				start.atZone(ZoneId.systemDefault()).toOffsetDateTime(),
				end.atZone(ZoneId.systemDefault()).toOffsetDateTime(),
				deviceID,
				sensorName
		);
		log.info("执行 Flux 查询:\n{}", flux);
		return parseFluxRecords(influxDBClient.getQueryApi().query(flux));
	}

	/**
	 * 聚合统计：平均值、最大值、最小值
	 */
	@Override
	public Map<String, Double> aggregateDeviceSensor(String deviceID, String sensorName,
													 LocalDateTime start, LocalDateTime end,
													 String field) {
		String flux = String.format("""
						from(bucket: "%s")
						  |> range(start: %s, stop: %s)
						  |> filter(fn: (r) => r._measurement == "device_sensors")
						  |> filter(fn: (r) => r.deviceID == "%s")
						  |> filter(fn: (r) => r.sensorName == "%s")
						  |> keep(columns: ["_value", "_field"])
						""",
				influxDBConfig.bucket,
				start.atZone(ZoneId.systemDefault()).toOffsetDateTime(),
				end.atZone(ZoneId.systemDefault()).toOffsetDateTime(),
				deviceID,
				sensorName
		);

		List<FluxRecord> records = influxDBClient.getQueryApi().query(flux)
				.stream().flatMap(t -> t.getRecords().stream()).toList();

		List<Double> values = records.stream()
				.filter(r -> Objects.equals(r.getField(), field))
				.map(r -> ((Number) Objects.requireNonNull(r.getValue())).doubleValue())
				.toList();

		if (values.isEmpty()) return Map.of("mean", 0.0, "max", 0.0, "min", 0.0);

		double mean = values.stream().mapToDouble(Double::doubleValue).average().orElse(0);
		double max = values.stream().mapToDouble(Double::doubleValue).max().orElse(0);
		double min = values.stream().mapToDouble(Double::doubleValue).min().orElse(0);

		return Map.of("mean", mean, "max", max, "min", min);
	}

	/**
	 * FluxRecord 转 SensorData
	 */
	private List<SensorData> parseFluxRecords(List<FluxTable> tables) {
		if (tables == null) return List.of(); // 防止空表

		return tables.stream()
				.flatMap(table -> table.getRecords().stream())
				.map(record -> {
					SensorData data = new SensorData();

					// 安全获取 tag 和 field
					Object sensorName = record.getValueByKey("sensorName");
					Object type = record.getValueByKey("type");

					data.setName(sensorName != null ? sensorName.toString() : "unknown");
					data.setType(type != null ? type.toString() : "unknown");

					String field = record.getField();
					Object val = record.getValue();

					// 根据字段类型设置 value
					if ("value_float".equals(field) && val instanceof Number) {
						data.setValue(((Number) val).doubleValue());
					} else if ("value_bool".equals(field) && val instanceof Boolean) {
						data.setValue(val);
					} else {
						data.setValue(val != null ? val.toString() : null);
					}
					data.setTimestamp(Objects.requireNonNull(record.getTime())
							.atZone(ZoneId.systemDefault()).toLocalDateTime());

					return data;
				})
				.toList();
	}


}
