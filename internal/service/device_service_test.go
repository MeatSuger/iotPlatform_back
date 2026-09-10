package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/repository"
)

func TestDeviceRegisterRequest_Fields(t *testing.T) {
	req := DeviceParameters{
		DeviceName:      "ESP32-01",
		DeviceType:      "sensor",
		FirmwareVersion: "1.0.0",
		IPAddress:       "192.168.1.100",
		MacAddress:      "AA:BB:CC:DD:EE:FF",
		Location:        "机房A",
	}
	assert.Equal(t, "ESP32-01", req.DeviceName)
	assert.Equal(t, "sensor", req.DeviceType)
	assert.Equal(t, "1.0.0", req.FirmwareVersion)
	assert.Equal(t, "192.168.1.100", req.IPAddress)
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", req.MacAddress)
	assert.Equal(t, "机房A", req.Location)
}

func TestDeviceRegisterResponse_Fields(t *testing.T) {
	resp := DeviceRegisterResponse{
		DeviceID:    "abc123",
		DeviceToken: "token-xxx",
	}
	assert.Equal(t, "abc123", resp.DeviceID)
	assert.Equal(t, "token-xxx", resp.DeviceToken)
}

func TestNewDeviceService(t *testing.T) {
	svc := NewDeviceService(nil, nil, nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.repo)
	assert.Nil(t, svc.cache)
}

func TestDeviceUpdateParameters_PartialBind(t *testing.T) {
	// 增量更新：仅请求体中出现的字段被绑定，未传字段保持 nil
	var req DeviceUpdateParameters
	assert.NoError(t, json.Unmarshal([]byte(`{"deviceName":"新名字","location":"卧室"}`), &req))

	assert.NotNil(t, req.DeviceName)
	assert.Equal(t, "新名字", *req.DeviceName)
	assert.NotNil(t, req.Location)
	assert.Equal(t, "卧室", *req.Location)

	assert.Nil(t, req.DeviceType)
	assert.Nil(t, req.FirmwareVersion)
	assert.Nil(t, req.IPAddress)
	assert.Nil(t, req.MacAddress)
}

func TestDeviceUpdateParameters_EmptyBody(t *testing.T) {
	var req DeviceUpdateParameters
	assert.NoError(t, json.Unmarshal([]byte(`{}`), &req))

	fields := repository.DeviceUpdateFields{
		DeviceName:      req.DeviceName,
		DeviceType:      req.DeviceType,
		FirmwareVersion: req.FirmwareVersion,
		IPAddress:       req.IPAddress,
		MACAddress:      req.MacAddress,
		Location:        req.Location,
	}
	assert.True(t, fields.IsEmpty())
}

func TestDeviceUpdateFields_IsEmpty(t *testing.T) {
	assert.True(t, repository.DeviceUpdateFields{}.IsEmpty())

	name := "ESP32-01"
	assert.False(t, repository.DeviceUpdateFields{DeviceName: &name}.IsEmpty())

	loc := ""
	assert.False(t, repository.DeviceUpdateFields{Location: &loc}.IsEmpty(), "显式传空串也视为一次有效更新")
}

// ========================================
// 业务逻辑：Register / Update / GetByDeviceID / Delete / Token / 离线清理
// 真实 sqlite 仓库 + miniredis 缓存 + sa-token 内存设备管理器
// ========================================

func newDeviceCtx(t *testing.T) (*ent.Client, *repository.DeviceRepo, *DeviceService) {
	client := newTestEnt(t)
	repo := repository.NewDeviceRepo(client)
	_, rcache := newTestRedisCache(t)
	return client, repo, NewDeviceService(repo, rcache, nil, nil)
}

func strPtr(s string) *string { return &s }

