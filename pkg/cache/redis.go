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
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// ============================================================
// RedisCache — 生产级 Redis 缓存层
//
// 设计参考：go-redis/cache（Cache-Aside + L1）、seaguest/cache
// （双层缓存 + singleflight）、viney-shih/go-cache（pub-sub 跨实例失效）、
// jetcache-go（L1 + L2）、cachex（负缓存 + TTL 抖动）
// ============================================================

// RedisCache Redis 缓存抽象（L2 层）
type RedisCache struct {
	client *redis.Client
	local  *LocalCache // L1 本地内存缓存
	sg     singleflight.Group

	// Lua 脚本：原子 LPush + LTrim + Expire
	mqttAtomicScript *redis.Script
}

// ============================================================
// 缓存键前缀
// ============================================================

const (
	PrefixDevice       = "cache:device:"        // 设备元数据
	PrefixDeviceStatus = "cache:device_status:" // 设备运行时状态（Hash）
	PrefixSensorRecent = "cache:sensor_recent:" // 传感器最新数据
	PrefixUser         = "cache:user:"          // 用户信息
	PrefixDeviceList   = "cache:device_list:"   // 用户设备列表
	PrefixMQTTMessage  = "cache:mqtt_msg:"      // MQTT 消息历史
	PrefixNegCache     = "cache:neg:"           // 负缓存（防穿透）

	PrefixSensorDefs   = "cache:def_sensor:"   // 传感器定义列表（物模型）
	PrefixActuatorDefs = "cache:def_actuator:" // 执行器定义列表（物模型）

	PrefixSensorHistory = "cache:sensor_history:" // 传感器历史曲线（时序查询，Cache-Aside）

	// CmdQueuePrefix 下行命令队列（由 DownlinkService 读写，删设备时需清理）
	CmdQueuePrefix = "cmd:queue:"
)

// ============================================================
// TTL 设计（遵循 L1<TTL < L2<TTL 原则）
// ============================================================

const (
	TTLDevice       = 10 * time.Minute // 设备元数据（L2）
	TTLDeviceLocal  = 30 * time.Second // 设备元数据（L1）
	TTLDeviceStatus = 10 * time.Minute // 设备运行时状态
	TTLSensorRecent = 8 * time.Minute  // 传感器最新数据（短于设备元数据 TTL，减少 miss）
	TTLUser         = 15 * time.Minute // 用户信息
	TTLUserLocal    = 60 * time.Second // 用户信息（L1）
	TTLDeviceList   = 2 * time.Minute  // 用户设备列表
	TTLNegCache     = 30 * time.Second // 负缓存（防穿透，短TTL）
	TTLMQTTMessage  = 24 * time.Hour   // MQTT 消息历史

	TTLDefs      = 30 * time.Minute // 物模型定义列表（L2，低频变更；写路径显式失效，TTL 仅兜底）
	TTLDefsLocal = 10 * time.Second // 物模型定义列表（L1）

	// 传感器历史曲线：单次查询要扫 InfluxDB 的 3 天窗口并排序，
	// 是全部接口里唯一的重查询（压测实测 P95 比其他接口高 8~10 倍）。
	// TTL 取"图表可接受的延迟"而非"数据实时性"——3 天趋势图落后 2 分钟无感知，
	// 却能把 InfluxDB 查询量降 1~2 个数量级（30s → 120s 再降 4 倍）。
	TTLSensorHistory      = 120 * time.Second // L2（3 天趋势图对 2 分钟延迟无感知）
	TTLSensorHistoryLocal = 15 * time.Second  // L1（须 < L2）

	// TTLDeviceStatusLocal 设备状态 L1 TTL。状态是上下线抖动最敏感的数据，
	// 取 5s 让跨实例的短暂不一致（状态写不广播失效）停在秒级。
	TTLDeviceStatusLocal = 5 * time.Second

	// sensorHistoryKeyBucket 历史查询缓存键的时间粒度。
	// 用 API 原始时间戳（秒）做键会让"滚动窗口"（每次请求 start/end 都变）
	// 命中率为 0——而滚动窗口正是历史曲线的典型用法。按分钟取整后同一分钟
	// 内的窗口共用一份缓存，代价是最多落后 1 个桶（叠加 L2 TTL 后仍无感知）。
	sensorHistoryKeyBucket = time.Minute

	// staleLocalTTL 进程内"上一次成功值"的保留时长。Redis 故障时用它顶上，
	// 避免每次缓存 miss 都回源 PostgreSQL / InfluxDB（打穿下游）。
	// 与 L1 同量级（60s）：兜底值无法接收跨实例失效通知，保留太久会让删除/更新
	// 在 Redis 故障期间对其他实例不可见（曾设 30 分钟，会把可见性放大到分钟级）。
	staleLocalTTL = 60 * time.Second

	// loaderTimeout singleflight 内 loader 的超时上限。
	// loader 用的是剥离取消信号的 ctx（首个请求取消不应连累其他等待者），
	// 因此必须自带超时，否则一次挂死的回源会永久占住该 key。
	// 不得短于最慢后端自己的超时：历史查询走 InfluxDB，其 QueryTimeout 默认 2 分钟，
	// 取短了会把"慢但能出"的重查询变成 500。
	loaderTimeout = 2 * time.Minute
)

