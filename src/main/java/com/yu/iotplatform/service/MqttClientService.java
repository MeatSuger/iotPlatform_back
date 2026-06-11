package com.yu.iotplatform.service;

import com.yu.iotplatform.entity.mqtt.MqttClientStatus;
import com.yu.iotplatform.entity.mqtt.MqttMessageView;
import com.yu.iotplatform.entity.mqtt.MqttPublishRequest;
import com.yu.iotplatform.entity.mqtt.MqttSubscribeRequest;

import java.util.List;

public interface MqttClientService {
	MqttClientStatus connect();

	MqttClientStatus disconnect();

	MqttClientStatus subscribe(MqttSubscribeRequest request);

	MqttClientStatus unsubscribe(String topic);

	void publish(MqttPublishRequest request);

	MqttClientStatus status();

	List<MqttMessageView> recentMessages(int limit);
}
