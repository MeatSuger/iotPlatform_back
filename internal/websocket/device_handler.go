package websocket

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"iot-platform.local/internal/model"
)

// DeviceTokenProvider 设备 Token → deviceID 查询接口（由 main 中 sa-token deviceMgr 实现）
type DeviceTokenProvider interface {
	GetLoginID(token string) (string, error)
}

// ReportService 设备上报服务接口（DeviceReportService 满足）
type ReportService interface {
	ReportStatus(ctx context.Context, deviceID, token string, dto entity.DeviceStatusDTO) error
	Heartbeat(ctx context.Context, deviceID, token string) error
}

// AckService 命令确认服务接口（DownlinkService 满足）
type AckService interface {
	AckCmd(ctx context.Context, cmdID uint) error
}

// SetupDeviceWS 配置设备 WebSocket 的 Token 校验器、上行消息处理器及上线/下线通知回调
// 将 main 中散布的 WebSocket 初始化逻辑收拢到一处
func (h *WsHandler) SetupDeviceWS(tokenProvider DeviceTokenProvider, report ReportService, ack AckService) {
	// Token 校验器
	h.SetDeviceTokenValidator(func(token string) (string, error) {
		return tokenProvider.GetLoginID(token)
	})

	// 上行消息处理器（传感器数据 / 心跳 / ACK）
	h.SetDeviceMessageHandler(func(deviceID, token string, message []byte) {
		var envelope struct {
			Type    string              `json:"type"`
			Sensors []entity.SensorData `json:"sensors"`
			CmdID   uint                `json:"cmdId"`
			ID      uint                `json:"id"` // 兼容 ESP32 response 消息
		}
		if err := json.Unmarshal(message, &envelope); err != nil {
			zap.S().Warnf("[DeviceWS] 消息解析失败 [device=%s]: %v", deviceID, err)
			return
		}

		ctx := context.Background()
		switch envelope.Type {
		case "data":
			dto := entity.DeviceStatusDTO{Sensors: envelope.Sensors}
			if err := report.ReportStatus(ctx, deviceID, token, dto); err != nil {
				zap.S().Warnf("[DeviceWS] 传感器数据上报失败 [device=%s]: %v", deviceID, err)
			}
		case "ping":
			if err := report.Heartbeat(ctx, deviceID, token); err != nil {
				zap.S().Warnf("[DeviceWS] 心跳处理失败 [device=%s]: %v", deviceID, err)
			}
		case "ack":
			if err := ack.AckCmd(ctx, envelope.CmdID); err != nil {
				zap.S().Warnf("[DeviceWS] ACK处理失败 [device=%s, cmd=%d]: %v", deviceID, envelope.CmdID, err)
			}
		case "response":
			// ESP32 对 cmd 的响应 {type:"response", id:N, status:"ok"}
			if envelope.ID > 0 {
				if err := ack.AckCmd(ctx, envelope.ID); err != nil {
					zap.S().Warnf("[DeviceWS] response处理失败 [device=%s, cmd=%d]: %v", deviceID, envelope.ID, err)
				}
			}
		default:
			zap.S().Warnf("[DeviceWS] 未知消息类型 [device=%s, type=%s]", deviceID, envelope.Type)
		}
	})

	// 设备上线/下线通知 + DB 持久化
	h.hub.OnDeviceOnline = func(deviceID string, ownerID uint) {
		// 更新数据库设备状态为 ONLINE
		if h.deviceStatusUpdater != nil {
			if err := h.deviceStatusUpdater(deviceID, "ONLINE"); err != nil {
				zap.S().Warnf("[DeviceWS] 更新设备上线状态失败 [device=%s]: %v", deviceID, err)
			}
		}
		msg, _ := json.Marshal(map[string]any{
			"type":      "deviceOnline",
			"deviceId":  deviceID,
			"timestamp": time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
		})
		h.hub.SendToOwner(ownerID, msg)
	}
	h.hub.OnDeviceOffline = func(deviceID string, ownerID uint) {
		// 更新数据库设备状态为 OFFLINE
		if h.deviceStatusUpdater != nil {
			if err := h.deviceStatusUpdater(deviceID, "OFFLINE"); err != nil {
				zap.S().Warnf("[DeviceWS] 更新设备下线状态失败 [device=%s]: %v", deviceID, err)
			}
		}
		msg, _ := json.Marshal(map[string]any{
			"type":      "deviceOffline",
			"deviceId":  deviceID,
			"timestamp": time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
		})
		h.hub.SendToOwner(ownerID, msg)
	}
}

// forwardToOwner 将设备上行消息转发给该设备的 Owner 管理端
// 优先从 Hub 缓存的设备连接中取 ownerID，避免每次查库
func (h *WsHandler) forwardToOwner(deviceID string, rawMessage []byte) {
	// 从 Hub 中取设备连接缓存的 ownerID（设备连接时已解析）
	h.hub.mu.RLock()
	client, ok := h.hub.deviceClients[deviceID]
	var ownerID uint
	if ok {
		ownerID = client.OwnerID
	}
	h.hub.mu.RUnlock()

	// Hub 中未缓存则回退到 resolveOwner 查库
	if ownerID == 0 && h.resolveOwner != nil {
		if oid, err := h.resolveOwner(deviceID); err == nil {
			ownerID = oid
		}
	}
	if ownerID == 0 {
		return
	}

	// 包装为统一格式，方便前端区分不同设备
	var payload any
	if err := json.Unmarshal(rawMessage, &payload); err != nil {
		payload = string(rawMessage)
	}
	msg, _ := json.Marshal(map[string]any{
		"deviceId":  deviceID,
		"data":      payload,
		"timestamp": time.Now().Format("2006-01-02T15:04:05.000Z07:00"),
	})
	h.hub.SendToOwner(ownerID, msg)
}
