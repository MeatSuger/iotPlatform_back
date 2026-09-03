package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
	"iot-platform.local/pkg/cache"
)

func TestNewDeviceOfflineSyncer_Defaults(t *testing.T) {
	svc := NewDeviceOfflineSyncer(nil, nil, 0, 0)
	assert.Equal(t, 30*time.Second, svc.interval)
	assert.Equal(t, 120*time.Second, svc.threshold)

	svc2 := NewDeviceOfflineSyncer(nil, nil, 10*time.Second, 30*time.Second)
	assert.Equal(t, 10*time.Second, svc2.interval)
	assert.Equal(t, 30*time.Second, svc2.threshold)

	assert.NotNil(t, svc.stopCh)
	assert.NotNil(t, svc.done)
}

func TestDeviceOfflineSyncer_Setters(t *testing.T) {
	svc := NewDeviceOfflineSyncer(nil, nil, time.Hour, time.Hour)
	svc.SetOnlineChecker(func(deviceID string) bool { return deviceID == "x" })
	svc.SetOfflineCallback(func(deviceID string) {})

	assert.NotNil(t, svc.isOnlineFn)
	assert.True(t, svc.isOnlineFn("x"))
	assert.False(t, svc.isOnlineFn("y"))
	assert.NotNil(t, svc.onOffline)
	svc.onOffline("x") // 不应 panic
}

func TestParseInt64(t *testing.T) {
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
			assert.Equal(t, tt.want, parseInt64(tt.val))
		})
	}
}

// buildSyncer 构造「真实 sqlite 仓库 + miniredis 缓存」的同步器；
// interval/threshold 调大避免 ticker 干扰 syncOnce 单轮测试。
func buildSyncer(t *testing.T) (*DeviceOfflineSyncer, *ent.Client, *miniredisSrv) {
	client := newTestEnt(t)
	mr, rcache := newTestRedisCache(t)
	svc := NewDeviceOfflineSyncer(repository.NewDeviceRepo(client), rcache, time.Hour, 2*time.Hour)
	return svc, client, mr
}

// seedOnline 在 PG 中预置 ONLINE 设备（先建 owner 满足 FK）
func seedOnline(t *testing.T, client *ent.Client, ids ...string) {
	t.Helper()
	owner := newOwner(t, client)
	repo := repository.NewDeviceRepo(client)
	for _, id := range ids {
		seedDevice(t, repo, id, owner, "ONLINE")
	}
}

// pgStatus 读回 PG 中的设备状态
func pgStatus(t *testing.T, client *ent.Client, id string) string {
	t.Helper()
	d, err := repository.NewDeviceRepo(client).GetByDeviceID(context.Background(), id)
	assert.NoError(t, err)
	if d == nil {
		return ""
	}
	return d.Status
}

func TestSyncOnce_RepoQueryError(t *testing.T) {
	client := newTestEnt(t)
	repo := repository.NewDeviceRepo(client)
	mr, rcache := newTestRedisCache(t)
	svc := NewDeviceOfflineSyncer(repo, rcache, time.Hour, time.Hour)

	// 关闭数据库 → ListByStatus 报错 → 仅告警返回
	assert.NoError(t, client.Close())
	svc.syncOnce()
	_ = mr
}

func TestSyncOnce_NoOnlineDevices(t *testing.T) {
	svc, _, _ := buildSyncer(t)
	svc.syncOnce()
}

func TestSyncOnce_MissingRedisKeyMarkedOffline(t *testing.T) {
	svc, client, mr := buildSyncer(t)
	seedOnline(t, client, "d1")

	offline := []string{}
	svc.SetOfflineCallback(func(deviceID string) { offline = append(offline, deviceID) })

	svc.syncOnce()

	// PG 与 Redis 均置为 OFFLINE
	assert.Equal(t, "OFFLINE", pgStatus(t, client, "d1"))
	status := mr.HGet(cache.PrefixDeviceStatus+"d1", "status")
	assert.Equal(t, "OFFLINE", status)
	assert.Equal(t, []string{"d1"}, offline)
}

