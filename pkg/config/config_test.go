package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDatabaseConfig_DSN(t *testing.T) {
	db := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "secret",
		DBName:   "iot_platform",
		SSLMode:  "disable",
	}
	dsn := db.DSN()
	assert.Contains(t, dsn, "host=localhost")
	assert.Contains(t, dsn, "port=5432")
	assert.Contains(t, dsn, "user=postgres")
	assert.Contains(t, dsn, "password=secret")
	assert.Contains(t, dsn, "dbname=iot_platform")
	assert.Contains(t, dsn, "sslmode=disable")
	assert.Contains(t, dsn, "TimeZone=Asia/Shanghai")
}

func TestRedisConfig_Addr(t *testing.T) {
	r := RedisConfig{Host: "127.0.0.1", Port: 6379}
	assert.Equal(t, "127.0.0.1:6379", r.Addr())
}

func TestRedisConfig_PoolConfig_Defaults(t *testing.T) {
	r := RedisConfig{}
	poolSize, minIdle, dialTimeout, readTimeout, writeTimeout, poolTimeout, maxRetries := r.PoolConfig()
	assert.Equal(t, 100, poolSize)
	assert.Equal(t, 10, minIdle)
	assert.Equal(t, 5, dialTimeout)
	assert.Equal(t, 3, readTimeout)
	assert.Equal(t, 3, writeTimeout)
	assert.Equal(t, 4, poolTimeout)
	// maxRetries defaults when 0: uses default 3 (since 0 is not < 0)
	assert.Equal(t, 0, maxRetries)
}

func TestRedisConfig_PoolConfig_Custom(t *testing.T) {
	r := RedisConfig{
		PoolSize:     50,
		MinIdleConns: 5,
		DialTimeout:  10,
		ReadTimeout:  5,
		WriteTimeout: 5,
		PoolTimeout:  6,
		MaxRetries:   2,
	}
	poolSize, minIdle, dialTimeout, readTimeout, writeTimeout, poolTimeout, maxRetries := r.PoolConfig()
	assert.Equal(t, 50, poolSize)
	assert.Equal(t, 5, minIdle)
	assert.Equal(t, 10, dialTimeout)
	assert.Equal(t, 5, readTimeout)
	assert.Equal(t, 5, writeTimeout)
	assert.Equal(t, 6, poolTimeout)
	assert.Equal(t, 2, maxRetries)
}

func TestRedisConfig_PoolConfig_NegativeRetries(t *testing.T) {
	r := RedisConfig{MaxRetries: -1}
	_, _, _, _, _, _, maxRetries := r.PoolConfig()
	assert.Equal(t, 3, maxRetries) // negative → default
}

func TestLoad_FileNotFound(t *testing.T) {
	err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "读取配置文件失败")
}

func TestLoad_ValidConfig(t *testing.T) {
	// Create a temporary YAML config
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := `
server:
  port: 8080
  udp-port: 0
  context-path: /api
  mode: debug
database:
  host: localhost
  port: 5432
  user: test
  password: test
  dbname: iot_test
  sslmode: disable
redis:
  host: localhost
  port: 6379
  password: ""
  db: 0
  pool-size: 100
  min-idle-conns: 10
  dial-timeout: 5
  read-timeout: 3
  write-timeout: 3
  pool-timeout: 4
influxdb:
  url: http://localhost:8086
  token: my-token
  org: my-org
  bucket: my-bucket
mqtt:
  enabled: false
  broker-url: tcp://localhost:1883
  client-id: test-client
  topics: ["iot/#"]
  qos: 1
  keep-alive: 60
cors:
  allowed-origins: ["*"]
`
	err := os.WriteFile(configPath, []byte(content), 0644)
	assert.NoError(t, err)

	err = Load(configPath)
	assert.NoError(t, err)
	assert.NotNil(t, Cfg)
	assert.Equal(t, 8080, Cfg.Server.Port)
	assert.Equal(t, "debug", Cfg.Server.Mode)
	assert.Equal(t, "localhost", Cfg.Database.Host)
	assert.Equal(t, 5432, Cfg.Database.Port)
	assert.Equal(t, "localhost", Cfg.Redis.Host)
	assert.Equal(t, 6379, Cfg.Redis.Port)
	assert.Equal(t, "http://localhost:8086", Cfg.InfluxDB.URL)
	assert.Equal(t, "my-token", Cfg.InfluxDB.Token)
	assert.Equal(t, "my-org", Cfg.InfluxDB.Org)
	assert.Equal(t, "my-bucket", Cfg.InfluxDB.Bucket)
	assert.False(t, Cfg.MQTT.Enabled)
	assert.Equal(t, "tcp://localhost:1883", Cfg.MQTT.BrokerURL)
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(configPath, []byte("invalid: yaml: ["), 0644)
	assert.NoError(t, err)

	err = Load(configPath)
	assert.Error(t, err)
}

func TestConfig_StructDefaults(t *testing.T) {
	cfg := &Config{}
	assert.Equal(t, 0, cfg.Server.Port)
	assert.Equal(t, 0, cfg.Server.UDPPort)
	assert.Empty(t, cfg.Server.Mode)
}
