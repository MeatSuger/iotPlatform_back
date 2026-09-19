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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockReportService 测试用的 ReportService 桩实现
type mockReportService struct{}

func (m *mockReportService) ReportStatus(ctx any, deviceID, token string, dto any) error {
	return nil
}

func TestNewUDPServer(t *testing.T) {
	mock := &mockReportService{}
	srv := NewUDPServer(nil)
	assert.NotNil(t, srv)

	_ = mock
	_ = srv
}

func TestUDPServer_StopBeforeStart(t *testing.T) {
	srv := NewUDPServer(nil)
	// Stop on a never-started server should not panic
	assert.NotPanics(t, func() {
		srv.Stop()
	})
}

func TestUDPServer_StopCalledOnce(t *testing.T) {
	srv := NewUDPServer(nil)
	// Stop should not panic
	assert.NotPanics(t, func() {
		srv.Stop()
	})
}

func TestUDPServer_StartInvalidPort(t *testing.T) {
	srv := NewUDPServer(nil)
	// Port -1 is invalid; Start should return error
	err := srv.Start(-1)
	assert.Error(t, err)
}

func TestUDPServer_StructFields(t *testing.T) {
	srv := &UDPServer{}
	assert.Nil(t, srv.reportSvc)
	assert.Nil(t, srv.conn)
	assert.Nil(t, srv.stop)
}
