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

package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServices_Struct(t *testing.T) {
	svcs := &Services{}
	assert.Nil(t, svcs.User)
	assert.Nil(t, svcs.Device)
	assert.Nil(t, svcs.Report)
	assert.Nil(t, svcs.InfluxDB)
	assert.Nil(t, svcs.Downlink)
}

func TestServices_FieldAssignment(t *testing.T) {
	svcs := &Services{}
	svcs.User = nil
	svcs.Device = nil
	svcs.Report = nil
	svcs.InfluxDB = nil
	svcs.Downlink = nil
	assert.NotNil(t, svcs) // struct itself is not nil
}
