package repository

import (
	"context"

	"github.com/yu/iot-platform-go/internal/ent"
	entcmd "github.com/yu/iot-platform-go/internal/ent/downlinkcmd"
)

// DownlinkCmdRepo 下放命令数据访问
type DownlinkCmdRepo struct {
	client *ent.Client
}

func NewDownlinkCmdRepo(client *ent.Client) *DownlinkCmdRepo {
	return &DownlinkCmdRepo{client: client}
}

func (r *DownlinkCmdRepo) Create(ctx context.Context, cmd *ent.DownlinkCmd) (*ent.DownlinkCmd, error) {
	return r.client.DownlinkCmd.Create().
		SetDeviceID(cmd.DeviceID).
		SetType(cmd.Type).
		SetPayload(cmd.Payload).
		SetStatus(cmd.Status).
		SetCreatedAt(cmd.CreatedAt).
		Save(ctx)
}

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

func (r *DownlinkCmdRepo) MarkSent(ctx context.Context, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.client.DownlinkCmd.Update().
		Where(entcmd.IDIn(ids...)).
		SetStatus("sent").
		Exec(ctx)
}

func (r *DownlinkCmdRepo) MarkDelivered(ctx context.Context, id uint) error {
	return r.client.DownlinkCmd.UpdateOneID(id).
		SetStatus("delivered").
		Exec(ctx)
}
