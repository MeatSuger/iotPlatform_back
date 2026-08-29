//go:build tools

// tools 文件: 锁定代码生成工具链的依赖版本。
// 仅用于 go mod tidy 保留 ent 工具链的传递依赖
// （如 github.com/clipperhouse/displaywidth，ent v0.14.6
// 要求的 v0.6.2 与 uax29 v2.7.0 API 不兼容，需固定为 v0.11.0）。
// 该文件不参与任何构建产物。
package tools

import (
	_ "entgo.io/ent/cmd/ent"
)
