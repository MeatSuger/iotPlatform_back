package com.yu.iotplatform;

import com.alibaba.fastjson.JSON;
import com.yu.iotplatform.Util.RedisUtil;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.User;
import com.yu.iotplatform.mapper.UserMapper;
import com.yu.iotplatform.service.UserService;
import jakarta.annotation.Resource;
import org.junit.jupiter.api.Assertions;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.util.DigestUtils;

import java.util.List;

@SpringBootTest
class IotPlatformApplicationTests {

    @Autowired
    private UserMapper userMapper;
    @Resource
    private UserService userService;
    @Resource
    private RedisUtil redisUtil;

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
}
