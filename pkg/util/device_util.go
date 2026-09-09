package util

import (
	"crypto/md5"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// 设备相关工具函数

// NormalizeDeviceID 标准化设备ID（trim + 小写）
func NormalizeDeviceID(deviceID string) string {
	return strings.ToLower(strings.TrimSpace(deviceID))
}

// GenerateShortDeviceID 生成6位短设备ID（UUID -> MD5 -> 前6位十六进制）
func GenerateShortDeviceID() string {
	id := uuid.New()
	hash := md5.Sum([]byte(id.String()))
	return fmt.Sprintf("%x", hash)[:6]
}

// MergeDeviceWithStatus 合并设备基本信息与运行时状态为 map
func MergeDeviceWithStatus(deviceID, deviceName string, ownerID uint, status string, lastActiveTime any) map[string]any {
	result := map[string]any{
		"deviceId":   deviceID,
		"deviceName": deviceName,
		"ownerId":    ownerID,
		"status":     status,
	}
	if lastActiveTime != nil {
		result["lastActiveTime"] = lastActiveTime
	}
	return result
}
