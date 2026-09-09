package common

import (
	"encoding/json"
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

func TestApiResponse_Serialization(t *testing.T) {
	resp := ApiResponse{
		Code:    200,
		Message: "success",
		Data:    map[string]any{"id": 1},
	}
	b, err := json.Marshal(resp)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"code":200`)
	assert.Contains(t, string(b), `"message":"success"`)
}
