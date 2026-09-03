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

// Create @Summary      用户注册
// @Tags         users, public
// @Accept       json
// @Produce      json
// @Param        body      body  service.RegisterRequest  true  "注册请求参数"
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Router       /api/users [post]
// Create 用户注册 (POST /api/users)
func (ctl *UserController) Create(c *gin.Context) {
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
// @Accept       json
// @Produce      json
// @Param        body      body  service.LoginRequest  true  "登录请求参数"
// @Success      200   {object}  common.ApiResponse{data=service.LoginResponse}
// @Failure      400   {object}  common.ApiResponse
// @Router       /api/users/login [post]
// Login 用户登录 (POST /api/users/login) — 支持 JSON body 和 Query/Form 参数
// 设备端标识优先取请求体 device 字段，缺省回退 User-Agent（用于 Sa-Token 多端登录区分）
func (ctl *UserController) Login(c *gin.Context) {
	var req service.LoginRequest
	// 优先尝试 JSON body，失败则从 Query/Form 读取
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Account = c.Query("account")
		req.Passwd = c.Query("passwd")
		req.Device = c.Query("device")
		if req.Account == "" {
			req.Account = c.PostForm("account")
			req.Passwd = c.PostForm("passwd")
			req.Device = c.PostForm("device")
		}
		if req.Account == "" {
			common.FailWithMsg(c, common.CodeBadRequest, "账号和密码不能为空")
			return
		}
	}
	if req.Device == "" {
		req.Device = c.GetHeader("User-Agent")
	}

	resp, err := ctl.userSvc.Login(c.Request.Context(), req)
	if err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	// Set-Cookie（httpOnly + 生产环境 secure + SameSite=Lax）
	common.SetAuthCookie(c, resp.TokenName, resp.TokenValue, int(resp.TokenTimeout))

	common.SuccessWithMsg(c, "登录成功", resp)
}

// IsLogin @Summary      检查登录状态
// @Tags         users, public
// @Accept       json
// @Produce      json
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Router       /api/users/isLogin [get]
// IsLogin 检查登录状态 (GET /api/users/isLogin) — Sa-Token 风格
func (ctl *UserController) IsLogin(c *gin.Context) {
	token := sagin.GetTokenFromCtx(c)

	if token == "" || !stputil.IsLogin(token) {
		common.SuccessWithMsg(c, "未登录", gin.H{
			"isLogin": false,
		})
		return
	}

	loginID, _ := stputil.GetLoginID(token)
	num, err := strconv.ParseUint(loginID, 10, 64)
	if err != nil {
		return
	}
	info, _ := stputil.GetTokenInfo(token)
	loginDevice := ""
	if info != nil {
		loginDevice = info.Device
	}
	user, _ := ctl.userSvc.GetByID(c.Request.Context(), uint(num))
	common.SuccessWithMsg(c, "已登录", gin.H{
		"isLogin":     true,
		"loginId":     loginID,
		"loginType":   "login",
		"loginDevice": loginDevice,
		"user":        user,
	})
}

// Logout @Summary      退出登录
// @Tags         users
// @Accept       json
// @Produce      json
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/users/logout [post]
// Logout 退出登录 (POST /api/users/logout)
func (ctl *UserController) Logout(c *gin.Context) {
	token := sagin.GetTokenFromCtx(c)
	if token != "" {
		_ = stputil.LogoutByToken(token)
	}
	// 清除 Cookie
	common.ClearAuthCookie(c, "Authorization")
	common.SuccessWithMsg(c, "退出登录成功", nil)
}

// Update @Summary      更新用户信息
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        userId    path      string    true  "用户ID（me=当前用户；本人或管理员可操作）"
// @Param        body      body      ent.User  true  "用户信息"
// @Success      200   {object}  common.ApiResponse{data=ent.User}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/users/{userId}/update [post]
// Update 更新用户信息 (POST /api/users/{userId}/update) — 合并语义
func (ctl *UserController) Update(c *gin.Context) {
	var req ent.User
	if err := c.ShouldBindJSON(&req); err != nil {
		common.FailWithMsg(c, common.CodeBadRequest, err.Error())
		return
	}

	// 目标用户ID从路径解析（me=当前用户）
	targetID, ok := resolveTargetUserID(c)
	if !ok {
		common.FailWithMsg(c, common.CodeBadRequest, "无效的用户ID")
		return
	}

	currentUID := middleware.GetUserID(c)
	currentRole := middleware.GetUserRole(c)

	// 权限检查：路径必须为 me 或本人ID，除非是管理员
	if currentRole != service.RoleSuperAdmin && currentRole != service.RoleAdmin && targetID != currentUID {
		common.FailWithMsg(c, common.CodeForbidden, "无权修改该用户信息")
		return
	}
	req.ID = targetID

	// 获取现有用户
	existing, err := ctl.userSvc.GetByID(c.Request.Context(), req.ID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "用户不存在")
		return
	}

	// account 是登录凭证，不允许修改（显式拒绝，避免前端误以为修改成功）
	if req.Account != "" && req.Account != existing.Account {
		common.FailWithMsg(c, common.CodeBadRequest, "账号不可修改")
		return
	}

	// 状态字段仅管理员/超级管理员可修改（普通用户无权变更自身状态）
	if req.Status != "" && currentRole != service.RoleSuperAdmin && currentRole != service.RoleAdmin {
		common.FailWithMsg(c, common.CodeForbidden, "无权修改用户状态")
		return
	}

	// 合并更新（仅开放 Name/Email/Age/Status，Account/Role 等字段不可通过此接口变更）
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

