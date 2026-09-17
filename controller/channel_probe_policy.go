package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type channelProbePolicyRequest struct {
	AutoProbeDisabled         *bool   `json:"auto_probe_disabled"`
	SmallInputResponseEnabled *bool   `json:"small_input_response_enabled"`
	SmallInputThresholdTokens *int    `json:"small_input_threshold_tokens"`
	SmallInputResponseText    *string `json:"small_input_response_text"`
	ProbePolicyRevision       *int64  `json:"probe_policy_revision"`
}

func GetChannelMonitorProbePolicy(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的渠道 ID"})
		return
	}
	policy, err := model.GetChannelProbePolicy(c.Request.Context(), channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, policy)
}

func UpdateChannelMonitorProbePolicy(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的渠道 ID"})
		return
	}
	var request channelProbePolicyRequest
	if common.DecodeJson(c.Request.Body, &request) != nil || request.AutoProbeDisabled == nil ||
		request.SmallInputResponseEnabled == nil || request.SmallInputThresholdTokens == nil ||
		request.SmallInputResponseText == nil || request.ProbePolicyRevision == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供完整的探测策略及修订号"})
		return
	}
	policy := model.ChannelProbePolicy{
		AutoProbeDisabled:         *request.AutoProbeDisabled,
		SmallInputResponseEnabled: *request.SmallInputResponseEnabled,
		SmallInputThresholdTokens: *request.SmallInputThresholdTokens,
		SmallInputResponseText:    *request.SmallInputResponseText,
		ProbePolicyRevision:       *request.ProbePolicyRevision,
	}
	if err := policy.Normalize(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	before, after, err := model.SaveChannelProbePolicy(c.Request.Context(), channelID, policy)
	if errors.Is(err, model.ErrChannelProbePolicyConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.probe_policy_update", map[string]any{
		"id": channelID, "before": before, "after": after,
	})
	refreshChannelPassiveConfiguration()
	notifyChannelStatusProbeOverviewChanged()
	common.ApiSuccess(c, after)
}
