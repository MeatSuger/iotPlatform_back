package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestInitLogger_Debug(t *testing.T) {
	// 不应 panic
	assert.NotPanics(t, func() {
		InitLogger("debug", "")
	})

	logger := zap.L()
	assert.NotNil(t, logger)
}

func TestInitLogger_Release(t *testing.T) {
	assert.NotPanics(t, func() {
		InitLogger("release", "")
	})

	logger := zap.L()
	assert.NotNil(t, logger)
}

func TestSync(t *testing.T) {
	InitLogger("debug", "")
	// Sync should not panic even if nothing to flush
	assert.NotPanics(t, func() {
		Sync()
	})
}
