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

package common

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// DateTime 自定义时间类型，JSON 序列化为 "2006-01-02T15:04:05.000-07:00"
// 对齐 Java @JsonFormat（"yyyy-MM-dd'T'HH:mm:ss.SSSXXX"）格式 + 时区标记
type DateTime struct {
	time.Time
}

const (
	// DateTimeFormat DB 解析格式（无时区，PostgreSQL 返回格式）
	DateTimeFormat = "2006-01-02 15:04:05"
	// DateTimeFormatWithZone JSON / API 输出格式（RFC 3339 / ISO 8601）
	DateTimeFormatWithZone = "2006-01-02T15:04:05.000-07:00"
)

// DateTimeNow 返回当前时间
func DateTimeNow() DateTime {
	return DateTime{Time: time.Now()}
}

// DateTimeFrom 包装 time.Time
func DateTimeFrom(t time.Time) DateTime {
	return DateTime{Time: t}
}

// MarshalJSON JSON 序列化（必须值接收器：遮蔽嵌入的 time.Time.MarshalJSON）
func (dt DateTime) MarshalJSON() ([]byte, error) {
	if dt.IsZero() {
		return []byte("null"), nil
	}
	return fmt.Appendf(nil, `"%s"`, dt.Time.Local().Format(DateTimeFormatWithZone)), nil
}

// timeLayouts 兼容的时间输入格式（RFC 3339 优先，空格格式向下兼容）。
// 供 DateTime.UnmarshalJSON 与 ParseFlexibleTime 共用，新增格式只需改这里。
var timeLayouts = []string{
	DateTimeFormatWithZone,          // "2006-01-02T15:04:05.000-07:00"
	time.RFC3339,                    // "2006-01-02T15:04:05Z07:00"
	"2006-01-02T15:04:05.000",       // 毫秒无时区
	"2006-01-02 15:04:05.000-07:00", // 旧格式（毫秒 + 时区 + 空格）
	"2006-01-02 15:04:05-07:00",     // 旧格式（无毫秒 + 时区）
	DateTimeFormat,                  // "2006-01-02 15:04:05"
	"2006-01-02 15:04:05 -0700",     // 空格分隔偏移
}

// ParseFlexibleTime 宽松解析时间字符串，命中 timeLayouts 任一格式即返回 true。
// 用于设备上报等来源不可控的时间字段：解析失败返回 ok=false，由调用方决定
// 回退策略（通常用服务端当前时间），而不是把整条数据判为非法。
func ParseFlexibleTime(s string) (time.Time, bool) {
	for _, layout := range timeLayouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ParseEpochValue 把 epoch 时间数值解析为时间，兼容纳秒/微秒/毫秒/秒四种数量级。
//
// 为什么按数量级猜单位：终端（设备固件）与 InfluxDB 客户端都不保证单位统一，
// 秒级（1.7e9）、毫秒（1.7e12）、微秒（1.7e15）、纳秒（1.7e18）量级相差 3 个
// 数量级，用阈值可以无歧义区分（公元 5138 年前的秒级值都小于 1e11）。
//
// 支持 time.Time、json.Number、数字串，以及任意底层为整型/浮点的类型。
// 非正数或无法识别返回 ok=false。
func ParseEpochValue(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return epochToTime(n)
		}
		if f, err := t.Float64(); err == nil {
			return epochToTime(int64(f))
		}
		return time.Time{}, false
	case string:
		s := strings.TrimSpace(t)
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return epochToTime(n)
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return epochToTime(int64(f))
		}
		return time.Time{}, false
	}

	// 数值类型走反射按底层 Kind 取值：必须兼容**命名类型**。
	// 实测 InfluxDB 客户端对 `date_bin(...) AS time`（降采样查询）返回的是
	// arrow.Timestamp（`type Timestamp int64` 的命名类型），类型断言匹配不到内置
	// int64，会导致解析失败并把裸纳秒值漏进响应。
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return epochToTime(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return epochToTime(int64(rv.Uint()))
	case reflect.Float32, reflect.Float64:
		return epochToTime(int64(rv.Float()))
	}
	return time.Time{}, false
}

// epochToTime 按数量级把 epoch 数值转为时间（整数入参，不走 float64 以免丢精度）。
func epochToTime(n int64) (time.Time, bool) {
	if n <= 0 {
		return time.Time{}, false
	}
	switch {
	case n >= 1e17: // 纳秒
		return time.Unix(0, n), true
	case n >= 1e14: // 微秒
		return time.UnixMicro(n), true
	case n >= 1e11: // 毫秒
		return time.UnixMilli(n), true
	default: // 秒
		return time.Unix(n, 0), true
	}
}

// UnmarshalJSON JSON 反序列化
func (dt *DateTime) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" || s == `""` {
		dt.Time = time.Time{}
		return nil
	}
	s = s[1 : len(s)-1] // 去除引号

	// RFC 3339 优先，空格格式向下兼容
	if t, ok := ParseFlexibleTime(s); ok {
		dt.Time = t
		return nil
	}
	return fmt.Errorf("DateTime.UnmarshalJSON: 无法解析时间 %q", s)
}

// Value 实现 driver.Valuer（必须值接收器：遮蔽嵌入的 time.Time.Value）
func (dt DateTime) Value() (driver.Value, error) {
	if dt.IsZero() {
		return nil, nil
	}
	return dt.Time, nil
}

// Scan 实现 sql.Scanner（从 DB 读取）
func (dt *DateTime) Scan(value any) error {
	if value == nil {
		dt.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		dt.Time = v
		return nil
	case []byte:
		t, err := time.Parse(DateTimeFormat, string(v))
		if err != nil {
			return err
		}
		dt.Time = t
		return nil
	case string:
		t, err := time.Parse(DateTimeFormat, v)
		if err != nil {
			return err
		}
		dt.Time = t
		return nil
	}
	return fmt.Errorf("DateTime.Scan: 不支持的类型 %T", value)
}
