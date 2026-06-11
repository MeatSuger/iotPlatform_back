package com.yu.iotplatform.config;

import com.influxdb.client.InfluxDBClient;
import com.influxdb.client.InfluxDBClientFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class InfluxDBConfig {

	@Value("${spring.data.influx.url}")
	public String influxUrl;

	@Value("${spring.data.influx.token}")
	public String token;

	@Value("${spring.data.influx.org}")
	public String org;

	@Value("${spring.data.influx.bucket}")
	public String bucket;

	@Bean
	public InfluxDBClient influxDBClient() {
		return InfluxDBClientFactory.create(influxUrl, token.toCharArray(), org, bucket);
	}
}