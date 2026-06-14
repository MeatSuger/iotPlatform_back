package com.yu.iotplatform.config;

import com.yu.iotplatform.handler.Mqtt2WebSocketHandler;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.socket.config.annotation.EnableWebSocket;
import org.springframework.web.socket.config.annotation.WebSocketConfigurer;
import org.springframework.web.socket.config.annotation.WebSocketHandlerRegistry;


@Configuration
@EnableWebSocket
public class WebSocketConfig implements WebSocketConfigurer {


	private final Mqtt2WebSocketHandler mqttWebSocketHandler;

	public WebSocketConfig(Mqtt2WebSocketHandler mqttWebSocketHandler) {
		this.mqttWebSocketHandler = mqttWebSocketHandler;
	}

	@Override
	public void registerWebSocketHandlers(WebSocketHandlerRegistry registry) {
		registry.addHandler(mqttWebSocketHandler, "/ws/mqtt")
				.setAllowedOrigins("*");  // 允许跨域，生产环境需按需配置
	}
}