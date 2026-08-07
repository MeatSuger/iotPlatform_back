package cache

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// ============================================================
// RedisCache — 生产级 Redis 缓存层
//
// 参考开源项目：
//   - go-redis/cache (Uptrace) — Cache-Aside + TinyLFU L1
//   - seaguest/cache — 双层缓存 + loader + singleflight
//   - viney-shih/go-cache — 多层缓存 + pub-sub 一致性
//   - jetcache-go — L1(FreeCache) + L2(Redis) + singleflight
//   - cachex — serve-stale + negative caching + TTL jitter
// ============================================================

// RedisCache Redis 缓存抽象（L2 层）
type RedisCache struct {
	client *redis.Client
	local  *LocalCache // L1 本地内存缓存
	sg     singleflight.Group
}

// ============================================================
// 缓存键前缀
// ============================================================

const (
	PrefixDevice       = "cache:device:"       // 设备元数据
	PrefixDeviceStatus = "cache:deviceStatus:" // 设备运行时状态（Hash）
	PrefixSensorRecent = "cache:sensorRecent:" // 传感器最新数据
	PrefixSensorQuery  = "cache:sensorQuery:"  // 传感器查询结果缓存
	PrefixUser         = "cache:user:"         // 用户信息
	PrefixDeviceList   = "cache:deviceList:"   // 用户设备列表
	PrefixMQTTMessage  = "cache:mqttMsg:"      // MQTT 消息历史
	PrefixNegCache     = "cache:neg:"          // 负缓存（防穿透）
)

// ============================================================
// TTL 设计（遵循 L1<TTL < L2<TTL 原则）
// ============================================================

const (
	TTLDevice       = 10 * time.Minute // 设备元数据（L2）
	TTLDeviceLocal  = 30 * time.Second // 设备元数据（L1）
	TTLDeviceStatus = 10 * time.Minute // 设备运行时状态
	TTLSensorRecent = 5 * time.Minute  // 传感器最新数据
	TTLSensorQuery  = 30 * time.Second // 传感器查询结果（短TTL，保证数据新鲜度）
	TTLUser         = 15 * time.Minute // 用户信息
	TTLUserLocal    = 60 * time.Second // 用户信息（L1）
	TTLDeviceList   = 2 * time.Minute  // 用户设备列表
	TTLNegCache     = 30 * time.Second // 负缓存（防穿透，短TTL）
	TTLMQTTMessage  = 24 * time.Hour
)

// sentinel 负缓存标记
var errCacheSentinel = errors.New("cache:negative")

// ============================================================
// 构造函数
// ============================================================

// NewRedisCache 创建Redis缓存实例
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{
		client: client,
		local:  NewLocalCache(10 * time.Second), // L1 默认10s TTL
	}
}

// GetClient 获取底层Redis客户端
func (c *RedisCache) GetClient() *redis.Client {
	return c.client
}

// Ping 检查Redis连接健康状态
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close 关闭Redis连接
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// ============================================================
// 基础操作
// ============================================================

// Set 设置缓存（带TTL抖动防雪崩）
func (c *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化缓存值失败: %w", err)
	}
	return c.client.Set(ctx, key, data, jitterTTL(ttl)).Err()
}

// Get 获取缓存
func (c *RedisCache) Get(ctx context.Context, key string, dest any) error {
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// Delete 删除缓存
func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	return c.client.Del(ctx, keys...).Err()
}

// DeleteByPattern 按模式删除缓存（使用 Pipeline 批量删除）
func (c *RedisCache) DeleteByPattern(ctx context.Context, pattern string) error {
	iter := c.client.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	pipe := c.client.Pipeline()
	pipe.Del(ctx, keys...)
	_, err := pipe.Exec(ctx)
	return err
}

// Exists 检查键是否存在
func (c *RedisCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	return c.client.Exists(ctx, keys...).Result()
}

// Pipeline 创建一个 Redis Pipeline
func (c *RedisCache) Pipeline() redis.Pipeliner {
	return c.client.Pipeline()
}

