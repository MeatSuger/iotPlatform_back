package common

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// DateTime 自定义时间类型，JSON 序列化为 "yyyy-MM-dd HH:mm:ss±07:00"
// 对齐 Java @JsonFormat 格式 + 时区标记
type DateTime struct {
	time.Time
}

const (
	// DateTimeFormat 输出格式（无时区，用于 DB 和控制器响应拼接）
	DateTimeFormat = "2006-01-02 15:04:05"
	// DateTimeFormatWithZone JSON 序列化格式（带时区偏移）
	DateTimeFormatWithZone = "2006-01-02 15:04:05-07:00"
)

// DateTimeNow 返回当前时间
func DateTimeNow() DateTime {
	return DateTime{Time: time.Now()}
}

// DateTimeFrom 包装 time.Time
func DateTimeFrom(t time.Time) DateTime {
	return DateTime{Time: t}
}

// MarshalJSON JSON 序列化（本地时区 + 偏移标记）
func (dt DateTime) MarshalJSON() ([]byte, error) {
	if dt.IsZero() {
		return []byte("null"), nil
	}
	return fmt.Appendf(nil, `"%s"`, dt.Time.Local().Format(DateTimeFormatWithZone)), nil
}

// UnmarshalJSON JSON 反序列化（兼容多种格式）
func (dt *DateTime) UnmarshalJSON(data []byte) error {
	s := string(data)
	if s == "null" || s == `""` {
		dt.Time = time.Time{}
		return nil
	}
	s = s[1 : len(s)-1] // 去除引号

	// 按优先级尝试解析
	layouts := []string{
		DateTimeFormatWithZone,      // "2006-01-02 15:04:05-07:00"
		DateTimeFormat,              // "2006-01-02 15:04:05"
		"2006-01-02 15:04:05 -0700", // 空格分隔偏移
		time.RFC3339,                // "2006-01-02T15:04:05Z07:00"
		"2006-01-02T15:04:05.000",   // ISO 毫秒无时区
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

// Value 实现 driver.Valuer（GORM 写入 DB）
func (dt DateTime) Value() (driver.Value, error) {
	if dt.IsZero() {
		return nil, nil
	}
	return dt.Time, nil
}

// Scan 实现 sql.Scanner（GORM 从 DB 读取）
func (dt *DateTime) Scan(value interface{}) error {
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
