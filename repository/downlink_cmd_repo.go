package repository

import (
	"context"

	"github.com/yu/iot-platform-go/entity"
	"gorm.io/gorm"
)

// DownlinkCmdRepo 下放命令数据访问
type DownlinkCmdRepo struct {
	BaseRepo[entity.DownlinkCmd]
}

// NewDownlinkCmdRepo 创建下放命令仓库
func NewDownlinkCmdRepo(db *gorm.DB) *DownlinkCmdRepo {
	return &DownlinkCmdRepo{BaseRepo: *NewBaseRepo[entity.DownlinkCmd](db)}
}

// ListPending 查询设备待下发命令（最近 50 条）
func (r *DownlinkCmdRepo) ListPending(ctx context.Context, deviceID string) ([]entity.DownlinkCmd, error) {
	var cmds []entity.DownlinkCmd
	err := r.DB.WithContext(ctx).
		Where("device_id = ? AND status = ?", deviceID, entity.CmdStatusPending).
		Order("id ASC").Limit(50).
		Find(&cmds).Error
	return cmds, err
}

// MarkSent 批量标记命令为已发送
func (r *DownlinkCmdRepo) MarkSent(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.DB.WithContext(ctx).
		Model(&entity.DownlinkCmd{}).
		Where("id IN ?", ids).
		Update("status", entity.CmdStatusSent).Error
}

// MarkDelivered 标记单条命令为已送达（设备 ACK 确认）
func (r *DownlinkCmdRepo) MarkDelivered(ctx context.Context, id uint) error {
	return r.DB.WithContext(ctx).
		Model(&entity.DownlinkCmd{}).
		Where("id = ?", id).
		Update("status", entity.CmdStatusDelivered).Error
}
