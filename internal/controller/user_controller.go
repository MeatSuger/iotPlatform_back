package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"
	sagin "github.com/sa-tokens/sa-token-go/integrations/gin"
	"github.com/sa-tokens/sa-token-go/stputil"
	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/middleware"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
)

// UserController 用户控制器
type UserController struct {
	userSvc *service.UserService
}

// NewUserController 创建用户控制器
func NewUserController(userSvc *service.UserService) *UserController {
	return &UserController{userSvc: userSvc}
}

// Register @Summary      用户注册
// @Tags         users, public
// @Accept       JSON
// @Produce      JSON
// @Param        body      service.RegisterRequest  true  "注册请求参数"
// @Success      200   {object}  common.ApiResponse{data=ent.User}
// @Failure      400   {object}  common.ApiResponse
// @Router       /api/user/register [post]
// Register 用户注册 (POST /user/register)
func (ctl *UserController) Register(c *gin.Context) {
	var req service.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	_, err := ctl.userSvc.Register(c.Request.Context(), req)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	common.SuccessWithMsg(c, "注册成功", nil)
}

// Login @Summary      用户登录
// @Tags         users, public
// @Accept       JSON
// @Produce      JSON
// @Param        body      service.LoginRequest  true  "登录请求参数"
// @Success      200   {object}  common.ApiResponse{data=service.LoginResponse}
// @Failure      400   {object}  common.ApiResponse
// @Router       /api/user/login [post]
// Login 用户登录 (POST /user/login) — 支持 JSON body 和 Query/Form 参数
func (ctl *UserController) Login(c *gin.Context) {
	var req service.LoginRequest
	// 优先尝试 JSON body，失败则从 Query/Form 读取
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Account = c.Query("account")
		req.Passwd = c.Query("passwd")
		if req.Account == "" {
			req.Account = c.PostForm("account")
			req.Passwd = c.PostForm("passwd")
		}
		if req.Account == "" {
			common.FailWithMsg(c, common.CodeBadRequest, "账号和密码不能为空")
			return
		}
	}

	resp, err := ctl.userSvc.Login(c.Request.Context(), req)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	// Set-Cookie（对齐 Java Sa-Token 行为：登录后自动下发 Cookie）
	c.SetCookie(
		resp.TokenName,         // "Authorization"
		resp.TokenValue,        // token 值
		int(resp.TokenTimeout), // maxAge（秒）
		"/",                    // path
		"",                     // domain
		false,                  // secure
		false,                  // httpOnly
	)

	common.SuccessWithMsg(c, "登录成功", resp)
}

// IsLogin @Summary      检查登录状态
// @Tags         users, public
// @Accept       JSON
// @Produce      JSON
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Router       /api/user/isLogin [get]
// IsLogin 检查登录状态 (GET /user/isLogin) — Sa-Token 风格
func (ctl *UserController) IsLogin(c *gin.Context) {
	token := sagin.GetTokenFromCtx(c)

	if token == "" || !stputil.IsLogin(token) {
		common.SuccessWithMsg(c, "未登录", gin.H{
			"isLogin": false,
		})
		return
	}

	loginID, _ := stputil.GetLoginID(token)
	common.SuccessWithMsg(c, "已登录", gin.H{
		"isLogin":     true,
		"loginId":     loginID,
		"loginType":   "login",
		"loginDevice": "default-device",
	})
}

// Logout @Summary      退出登录
// @Tags         users
// @Accept       JSON
// @Produce      JSON
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/user/logout [post]
// Logout 退出登录 (POST /user/logout)
func (ctl *UserController) Logout(c *gin.Context) {
	token := sagin.GetTokenFromCtx(c)
	if token != "" {
		_ = stputil.LogoutByToken(token)
	}
	// 清除 Cookie
	c.SetCookie("Authorization", "", -1, "/", "", false, false)
	common.SuccessWithMsg(c, "退出登录成功", nil)
}

