// 物联网设备接入与物模型管理平台软件（物咸通）V1.0
// Copyright (C) 2025-2026 余昊
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
