package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sa-tokens/sa-token-go/stputil"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/common"
)

// UserService 用户服务
type UserService struct {
	repo  *repository.UserRepo
	cache *cache.RedisCache
}

func NewUserService(repo *repository.UserRepo, redisCache *cache.RedisCache) *UserService {
	return &UserService{repo: repo, cache: redisCache}
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Account string `json:"account" binding:"required,min=3,max=50"`
	Passwd  string `json:"passwd" binding:"required,min=6,max=100"`
	Name    string `json:"name"`
	Email   string `json:"email"`
}

// LoginRequest 登录请求
// Device 为可选的设备端标识（如前端 localStorage 生成的 UUID、App 设备名）：
// Sa-Token 按该标识区分多端登录，同一账号最多 MaxLoginCount 个设备端同时在线；
// 不传时服务端回退用 User-Agent 生成，完全缺失则视为同一默认设备。
type LoginRequest struct {
	Account string `json:"account" binding:"required"`
	Passwd  string `json:"passwd" binding:"required"`
	Device  string `json:"device"`
}

// LoginResponse Sa-Token 风格登录响应
type LoginResponse struct {
	TokenName            string    `json:"tokenName"`
	TokenValue           string    `json:"tokenValue"`
	IsLogin              bool      `json:"isLogin"`
	LoginID              string    `json:"loginId"`
	LoginType            string    `json:"loginType"`
	TokenTimeout         int64     `json:"tokenTimeout"`
	SessionTimeout       int64     `json:"sessionTimeout"`
	TokenSessionTimeout  int64     `json:"tokenSessionTimeout"`
	TokenActivityTimeout int64     `json:"tokenActivityTimeout"`
	LoginDevice          string    `json:"loginDevice"`
	UserInfo             *ent.User `json:"userInfo,omitempty"`
}

const (
	RoleSuperAdmin = "super-admin" // RoleSuperAdmin 超级管理员角色
	RoleAdmin      = "admin"       // RoleAdmin 管理员角色
	RoleUser       = "user"        // RoleUser 普通用户角色
)

const (
	UserStatusActive   = "ACTIVE"   // UserStatusActive 用户启用状态
	UserStatusDisabled = "DISABLED" // UserStatusDisabled 用户禁用状态
)

// Register 注册用户（bcrypt 加密存储密码）
func (s *UserService) Register(ctx context.Context, req RegisterRequest) (*ent.User, error) {
	exist, err := s.repo.GetByAccount(ctx, req.Account)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("查询账号失败: %w", err)
	}
	if exist != nil {
		return nil, common.ErrAccountExist
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Passwd), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	now := time.Now()
	user, err := s.repo.Create(ctx, &ent.User{
		Account:    req.Account,
		Passwd:     string(hashed),
		Name:       req.Name,
		Email:      req.Email,
		Role:       RoleUser,
		Status:     UserStatusActive,
		Age:        0,
		CreateTime: now,
		UpdateTime: now,
	})
	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	return user, nil
}

