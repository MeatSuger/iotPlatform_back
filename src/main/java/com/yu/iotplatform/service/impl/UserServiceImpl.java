package com.yu.iotplatform.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.yu.iotplatform.entity.User;
import com.yu.iotplatform.mapper.UserMapper;
import com.yu.iotplatform.service.UserService;
import org.springframework.stereotype.Service;

@Service
public class UserServiceImpl extends ServiceImpl<UserMapper, User> implements UserService {

}
