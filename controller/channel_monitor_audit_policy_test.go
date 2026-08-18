package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRatioAuditSkipsTaskStartsAndUnchangedValues(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "root"}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: 101, Name: "audit-ratio", Status: common.ChannelStatusEnabled,
	}).Error)

	runContext, runRecorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel_monitor/ratio/run", nil,
	)
	RunChannelMonitorRatioUpdate(runContext)
	require.Equal(t, http.StatusOK, runRecorder.Code, runRecorder.Body.String())

	updateContext, updateRecorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/channel/101", map[string]any{
			"ratio":  1.5,
			"remark": "首次设置",
		},
	)
	updateContext.Params = append(updateContext.Params, gin.Param{Key: "id", Value: "101"})
	UpdateChannelMonitorRatio(updateContext)
	require.Equal(t, http.StatusOK, updateRecorder.Code, updateRecorder.Body.String())

	unchangedContext, unchangedRecorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/channel/101", map[string]any{
			"ratio":  1.5,
			"remark": "首次设置",
		},
	)
	unchangedContext.Params = append(unchangedContext.Params, gin.Param{Key: "id", Value: "101"})
	UpdateChannelMonitorRatio(unchangedContext)
	require.Equal(t, http.StatusOK, unchangedRecorder.Code, unchangedRecorder.Body.String())

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeManage).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, "已将渠道 audit-ratio（ID: 101）的倍率更新为 1.5", logs[0].Content)
	assert.Contains(t, logs[0].Other, `"action":"channel.monitor_ratio_update"`)
	assert.Contains(t, logs[0].Other, `"channel_name":"audit-ratio"`)
}

func TestChannelStatusAuditOnlyRecordsActualChangesWithReadableStatus(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "root"}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: 102, Name: "audit-status", Status: common.ChannelStatusEnabled,
	}).Error)

	unchangedContext, unchangedRecorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel/102/status", map[string]any{"status": common.ChannelStatusEnabled},
	)
	unchangedContext.Params = append(unchangedContext.Params, gin.Param{Key: "id", Value: "102"})
	UpdateChannelStatus(unchangedContext)
	require.Equal(t, http.StatusOK, unchangedRecorder.Code, unchangedRecorder.Body.String())

	changedContext, changedRecorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel/102/status", map[string]any{"status": common.ChannelStatusManuallyDisabled},
	)
	changedContext.Params = append(changedContext.Params, gin.Param{Key: "id", Value: "102"})
	UpdateChannelStatus(changedContext)
	require.Equal(t, http.StatusOK, changedRecorder.Code, changedRecorder.Body.String())

	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeManage).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, "已禁用渠道 audit-status（ID: 102）", logs[0].Content)
	assert.Contains(t, logs[0].Other, `"channel_name":"audit-status"`)
}
