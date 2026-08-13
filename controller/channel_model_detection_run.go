package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type channelModelDetectionManualRunRequest struct {
	Preset          string `json:"preset"`
	ConfirmHighCost bool   `json:"confirm_high_cost"`
}

func StartChannelModelDetectionManualRun(c *gin.Context) {
	channelID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道 ID 必须为正整数"})
		return
	}
	var request channelModelDetectionManualRunRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "手动模型检测请求格式无效"})
		return
	}
	response, err := service.StartChannelModelDetectionManualRun(c.Request.Context(), nil, service.ChannelModelDetectionManualRunRequest{
		ChannelID: channelID, Preset: request.Preset, ConfirmHighCost: request.ConfirmHighCost,
		CreatedByUserID: c.GetInt("id"), CreatedByUsername: c.GetString("username"),
	}, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道或模型检测配置不存在"})
		case errors.Is(err, service.ErrChannelModelDetectionRunAlreadyActive):
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		case errors.Is(err, service.ErrChannelModelDetectionManualHighUnconfirmed), errors.Is(err, model.ErrChannelModelDetectionInvalidPreset), errors.Is(err, service.ErrChannelModelDetectionNoEnabledTargets), errors.Is(err, service.ErrChannelModelDetectionDetectorURLMissing):
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		default:
			common.ApiError(c, err)
		}
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "message": "", "data": response})
}

func CancelChannelModelDetectionRun(c *gin.Context) {
	runID := strings.TrimSpace(c.Param("run_id"))
	response, err := service.CancelChannelModelDetectionRun(c.Request.Context(), nil, runID)
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "模型检测轮次不存在"})
		case errors.Is(err, service.ErrChannelModelDetectionWorkerBusy):
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "模型检测轮次正在由其他实例处理，请稍后重试"})
		default:
			common.ApiError(c, err)
		}
		return
	}
	common.ApiSuccess(c, response)
}
