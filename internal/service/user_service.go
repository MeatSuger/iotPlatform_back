package service

import (
	"context"
	"fmt"
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
type LoginRequest struct {
	Account string `json:"account" binding:"required"`
	Passwd  string `json:"passwd" binding:"required"`
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
	RoleSuperAdmin = "super-admin"
	RoleAdmin      = "admin"
	RoleUser       = "user"
)

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

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
	if user.Status == UserStatusDisabled {
		zap.S().Warnf("[User] 登录失败: 账号 %s (ID=%d) 已被禁用", user.Account, user.ID)
		return nil, common.ErrAccountDisabled
	}

	role := user.Role
	if role == "" {
		role = RoleUser
	}

	loginID := fmt.Sprintf("%d", user.ID)
	token, err := stputil.Login(user.ID)
	if err != nil {
		return nil, fmt.Errorf("生成Token失败: %w", err)
	}
	_ = stputil.SetRoles(loginID, []string{role})

	expireSeconds := int64(60 * 60 * 24)
	zap.S().Infof("[User] 账号 %s (ID=%d, role=%s) 登录成功", user.Account, user.ID, role)
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
		LoginDevice:          "default-device",
		UserInfo:             user,
	}, nil
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
	_ = stputil.Kickout(id)
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	_ = s.cache.EvictUserCache(ctx, fmt.Sprintf("%d", id))
	return nil
}

func (s *UserService) List(ctx context.Context) ([]*ent.User, error) {
	return s.repo.List(ctx)
}

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

func (s *UserService) IsExist(ctx context.Context, id uint) (bool, error) {
	return s.repo.IsExist(ctx, id)
}

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
