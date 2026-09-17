package controller

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAutomationDraftPreservesStateAndMasksCredentials(t *testing.T) {
	setupChannelMonitorCustomActionRefreshDB(t, "sqlite")
	disableChannelMonitorSSRFProtection(t)
	var actions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			assert.Equal(t, "login-password", r.Header.Get("X-Password"))
			_, _ = w.Write([]byte(`{"token":"draft-token"}`))
		case "/reset":
			actions.Add(1)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			assert.Equal(t, "Bearer draft-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"balance":0}`))
		}
	}))
	defer server.Close()
	input := automationTestConfig(server.URL)
	input.CustomConfig.VariableRequests = automationTestVariables()
	input.CustomConfig.Balance.Request.Headers = []service.ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{token}}"}}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/automations", input)
	SaveUpstreamAutomation(ctx)
	var response struct {
		Success bool                           `json:"success"`
		Data    service.UpstreamAutomationView `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "login-password")
	view := response.Data
	before, err := model.GetUpstreamAutomation(t.Context(), view.ID)
	require.NoError(t, err)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/automations/test", view.UpstreamAutomationConfig)
	TestUpstreamAutomation(ctx)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.NotContains(t, recorder.Body.String(), "draft-token")
	assert.Zero(t, actions.Load(), "测试只获取指标，不执行触发接口")
	draft := struct {
		service.UpstreamAutomationConfig
		RequestID string `json:"request_id"`
	}{view.UpstreamAutomationConfig, "login"}
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/automations/variable/fetch", draft)
	FetchUpstreamAutomationDraftVariables(ctx)
	assert.Contains(t, recorder.Body.String(), "draft-token")
	assert.NotContains(t, recorder.Body.String(), "login-password")
	after, err := model.GetUpstreamAutomation(t.Context(), view.ID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "草稿指标和凭据测试都不能改写持久化状态")
	tasks, err := model.ListSystemTasks(100)
	require.NoError(t, err)
	assert.Empty(t, tasks, "通用任务列表不能暴露自动任务配置")
	assert.Nil(t, before.ToResponse().Payload)
	assert.Nil(t, before.ToResponse().State)

	changedOrigin := view.UpstreamAutomationConfig
	changedOrigin.BaseURL = "https://changed.example"
	_, err = service.PrepareUpstreamAutomationDraft(t.Context(), changedOrigin)
	require.Error(t, err, "更换上游地址不能复用隐藏密码")
	view.Enabled = false
	_, err = service.SaveUpstreamAutomation(t.Context(), view.UpstreamAutomationConfig)
	require.NoError(t, err)
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/automations", view.UpstreamAutomationConfig)
	SaveUpstreamAutomation(ctx)
	assert.Contains(t, recorder.Body.String(), "配置已变化")
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/automations/"+view.ID+"/run", nil)
	ctx.Params = gin.Params{{Key: "id", Value: view.ID}}
	RunUpstreamAutomationNow(ctx)
	assert.Contains(t, recorder.Body.String(), "请先启用并保存任务")
}

func TestUpstreamAutomationMigrationFailureDoesNotBlockIndependentSchedule(t *testing.T) {
	db := setupChannelMonitorCustomActionRefreshDB(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&model.SystemTaskLock{}))
	useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoUpdateIntervalOption: "0"})
	config := automationTestConfig("https://upstream.example")
	config.CustomConfig.Actions[0].Enabled = false
	view, err := service.SaveUpstreamAutomation(t.Context(), config)
	require.NoError(t, err)
	config.CustomConfig.Actions[0].Enabled = true
	raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Channel{Id: 21, Name: "暂停的旧账户"}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 21, UpstreamType: "custom", UpstreamBaseURL: config.BaseURL, CustomUpstreamConfig: raw}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 22, UpstreamType: "custom", CustomUpstreamConfig: "invalid"}).Error)
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/automations", nil)
	ListUpstreamAutomations(ctx)
	var listed struct {
		Success bool                             `json:"success"`
		Data    []service.UpstreamAutomationView `json:"data"`
		Warning string                           `json:"migration_warning"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &listed))
	require.True(t, listed.Success)
	require.Len(t, listed.Data, 2)
	assert.Contains(t, listed.Warning, "渠道 22")
	assert.False(t, listed.Data[1].Enabled, "迁移保留原先关闭自动更新的意图")
	assert.True(t, (upstreamAutomationTaskHandler{}).Enabled(), "独立任务不依赖监控总开关")
	task := model.SystemTask{TaskID: "dispatch", Type: upstreamAutomationTaskType, Status: model.SystemTaskStatusRunning, LockedBy: "test-runner", Payload: "{}"}
	require.NoError(t, db.Create(&task).Error)
	require.NoError(t, db.Create(&model.SystemTaskLock{Type: task.Type, TaskID: task.TaskID, LockedBy: task.LockedBy, LockedUntil: time.Now().Unix() + 60}).Error)
	(upstreamAutomationTaskHandler{}).Run(t.Context(), &task, task.LockedBy)
	row, err := model.GetUpstreamAutomation(t.Context(), view.ID)
	require.NoError(t, err)
	view, err = service.UpstreamAutomationResponse(row)
	require.NoError(t, err)
	assert.Positive(t, view.State.LastCheck, "单个迁移故障不能阻塞已有任务")
	assert.Equal(t, "checked", view.State.Status)
}
