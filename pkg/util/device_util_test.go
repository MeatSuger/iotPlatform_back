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
