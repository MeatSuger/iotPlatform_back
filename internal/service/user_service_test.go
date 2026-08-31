package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestVerifyPassword_Bcrypt(t *testing.T) {
	// Generate a bcrypt hash
	hashed, err := bcrypt.GenerateFromPassword([]byte("testpassword"), bcrypt.DefaultCost)
	assert.NoError(t, err)

	assert.True(t, verifyPassword(string(hashed), "testpassword"))
	assert.False(t, verifyPassword(string(hashed), "wrongpassword"))
}

func TestVerifyPassword_PlainText(t *testing.T) {
	// Plain text comparison (legacy)
	assert.True(t, verifyPassword("plaintext", "plaintext"))
	assert.False(t, verifyPassword("plaintext", "different"))
}

func TestIsBcryptHash(t *testing.T) {
	// Generate real hashes to test with
	hash2a, _ := bcrypt.GenerateFromPassword([]byte("test"), bcrypt.DefaultCost)
	assert.True(t, isBcryptHash(string(hash2a)))

	hash2b, _ := bcrypt.GenerateFromPassword([]byte("test2"), bcrypt.DefaultCost)
	assert.True(t, isBcryptHash(string(hash2b)))

	assert.False(t, isBcryptHash("notbcrypthash"))
	assert.False(t, isBcryptHash("$2c$10$..."))
	assert.False(t, isBcryptHash("short"))
	assert.False(t, isBcryptHash(""))
}

func TestRoleConstants(t *testing.T) {
	assert.Equal(t, "super-admin", RoleSuperAdmin)
	assert.Equal(t, "admin", RoleAdmin)
	assert.Equal(t, "user", RoleUser)
}

func TestUserStatusConstants(t *testing.T) {
	assert.Equal(t, "ACTIVE", UserStatusActive)
	assert.Equal(t, "DISABLED", UserStatusDisabled)
}

func TestRegisterRequest_Validation(t *testing.T) {
	req := RegisterRequest{
		Account: "testuser",
		Passwd:  "password123",
		Name:    "Test User",
		Email:   "test@example.com",
	}
	assert.Equal(t, "testuser", req.Account)
	assert.Equal(t, "password123", req.Passwd)
	assert.Equal(t, "Test User", req.Name)
	assert.Equal(t, "test@example.com", req.Email)
}

func TestLoginRequest_Fields(t *testing.T) {
	req := LoginRequest{
		Account: "admin",
		Passwd:  "admin123",
	}
	assert.Equal(t, "admin", req.Account)
	assert.Equal(t, "admin123", req.Passwd)
}

func TestLoginResponse_Fields(t *testing.T) {
	resp := LoginResponse{
		TokenName:            "Authorization",
		TokenValue:           "token-xxx",
		IsLogin:              true,
		LoginID:              "1",
		LoginType:            "login",
		TokenTimeout:         2592000,
		SessionTimeout:       2592000,
		TokenSessionTimeout:  -2,
		TokenActivityTimeout: -1,
		LoginDevice:          "default-device",
	}
	assert.True(t, resp.IsLogin)
	assert.Equal(t, "1", resp.LoginID)
	assert.Equal(t, "login", resp.LoginType)
	assert.Equal(t, int64(2592000), resp.TokenTimeout)
}

func TestNewUserService(t *testing.T) {
	svc := NewUserService(nil, nil)
	assert.NotNil(t, svc)
	assert.Nil(t, svc.repo)
	assert.Nil(t, svc.cache)
}
