package common

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 全局 Zap Logger 实例（SugaredLogger 用于 printf 风格调用）
var Logger *zap.SugaredLogger

// InitLogger 初始化全局 Zap 日志器
// mode: "debug" → 彩色控制台（开发环境）；其他 → JSON 输出（生产环境，便于采集）
// logLevel: debug, info, warn, error（空则默认 debug→debug, 其他→info）
//
// 级别策略（配合 internal/middleware.Logger 与各业务日志）：
//   - 开发环境（mode: debug, log-level: debug）：输出全部日志（含高频数据路径、成功请求），方便排查
//   - 生产环境（mode: release, log-level: info）：只输出必要日志
//     启动/关闭、连接事件、登录、命令下发等 Info + 警告/错误 + 失败请求(4xx/5xx)；
//     高频数据路径（UDP上报/WS消息/InfluxDB写入/MQTT入库）与成功请求为 Debug，生产不输出
func InitLogger(mode, logLevel string) {
	var cfg zap.Config

	if mode == "debug" {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig.TimeKey = "timestamp"
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	// 设置日志级别
	cfg.Level = parseLevel(logLevel, mode)

	logger, err := cfg.Build()
	if err != nil {
		panic("初始化日志器失败: " + err.Error())
	}

	zap.ReplaceGlobals(logger)
	Logger = logger.Sugar()
}

// parseLevel 解析日志级别字符串
func parseLevel(level, mode string) zap.AtomicLevel {
	switch strings.ToLower(level) {
	case "debug":
		return zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "info":
		return zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn", "warning":
		return zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		return zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	default:
		// 未配置时：debug 模式默认 debug，其他默认 info
		if mode == "debug" {
			return zap.NewAtomicLevelAt(zapcore.DebugLevel)
		}
		return zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}
}

// Sync 刷新日志缓冲区（应在程序退出前调用）
func Sync() {
	if Logger != nil {
		_ = Logger.Sync()
	}
}
