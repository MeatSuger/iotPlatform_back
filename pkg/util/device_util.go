package util

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// 设备相关工具函数

const (
	// DeviceOnlineStatus 设备在线状态
	DeviceOnlineStatus = "ONLINE"
	// HeartbeatIntervalSeconds 心跳间隔（秒）
	HeartbeatIntervalSeconds = 60
)

// NormalizeDeviceID 标准化设备ID（trim + 小写）
func NormalizeDeviceID(deviceID string) string {
	return strings.ToLower(strings.TrimSpace(deviceID))
}

// IsValidDeviceID 校验设备ID是否为6位十六进制字符（大写强制转小写后再校验）
func IsValidDeviceID(deviceID string) bool {
	deviceID = NormalizeDeviceID(deviceID)
	matched, _ := regexp.MatchString(`^[0-9a-f]{6}$`, deviceID)
	return matched
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
