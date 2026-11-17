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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	for range 500 {
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
	m, ok3 := val2.(map[string]any)
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
	for range 20 {
		wg.Go(func() {
			var out string
			results <- c.GetOrLoad(ctx, "hot", &out, time.Minute, 0, loader)
		})
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
	for i := range 600 {
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

// ---- 缓存缺陷回归测试 ----

// TestRedisCache_SensorHistoryKeyBucket 历史查询按分钟取整入键：
// 滚动窗口（每次 start/end 都变）必须命中缓存，否则历史接口必定打穿 InfluxDB。
func TestRedisCache_SensorHistoryKeyBucket(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		return []int{1}, nil
	}
	start := time.Unix(1700000000, 0)
	end := time.Unix(1700003600, 0)

	var out []int
	assert.NoError(t, c.GetCachedSensorHistoryWithLoader(ctx, "d1", 50, start, end, false, "raw", &out, loader))
	// 1 秒后的滚动窗口（同一分钟内）→ 命中同一份缓存
	assert.NoError(t, c.GetCachedSensorHistoryWithLoader(ctx, "d1", 50, start.Add(time.Second), end.Add(time.Second), false, "raw", &out, loader))
	assert.Equal(t, 1, loadCount)

	// 不同时间窗仍是不同键
	assert.NoError(t, c.GetCachedSensorHistoryWithLoader(ctx, "d1", 50, start.Add(-time.Hour), end, false, "raw", &out, loader))
	assert.Equal(t, 2, loadCount)

	// 降采样变体不能与原始点共用缓存
	assert.NoError(t, c.GetCachedSensorHistoryWithLoader(ctx, "d1", 50, start, end, false, "1h", &out, loader))
	assert.Equal(t, 3, loadCount)

	// 窄窗口（<1 个桶）必须精确入键：两端取整都会落到同一分钟，
	// 取整会让两个不同的 30 秒窗口互相串数据（返回窗口外的点）
	narrowStart := time.Unix(1700010005, 0)
	assert.NoError(t, c.GetCachedSensorHistoryWithLoader(ctx, "d1", 50, narrowStart, narrowStart.Add(30*time.Second), false, "raw", &out, loader))
	assert.NoError(t, c.GetCachedSensorHistoryWithLoader(ctx, "d1", 50, narrowStart.Add(15*time.Second), narrowStart.Add(45*time.Second), false, "raw", &out, loader))
	assert.Equal(t, 5, loadCount)
}

// TestHashInt64 Hash 字段数字解析（HSet 写入的 int64 读回为 string）
func TestHashInt64(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int64
	}{
		{"string 数字", "123456", 123456},
		{"string 负数", "-5", -5},
		{"string 非数字", "abc", 0},
		{"string 空", "", 0},
		{"int64", int64(42), 42},
		{"float64 不支持", float64(1.5), 0},
		{"nil", nil, 0},
		{"uint64 不支持", uint64(7), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, HashInt64(tt.val))
		})
	}
}

// TestRedisCache_GetOrLoad_StaleFallbackOnRedisDown Redis 故障时用进程内兜底值，
// 不再回源（否则 Redis 挂掉会把 PostgreSQL / InfluxDB 一起打穿）。
func TestRedisCache_GetOrLoad_StaleFallbackOnRedisDown(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1", // 必然连不上
		DialTimeout:  20 * time.Millisecond,
		ReadTimeout:  20 * time.Millisecond,
		WriteTimeout: 20 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	c := NewRedisCache(rdb)
	ctx := context.Background()

	loadCount := 0
	loader := func(ctx context.Context) (any, error) {
		loadCount++
		return "db-value", nil
	}

	var out string
	assert.NoError(t, c.GetOrLoad(ctx, "k", &out, time.Minute, time.Millisecond, loader))
	assert.Equal(t, "db-value", out)
	assert.Equal(t, 1, loadCount)

	time.Sleep(20 * time.Millisecond) // 让 L1 过期，只剩兜底副本

	var out2 string
	assert.NoError(t, c.GetOrLoad(ctx, "k", &out2, time.Minute, time.Millisecond, loader))
	assert.Equal(t, "db-value", out2)
	assert.Equal(t, 1, loadCount, "Redis 故障时应使用兜底值，不回源")
}

