package middleware

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_normalizeDeviceID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"ABC123", "ABC123"},
		{"abc123", "abc123"},
		{"ABCDEF", "ABCDEF"},
		{"123abc", "123abc"},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, normalizeDeviceID(tt.in))
	}
}

func Test_isValidDeviceID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{"abc123", true},
		{"ABCDEF", true},
		{"123456", true},
		{"abc12", false},
		{"abc1234", false},
		{"abc12g", false},
		{"", false},
		{"GHIJKL", false}, // G 以上不合法
	}
	for _, tt := range tests {
		assert.Equal(t, tt.valid, isValidDeviceID(tt.id), "id=%q", tt.id)
	}
}

func ExampleDeviceIDAuthMiddleware() {
	// 设备 ID 认证中间件使用示例
	fmt.Println("DeviceIDAuthMiddleware validates 6-char hex device IDs from URL path")
	// Output: DeviceIDAuthMiddleware validates 6-char hex device IDs from URL path
}
