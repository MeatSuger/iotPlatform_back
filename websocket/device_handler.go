package websocket

import (
	"context"
	"encoding/json"

	"go.uber.org/zap"

	"github.com/yu/iot-platform-go/entity"
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

// SetupDeviceWS 配置设备 WebSocket 的 Token 校验器和上行消息处理器
// 将 main 函数中散布的 WebSocket 初始化逻辑收拢到一处
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
}
