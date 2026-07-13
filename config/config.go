package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Config 全局配置结构
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	InfluxDB InfluxDBConfig `mapstructure:"influxdb"`
	MQTT     MQTTConfig     `mapstructure:"mqtt"`
	CORS     CORSConfig     `mapstructure:"cors"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port        int    `mapstructure:"port"`
	ContextPath string `mapstructure:"context-path"`
	Mode        string `mapstructure:"mode"` // debug, release, test
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	SSLMode  string `mapstructure:"sslmode"`
	MaxOpen  int    `mapstructure:"max-open"`
	MaxIdle  int    `mapstructure:"max-idle"`
}

// DSN 返回PostgreSQL连接字符串
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=Asia/Shanghai",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode)
}

// RedisConfig Redis配置
type RedisConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	Password     string `mapstructure:"password"`
	DB           int    `mapstructure:"db"`
	PoolSize     int    `mapstructure:"pool-size"`
	MinIdleConns int    `mapstructure:"min-idle-conns"`
	DialTimeout  int    `mapstructure:"dial-timeout"`
	ReadTimeout  int    `mapstructure:"read-timeout"`
	WriteTimeout int    `mapstructure:"write-timeout"`
	PoolTimeout  int    `mapstructure:"pool-timeout"`
	MaxRetries   int    `mapstructure:"max-retries"`
}

// Addr 返回Redis地址
func (r RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

// PoolConfig 返回连接池配置，缺失值使用文档推荐的默认值
func (r RedisConfig) PoolConfig() (poolSize, minIdle, dialTimeout, readTimeout, writeTimeout, poolTimeout, maxRetries int) {
	poolSize = r.PoolSize
	if poolSize <= 0 {
		poolSize = 100
	}
	minIdle = r.MinIdleConns
	if minIdle <= 0 {
		minIdle = 10
	}
	dialTimeout = r.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5
	}
	readTimeout = r.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 3
	}
	writeTimeout = r.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = 3
	}
	poolTimeout = r.PoolTimeout
	if poolTimeout <= 0 {
		poolTimeout = 4
	}
	maxRetries = r.MaxRetries
	if maxRetries < 0 {
		maxRetries = 3
	}
	return
}

// InfluxDBConfig InfluxDB配置
type InfluxDBConfig struct {
	URL    string `mapstructure:"url"`
	Token  string `mapstructure:"token"`
	Org    string `mapstructure:"org"`
	Bucket string `mapstructure:"bucket"`
}

// MQTTConfig MQTT配置
type MQTTConfig struct {
	BrokerURL string   `mapstructure:"broker-url"`
	ClientID  string   `mapstructure:"client-id"`
	Username  string   `mapstructure:"username"`
	Password  string   `mapstructure:"password"`
	Topics    []string `mapstructure:"topics"`
	Qos       byte     `mapstructure:"qos"`
	KeepAlive int      `mapstructure:"keep-alive"`
}

// CORSConfig 跨域配置
type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed-origins"`
}

// Cfg 全局配置实例
var Cfg *Config

// Load 加载配置文件
func Load(configPath string) error {
	v := viper.New()

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("../")
	}

	// 环境变量覆盖（支持嵌套键，如 DATABASE_HOST 覆盖 database.host）
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	Cfg = &Config{}
	if err := v.Unmarshal(Cfg); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	zap.L().Info("[Config] 配置加载成功",
		zap.Int("server.port", Cfg.Server.Port),
		zap.String("db.host", Cfg.Database.Host),
	)
	return nil
}
