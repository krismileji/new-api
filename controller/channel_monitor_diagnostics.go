package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetChannelMonitorDiagnostics(c *gin.Context) {
	snapshot, err := service.GetChannelMonitorDiagnostics(c.Request.Context())
	if err != nil {
		common.ApiErrorMsg(c, "今日诊断读取失败，请检查 Redis 连接后重试")
		return
	}
	common.ApiSuccess(c, snapshot)
}

func ResetChannelMonitorDiagnostics(c *gin.Context) {
	var request struct {
		DayStart int64 `json:"day_start"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.DayStart <= 0 {
		common.ApiErrorMsg(c, "请提供有效的统计日期")
		return
	}
	snapshot, err := service.ResetChannelMonitorDiagnostics(c.Request.Context(), request.DayStart)
	if err != nil {
		if err == service.ErrChannelMonitorDiagnosticsDayChanged {
			common.ApiError(c, err)
			return
		}
		common.ApiErrorMsg(c, "重置今日诊断失败，请检查 Redis 连接后重试")
		return
	}
	common.SysLog(fmt.Sprintf("渠道监控今日诊断计数已重置: user_id=%d day_start=%d reset_at=%d", c.GetInt("id"), snapshot.DayStart, snapshot.LastResetAt))
	common.ApiSuccess(c, snapshot)
}
