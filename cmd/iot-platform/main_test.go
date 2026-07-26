package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppComponents_Struct(t *testing.T) {
	components := &AppComponents{}
	assert.Nil(t, components.Services)
	assert.Nil(t, components.WsHandler)
}