// ============================================================
// 构造函数
// ============================================================

// MQTT Lua 原子脚本：LPush + LTrim + Expire 一次性执行
// 解决 Pipeline 非原子性（LPush 成功 LTrim 失败 → List 无限增长）
var mqttLuaScript = redis.NewScript(`
	redis.call('LPUSH', KEYS[1], ARGV[1])
	redis.call('LTRIM', KEYS[1], 0, tonumber(ARGV[2]))
	redis.call('EXPIRE', KEYS[1], tonumber(ARGV[3]))
	return 1
`)

// NewRedisCache 创建Redis缓存实例
func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{
		client:           client,
		local:            NewLocalCache(10 * time.Second), // L1 默认10s TTL
		mqttAtomicScript: mqttLuaScript,
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

// Pipeline 创建一个 Redis Pipeline
func (c *RedisCache) Pipeline() redis.Pipeliner {
	return c.client.Pipeline()
}

// ============================================================
// 核心模式1：Cache-Aside with Loader（参考 seaguest/cache）
//
// 流程：L1 → L2 → 负缓存检查 → Loader → Backfill
//
// 内置：
//  - singleflight 防击穿（并发 miss 只回源一次）
//  - 负缓存防穿透（"不存在"结果缓存到 L1 + L2，L1/L2 双重检查）
//  - TTL 抖动防雪崩（±25% 随机偏移，避免批量同时过期）
// ============================================================

// GetOrLoad 缓存即用模式：优先读缓存，miss 时回调 loader 加载并回填
// dest: 反序列化目标（必须是指针）
// loader: DB/上游数据加载函数
// localTTL: L1 本地缓存 TTL（0 表示不缓存到 L1）
func (c *RedisCache) GetOrLoad(ctx context.Context, key string, dest any, redisTTL time.Duration, localTTL time.Duration, loader func(context.Context) (any, error)) error {
	// 1. 尝试 L1 本地缓存（直接存储 JSON bytes，无类型丢失）
	if localTTL > 0 {
		if data, found, isNeg := c.local.getBytes(key); found {
			if isNeg {
				return redis.Nil // 负缓存命中
			}
			return json.Unmarshal(data, dest)
		}
	}

	// 2. 尝试 L2 Redis 缓存
	rawData, err := c.client.Get(ctx, key).Bytes()
	if err == nil {
		// 命中 L2：直接反序列化到 dest（无中间 any 类型丢失）
		if err := json.Unmarshal(rawData, dest); err != nil {
			return fmt.Errorf("反序列化缓存值失败: %w", err)
		}
		// 回填 L1（存储原始 JSON bytes）
		if localTTL > 0 {
			c.local.setBytes(key, rawData, localTTL)
		}
		c.local.setStale(key, rawData)
		return nil
	}

	if !errors.Is(err, redis.Nil) {
		// Redis 故障（连接/超时）：先用进程内"上一次成功值"兜底。
		// 直接降级到 loader 会让 Redis 挂掉的瞬间所有缓存 miss 都回源，
		// 把 PostgreSQL / InfluxDB 一起打挂——这正是缓存要防的事。
		zap.L().Warn("[RedisCache] L2 读取失败，尝试进程内兜底值",
			zap.String("key", key), zap.Error(err))
		if data, ok := c.local.getStale(key); ok {
			if uerr := json.Unmarshal(data, dest); uerr == nil {
				return nil
			}
		}
		zap.L().Warn("[RedisCache] 无兜底值，降级到 loader", zap.String("key", key))
	} else {
		// 2.5 检查 L2 负缓存（防穿透：redis.Nil 时优先查负缓存）
		negExists, negErr := c.client.Exists(ctx, negCacheKey(key)).Result()
		if negErr == nil && negExists > 0 {
			// L2 负缓存命中
			if localTTL > 0 {
				c.local.setNeg(key, minDuration(localTTL, TTLNegCache))
			}
			return redis.Nil
		}
	}

	// 3. singleflight 防击穿：并发 miss 只触发一次 loader
	v, err, _ := c.sg.Do(key, func() (any, error) {
		// loader 跑在剥离了取消信号的 ctx 上：首个请求被客户端取消（如前端切页）
		// 时，同一 key 的其他等待者不应一起失败；超时由 loaderTimeout 自行兜底。
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loaderTimeout)
		defer cancel()

		// Double-check L2（可能其他 goroutine 已加载）
		data, getErr := c.client.Get(loadCtx, key).Bytes()
		if getErr == nil {
			return data, nil // 返回原始 bytes，外层负责 unmarshal
		}

		// 调用 loader 从 DB 加载
		result, loadErr := loader(loadCtx)
		if loadErr != nil {
			// 负缓存：将"不存在"的结果缓存到 L1 + L2
			if errors.Is(loadErr, redis.Nil) || isNotFoundErr(loadErr) {
				c.client.Set(loadCtx, negCacheKey(key), "1", jitterTTL(TTLNegCache))
				// 清除旧值（Delete 会连带清除 stale 兜底副本）：否则下一次 Redis
				// 故障时 stale 分支会把已删除的数据重新端上来。
				c.local.Delete(key)
				if localTTL > 0 {
					c.local.setNeg(key, minDuration(localTTL, TTLNegCache))
				}
			}
			return nil, loadErr
		}

		// 回填 L2 Redis（存储 JSON bytes）
		marshalData, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return nil, fmt.Errorf("序列化加载结果失败: %w", marshalErr)
		}
		c.client.Set(loadCtx, key, marshalData, jitterTTL(redisTTL))

		return marshalData, nil // 返回原始 bytes
	})

	if err != nil {
		return err
	}

	// v 要么是 double-check 返回的 raw bytes，要么是 loader 返回的 json bytes
	resultBytes, err := toBytes(v)
	if err != nil {
		return fmt.Errorf("类型转换失败: %w", err)
	}

	// 反序列化到 dest
	if err := json.Unmarshal(resultBytes, dest); err != nil {
		return fmt.Errorf("反序列化缓存值失败: %w", err)
	}

	// 回填 L1（存储原始 JSON bytes，保持与 L2 一致）
	if localTTL > 0 {
		c.local.setBytes(key, resultBytes, localTTL)
	}
	c.local.setStale(key, resultBytes)

	return nil
}

