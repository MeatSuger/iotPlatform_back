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
	// 无内容可刷新时也不应 panic
	assert.NotPanics(t, func() {
		Sync()
	})
}
