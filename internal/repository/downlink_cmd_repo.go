package repository

import (
	"context"

	"iot-platform.local/internal/ent"
	entcmd "iot-platform.local/internal/ent/downlinkcmd"
)

// DownlinkCmdRepo 下放命令数据访问
type DownlinkCmdRepo struct {
	client *ent.Client
}

// NewDownlinkCmdRepo 创建下放命令仓库
func NewDownlinkCmdRepo(client *ent.Client) *DownlinkCmdRepo {
	return &DownlinkCmdRepo{client: client}
}

// Create 创建下放命令记录
func (r *DownlinkCmdRepo) Create(ctx context.Context, cmd *ent.DownlinkCmd) (*ent.DownlinkCmd, error) {
	return r.client.DownlinkCmd.Create().
		SetDeviceID(cmd.DeviceID).
		SetType(cmd.Type).
		SetPayload(cmd.Payload).
		SetStatus(cmd.Status).
		SetCreatedAt(cmd.CreatedAt).
		Save(ctx)
}

// ListPending 查询设备待下发命令（status=pending，按 ID 升序，最多 50 条）
func (r *DownlinkCmdRepo) ListPending(ctx context.Context, deviceID string) ([]*ent.DownlinkCmd, error) {
	return r.client.DownlinkCmd.Query().
		Where(
			entcmd.DeviceIDEQ(deviceID),
			entcmd.StatusEQ("pending"),
		).
		Order(ent.Asc(entcmd.FieldID)).
		Limit(50).
		All(ctx)
}

// MarkSent 将一批命令标记为已发送（sent）
func (r *DownlinkCmdRepo) MarkSent(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.client.DownlinkCmd.Update().
		Where(entcmd.IDIn(ids...)).
		SetStatus("sent").
		Exec(ctx)
}

// MarkDelivered 将命令标记为已送达（delivered）
func (r *DownlinkCmdRepo) MarkDelivered(ctx context.Context, id uint) error {
	return r.client.DownlinkCmd.UpdateOneID(id).
		SetStatus("delivered").
		Exec(ctx)
}
