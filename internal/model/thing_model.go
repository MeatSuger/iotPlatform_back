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

package entity

import "iot-platform.local/pkg/common"

// SensorLatest 传感器最近一次上报值（物模型聚合视图，由设备详情接口携带）。
// 与 SensorData 的区别：仅保留展示所需 value/timestamp，去掉上报侧 name/type。
type SensorLatest struct {
	Value     any             `json:"value"`
	Timestamp common.DateTime `json:"timestamp"`
}

// SensorWithLatest 传感器定义 + 最近一次上报值（设备详情物模型数组元素）。
// Latest 为 nil 表示该定义从未上报过数据（前端可显示"无数据"占位）。
type SensorWithLatest struct {
	Sensor
	Latest *SensorLatest `json:"latest"`
}

// AttachLatest 将最近一次上报的遥测（recent）合并到传感器定义列表。
//
// 关联规则（与固件上报契约对齐）：
//  1. 上报 name == 定义 id（固件按配置编译后上报，name 即定义 id）；
//  2. 兜底：上报 name == 定义 name（存量固件/自定义上报）；
//  3. 均不匹配 → latest 置 nil（从未上报或命名漂移，由前端做"无数据"展示）。
//
// 同一上报名出现多条时取 timestamp 最新的一条。defs 顺序保持入参顺序（定义按 id 升序），
// 与 List 接口输出一致。
func AttachLatest(defs []Sensor, recent []SensorData) []SensorWithLatest {
	out := make([]SensorWithLatest, 0, len(defs))
	if len(defs) == 0 {
		return out
	}

	// 上报名 → 最新采样（批内同名单条取时间最新）
	latestByName := make(map[string]SensorData, len(recent))
	for _, d := range recent {
		if d.Name == "" {
			continue
		}
		prev, ok := latestByName[d.Name]
		if !ok || d.Timestamp.After(prev.Timestamp) {
			latestByName[d.Name] = d
		}
	}
	if len(latestByName) == 0 {
		for _, def := range defs {
			out = append(out, SensorWithLatest{Sensor: def})
		}
		return out
	}

	for _, def := range defs {
		item := SensorWithLatest{Sensor: def}
		if data, ok := latestByName[def.ID]; ok {
			item.Latest = toLatest(data)
		} else if def.Name != "" {
			if data, ok := latestByName[def.Name]; ok {
				item.Latest = toLatest(data)
			}
		}
		out = append(out, item)
	}
	return out
}

func toLatest(data SensorData) *SensorLatest {
	return &SensorLatest{Value: data.Value, Timestamp: common.DateTimeFrom(data.Timestamp)}
}
