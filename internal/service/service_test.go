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

package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	"github.com/sa-tokens/sa-token-go/storage/memory"
	"golang.org/x/crypto/bcrypt"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
)

// TestMain 初始化 sa-token 全局管理器（内存存储，无需外部依赖），
// 供依赖 middleware.GetDeviceManager / stputil 的服务方法测试使用。
func TestMain(m *testing.M) {
	userStorage := memory.NewStorage()
	userCfg := sagin.DefaultConfig()
	userCfg.TokenName = "Authorization"
	userCfg.KeyPrefix = "Authorization:"
	userCfg.Timeout = 2592000
	userCfg.IsLog = false
	userCfg.TokenStyle = sagin.TokenStyleUUID
	userMgr := sagin.NewManager(userStorage, userCfg)
	sagin.SetManager(userMgr)

	deviceStorage := memory.NewStorage()
	deviceCfg := sagin.DefaultConfig()
	deviceCfg.TokenName = "X-Device-Token"
	deviceCfg.KeyPrefix = "X-Device-Token:"
	deviceCfg.Timeout = -1
	deviceCfg.IsLog = false
	deviceCfg.TokenStyle = sagin.TokenStyleUUID
	deviceMgr := sagin.NewManager(deviceStorage, deviceCfg)
	middleware.SetDeviceManager(deviceMgr)

	os.Exit(m.Run())
}

// newTestRedis 启动 miniredis 内存服务器并返回 go-redis 客户端
func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

// newTestRedisCache 构造挂载 miniredis 的真实 RedisCache
func newTestRedisCache(t *testing.T) (*miniredis.Miniredis, *cache.RedisCache) {
	t.Helper()
	mr, rdb := newTestRedis(t)
	return mr, cache.NewRedisCache(rdb)
}

// newTestEnt 打开 sqlite 文件库（临时目录）并完成 ent schema 迁移，返回 ent 客户端
func newTestEnt(t *testing.T) *ent.Client {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	client, err := ent.Open("sqlite3", "file:"+dbPath+"?_fk=1")
	if err != nil {
		t.Fatalf("打开 sqlite 失败: %v", err)
	}
	if err := client.Schema.Create(context.Background()); err != nil {
		_ = client.Close()
		t.Fatalf("ent schema 迁移失败: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// newThingModelEnv 构造「真实 sqlite 仓库 + miniredis 命令队列」的公共依赖
func newThingModelEnv(t *testing.T) (*repository.DeviceRepo, *DeviceConfigService, *cache.RedisCache, *ent.Client) {
	t.Helper()
	client := newTestEnt(t)
	deviceRepo := repository.NewDeviceRepo(client)
	configRepo := repository.NewDeviceConfigRepo(client)
	cmdRepo := repository.NewMessageLogRepo(client)
	_, rdb := newTestRedis(t)
	rcache := cache.NewRedisCache(rdb)

	downlinkSvc := NewDownlinkService(cmdRepo, deviceRepo, rdb, nil, nil)
	configSvc := NewDeviceConfigService(configRepo, downlinkSvc, nil)
	return deviceRepo, configSvc, rcache, client
}

// seedThingModelDevice 创建属主用户 + 测试设备（满足 owner 外键约束）
func seedThingModelDevice(t *testing.T, client *ent.Client, deviceRepo *repository.DeviceRepo) {
	t.Helper()
	owner := newOwner(t, client)
	seedDevice(t, deviceRepo, "dev1", owner, "ONLINE")
}

// seedUser 创建测试用户并返回（密码固定为 secret123 的 bcrypt 哈希）
func seedUser(t *testing.T, repo *repository.UserRepo, account string) *ent.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	now := time.Now()
	u, err := repo.Create(context.Background(), &ent.User{
		Account:    account,
		Passwd:     string(hash),
		Name:       "测试用户",
		Email:      account + "@example.com",
		Role:       RoleUser,
		Status:     UserStatusActive,
		Age:        0,
		CreateTime: now,
		UpdateTime: now,
	})
	if err != nil {
		t.Fatalf("种子用户失败: %v", err)
	}
	return u
}

// newOwner 创建 owner 用户并返回其 ID（账号唯一）
func newOwner(t *testing.T, client *ent.Client) uint {
	t.Helper()
	account := "owner_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	return seedUser(t, repository.NewUserRepo(client), account).ID
}

// seedDevice 创建测试设备并返回
func seedDevice(t *testing.T, repo *repository.DeviceRepo, id string, ownerID uint, status string) *ent.Device {
	t.Helper()
	now := time.Now()
	d, err := repo.Create(context.Background(), &ent.Device{
		ID:         id,
		DeviceName: "测试设备",
		DeviceType: "sensor",
		OwnerID:    ownerID,
		Status:     status,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		t.Fatalf("种子设备失败: %v", err)
	}
	return d
}

// miniredisSrv 便于在测试签名中引用 miniredis 服务器类型
type miniredisSrv = miniredis.Miniredis

// stopPGDebounce 停止 DeviceReportService 的防抖定时器并清空待处理集合，
// 防止 5s 后定时器在测试结束后触发对已关闭数据库的回调。
func stopPGDebounce(s *DeviceReportService) {
	s.pgUpdateMu.Lock()
	if s.pgUpdateTimer != nil {
		s.pgUpdateTimer.Stop()
		s.pgUpdateTimer = nil
	}
	s.pgUpdatePending = make(map[string]time.Time)
	s.pgUpdateMu.Unlock()
}