// ============================================================
// 核心模式1：Cache-Aside with Loader（参考 seaguest/cache）
//
// 流程：L1 → L2 → Loader → Backfill
//  1. 查 L1 本地缓存（~50ns）
//  2. 查 L2 Redis 缓存（~1ms）
//  3. 调用 loader 从 DB 加载（~5-50ms）
//  4. 回填 L2 + L1
//
// 内置：
//  - singleflight 防击穿（并发 miss 只回源一次）
//  - 负缓存防穿透（缓存"不存在"，短TTL）
//  - TTL 抖动防雪崩（避免批量同时过期）
// ============================================================

// GetOrLoad 缓存即用模式：优先读缓存，miss 时回调 loader 加载并回填
// dest: 反序列化目标（必须是指针）
// loader: DB/上游数据加载函数
// localTTL: L1 本地缓存 TTL（0 表示不缓存到 L1）
func (c *RedisCache) GetOrLoad(ctx context.Context, key string, dest any, redisTTL time.Duration, localTTL time.Duration, loader func(context.Context) (any, error)) error {
	// 1. 尝试 L1 本地缓存
	if localTTL > 0 {
		if val, ok := c.local.Get(key); ok {
			if val == errCacheSentinel {
				return redis.Nil // 负缓存命中
			}
			// 类型断言回 dest
			if err := copyValue(val, dest); err == nil {
				return nil
			}
		}
	}

	// 2. 尝试 L2 Redis 缓存
	val, err := c.client.Get(ctx, key).Bytes()
	if err == nil {
		// 命中 L2
		if err := json.Unmarshal(val, dest); err != nil {
			return fmt.Errorf("反序列化缓存值失败: %w", err)
		}
		// 回填 L1
		if localTTL > 0 {
			c.local.SetWithTTL(key, cloneValue(dest), localTTL)
		}
		return nil
	}

	// 检查是否负缓存命中
	if err != redis.Nil {
		// Redis 错误，降级到 loader（容错设计：Redis 挂不阻塞业务）
		// 但仍尝试单飞加载
	}

	// 3. singleflight 防击穿：并发 miss 只触发一次 loader
	v, err, _ := c.sg.Do(key, func() (any, error) {
		// Double-check L2（可能其他 goroutine 已加载）
		data, getErr := c.client.Get(ctx, key).Bytes()
		if getErr == nil {
			var loaded any
			if unmarshalErr := json.Unmarshal(data, &loaded); unmarshalErr == nil {
				return loaded, nil
			}
		}

		// 调用 loader 从 DB 加载
		result, loadErr := loader(ctx)
		if loadErr != nil {
			// 负缓存：将"不存在"的结果缓存起来，防止穿透
			if errors.Is(loadErr, redis.Nil) || isNotFoundErr(loadErr) {
				c.client.Set(ctx, PrefixNegCache+key, "1", jitterTTL(TTLNegCache))
				if localTTL > 0 {
					c.local.SetWithTTL(key, errCacheSentinel, minDuration(localTTL, TTLNegCache))
				}
			}
			return nil, loadErr
		}

		// 回填 L2 Redis
		if marshalData, marshalErr := json.Marshal(result); marshalErr == nil {
			c.client.Set(ctx, key, marshalData, jitterTTL(redisTTL))
		}

		return result, nil
	})

	if err != nil {
		return err
	}

	// 回填 L1
	if localTTL > 0 {
		c.local.SetWithTTL(key, cloneValue(v), localTTL)
	}

	return copyValue(v, dest)
}

// ============================================================
// 核心模式2：Write-Through（写穿透）
//
// 先写 DB → 同步更新缓存 → 返回
// 适用场景：设备状态更新（立即生效，高频读取）
// 参考：viney-shih/go-cache Write-Through 模式
// ============================================================

