package com.yu.iotplatform.config;


import com.fasterxml.jackson.annotation.JsonAutoDetect;
import com.fasterxml.jackson.annotation.JsonTypeInfo;
import com.fasterxml.jackson.annotation.PropertyAccessor;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.databind.jsontype.impl.LaissezFaireSubTypeValidator;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.ApplicationRunner;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.data.redis.connection.RedisConnectionFactory;
import org.springframework.data.redis.connection.RedisStandaloneConfiguration;
import org.springframework.data.redis.connection.lettuce.LettuceConnectionFactory;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.data.redis.serializer.Jackson2JsonRedisSerializer;
import org.springframework.data.redis.serializer.StringRedisSerializer;

import javax.sql.DataSource;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.util.ArrayList;
import java.util.List;


@Configuration
@Slf4j
public class RedisConfig {

	@Value("${spring.data.redis.host}")
	private String redisHost;

	@Value("${spring.data.redis.port}")
	private int redisPort;

	@Value("${spring.data.redis.password}")
	private String redisPassword;

	@Value("${spring.datasource.hikari.minimum-idle:5}")
	private int minimumIdle;

	@Bean
	public RedisConnectionFactory redisConnectionFactory() {
		RedisStandaloneConfiguration config = new RedisStandaloneConfiguration(redisHost, redisPort);
		config.setPassword(redisPassword);
		return new LettuceConnectionFactory(config);
	}

	@Bean
	public RedisTemplate<String, Object> redisTemplate(RedisConnectionFactory factory) {
		RedisTemplate<String, Object> template = new RedisTemplate<>();
		template.setConnectionFactory(factory);

		// key 序列化
		template.setKeySerializer(new StringRedisSerializer());
		template.setHashKeySerializer(new StringRedisSerializer());

		// value 序列化：使用 GenericJackson2JsonRedisSerializer
		ObjectMapper mapper = new ObjectMapper();
		mapper.registerModule(new JavaTimeModule());
		mapper.disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS);
		mapper.setVisibility(PropertyAccessor.ALL, JsonAutoDetect.Visibility.ANY);
		mapper.activateDefaultTyping(LaissezFaireSubTypeValidator.instance, ObjectMapper.DefaultTyping.NON_FINAL, JsonTypeInfo.As.PROPERTY);

		Jackson2JsonRedisSerializer<Object> serializer = new Jackson2JsonRedisSerializer<>(mapper, mapper.constructType(Object.class));
		template.setValueSerializer(serializer);
		template.setHashValueSerializer(serializer);

		template.afterPropertiesSet();
		return template;
	}

	@Bean
	public ApplicationRunner dataSourceWarmupRunner(DataSource dataSource) {
		return args -> {
			long start = System.currentTimeMillis();
			int target = Math.max(1, minimumIdle);
			List<Connection> opened = new ArrayList<>(target);
			try {
				log.info("PostgreSQL warmup start, targetConnections={}", target);
				for (int i = 0; i < target; i++) {
					Connection connection = dataSource.getConnection();
					opened.add(connection);
					try (PreparedStatement ps = connection.prepareStatement("SELECT 1")) {
						ps.execute();
					}
				}
				log.info("PostgreSQL warmup success, openedConnections={}, elapsed={}ms", opened.size(), System.currentTimeMillis() - start);
			} catch (Exception e) {
				throw new IllegalStateException("PostgreSQL warmup failed on startup", e);
			} finally {
				for (Connection connection : opened) {
					try {
						connection.close();
					} catch (Exception ignored) {
					}
				}
			}
		};
	}

}
