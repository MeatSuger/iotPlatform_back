package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
)

func TestNewDeviceConfigRepo(t *testing.T) {
	repo := NewDeviceConfigRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestDeviceConfigRepo_CRUD(t *testing.T) {
	client := newRepoEnt(t)
	userRepo := NewUserRepo(client)
	deviceRepo := NewDeviceRepo(client)
	repo := NewDeviceConfigRepo(client)

	owner, err := userRepo.Create(context.Background(), makeUser("owner"))
	assert.NoError(t, err)
	deviceRepo.Create(context.Background(), makeDevice("dev1", owner.ID))

	// 不存在 → NotFoundError
	_, err = repo.GetByDeviceID(context.Background(), "dev1")
	assert.True(t, ent.IsNotFound(err))

	// Upsert 首次创建
	assert.NoError(t, repo.Upsert(context.Background(), "dev1", `{"sensor":{"reportInterval":60}}`))
	got, err := repo.GetByDeviceID(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, uint(1), got.Version)
	assert.JSONEq(t, `{"sensor":{"reportInterval":60}}`, got.Payload)
	assert.Equal(t, "pending", got.Status)

	// Upsert 覆盖更新（版本原子递增 1 → 2）
	assert.NoError(t, repo.Upsert(context.Background(), "dev1", `{"sensor":{"reportInterval":120}}`))
	got2, err := repo.GetByDeviceID(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, uint(2), got2.Version)
	assert.JSONEq(t, `{"sensor":{"reportInterval":120}}`, got2.Payload)

	// 回执回写
	assert.NoError(t, repo.UpdateReported(context.Background(), "dev1", 2, `{"sensor":{"reportInterval":120}}`))
	got3, err := repo.GetByDeviceID(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "acked", got3.Status)
	assert.Equal(t, uint(2), got3.ReportedVersion)
	assert.JSONEq(t, `{"sensor":{"reportInterval":120}}`, got3.ReportedPayload)

	// Delete
	assert.NoError(t, repo.Delete(context.Background(), "dev1"))
	_, err = repo.GetByDeviceID(context.Background(), "dev1")
	assert.True(t, ent.IsNotFound(err))
}

func TestDeviceConfigRepo_UpdatedAt(t *testing.T) {
	client := newRepoEnt(t)
	userRepo := NewUserRepo(client)
	deviceRepo := NewDeviceRepo(client)
	repo := NewDeviceConfigRepo(client)

	owner, _ := userRepo.Create(context.Background(), makeUser("owner"))
	deviceRepo.Create(context.Background(), makeDevice("dev1", owner.ID))

	assert.NoError(t, repo.Upsert(context.Background(), "dev1", `{}`))
	got, _ := repo.GetByDeviceID(context.Background(), "dev1")
	assert.False(t, got.UpdatedAt.IsZero())

	before := got.UpdatedAt
	time.Sleep(10 * time.Millisecond)
	assert.NoError(t, repo.Upsert(context.Background(), "dev1", `{"a":1}`))
	got2, _ := repo.GetByDeviceID(context.Background(), "dev1")
	assert.True(t, got2.UpdatedAt.After(before))
}