// Login 用户登录：校验账号密码与状态，签发 Sa-Token 并返回登录信息
func (s *UserService) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	user, err := s.repo.GetByAccount(ctx, req.Account)
	if err != nil {
		if ent.IsNotFound(err) {
			zap.S().Warnf("[User] 登录失败: 账号 %s 不存在", req.Account)
			return nil, common.ErrUserNotFound
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	if !verifyPassword(user.Passwd, req.Passwd) {
		zap.S().Warnf("[User] 登录失败: 账号 %s (ID=%d) 密码错误", user.Account, user.ID)
		return nil, common.ErrPasswordWrong
	}
	if strings.EqualFold(user.Status, UserStatusDisabled) {
		zap.S().Warnf("[User] 登录失败: 账号 %s (ID=%d) 已被禁用", user.Account, user.ID)
		return nil, common.ErrAccountDisabled
	}

	role := user.Role
	if role == "" {
		role = RoleUser
	}

	loginID := fmt.Sprintf("%d", user.ID)
	device := normalizeLoginDevice(req.Device)
	token, err := stputil.Login(user.ID, device)
	if err != nil {
		return nil, fmt.Errorf("生成Token失败: %w", err)
	}
	_ = stputil.SetRoles(loginID, []string{role})

	// 与 main.go 中 userMgr Timeout 保持一致（3 天）；该值同时用作响应 tokenTimeout
	// 字段与认证 Cookie 的 MaxAge，必须与真实 Token TTL 一致，避免 Cookie 提前失效
	expireSeconds := int64(60 * 60 * 24 * 3)
	zap.S().Infof("[User] 账号 %s (ID=%d, role=%s, device=%s) 登录成功", user.Account, user.ID, role, device)
	user.UpdateTime = time.Now()
	if err := s.repo.Update(ctx, user); err != nil {
		zap.S().Warnf("[User] 更新用户登录时间失败: %v", err)
	}
	return &LoginResponse{
		TokenName:            "Authorization",
		TokenValue:           token,
		IsLogin:              true,
		LoginID:              loginID,
		LoginType:            "login",
		TokenTimeout:         expireSeconds,
		SessionTimeout:       expireSeconds,
		TokenSessionTimeout:  -2,
		TokenActivityTimeout: -1,
		LoginDevice:          device,
		UserInfo:             user,
	}, nil
}

// normalizeLoginDevice 规范化登录设备端标识：
// 仅保留字母/数字/下划线/连字符并截断到 50 字符（该值会进入 Redis 键 account:<loginID>:<device>），
// 为空或清洗后为空时回退 "default"（Sa-Token 默认设备键）。
func normalizeLoginDevice(device string) string {
	device = strings.TrimSpace(device)
	if device == "" {
		return "default"
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return -1
	}, device)
	if cleaned == "" {
		return "default"
	}
	if len(cleaned) > 50 {
		cleaned = cleaned[:50]
	}
	return cleaned
}

// GetByID 获取用户（Cache-Aside：L1本地 → L2 Redis → PostgreSQL回源）
func (s *UserService) GetByID(ctx context.Context, id uint) (*ent.User, error) {
	var user ent.User
	err := s.cache.GetCachedUser(ctx, fmt.Sprintf("%d", id), &user, func(ctx context.Context) (any, error) {
		return s.repo.GetByID(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Update 更新用户（Write-Invalidate：更新DB后失效缓存）
func (s *UserService) Update(ctx context.Context, user *ent.User) error {
	if err := s.repo.Update(ctx, user); err != nil {
		return err
	}
	// 失效缓存，下次读取自动回填
	_ = s.cache.EvictUserCache(ctx, fmt.Sprintf("%d", user.ID))
	return nil
}

// Delete 删除用户（Write-Invalidate：删DB后失效缓存）
func (s *UserService) Delete(ctx context.Context, id uint) error {
	// 完整删除该用户全部 token（同一账号最多同时在线 5 端）；
	// Logout 事件会删除 token/account/renew 三个 key，不留 KICK_OUT 标记
	if tokens, err := stputil.GetTokenValueList(id); err == nil {
		for _, t := range tokens {
			_ = stputil.LogoutByToken(t)
		}
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	_ = s.cache.EvictUserCache(ctx, fmt.Sprintf("%d", id))
	return nil
}

// List 查询全部用户
func (s *UserService) List(ctx context.Context) ([]*ent.User, error) {
	return s.repo.List(ctx)
}

// Page 分页查询用户（page/size 非法时使用默认值，name 模糊匹配）
func (s *UserService) Page(ctx context.Context, page, size int, name string) ([]*ent.User, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	users, total, err := s.repo.Page(ctx, page, size, name)
	return users, int64(total), err
}

// IsExist 判断用户是否存在
func (s *UserService) IsExist(ctx context.Context, id uint) (bool, error) {
	return s.repo.IsExist(ctx, id)
}

// UpgradePasswordHash 将密码升级为 bcrypt 哈希（兼容旧明文密码的迁移场景）
func (s *UserService) UpgradePasswordHash(ctx context.Context, userID uint, plainPassword string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成bcrypt哈希失败: %w", err)
	}
	return s.repo.UpdatePassword(ctx, userID, string(hashed))
}

func verifyPassword(stored, input string) bool {
	if isBcryptHash(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(input)) == nil
	}
	return stored == input
}

func isBcryptHash(s string) bool {
	return len(s) == 60 && (s[:4] == "$2a$" || s[:4] == "$2b$" || s[:4] == "$2y$")
}
