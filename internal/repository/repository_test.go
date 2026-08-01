package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Repository layer tests validate constructor functions and type fields.
// Full integration tests require a running PostgreSQL + Ent schema migration,
// which is handled by integration test suites.

func TestNewUserRepo(t *testing.T) {
	repo := NewUserRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestNewDeviceRepo(t *testing.T) {
	repo := NewDeviceRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}

func TestNewDownlinkCmdRepo(t *testing.T) {
	repo := NewDownlinkCmdRepo(nil)
	assert.NotNil(t, repo)
	assert.Nil(t, repo.client)
}
