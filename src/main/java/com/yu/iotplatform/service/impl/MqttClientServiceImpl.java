package com.yu.iotplatform.service.impl;

import com.alibaba.fastjson2.JSON;
import com.yu.iotplatform.entity.MqttPublishLog;
import com.yu.iotplatform.entity.mqtt.MqttClientStatus;
import com.yu.iotplatform.entity.mqtt.MqttMessageView;
import com.yu.iotplatform.entity.mqtt.MqttPublishRequest;
import com.yu.iotplatform.entity.mqtt.MqttSubscribeRequest;
import com.yu.iotplatform.handler.Mqtt2WebSocketHandler;
import com.yu.iotplatform.service.MqttClientService;
import com.yu.iotplatform.service.MqttPublishLogService;
import jakarta.annotation.PreDestroy;
import lombok.extern.slf4j.Slf4j;
import org.eclipse.paho.client.mqttv3.*;
import org.eclipse.paho.client.mqttv3.persist.MemoryPersistence;
import org.jspecify.annotations.NonNull;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import java.nio.charset.StandardCharsets;
import java.time.LocalDateTime;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentLinkedDeque;

@Slf4j
@Service
public class MqttClientServiceImpl implements MqttClientService {

	private static final int MAX_BUFFERED_MESSAGES = 500;
	private static final int MAX_PUBLISH_HISTORY = 500;
	private static final String PUBLISH_HISTORY_KEY = "iot:mqtt:publish:messages";
	private final Map<String, Integer> subscriptions = new ConcurrentHashMap<>();
	private final ConcurrentLinkedDeque<MqttMessageView> messageBuffer = new ConcurrentLinkedDeque<>();
	private final MqttPublishLogService mqttPublishLogService;
	private final StringRedisTemplate stringRedisTemplate;
	@Value("${iot.mqtt.broker-url:}")
	private String defaultBrokerUrl;
	@Value("${iot.mqtt.client-id:iot-platform-backend}")
	private String defaultClientId;
	@Value("${iot.mqtt.username:}")
	private String defaultUsername;
	@Value("${iot.mqtt.password:}")
	private String defaultPassword;
	@Value("${iot.mqtt.clean-session:true}")
	private boolean defaultCleanSession;
	@Value("${iot.mqtt.keep-alive-seconds:60}")
	private int defaultKeepAliveSeconds;
	@Value("${iot.mqtt.connection-timeout-seconds:10}")
	private int defaultConnectionTimeoutSeconds;
	@Value("${iot.mqtt.automatic-reconnect:true}")
	private boolean defaultAutomaticReconnect;
	@Value("${iot.mqtt.will-topic:}")
	private String defaultWillTopic;
	@Value("${iot.mqtt.will-payload:}")
	private String defaultWillPayload;
	@Value("${iot.mqtt.will-qos:0}")
	private int defaultWillQos;
	@Value("${iot.mqtt.will-retained:false}")
	private boolean defaultWillRetained;
	private volatile MqttClient client;
	private volatile String activeBrokerUrl;
	private volatile String activeClientId;

	public MqttClientServiceImpl(MqttPublishLogService mqttPublishLogService, StringRedisTemplate stringRedisTemplate) {
		this.mqttPublishLogService = mqttPublishLogService;
		this.stringRedisTemplate = stringRedisTemplate;
	}

