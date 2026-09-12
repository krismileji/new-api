package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func FetchChannelMonitorCustomVariable(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var request struct {
		channelMonitorUpstreamRequest
		RequestID string `json:"request_id"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.Type != service.CustomUpstreamType || request.CustomConfig == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请先配置自定义上游的独立请求"})
		return
	}
	legacy := request.CustomConfig.VariableRequest != nil && len(request.CustomConfig.VariableRequests) == 0
	if legacy {
		request.RequestID = "legacy-variable"
	} else {
		var selected *service.ChannelMonitorCustomVariableRequest
		for _, candidate := range request.CustomConfig.VariableRequests {
			if candidate.ID == request.RequestID {
				copy := candidate
				selected = &copy
				break
			}
		}
		if selected == nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "独立请求不存在，请重新选择"})
			return
		}
		request.CustomConfig.VariableRequests = []service.ChannelMonitorCustomVariableRequest{*selected}
		request.CustomConfig.VariableRequest = nil
	}
	// A draft variable request can be tested before completing metric endpoints.
	ratio, balance := 1.0, 0.0
	request.CustomConfig.Ratio = service.ChannelMonitorCustomMetricConfig{Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &ratio}
	request.CustomConfig.Balance = service.ChannelMonitorCustomMetricConfig{Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &balance}
	request.CustomConfig.BalanceReuseRatioRequest = false
	config, err := resolveChannelMonitorUpstreamRequest(channel, request.channelMonitorUpstreamRequest, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	variables, err := service.FetchChannelMonitorCustomVariables(c.Request.Context(), config, request.RequestID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	data := gin.H{"request_id": request.RequestID, "variables": variables}
	if legacy && len(variables) == 1 {
		data["name"], data["value"] = variables[0].Name, variables[0].Value
	}
	common.ApiSuccess(c, data)
}
