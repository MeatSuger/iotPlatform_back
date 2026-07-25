package common

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 全局 Zap Logger 实例（SugaredLogger 用于 printf 风格调用）
var Logger *zap.SugaredLogger

// InitLogger 初始化全局 Zap 日志器
// mode: "debug" → 彩色控制台；其他 → JSON 格式
func InitLogger(mode string) {
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

	logger, err := cfg.Build()
	if err != nil {
		panic("初始化日志器失败: " + err.Error())
	}

	zap.ReplaceGlobals(logger)
	Logger = logger.Sugar()
}

// Sync 刷新日志缓冲区（应在程序退出前调用）
func Sync() {
	if Logger != nil {
		_ = Logger.Sync()
	}
}
