package com.yu.iotplatform;

import com.alibaba.fastjson.JSON;
import com.yu.iotplatform.Util.RedisUtil;
import com.yu.iotplatform.control.DeviceController.SensorData;
import com.yu.iotplatform.entity.User;
import com.yu.iotplatform.mapper.UserMapper;
import com.yu.iotplatform.service.InfluxDBService;
import com.yu.iotplatform.service.UserService;
import jakarta.annotation.Resource;
import lombok.extern.slf4j.Slf4j;
import org.junit.jupiter.api.Assertions;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.util.DigestUtils;

import java.time.LocalDateTime;
import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.*;

@Slf4j
@SpringBootTest
class IotPlatformApplicationTests {

    @Autowired
    private UserMapper userMapper;
    @Resource
    private UserService userService;
    @Resource
    private RedisUtil redisUtil;

    @Resource
    private InfluxDBService influxDBService;

    public static final String deviceId = "TEST-DEVICE-001";

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
        System.out.println("所有用户：" + JSON.toJSONString(users, true));
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
        System.out.println("查询用户：" + JSON.toJSONString(user, true));
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
        System.out.println("删除结果：" + removed);
        Assertions.assertTrue(removed);
    }

    private List<SensorData> mockSensors;

    @BeforeEach
    void setUp() {
        mockSensors = List.of(
                new SensorData() {{
                    setName("temperature");
                    setType("temp");
                    setValue(26.4);
                    setTimestamp(LocalDateTime.now());
                }},
                new SensorData() {{
                    setName("humidity");
                    setType("humidy");
                    setValue(75);
                    setTimestamp(LocalDateTime.now());
                }}
        );
    }


    @Test
    void testWriteAndQuerySensors() {
        // 准备测试数据
        SensorData s1 = new SensorData();
        s1.setName("temp");
        s1.setType("temperature");
        s1.setValue(25.3);

        SensorData s2 = new SensorData();
        s2.setName("humidity");
        s2.setType("humidity");
        s2.setValue(60);

        List<SensorData> sensors = List.of(s1, s2);
        String deviceId = "TEST_DEVICE_001";

        // ✅ 测试写入
        assertDoesNotThrow(() -> influxDBService.writeDeviceSensers(deviceId, sensors));

        // ✅ 测试查询最近 N 条
        List<SensorData> recent = influxDBService.queryRecentDeviceSensors(deviceId, 10);
        assertNotNull(recent);
        assertTrue(recent.size() >= 2);

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