// Get @Summary      获取用户信息
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        userId    path     string   true  "用户ID（me=当前登录用户）"
// @Success      200   {object}  common.ApiResponse{data=ent.User}
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/users/{userId} [get]
// Get 获取用户信息 (GET /api/users/{userId}) — me 表示当前登录用户
func (ctl *UserController) Get(c *gin.Context) {
	// 目标用户ID从路径解析（me 或空=当前登录用户；数字=目标用户）
	targetID, ok := resolveTargetUserID(c)
	if !ok {
		common.FailWithMsg(c, common.CodeBadRequest, "无效的用户ID")
		return
	}

	currentUID := middleware.GetUserID(c)
	currentRole := middleware.GetUserRole(c)

	// 普通用户只能查看自己的信息
	if currentRole != service.RoleSuperAdmin && currentRole != service.RoleAdmin && targetID != currentUID {
		common.FailWithMsg(c, common.CodeForbidden, "无权查看该用户信息")
		return
	}

	user, err := ctl.userSvc.GetByID(c.Request.Context(), targetID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "用户不存在")
		return
	}

	common.Success(c, user)
}

// resolveTargetUserID 解析路径中的目标用户ID：
// "me" 或空 → 当前登录用户；数字 → 目标用户ID；非法 → false
func resolveTargetUserID(c *gin.Context) (uint, bool) {
	idStr := c.Param("userId")
	if idStr == "" || idStr == "me" {
		return middleware.GetUserID(c), true
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}

// List @Summary      列出所有用户
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        pageNum   query     int     false  "页码（从0开始，仅 pageSize > 0 时生效）"
// @Param        pageSize  query     int     false  "每页数量（>0 时返回分页 envelope，缺省返回全量数组）"
// @Param        name      query     string  false  "用户名（模糊匹配，仅分页时生效）"
// @Success      200      {object}  common.ApiResponse{data=[]ent.User}
// @Failure      400      {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/users [get]
// List 列出所有用户 (GET /api/users) — pageSize > 0 时返回分页 envelope（records/total/size/current/pages），否则返回全量数组
func (ctl *UserController) List(c *gin.Context) {
	pageSize, _ := strconv.Atoi(c.Query("pageSize"))

	// 分页模式：与原 Page 方法一致的响应形态
	if pageSize > 0 {
		pageNum, _ := strconv.Atoi(c.DefaultQuery("pageNum", "0"))
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
		return
	}

	// 全量模式
	users, err := ctl.userSvc.List(c.Request.Context())
	if err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}
	common.Success(c, users)
}

// Delete @Summary      删除用户
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        userId    path     string   true  "用户ID（本人或管理员可操作）"
// @Success      200   {object}  common.ApiResponse
// @Failure      400   {object}  common.ApiResponse
// @Security     UserAuth
// @Router       /api/users/{userId}/delete [post]
// Delete 删除用户 (POST /api/users/{userId}/delete)
func (ctl *UserController) Delete(c *gin.Context) {
	// 目标用户ID从路径解析（me=当前用户）
	targetID, ok := resolveTargetUserID(c)
	if !ok {
		common.FailWithMsg(c, common.CodeBadRequest, "无效的用户ID")
		return
	}

	currentUID := middleware.GetUserID(c)
	currentRole := middleware.GetUserRole(c)

	// 普通用户只能删除自己，管理员可以删除任何用户
	if currentRole != service.RoleSuperAdmin && currentRole != service.RoleAdmin && targetID != currentUID {
		common.FailWithMsg(c, common.CodeForbidden, "无权删除该用户")
		return
	}

	if err := ctl.userSvc.Delete(c.Request.Context(), targetID); err != nil {
		common.FailWithMsg(c, common.CodeServerError, err.Error())
		return
	}

	common.SuccessWithMsg(c, "删除成功", nil)
}
