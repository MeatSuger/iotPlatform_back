package com.yu.iotplatform.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import com.yu.iotplatform.entity.MqttPublishLog;
import org.apache.ibatis.annotations.Mapper;

@Mapper
public interface MqttPublishLogMapper extends BaseMapper<MqttPublishLog> {
}
