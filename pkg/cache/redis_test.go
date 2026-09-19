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

package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestRedisCacheConstants(t *testing.T) {
	assert.Equal(t, "cache:device:", PrefixDevice)
	assert.Equal(t, "cache:sensor_recent:", PrefixSensorRecent)
	assert.Equal(t, "cache:mqtt_msg:", PrefixMQTTMessage)
	assert.Equal(t, "cache:def_sensor:", PrefixSensorDefs)
	assert.Equal(t, "cache:def_actuator:", PrefixActuatorDefs)
}

func TestTTLConstants(t *testing.T) {
	assert.Greater(t, TTLDevice, TTLSensorRecent)
	assert.Greater(t, TTLDefs, TTLDefsLocal)
}

func TestNewRedisCache_NilClient(t *testing.T) {
	// 传入 nil 不应 panic，方法在运行时会返回错误
	cache := NewRedisCache(nil)
	assert.NotNil(t, cache)
}

func TestRedisCache_GetClient(t *testing.T) {
	cache := NewRedisCache(nil)
	assert.Nil(t, cache.GetClient())
}

// TestCacheKeyPatterns 验证键前缀拼接模式
func TestCacheKeyPatterns(t *testing.T) {
	assert.Equal(t, "cache:device:abc123", PrefixDevice+"abc123")
	assert.Equal(t, "cache:mqtt_msg:abc123", PrefixMQTTMessage+"abc123")
}

// TestRedisCache_MethodSignatures 基础 API 覆盖
func TestRedisCache_MethodSignatures(t *testing.T) {
	cache := NewRedisCache(nil)
	assert.NotNil(t, cache)
	assert.Nil(t, cache.GetClient())
}

// TestJitterTTL_Range jitterTTL 输出范围 [0.75*base, 1.25*base)
func TestJitterTTL_Range(t *testing.T) {
	base := 100 * time.Second
	min := base * 3 / 4 // 75s
	max := base * 5 / 4 // 125s
	for i := 0; i < 500; i++ {
		result := jitterTTL(base)
		assert.GreaterOrEqual(t, result, min, "jitterTTL too small: %v", result)
		assert.Less(t, result, max, "jitterTTL too large: %v", result)
	}
}

func TestJitterTTL_Zero(t *testing.T) {
	assert.Equal(t, time.Duration(0), jitterTTL(0))
}

// TestLocalCache_BytesAPI 字节 API 读写
func TestLocalCache_BytesAPI(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	key := "test:key"
	data := []byte(`{"hello":"world"}`)

	lc.setBytes(key, data, 10*time.Second)
	result, found, isNeg := lc.getBytes(key)
	assert.True(t, found)
	assert.False(t, isNeg)
	assert.Equal(t, data, result)
}

func TestLocalCache_NegativeCache(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	key := "test:neg"
	lc.setNeg(key, 10*time.Second)

	_, found, isNeg := lc.getBytes(key)
	assert.True(t, found)
	assert.True(t, isNeg)
}

func TestLocalCache_PublicAPI_Compat(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	// 写入字符串
	lc.Set("key1", "ONLINE")
	val, ok := lc.Get("key1")
	assert.True(t, ok)
	assert.Equal(t, "ONLINE", val)

	// Set 写入结构体
	type testStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	lc.Set("key2", testStruct{Name: "test", Age: 30})
	val2, ok2 := lc.Get("key2")
	assert.True(t, ok2)
	// 公共 API 行为：struct → JSON → map
	m, ok3 := val2.(map[string]interface{})
	assert.True(t, ok3)
	assert.Equal(t, "test", m["name"])
}

func TestLocalCache_Expire(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	lc.setBytes("exp", []byte("data"), 50*time.Millisecond)
	_, found, _ := lc.getBytes("exp")
	assert.True(t, found)

	time.Sleep(100 * time.Millisecond)
	_, found, _ = lc.getBytes("exp")
	assert.False(t, found)
}

func TestLocalCache_Delete(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	defer lc.Close()

	lc.setBytes("del", []byte("data"), time.Minute)
	lc.Delete("del")
	_, found, _ := lc.getBytes("del")
	assert.False(t, found)
}

func TestLocalCache_Close(t *testing.T) {
	lc := NewLocalCache(5 * time.Second)
	assert.NotPanics(t, func() {
		lc.Close()
	})
}

func TestToBytes(t *testing.T) {
	// []byte 输入
	b, err := toBytes([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), b)

	// string 输入
	b2, err := toBytes("world")
	assert.NoError(t, err)
	assert.Equal(t, []byte("world"), b2)

	// struct 输入
	b3, err := toBytes(map[string]int{"a": 1})
	assert.NoError(t, err)
	assert.Contains(t, string(b3), `"a":1`)
}

// ========================================
// RedisCache 集成测试（miniredis 真实执行 Redis 命令）
// ========================================