// Update @Summary      更新用户信息
// @Tags         users
// @Accept       JSON
// @Produce      JSON
// @Param        body      ent.User  true  "用户信息"
// @Success      200   {object}  common.ApiResponse{data=ent.User}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/user [put]
// Update 更新用户信息 (PUT /user)
func (ctl *UserController) Update(c *gin.Context) {
	var req ent.User
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	currentUID := middleware.GetUserID(c)
	currentRole := middleware.GetUserRole(c)

	// 权限检查：只能修改自己的信息，除非是管理员
	if currentRole != service.RoleSuperAdmin && currentRole != service.RoleAdmin {
		req.ID = currentUID
	} else if req.ID == 0 {
		req.ID = currentUID
	}

	// 获取现有用户
	existing, err := ctl.userSvc.GetByID(c.Request.Context(), req.ID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "用户不存在")
		return
	}

	// 合并更新
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Email != "" {
		existing.Email = req.Email
	}
	if req.Age > 0 {
		existing.Age = req.Age
	}
	if req.Status != "" {
		existing.Status = req.Status
	}

	if err := ctl.userSvc.Update(c.Request.Context(), existing); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.Success(c, existing)
}

// GetProfile @Summary      获取用户信息
// @Tags         users
// @Accept       JSON
// @Produce      JSON
// @Param        id    query     int     false  "用户ID"
// @Success      200   {object}  common.ApiResponse{data=ent.User}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/user/profile [get]
// GetProfile 获取用户信息 (GET /user/profile)
func (ctl *UserController) GetProfile(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		// 没有指定ID，返回当前登录用户
		uid := middleware.GetUserID(c)
		user, err := ctl.userSvc.GetByID(c.Request.Context(), uid)
		if err != nil {
			common.FailWithMsg(c, common.CodeNotFound, "用户不存在")
			return
		}
		common.Success(c, user)
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, "无效的用户ID")
		return
	}

	currentUID := middleware.GetUserID(c)
	currentRole := middleware.GetUserRole(c)

	// 普通用户只能查看自己的信息
	if currentRole != service.RoleSuperAdmin && currentRole != service.RoleAdmin && uint(id) != currentUID {
		common.FailWithMsg(c, common.CodeForbidden, "无权查看该用户信息")
		return
	}

	user, err := ctl.userSvc.GetByID(c.Request.Context(), uint(id))
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "用户不存在")
		return
	}

	common.Success(c, user)
}

// List @Summary      列出所有用户
// @Tags         users
// @Accept       JSON
// @Produce      JSON
// @Success      200   {object}  common.ApiResponse{data=[]ent.User}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/user/list [get]
// List 列出所有用户 (GET /user/list)
func (ctl *UserController) List(c *gin.Context) {
	users, err := ctl.userSvc.List(c.Request.Context())
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.Success(c, users)
}

// Page @Summary      分页查询用户
// @Tags         users
// @Accept       JSON
// @Produce      JSON
// @Param        pageNum  query     int     false  "页码（从0开始）"
// @Param        pageSize query     int     false  "每页数量"
// @Param        name     query     string  false  "用户名"
// @Success      200      {object}  common.ApiResponse{data=map[string]any}
// @Failure      400      {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/user/page [get]
// Page 分页查询用户 (GET /user/page)
func (ctl *UserController) Page(c *gin.Context) {
	pageNum, _ := strconv.Atoi(c.DefaultQuery("pageNum", "0"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	name := c.Query("name")

	users, total, err := ctl.userSvc.Page(c.Request.Context(), pageNum+1, pageSize, name)
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	// 计算总页数
	pages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		pages++
	}

	common.Success(c, gin.H{
		"records": users,
		"total":   total,
		"size":    pageSize,
		"current": pageNum,
		"pages":   pages,
	})
}

// Delete @Summary      删除用户
// @Tags         users
// @Accept       JSON
// @Produce      JSON
// @Param        id    query     int     true   "用户ID"
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/user/delete [post]
// Delete 删除用户 (POST /user/delete)
func (ctl *UserController) Delete(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		common.FailWithMsg(c, common.CodeBadRequest, "缺少用户ID")
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, "无效的用户ID")
		return
	}

	currentUID := middleware.GetUserID(c)
	currentRole := middleware.GetUserRole(c)

	// 普通用户只能删除自己，管理员可以删除任何用户
	if currentRole != service.RoleSuperAdmin && currentRole != service.RoleAdmin && uint(id) != currentUID {
		common.FailWithMsg(c, common.CodeForbidden, "无权删除该用户")
		return
	}

	if err := ctl.userSvc.Delete(c.Request.Context(), uint(id)); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "删除成功", nil)
}
