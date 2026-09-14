package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ListChannelMonitorVariableGroups(c *gin.Context) {
	groups, err := model.ListChannelMonitorVariableGroups(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result := make([]service.ChannelMonitorVariableGroupConfig, 0, len(groups))
	for _, group := range groups {
		view, err := service.ChannelMonitorVariableGroupView(group)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		result = append(result, view)
	}
	common.ApiSuccess(c, result)
}

func SaveChannelMonitorVariableGroup(c *gin.Context) {
	var request service.ChannelMonitorVariableGroupConfig
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "共享请求与变量配置无效或过大"})
		return
	}
	result, err := service.SaveChannelMonitorVariableGroup(c.Request.Context(), request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, result)
}

func DeleteChannelMonitorVariableGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	revision, revisionErr := strconv.ParseInt(c.Query("revision"), 10, 64)
	if err != nil || revisionErr != nil || id <= 0 || revision <= 0 {
		common.ApiErrorMsg(c, "共享配置标识或修订号无效")
		return
	}
	if err := model.DeleteChannelMonitorVariableGroup(c.Request.Context(), id, revision); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func FetchChannelMonitorVariableGroupDraft(c *gin.Context) {
	var request struct {
		service.ChannelMonitorVariableGroupConfig
		RequestID string `json:"request_id"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &request); err != nil {
		common.ApiErrorMsg(c, "共享请求配置无效或过大")
		return
	}
	variables, err := service.FetchChannelMonitorVariableGroupDraft(c.Request.Context(), request.ChannelMonitorVariableGroupConfig, request.RequestID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"request_id": request.RequestID, "variables": variables})
}
