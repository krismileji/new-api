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

type channelModelDetectionTargetUpdateRequest struct {
	TargetKey    string `json:"target_key"`
	RequestModel string `json:"request_model"`
	ClaimedModel string `json:"claimed_model"`
}

type channelModelDetectionConfigUpdateRequest struct {
	ScheduleEnabled bool                                       `json:"schedule_enabled"`
	Targets         []channelModelDetectionTargetUpdateRequest `json:"targets"`
	Revision        int64                                      `json:"revision"`
}

type channelModelDetectionEstimateRequest struct {
	Preset string `json:"preset"`
}

func UpdateChannelModelDetectionConfig(c *gin.Context) {
	channelID, ok := channelModelDetectionPathID(c)
	if !ok {
		return
	}
	var request channelModelDetectionConfigUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型检测渠道配置请求格式无效"})
		return
	}
	targets := make([]service.ChannelModelDetectionTargetUpdateInput, 0, len(request.Targets))
	for _, target := range request.Targets {
		targets = append(targets, service.ChannelModelDetectionTargetUpdateInput{TargetKey: target.TargetKey, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel})
	}
	response, err := service.UpdateChannelModelDetectionConfig(c.Request.Context(), nil, channelID, service.ChannelModelDetectionConfigUpdateInput{
		ScheduleEnabled: request.ScheduleEnabled, Targets: targets, ExpectedRevision: request.Revision,
	}, time.Now().UTC())
	if err != nil {
		channelModelDetectionWriteError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func EstimateChannelModelDetectionCost(c *gin.Context) {
	channelID, ok := channelModelDetectionPathID(c)
	if !ok {
		return
	}
	var request channelModelDetectionEstimateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "模型检测估算请求格式无效"})
		return
	}
	response, err := service.EstimateChannelModelDetectionCost(c.Request.Context(), nil, channelID, request.Preset)
	if err != nil {
		channelModelDetectionWriteError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func channelModelDetectionPathID(c *gin.Context) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || value <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道 ID 必须为正整数"})
		return 0, false
	}
	return value, true
}

func channelModelDetectionWriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrChannelModelDetectionRevisionConflict):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error(), "code": "revision_conflict"})
	case errors.Is(err, service.ErrChannelModelDetectionChannelNotFound), errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, service.ErrChannelModelDetectionConfigNotFound), errors.Is(err, service.ErrChannelModelDetectionTargetNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
	case errors.Is(err, service.ErrChannelModelDetectionDetectorNotConfigured), errors.Is(err, model.ErrChannelModelDetectionInvalidPreset), errors.Is(err, model.ErrChannelModelDetectionInvalidClaimedModel), errors.Is(err, service.ErrChannelModelDetectionEstimateInvalid), errors.Is(err, service.ErrChannelModelDetectionInvalidConfig):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
	default:
		// Keep the established API envelope for unexpected infrastructure errors.
		common.ApiError(c, err)
	}
}
