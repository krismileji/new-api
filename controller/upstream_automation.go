package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func ListUpstreamAutomations(c *gin.Context) {
	migrationErr := service.MigrateUpstreamAutomations(c.Request.Context(), getChannelMonitorSettings().AutoUpdateIntervalMinutes)
	rows, err := model.ListUpstreamAutomations(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	views := make([]service.UpstreamAutomationView, 0, len(rows))
	for _, row := range rows {
		view, err := service.UpstreamAutomationResponse(row)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		views = append(views, view)
	}
	warning := ""
	if migrationErr != nil {
		warning = migrationErr.Error()
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": views, "migration_warning": warning})
}

func MergeUpstreamAccountAutomations(c *gin.Context) {
	var input service.UpstreamAccountAutomationMergeRequest
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &input); err != nil {
		common.ApiErrorMsg(c, "任务合并参数无效")
		return
	}
	preview, err := service.MergeUpstreamAccountAutomations(c.Request.Context(), input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !input.Preview {
		recordManageAudit(c, "channel.upstream_account_automations_merge", map[string]any{"account_id": input.AccountID, "target_id": input.TargetID})
	}
	common.ApiSuccess(c, preview)
}

func SaveUpstreamAutomation(c *gin.Context) {
	var input service.UpstreamAutomationConfig
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &input); err != nil {
		common.ApiErrorMsg(c, upstreamAutomationConfigDecodeMessage(err))
		return
	}
	view, err := service.SaveUpstreamAutomation(c.Request.Context(), input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "upstream.automation_save", map[string]any{"id": view.ID, "name": view.Name, "enabled": view.Enabled})
	common.ApiSuccess(c, view)
}

func DeleteUpstreamAutomation(c *gin.Context) {
	revision, err := strconv.ParseInt(c.Query("revision"), 10, 64)
	if err != nil || revision <= 0 {
		common.ApiErrorMsg(c, "任务修订号无效")
		return
	}
	if err := model.DeleteUpstreamAutomation(c.Request.Context(), c.Param("id"), revision); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "upstream.automation_delete", map[string]any{"id": c.Param("id")})
	common.ApiSuccess(c, nil)
}

func RunUpstreamAutomationNow(c *gin.Context) {
	row, err := model.GetUpstreamAutomation(c.Request.Context(), c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	view, err := service.UpstreamAutomationResponse(row)
	if err != nil || !view.Enabled {
		common.ApiErrorMsg(c, "请先启用并保存任务")
		return
	}
	task, _, err := service.EnqueueRequiredSystemTask(upstreamAutomationTaskType, upstreamAutomationTaskPayload{IDs: []string{row.TaskID}})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "upstream.automation_check", map[string]any{"id": row.TaskID})
	common.ApiSuccess(c, gin.H{"task_id": task.TaskID})
}

func AcknowledgeUpstreamAutomationAction(c *gin.Context) {
	var input struct {
		Revision  int64  `json:"revision"`
		AttemptID string `json:"attempt_id"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4096), &input); err != nil {
		common.ApiErrorMsg(c, "执行记录无效")
		return
	}
	if err := service.AcknowledgeUpstreamAutomationAction(c.Request.Context(), c.Param("id"), c.Param("action_id"), input.AttemptID, input.Revision); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "upstream.automation_acknowledge", map[string]any{"id": c.Param("id"), "action_id": c.Param("action_id"), "attempt_id": input.AttemptID})
	common.ApiSuccess(c, nil)
}

func ResetUpstreamAutomationAttempts(c *gin.Context) {
	var input struct {
		Revision int64 `json:"revision"`
		service.ChannelMonitorCustomActionResetRequest
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 4096), &input); err != nil {
		common.ApiErrorMsg(c, "调用记录无效")
		return
	}
	if err := service.ResetUpstreamAutomationAttempts(c.Request.Context(), c.Param("id"), c.Param("action_id"), input.Revision, input.ChannelMonitorCustomActionResetRequest); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "upstream.automation_reset_count", map[string]any{"id": c.Param("id"), "action_id": c.Param("action_id"), "previous_attempts": input.Attempts})
	common.ApiSuccess(c, nil)
}

func TestUpstreamAutomation(c *gin.Context) {
	var input service.UpstreamAutomationConfig
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &input); err != nil {
		common.ApiErrorMsg(c, upstreamAutomationConfigDecodeMessage(err))
		return
	}
	config, err := service.PrepareUpstreamAutomationDraft(c.Request.Context(), input)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	request := service.ChannelMonitorUpstreamConfig{AccountID: config.AccountID, AccountRevision: config.AccountRevision, Type: service.CustomUpstreamType, BaseURL: config.BaseURL, Proxy: config.Proxy, RequestTimeout: time.Duration(config.RequestTimeout) * time.Second, CustomConfig: config.CustomConfig}
	if config.RatioChannelID > 0 {
		monitor, readErr := model.GetChannelRatioMonitor(config.RatioChannelID)
		if readErr != nil {
			common.ApiError(c, readErr)
			return
		}
		request.Group = monitor.UpstreamGroup
		channel, readErr := model.GetChannelById(config.RatioChannelID, true)
		if readErr != nil {
			common.ApiError(c, readErr)
			return
		}
		request.ChannelKeys = channel.GetKeys()
	}
	if config.AccountID > 0 && config.RatioChannelID == 0 {
		balance, err := service.FetchChannelMonitorUpstreamBalance(c.Request.Context(), request)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, service.NewAPIGroupRatioResult{Balance: balance})
		return
	}
	result, err := service.FetchChannelMonitorUpstreamGroupRatio(c.Request.Context(), request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func FetchUpstreamAutomationDraftVariables(c *gin.Context) {
	var input struct {
		service.UpstreamAutomationConfig
		RequestID string `json:"request_id"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10), &input); err != nil {
		common.ApiErrorMsg(c, upstreamAutomationConfigDecodeMessage(err))
		return
	}
	variables, err := service.FetchUpstreamAutomationDraftVariables(c.Request.Context(), input.UpstreamAutomationConfig, input.RequestID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, variables)
}