// ============================================================
// 设备缓存
// ============================================================

// GetCachedDeviceWithLoader 获取设备元数据（带 DB 加载器）
func (c *RedisCache) GetCachedDeviceWithLoader(ctx context.Context, deviceID string, dest any, loader func(context.Context) (any, error)) error {
	key := PrefixDevice + deviceID
	return c.GetOrLoad(ctx, key, dest, TTLDevice, TTLDeviceLocal, loader)
}

// CacheDevice 缓存设备信息（Write-Through）
// 先写共享 L2，再同步失效本实例 L1，并发布失效通知让其他实例清掉其 L1。
// 读者下一次访问会从新 L2 回填 L1，不会命中旧值。
func (c *RedisCache) CacheDevice(ctx context.Context, deviceID string, device any) error {
	key := PrefixDevice + deviceID
	data, err := json.Marshal(device)
	if err != nil {
		return fmt.Errorf("序列化设备缓存失败: %w", err)
	}
	// 1. 写 L2（共享层）
	if err := c.client.Set(ctx, key, data, jitterTTL(TTLDevice)).Err(); err != nil {
		return err
	}
	// 2. 同步失效本实例 L1，避免写后本实例立即读到旧 L1
	c.local.Delete(key)
	// 3. 通知其他实例失效其 L1（L2 已是新值，下次读会回填）
	return c.PublishInvalidate(ctx, key)
}

// evictKey 失效缓存键：按需清除本实例 L1、删除 L2（含负缓存）、广播跨实例失效
func (c *RedisCache) evictKey(ctx context.Context, key string, evictLocal bool) error {
	if evictLocal {
		c.local.Delete(key)
	}
	// 一并清除负缓存：只删数据键时，紧接着的读取会命中上一次的"不存在"标记，
	// 在 TTLNegCache 内持续返回 404
	if err := c.Delete(ctx, key, negCacheKey(key)); err != nil {
		return err
	}
	return c.PublishInvalidate(ctx, key)
}

