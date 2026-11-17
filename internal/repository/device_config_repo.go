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

package repository

import (
	"context"
	"fmt"

	"iot-platform.local/internal/ent"
	entconfig "iot-platform.local/internal/ent/deviceconfig"
)

// DeviceConfigRepo 设备配置快照数据访问
type DeviceConfigRepo struct {
	client *ent.Client
}

// NewDeviceConfigRepo 创建设备配置仓库
func NewDeviceConfigRepo(client *ent.Client) *DeviceConfigRepo {
	return &DeviceConfigRepo{client: client}
}

// GetByDeviceID 查询设备配置快照；不存在返回 ent.NotFoundError
func (r *DeviceConfigRepo) GetByDeviceID(ctx context.Context, deviceID string) (*ent.DeviceConfig, error) {
	return r.client.DeviceConfig.Query().
		Where(entconfig.DeviceIDEQ(deviceID)).
		First(ctx)
}

// Upsert 原子写入设备配置快照：version 递增与 payload/status 更新在同一条
// UPDATE 中完成（AddVersion 生成 version = version + 1），避免并发 Save 时
// 「读旧版本再写回」导致的版本重复/覆盖；记录不存在则插入 version=1，
// 并发插入撞唯一约束时回退重试更新（此时能命中先插入方的行，version 继续递增）。
func (r *DeviceConfigRepo) Upsert(ctx context.Context, deviceID string, payload string) error {
	// 1. 优先原子递增更新：命中即返回，version 最终值由数据库保证连续
	n, err := r.client.DeviceConfig.Update().
		Where(entconfig.DeviceIDEQ(deviceID)).
		SetPayload(payload).
		SetStatus("pending").
		AddVersion(1).
		Save(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	// 2. 记录不存在：插入 version=1；并发下唯一约束冲突 → 回退到步骤 1 重试
	_, err = r.client.DeviceConfig.Create().
		SetDeviceID(deviceID).
		SetPayload(payload).
		SetVersion(1).
		SetStatus("pending").
		Save(ctx)
	if ent.IsConstraintError(err) {
		n, uerr := r.client.DeviceConfig.Update().
			Where(entconfig.DeviceIDEQ(deviceID)).
			SetPayload(payload).
			SetStatus("pending").
			AddVersion(1).
			Save(ctx)
		if uerr != nil {
			return uerr
		}
		if n == 0 {
			return fmt.Errorf("配置插入冲突但更新未命中: %s", deviceID)
		}
		return nil
	}
	return err
}

// MergePayload 乐观并发合并：读取当前 payload，经 mutate 生成新 payload，
// 再以 version 为条件原子更新（version 变了说明有人先写，重读重试）。
// 用于 sensors/actuators 这类「只改一个顶层 key」的更新，避免后写覆盖先写。
func (r *DeviceConfigRepo) MergePayload(ctx context.Context, deviceID string, mutate func(current string) (string, error)) (*ent.DeviceConfig, error) {
	const maxRetries = 3
	for range maxRetries {
		cfg, err := r.GetByDeviceID(ctx, deviceID)
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
		current := ""
		var version uint
		if cfg != nil {
			current = cfg.Payload
			version = cfg.Version
		}

		payload, err := mutate(current)
		if err != nil {
			return nil, err
		}

		if cfg == nil {
			created, err := r.client.DeviceConfig.Create().
				SetDeviceID(deviceID).
				SetPayload(payload).
				SetVersion(1).
				SetStatus("pending").
				Save(ctx)
			if err == nil {
				return created, nil
			}
			if ent.IsConstraintError(err) {
				continue // 并发创建：重读后走更新分支
			}
			return nil, err
		}

		n, err := r.client.DeviceConfig.Update().
			Where(
				entconfig.DeviceIDEQ(deviceID),
				entconfig.VersionEQ(version),
			).
			SetPayload(payload).
			SetStatus("pending").
			AddVersion(1).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return r.GetByDeviceID(ctx, deviceID)
		}
		// 0 行命中：版本已被并发修改，重试
	}
	return nil, fmt.Errorf("配置并发更新冲突，请重试: %s", deviceID)
}

// UpdateReported 回写设备上报的实际配置与版本，并将下发状态置为已确认
func (r *DeviceConfigRepo) UpdateReported(ctx context.Context, deviceID string, version uint, reported string) error {
	return r.client.DeviceConfig.Update().
		Where(entconfig.DeviceIDEQ(deviceID)).
		SetReportedVersion(version).
		SetReportedPayload(reported).
		SetStatus("acked").
		Exec(ctx)
}

// Delete 删除设备配置快照（通常由设备删除级联触发，这里提供显式入口）
func (r *DeviceConfigRepo) Delete(ctx context.Context, deviceID string) error {
	_, err := r.client.DeviceConfig.Delete().
		Where(entconfig.DeviceIDEQ(deviceID)).
		Exec(ctx)
	return err
}
