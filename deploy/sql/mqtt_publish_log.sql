-- ===========================================
-- MQTT发布日志表（PostgreSQL）
-- ===========================================

CREATE TABLE IF NOT EXISTS "mqtt_publish_log" (
    id BIGSERIAL PRIMARY KEY,
    topic VARCHAR(255) NOT NULL,
    payload TEXT,
    qos INTEGER NOT NULL DEFAULT 0,
    retained BOOLEAN NOT NULL DEFAULT FALSE,
    client_id VARCHAR(128),
    broker_url VARCHAR(255),
    create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_mqtt_publish_log_topic ON "mqtt_publish_log" (topic);
CREATE INDEX IF NOT EXISTS idx_mqtt_publish_log_create_time ON "mqtt_publish_log" (create_time DESC);

-- ===========================================
-- 如果使用GORM AutoMigrate，以下表会自动创建：
-- - app_user (用户表)
-- - iot_device (设备表)
-- 手动创建可以参考以下DDL：
-- ===========================================

-- CREATE TABLE IF NOT EXISTS "app_user" (
--     id BIGSERIAL PRIMARY KEY,
--     name VARCHAR(100),
--     age INTEGER DEFAULT 0,
--     email VARCHAR(255),
--     account VARCHAR(100) UNIQUE NOT NULL,
--     passwd VARCHAR(255) NOT NULL,
--     status VARCHAR(50) DEFAULT 'active',
--     create_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
--     update_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
-- );

-- CREATE TABLE IF NOT EXISTS "iot_device" (
--     id BIGSERIAL PRIMARY KEY,
--     device_id VARCHAR(50) UNIQUE NOT NULL,
--     device_name VARCHAR(100),
--     device_type VARCHAR(50),
--     firmware_version VARCHAR(50),
--     ip_address VARCHAR(45),
--     mac_address VARCHAR(17),
--     location VARCHAR(255),
--     owner_id BIGINT NOT NULL,
--     status VARCHAR(50),
--     last_active_time TIMESTAMP,
--     created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
--     updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
-- );
