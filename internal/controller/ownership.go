package controller

import (
	"errors"

	"github.com/gin-gonic/gin"

	"iot-platform.local/internal/ent"
	"iot-platform.local/internal/service"
	"iot-platform.local/pkg/common"
)

// checkOwnership 校验设备存在且归属指定 owner，失败时写响应并返回非 nil 错误。
// forbiddenMsg 为空时使用默认文案「无权操作该设备」。
func checkOwnership(c *gin.Context, deviceSvc *service.DeviceService, deviceID string, ownerID uint, forbiddenMsg string) (*ent.Device, error) {
	device, err := deviceSvc.GetByDeviceID(c.Request.Context(), deviceID)
	if err != nil {
		common.FailWithMsg(c, common.CodeNotFound, "设备不存在")
		return nil, err
	}
	if device.OwnerID != ownerID {
		if forbiddenMsg == "" {
			forbiddenMsg = "无权操作该设备"
		}
		common.FailWithMsg(c, common.CodeForbidden, forbiddenMsg)
		return nil, errors.New(forbiddenMsg)
	}
	return device, nil
}