// TestRedisCache_EvictKey_ClearsNegativeCache 写路径失效必须连带清除负缓存，
// 否则紧接的读取会命中上一次的"不存在"，在 TTLNegCache 内持续 404。
func TestRedisCache_EvictKey_ClearsNegativeCache(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	var out map[string]string
	err := c.GetOrLoad(ctx, PrefixDevice+"ghost", &out, time.Minute, 0, func(ctx context.Context) (any, error) {
		return nil, redis.Nil
	})
	assert.ErrorIs(t, err, redis.Nil)
	assert.Equal(t, int64(1), c.client.Exists(ctx, negCacheKey(PrefixDevice+"ghost")).Val())

	assert.NoError(t, c.EvictDeviceCache(ctx, "ghost"))
	assert.Equal(t, int64(0), c.client.Exists(ctx, negCacheKey(PrefixDevice+"ghost")).Val())
}

// TestRedisCache_CacheDeviceStatus_PublishesInvalidate 状态写入必须广播跨实例失效
func TestRedisCache_CacheDeviceStatus_PublishesInvalidate(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	sub := c.client.Subscribe(ctx, invalidationChannel)
	defer func() { _ = sub.Close() }()
	// 必须等 SUBSCRIBE 被服务端确认再发布：PUBLISH 是发射后不管的，
	// 早发的消息会直接丢掉（固定 sleep 在慢机器上会偶发失败）
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("订阅建立失败: %v", err)
	}
	ch := sub.Channel()

	assert.NoError(t, c.CacheDeviceStatus(ctx, "d-pub", "OFFLINE", 0))
	select {
	case msg := <-ch:
		assert.Equal(t, PrefixDeviceStatus+"d-pub", msg.Payload)
	case <-time.After(time.Second):
		t.Fatal("未收到跨实例失效通知")
	}
}

// TestRedisCache_EvictDeviceAllCaches_ClearsDefs 删设备要连带清掉物模型定义缓存
func TestRedisCache_EvictDeviceAllCaches_ClearsDefs(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	c.client.Set(ctx, PrefixSensorDefs+"d1", "[]", time.Minute)
	c.client.Set(ctx, PrefixActuatorDefs+"d1", "[]", time.Minute)

	assert.NoError(t, c.EvictDeviceAllCaches(ctx, "d1"))
	assert.Equal(t, int64(0), c.client.Exists(ctx, PrefixSensorDefs+"d1").Val())
	assert.Equal(t, int64(0), c.client.Exists(ctx, PrefixActuatorDefs+"d1").Val())
}

// TestRedisCache_FastReportWrite_KeepsSensorSnapshotWhenEmpty
// 不带传感器的状态上报不应把上一次的"最新快照"抹成空
func TestRedisCache_FastReportWrite_KeepsSensorSnapshotWhenEmpty(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	assert.NoError(t, c.FastReportWrite(ctx, "d1", "ONLINE", 1, []byte(`[{"name":"t","value":1}]`), []byte(`{}`)))
	var sensors []map[string]any
	assert.NoError(t, c.GetCachedSensorRecent(ctx, "d1", &sensors))
	assert.Len(t, sensors, 1)

	assert.NoError(t, c.FastReportWrite(ctx, "d1", "ONLINE", 2, []byte(`[]`), []byte(`{}`)))
	assert.NoError(t, c.GetCachedSensorRecent(ctx, "d1", &sensors))
	assert.Len(t, sensors, 1)
}

