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

package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestUserAuthCache_TTL 用户 Token 本地缓存：命中复用，过期后不再命中
func TestUserAuthCache_TTL(t *testing.T) {
	setUserAuth("tok-1", 42, "admin")

	got, ok := getUserAuth("tok-1")
	assert.True(t, ok)
	assert.Equal(t, uint(42), got.userID)
	assert.Equal(t, "admin", got.role)

	assert.False(t, func() bool { _, hit := getUserAuth("tok-missing"); return hit }())

	userAuthCacheMu.Lock()
	entry := userAuthCache["tok-1"]
	entry.expiresAt = time.Now().Add(-time.Second)
	userAuthCache["tok-1"] = entry
	userAuthCacheMu.Unlock()

	_, ok = getUserAuth("tok-1")
	assert.False(t, ok, "过期条目不应命中")
}
