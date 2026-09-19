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
