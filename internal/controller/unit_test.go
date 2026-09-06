package controller

import (
	"testing"

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
		ctl := NewDeviceController(nil, nil, nil, nil)
		assert.NotNil(t, ctl)
	})
	t.Run("UserController", func(t *testing.T) {
		ctl := NewUserController(nil)
		assert.NotNil(t, ctl)
	})
}
