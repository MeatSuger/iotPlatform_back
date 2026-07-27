package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache Redis缓存抽象
type RedisCache struct {
	client *redis.Client
}

// 缓存键前缀（与 Java 后端保持一致）
const (
	PrefixDevice       = "device:"
	PrefixSensorRecent = "sensorRecent:"
	PrefixMQTTMessage  = "mqtt:messages:"
)

// 缓存过期时间
const (
	TTLDevice       = 10 * time.Minute // 设备元数据
	TTLSensorRecent = 5 * time.Minute  // 传感器最新数据（需覆盖典型上报间隔 30-60s）
)

// NewRedisCache 创建Redis缓存实例
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client}
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

// Set 设置缓存
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化缓存值失败: %w", err)
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// Get 获取缓存
func (c *RedisCache) Get(ctx context.Context, key string, dest interface{}) error {
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

// DeleteByPattern 按模式删除缓存（使用Pipeline批量删除）
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

	// 使用 Pipeline 批量删除
	pipe := c.client.Pipeline()
	pipe.Del(ctx, keys...)
	_, err := pipe.Exec(ctx)
	return err
}

// Exists 检查键是否存在
func (c *RedisCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	return c.client.Exists(ctx, keys...).Result()
}

// ---- 设备缓存便捷方法 ----

// CacheDevice 缓存设备信息
func (c *RedisCache) CacheDevice(ctx context.Context, deviceID string, device interface{}) error {
	return c.Set(ctx, PrefixDevice+deviceID, device, TTLDevice)
}

// GetCachedDevice 获取缓存的设备信息
func (c *RedisCache) GetCachedDevice(ctx context.Context, deviceID string, dest interface{}) error {
	return c.Get(ctx, PrefixDevice+deviceID, dest)
}

// EvictDeviceCache 清除设备缓存
func (c *RedisCache) EvictDeviceCache(ctx context.Context, deviceID string) error {
	return c.Delete(ctx, PrefixDevice+deviceID)
}

// ---- 传感器近期数据缓存便捷方法 ----

// CacheSensorRecent 缓存传感器近期数据（用于快速查询最新一条）
func (c *RedisCache) CacheSensorRecent(ctx context.Context, deviceID string, data interface{}) error {
	return c.Set(ctx, PrefixSensorRecent+deviceID+":latest", data, TTLSensorRecent)
}

// GetCachedSensorRecent 获取缓存的传感器近期数据
func (c *RedisCache) GetCachedSensorRecent(ctx context.Context, deviceID string, dest interface{}) error {
	return c.Get(ctx, PrefixSensorRecent+deviceID+":latest", dest)
}

// EvictSensorRecentCache 清除传感器近期缓存
func (c *RedisCache) EvictSensorRecentCache(ctx context.Context, deviceID string) error {
	// 改为精确删除，避免 SCAN 全库
	return c.Delete(ctx, PrefixSensorRecent+deviceID+":latest")
}

// ---- 设备活跃时间防抖（Debounce） ----

const (
	PrefixDebounceActive = "debounce:active:"
	TTLDebounceActive    = 30 * time.Second // 30s 内同一设备只写一次 PG
)

// ShouldUpdateActive 设备活跃时间防抖：返回 true 表示应该更新 PostgreSQL
// 使用 Redis SETNX 实现：30s 内同一设备只有第一次返回 true
func (c *RedisCache) ShouldUpdateActive(ctx context.Context, deviceID string) (bool, error) {
	key := PrefixDebounceActive + deviceID
	ok, err := c.client.SetNX(ctx, key, "1", TTLDebounceActive).Result()
	return ok, err
}

// ---- MQTT消息缓存 ----

// LPushMQTTMessage 将MQTT消息推入列表
func (c *RedisCache) LPushMQTTMessage(ctx context.Context, deviceID string, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	key := PrefixMQTTMessage + deviceID
	pipe := c.client.Pipeline()
	pipe.LPush(ctx, key, data)
	pipe.LTrim(ctx, key, 0, 499) // 最多保留500条
	pipe.Expire(ctx, key, 24*time.Hour)
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
