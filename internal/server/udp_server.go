package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"go.uber.org/zap"

	"iot-platform.local/internal/model"
)

// ReportService UDP 上报所需的服务接口
type ReportService interface {
	ReportStatus(ctx context.Context, deviceID, token string, dto entity.DeviceStatusDTO) error
}

// UDPServer UDP 设备数据上报服务
type UDPServer struct {
	reportSvc ReportService
	conn      *net.UDPConn
	stop      chan struct{}
}

// NewUDPServer 创建 UDP 服务器
func NewUDPServer(reportSvc ReportService) *UDPServer {
	return &UDPServer{
		reportSvc: reportSvc,
		stop:      make(chan struct{}),
	}
}

// udpPayload UDP 数据包格式
type udpPayload struct {
	DeviceID string              `json:"deviceId"`
	Token    string              `json:"token"`
	Sensors  []entity.SensorData `json:"sensors"`
}

// Start 启动 UDP 监听
func (s *UDPServer) Start(port int) error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("解析UDP地址失败: %w", err)
	}

	s.conn, err = net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("UDP监听失败: %w", err)
	}

	zap.L().Info("[UDP] 监听端口", zap.Int("port", port))

	go s.loop()

	return nil
}

func (s *UDPServer) loop() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-s.stop:
			return
		default:
		}

		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				zap.S().Warnf("[UDP] 读取失败: %v", err)
				continue
			}
		}

		data := make([]byte, n)
		copy(data, buf[:n])
		go s.handle(remoteAddr, data)
	}
}

func (s *UDPServer) handle(addr *net.UDPAddr, data []byte) {
	var p udpPayload
	if err := json.Unmarshal(data, &p); err != nil {
		zap.S().Warnf("[UDP] JSON解析失败 [remote=%s, data=%s]: %v", addr, string(data), err)
		return
	}

	if p.DeviceID == "" {
		zap.S().Warnf("[UDP] 缺少deviceId [remote=%s]", addr)
		return
	}

	ctx := context.Background()
	dto := entity.DeviceStatusDTO{Sensors: p.Sensors}
	if err := s.reportSvc.ReportStatus(ctx, p.DeviceID, p.Token, dto); err != nil {
		zap.S().Warnf("[UDP] 上报失败 [device=%s, remote=%s]: %v", p.DeviceID, addr, err)
		return
	}

	zap.S().Debugf("[UDP] 数据上报成功 [device=%s, remote=%s, sensors=%d]", p.DeviceID, addr, len(p.Sensors))
}

// Stop 停止 UDP 监听
func (s *UDPServer) Stop() {
	close(s.stop)
	if s.conn != nil {
		err := s.conn.Close()
		if err != nil {
			return
		}
	}
	zap.L().Info("[UDP] 已关闭")
}
