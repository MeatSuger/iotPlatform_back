package com.yu.iotplatform.service.impl;

import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import com.yu.iotplatform.entity.MqttPublishLog;
import com.yu.iotplatform.mapper.MqttPublishLogMapper;
import com.yu.iotplatform.service.MqttPublishLogService;
import org.springframework.stereotype.Service;

@Service
public class MqttPublishLogServiceImpl extends ServiceImpl<MqttPublishLogMapper, MqttPublishLog> implements MqttPublishLogService {
}
