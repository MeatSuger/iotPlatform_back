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
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/repository"
	"iot-platform.local/internal/websocket"
	"iot-platform.local/pkg/cache"
	"iot-platform.local/pkg/common"
)

const (
	cmdQueuePrefix    = cache.CmdQueuePrefix
	cmdQueueMaxLen    = 200
	cmdQueueTTL       = 7 * 24 * time.Hour // 队列键 TTL，防废弃设备永久残留
	cmdRequeueTimeout = 5 * time.Second    // 标记失败后回滚队列的独立超时
)

// enqueueCmdLua 原子入队：RPUSH + LTRIM + EXPIRE。
// 拆成三条命令时，LTrim/Expire 失败会让接口报错但命令已在队列（调用方重试 → 重复下发），
// 或队列键失去 TTL 永久残留。
var enqueueCmdLua = redis.NewScript(`
	redis.call('RPUSH', KEYS[1], ARGV[1])
	redis.call('LTRIM', KEYS[1], -tonumber(ARGV[2]), -1)
	redis.call('EXPIRE', KEYS[1], tonumber(ARGV[3]))
	return 1
`)

// pollCmdLua 原子取出并删除队列全部命令（LRange + Del 非原子 → 并发 Poll 竞态修复）
var pollCmdLua = redis.NewScript(`
	local vals = redis.call('LRANGE', KEYS[1], 0, -1)
	if #vals > 0 then
		redis.call('DEL', KEYS[1])
	end
	return vals
`)

// requeueCmdLua 回滚 PollCmd：命令已从队列弹出但 MarkSent 失败时，按原顺序放回，
// 并恢复长度上限与 TTL。参数：ARGV[1]=条数，ARGV[2..n+1]=命令 JSON，
// ARGV[n+2]=最大长度，ARGV[n+3]=TTL 秒。
var requeueCmdLua = redis.NewScript(`
	local n = tonumber(ARGV[1])
	for i = 2, n + 1 do
		redis.call('RPUSH', KEYS[1], ARGV[i])
	end
	redis.call('LTRIM', KEYS[1], -tonumber(ARGV[n + 2]), -1)
	redis.call('EXPIRE', KEYS[1], tonumber(ARGV[n + 3]))
	return n
`)

// DownlinkService 下放服务
type DownlinkService struct {
	msgRepo    *repository.MessageLogRepo
	deviceRepo *repository.DeviceRepo
	rdb        *redis.Client
	wsHub      *websocket.Hub
	mqttPub    MqttPublisher
	pubCh      chan pubJob
}

// pubJob 待发布的 MQTT 命令（单 worker 消费，保证命令顺序）
type pubJob struct {
	deviceID string
	payload  []byte
}

func NewDownlinkService(msgRepo *repository.MessageLogRepo, deviceRepo *repository.DeviceRepo, rdb *redis.Client, wsHub *websocket.Hub, mqttPub MqttPublisher) *DownlinkService {
	s := &DownlinkService{msgRepo: msgRepo, deviceRepo: deviceRepo, rdb: rdb, wsHub: wsHub, mqttPub: mqttPub}
	if mqttPub != nil {
		s.pubCh = make(chan pubJob, 256)
		go s.publishWorker()
	}
	return s
}

// publishWorker 异步发布 MQTT 命令：不在 HTTP/WS 请求路径里等 5s QoS1 PUBACK。
// 发布失败只告警；命令已落库+入 Redis 队列，设备可由 GET /commands 兜底拉取。
func (s *DownlinkService) publishWorker() {
	for job := range s.pubCh {
		if err := s.mqttPub.PublishCommand(job.deviceID, job.payload); err != nil {
			zap.S().Warnf("[Downlink] MQTT 命令发布失败 [device=%s]: %v", job.deviceID, err)
		}
	}
}

// DownlinkCmdRequest 下发命令请求
//
// swaggertype 标注：swag 解析不了 json.RawMessage，不标注会把整个结构体丢成空对象。
type DownlinkCmdRequest struct {
	Type    string          `json:"type" binding:"required"`
	Payload json.RawMessage `json:"payload" binding:"required" swaggertype:"object"`
}

// DownlinkCmdResponse 设备拉取命令响应
type DownlinkCmdResponse struct {
	ID        uint            `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload" swaggertype:"object"`
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
	if err := enqueueCmdLua.Run(ctx, s.rdb, []string{queueKey},
		string(cmdJSON), cmdQueueMaxLen, int64(cmdQueueTTL.Seconds()),
	).Err(); err != nil {
		return nil, fmt.Errorf("命令入队失败: %w", err)
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
			"createdAt": cmd.CreatedAt.Format(common.DateTimeFormatWithZone),
		})
		s.wsHub.SendToDevice(deviceID, msg)
	}

	// MQTT 实时下行（QoS1 → iot/{deviceId}/cmd，MQTT 设备订阅即收）。
	// type=config 不在此发布：配置快照由 DeviceConfigService 经 retained 主题专管，
	// 避免双通道重复投递；离线期间的命令后续经 HTTP GET /commands 兜底拉取。
	if s.mqttPub != nil && cmd.Type != "config" {
		select {
		case s.pubCh <- pubJob{deviceID: deviceID, payload: cmdJSON}:
		default:
			zap.S().Warnf("[Downlink] MQTT 发布队列已满，实时下行跳过 [device=%s cmd=%d]（HTTP 拉取兜底）", deviceID, cmd.ID)
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
	ids := make([]uint, 0, len(vals))
	raws := make([]string, 0, len(vals))
	for _, v := range vals {
		raw, ok := v.(string)
		if !ok {
			continue
		}
		var cmd DownlinkCmdResponse
		if err := json.Unmarshal([]byte(raw), &cmd); err == nil {
			cmds = append(cmds, cmd)
			ids = append(ids, cmd.ID)
			raws = append(raws, raw)
		}
	}
	if len(ids) > 0 {
		if err := s.msgRepo.MarkSent(ctx, ids); err != nil {
			// 命令已经被 Lua 从队列取出；标记失败若直接把错误抛给设备，队列已空、
			// 这批命令永远不会再投递（message_log 永久 pending）。必须先回滚队列。
			if requeueErr := s.requeueCommands(ctx, queueKey, raws); requeueErr != nil {
				return nil, errors.Join(
					fmt.Errorf("标记命令为已发送失败: %w", err),
					fmt.Errorf("回滚命令队列失败: %w", requeueErr),
				)
			}
			return nil, fmt.Errorf("标记命令为已发送失败: %w", err)
		}
	}
	return cmds, nil
}

// requeueCommands 把 PollCmd 已弹出但未能标记为 sent 的命令按原顺序放回队列。
// 使用剥离取消信号的 ctx：调用方请求可能已取消，但回滚仍必须执行。
func (s *DownlinkService) requeueCommands(ctx context.Context, queueKey string, raws []string) error {
	if len(raws) == 0 {
		return nil
	}
	args := make([]any, 0, len(raws)+3)
	args = append(args, len(raws))
	for _, raw := range raws {
		args = append(args, raw)
	}
	args = append(args, cmdQueueMaxLen, int64(cmdQueueTTL.Seconds()))

	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cmdRequeueTimeout)
	defer cancel()
	return requeueCmdLua.Run(rctx, s.rdb, []string{queueKey}, args...).Err()
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
