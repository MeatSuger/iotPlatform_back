-- ============================================
-- 设备表 (iot_device)
-- 主键: device_id (VARCHAR, 应用生成6位hex)
-- 注: 应用启动时 ent 会自动迁移补齐表结构，本脚本仅用于全新环境初始化
-- 用法: psql -U <user> -d <database> -f iot_device.sql
-- ============================================

CREATE TABLE IF NOT EXISTS iot_device (
    device_id        VARCHAR(50)   PRIMARY KEY,
    device_name      VARCHAR(100)  NOT NULL DEFAULT '',
    device_type      VARCHAR(50)   NOT NULL DEFAULT '',
    firmware_version VARCHAR(50)   DEFAULT '',
    ip_address       VARCHAR(45)   DEFAULT '',
    mac_address      VARCHAR(17)   DEFAULT '',
    location         VARCHAR(255)  DEFAULT '',
    owner_id         BIGINT        NOT NULL DEFAULT 1,
    status           VARCHAR(50)   DEFAULT 'OFFLINE',
    last_active_time TIMESTAMPTZ,
    created_at       TIMESTAMPTZ   DEFAULT NOW(),
    updated_at       TIMESTAMPTZ   DEFAULT NOW(),

    CONSTRAINT chk_device_status CHECK (status IN ('ONLINE', 'OFFLINE', 'ACTIVE')),
    CONSTRAINT iot_device_app_user_devices FOREIGN KEY (owner_id) REFERENCES app_user(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS device_owner_id ON iot_device (owner_id);
