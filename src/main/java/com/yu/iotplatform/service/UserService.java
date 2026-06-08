package com.yu.iotplatform.service;

import com.baomidou.mybatisplus.core.conditions.query.LambdaQueryWrapper;
import com.baomidou.mybatisplus.extension.service.IService;
import com.yu.iotplatform.entity.User;

public interface UserService extends IService<User> {

    // 自定义按用户名查询用户的方法
    default User getByAccount(String account) {
        return this.getOne(new LambdaQueryWrapper<User>()
                .eq(User::getAccount, account));
    }

    default boolean isUserExist(Long uid) {
        if (uid == null) {
            return false;
        };
        return count(new LambdaQueryWrapper<User>().eq(User::getId, uid)) > 0;
    }
}
