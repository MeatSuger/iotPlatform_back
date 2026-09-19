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
		return nil
	}

	// Redis 连接错误：降级到 loader（容错设计，Redis 挂不阻塞业务）
	if err != redis.Nil {
		zap.L().Warn("[RedisCache] L2 读取失败，降级到 loader",
			zap.String("key", key), zap.Error(err))
	}

	// 2.5 检查 L2 负缓存（防穿透：redis.Nil 时优先查负缓存）
	if err == redis.Nil {
		negKey := negCacheKey(key)
		negExists, negErr := c.client.Exists(ctx, negKey).Result()
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
		// Double-check L2（可能其他 goroutine 已加载）
		data, getErr := c.client.Get(ctx, key).Bytes()
		if getErr == nil {
			return data, nil // 返回原始 bytes，外层负责 unmarshal
		}

		// 调用 loader 从 DB 加载
		result, loadErr := loader(ctx)
		if loadErr != nil {
			// 负缓存：将"不存在"的结果缓存到 L1 + L2
			if errors.Is(loadErr, redis.Nil) || isNotFoundErr(loadErr) {
				c.client.Set(ctx, negCacheKey(key), "1", jitterTTL(TTLNegCache))
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
		c.client.Set(ctx, key, marshalData, jitterTTL(redisTTL))

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

// evictKey 失效缓存键：按需清除本实例 L1、删除 L2、广播跨实例失效
func (c *RedisCache) evictKey(ctx context.Context, key string, evictLocal bool) error {
	if evictLocal {
		c.local.Delete(key)
	}
	if err := c.Delete(ctx, key); err != nil {
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
func (c *RedisCache) CacheDeviceStatus(ctx context.Context, deviceID string, status string, lastActiveTime int64) error {
	key := PrefixDeviceStatus + deviceID
	// 写穿透：先失效 L1 读回填缓存（GetCachedDeviceStatus 会填充 5s TTL 的 L1）
	// 否则写后 5s 内读取仍会命中旧状态，导致上线/离线切换被吞掉
	c.local.Delete(key)
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
	if data, found, _ := c.local.getBytes(key); found {
		var entry deviceStatusEntry
		if json.Unmarshal(data, &entry) == nil {
			return entry.Status, entry.LastActiveTime, nil
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

	// 回填 L1（存储 JSON bytes）
	if status != "" {
		entryBytes, _ := json.Marshal(deviceStatusEntry{Status: status, LastActiveTime: lastActiveTime})
		c.local.setBytes(key, entryBytes, 5*time.Second)
	}

	return
}

type deviceStatusEntry struct {
	Status         string `json:"status"`
	LastActiveTime int64  `json:"lastActiveTime"`
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
// key 组成：deviceID + limit + 时间窗
//   - 默认时间窗 → ":default"（高频路径，稳定命中）
//   - 显式时间窗 → 按 unix 秒区分（避免不同窗口互相污染）
func (c *RedisCache) GetCachedSensorHistoryWithLoader(
	ctx context.Context,
	deviceID string,
	limit int,
	start, end time.Time,
	isDefaultRange bool,
	dest any,
	loader func(context.Context) (any, error),
) error {
	key := fmt.Sprintf("%s%s:%d", PrefixSensorHistory, deviceID, limit)
	if isDefaultRange {
		key += ":default"
	} else {
		key += fmt.Sprintf(":%d:%d", start.Unix(), end.Unix())
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
// 包括: 下行命令队列、MQTT 消息历史（精确键）
func (c *RedisCache) EvictDeviceAllCaches(ctx context.Context, deviceID string) error {
	var errs []error

	// 1. 下行命令队列
	if err := c.Delete(ctx, "cmd:queue:"+deviceID); err != nil {
		errs = append(errs, fmt.Errorf("命令队列: %w", err))
	}

	// 2. MQTT 消息历史
	if err := c.Delete(ctx, PrefixMQTTMessage+deviceID); err != nil {
		errs = append(errs, fmt.Errorf("MQTT消息: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("清理设备缓存: %v", errs)
	}
	return nil
}

// ============================================================
// 设备上报快速路径：Pipeline 批量写入（一次往返完成 5 个命令）
// ============================================================

// BufferDeviceReportsKey 上报缓冲队列 key（与 device_data_buffer.go 共享）
const BufferDeviceReportsKey = "buffer:device_reports"

// BufferQueueMaxLen 缓冲队列长度上限（防 InfluxDB 故障时无限增长耗尽内存）
const BufferQueueMaxLen = 20000

// FastReportWrite 一次 Pipeline 完成：更新设备状态 Hash + 缓存传感器数据 + 推入缓冲队列
// 将多次 Redis 往返合并为 1 次
// FastReportWrite 上报热路径的一次性 Pipeline 写入。
//
// sensorJSON 由调用方预先序列化（entity 形态，与 CacheSensorRecent 一致），
// 避免在本函数内重复 json.Marshal —— 上报是最热路径，每次省一次反射序列化。
func (c *RedisCache) FastReportWrite(ctx context.Context, deviceID string, status string, lastActiveTime int64, sensorJSON []byte, bufferData []byte) error {
	// 写穿透：先失效 L1 读回填缓存（与 CacheDeviceStatus 一致），
	// 否则上报置 ONLINE 后 5s 内 GetCachedDeviceStatus 仍读到旧状态
	c.local.Delete(PrefixDeviceStatus + deviceID)

	pipe := c.client.Pipeline()

	// 1. 更新设备状态 Hash
	statusKey := PrefixDeviceStatus + deviceID
	pipe.HSet(ctx, statusKey, "status", status, "lastActiveTime", lastActiveTime)
	pipe.Expire(ctx, statusKey, jitterTTL(TTLDeviceStatus))

	// 2. 缓存传感器最新数据
	sensorKey := PrefixSensorRecent + deviceID + ":latest"
	if len(sensorJSON) == 0 {
		sensorJSON = []byte("[]")
	}
	pipe.Set(ctx, sensorKey, sensorJSON, jitterTTL(TTLSensorRecent))

	// 3. 推入缓冲队列（带 LTrim 上限，防内存耗尽；保留头部最新数据）
	pipe.LPush(ctx, BufferDeviceReportsKey, bufferData)
	pipe.LTrim(ctx, BufferDeviceReportsKey, 0, BufferQueueMaxLen-1)

	_, err := pipe.Exec(ctx)
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
	for i := 0; i < localCacheShards; i++ {
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
	interval := ttl
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
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
	shard := lc.getShard(key)
	shard.mu.Lock()
	shard.items[key] = localCacheEntry{
		data:      data,
		expiresAt: time.Now().Add(ttl),
		isNeg:     false,
	}
	shard.mu.Unlock()
}

// setNeg 设置负缓存标记
func (lc *LocalCache) setNeg(key string, ttl time.Duration) {
	shard := lc.getShard(key)
	shard.mu.Lock()
	shard.items[key] = localCacheEntry{
		data:      nil,
		expiresAt: time.Now().Add(ttl),
		isNeg:     true,
	}
	shard.mu.Unlock()
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
	shard := lc.getShard(key)
	shard.mu.Lock()
	// 简单随机淘汰（Go map 迭代顺序）
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
	data, _ := json.Marshal(value)
	shard.items[key] = localCacheEntry{
		data:      data,
		expiresAt: time.Now().Add(10 * time.Second),
		isNeg:     false,
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
	if strings.HasPrefix(key, cachePrefix) {
		return PrefixNegCache + strings.TrimPrefix(key, cachePrefix)
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