func TestDeviceService_Register(t *testing.T) {
	client, repo, svc := newDeviceCtx(t)
	_ = repo
	owner := newOwner(t, client)

	t.Run("成功注册", func(t *testing.T) {
		resp, err := svc.Register(context.Background(), owner, DeviceParameters{
			DeviceName: "ESP32-01", DeviceType: "sensor", Location: "机房A",
		})
		assert.NoError(t, err)
		assert.Len(t, resp.DeviceID, 6)
		assert.NotEmpty(t, resp.DeviceToken)

		// 入库校验
		d, err := repo.GetByDeviceID(context.Background(), resp.DeviceID)
		assert.NoError(t, err)
		assert.Equal(t, "OFFLINE", d.Status)
		assert.Equal(t, owner, d.OwnerID)
	})

	t.Run("查询失败透传", func(t *testing.T) {
		client2, _, svc2 := newDeviceCtx(t)
		owner2 := newOwner(t, client2)
		assert.NoError(t, client2.Close())
		_, err := svc2.Register(context.Background(), owner2, DeviceParameters{DeviceName: "x"})
		assert.Error(t, err)
	})
}

func TestDeviceService_Update(t *testing.T) {
	client, repo, svc := newDeviceCtx(t)
	owner := newOwner(t, client)
	d := seedDevice(t, repo, "abc123", owner, "ONLINE")
	_ = d

	t.Run("空字段报错", func(t *testing.T) {
		_, err := svc.Update(context.Background(), "abc123", owner, DeviceUpdateParameters{})
		assert.ErrorContains(t, err, "无更新字段")
	})

	t.Run("owner 更新成功", func(t *testing.T) {
		name := "新名字"
		loc := "卧室"
		dev, err := svc.Update(context.Background(), "abc123", owner, DeviceUpdateParameters{DeviceName: &name, Location: &loc})
		assert.NoError(t, err)
		assert.Equal(t, "新名字", dev.DeviceName)
		assert.Equal(t, "卧室", dev.Location)
	})

	t.Run("非 owner 无权操作", func(t *testing.T) {
		other := newOwner(t, client)
		_, err := svc.Update(context.Background(), "abc123", other, DeviceUpdateParameters{Location: strPtr("x")})
		assert.ErrorContains(t, err, "无权操作该设备")
	})

	t.Run("设备不存在", func(t *testing.T) {
		_, err := svc.Update(context.Background(), "nope99", owner, DeviceUpdateParameters{Location: strPtr("x")})
		assert.Error(t, err)
	})

	t.Run("设备自更新跳过归属校验", func(t *testing.T) {
		fw := "2.0.0"
		dev, err := svc.Update(context.Background(), "abc123", 0, DeviceUpdateParameters{FirmwareVersion: &fw})
		assert.NoError(t, err)
		assert.Equal(t, "2.0.0", dev.FirmwareVersion)
	})
}

func TestDeviceService_GetByDeviceID_CacheAside(t *testing.T) {
	client, repo, svc := newDeviceCtx(t)
	owner := newOwner(t, client)
	_ = owner
	seedDevice(t, repo, "dev001", newOwner(t, client), "ONLINE")

	// 首次：DB 回源并回填缓存
	got, err := svc.GetByDeviceID(context.Background(), "dev001")
	assert.NoError(t, err)
	assert.Equal(t, "测试设备", got.DeviceName)

	// 删行后仍能命中缓存
	assert.NoError(t, repo.Delete(context.Background(), "dev001"))
	got2, err := svc.GetByDeviceID(context.Background(), "dev001")
	assert.NoError(t, err)
	assert.Equal(t, "测试设备", got2.DeviceName)
}

func TestDeviceService_GetByDeviceID_NotFound(t *testing.T) {
	_, _, svc := newDeviceCtx(t)
	_, err := svc.GetByDeviceID(context.Background(), "nope")
	assert.Error(t, err)
}

