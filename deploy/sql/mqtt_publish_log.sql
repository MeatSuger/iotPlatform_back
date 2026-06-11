CREATE TABLE IF NOT EXISTS "mqtt_publish_log"
(
    id BIGSERIAL PRIMARY KEY,
    topic       VARCHAR(255) NOT NULL,
    payload     TEXT,
    qos         INTEGER      NOT NULL DEFAULT 0,
    retained    BOOLEAN      NOT NULL DEFAULT FALSE,
    client_id   VARCHAR(128),
    broker_url  VARCHAR(255),
    create_time TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mqtt_publish_log_topic ON "mqtt_publish_log" (topic);
CREATE INDEX IF NOT EXISTS idx_mqtt_publish_log_create_time ON "mqtt_publish_log" (create_time DESC);
