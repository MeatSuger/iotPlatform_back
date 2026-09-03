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
