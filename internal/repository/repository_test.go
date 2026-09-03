package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
)

// 仓库层集成测试：ent + sqlite 文件库真实执行全部 CRUD/查询方法
// （迁移由 ent schema 完成，无需外部 PostgreSQL）

func newRepoEnt(t *testing.T) *ent.Client {
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

func makeUser(account string) *ent.User {
	now := time.Now()
	return &ent.User{
		Account:    account,
		Passwd:     "hash",
		Name:       "用户" + account,
		Email:      account + "@example.com",
		Role:       "user",
		Status:     "ACTIVE",
		Age:        20,
		CreateTime: now,
		UpdateTime: now,
	}
}

func makeDevice(id string, owner uint) *ent.Device {
	now := time.Now()
	return &ent.Device{
		ID:         id,
		DeviceName: "设备" + id,
		OwnerID:    owner,
		Status:     "ONLINE",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestNewUserRepo(t *testing.T) {
	repo := NewUserRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestUserRepo_CRUD(t *testing.T) {
	client := newRepoEnt(t)
	repo := NewUserRepo(client)

	// Create
	u, err := repo.Create(context.Background(), makeUser("alice"))
	assert.NoError(t, err)
	assert.NotZero(t, u.ID)

	// GetByID
	got, err := repo.GetByID(context.Background(), u.ID)
	assert.NoError(t, err)
	assert.Equal(t, "alice", got.Account)

	// GetByAccount 存在/不存在（NotFoundError）
	got2, err := repo.GetByAccount(context.Background(), "alice")
	assert.NoError(t, err)
	assert.Equal(t, u.ID, got2.ID)
	_, err = repo.GetByAccount(context.Background(), "ghost")
	assert.Error(t, err)
	assert.True(t, ent.IsNotFound(err))

	// Update
	u.Name = "改名"
	u.Role = "admin"
	assert.NoError(t, repo.Update(context.Background(), u))
	got3, _ := repo.GetByID(context.Background(), u.ID)
	assert.Equal(t, "改名", got3.Name)
	assert.Equal(t, "admin", got3.Role)

	// UpdatePassword
	assert.NoError(t, repo.UpdatePassword(context.Background(), u.ID, "newhash"))
	got4, _ := repo.GetByID(context.Background(), u.ID)
	assert.Equal(t, "newhash", got4.Passwd)

	// List / Page / IsExist
	_, err = repo.Create(context.Background(), makeUser("bob"))
	assert.NoError(t, err)

	users, err := repo.List(context.Background())
	assert.NoError(t, err)
	assert.Len(t, users, 2)

	page, total, err := repo.Page(context.Background(), 1, 1, "")
	assert.NoError(t, err)
	assert.Len(t, page, 1)
	assert.Equal(t, 2, total)

	exist, err := repo.IsExist(context.Background(), u.ID)
	assert.NoError(t, err)
	assert.True(t, exist)
	exist2, err := repo.IsExist(context.Background(), 9999)
	assert.NoError(t, err)
	assert.False(t, exist2)

	// Delete
	assert.NoError(t, repo.Delete(context.Background(), u.ID))
	_, err = repo.GetByID(context.Background(), u.ID)
	assert.True(t, ent.IsNotFound(err))
}

func TestNewDeviceRepo(t *testing.T) {
	repo := NewDeviceRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestDeviceRepo_CRUD(t *testing.T) {
	client := newRepoEnt(t)
	userRepo := NewUserRepo(client)
	repo := NewDeviceRepo(client)

	owner, err := userRepo.Create(context.Background(), makeUser("owner"))
	assert.NoError(t, err)

	// Create / GetByID / GetByDeviceID
	d, err := repo.Create(context.Background(), makeDevice("abc123", owner.ID))
	assert.NoError(t, err)
	assert.Equal(t, "abc123", d.ID)

	got, err := repo.GetByID(context.Background(), "abc123")
	assert.NoError(t, err)
	assert.Equal(t, "设备abc123", got.DeviceName)
	got2, err := repo.GetByDeviceID(context.Background(), "abc123")
	assert.NoError(t, err)
	assert.Equal(t, d.ID, got2.ID)

	// 不存在 → NotFoundError
	_, err = repo.GetByDeviceID(context.Background(), "zzz")
	assert.True(t, ent.IsNotFound(err))

	// Update（增量，仅非 nil 字段）
	name := "新名字"
	assert.NoError(t, repo.Update(context.Background(), "abc123", DeviceUpdateFields{DeviceName: &name}))
	got3, _ := repo.GetByDeviceID(context.Background(), "abc123")
	assert.Equal(t, "新名字", got3.DeviceName)

	// 第二台设备 + ListByOwnerID / ListByDeviceID
	_, err = repo.Create(context.Background(), makeDevice("def456", owner.ID))
	assert.NoError(t, err)
	otherOwner, _ := userRepo.Create(context.Background(), makeUser("other"))
	_, err = repo.Create(context.Background(), makeDevice("own999", otherOwner.ID))
	assert.NoError(t, err)

	devs, err := repo.ListByOwnerID(context.Background(), owner.ID)
	assert.NoError(t, err)
	assert.Len(t, devs, 2)
	assert.Equal(t, "def456", devs[0].ID) // ID 倒序

	devs2, err := repo.ListByDeviceID(context.Background(), "abc123")
	assert.NoError(t, err)
	assert.Len(t, devs2, 1)

	// UpdateStatus / UpdateLastActive
	assert.NoError(t, repo.UpdateStatus(context.Background(), "abc123", "OFFLINE"))
	got4, _ := repo.GetByDeviceID(context.Background(), "abc123")
	assert.Equal(t, "OFFLINE", got4.Status)

	assert.NoError(t, repo.UpdateLastActive(context.Background(), "abc123", "ONLINE"))
	got5, _ := repo.GetByDeviceID(context.Background(), "abc123")
	assert.Equal(t, "ONLINE", got5.Status)
	assert.NotNil(t, got5.LastActiveTime)

	// ListByStatus
	online, err := repo.ListByStatus(context.Background(), "ONLINE")
	assert.NoError(t, err)
	assert.Len(t, online, 3)

	// ListInactiveBefore：从不上线且创建时间早于 cutoff
	old := time.Now().Add(-40 * 24 * time.Hour)
	_, err = repo.Create(context.Background(), &ent.Device{
		ID: "old001", DeviceName: "旧", OwnerID: owner.ID, Status: "OFFLINE",
		CreatedAt: old, UpdatedAt: old,
	})
	assert.NoError(t, err)
	inactive, err := repo.ListInactiveBefore(context.Background(), time.Now().Add(-30*24*time.Hour))
	assert.NoError(t, err)
	foundOld := false
	for _, idv := range inactive {
		if idv.ID == "old001" {
			foundOld = true
		}
	}
	assert.True(t, foundOld)

	// Delete
	assert.NoError(t, repo.Delete(context.Background(), "abc123"))
	_, err = repo.GetByID(context.Background(), "abc123")
	assert.True(t, ent.IsNotFound(err))
}

func TestDeviceUpdateFields_IsEmpty(t *testing.T) {
	assert.True(t, DeviceUpdateFields{}.IsEmpty())
	name := "x"
	assert.False(t, DeviceUpdateFields{DeviceName: &name}.IsEmpty())
	empty := ""
	assert.False(t, DeviceUpdateFields{Location: &empty}.IsEmpty())
}

func TestNewDownlinkCmdRepo(t *testing.T) {
	repo := NewDownlinkCmdRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestDownlinkCmdRepo_FullFlow(t *testing.T) {
	client := newRepoEnt(t)
	userRepo := NewUserRepo(client)
	deviceRepo := NewDeviceRepo(client)
	repo := NewDownlinkCmdRepo(client)

	owner, _ := userRepo.Create(context.Background(), makeUser("owner"))
	deviceRepo.Create(context.Background(), makeDevice("dev1", owner.ID))

	// Create
	cmd, err := repo.Create(context.Background(), &ent.DownlinkCmd{
		DeviceID: "dev1", Type: "reboot", Payload: `{}`, Status: "pending",
		CreatedAt: time.Now(),
	})
	assert.NoError(t, err)
	assert.NotZero(t, cmd.ID)

	// ListPending
	pending, err := repo.ListPending(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, pending, 1)

	// MarkSent
	assert.NoError(t, repo.MarkSent(context.Background(), []uint{cmd.ID}))
	pending2, err := repo.ListPending(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Empty(t, pending2)

	// 空列表 MarkSent 安全
	assert.NoError(t, repo.MarkSent(context.Background(), nil))

	// MarkDelivered
	assert.NoError(t, repo.MarkDelivered(context.Background(), cmd.ID))
}

func TestMqttPublishLogRepo_Create(t *testing.T) {
	client := newRepoEnt(t)
	repo := NewMqttPublishLogRepo(client)
	log, err := repo.Create(context.Background(), &ent.MqttPublishLog{
		Topic: "dev/up", Payload: `{"a":1}`, Qos: 1, Retained: false,
		ClientID: "gw-1", BrokerURL: "tcp://mqtt:1883", CreateTime: time.Now(),
	})
	assert.NoError(t, err)
	assert.NotZero(t, log.ID)
	// 读回校验
	got, err := client.MqttPublishLog.Get(context.Background(), log.ID)
	assert.NoError(t, err)
	assert.Equal(t, "dev/up", got.Topic)
}