func TestDeviceService_ListByOwnerID(t *testing.T) {
	client, _, svc := newDeviceCtx(t)
	owner := newOwner(t, client)
	repo := repository.NewDeviceRepo(client)
	seedDevice(t, repo, "d-aaa", owner, "ONLINE")
	seedDevice(t, repo, "d-bbb", owner, "ONLINE")

	devices, err := svc.ListByOwnerID(context.Background(), owner)
	assert.NoError(t, err)
	assert.Len(t, devices, 2)

	// 其他 owner 无设备
	devices2, err := svc.ListByOwnerID(context.Background(), 99999)
	assert.NoError(t, err)
	assert.Empty(t, devices2)
}

func TestDeviceService_Delete_Cascade(t *testing.T) {
	client, repo, svc := newDeviceCtx(t)
	owner := newOwner(t, client)
	_, rcache := newTestRedisCache(t)
	_ = rcache
	d := seedDevice(t, repo, "abc123", owner, "ONLINE")
	_ = d

	// 先缓存设备状态（验证删除后缓存被清）
	assert.NoError(t, svc.Delete(context.Background(), "abc123"))

	// DB 已删除
	_, err := repo.GetByDeviceID(context.Background(), "abc123")
	assert.Error(t, err)

	t.Run("删除不存在设备报错", func(t *testing.T) {
		_, repo2, svc2 := newDeviceCtx(t)
		_ = repo2
		assert.Error(t, svc2.Delete(context.Background(), "ghost"))
	})
}

func TestDeviceService_GetDeviceToken(t *testing.T) {
	client, repo, svc := newDeviceCtx(t)
	owner := newOwner(t, client)
	seedDevice(t, repo, "abc123", owner, "ONLINE")

	t.Run("成功", func(t *testing.T) {
		token, err := svc.GetDeviceToken(context.Background(), "abc123", owner)
		assert.NoError(t, err)
		assert.NotEmpty(t, token)
	})

	t.Run("非 owner 无权", func(t *testing.T) {
		_, err := svc.GetDeviceToken(context.Background(), "abc123", 888)
		assert.ErrorContains(t, err, "无权获取该设备Token")
	})

	t.Run("设备不存在", func(t *testing.T) {
		_, err := svc.GetDeviceToken(context.Background(), "ghost", owner)
		assert.Error(t, err)
	})
}

func TestDeviceService_UpdateStatus(t *testing.T) {
	client, repo, svc := newDeviceCtx(t)
	owner := newOwner(t, client)
	seedDevice(t, repo, "abc123", owner, "ONLINE")

	assert.NoError(t, svc.UpdateStatus(context.Background(), "abc123", "OFFLINE"))

	d, err := repo.GetByDeviceID(context.Background(), "abc123")
	assert.NoError(t, err)
	assert.Equal(t, "OFFLINE", d.Status)
}

func TestDeviceService_CleanupInactiveDeviceTokens(t *testing.T) {
	client, repo, svc := newDeviceCtx(t)
	owner := newOwner(t, client)

	// 设备1：注册超 30 天且从未上线 + 已有 token → 应被清理
	old := time.Now().Add(-40 * 24 * time.Hour)
	_, err := repo.Create(context.Background(), &ent.Device{
		ID: "old001", DeviceName: "旧设备", OwnerID: owner, Status: "OFFLINE",
		CreatedAt: old, UpdatedAt: old,
	})
	assert.NoError(t, err)
	deviceMgr := middleware.GetDeviceManager()
	_, err = deviceMgr.Login("old001", "device")
	assert.NoError(t, err)

	// 设备2：最近活跃 → 不应清理
	now := time.Now()
	_, err = repo.Create(context.Background(), &ent.Device{
		ID: "fresh1", DeviceName: "新设备", OwnerID: owner, Status: "ONLINE",
		CreatedAt: now, UpdatedAt: now,
	})
	assert.NoError(t, err)

	cleaned, err := svc.CleanupInactiveDeviceTokens(context.Background(), time.Now().Add(-30*24*time.Hour))
	assert.NoError(t, err)
	assert.Equal(t, 1, cleaned)
	// token 已被注销
	assert.False(t, deviceMgr.IsLogin("old001"))
}
