package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/websocket"
)

const (
	cmdQueuePrefix = "cmd:queue:"
	cmdQueueMaxLen = 200
)

// DownlinkService 下放服务
type DownlinkService struct {
	cmdRepo    *repository.DownlinkCmdRepo
	deviceRepo *repository.DeviceRepo
	rdb        *redis.Client
	wsHub      *websocket.Hub
}

func NewDownlinkService(cmdRepo *repository.DownlinkCmdRepo, deviceRepo *repository.DeviceRepo, rdb *redis.Client, wsHub *websocket.Hub) *DownlinkService {
	return &DownlinkService{cmdRepo: cmdRepo, deviceRepo: deviceRepo, rdb: rdb, wsHub: wsHub}
}

// DownlinkCmdRequest 下发命令请求
type DownlinkCmdRequest struct {
	Type    string          `json:"type" binding:"required"`
	Payload json.RawMessage `json:"payload" binding:"required"`
}

// DownlinkCmdResponse 设备拉取命令响应
type DownlinkCmdResponse struct {
	ID        uint            `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

func (s *DownlinkService) EnqueueCmd(ctx context.Context, deviceID string, req DownlinkCmdRequest) (*ent.DownlinkCmd, error) {
	now := time.Now()
	payloadStr := string(req.Payload)

	cmd, err := s.cmdRepo.Create(ctx, &ent.DownlinkCmd{
		DeviceID:  deviceID,
		Type:      req.Type,
		Payload:   payloadStr,
		Status:    "pending",
		CreatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("保存命令失败: %w", err)
	}

	// Redis 队列
	queueKey := cmdQueuePrefix + deviceID
	cmdJSON, _ := json.Marshal(DownlinkCmdResponse{
		ID:        cmd.ID,
		Type:      cmd.Type,
		Payload:   json.RawMessage(cmd.Payload),
		CreatedAt: cmd.CreatedAt,
	})
	s.rdb.RPush(ctx, queueKey, string(cmdJSON))
	s.rdb.LTrim(ctx, queueKey, -cmdQueueMaxLen, -1)

	// WebSocket 实时推送
	if s.wsHub != nil {
		var payloadObj any
		err := json.Unmarshal([]byte(cmd.Payload), &payloadObj)
		if err != nil {
			return nil, err
		}
		msg, _ := json.Marshal(map[string]any{
			"type":      "cmd",
			"id":        cmd.ID,
			"cmdType":   cmd.Type,
			"payload":   payloadObj,
			"createdAt": cmd.CreatedAt.Format("2006-01-02T15:04:05.000-07:00"),
		})
		s.wsHub.SendToDevice(deviceID, msg)
	}

	return cmd, nil
}

func (s *DownlinkService) PollCmd(ctx context.Context, deviceID string) ([]DownlinkCmdResponse, error) {
	queueKey := cmdQueuePrefix + deviceID
	results, err := s.rdb.LRange(ctx, queueKey, 0, -1).Result()
	if err != nil || len(results) == 0 {
		return []DownlinkCmdResponse{}, nil
	}
	s.rdb.Del(ctx, queueKey)

	cmds := make([]DownlinkCmdResponse, 0, len(results))
	var ids []uint
	for _, raw := range results {
		var cmd DownlinkCmdResponse
		if err := json.Unmarshal([]byte(raw), &cmd); err == nil {
			cmds = append(cmds, cmd)
			ids = append(ids, cmd.ID)
		}
	}
	if len(ids) > 0 {
		err := s.cmdRepo.MarkSent(ctx, ids)
		if err != nil {
			return nil, err
		}
	}
	return cmds, nil
}

func (s *DownlinkService) AckCmd(ctx context.Context, cmdID uint) error {
	if err := s.cmdRepo.MarkDelivered(ctx, cmdID); err != nil {
		zap.S().Warnf("[Downlink] ACK 更新失败 [cmd=%d]: %v", cmdID, err)
		return err
	}
	zap.S().Infof("[Downlink] 设备已确认 [cmd=%d]", cmdID)
	return nil
}

func (s *DownlinkService) NotifyOwnerCmd(deviceID string, cmd *ent.DownlinkCmd) {
	if s.wsHub == nil {
		return
	}
	var payloadObj any
	err := json.Unmarshal([]byte(cmd.Payload), &payloadObj)
	if err != nil {
		return
	}
	msg, _ := json.Marshal(map[string]any{
		"type":      "cmdSent",
		"deviceId":  deviceID,
		"cmdId":     cmd.ID,
		"cmdType":   cmd.Type,
		"payload":   payloadObj,
		"status":    cmd.Status,
		"createdAt": cmd.CreatedAt.Format("2006-01-02T15:04:05.000-07:00"),
	})
	s.wsHub.SendToDeviceOwner(deviceID, msg)
}
