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
	"fmt"
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

// UnmarshalJSON JSON 反序列化
func (dt *DateTime) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" || s == `""` {
		dt.Time = time.Time{}
		return nil
	}
	s = s[1 : len(s)-1] // 去除引号

	// RFC 3339 优先，空格格式向下兼容
	layouts := []string{
		DateTimeFormatWithZone,          // "2006-01-02T15:04:05.000-07:00"
		time.RFC3339,                    // "2006-01-02T15:04:05Z07:00"
		"2006-01-02T15:04:05.000",       // 毫秒无时区
		"2006-01-02 15:04:05.000-07:00", // 旧格式（毫秒 + 时区 + 空格）
		"2006-01-02 15:04:05-07:00",     // 旧格式（无毫秒 + 时区）
		DateTimeFormat,                  // "2006-01-02 15:04:05"
		"2006-01-02 15:04:05 -0700",     // 空格分隔偏移
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			dt.Time = t
			return nil
		}
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