// EvictDeviceCache 失效设备缓存（Write-Invalidate）
func (c *RedisCache) EvictDeviceCache(ctx context.Context, deviceID string) error {
	return c.evictKey(ctx, PrefixDevice+deviceID, true)
}

// ============================================================
// 设备运行时状态缓存（Redis Hash，轻量级）
// ============================================================

// CacheDeviceStatus 更新设备运行时状态（Write-Through）到 L2 Redis Hash
// （status + lastActiveTime，避免完整 Device JSON 序列化）
// lastActiveTime 传 0 表示"未知"，此时不覆盖已存的值（见 GetDeviceStatus 的 >0 判断）。
func (c *RedisCache) CacheDeviceStatus(ctx context.Context, deviceID string, status string, lastActiveTime int64) error {
	key := PrefixDeviceStatus + deviceID
	// 写穿透：先失效 L1 读回填缓存（GetCachedDeviceStatus 会填充 TTLDeviceStatusLocal 的 L1）
	// 否则写后几秒内读取仍会命中旧状态，导致上线/离线切换被吞掉
	c.local.Delete(key)
	pipe := c.client.Pipeline()
	if lastActiveTime > 0 {
		pipe.HSet(ctx, key, "status", status, "lastActiveTime", lastActiveTime)
	} else {
		// 契约：0 表示"未知"，只更新状态，不覆盖已存的 lastActiveTime。
		pipe.HSet(ctx, key, "status", status)
	}
	pipe.Expire(ctx, key, jitterTTL(TTLDeviceStatus))
	// 跨实例失效：其他实例的 L1 也应立即失效，否则多实例下状态最长脏一个 L1 TTL
	pipe.Publish(ctx, invalidationChannel, key)
	_, err := pipe.Exec(ctx)
	return err
}

// GetCachedDeviceStatus 获取设备运行时状态（优先 L1 → L2 Hash）
func (c *RedisCache) GetCachedDeviceStatus(ctx context.Context, deviceID string) (status string, lastActiveTime int64, err error) {
	key := PrefixDeviceStatus + deviceID

	// L1
	if data, found, _ := c.local.getBytes(key); found {
		var entry DeviceStatusEntry
		if json.Unmarshal(data, &entry) == nil {
			return entry.Status, entry.LastActiveTime, nil
		}
	}

	// L2
	results, err := c.client.HMGet(ctx, key, "status", "lastActiveTime").Result()
	if err != nil {
		return "", 0, err
	}
	if len(results) >= 2 {
		status = hashString(results[0])
		lastActiveTime = HashInt64(results[1])
	}

	// 回填 L1（存储 JSON bytes）
	if status != "" {
		entryBytes, _ := json.Marshal(DeviceStatusEntry{Status: status, LastActiveTime: lastActiveTime})
		c.local.setBytes(key, entryBytes, TTLDeviceStatusLocal)
	}

	return
}

// GetCachedDeviceStatusBatch 批量读取设备运行时状态（1 次 pipeline 往返）。
//
// 列表接口专用：列表缓存存的是 PostgreSQL 快照（上报路径防抖 5s + 列表缓存 2min），
// 不叠加 Hash 覆盖的话，设备上下线要在列表页最长滞后 2 分钟才变色。
// 返回的 map 只包含 Hash 里确实存在的设备，调用方据此逐个覆盖。
func (c *RedisCache) GetCachedDeviceStatusBatch(ctx context.Context, deviceIDs []string) (map[string]DeviceStatusEntry, error) {
	out := make(map[string]DeviceStatusEntry, len(deviceIDs))
	if len(deviceIDs) == 0 {
		return out, nil
	}

	pipe := c.client.Pipeline()
	cmds := make([]*redis.SliceCmd, len(deviceIDs))
	for i, id := range deviceIDs {
		cmds[i] = pipe.HMGet(ctx, PrefixDeviceStatus+id, "status", "lastActiveTime")
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return out, err
	}

	for i, cmd := range cmds {
		vals, err := cmd.Result()
		if err != nil || len(vals) < 2 {
			continue
		}
		entry := DeviceStatusEntry{Status: hashString(vals[0]), LastActiveTime: HashInt64(vals[1])}
		if entry.Status != "" {
			out[deviceIDs[i]] = entry
		}
	}
	return out, nil
}

// DeviceStatusEntry 设备运行时状态快照（Redis Hash 形态）
type DeviceStatusEntry struct {
	Status         string `json:"status"`
	LastActiveTime int64  `json:"lastActiveTime"`
}

// hashString 读取 Hash 字段的字符串值（类型异常时返回空串，不 panic）
func hashString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return ""
	}
}

