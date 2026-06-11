package com.yu.iotplatform;

import com.alibaba.fastjson2.JSON;
import com.yu.iotplatform.entity.SensorData;
import com.yu.iotplatform.entity.User;
import com.yu.iotplatform.service.InfluxDBService;
import com.yu.iotplatform.service.UserService;
import jakarta.annotation.Resource;
import lombok.extern.slf4j.Slf4j;
import org.junit.jupiter.api.Assertions;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.util.DigestUtils;

import java.time.LocalDateTime;
import java.util.List;
import java.util.Map;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;

@Slf4j
@SpringBootTest
class IotPlatformApplicationTests {


	public static final String deviceId = "TEST-DEVICE-001";
	@Resource
	private UserService userService;
	@Resource
	private InfluxDBService influxDBService;

	/**
	 * ✅ 测试注册用户
	 */
	@Test
	void testRegister() {
		User user = new User();
		user.setAccount("iot_test");
		user.setPasswd(DigestUtils.md5DigestAsHex("123456".getBytes()));
		user.setName("测试用户");
		user.setStatus("active");

		boolean saved = userService.save(user);
		System.out.println("注册用户结果：" + saved);
		Assertions.assertTrue(saved);
	}

	/**
	 * ✅ 测试查询所有用户
	 */
	@Test
	void testListAll() {
		List<User> users = userService.list();
		System.out.println("所有用户：" + JSON.toJSONString(users, String.valueOf(true)));
		Assertions.assertFalse(users.isEmpty());
	}

	/**
	 * ✅ 测试按账号查询
	 */
	@Test
	void testGetByAccount() {
		User user = userService.getOne(
				new com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper<User>()
						.eq(User::getAccount, "iot_test")
		);
		System.out.println("查询用户：" + JSON.toJSONString(user, String.valueOf(true)));
		Assertions.assertNotNull(user);
	}


	/**
	 * ✅ 测试删除用户
	 */
	@Test
	void testDeleteUser() {
		boolean removed = userService.remove(
				new com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper<User>()
						.eq(User::getAccount, "iot_test")
		);
		Assertions.assertTrue(removed);
	}

	@BeforeEach
	void setUp() {
		List.of(
				new SensorData() {{
					setName("temperature");
					setType("temp");
					setValue(Optional.of(26.4));
					setTimestamp(LocalDateTime.now());
				}},
				new SensorData() {{
					setName("humidity");
					setType("humidy");
					setValue(Optional.of(75));
					setTimestamp(LocalDateTime.now());
				}}
		);
	}


	//    @Test
	void testWriteAndQuerySensors() {
		// 准备测试数据
		SensorData s1 = new SensorData();
		s1.setName("temp");
		s1.setType("temperature");
		s1.setValue(Optional.of(25.3));

		SensorData s2 = new SensorData();
		s2.setName("humidity");
		s2.setType("humidity");
		s2.setValue(Optional.of(60));

		List<SensorData> sensors = List.of(s1, s2);
		String deviceId = "TEST_DEVICE_001";

		// ✅ 测试写入
		assertDoesNotThrow(() -> influxDBService.writeDeviceSensers(deviceId, sensors));

		// ✅ 测试查询最近 N 条
		List<SensorData> recent = influxDBService.queryRecentDeviceSensors(deviceId, 10);
		assertNotNull(recent);
		assertTrue(recent.size() >= 2);

		List<SensorData> recent1 = influxDBService.queryRecentDeviceSensors(deviceId, 10, LocalDateTime.now().minusDays(4));
		assertNotNull(recent1);
		assertTrue(recent1.size() >= 2);

		// ✅ 测试按时间区间查询
		LocalDateTime start = LocalDateTime.now().minusHours(1);
		LocalDateTime end = LocalDateTime.now();
		List<SensorData> rangeData = influxDBService.queryDeviceSensorsByTime(deviceId, "temp", start, end);
		assertNotNull(rangeData);
		log.info(rangeData.toString());
		assertTrue(rangeData.stream().anyMatch(d -> d.getName().equals("temp")));

		// ✅ 测试聚合统计
		Map<String, Double> stats = influxDBService.aggregateDeviceSensor(deviceId, "temp", start, end, "value_float");
		assertNotNull(stats);
		assertTrue(stats.containsKey("mean"));
		assertTrue(stats.containsKey("max"));
		assertTrue(stats.containsKey("min"));
	}
}
