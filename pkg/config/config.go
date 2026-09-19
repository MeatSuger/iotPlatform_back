// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Config 全局配置结构
type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	Database    DatabaseConfig    `mapstructure:"database"`
	Redis       RedisConfig       `mapstructure:"redis"`
	InfluxDB    InfluxDBConfig    `mapstructure:"influxdb"`
	MQTT        MQTTConfig        `mapstructure:"mqtt"`
	MqttGateway MqttGatewayConfig `mapstructure:"mqtt-gateway"`
	CORS        CORSConfig        `mapstructure:"cors"`
	Device      DeviceConfig      `mapstructure:"device"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port           int    `mapstructure:"port"`
	UDPPort        int    `mapstructure:"udp-port"`        // UDP 设备上报端口，0=禁用
	Mode           string `mapstructure:"mode"`            // debug, release, test
	LogLevel       string `mapstructure:"log-level"`       // 日志级别: debug, info, warn, error（默认: debug模式下debug，其他info）
	SwaggerEnabled bool   `mapstructure:"swagger-enabled"` // 是否启用 Swagger 文档（生产环境关闭，默认 true）
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

// DSN 返回 PostgreSQL 连接字符串
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=Asia/Shanghai",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode)
}

// RedisConfig Redis 配置
// Mode 支持: "standalone"（默认单节点）、"sentinel"（哨兵高可用）；"cluster" 暂不支持
type RedisConfig struct {
	Mode          string `mapstructure:"mode"` // standalone | sentinel（cluster 暂不支持）
	Host          string `mapstructure:"host"`
	Port          int    `mapstructure:"port"`
	Password      string `mapstructure:"password"`
	DB            int    `mapstructure:"db"`
	MasterName    string `mapstructure:"master-name"`    // Sentinel 主节点名称
	SentinelAddrs string `mapstructure:"sentinel-addrs"` // Sentinel 地址列表（逗号分隔）
	ClientName    string `mapstructure:"client-name"`    // Redis 连接标识（CLIENT LIST 可辨识）
	PoolSize      int    `mapstructure:"pool-size"`
	MinIdleConns  int    `mapstructure:"min-idle-conns"`
	DialTimeout   int    `mapstructure:"dial-timeout"`
	ReadTimeout   int    `mapstructure:"read-timeout"`
	WriteTimeout  int    `mapstructure:"write-timeout"`
	PoolTimeout   int    `mapstructure:"pool-timeout"`
	MaxRetries    int    `mapstructure:"max-retries"`
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
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return
}

// InfluxDBConfig InfluxDB v3 配置（v3 无 org/bucket 概念，统一使用 database）
type InfluxDBConfig struct {
	URL                string `mapstructure:"url"`
	Token              string `mapstructure:"token"`
	Database           string `mapstructure:"database"`
	AuthScheme         string `mapstructure:"auth-scheme"`          // Token(Cloud) | Bearer(Core/Edge自部署)，默认 Token
	MaxIdleConnections int    `mapstructure:"max-idle-connections"` // 最大空闲连接数，默认 50
}

// MQTTConfig MQTT Broker 连接地址（供网关透明转发）
type MQTTConfig struct {
	BrokerURL string `mapstructure:"broker-url"` // 外部 MQTT Docker 地址，如 tcp://mqtt:1883
}

// MqttGatewayConfig MQTT 桥接网关配置（胶水层：设备 WSS 接入 → 框架鉴权 → 透明转发到外部 MQTT Docker）
// 设备连接: wss://<host>/api/ws/mqtt/broker?X-Device-Token=<token>，与 HTTP 同端口
// 鉴权由 DeviceAuthMiddleware（Sa-Token）接管
type MqttGatewayConfig struct {
	Enabled bool `mapstructure:"enabled"`

	// IdleTimeoutSec 设备连接空闲看护阈值（秒）：连接在无任何 MQTT 帧（含 PINGREQ 心跳）
	// 持续该时长后判定设备失联，强制断开并置为离线。
	// 0（默认）= 尽量跟随设备 CONNECT 携带的 keepalive（1.5×keepalive，夹在 15s~30min）；
	// 设备 keepalive 禁用（0）或无 keepalive 时才回退到本配置，再未配置则兜底 5min。
	IdleTimeoutSec int `mapstructure:"idle-timeout-sec"`
}

// DeviceConfig 设备离线检测配置（Redis 状态为准，离线后同步 PostgreSQL）
type DeviceConfig struct {
	OfflineScanInterval int `mapstructure:"offline-scan-interval"` // 扫描间隔（秒），默认 30
	OfflineThreshold    int `mapstructure:"offline-threshold"`     // 离线判定阈值（秒），默认 120（2×心跳周期）
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

	// Swagger 默认开启（仅生产环境显式关闭），保证旧配置无需改动
	v.SetDefault("server.swagger-enabled", true)

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
