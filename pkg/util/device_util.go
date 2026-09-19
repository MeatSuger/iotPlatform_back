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