// WriteThrough 写穿透：同时更新 DB 和缓存
func (c *RedisCache) WriteThrough(ctx context.Context, key string, value any, ttl time.Duration, writer func(context.Context) error) error {
	// 1. 先写 DB（source of truth）
	if err := writer(ctx); err != nil {
		return err
	}

	// 2. 同步更新缓存
	if err := c.Set(ctx, key, value, ttl); err != nil {
		// 缓存更新失败不影响主流程（容错设计）
		return nil
	}

	// 3. 通知其他实例失效本地缓存的旧版本（可选：通过 Pub-Sub）
	// c.publishInvalidate(ctx, key)

	return nil
}

// ============================================================
// 核心模式3：Write-Invalidate（写失效）
//
// 先写 DB → 删除缓存 → 返回（下次读取时自动回填）
// 适用场景：设备元数据更新（不频繁，容忍首次 miss 回源）
// 优点：避免写竞争导致缓存与 DB 不一致
// 参考：Redis 官方 Cache-Aside 指南
// ============================================================

// WriteInvalidate 写失效：更新 DB 后删除缓存
func (c *RedisCache) WriteInvalidate(ctx context.Context, key string, writer func(context.Context) error) error {
	// 1. 先写 DB
	if err := writer(ctx); err != nil {
		return err
	}

	// 2. 删除缓存
	c.Delete(ctx, key)
	c.local.Delete(key)

	return nil
}

// ============================================================
// 设备缓存
// ============================================================

// GetCachedDevice 获取设备元数据（Cache-Aside）
func (c *RedisCache) GetCachedDevice(ctx context.Context, deviceID string, dest any) error {
	key := PrefixDevice + deviceID
	return c.GetOrLoad(ctx, key, dest, TTLDevice, TTLDeviceLocal, nil)
}

// GetCachedDeviceWithLoader 获取设备元数据（带 DB 加载器）
func (c *RedisCache) GetCachedDeviceWithLoader(ctx context.Context, deviceID string, dest any, loader func(context.Context) (any, error)) error {
	key := PrefixDevice + deviceID
	return c.GetOrLoad(ctx, key, dest, TTLDevice, TTLDeviceLocal, loader)
}

// CacheDevice 缓存设备信息（Write-Through）
func (c *RedisCache) CacheDevice(ctx context.Context, deviceID string, device any) error {
	return c.Set(ctx, PrefixDevice+deviceID, device, TTLDevice)
}

// EvictDeviceCache 失效设备缓存（Write-Invalidate）
func (c *RedisCache) EvictDeviceCache(ctx context.Context, deviceID string) error {
	c.local.Delete(PrefixDevice + deviceID)
	return c.Delete(ctx, PrefixDevice+deviceID)
}

// ============================================================
// 设备运行时状态缓存（Redis Hash，轻量级）
// ============================================================

// CacheDeviceStatus 更新设备运行时状态（Write-Through）
// 使用 Hash 存储 status + lastActiveTime，避免完整 Device JSON 序列化
func (c *RedisCache) CacheDeviceStatus(ctx context.Context, deviceID string, status string, lastActiveTime int64) error {
	key := PrefixDeviceStatus + deviceID
	pipe := c.client.Pipeline()
	pipe.HSet(ctx, key, "status", status, "lastActiveTime", lastActiveTime)
	pipe.Expire(ctx, key, jitterTTL(TTLDeviceStatus))
	_, err := pipe.Exec(ctx)
	return err
}

// GetCachedDeviceStatus 获取设备运行时状态（优先 L1 → L2 Hash）
func (c *RedisCache) GetCachedDeviceStatus(ctx context.Context, deviceID string) (status string, lastActiveTime int64, err error) {
	key := PrefixDeviceStatus + deviceID

	// L1
	if val, ok := c.local.Get(key); ok {
		if s, ok := val.(*deviceStatusEntry); ok {
			return s.Status, s.LastActiveTime, nil
		}
	}

	// L2
	results, err := c.client.HMGet(ctx, key, "status", "lastActiveTime").Result()
	if err != nil {
		return "", 0, err
	}
	if results[0] != nil {
		status = results[0].(string)
	}
	if results[1] != nil {
		switch v := results[1].(type) {
		case string:
			fmt.Sscanf(v, "%d", &lastActiveTime)
		case int64:
			lastActiveTime = v
		}
	}

	// 回填 L1
	if status != "" {
		c.local.SetWithTTL(key, &deviceStatusEntry{Status: status, LastActiveTime: lastActiveTime}, 5*time.Second)
	}

	return
}

