package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type channelModelDetectionSettingsUpdateRequest struct {
	DetectorURL      *string `json:"detector_url"`
	ClearDetectorURL bool    `json:"clear_detector_url"`
	ScheduledPreset  string  `json:"scheduled_preset"`
	ConfirmHighCost  bool    `json:"confirm_high_cost"`
	ScheduleEnabled  bool    `json:"schedule_enabled"`
	IntervalHours    int     `json:"interval_hours"`
	ScheduleTime     string  `json:"schedule_time"`
	Timezone         string  `json:"timezone"`
	Revision         int64   `json:"revision"`
}

func GetChannelModelDetectionSettings(c *gin.Context) {
	response, err := service.GetChannelModelDetectionSettings(c.Request.Context(), nil, time.Now().UTC())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func UpdateChannelModelDetectionSettings(c *gin.Context) {
	var request channelModelDetectionSettingsUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型检测统一设置请求格式无效"})
		return
	}
	response, err := service.UpdateChannelModelDetectionSettings(c.Request.Context(), nil, service.ChannelModelDetectionSettingsUpdate{
		DetectorURL: request.DetectorURL, ClearDetectorURL: request.ClearDetectorURL,
		ScheduledPreset: request.ScheduledPreset, ConfirmHighCost: request.ConfirmHighCost,
		ScheduleEnabled: request.ScheduleEnabled, IntervalHours: request.IntervalHours,
		ScheduleTime: request.ScheduleTime, Timezone: request.Timezone, ExpectedRevision: request.Revision,
	}, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, service.ErrChannelModelDetectionSettingsConflict):
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error(), "code": "revision_conflict"})
		case errors.Is(err, model.ErrChannelModelDetectionScheduledHighUnconfirmed):
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error(), "code": "high_cost_confirmation_required"})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		}
		return
	}
	common.ApiSuccess(c, response)
}

func GetChannelModelDetectionService(c *gin.Context) {
	response, err := service.GetChannelModelDetectionService(c.Request.Context(), nil, time.Now().UTC())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func TestChannelModelDetectionService(c *gin.Context) {
	response, err := service.TestChannelModelDetectionService(c.Request.Context(), nil, time.Now().UTC())
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, service.ErrChannelModelDetectionDetectorNotConfigured) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error(), "data": response})
		return
	}
	common.ApiSuccess(c, response)
}