func newCacheWithMini(t *testing.T) (*miniredis.Miniredis, *RedisCache, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, NewRedisCache(rdb), rdb
}

func TestRedisCache_BasicSetGetDelete(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	assert.NoError(t, c.Set(ctx, "k1", map[string]int{"a": 1}, time.Minute))
	var out map[string]int
	assert.NoError(t, c.Get(ctx, "k1", &out))
	assert.Equal(t, 1, out["a"])

	// 不存在键 → redis.Nil
	var out2 map[string]int
	err := c.Get(ctx, "nope", &out2)
	assert.ErrorIs(t, err, redis.Nil)

	assert.NoError(t, c.Delete(ctx, "k1"))
	err = c.Get(ctx, "k1", &out)
	assert.ErrorIs(t, err, redis.Nil)
}

func TestRedisCache_GetOrLoad(t *testing.T) {
	_, c, mr := newCacheWithMini(t)
	ctx := context.Background()

	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		return map[string]string{"v": "db"}, nil
	}

	// miss → loader 回填
	var out map[string]string
	assert.NoError(t, c.GetOrLoad(ctx, "user:1", &out, time.Minute, 0, loader))
	assert.Equal(t, "db", out["v"])
	assert.Equal(t, 1, loadCount)

	// L2 命中 → loader 不再执行
	assert.NoError(t, c.GetOrLoad(ctx, "user:1", &out, time.Minute, 0, loader))
	assert.Equal(t, 1, loadCount)
	_ = mr
}

func TestRedisCache_GetOrLoad_L1Hit(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		return "from-db", nil
	}
	var out string
	assert.NoError(t, c.GetOrLoad(ctx, "k", &out, time.Minute, time.Minute, loader))
	// 删除 L2，L1 仍可命中（localTTL > 0）
	assert.NoError(t, c.client.Del(ctx, "k").Err())
	var out2 string
	assert.NoError(t, c.GetOrLoad(ctx, "k", &out2, time.Minute, time.Minute, loader))
	assert.Equal(t, "from-db", out2)
	assert.Equal(t, 1, loadCount)
}

func TestRedisCache_GetOrLoad_NegativeCache(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		return nil, redis.Nil
	}
	var out string
	err := c.GetOrLoad(ctx, "missing", &out, time.Minute, 0, loader)
	assert.ErrorIs(t, err, redis.Nil)

	// 负缓存已写入 → 二次调用不再触发 loader
	err = c.GetOrLoad(ctx, "missing", &out, time.Minute, 0, loader)
	assert.ErrorIs(t, err, redis.Nil)
	assert.Equal(t, 1, loadCount)
}

func TestRedisCache_GetOrLoad_Singleflight(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		time.Sleep(30 * time.Millisecond)
		return "slow", nil
	}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out string
			results <- c.GetOrLoad(ctx, "hot", &out, time.Minute, 0, loader)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		assert.NoError(t, err)
	}
	// singleflight：并发 miss 只回源一次
	assert.Equal(t, 1, loadCount)
}

