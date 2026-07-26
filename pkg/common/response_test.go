package common

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupGin() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	return c, w
}

func TestGetMsg(t *testing.T) {
	assert.Equal(t, "success", getMsg(CodeSuccess))
	assert.Equal(t, "请求参数错误", getMsg(CodeBadRequest))
	assert.Equal(t, "未登录或Token已过期", getMsg(CodeUnauthorized))
	assert.Equal(t, "无权限", getMsg(CodeForbidden))
	assert.Equal(t, "资源不存在", getMsg(CodeNotFound))
	assert.Equal(t, "服务器内部错误", getMsg(CodeServerError))
	assert.Equal(t, "未知错误", getMsg(999))
}

func TestSuccess(t *testing.T) {
	c, w := setupGin()
	Success(c, gin.H{"key": "value"})

	assert.Equal(t, 200, w.Code)
	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeSuccess, resp.Code)
	assert.Equal(t, "success", resp.Message)
	assert.NotNil(t, resp.Data)
}

func TestSuccessWithMsg(t *testing.T) {
	c, w := setupGin()
	SuccessWithMsg(c, "操作成功", gin.H{"id": 1})

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeSuccess, resp.Code)
	assert.Equal(t, "操作成功", resp.Message)
}

func TestFail(t *testing.T) {
	c, w := setupGin()
	Fail(c, CodeBadRequest)

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeBadRequest, resp.Code)
	assert.NotEmpty(t, resp.Message)
	assert.Nil(t, resp.Data)
}

func TestFailWithMsg(t *testing.T) {
	c, w := setupGin()
	FailWithMsg(c, CodeForbidden, "无权限访问")

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeForbidden, resp.Code)
	assert.Equal(t, "无权限访问", resp.Message)
}

func TestFailWithData(t *testing.T) {
	c, w := setupGin()
	FailWithData(c, CodeBadRequest, "验证失败", gin.H{"field": "name"})

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeBadRequest, resp.Code)
	assert.Equal(t, "验证失败", resp.Message)
	assert.NotNil(t, resp.Data)
}

func TestError(t *testing.T) {
	c, w := setupGin()
	Error(c, "数据库错误")

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeServerError, resp.Code)
	assert.Equal(t, "数据库错误", resp.Message)
}

func TestRespond_Success(t *testing.T) {
	c, w := setupGin()
	Respond(c, gin.H{"id": 1}, nil)

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeSuccess, resp.Code)
	assert.Equal(t, "success", resp.Message)
}

func TestRespond_AppError(t *testing.T) {
	c, w := setupGin()
	appErr := NewAppError(401, CodeUnauthorized, "未登录")
	Respond(c, nil, appErr)

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeUnauthorized, resp.Code)
	assert.Equal(t, "未登录", resp.Message)
}

func TestRespond_PlainError(t *testing.T) {
	c, w := setupGin()
	plainErr := errors.New("未知错误")
	Respond(c, nil, plainErr)

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeServerError, resp.Code)
	assert.Equal(t, "未知错误", resp.Message)
}

func TestHandleServiceError_AppError(t *testing.T) {
	c, w := setupGin()
	appErr := NewAppError(404, CodeNotFound, "用户不存在")
	HandleServiceError(c, appErr)

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeNotFound, resp.Code)
	assert.Equal(t, "用户不存在", resp.Message)
}

func TestHandleServiceError_PlainError(t *testing.T) {
	c, w := setupGin()
	plainErr := errors.New("数据库连接失败")
	HandleServiceError(c, plainErr)

	var resp ApiResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, CodeBadRequest, resp.Code)
	assert.Equal(t, "数据库连接失败", resp.Message)
}

func TestApiResponse_Serialization(t *testing.T) {
	resp := ApiResponse{
		Code:    200,
		Message: "success",
		Data:    map[string]interface{}{"id": 1},
	}
	b, err := json.Marshal(resp)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"code":200`)
	assert.Contains(t, string(b), `"message":"success"`)
}
