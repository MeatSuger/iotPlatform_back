package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePayload_ValidJSON(t *testing.T) {
	result := parsePayload(`{"key":"value","num":42}`)
	m, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", m["key"])
	assert.Equal(t, float64(42), m["num"])
}

func TestParsePayload_InvalidJSON(t *testing.T) {
	result := parsePayload(`not valid json`)
	s, ok := result.(string)
	require.True(t, ok)
	assert.Equal(t, "not valid json", s)
}

func TestParsePayload_Array(t *testing.T) {
	result := parsePayload(`[1,2,3]`)
	arr, ok := result.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 3)
}

func TestParsePayload_EmptyString(t *testing.T) {
	result := parsePayload(``)
	s, ok := result.(string)
	require.True(t, ok)
	assert.Equal(t, ``, s)
}

func TestListData_ReturnsMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctl := &DataController{}
	r := gin.New()
	r.GET("/list", ctl.ListData)

	req := httptest.NewRequest("GET", "/list", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	resp := parseResp(w)
	assert.Equal(t, 200, resp.Code)
	data, _ := resp.Data.(map[string]any)
	assert.Equal(t, "请在具体设备下查询数据", data["message"])
}

func TestAllControllerConstructors(t *testing.T) {
	t.Run("DataController", func(t *testing.T) {
		ctl := NewDataController(nil, nil)
		assert.NotNil(t, ctl)
	})
	t.Run("DownlinkController", func(t *testing.T) {
		ctl := NewDownlinkController(nil, nil)
		assert.NotNil(t, ctl)
	})
	t.Run("DeviceController", func(t *testing.T) {
		ctl := NewDeviceController(nil, nil)
		assert.NotNil(t, ctl)
	})
	t.Run("UserController", func(t *testing.T) {
		ctl := NewUserController(nil)
		assert.NotNil(t, ctl)
	})
}