func TestSyncOnce_RedisAlreadyOfflineSyncPG(t *testing.T) {
	svc, client, mr := buildSyncer(t)
	seedOnline(t, client, "d2")
	mr.HSet(cache.PrefixDeviceStatus+"d2", "status", "OFFLINE",
		"lastActiveTime", strconv.FormatInt(time.Now().UnixMilli(), 10))

	svc.syncOnce()
	assert.Equal(t, "OFFLINE", pgStatus(t, client, "d2"))
}

func TestSyncOnce_FreshLastActiveStaysOnline(t *testing.T) {
	svc, client, mr := buildSyncer(t)
	seedOnline(t, client, "d3")
	// 阈值 2h，最近活跃 1h 前 → 仍在线
	mr.HSet(cache.PrefixDeviceStatus+"d3", "status", "ONLINE",
		"lastActiveTime", strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli(), 10))

	svc.syncOnce()
	assert.Equal(t, "ONLINE", pgStatus(t, client, "d3"))
}

func TestSyncOnce_StaleLastActiveMarkedOffline(t *testing.T) {
	svc, client, mr := buildSyncer(t)
	seedOnline(t, client, "d4")
	// 超过阈值（3h > 2h）→ 离线
	mr.HSet(cache.PrefixDeviceStatus+"d4", "status", "ONLINE",
		"lastActiveTime", strconv.FormatInt(time.Now().Add(-3*time.Hour).UnixMilli(), 10))

	svc.syncOnce()
	assert.Equal(t, "OFFLINE", pgStatus(t, client, "d4"))
}

func TestSyncOnce_AllSkipWhenWSOnline(t *testing.T) {
	svc, client, mr := buildSyncer(t)
	seedOnline(t, client, "d5")
	svc.SetOnlineChecker(func(deviceID string) bool { return true })

	svc.syncOnce()
	// WS 在线 → 跳过 Redis 判定与 PG 更新
	assert.Equal(t, "ONLINE", pgStatus(t, client, "d5"))
	assert.Empty(t, mr.Keys())
}

func TestSyncOnce_MixedSkipAndOffline(t *testing.T) {
	svc, client, _ := buildSyncer(t)
	seedOnline(t, client, "skip1", "d6")
	// skip1 由 WS 判定在线，d6 无 Redis 状态 → 离线
	svc.SetOnlineChecker(func(deviceID string) bool { return deviceID == "skip1" })

	svc.syncOnce()
	assert.Equal(t, "ONLINE", pgStatus(t, client, "skip1"))
	assert.Equal(t, "OFFLINE", pgStatus(t, client, "d6"))
}

func TestSyncOnce_InvalidLastActiveParsedAsOffline(t *testing.T) {
	svc, client, mr := buildSyncer(t)
	seedOnline(t, client, "d7")
	mr.HSet(cache.PrefixDeviceStatus+"d7", "status", "ONLINE", "lastActiveTime", "not-a-number")

	svc.syncOnce()
	assert.Equal(t, "OFFLINE", pgStatus(t, client, "d7"))
}

func TestSyncOnce_PipelineExecError(t *testing.T) {
	svc, client, mr := buildSyncer(t)
	seedOnline(t, client, "d8")

	// 关闭 miniredis → Pipeline Exec 报错 → 告警返回，PG 不变
	mr.Close()
	svc.syncOnce()
	assert.Equal(t, "ONLINE", pgStatus(t, client, "d8"))
}

func TestDeviceOfflineSyncer_StartStop(t *testing.T) {
	client := newTestEnt(t)
	repo := repository.NewDeviceRepo(client)
	_, rcache := newTestRedisCache(t)
	svc := NewDeviceOfflineSyncer(repo, rcache, 10*time.Millisecond, time.Hour)
	seedOnline(t, client, "abc123")

	svc.Start()
	time.Sleep(50 * time.Millisecond)
	svc.Stop()

	// 一轮 tick 已执行：设备被判定离线并同步 PG
	assert.Equal(t, "OFFLINE", pgStatus(t, client, "abc123"))

	// 幂等 Stop：再次调用不应阻塞/panic
	done := make(chan struct{})
	go func() {
		svc.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("二次 Stop 阻塞")
	}
}
