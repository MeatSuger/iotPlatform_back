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
	// Debug 级别输出每条查询（query 为参数化 SQL $1/$2，不含明文值）
	zap.L().Debug("[DB]",
		zap.String("op", op),
		zap.String("query", query),
		zap.Duration("latency", d),
	)
}
