package controller

import (
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ResetChannelMonitorCustomActionAttempts(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	var request service.ChannelMonitorCustomActionResetRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "请提供待重置的调用记录")
		return
	}
	actionID := c.Param("action_id")
	state, err := service.ResetChannelMonitorCustomActionAttempts(c.Request.Context(), channelID, actionID, request, time.Now)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.SysLog(fmt.Sprintf("渠道监控触发接口今日次数已重置: user_id=%d channel_id=%d action_id=%s day=%s previous_attempts=%d", c.GetInt("id"), channelID, actionID, request.Day, request.Attempts))
	common.ApiSuccess(c, state)
}
