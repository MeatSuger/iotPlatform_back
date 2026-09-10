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
	cmdQueueTTL    = 7 * 24 * time.Hour // 队列键 TTL，防废弃设备永久残留
)

// pollCmdLua 原子取出并删除队列全部命令（LRange + Del 非原子 → 并发 Poll 竞态修复）
var pollCmdLua = redis.NewScript(`
	local vals = redis.call('LRANGE', KEYS[1], 0, -1)
	if #vals > 0 then
		redis.call('DEL', KEYS[1])
	end
	return vals
`)

// DownlinkService 下放服务
type DownlinkService struct {
	msgRepo    *repository.MessageLogRepo
	deviceRepo *repository.DeviceRepo
	rdb        *redis.Client
	wsHub      *websocket.Hub
	mqttPub    MqttPublisher
}

func NewDownlinkService(msgRepo *repository.MessageLogRepo, deviceRepo *repository.DeviceRepo, rdb *redis.Client, wsHub *websocket.Hub, mqttPub MqttPublisher) *DownlinkService {
	return &DownlinkService{msgRepo: msgRepo, deviceRepo: deviceRepo, rdb: rdb, wsHub: wsHub, mqttPub: mqttPub}
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

// EnqueueCmd 下发命令：持久化到 PostgreSQL + 写入 Redis 队列 + WebSocket 实时推送
func (s *DownlinkService) EnqueueCmd(ctx context.Context, deviceID string, req DownlinkCmdRequest) (*ent.MessageLog, error) {
	now := time.Now()
	payloadStr := string(req.Payload)

	category := "cmd"
	if req.Type == "config" {
		category = "config"
	}

	cmd, err := s.msgRepo.Create(ctx, &ent.MessageLog{
		Direction: "down",
		Category:  category,
		DeviceID:  deviceID,
		Type:      req.Type,
		Payload:   payloadStr,
		Status:    "pending",
		CreatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("保存命令失败: %w", err)
	}

	// Redis 队列（RPush + LTrim 保底 + EXPIRE 防残留）
	queueKey := cmdQueuePrefix + deviceID
	cmdJSON, _ := json.Marshal(DownlinkCmdResponse{
		ID:        cmd.ID,
		Type:      cmd.Type,
		Payload:   json.RawMessage(cmd.Payload),
		CreatedAt: cmd.CreatedAt,
	})
	// 注意: LTrim 只保留最近 cmdQueueMaxLen 条，慢轮询设备离线期间
	// 积累的旧命令会被裁剪（DB 中仍为 pending）。如需不丢命令应改用
	// Stream consumer group 或 Set + 确认机制。
	if err := s.rdb.RPush(ctx, queueKey, string(cmdJSON)).Err(); err != nil {
		return nil, fmt.Errorf("命令入队失败: %w", err)
	}
	if err := s.rdb.LTrim(ctx, queueKey, -cmdQueueMaxLen, -1).Err(); err != nil {
		return nil, fmt.Errorf("命令队列裁剪失败: %w", err)
	}
	if err := s.rdb.Expire(ctx, queueKey, cmdQueueTTL).Err(); err != nil {
		return nil, fmt.Errorf("命令队列 TTL 设置失败: %w", err)
	}

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

	// MQTT 实时下行（QoS1 → iot/{deviceId}/cmd，MQTT 设备订阅即收）。
	// type=config 不在此发布：配置快照由 DeviceConfigService 经 retained 主题专管，
	// 避免双通道重复投递；离线期间的命令后续经 HTTP GET /commands 兜底拉取。
	if s.mqttPub != nil && cmd.Type != "config" {
		if err := s.mqttPub.PublishCommand(deviceID, cmdJSON); err != nil {
			zap.S().Warnf("[Downlink] MQTT 命令发布失败 [device=%s cmd=%d]: %v", deviceID, cmd.ID, err)
		}
	}

	return cmd, nil
}

// PollCmd 设备轮询命令：Lua 原子取出并删除队列，标记为已发送
func (s *DownlinkService) PollCmd(ctx context.Context, deviceID string) ([]DownlinkCmdResponse, error) {
	queueKey := cmdQueuePrefix + deviceID

	results, err := pollCmdLua.Run(ctx, s.rdb, []string{queueKey}).Result()
	if err != nil {
		// Redis 故障不再静默吞掉，上抛给调用方
		return nil, fmt.Errorf("轮询命令失败: %w", err)
	}
	vals, ok := results.([]any)
	if !ok || len(vals) == 0 {
		return []DownlinkCmdResponse{}, nil
	}

	cmds := make([]DownlinkCmdResponse, 0, len(vals))
	var ids []uint
	for _, v := range vals {
		raw, ok := v.(string)
		if !ok {
			continue
		}
		var cmd DownlinkCmdResponse
		if err := json.Unmarshal([]byte(raw), &cmd); err == nil {
			cmds = append(cmds, cmd)
			ids = append(ids, cmd.ID)
		}
	}
	if len(ids) > 0 {
		if err := s.msgRepo.MarkSent(ctx, ids); err != nil {
			return nil, err
		}
	}
	return cmds, nil
}

// AckCmd 设备确认命令（标记为已送达）
func (s *DownlinkService) AckCmd(ctx context.Context, cmdID uint) error {
	if err := s.msgRepo.MarkDelivered(ctx, cmdID); err != nil {
		zap.S().Warnf("[Downlink] ACK 更新失败 [cmd=%d]: %v", cmdID, err)
		return err
	}
	zap.S().Infof("[Downlink] 设备已确认 [cmd=%d]", cmdID)
	return nil
}
