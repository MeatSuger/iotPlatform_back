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