	@Override
	public synchronized MqttClientStatus connect() {
		String brokerUrl = trimToNull(defaultBrokerUrl);
		if (!StringUtils.hasText(brokerUrl)) {
			throw new IllegalArgumentException("brokerUrl不能为空");
		}

		String clientId = trimToNull(defaultClientId);
		if (!StringUtils.hasText(clientId)) {
			clientId = "iot-platform-" + UUID.randomUUID();
		}

		String username = trimToNull(defaultUsername);
		String password = trimToNull(defaultPassword);

		boolean cleanSession = defaultCleanSession;
		int keepAliveSeconds = Math.max(1, defaultKeepAliveSeconds);
		int connectionTimeoutSeconds = Math.max(1, defaultConnectionTimeoutSeconds);
		boolean automaticReconnect = defaultAutomaticReconnect;

		try {
			closeCurrentClient();

			MqttClient newClient = getMqttClient(brokerUrl, clientId);

			MqttConnectOptions options = new MqttConnectOptions();
			options.setCleanSession(cleanSession);
			options.setKeepAliveInterval(keepAliveSeconds);
			options.setConnectionTimeout(connectionTimeoutSeconds);
			options.setAutomaticReconnect(automaticReconnect);

			if (StringUtils.hasText(username)) {
				options.setUserName(username);
			}
			if (StringUtils.hasText(password)) {
				options.setPassword(password.toCharArray());
			}

			applyWillOption(options);

			newClient.connect(options);
			this.client = newClient;
			this.activeBrokerUrl = brokerUrl;
			this.activeClientId = clientId;

			if (!subscriptions.isEmpty()) {
				for (Map.Entry<String, Integer> entry : subscriptions.entrySet()) {
					newClient.subscribe(entry.getKey(), entry.getValue());
				}
			}

			return status();
		} catch (MqttException e) {
			throw new IllegalStateException("MQTT连接失败: " + e.getMessage(), e);
		}
	}

	private @NonNull MqttClient getMqttClient(String brokerUrl, String clientId) throws MqttException {
		MqttClient newClient = new MqttClient(brokerUrl, clientId, new MemoryPersistence());
		newClient.setCallback(new MqttCallback() {
			@Override
			public void connectionLost(Throwable cause) {
				log.warn("MQTT连接断开: {}", cause == null ? "unknown" : cause.getMessage());
			}

			@Override
			public void messageArrived(String topic, MqttMessage message) {
				MqttMessageView view = MqttMessageView.of(topic,
						new String(message.getPayload(), StandardCharsets.UTF_8),
						message.getQos(),
						message.isRetained(),
						message.isDuplicate());
				messageBuffer.addLast(view);
				while (messageBuffer.size() > MAX_BUFFERED_MESSAGES) {
					messageBuffer.pollFirst();
				}
				String json = JSON.toJSONString(view);
				Mqtt2WebSocketHandler.broadcast(json);
			}

			@Override
			public void deliveryComplete(IMqttDeliveryToken token) {
				// 发布完成回调，可按需扩展
				log.info("发布完成");
			}
		});
		return newClient;
	}

	@Override
	public synchronized MqttClientStatus disconnect() {
		closeCurrentClient();
		return status();
	}

	@Override
	public synchronized MqttClientStatus subscribe(MqttSubscribeRequest request) {
		ensureConnected();
		if (request == null || !StringUtils.hasText(request.topic())) {
			throw new IllegalArgumentException("订阅topic不能为空");
		}
		int qos = normalizeQos(request.qos());
		try {
			client.subscribe(request.topic(), qos);
			subscriptions.put(request.topic(), qos);
			return status();
		} catch (MqttException e) {
			throw new IllegalStateException("订阅失败: " + e.getMessage(), e);
		}
	}

	@Override
	public synchronized MqttClientStatus unsubscribe(String topic) {
		ensureConnected();
		if (!StringUtils.hasText(topic)) {
			throw new IllegalArgumentException("取消订阅topic不能为空");
		}
		try {
			client.unsubscribe(topic);
			subscriptions.remove(topic);
			return status();
		} catch (MqttException e) {
			throw new IllegalStateException("取消订阅失败: " + e.getMessage(), e);
		}
	}