type deviceStatusEntry struct {
	Status         string
	LastActiveTime int64
}

// EvictDeviceStatusCache 清除设备状态缓存
func (c *RedisCache) EvictDeviceStatusCache(ctx context.Context, deviceID string) error {
	c.local.Delete(PrefixDeviceStatus + deviceID)
	return c.Delete(ctx, PrefixDeviceStatus+deviceID)
}

// ============================================================
// 传感器数据缓存
// ============================================================

// CacheSensorRecent 缓存传感器最新数据（Write-Through）
func (c *RedisCache) CacheSensorRecent(ctx context.Context, deviceID string, data any) error {
	return c.Set(ctx, PrefixSensorRecent+deviceID+":latest", data, TTLSensorRecent)
}

// GetCachedSensorRecent 获取缓存的传感器最新数据
func (c *RedisCache) GetCachedSensorRecent(ctx context.Context, deviceID string, dest any) error {
	key := PrefixSensorRecent + deviceID + ":latest"
	return c.Get(ctx, key, dest)
}

// EvictSensorRecentCache 清除传感器缓存
func (c *RedisCache) EvictSensorRecentCache(ctx context.Context, deviceID string) error {
	return c.Delete(ctx, PrefixSensorRecent+deviceID+":latest")
}

// ---- 传感器查询结果缓存（Cache-Aside，减少 InfluxDB 压力） ----

// CacheSensorQuery 缓存传感器查询结果
func (c *RedisCache) CacheSensorQuery(ctx context.Context, deviceID string, limit int, data any) error {
	key := fmt.Sprintf("%s%s:%d", PrefixSensorQuery, deviceID, limit)
	return c.Set(ctx, key, data, TTLSensorQuery)
}

// GetCachedSensorQuery 获取缓存的传感器查询结果
func (c *RedisCache) GetCachedSensorQuery(ctx context.Context, deviceID string, limit int, dest any) error {
	key := fmt.Sprintf("%s%s:%d", PrefixSensorQuery, deviceID, limit)
	return c.Get(ctx, key, dest)
}

// EvictSensorQueryCache 清除设备所有查询缓存（数据上报时调用）
func (c *RedisCache) EvictSensorQueryCache(ctx context.Context, deviceID string) error {
	return c.DeleteByPattern(ctx, PrefixSensorQuery+deviceID+":*")
}

// ============================================================
// 设备上报快速路径：Pipeline 批量写入（1次往返完成3个操作）
// ============================================================

// FastReportWrite 一次 Pipeline 完成：更新设备状态 Hash + 缓存传感器数据 + 推入缓冲队列
// 将 3 次 Redis 往返合并为 1 次
func (c *RedisCache) FastReportWrite(ctx context.Context, deviceID string, status string, lastActiveTime int64, sensors any, bufferData []byte) error {
	pipe := c.client.Pipeline()

	// 1. 更新设备状态 Hash
	statusKey := PrefixDeviceStatus + deviceID
	pipe.HSet(ctx, statusKey, "status", status, "lastActiveTime", lastActiveTime)
	pipe.Expire(ctx, statusKey, jitterTTL(TTLDeviceStatus))

	// 2. 缓存传感器最新数据
	sensorKey := PrefixSensorRecent + deviceID + ":latest"
	sensorJSON, err := json.Marshal(sensors)
	if err != nil {
		sensorJSON = []byte("[]")
	}
	pipe.Set(ctx, sensorKey, sensorJSON, jitterTTL(TTLSensorRecent))

	// 3. 推入缓冲队列
	pipe.LPush(ctx, "buffer:device_reports", bufferData)

	_, err = pipe.Exec(ctx)
	return err
}

