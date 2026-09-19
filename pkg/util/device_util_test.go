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

package util

import (
	"strings"
	"testing"
)

func TestNormalizeDeviceID_ShouldTrimAndLowercase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  ABC123  ", "abc123"},
		{"ABC123", "abc123"},
		{"  abc123", "abc123"},
		{"ABC123  ", "abc123"},
		{"", ""},
		{"  A B C  ", "a b c"},
	}

	for _, tt := range tests {
		result := NormalizeDeviceID(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeDeviceID(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestGenerateShortDeviceID_ShouldBeLowerHexWithLength6(t *testing.T) {
	hexChars := "0123456789abcdef"
	for range 100 {
		id := GenerateShortDeviceID()
		if len(id) != 6 {
			t.Errorf("GenerateShortDeviceID() length = %d, want 6", len(id))
		}
		for _, r := range id {
			if !strings.ContainsRune(hexChars, r) {
				t.Errorf("GenerateShortDeviceID() = %q, not lowercase hex", id)
				break
			}
		}
	}
}

func TestMergeDeviceWithStatus_ShouldCreateWhenStatusNull(t *testing.T) {
	result := MergeDeviceWithStatus("abc123", "TestDevice", 1, "ONLINE", nil)
	if result["deviceId"] != "abc123" {
		t.Errorf("deviceId = %v, want abc123", result["deviceId"])
	}
	if result["deviceName"] != "TestDevice" {
		t.Errorf("deviceName = %v, want TestDevice", result["deviceName"])
	}
	if result["status"] != "ONLINE" {
		t.Errorf("status = %v, want ONLINE", result["status"])
	}
}

func TestMergeDeviceWithStatus_ShouldKeepLastActiveWhenPresent(t *testing.T) {
	lastActive := "2024-01-01T00:00:00Z"
	result := MergeDeviceWithStatus("abc123", "TestDevice", 1, "ONLINE", lastActive)
	if result["lastActiveTime"] != lastActive {
		t.Errorf("lastActiveTime = %v, want %v", result["lastActiveTime"], lastActive)
	}
}
