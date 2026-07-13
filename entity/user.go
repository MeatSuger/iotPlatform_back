package entity

import (
	"github.com/yu/iot-platform-go/common"
)

// User 用户实体，对应表 app_user
type User struct {
	ID         uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	Name       string          `gorm:"size:100" json:"name"`
	Age        int             `json:"age"`
	Email      string          `gorm:"size:255" json:"email"`
	Account    string          `gorm:"size:100;uniqueIndex;not null" json:"account"`
	Passwd     string          `gorm:"size:255;not null" json:"-"`         // json:"-" 防止密码泄露
	Role       string          `gorm:"size:50;default:'user'" json:"role"` // 角色: user / admin / super-admin
	Status     string          `gorm:"size:50;default:'active'" json:"status"`
	CreateTime common.DateTime `gorm:"column:create_time" json:"createTime"`
	UpdateTime common.DateTime `gorm:"column:update_time" json:"updateTime"`
}

// TableName 指定表名
func (User) TableName() string {
	return "app_user"
}

// UserRole 用户角色常量
const (
	RoleSuperAdmin = "super-admin"
	RoleAdmin      = "admin"
	RoleUser       = "user"
)

// UserStatus 用户状态常量
const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)