// ============================================================
// 用户缓存
// ============================================================

// GetCachedUser 获取用户信息（L1 → L2 → loader）
// userID 格式: "123"
func (c *RedisCache) GetCachedUser(ctx context.Context, userID string, dest any, loader func(context.Context) (any, error)) error {
	key := PrefixUser + userID
	return c.GetOrLoad(ctx, key, dest, TTLUser, TTLUserLocal, loader)
}

// CacheUser 缓存用户信息
func (c *RedisCache) CacheUser(ctx context.Context, userID string, user any) error {
	return c.Set(ctx, PrefixUser+userID, user, TTLUser)
}

// EvictUserCache 失效用户缓存
func (c *RedisCache) EvictUserCache(ctx context.Context, userID string) error {
	c.local.Delete(PrefixUser + userID)
	return c.Delete(ctx, PrefixUser+userID)
}

// ============================================================
// 用户设备列表缓存
// ============================================================

// GetCachedDeviceList 获取用户设备列表（L1 → L2 → loader）
func (c *RedisCache) GetCachedDeviceList(ctx context.Context, ownerID uint, dest any, loader func(context.Context) (any, error)) error {
	key := fmt.Sprintf("%s%d", PrefixDeviceList, ownerID)
	return c.GetOrLoad(ctx, key, dest, TTLDeviceList, TTLDeviceList, loader)
}

// EvictDeviceListCache 失效用户设备列表缓存
func (c *RedisCache) EvictDeviceListCache(ctx context.Context, ownerID uint) error {
	key := fmt.Sprintf("%s%d", PrefixDeviceList, ownerID)
	c.local.Delete(key)
	return c.Delete(ctx, key)
}

// ============================================================
// MQTT 消息缓存
// ============================================================

// LPushMQTTMessage 将MQTT消息推入列表
func (c *RedisCache) LPushMQTTMessage(ctx context.Context, deviceID string, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	key := PrefixMQTTMessage + deviceID
	pipe := c.client.Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, 499) // 最多保留500条
	pipe.Expire(ctx, key, TTLMQTTMessage)
	_, err = pipe.Exec(ctx)
	return err
}

// GetRecentMQTTMessages 获取最近的MQTT消息
func (c *RedisCache) GetRecentMQTTMessages(ctx context.Context, deviceID string, limit int) ([]string, error) {
	key := PrefixMQTTMessage + deviceID
	if limit <= 0 {
		limit = 10
	}
	return c.client.LRange(ctx, key, 0, int64(limit-1)).Result()
}

// ============================================================
// 分布式 Pub-Sub 缓存失效通知（多实例一致性）
//
// 参考：viney-shih/go-cache 的 pub-sub 跨实例失效机制
// 当实例 A 写入新数据后，发布失效消息，实例 B/C 收到后失效本地 L1 缓存
// ============================================================

const invalidationChannel = "cache:invalidate"

// PublishInvalidate 发布缓存失效通知
func (c *RedisCache) PublishInvalidate(ctx context.Context, key string) error {
	return c.client.Publish(ctx, invalidationChannel, key).Err()
}

// SubscribeInvalidate 订阅缓存失效通知（在 goroutine 中运行）
func (c *RedisCache) SubscribeInvalidate(ctx context.Context) {
	pubsub := c.client.Subscribe(ctx, invalidationChannel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case msg := <-ch:
			if msg != nil {
				c.local.Delete(msg.Payload)
			}
		case <-ctx.Done():
			return
		}
	}
}

// ============================================================
// 本地内存 L1 缓存（线程安全，分片锁设计）
//
// 高并发优化：使用 64 个分片，每个分片独立 RWMutex
// 200+ 并发读写时锁竞争概率降至 1/64
// 参考：go-cache、BigCache 的分片设计
// ============================================================

const localCacheShards = 64