// HashInt64 读取 Hash 字段的整数值（HSet 写入的 int64 读回为 string）
func HashInt64(v any) int64 {
	switch val := v.(type) {
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	case int64:
		return val
	default:
		return 0
	}
}

// EvictDeviceStatusCache 清除设备状态缓存
func (c *RedisCache) EvictDeviceStatusCache(ctx context.Context, deviceID string) error {
	return c.evictKey(ctx, PrefixDeviceStatus+deviceID, true)
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
	return c.evictKey(ctx, PrefixSensorRecent+deviceID+":latest", false)
}

// GetCachedSensorHistoryWithLoader 读取设备历史曲线（Cache-Aside + singleflight）
//
// 为什么必须缓存：历史查询要扫 InfluxDB 的 3 天窗口并排序，是全部接口里
// 唯一的重查询。压测显示它是把 InfluxDB 打满（147% CPU）进而拖垮写路径的根因。
//
// 走 GetOrLoad 直接复用了缓存层已有的三项保护：
//   - singleflight 防击穿：缓存过期瞬间的并发 miss 只回源一次
//   - TTL 抖动防雪崩：±25% 随机偏移，避免批量同时过期
//   - L1(本地) + L2(Redis) 两级缓存
//
// key 组成：deviceID + limit + 时间窗 + variant
//   - 默认时间窗 → ":default"（高频路径，稳定命中）
//   - 显式时间窗 → 按 unix 秒区分（避免不同窗口互相污染）
//   - variant → 降采样模式等变体，必须区分：否则原始点请求会命中聚合缓存（反之亦然）
func (c *RedisCache) GetCachedSensorHistoryWithLoader(
	ctx context.Context,
	deviceID string,
	limit int,
	start, end time.Time,
	isDefaultRange bool,
	variant string,
	dest any,
	loader func(context.Context) (any, error),
) error {
	key := fmt.Sprintf("%s%s:%d", PrefixSensorHistory, deviceID, limit)
	if isDefaultRange {
		key += ":default"
	} else if end.Sub(start) >= sensorHistoryKeyBucket {
		// 宽窗口按桶取整：秒级时间戳会让滚动窗口（每次请求 start/end 都变）
		// 与"只传 start"的请求命中率恒为 0，历史查询就会全部压到 InfluxDB。
		key += fmt.Sprintf(":%d:%d", start.Truncate(sensorHistoryKeyBucket).Unix(), end.Truncate(sensorHistoryKeyBucket).Unix())
	} else {
		// 窄窗口（<1 个桶）两端会落到同一分钟，取整会让不同窗口互相串数据，必须精确
		key += fmt.Sprintf(":%d:%d", start.Unix(), end.Unix())
	}
	if variant != "" {
		key += ":" + variant
	}
	return c.GetOrLoad(ctx, key, dest, TTLSensorHistory, TTLSensorHistoryLocal, loader)
}

// ============================================================
// 物模型定义列表缓存（Cache-Aside + Write-Invalidate）
// 物模型为低频写（CRUD）、高频读（详情页/物模型页），整设备列表作为
// 不可变对象缓存；Create/Update/Delete 写路径显式 Evict，TTL 仅兜底自愈。
// ============================================================

// GetCachedSensorDefsWithLoader 读取设备传感器定义列表（带 DB loader，L1/L2 两层）
func (c *RedisCache) GetCachedSensorDefsWithLoader(ctx context.Context, deviceID string, dest any, loader func(context.Context) (any, error)) error {
	key := PrefixSensorDefs + deviceID
	return c.GetOrLoad(ctx, key, dest, TTLDefs, TTLDefsLocal, loader)
}

// EvictSensorDefsCache 失效设备传感器定义列表缓存（Write-Invalidate，跨实例通知）
func (c *RedisCache) EvictSensorDefsCache(ctx context.Context, deviceID string) error {
	return c.evictKey(ctx, PrefixSensorDefs+deviceID, true)
}

// GetCachedActuatorDefsWithLoader 读取设备执行器定义列表（带 DB loader，L1/L2 两层）
func (c *RedisCache) GetCachedActuatorDefsWithLoader(ctx context.Context, deviceID string, dest any, loader func(context.Context) (any, error)) error {
	key := PrefixActuatorDefs + deviceID
	return c.GetOrLoad(ctx, key, dest, TTLDefs, TTLDefsLocal, loader)
}

// EvictActuatorDefsCache 失效设备执行器定义列表缓存（Write-Invalidate，跨实例通知）
func (c *RedisCache) EvictActuatorDefsCache(ctx context.Context, deviceID string) error {
	return c.evictKey(ctx, PrefixActuatorDefs+deviceID, true)
}

