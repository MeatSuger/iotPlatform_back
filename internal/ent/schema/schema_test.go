package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchemaPackage(t *testing.T) {
	assert.True(t, true)
}

func TestDeviceConfigFields(t *testing.T) {
	fields := DeviceConfig{}.Fields()
	assert.NotEmpty(t, fields)

	// 关键字段存在性：设备ID、版本、载荷、状态、回执
	names := map[string]bool{}
	for _, f := range fields {
		names[f.Descriptor().Name] = true
	}
	for _, want := range []string{"device_id", "version", "payload", "status", "reported_version", "reported_payload"} {
		assert.True(t, names[want], "缺少字段 %s", want)
	}
}

func TestDeviceHasConfigEdge(t *testing.T) {
	edges := Device{}.Edges()
	found := false
	for _, e := range edges {
		if e.Descriptor().Name == "config" {
			found = true
		}
	}
	assert.True(t, found, "Device 缺少 config 边（级联删除配置快照）")
}
