package middleware

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestLogger_NotNil(t *testing.T) {
	handler := Logger()
	assert.NotNil(t, handler)
}

func TestRecoveryWithZap_NotNil(t *testing.T) {
	handler := RecoveryWithZap()
	assert.NotNil(t, handler)
}
