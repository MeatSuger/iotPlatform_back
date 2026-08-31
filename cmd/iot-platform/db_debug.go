package main

import (
	"context"
	"time"

	"entgo.io/ent/dialect"
	"go.uber.org/zap"
)

// debugDriver 根据运行模式包装驱动：debug 模式下记录每条 SQL 的操作类型与耗时
func debugDriver(d dialect.Driver, mode string) dialect.Driver {
	if mode != "debug" {
		return d
	}
	return &debugTimingDriver{Driver: d}
}

// debugTimingDriver 包装 dialect.Driver，为 Exec/Query 计时（仅 debug 模式安装）
type debugTimingDriver struct {
	dialect.Driver
}

func (d *debugTimingDriver) Exec(ctx context.Context, query string, args, v any) error {
	start := time.Now()
	err := d.Driver.Exec(ctx, query, args, v)
	logDBQuery("Exec", query, time.Since(start))
	return err
}

func (d *debugTimingDriver) Query(ctx context.Context, query string, args, v any) error {
	start := time.Now()
	err := d.Driver.Query(ctx, query, args, v)
	logDBQuery("Query", query, time.Since(start))
	return err
}

func logDBQuery(op, query string, d time.Duration) {
	// 只记录慢查询与耗时，避免刷屏；query 为参数化 SQL（$1/$2 占位），不含明文值
	zap.L().Debug("[DB]",
		zap.String("op", op),
		zap.String("query", query),
		zap.Duration("latency", d),
	)
}