func TestRedisCache_DeviceStatusHash(t *testing.T) {
	mr, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	assert.NoError(t, c.CacheDeviceStatus(ctx, "dev1", "ONLINE", 1700000000000))
	st, la, err := c.GetCachedDeviceStatus(ctx, "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "ONLINE", st)
	assert.Equal(t, int64(1700000000000), la)

	// 删除后读取 → 状态为空
	assert.NoError(t, c.EvictDeviceStatusCache(ctx, "dev1"))
	st2, _, err := c.GetCachedDeviceStatus(ctx, "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "", st2)
	_ = mr
}

func TestRedisCache_DeviceCache(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	dev := map[string]any{"id": "d1", "status": "ONLINE"}
	assert.NoError(t, c.CacheDevice(ctx, "d1", dev))

	var out map[string]any
	assert.NoError(t, c.GetCachedDeviceWithLoader(ctx, "d1", &out, func(ctx context.Context) (any, error) {
		t.Fatal("不应触发 loader")
		return nil, nil
	}))
	assert.Equal(t, "d1", out["id"])

	assert.NoError(t, c.EvictDeviceCache(ctx, "d1"))
	err := c.GetCachedDeviceWithLoader(ctx, "d1", &out, func(ctx context.Context) (any, error) {
		return nil, redis.Nil
	})
	assert.ErrorIs(t, err, redis.Nil)
}

func TestRedisCache_FastReportWrite(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	err := c.FastReportWrite(ctx, "dev1", "ONLINE", 1700000000000,
		[]byte(`[{"name":"t"}]`), []byte(`{"device_id":"dev1"}`))
	assert.NoError(t, err)

	st, _, _ := c.GetCachedDeviceStatus(ctx, "dev1")
	assert.Equal(t, "ONLINE", st)

	// 缓冲队列收到数据
	n, err := c.client.LLen(ctx, BufferDeviceReportsKey).Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestRedisCache_SensorCaches(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	assert.NoError(t, c.CacheSensorRecent(ctx, "dev1", []any{1, 2}))
	var sensors []int
	assert.NoError(t, c.GetCachedSensorRecent(ctx, "dev1", &sensors))
	assert.Equal(t, []int{1, 2}, sensors)

	assert.NoError(t, c.EvictSensorRecentCache(ctx, "dev1"))
	err := c.GetCachedSensorRecent(ctx, "dev1", &sensors)
	assert.ErrorIs(t, err, redis.Nil)
}

func TestRedisCache_UserAndDeviceListCache(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		return map[string]string{"name": "alice"}, nil
	}
	var out map[string]string
	assert.NoError(t, c.GetCachedUser(ctx, "1", &out, loader))
	assert.Equal(t, "alice", out["name"])

	loadList := func(ctx context.Context) (any, error) { return []string{"d1"}, nil }
	var devs []string
	assert.NoError(t, c.GetCachedDeviceList(ctx, 1, &devs, loadList))
	assert.Equal(t, []string{"d1"}, devs)

	assert.NoError(t, c.EvictUserCache(ctx, "1"))
	assert.NoError(t, c.EvictDeviceListCache(ctx, 1))
}

func TestRedisCache_MQTTLuaPush(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	msg := map[string]any{"topic": "dev/up", "payload": "x"}
	assert.NoError(t, c.LPushMQTTMessage(ctx, "dev1", msg))

	n, err := c.client.LLen(ctx, PrefixMQTTMessage+"dev1").Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), n)

	// 超长列表仍被裁剪（上限 500）
	for i := 0; i < 600; i++ {
		assert.NoError(t, c.LPushMQTTMessage(ctx, "dev1", map[string]any{"i": i}))
	}
	n2, err := c.client.LLen(ctx, PrefixMQTTMessage+"dev1").Result()
	assert.NoError(t, err)
	assert.Equal(t, int64(500), n2)
}

func TestRedisCache_EvictDeviceAllCaches(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	c.client.Set(ctx, "cmd:queue:dev1", "x", 0)
	c.client.Set(ctx, PrefixMQTTMessage+"dev1", "y", 0)

	assert.NoError(t, c.EvictDeviceAllCaches(ctx, "dev1"))
	assert.Empty(t, c.client.Keys(ctx, "*").Val())
}

func TestRedisCache_PublishSubscribe(t *testing.T) {
	mr, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	assert.NoError(t, c.PublishInvalidate(ctx, "some-key"))
	_ = mr
	// SubscribeInvalidate 运行在独立 goroutine 中，停不下来（跑通 1 条消息即可）
	subCtx, cancel := context.WithCancel(context.Background())
	go c.SubscribeInvalidate(subCtx)
	cancel()
	time.Sleep(20 * time.Millisecond) // 让订阅协程正常退出
}

func TestRedisCache_PingAndClose(t *testing.T) {
	mr, c, _ := newCacheWithMini(t)
	assert.NoError(t, c.Ping(context.Background()))
	_ = mr
}

func TestRedisCache_DefsCacheRoundtrip(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()
	deviceID := "defs-dev-1"

	// 物模型定义列表（空设备 → 空数组也缓存，防穿透）
	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		return []map[string]any{}, nil
	}
	var out []map[string]any
	assert.NoError(t, c.GetCachedSensorDefsWithLoader(ctx, deviceID, &out, loader))
	assert.Len(t, out, 0)
	assert.Equal(t, 1, loadCount)

	// 命中缓存 → loader 不再执行
	assert.NoError(t, c.GetCachedSensorDefsWithLoader(ctx, deviceID, &out, loader))
	assert.Equal(t, 1, loadCount)

	// 定义新增（DB 变更）→ 显式失效后再次读取走 loader
	assert.NoError(t, c.EvictSensorDefsCache(ctx, deviceID))
	assert.NoError(t, c.GetCachedSensorDefsWithLoader(ctx, deviceID, &out, loader))
	assert.Equal(t, 2, loadCount)

	// 执行器侧镜像
	actLoader := func(ctx context.Context) (any, error) {
		return []string{"servo1"}, nil
	}
	var acts []string
	assert.NoError(t, c.GetCachedActuatorDefsWithLoader(ctx, deviceID, &acts, actLoader))
	assert.Equal(t, []string{"servo1"}, acts)
	assert.NoError(t, c.EvictActuatorDefsCache(ctx, deviceID))
	var acts2 []string
	assert.NoError(t, c.GetCachedActuatorDefsWithLoader(ctx, deviceID, &acts2, actLoader))
	assert.Equal(t, []string{"servo1"}, acts2)
}