	@Override
	public synchronized void publish(MqttPublishRequest request) {
		ensureConnected();
		if (request == null || !StringUtils.hasText(request.topic())) {
			throw new IllegalArgumentException("发布topic不能为空");
		}
		try {
			String payload = request.payload() == null ? "" : request.payload();
			int qos = normalizeQos(request.qos());
			boolean retained = request.retained() != null && request.retained();

			MqttMessage message = new MqttMessage();
			message.setPayload(payload.getBytes(StandardCharsets.UTF_8));
			message.setQos(qos);
			message.setRetained(retained);
			client.publish(request.topic(), message);

			persistPublishedMessage(request.topic(), payload, qos, retained);
		} catch (MqttException e) {
			throw new IllegalStateException("发布失败: " + e.getMessage(), e);
		}
	}

	@Override
	public MqttClientStatus status() {
		boolean connected = (client != null && client.isConnected());
		if (!connected) {
			return MqttClientStatus.disconnected();  // 无需传 brokerUrl/clientId
		}
		return MqttClientStatus.connected(activeBrokerUrl, activeClientId,
				Map.copyOf(subscriptions), messageBuffer.size());
	}

	@Override
	public List<MqttMessageView> recentMessages(int limit) {
		int size = Math.max(1, Math.min(limit, MAX_BUFFERED_MESSAGES));
		List<MqttMessageView> snapshot = new ArrayList<>(messageBuffer);
		if (snapshot.size() <= size) {
			return snapshot;
		}
		return snapshot.subList(snapshot.size() - size, snapshot.size());
	}

	@PreDestroy
	public synchronized void shutdown() {
		closeCurrentClient();
	}

	private void ensureConnected() {
		if (client == null || !client.isConnected()) {
			throw new IllegalStateException("MQTT客户端未连接");
		}
	}

	private void closeCurrentClient() {
		if (client == null) {
			return;
		}
		try {
			if (client.isConnected()) {
				client.disconnect();
			}
			client.close();
		} catch (MqttException e) {
			log.warn("关闭MQTT客户端失败: {}", e.getMessage());
		} finally {
			client = null;
			activeBrokerUrl = null;
			activeClientId = null;
		}
	}

	private void applyWillOption(MqttConnectOptions options) {
		String willTopic = trimToNull(defaultWillTopic);
		if (!StringUtils.hasText(willTopic)) {
			return;
		}
		int willQos = normalizeQos(defaultWillQos);
		byte[] willPayload = (defaultWillPayload == null ? "" : defaultWillPayload).getBytes(StandardCharsets.UTF_8);
		options.setWill(willTopic, willPayload, willQos, defaultWillRetained);
	}

	private int normalizeQos(Integer qos) {
		if (qos == null) {
			return 0;
		}
		if (qos < 0 || qos > 2) {
			throw new IllegalArgumentException("QoS 只能是 0/1/2");
		}
		return qos;
	}

	private String trimToNull(String value) {
		if (!StringUtils.hasText(value)) {
			return null;
		}
		return value.trim();
	}

	private void persistPublishedMessage(String topic, String payload, int qos, boolean retained) {
		MqttPublishLog logEntry = new MqttPublishLog();
		logEntry.setTopic(topic);
		logEntry.setPayload(payload);
		logEntry.setQos(qos);
		logEntry.setRetained(retained);
		logEntry.setClientId(activeClientId);
		logEntry.setBrokerUrl(activeBrokerUrl);
		logEntry.setCreateTime(LocalDateTime.now());

		try {
			mqttPublishLogService.save(logEntry);
		} catch (Exception e) {
			log.error("MQTT发布日志写入数据库失败: {}", e.getMessage(), e);
		}

		try {
			stringRedisTemplate.opsForList().rightPush(PUBLISH_HISTORY_KEY, JSON.toJSONString(logEntry));
			stringRedisTemplate.opsForList().trim(PUBLISH_HISTORY_KEY, -MAX_PUBLISH_HISTORY, -1);
		} catch (Exception e) {
			log.error("MQTT发布日志写入Redis失败: {}", e.getMessage(), e);
		}
	}
}
