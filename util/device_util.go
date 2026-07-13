package util

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// DeviceUtil 设备相关工具函数

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

// MergeDeviceWithStatus 将设备信息合并到状态对象中
// 返回一个包含设备基本信息和状态的map
func MergeDeviceWithStatus(deviceID, deviceName string, ownerID uint, status string, lastActiveTime interface{}) map[string]interface{} {
	result := map[string]interface{}{
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