// TestRedisCache_GetCachedDeviceStatusBatch 批量状态读取（列表页状态覆盖用）
func TestRedisCache_GetCachedDeviceStatusBatch(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	assert.NoError(t, c.CacheDeviceStatus(ctx, "d1", "ONLINE", 1700000000000))
	assert.NoError(t, c.CacheDeviceStatus(ctx, "d2", "OFFLINE", 0))

	got, err := c.GetCachedDeviceStatusBatch(ctx, []string{"d1", "d2", "missing"})
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "ONLINE", got["d1"].Status)
	assert.Equal(t, int64(1700000000000), got["d1"].LastActiveTime)
	assert.Equal(t, "OFFLINE", got["d2"].Status)
	assert.NotContains(t, got, "missing")
}

// TestLocalCache_SizeCap L1 上限对所有写入路径生效（含负缓存/兜底副本）
func TestLocalCache_SizeCap(t *testing.T) {
	lc := NewLocalCache(time.Second)
	defer lc.Close()

	for i := range 5000 {
		key := "k" + strconv.Itoa(i)
		lc.setBytes(key, []byte(`1`), time.Minute)
		lc.setNeg("neg"+strconv.Itoa(i), time.Minute)
	}

	total := 0
	for _, shard := range lc.shards {
		shard.mu.RLock()
		total += len(shard.items)
		shard.mu.RUnlock()
	}
	assert.LessOrEqual(t, total, localCacheShards*lc.maxSize)
}

// TestRedisCache_CacheDeviceStatus_ZeroKeepsLastActive 回归：文档承诺
// lastActiveTime=0 表示"未知"、不覆盖已存值；实现曾无条件 HSet 0。
func TestRedisCache_CacheDeviceStatus_ZeroKeepsLastActive(t *testing.T) {
	_, c, _ := newCacheWithMini(t)
	ctx := context.Background()

	require.NoError(t, c.CacheDeviceStatus(ctx, "dev-zero", "ONLINE", 1700000000000))
	require.NoError(t, c.CacheDeviceStatus(ctx, "dev-zero", "OFFLINE", 0))

	status, lastActive, err := c.GetCachedDeviceStatus(ctx, "dev-zero")
	require.NoError(t, err)
	assert.Equal(t, "OFFLINE", status)
	assert.Equal(t, int64(1700000000000), lastActive, "lastActiveTime=0 不应覆盖已存值")
}

// TestRedisCache_GetOrLoad_NotFoundClearsStale 回归：loader 返回 not-found 时
// 必须清除进程内 stale 兜底副本，否则 Redis 故障期间会把已删除数据重新端上来。
func TestRedisCache_GetOrLoad_NotFoundClearsStale(t *testing.T) {
	mr, c, _ := newCacheWithMini(t)
	ctx := context.Background()
	key := "stale-not-found"

	loadCount := 0
	var out string
	require.NoError(t, c.GetOrLoad(ctx, key, &out, time.Minute, 0, func(context.Context) (any, error) {
		loadCount++
		return "old-value", nil
	}))
	require.Equal(t, "old-value", out)

	// 模拟 L2 过期，再让 loader 返回 not-found（清除 stale）
	require.NoError(t, c.client.Del(ctx, key).Err())
	err := c.GetOrLoad(ctx, key, &out, time.Minute, 0, func(context.Context) (any, error) {
		loadCount++
		return nil, redis.Nil
	})
	require.ErrorIs(t, err, redis.Nil)

	// Redis 故障：stale 已清 → 必须回源，而不是返回 old-value
	mr.Close()
	// 换成必然连不上的客户端，避免 go-redis 重试退避拖慢测试
	dead := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  20 * time.Millisecond,
		ReadTimeout:  20 * time.Millisecond,
		WriteTimeout: 20 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = dead.Close() })
	c.client = dead
	var fresh string
	require.NoError(t, c.GetOrLoad(ctx, key, &fresh, time.Minute, 0, func(context.Context) (any, error) {
		loadCount++
		return "fresh-value", nil
	}))
	assert.Equal(t, "fresh-value", fresh, "not-found 后不得再返回 stale 旧值")
	assert.Equal(t, 3, loadCount, "Redis 故障且无 stale 时应调用 loader")
}
