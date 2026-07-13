package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/sa-tokens/sa-token-go/stputil"
	"github.com/yu/iot-platform-go/common"
	"github.com/yu/iot-platform-go/entity"
	"github.com/yu/iot-platform-go/repository"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserService 用户服务
type UserService struct {
	repo *repository.UserRepo
}

// NewUserService 创建用户服务
func NewUserService(repo *repository.UserRepo) *UserService {
	return &UserService{repo: repo}
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
	TokenName            string `json:"tokenName"`
	TokenValue           string `json:"tokenValue"`
	IsLogin              bool   `json:"isLogin"`
	LoginID              string `json:"loginId"`
	LoginType            string `json:"loginType"`
	TokenTimeout         int64  `json:"tokenTimeout"`
	SessionTimeout       int64  `json:"sessionTimeout"`
	TokenSessionTimeout  int64  `json:"tokenSessionTimeout"`
	TokenActivityTimeout int64  `json:"tokenActivityTimeout"`
	LoginDevice          string `json:"loginDevice"`
}

// Register 用户注册
func (s *UserService) Register(ctx context.Context, req RegisterRequest) (*entity.User, error) {
	// 检查账号是否已存在
	exist, err := s.repo.GetByAccount(ctx, req.Account)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询账号失败: %w", err)
	}
	if exist != nil {
		return nil, common.ErrAccountExist
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Passwd), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	user := &entity.User{
		Account:    req.Account,
		Passwd:     string(hashedPassword),
		Name:       req.Name,
		Email:      req.Email,
		Role:       entity.RoleUser,
		Status:     entity.UserStatusActive,
		Age:        0,
		CreateTime: common.DateTimeNow(),
		UpdateTime: common.DateTimeNow(),
	}

	if err := s.repo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	return user, nil
}

// Login 用户登录（Sa-Token）
func (s *UserService) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	user, err := s.repo.GetByAccount(ctx, req.Account)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			zap.S().Warnf("[User] 登录失败: 账号 %s 不存在", req.Account)
			return nil, common.ErrUserNotFound
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	// 验证密码 — 兼容 bcrypt 哈希 + 明文（开发阶段）
	if !s.verifyPassword(user.Passwd, req.Passwd) {
		zap.S().Warnf("[User] 登录失败: 账号 %s (ID=%d) 密码错误", user.Account, user.ID)
		return nil, common.ErrPasswordWrong
	}

	// 检查用户状态
	if user.Status == entity.UserStatusDisabled {
		zap.S().Warnf("[User] 登录失败: 账号 %s (ID=%d) 已被禁用", user.Account, user.ID)
		return nil, common.ErrAccountDisabled
	}

	// 确定用户角色
	role := user.Role
	if role == "" {
		role = entity.RoleUser
	}

	// Sa-Token 登录（stputil 全局函数，loginID 支持 int/uint/string）
	loginID := fmt.Sprintf("%d", user.ID)
	token, err := stputil.Login(user.ID)
	if err != nil {
		return nil, fmt.Errorf("生成Token失败: %w", err)
	}

	// 存储角色
	if err := stputil.SetRoles(loginID, []string{role}); err != nil {
		zap.S().Infof("[User] 存储角色失败: %v", err)
	}

	// token 过期时间（对齐 sa-token 配置，默认 30 天）
	expireSeconds := int64(2592000)

	zap.S().Infof("[User] 账号 %s (ID=%d, role=%s) 登录成功", user.Account, user.ID, role)

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
	}, nil
}

// GetByID 根据ID获取用户
func (s *UserService) GetByID(ctx context.Context, id uint) (*entity.User, error) {
	return s.repo.GetByID(ctx, id)
}

// Update 更新用户信息
func (s *UserService) Update(ctx context.Context, user *entity.User) error {
	user.UpdateTime = common.DateTimeNow()
	return s.repo.Update(ctx, user)
}

// Delete 删除用户
func (s *UserService) Delete(ctx context.Context, id uint) error {
	// 同时踢出该用户的所有登录会话（stputil.Kickout）
	_ = stputil.Kickout(id)
	return s.repo.Delete(ctx, id)
}

// List 列出所有用户
func (s *UserService) List(ctx context.Context) ([]entity.User, error) {
	return s.repo.List(ctx)
}

// Page 分页查询用户
func (s *UserService) Page(ctx context.Context, page, size int, name string) ([]entity.User, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	return s.repo.Page(ctx, page, size, name)
}

// IsExist 检查用户是否存在
func (s *UserService) IsExist(ctx context.Context, id uint) (bool, error) {
	return s.repo.IsExist(ctx, id)
}

// verifyPassword 校验密码（兼容 bcrypt 哈希 + 明文旧数据）
func (s *UserService) verifyPassword(stored, input string) bool {
	if isBcryptHash(stored) {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(input)) == nil
	}
	// 明文对比（兼容 Java 版迁移的旧数据）
	return stored == input
}

// UpgradePasswordHash 升级单个用户密码到 bcrypt（预留接口）
// 需要提供原始明文密码；通常在用户登录确认身份后调用
func (s *UserService) UpgradePasswordHash(ctx context.Context, userID uint, plainPassword string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成bcrypt哈希失败: %w", err)
	}
	return s.repo.DB.WithContext(ctx).Model(&entity.User{}).
		Where("id = ?", userID).
		Update("passwd", string(hashed)).Error
}

// isBcryptHash 判断是否为 bcrypt 哈希（固定 60 字符，以 $2a$/$2b$/$2y$ 开头）
func isBcryptHash(s string) bool {
	return len(s) == 60 && (s[:4] == "$2a$" || s[:4] == "$2b$" || s[:4] == "$2y$")
}