// EvictDeviceAllCaches 设备删除时清理该设备的所有关联缓存
// 包括: 物模型定义（传感器/执行器）、下行命令队列、MQTT 消息历史（精确键）
//
// 注意：SensorHistory 的键带 limit/时间窗后缀（一设备多键），DEL 删不了，
// 靠 TTL（120s）自愈；设备主体已删，归属校验会先拦住读取。
func (c *RedisCache) EvictDeviceAllCaches(ctx context.Context, deviceID string) error {
	var errs []error

	// 1. 物模型定义（有 L1 副本，需 evictKey 走跨实例广播）。
	// 不清理的话，已删设备的 sensors/actuators 与其详情页物模型会继续可读最长 30 分钟。
	for _, key := range []string{PrefixSensorDefs + deviceID, PrefixActuatorDefs + deviceID} {
		if err := c.evictKey(ctx, key, true); err != nil {
			errs = append(errs, fmt.Errorf("物模型定义(%s): %w", key, err))
		}
	}

	// 2. 仅 L2 的键：下行命令队列 + MQTT 消息历史（一次 DEL 完成）
	if err := c.Delete(ctx, CmdQueuePrefix+deviceID, PrefixMQTTMessage+deviceID); err != nil {
		errs = append(errs, fmt.Errorf("命令队列/MQTT消息: %w", err))
	}

	return errors.Join(errs...)
}

// ============================================================
// 设备上报快速路径：Pipeline 批量写入（一次往返完成 5 个命令）
// ============================================================

// BufferDeviceReportsKey 上报缓冲队列 key（与 device_data_buffer.go 共享）
const BufferDeviceReportsKey = "buffer:device_reports"

// BufferQueueMaxLen 缓冲队列长度上限（防 InfluxDB 故障时无限增长耗尽内存）
const BufferQueueMaxLen = 20000

// bufferPushSrc 缓冲队列原子入队：LPUSH + LTRIM。
// 拆成两条命令时，LTrim 失败会让 List 无限增长（与 MQTT 历史同一个坑）。
const bufferPushSrc = `
	redis.call('LPUSH', KEYS[1], ARGV[1])
	redis.call('LTRIM', KEYS[1], 0, tonumber(ARGV[2]))
	return 1
`

// FastReportWrite 一次 Pipeline 完成：更新设备状态 Hash + 缓存传感器数据 + 推入缓冲队列
// 将多次 Redis 往返合并为 1 次
// FastReportWrite 上报热路径的一次性 Pipeline 写入。
//
// sensorJSON 由调用方预先序列化（entity 形态，与 CacheSensorRecent 一致），
// 避免在本函数内重复 json.Marshal —— 上报是最热路径，每次省一次反射序列化。
func (c *RedisCache) FastReportWrite(ctx context.Context, deviceID string, status string, lastActiveTime int64, sensorJSON []byte, bufferData []byte) error {
	// 写穿透：先失效 L1 读回填缓存（与 CacheDeviceStatus 一致），
	// 否则上报置 ONLINE 后几秒内 GetCachedDeviceStatus 仍读到旧状态
	statusKey := PrefixDeviceStatus + deviceID
	c.local.Delete(statusKey)

	pipe := c.client.Pipeline()

	// 1. 更新设备状态 Hash
	pipe.HSet(ctx, statusKey, "status", status, "lastActiveTime", lastActiveTime)
	pipe.Expire(ctx, statusKey, jitterTTL(TTLDeviceStatus))
	// 跨实例失效（同一个 pipeline，不额外增加往返）
	pipe.Publish(ctx, invalidationChannel, statusKey)

	// 2. 缓存传感器最新数据。空数据（[]/null）不写：
	// 否则一条不带传感器的状态上报会把上一次的"最新快照"抹成空，设备页数据凭空消失。
	sensorKey := PrefixSensorRecent + deviceID + ":latest"
	if hasSensorData(sensorJSON) {
		pipe.Set(ctx, sensorKey, sensorJSON, jitterTTL(TTLSensorRecent))
	}

	// 3. 推入缓冲队列（Lua 原子 LPUSH + LTRIM，防部分失败导致 List 无限增长）
	pipe.Eval(ctx, bufferPushSrc, []string{BufferDeviceReportsKey}, bufferData, BufferQueueMaxLen-1)

	_, err := pipe.Exec(ctx)
	return err
}

