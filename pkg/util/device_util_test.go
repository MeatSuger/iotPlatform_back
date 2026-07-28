package util

import (
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

func TestIsValidDeviceID_ShouldValidateHex6(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"abc123", true},
		{"123456", true},
		{"abcdef", true},
		{"ABCDEF", true},   // 大写自动转小写后合法
		{"abc12", false},   // too short
		{"abc1234", false}, // too long
		{"ghijk1", false},  // invalid hex
		{"", false},
		{"!@#$%^", false},
	}

	for _, tt := range tests {
		result := IsValidDeviceID(tt.input)
		if result != tt.expected {
			t.Errorf("IsValidDeviceID(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestGenerateShortDeviceID_ShouldBeLowerHexWithLength6(t *testing.T) {
	for range 100 {
		id := GenerateShortDeviceID()
		if len(id) != 6 {
			t.Errorf("GenerateShortDeviceID() length = %d, want 6", len(id))
		}
		if !IsValidDeviceID(id) {
			t.Errorf("GenerateShortDeviceID() = %q, not a valid 6-char hex", id)
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
