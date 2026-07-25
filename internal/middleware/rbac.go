package middleware

import (
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/yu/iot-platform-go/pkg/common"
)

// casbinEnforcer 全局 Casbin 执行器
var casbinEnforcer *casbin.Enforcer

// defaultRBACModel Casbin RBAC 默认模型（RESTful 风格）
const defaultRBACModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && keyMatch(r.obj, p.obj) && regexMatch(r.act, p.act)
`

// InitEnforcer 初始化 Casbin RBAC 执行器（内存模式）
// 生产环境可替换为 GORM adapter（需要 casbin/gorm-adapter/v3）
func InitEnforcer() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(defaultRBACModel)
	if err != nil {
		return nil, err
	}

	enforcer, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}

	// 默认策略：admin 拥有所有权限
	enforcer.AddPolicy("admin", "/*", ".*")
	enforcer.AddPolicy("super-admin", "/*", ".*")
	enforcer.AddPolicy("user", "/api/user/*", "GET|POST|PUT")
	enforcer.AddPolicy("user", "/api/device/*", "GET")

	casbinEnforcer = enforcer
	return enforcer, nil
}

// GetEnforcer 获取 Casbin 执行器
func GetEnforcer() *casbin.Enforcer {
	return casbinEnforcer
}

// CheckPermission 权限检查中间件（对标 Sa-Token 的 @SaCheckPermission）
// 用法：router.Use(CheckPermission("/api/device/*", "DELETE"))
func CheckPermission(obj, act string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			common.Fail(c, common.CodeForbidden)
			c.Abort()
			return
		}

		if casbinEnforcer == nil {
			// Casbin 未初始化，回退到放行
			c.Next()
			return
		}

		ok, err := casbinEnforcer.Enforce(role.(string), obj, act)
		if err != nil {
			common.FailWithMsg(c, common.CodeServerError, "权限检查失败")
			c.Abort()
			return
		}

		if !ok {
			common.FailWithMsg(c, common.CodeForbidden, "无权限执行此操作")
			c.Abort()
			return
		}

		c.Next()
	}
}

// AddPolicy 为角色添加权限策略
func AddPolicy(role, obj, act string) (bool, error) {
	if casbinEnforcer == nil {
		return false, nil
	}
	return casbinEnforcer.AddPolicy(role, obj, act)
}

// AddRoleForUser 为用户添加角色
func AddRoleForUser(userID, role string) (bool, error) {
	if casbinEnforcer == nil {
		return false, nil
	}
	return casbinEnforcer.AddRoleForUser(userID, role)
}
