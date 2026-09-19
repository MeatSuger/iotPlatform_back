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

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	entity "iot-platform.local/internal/model"
	"iot-platform.local/internal/repository"

	"iot-platform.local/internal/ent"
)

// buildSensorSvc 构造「真实 sqlite 仓库 + miniredis 命令队列」的传感器定义服务
func buildSensorSvc(t *testing.T) (*DeviceSensorService, *DeviceConfigService, *repository.DeviceRepo, *ent.Client) {
	t.Helper()
	deviceRepo, configSvc, rcache, client := newThingModelEnv(t)
	return NewDeviceSensorService(repository.NewDeviceThingRepo(client), configSvc, rcache), configSvc, deviceRepo, client
}

func validSensorReq(id string) entity.SensorCreateRequest {
	return entity.SensorCreateRequest{
		ID:       id,
		Name:     "温度",
		Type:     "temperature",
		DataType: "float",
		Unit:     "°C",
		Specs: &entity.SensorSpecs{
			Min:  fp(-40),
			Max:  fp(125),
			Step: fp(0.1),
			Thresholds: &entity.SpecsThresholds{
				Min:   fp(0),
				Max:   fp(100),
				Extra: map[string]any{"alarm": true},
			},
		},
		ReportInterval: ip(60),
	}
}

// fp / ip 指针构造助手（模拟 JSON 请求绑定后的指针字段）
func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }

func TestDeviceSensorService_CreateAndGet(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	created, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	assert.Equal(t, "temperature", created.ID)
	assert.Equal(t, "float", created.DataType)
	assert.Equal(t, 60, *created.ReportInterval)
	assert.True(t, created.Enabled)
	assert.Equal(t, 125.0, *created.Specs.Max)
	assert.Equal(t, true, created.Specs.Thresholds.Extra["alarm"]) // 扩展键保留

	got, err := svc.Get(context.Background(), "dev1", "temperature")
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, "温度", got.Name)
}

func TestDeviceSensorService_CreateDefaults(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	created, err := svc.Create(context.Background(), "dev1", entity.SensorCreateRequest{
		ID:   "switch_1",
		Name: "开关",
		Type: "switch",
	})
	assert.NoError(t, err)
	assert.Equal(t, "float", created.DataType) // 缺省回退 float
	assert.True(t, created.Enabled)            // 缺省启用
	assert.Nil(t, created.Specs)               // specs 省略（无定义）
	assert.Nil(t, created.ReportInterval)      // reportInterval 省略（继承全局）
}

func TestDeviceSensorService_CreateDuplicateID(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "已存在")
}

func TestDeviceSensorService_CreateValidation(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	cases := []entity.SensorCreateRequest{
		{ID: "9lead", Name: "x", Type: "t"},                         // 非法标识符：数字开头
		{ID: "bad-id", Name: "x", Type: "t"},                        // 非法标识符：连字符
		{ID: "ok_id", Name: "n", Type: "t", DataType: "unknown"},    // 非法数据类型
		{ID: "ok_id", Name: "n", Type: "t", ReportInterval: ip(-1)}, // 非法上报周期
	}
	for _, req := range cases {
		_, err := svc.Create(context.Background(), "dev1", req)
		assert.Error(t, err, "请求 %+v 应校验失败", req)
	}

	// 大写标识符合法（规则已放开大小写，与设备上报 name 对齐）
	_, err := svc.Create(context.Background(), "dev1", entity.SensorCreateRequest{ID: "Pi_AHT20", Name: "温湿度", Type: "temperature"})
	assert.NoError(t, err)
}

func TestDeviceSensorService_List(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", entity.SensorCreateRequest{ID: "humidity", Name: "湿度", Type: "humidity"})
	assert.NoError(t, err)

	list, err := svc.List(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, "temperature", list[0].ID) // 按创建顺序稳定输出
	assert.Equal(t, "humidity", list[1].ID)
}

func TestDeviceSensorService_GetNotExist(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	got, err := svc.Get(context.Background(), "dev1", "nope")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeviceSensorService_UpdateIncremental(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	// 仅更新名称与上报周期，其余字段保持原值
	newName := "环境温度"
	newInterval := json.RawMessage(`120`)
	got, err := svc.Update(context.Background(), "dev1", "temperature", entity.SensorUpdateRequest{
		Name:           &newName,
		ReportInterval: &newInterval,
	})
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, newName, got.Name)
	assert.Equal(t, 120, *got.ReportInterval)
	assert.Equal(t, "°C", got.Unit)        // 未传字段保持原值
	assert.Equal(t, 125.0, *got.Specs.Max) // specs 未受影响
}

func TestDeviceSensorService_UpdateClearReportInterval(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	// reportInterval 传 null → 恢复继承全局采样周期（列置 NULL）
	nullRaw := json.RawMessage(`null`)
	got, err := svc.Update(context.Background(), "dev1", "temperature", entity.SensorUpdateRequest{
		ReportInterval: &nullRaw,
	})
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Nil(t, got.ReportInterval)
}

