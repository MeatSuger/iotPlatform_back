package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/sa-tokens/sa-token-go/stputil"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/common"
)

func TestVerifyPassword_Bcrypt(t *testing.T) {
	hashed, err := bcrypt.GenerateFromPassword([]byte("testpassword"), bcrypt.DefaultCost)
	assert.NoError(t, err)

	assert.True(t, verifyPassword(string(hashed), "testpassword"))
	assert.False(t, verifyPassword(string(hashed), "wrongpassword"))
}

func TestVerifyPassword_PlainText(t *testing.T) {
	// 兼容旧明文密码（历史遗留）
	assert.True(t, verifyPassword("plaintext", "plaintext"))
	assert.False(t, verifyPassword("plaintext", "different"))
}

func TestIsBcryptHash(t *testing.T) {
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

// ========================================
// 业务逻辑：Register / Login / CRUD（真实 sqlite 仓库 + miniredis 缓存）
// ========================================

func newUserCtx(t *testing.T) (*ent.Client, *repository.UserRepo, *UserService) {
	client := newTestEnt(t)
	repo := repository.NewUserRepo(client)
	_, rcache := newTestRedisCache(t)
	return client, repo, NewUserService(repo, rcache)
}

func TestRegister(t *testing.T) {
	_, repo, svc := newUserCtx(t)

	t.Run("成功注册（含账号不存在分支）", func(t *testing.T) {
		user, err := svc.Register(context.Background(), RegisterRequest{
			Account: "alice", Passwd: "secret123", Name: "Alice", Email: "alice@example.com",
		})
		assert.NoError(t, err)
		assert.NotZero(t, user.ID)
		assert.Equal(t, RoleUser, user.Role)
		assert.Equal(t, UserStatusActive, user.Status)
		// 密码为 bcrypt 哈希而非明文
		assert.True(t, isBcryptHash(user.Passwd))
		assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.Passwd), []byte("secret123")))
	})

	t.Run("账号已存在", func(t *testing.T) {
		seedUser(t, repo, "dup")
		_, err := svc.Register(context.Background(), RegisterRequest{Account: "dup", Passwd: "secret123"})
		assert.ErrorIs(t, err, common.ErrAccountExist)
	})

	t.Run("数据库错误", func(t *testing.T) {
		client2, repo2, svc2 := newUserCtx(t)
		_ = repo2
		assert.NoError(t, client2.Close())
		_, err := svc2.Register(context.Background(), RegisterRequest{Account: "x", Passwd: "secret123"})
		assert.Error(t, err)
	})
}

func TestLogin(t *testing.T) {
	_, _, svc := newUserCtx(t)
	// Login 不使用缓存，直接用共享仓库构造 svc
	client := newTestEnt(t)
	repo := repository.NewUserRepo(client)
	svc = NewUserService(repo, nil)

	t.Run("登录成功", func(t *testing.T) {
		seedUser(t, repo, "alice")
		resp, err := svc.Login(context.Background(), LoginRequest{Account: "alice", Passwd: "secret123", Device: "web-ui"})
		assert.NoError(t, err)
		assert.True(t, resp.IsLogin)
		assert.NotEmpty(t, resp.TokenValue)
		assert.Equal(t, RoleUser, resp.UserInfo.Role)
		assert.Equal(t, "web-ui", resp.LoginDevice)
	})

	t.Run("账号不存在", func(t *testing.T) {
		_, err := svc.Login(context.Background(), LoginRequest{Account: "ghost", Passwd: "x"})
		assert.ErrorIs(t, err, common.ErrUserNotFound)
	})

	t.Run("密码错误", func(t *testing.T) {
		seedUser(t, repo, "bob")
		_, err := svc.Login(context.Background(), LoginRequest{Account: "bob", Passwd: "wrong"})
		assert.ErrorIs(t, err, common.ErrPasswordWrong)
	})

	t.Run("明文密码兼容", func(t *testing.T) {
		now := time.Now()
		_, err := repo.Create(context.Background(), &ent.User{
			Account: "legacy", Passwd: "plain-old", Role: RoleUser, Status: UserStatusActive,
			CreateTime: now, UpdateTime: now,
		})
		assert.NoError(t, err)
		resp, err := svc.Login(context.Background(), LoginRequest{Account: "legacy", Passwd: "plain-old"})
		assert.NoError(t, err)
		assert.True(t, resp.IsLogin)
	})

	t.Run("账号禁用", func(t *testing.T) {
		u := seedUser(t, repo, "carol")
		u.Status = UserStatusDisabled
		assert.NoError(t, repo.Update(context.Background(), u))
		_, err := svc.Login(context.Background(), LoginRequest{Account: "carol", Passwd: "secret123"})
		assert.ErrorIs(t, err, common.ErrAccountDisabled)
	})

	t.Run("空角色回退默认 user", func(t *testing.T) {
		now := time.Now()
		u, err := repo.Create(context.Background(), &ent.User{
			Account: "norole", Passwd: "secret123", Role: "", Status: UserStatusActive,
			CreateTime: now, UpdateTime: now,
		})
		assert.NoError(t, err)
		resp, err := svc.Login(context.Background(), LoginRequest{Account: "norole", Passwd: "secret123"})
		assert.NoError(t, err)
		assert.True(t, resp.IsLogin)
		roles, _ := stputil.GetRoles(strconv.FormatUint(uint64(u.ID), 10))
		assert.Equal(t, []string{RoleUser}, roles)
	})

}

