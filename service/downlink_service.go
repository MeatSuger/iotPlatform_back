package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	mqttEntity "github.com/yu/iot-platform-go/entity/mqtt"

	"github.com/yu/iot-platform-go/common"
	"github.com/yu/iot-platform-go/entity"
	"github.com/yu/iot-platform-go/repository"
	"github.com/yu/iot-platform-go/websocket"
)

const (
	cmdQueuePrefix = "cmd:queue:"
	cmdQueueMaxLen = 200 // 每设备最多保留 200 条待消费命令
)

// DownlinkService 下放服务（HTTP 轮询 + WebSocket 实时推送）
type DownlinkService struct {
	cmdRepo    *repository.DownlinkCmdRepo
	deviceRepo *repository.DeviceRepo
	rdb        *redis.Client
	wsHub      *websocket.Hub // nil if WebSocket not configured
}

// NewDownlinkService 创建下放服务
func NewDownlinkService(cmdRepo *repository.DownlinkCmdRepo, deviceRepo *repository.DeviceRepo, rdb *redis.Client, wsHub *websocket.Hub) *DownlinkService {
	return &DownlinkService{
		cmdRepo:    cmdRepo,
		deviceRepo: deviceRepo,
		rdb:        rdb,
		wsHub:      wsHub,
	}
}

// EnqueueCmd 用户向设备下发命令
func (s *DownlinkService) EnqueueCmd(ctx context.Context, deviceID string, req entity.DownlinkCmdRequest) (*entity.DownlinkCmd, error) {
	// Payload 是 json.RawMessage，序列化为 string 存入 DB
	payloadStr := string(req.Payload)

	cmd := &entity.DownlinkCmd{
		DeviceID:  deviceID,
		Type:      req.Type,
		Payload:   payloadStr,
		Status:    entity.CmdStatusPending,
		CreatedAt: common.DateTimeNow(),
	}

	// 1. 持久化到 DB
	if err := s.cmdRepo.Create(ctx, cmd); err != nil {
		return nil, fmt.Errorf("保存命令失败: %w", err)
	}

	// 2. 推入 Redis 队列（设备轮询消费）
	queueKey := cmdQueuePrefix + deviceID
	cmdJSON, _ := json.Marshal(entity.DownlinkCmdResponse{
		ID:        cmd.ID,
		Type:      cmd.Type,
		Payload:   req.Payload, // 保留原始 JSON 结构
		CreatedAt: cmd.CreatedAt,
	})
	if err := s.rdb.RPush(ctx, queueKey, string(cmdJSON)).Err(); err != nil {
		zap.S().Infof("[Downlink] Redis 入队失败 [device=%s]: %v", deviceID, err)
	}
	// 限制队列长度
	s.rdb.LTrim(ctx, queueKey, -cmdQueueMaxLen, -1)

	// 3. 尝试 WebSocket 实时推送
	if s.wsHub != nil {
		var payloadObj interface{}
		json.Unmarshal(req.Payload, &payloadObj)

		msg, _ := json.Marshal(map[string]interface{}{
			"type":      "cmd",
			"id":        cmd.ID,
			"cmdType":   cmd.Type,
			"payload":   payloadObj,
			"createdAt": cmd.CreatedAt.Format(common.DateTimeFormatWithZone),
		})
		s.wsHub.SendToDevice(deviceID, msg)
	}

	return cmd, nil
}

// PollCmd 设备拉取待消费命令（消费后从队列移除）
func (s *DownlinkService) PollCmd(ctx context.Context, deviceID string) ([]entity.DownlinkCmdResponse, error) {
	queueKey := cmdQueuePrefix + deviceID

	// 从 Redis 队列弹出所有待消费命令
	results, err := s.rdb.LRange(ctx, queueKey, 0, -1).Result()
	if err != nil || len(results) == 0 {
		return []entity.DownlinkCmdResponse{}, nil
	}

	// 删除队列
	s.rdb.Del(ctx, queueKey)

	cmds := make([]entity.DownlinkCmdResponse, 0, len(results))
	var ids []uint
	for _, raw := range results {
		var cmd entity.DownlinkCmdResponse
		if err := json.Unmarshal([]byte(raw), &cmd); err == nil {
			cmds = append(cmds, cmd)
			ids = append(ids, cmd.ID)
		}
	}

	// 批量标记 DB 中命令为已发送
	if len(ids) > 0 {
		if err := s.cmdRepo.MarkSent(ctx, ids); err != nil {
			zap.S().Infof("[Downlink] 标记已发送失败: %v", err)
		}
	}

	return cmds, nil
}

// PublishViaMQTT 通过 MQTT 向设备下发（供外部实时通道使用）
func (s *DownlinkService) PublishViaMQTT(deviceID, cmdType, payload string) mqttEntity.PublishRequest {
	topic := fmt.Sprintf("device/%s/cmd", deviceID)
	qos := 1
	return mqttEntity.PublishRequest{
		Topic:   topic,
		Qos:     &qos,
		Payload: payload,
	}
}

// AckCmd 设备确认收到命令（ACK）
func (s *DownlinkService) AckCmd(ctx context.Context, cmdID uint) error {
	if err := s.cmdRepo.MarkDelivered(ctx, cmdID); err != nil {
		zap.S().Warnf("[Downlink] ACK 更新失败 [cmd=%d]: %v", cmdID, err)
		return err
	}
	zap.S().Infof("[Downlink] 设备已确认 [cmd=%d]", cmdID)
	return nil
}

// NotifyOwnerCmd 命令下发后，通知该设备 owner 的管理端（WebSocket 实时推送）
func (s *DownlinkService) NotifyOwnerCmd(deviceID string, cmd *entity.DownlinkCmd) {
	if s.wsHub == nil {
		return
	}
	// 解码 payload
	var payloadObj interface{}
	json.Unmarshal([]byte(cmd.Payload), &payloadObj)
	msg, _ := json.Marshal(map[string]interface{}{
		"type":      "cmdSent",
		"deviceId":  deviceID,
		"cmdId":     cmd.ID,
		"cmdType":   cmd.Type,
		"payload":   payloadObj,
		"status":    cmd.Status,
		"createdAt": cmd.CreatedAt.Format(common.DateTimeFormatWithZone),
	})
	s.wsHub.SendToDeviceOwner(deviceID, msg)
}