// hasSensorData 判断序列化后的传感器快照是否为"有数据"（非 null、非空数组）
func hasSensorData(sensorJSON []byte) bool {
	s := strings.TrimSpace(string(sensorJSON))
	return s != "" && s != "null" && s != "[]"
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

// EvictUserCache 失效用户缓存
func (c *RedisCache) EvictUserCache(ctx context.Context, userID string) error {
	return c.evictKey(ctx, PrefixUser+userID, true)
}

// ============================================================
// 用户设备列表缓存
// ============================================================

// GetCachedDeviceList 获取用户设备列表（L1 → L2 → loader）
func (c *RedisCache) GetCachedDeviceList(ctx context.Context, ownerID uint, dest any, loader func(context.Context) (any, error)) error {
	key := fmt.Sprintf("%s%d", PrefixDeviceList, ownerID)
	// L1 短 TTL（20s），保持 L1<TTL < L2<TTL（2min）原则
	return c.GetOrLoad(ctx, key, dest, TTLDeviceList, 20*time.Second, loader)
}

// EvictDeviceListCache 失效用户设备列表缓存
func (c *RedisCache) EvictDeviceListCache(ctx context.Context, ownerID uint) error {
	return c.evictKey(ctx, fmt.Sprintf("%s%d", PrefixDeviceList, ownerID), true)
}

// ============================================================
// MQTT 消息缓存
// ============================================================

// LPushMQTTMessage 将MQTT消息推入列表
// 使用 Lua 脚本保证 LPush + LTrim + Expire 原子性
func (c *RedisCache) LPushMQTTMessage(ctx context.Context, deviceID string, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	key := PrefixMQTTMessage + deviceID
	return c.mqttAtomicScript.Run(ctx, c.client, []string{key},
		string(data), "499", int64(TTLMQTTMessage.Seconds()),
	).Err()
}

// ============================================================
// 分布式 Pub-Sub 缓存失效通知（多实例一致性）
//
// 参考：viney-shih/go-cache 的 pub-sub 跨实例失效机制
// 当实例 A 写入新数据后，发布失效消息，实例 B/C 收到后失效本地 L1 缓存
// ============================================================

// invalidationChannel 跨实例缓存失效通知频道
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
//
// 存储改进：
//  - 存储原始 JSON bytes，避免 any → struct 类型丢失
//  - isNeg 标记位支持负缓存（不必在 bytes 中混入 sentinel）
//  - 随机淘汰（利用 Go map 无序迭代顺序）
// ============================================================

const localCacheShards = 64

// staleKeyPrefix 进程内兜底副本的键前缀（与业务键共用 LocalCache，不占 Redis 空间）
const staleKeyPrefix = "stale|"

type localCacheEntry struct {
	data      []byte    // JSON 原始字节
	expiresAt time.Time // 过期时间
	isNeg     bool      // 是否为负缓存标记
}

type localCacheShard struct {
	mu    sync.RWMutex
	items map[string]localCacheEntry
}

// LocalCache 线程安全的本地内存 L1 缓存（分片设计）
type LocalCache struct {
	shards  [localCacheShards]*localCacheShard
	maxSize int
	stopCh  chan struct{} // 停止清理 goroutine
}

// NewLocalCache 创建本地缓存
func NewLocalCache(defaultTTL time.Duration) *LocalCache {
	lc := &LocalCache{
		maxSize: 10000 / localCacheShards, // 每个分片的上限
		stopCh:  make(chan struct{}),
	}
	for i := range localCacheShards {
		lc.shards[i] = &localCacheShard{
			items: make(map[string]localCacheEntry),
		}
	}
	// 定期清理过期条目
	go lc.cleanupLoop(defaultTTL)
	return lc
}

// Close 停止清理 goroutine，释放资源
func (lc *LocalCache) Close() {
	close(lc.stopCh)
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
	interval := max(ttl, 30*time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-lc.stopCh:
			return
		case <-ticker.C:
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
}

// ---- 内部方法（操作 []byte，避免类型丢失） ----

// getBytes 从 L1 获取原始字节
// 返回值: data（JSON bytes）, found（是否命中）, isNeg（是否负缓存）
func (lc *LocalCache) getBytes(key string) ([]byte, bool, bool) {
	shard := lc.getShard(key)
	shard.mu.RLock()
	entry, ok := shard.items[key]
	shard.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false, false
	}
	return entry.data, true, entry.isNeg
}

// setBytes 使用指定 TTL 设置原始字节
func (lc *LocalCache) setBytes(key string, data []byte, ttl time.Duration) {
	lc.put(key, localCacheEntry{data: data, expiresAt: time.Now().Add(ttl)})
}

// setNeg 设置负缓存标记
func (lc *LocalCache) setNeg(key string, ttl time.Duration) {
	lc.put(key, localCacheEntry{expiresAt: time.Now().Add(ttl), isNeg: true})
}

// setStale 记录"上一次成功值"，仅在 Redis 故障时用（见 GetOrLoad）
func (lc *LocalCache) setStale(key string, data []byte) {
	lc.put(staleKeyPrefix+key, localCacheEntry{data: data, expiresAt: time.Now().Add(staleLocalTTL)})
}

// getStale 读取"上一次成功值"（不受 L1 短 TTL 限制）
func (lc *LocalCache) getStale(key string) ([]byte, bool) {
	data, found, isNeg := lc.getBytes(staleKeyPrefix + key)
	return data, found && !isNeg
}

// put 写入分片并执行上限淘汰（所有写入路径共用，否则 maxSize 形同虚设）
func (lc *LocalCache) put(key string, entry localCacheEntry) {
	shard := lc.getShard(key)
	shard.mu.Lock()
	// 顺手清理过期项，避免只能等清理协程
	if len(shard.items) >= lc.maxSize {
		lc.evictLocked(shard, time.Now())
	}
	shard.items[key] = entry
	shard.mu.Unlock()
}

// evictLocked 先删过期项，再随机淘汰一成（Go map 迭代顺序）；调用方持锁
func (lc *LocalCache) evictLocked(shard *localCacheShard, now time.Time) {
	for k, v := range shard.items {
		if now.After(v.expiresAt) {
			delete(shard.items, k)
		}
	}
	if len(shard.items) < lc.maxSize {
		return
	}
	evict := max(lc.maxSize/10, 1)
	count := 0
	for k := range shard.items {
		delete(shard.items, k)
		count++
		if count >= evict {
			break
		}
	}
}

// ---- 公共方法（保持 any 接口兼容外部直接调用者） ----

// Get 从 L1 获取值（返回 any，兼容旧 API）
// 注意：内部存储为 JSON bytes，反序列化回 any 时 struct 变为 map[string]interface{}
// RedisCache 内部应使用 getBytes 以避免类型丢失，外部仅用于简单标记值
func (lc *LocalCache) Get(key string) (any, bool) {
	data, found, isNeg := lc.getBytes(key)
	if !found {
		return nil, false
	}
	if isNeg {
		return nil, false // 负缓存对 any 接口返回"未命中"
	}
	if len(data) == 0 {
		return nil, true
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false
	}
	return result, true
}

// Set 使用默认 TTL 设置
func (lc *LocalCache) Set(key string, value any) {
	data, _ := json.Marshal(value)
	lc.put(key, localCacheEntry{data: data, expiresAt: time.Now().Add(10 * time.Second)})
}

// Delete 删除条目（连同 Redis 故障兜底副本）
func (lc *LocalCache) Delete(key string) {
	// 两个 key 可能落在不同分片，逐个持锁
	for _, k := range [2]string{key, staleKeyPrefix + key} {
		shard := lc.getShard(k)
		shard.mu.Lock()
		delete(shard.items, k)
		shard.mu.Unlock()
	}
}

// ============================================================
// 工具函数
// ============================================================

// jitterTTL 在基础 TTL 上添加 ±25% 随机抖动，防止缓存雪崩
// 结果范围: [base*0.75, base*1.25)，均匀随机
// 参考：cachex 的 TTL jitter 设计
func jitterTTL(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	// base 过小（< 4ns）直接返回，避免 rand.Int63n(0) 非法参数
	if base < 4 {
		return base
	}
	// ±25%: jitter ∈ [-base/4, base/4)，结果 ∈ [base*3/4, base*5/4)
	jitter := rand.Int63n(int64(base/2)) - int64(base/4) // nolint:gosec
	return base + time.Duration(jitter)
}

// minDuration 取较小值
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// toBytes 将 singleflight 返回的 any 转换为 []byte
// 支持 direct type 和 json bytes 两种来源
func toBytes(v any) ([]byte, error) {
	switch val := v.(type) {
	case []byte:
		return val, nil
	case string:
		return []byte(val), nil
	default:
		return json.Marshal(val)
	}
}

// negCacheKey 生成负缓存键：剥离 key 的 "cache:" 前缀，避免双重前缀
// 例: cache:device:abc -> cache:neg:device:abc
func negCacheKey(key string) string {
	const cachePrefix = "cache:"
	if after, ok := strings.CutPrefix(key, cachePrefix); ok {
		return PrefixNegCache + after
	}
	return PrefixNegCache + key
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