func TestNormalizeLoginDevice(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"空串回退 default", "", "default"},
		{"纯空白回退", "  \t ", "default"},
		{"非法字符清洗为空回退", "中文设备，。/", "default"},
		{"保留合法字符", "web-UI_01", "web-UI_01"},
		{"混合字符清洗", "iPhone 15 Pro!", "iPhone15Pro"},
		{"全非法字符", "！！！", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, normalizeLoginDevice(tt.input))
		})
	}

	// 超过 50 字符截断
	long := ""
	for i := 0; i < 60; i++ {
		long += "a"
	}
	assert.Len(t, normalizeLoginDevice(long), 50)
}

func TestUserService_GetByID_CacheAside(t *testing.T) {
	client, repo, svc := newUserCtx(t)
	u := seedUser(t, repo, "alice")

	// 首次调用：loader 从 DB 加载并回填缓存
	got, err := svc.GetByID(context.Background(), u.ID)
	assert.NoError(t, err)
	assert.Equal(t, "alice", got.Account)

	// 删除 DB 行后仍能命中缓存（证明走 L1/L2 缓存）
	assert.NoError(t, repo.Delete(context.Background(), u.ID))
	got2, err := svc.GetByID(context.Background(), u.ID)
	assert.NoError(t, err)
	assert.Equal(t, "alice", got2.Account)
	_ = client
}

func TestUserService_GetByID_NotFound(t *testing.T) {
	_, _, svc := newUserCtx(t)
	_, err := svc.GetByID(context.Background(), 9999)
	assert.Error(t, err)
}

func TestUserService_UpdateEvictsCache(t *testing.T) {
	_, repo, svc := newUserCtx(t)
	u := seedUser(t, repo, "alice")
	u.Name = "改名"
	assert.NoError(t, repo.Update(context.Background(), u))
	assert.NoError(t, svc.Update(context.Background(), u))

	// 更新后缓存被失效 → 读到新数据
	got, err := svc.GetByID(context.Background(), u.ID)
	assert.NoError(t, err)
	assert.Equal(t, "改名", got.Name)
}

func TestUserService_Delete(t *testing.T) {
	_, repo, svc := newUserCtx(t)
	u := seedUser(t, repo, "alice")

	assert.NoError(t, svc.Delete(context.Background(), u.ID))
	_, err := repo.GetByAccount(context.Background(), "alice")
	assert.Error(t, err)
}

func TestUserService_ListPage(t *testing.T) {
	client, repo, svc := newUserCtx(t)
	seedUser(t, repo, "u1")
	seedUser(t, repo, "u2")

	users, err := svc.List(context.Background())
	assert.NoError(t, err)
	assert.Len(t, users, 2)

	// page/size 非法 → 默认 1/10；name 按 Name 字段模糊过滤
	users2, total, err := svc.Page(context.Background(), 0, -1, "用户")
	assert.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, users2, 2)

	users3, total3, err := svc.Page(context.Background(), 1, 1, "测试")
	assert.NoError(t, err)
	assert.Equal(t, int64(2), total3)
	assert.Len(t, users3, 1)
	_ = client
}
