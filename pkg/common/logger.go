package common

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 全局 Zap Logger 实例（SugaredLogger 用于 printf 风格调用）
var Logger *zap.SugaredLogger

// InitLogger 初始化全局 Zap 日志器
// mode: "debug" → 彩色控制台；其他 → JSON 格式
// logLevel: debug, info, warn, error（空则默认 debug→debug, 其他→info）
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