func TestDeviceSensorService_UpdateClearSpecs(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	// specs 传 {} 显式清空
	raw := json.RawMessage(`{}`)
	got, err := svc.Update(context.Background(), "dev1", "temperature", entity.SensorUpdateRequest{Specs: &raw})
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Nil(t, got.Specs)
}

func TestDeviceSensorService_UpdateEmptyRequest(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	got, err := svc.Update(context.Background(), "dev1", "temperature", entity.SensorUpdateRequest{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "无更新字段")
	assert.Nil(t, got)
}

func TestDeviceSensorService_UpdateNotExist(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	name := "x"
	got, err := svc.Update(context.Background(), "dev1", "nope", entity.SensorUpdateRequest{Name: &name})
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeviceSensorService_Delete(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	_, err := svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)

	assert.NoError(t, svc.Delete(context.Background(), "dev1", "temperature"))

	err = svc.Delete(context.Background(), "dev1", "temperature")
	assert.ErrorIs(t, err, ErrSensorNotFound)
}

func TestDeviceSensorService_Apply(t *testing.T) {
	svc, configSvc, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	// 预置一份含其他分区的配置
	_, err := configSvc.Save(context.Background(), "dev1", map[string]any{
		"sensor": map[string]any{"reportInterval": 60},
	})
	assert.NoError(t, err)

	_, err = svc.Create(context.Background(), "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	_, err = svc.Create(context.Background(), "dev1", entity.SensorCreateRequest{ID: "humidity", Name: "湿度", Type: "humidity"})
	assert.NoError(t, err)

	resp, err := svc.Apply(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, "dev1", resp.DeviceID)
	assert.Equal(t, uint(2), resp.Version) // 在预置配置基础上递增
	assert.Equal(t, "pending", resp.Status)
	assert.Equal(t, 2, resp.Count)

	// 配置快照包含 sensors 分区且保留原有 sensor 分区
	got, err := configSvc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Contains(t, got.Payload, `"sensors"`)
	assert.Contains(t, got.Payload, `"temperature"`)
	assert.Contains(t, got.Payload, `"humidity"`)
	assert.Contains(t, got.Payload, `"reportInterval"`)
}

func TestDeviceSensorService_ApplyEmpty(t *testing.T) {
	svc, configSvc, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	// 无传感器定义时 Apply：下发空 sensors 数组
	resp, err := svc.Apply(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Equal(t, uint(1), resp.Version)
	assert.Equal(t, 0, resp.Count)

	got, err := configSvc.Get(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Contains(t, got.Payload, `"sensors":[]`)
}

// TestDeviceSensorService_ListCacheConsistency 验证定义列表缓存的写路径失效：
// Create/Update/Delete 后 List 必须立即可见新状态（缓存陈旧会导致断言失败）。
func TestDeviceSensorService_ListCacheConsistency(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)
	ctx := context.Background()

	// 预置两条定义并预热缓存（List 首次回源后缓存整列表）
	_, err := svc.Create(ctx, "dev1", validSensorReq("temperature"))
	assert.NoError(t, err)
	_, err = svc.Create(ctx, "dev1", entity.SensorCreateRequest{
		ID: "humidity", Name: "湿度", Type: "humidity", DataType: "float", Unit: "%RH",
	})
	assert.NoError(t, err)
	list, err := svc.List(ctx, "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 2)

	// Create 后缓存失效 → List 立即可见
	_, err = svc.Create(ctx, "dev1", entity.SensorCreateRequest{
		ID: "light", Name: "光照", Type: "light",
	})
	assert.NoError(t, err)
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	ids := make([]string, len(list))
	for i, s := range list {
		ids[i] = s.ID
	}
	// 列表按创建顺序（DB 主键升序）稳定输出
	assert.Equal(t, []string{"temperature", "humidity", "light"}, ids)

	// Update 后缓存失效 → List 反映新名称
	newName := "室温"
	_, err = svc.Update(ctx, "dev1", "temperature", entity.SensorUpdateRequest{
		Name: &newName,
	})
	assert.NoError(t, err)
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	for _, s := range list {
		if s.ID == "temperature" {
			assert.Equal(t, "室温", s.Name)
		}
	}

	// Delete 后缓存失效 → 列表收缩
	assert.NoError(t, svc.Delete(ctx, "dev1", "humidity"))
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 2)

	// 连续 List 稳定（命中缓存路径）
	list, err = svc.List(ctx, "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 2)
}

// TestDeviceSensorService_ListEmptyDevice 设备无定义时 List 返回空切片且可重复调用
func TestDeviceSensorService_ListEmptyDevice(t *testing.T) {
	svc, _, deviceRepo, client := buildSensorSvc(t)
	seedThingModelDevice(t, client, deviceRepo)

	list, err := svc.List(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.NotNil(t, list)
	assert.Len(t, list, 0)

	// 空列表已被缓存，再次调用仍返回空（不报错）
	list, err = svc.List(context.Background(), "dev1")
	assert.NoError(t, err)
	assert.Len(t, list, 0)
}
