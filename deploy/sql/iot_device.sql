-- ============================================
-- 设备表 (iot_device)
-- 主键: device_id (6位十六进制，由应用生成)
-- 用法: psql -U <user> -d <database> -f iot_device.sql
-- ============================================

CREATE TABLE IF NOT EXISTS iot_device (
    device_id         VARCHAR(50)  PRIMARY KEY,
    device_name       VARCHAR(100) DEFAULT '',
    device_type       VARCHAR(50)  DEFAULT '',
    firmware_version  VARCHAR(50)  DEFAULT '',
    ip_address        VARCHAR(45)  DEFAULT '',
    mac_address       VARCHAR(17)  DEFAULT '',
    location          VARCHAR(255) DEFAULT '',
    owner_id          BIGINT       DEFAULT 1,
    status            VARCHAR(50)  DEFAULT 'OFFLINE',
    last_active_time  TIMESTAMPTZ,
    created_at        TIMESTAMPTZ  DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_iot_device_owner_id ON iot_device (owner_id);
