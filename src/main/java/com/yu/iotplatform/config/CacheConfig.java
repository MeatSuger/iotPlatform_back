package com.yu.iotplatform.config;

import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.yu.iotplatform.entity.Device;
import com.yu.iotplatform.entity.DeviceStatus;
import com.yu.iotplatform.entity.SensorData;
import org.springframework.cache.annotation.EnableCaching;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.data.redis.cache.RedisCacheConfiguration;
import org.springframework.data.redis.cache.RedisCacheManager;
import org.springframework.data.redis.connection.RedisConnectionFactory;
import org.springframework.data.redis.serializer.Jackson2JsonRedisSerializer;
import org.springframework.data.redis.serializer.RedisSerializationContext;
import org.springframework.data.redis.serializer.StringRedisSerializer;

import java.time.Duration;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

@Configuration
@EnableCaching
public class CacheConfig {

	public static final String CACHE_DEVICE = "device";
	public static final String CACHE_DEVICE_STATUS = "deviceStatus";
	public static final String CACHE_SENSOR_RECENT = "sensorRecent";

	private static final Duration DEVICE_TTL = Duration.ofMinutes(10);
	private static final Duration DEVICE_STATUS_TTL = Duration.ofSeconds(120);
	private static final Duration SENSOR_RECENT_TTL = Duration.ofSeconds(30);

	private final ObjectMapper mapper;

	public CacheConfig() {
		mapper = new ObjectMapper();
		mapper.registerModule(new JavaTimeModule());
	}

	@Bean
	public RedisCacheManager cacheManager(RedisConnectionFactory connectionFactory) {
		JavaType sensorListType = mapper.getTypeFactory()
				.constructCollectionType(List.class, SensorData.class);

		Map<String, RedisCacheConfiguration> cacheConfigs = new HashMap<>();
		cacheConfigs.put(CACHE_DEVICE, cacheConfig(DEVICE_TTL, Device.class));
		cacheConfigs.put(CACHE_DEVICE_STATUS, cacheConfig(DEVICE_STATUS_TTL, DeviceStatus.class));
		cacheConfigs.put(CACHE_SENSOR_RECENT, cacheConfig(SENSOR_RECENT_TTL, sensorListType));

		return RedisCacheManager.builder(connectionFactory)
				.cacheDefaults(cacheConfig(Duration.ofMinutes(5), Object.class))
				.withInitialCacheConfigurations(cacheConfigs)
				.build();
	}

	private RedisCacheConfiguration cacheConfig(Duration ttl, Class<?> type) {
		return RedisCacheConfiguration.defaultCacheConfig()
				.entryTtl(ttl)
				.serializeKeysWith(RedisSerializationContext.SerializationPair.fromSerializer(new StringRedisSerializer()))
				.serializeValuesWith(RedisSerializationContext.SerializationPair.fromSerializer(
						new Jackson2JsonRedisSerializer<>(mapper, type)))
				.disableCachingNullValues();
	}

	private RedisCacheConfiguration cacheConfig(Duration ttl, JavaType type) {
		return RedisCacheConfiguration.defaultCacheConfig()
				.entryTtl(ttl)
				.serializeKeysWith(RedisSerializationContext.SerializationPair.fromSerializer(new StringRedisSerializer()))
				.serializeValuesWith(RedisSerializationContext.SerializationPair.fromSerializer(
						new Jackson2JsonRedisSerializer<>(mapper, type)))
				.disableCachingNullValues();
	}
}