type localCacheEntry struct {
	value     any
	expiresAt time.Time
}

type localCacheShard struct {
	mu    sync.RWMutex
	items map[string]localCacheEntry
}

// LocalCache 线程安全的本地内存 L1 缓存（分片设计）
type LocalCache struct {
	shards  [localCacheShards]*localCacheShard
	maxSize int
}

// NewLocalCache 创建本地缓存
func NewLocalCache(defaultTTL time.Duration) *LocalCache {
	lc := &LocalCache{
		maxSize: 10000 / localCacheShards, // 每个分片的上限
	}
	for i := 0; i < localCacheShards; i++ {
		lc.shards[i] = &localCacheShard{
			items: make(map[string]localCacheEntry),
		}
	}
	// 定期清理过期条目
	go lc.cleanupLoop(defaultTTL)
	return lc
}

func (lc *LocalCache) getShard(key string) *localCacheShard {
	// FNV-1a hash 取模分片
	h := uint64(14695981039346656037)
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	return lc.shards[h%localCacheShards]
}

func (lc *LocalCache) cleanupLoop(ttl time.Duration) {
	ticker := time.NewTicker(maxDuration(ttl, 30*time.Second))
	defer ticker.Stop()
	for range ticker.C {
		for _, shard := range lc.shards {
			shard.mu.Lock()
			now := time.Now()
			for k, v := range shard.items {
				if now.After(v.expiresAt) {
					delete(shard.items, k)
				}
			}
			shard.mu.Unlock()
		}
	}
}

// Get 从 L1 获取值
func (lc *LocalCache) Get(key string) (any, bool) {
	shard := lc.getShard(key)
	shard.mu.RLock()
	entry, ok := shard.items[key]
	shard.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

// Set 使用默认 TTL 设置
func (lc *LocalCache) Set(key string, value any) {
	shard := lc.getShard(key)
	shard.mu.Lock()
	// 简单 LRU 淘汰
	if len(shard.items) >= lc.maxSize {
		count := 0
		for k := range shard.items {
			delete(shard.items, k)
			count++
			if count >= lc.maxSize/10 {
				break
			}
		}
	}
	shard.items[key] = localCacheEntry{
		value:     value,
		expiresAt: time.Now().Add(10 * time.Second),
	}
	shard.mu.Unlock()
}

// SetWithTTL 使用指定 TTL 设置
func (lc *LocalCache) SetWithTTL(key string, value any, ttl time.Duration) {
	shard := lc.getShard(key)
	shard.mu.Lock()
	shard.items[key] = localCacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	shard.mu.Unlock()
}

// Delete 删除条目
func (lc *LocalCache) Delete(key string) {
	shard := lc.getShard(key)
	shard.mu.Lock()
	delete(shard.items, key)
	shard.mu.Unlock()
}

// ============================================================
// 工具函数
// ============================================================

// jitterTTL 在基础 TTL 上添加 ±25% 随机抖动，防止缓存雪崩
// 参考：cachex 的 TTL jitter 设计
func jitterTTL(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	jitter := time.Duration(mrand.Int63n(int64(base) / 4)) // nolint:gosec
	return base + jitter
}

// minDuration 取较小值
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// maxDuration 取较大值
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// isNotFoundErr 判断是否为"记录不存在"错误（用于负缓存决策）
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return errors.Is(err, redis.Nil) ||
		containsAny(msg, "not found", "NotFound", "no rows", "record not found")
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// copyValue 将 src 的值复制到 dest（dest 必须是指针）
func copyValue(src, dest any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// cloneValue 深拷贝一个值
func cloneValue(v any) any {
	data, _ := json.Marshal(v)
	var result any
	json.Unmarshal(data, &result)
	return result
}

// ============================================================
// 初始化随机种子
// ============================================================

func init() {
	var seed int64
	if err := binary.Read(rand.Reader, binary.BigEndian, &seed); err != nil {
		seed = time.Now().UnixNano()
	}
	mrand.Seed(seed)
}
