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

package controller

import (
	"context"
	"testing"
	"time"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
)

// seedOwnedDevice 创建属主用户 + 指定设备，返回 ownerID（供各控制器测试环境复用）
func seedOwnedDevice(t *testing.T, client *ent.Client, deviceID, deviceName string) uint {
	t.Helper()
	now := time.Now()
	owner, err := repository.NewUserRepo(client).Create(context.Background(), &ent.User{
		Account: "owner_" + deviceID, Passwd: "h", Role: "user", Status: "ACTIVE",
		CreateTime: now, UpdateTime: now,
	})
	if err != nil {
		t.Fatalf("种子用户失败: %v", err)
	}
	_, err = repository.NewDeviceRepo(client).Create(context.Background(), &ent.Device{
		ID: deviceID, DeviceName: deviceName, OwnerID: owner.ID, Status: "ONLINE",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("种子设备失败: %v", err)
	}
	return owner.ID
}
