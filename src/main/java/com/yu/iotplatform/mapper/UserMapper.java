package com.yu.iotplatform.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.yu.iotplatform.entity.User;
import org.apache.ibatis.annotations.Mapper;

@Mapper
public interface UserMapper extends BaseMapper<User> {

}
