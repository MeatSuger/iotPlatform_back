package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockReportService for testing
type mockReportService struct{}

func (m *mockReportService) ReportStatus(ctx interface{}, deviceID, token string, dto interface{}) error {
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
