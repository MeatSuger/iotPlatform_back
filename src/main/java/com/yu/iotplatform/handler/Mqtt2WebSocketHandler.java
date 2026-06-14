package com.yu.iotplatform.handler;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.yu.iotplatform.entity.mqtt.MqttPublishRequest;
import com.yu.iotplatform.service.MqttClientService;
import jakarta.websocket.server.ServerEndpoint;
import lombok.extern.slf4j.Slf4j;
import org.jspecify.annotations.NonNull;
import org.springframework.stereotype.Component;
import org.springframework.web.socket.CloseStatus;
import org.springframework.web.socket.TextMessage;
import org.springframework.web.socket.WebSocketSession;
import org.springframework.web.socket.handler.TextWebSocketHandler;

import java.io.IOException;
import java.util.concurrent.CopyOnWriteArraySet;

@Slf4j
@Component
@ServerEndpoint("/ws/mqtt")
public class Mqtt2WebSocketHandler extends TextWebSocketHandler {

	// 保存所有连接的会话
	private static final CopyOnWriteArraySet<WebSocketSession> sessions = new CopyOnWriteArraySet<>();
	private final ObjectMapper objectMapper = new ObjectMapper();
	private final MqttClientService mqttClientService;

	public Mqtt2WebSocketHandler(MqttClientService mqttClientService) {
		this.mqttClientService = mqttClientService;
	}

	@Override
	public void afterConnectionEstablished(@NonNull WebSocketSession session) {
		sessions.add(session);
		log.info("WebSocket 连接建立，Session ID: {}，当前连接数: {}", session.getId(), sessions.size());
	}

	@Override
	public void afterConnectionClosed(@NonNull WebSocketSession session, @NonNull CloseStatus status) {
		sessions.remove(session);
		log.info("WebSocket 连接关闭，Session ID: {}，剩余连接数: {}", session.getId(), sessions.size());
	}

	@Override
	protected void handleTextMessage(WebSocketSession session, TextMessage message) throws Exception {
		String payload = message.getPayload();
		log.info("收到客户端消息: \n{}\nSession: {}\n", payload, session.getId());
		try {
			// 将 JSON 字符串转换为 MqttPublishRequest
			MqttPublishRequest request = objectMapper.readValue(payload, MqttPublishRequest.class);
			// 发布到 MQTT Broker
			mqttClientService.publish(request);
		} catch (Exception e) {
			log.error("解析或发布消息失败", e);
			// 可选：向客户端发送错误响应
			session.sendMessage(new TextMessage("""
					{"error":"Invalid request"}"""));
		}
	}

	@Override
	public void handleTransportError(WebSocketSession session, @NonNull Throwable exception) throws Exception {
		log.error("WebSocket 传输错误，Session: {}", session.getId(), exception);
		sessions.remove(session);
		try {
			session.close(CloseStatus.SERVER_ERROR);
		} catch (IOException ignored) {
		}
	}

	/**
	 * 广播消息给所有客户端
	 */
	public static void broadcast(String message) {
		if (sessions.isEmpty()) {
			return;
		}
		for (WebSocketSession session : sessions) {
			if (session.isOpen()) {
				try {
					session.sendMessage(new TextMessage(message));
				} catch (IOException e) {
					log.error("发送消息失败，Session ID: {}", session.getId(), e);
				}
			}
		}
	}
}
