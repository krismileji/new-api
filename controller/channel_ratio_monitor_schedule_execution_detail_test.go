package controller

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListChannelMonitorTasksOmitsOversizedStoredResult(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	largeResult := strings.Repeat("x", model.SystemTaskListResultMaxCharacters+1)
	task := model.SystemTask{
		TaskID: "schedule-large-result",
		Type:   channelMonitorSmartScheduleTaskType,
		Status: model.SystemTaskStatusSucceeded,
		Result: largeResult,
	}
	require.NoError(t, db.Create(&task).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/tasks?kind=schedule&p=1&page_size=10", nil,
	)
	ListChannelMonitorTasks(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				TaskID string `json:"task_id"`
				Result any    `json:"result"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, task.TaskID, response.Data.Items[0].TaskID)
	assert.Nil(t, response.Data.Items[0].Result)
	assert.Less(t, len(recorder.Body.Bytes()), model.SystemTaskListResultMaxCharacters)
}

func TestListChannelMonitorTasksReturnsScheduleSummaryWithoutExecutionDetails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))

	storedSummary := channelSmartScheduleTaskResult{
		Total: 1, Planned: 1, Updated: 1,
		PerformanceWindowMinutes: 60, StabilityWindowMinutes: 30,
		GroupPolicies: smartScheduleGroupPolicies{{Group: "vip"}},
	}
	resultJSON, err := common.Marshal(storedSummary)
	require.NoError(t, err)
	task := model.SystemTask{
		TaskID: "schedule-detail-task",
		Type:   channelMonitorSmartScheduleTaskType,
		Status: model.SystemTaskStatusSucceeded,
		Result: string(resultJSON),
	}
	require.NoError(t, db.Create(&task).Error)

	finalScore := 0.9134
	adjustment := channelSmartScheduleTaskAdjustment{
		ChannelId:   71,
		ChannelName: "稳定高速渠道",
		Group:       "vip",
		Model:       "model-a",
		Action:      channelSmartScheduleAdjustmentUpdated,
		OldPriority: 80,
		NewPriority: 100,
		OldWeight:   100,
		NewWeight:   1000,
		Score:       &finalScore,
		Reason:      "评分最高，调整为主渠道",
		ScoreDetails: &model.ChannelSmartScheduleScoreDetails{
			Version:    model.ChannelSmartScheduleScoreDetailsVersion,
			Strategy:   channelMonitorSmartScheduleStrategySmart,
			FinalScore: &finalScore,
		},
	}
	require.NoError(t, model.SaveChannelSmartScheduleExecutionDetails(
		task.TaskID,
		[]model.ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload:         adjustment,
		}},
	))

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/tasks?kind=schedule&p=1&page_size=10", nil,
	)
	ListChannelMonitorTasks(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				TaskID string                         `json:"task_id"`
				Result channelSmartScheduleTaskResult `json:"result"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, task.TaskID, response.Data.Items[0].TaskID)
	assert.Empty(t, response.Data.Items[0].Result.Adjustments)
	assert.Equal(t, 1, response.Data.Items[0].Result.GroupPolicyCount)
	assert.NotContains(t, recorder.Body.String(), "评分最高，调整为主渠道")
	assert.NotContains(t, recorder.Body.String(), `"adjustments"`)
	assert.NotContains(t, recorder.Body.String(), `"group_policies"`)

	var persisted model.SystemTask
	require.NoError(t, db.Where("task_id = ?", task.TaskID).First(&persisted).Error)
	assert.NotContains(t, persisted.Result, "score_details")
	assert.NotContains(t, persisted.Result, "adjustments")
}

func TestGetChannelMonitorRatioTaskDetailsReturnsRatioResult(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	previousBalance := 12.5
	threshold := 10.0
	resultJSON, err := common.Marshal(channelRatioMonitorTaskResult{
		Total:          2,
		Updated:        2,
		Changed:        1,
		BalanceUpdated: 1,
		ChangedChannels: []channelRatioMonitorTaskChange{{
			ChannelId: 2, ChannelName: "倍率渠道", OldRatio: 1, NewRatio: 1.2,
		}},
		BalanceUpdates: []channelRatioMonitorTaskBalance{{
			ChannelId: 1, ChannelName: "余额渠道", PreviousBalance: &previousBalance,
			Balance: 8, Warning: true, WarningThreshold: &threshold,
		}},
	})
	require.NoError(t, err)
	task := model.SystemTask{
		TaskID: "ratio-detail-task",
		Type:   model.SystemTaskTypeChannelRatioMonitor,
		Status: model.SystemTaskStatusSucceeded,
		Result: string(resultJSON),
	}
	require.NoError(t, db.Create(&task).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/tasks/ratio-detail-task/ratio-details", nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorRatioTaskDetails(ctx)

	var response struct {
		Success bool                          `json:"success"`
		Data    channelRatioMonitorTaskResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.ChangedChannels, 1)
	assert.Equal(t, 2, response.Data.ChangedChannels[0].ChannelId)
	require.Len(t, response.Data.BalanceUpdates, 1)
	assert.Equal(t, 1, response.Data.BalanceUpdates[0].ChannelId)
	assert.InDelta(t, 8, response.Data.BalanceUpdates[0].Balance, 1e-9)
}

func TestOversizedRatioTaskResultCanStillLoadDetails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	changedChannels := make([]channelRatioMonitorTaskChange, 100)
	balanceUpdates := make([]channelRatioMonitorTaskBalance, 100)
	for index := range changedChannels {
		changedChannels[index] = channelRatioMonitorTaskChange{
			ChannelId:     index + 1,
			ChannelName:   strings.Repeat("channel-", 16),
			ChannelRemark: strings.Repeat("remark-", 36),
			OldRatio:      1,
			NewRatio:      1.2,
			OldCostRatio:  1,
			NewCostRatio:  1.2,
		}
		balanceUpdates[index] = channelRatioMonitorTaskBalance{
			ChannelId:     index + 1,
			ChannelName:   strings.Repeat("channel-", 16),
			ChannelRemark: strings.Repeat("remark-", 36),
			Balance:       float64(index),
		}
	}
	resultJSON, err := common.Marshal(channelRatioMonitorTaskResult{
		Total:           100,
		Updated:         100,
		Changed:         100,
		BalanceUpdated:  100,
		ChangedChannels: changedChannels,
		BalanceUpdates:  balanceUpdates,
	})
	require.NoError(t, err)
	require.Greater(t, len(resultJSON), model.SystemTaskListResultMaxCharacters)
	task := model.SystemTask{
		TaskID: "ratio-oversized-detail-task",
		Type:   model.SystemTaskTypeChannelRatioMonitor,
		Status: model.SystemTaskStatusSucceeded,
		Result: string(resultJSON),
	}
	require.NoError(t, db.Create(&task).Error)

	listContext, listRecorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/tasks?kind=ratio&p=1&page_size=10", nil,
	)
	ListChannelMonitorTasks(listContext)
	var listResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				Result any `json:"result"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	require.Len(t, listResponse.Data.Items, 1)
	assert.Nil(t, listResponse.Data.Items[0].Result)

	detailContext, detailRecorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/tasks/ratio-oversized-detail-task/ratio-details", nil,
	)
	detailContext.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorRatioTaskDetails(detailContext)
	var detailResponse struct {
		Success bool                          `json:"success"`
		Data    channelRatioMonitorTaskResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse))
	require.True(t, detailResponse.Success)
	assert.Len(t, detailResponse.Data.ChangedChannels, 100)
	assert.Len(t, detailResponse.Data.BalanceUpdates, 100)
}

func TestChannelMonitorTaskQueriesNormalizeNegativePages(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))
	task := model.SystemTask{
		TaskID: "schedule-negative-page",
		Type:   channelMonitorSmartScheduleTaskType,
		Status: model.SystemTaskStatusSucceeded,
	}
	require.NoError(t, db.Create(&task).Error)
	require.NoError(t, model.SaveChannelSmartScheduleExecutionDetails(
		task.TaskID,
		[]model.ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleTaskAdjustment{
				ChannelId: 71, ChannelName: "稳定渠道", Group: "vip", Model: "model-a",
				Action: channelSmartScheduleAdjustmentUpdated,
			},
		}},
	))

	listContext, listRecorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/tasks?kind=schedule&p=-1&page_size=-1", nil,
	)
	ListChannelMonitorTasks(listContext)
	var listResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	assert.Equal(t, 1, listResponse.Data.Page)
	assert.Equal(t, common.ItemsPerPage, listResponse.Data.PageSize)

	detailContext, detailRecorder := newChannelMonitorControllerContext(
		t, http.MethodGet,
		"/api/channel_monitor/tasks/schedule-negative-page/details?p=-1&page_size=-1",
		nil,
	)
	detailContext.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorSmartScheduleExecutionDetails(detailContext)
	var detailResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Page     int                                  `json:"page"`
			PageSize int                                  `json:"page_size"`
			Items    []channelSmartScheduleTaskAdjustment `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse))
	require.True(t, detailResponse.Success)
	assert.Equal(t, 1, detailResponse.Data.Page)
	assert.Equal(t, channelMonitorSmartScheduleExecutionDetailPageSize, detailResponse.Data.PageSize)
	require.Len(t, detailResponse.Data.Items, 1)
}

func TestGetChannelMonitorSmartScheduleExecutionDetailsFiltersAndPaginatesOneTask(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))
	task := model.SystemTask{
		TaskID: "schedule-detail-page",
		Type:   channelMonitorSmartScheduleTaskType,
		Status: model.SystemTaskStatusSucceeded,
	}
	require.NoError(t, db.Create(&task).Error)

	adjustments := []channelSmartScheduleTaskAdjustment{
		{
			ChannelId: 71, ChannelName: "高速渠道", Group: "vip", Model: "model-a",
			Action: channelSmartScheduleAdjustmentUpdated, NewPriority: 100, NewWeight: 80,
			Reason: "评分最高，调整为主渠道",
		},
		{
			ChannelId: 72, ChannelName: "标准渠道", Group: "default", Model: "model-b",
			Action: channelSmartScheduleAdjustmentUnchanged, NewPriority: 90, NewWeight: 100,
			Reason: "评分与上一轮一致",
		},
		{
			ChannelId: 73, ChannelName: "备用渠道", Group: "vip", Model: "model-c",
			Action: channelSmartScheduleAdjustmentFailed, OldPriority: 100, OldWeight: 20,
			NewPriority: 120, NewWeight: 100, Reason: "写入路由失败",
		},
	}
	inputs := make([]model.ChannelSmartScheduleExecutionDetailInput, 0, len(adjustments))
	for index, adjustment := range adjustments {
		inputs = append(inputs, model.ChannelSmartScheduleExecutionDetailInput{
			AdjustmentIndex: index,
			Payload:         adjustment,
		})
	}
	require.NoError(t, model.SaveChannelSmartScheduleExecutionDetails(task.TaskID, inputs))

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet,
		"/api/channel_monitor/tasks/schedule-detail-page/details?p=2&page_size=1&group=vip",
		nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorSmartScheduleExecutionDetails(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Page          int                                  `json:"page"`
			PageSize      int                                  `json:"page_size"`
			Total         int                                  `json:"total"`
			Items         []channelSmartScheduleTaskAdjustment `json:"items"`
			Groups        []string                             `json:"groups"`
			Models        []string                             `json:"models"`
			ModelsByGroup map[string][]string                  `json:"models_by_group"`
			ChannelNames  map[string]string                    `json:"channel_names"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Page)
	assert.Equal(t, 1, response.Data.PageSize)
	assert.Equal(t, 2, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 73, response.Data.Items[0].ChannelId)
	assert.Equal(t, []string{"vip"}, response.Data.Groups)
	assert.Equal(t, []string{"model-a", "model-c"}, response.Data.Models)
	assert.Equal(t, map[string][]string{
		"vip": []string{"model-a", "model-c"},
	}, response.Data.ModelsByGroup)
	assert.Equal(t, "备用渠道", response.Data.ChannelNames["73"])
	assert.NotContains(t, response.Data.ChannelNames, "71")

	routingCtx, routingRecorder := newChannelMonitorControllerContext(
		t, http.MethodGet,
		"/api/channel_monitor/tasks/schedule-detail-page/details?p=1&page_size=50",
		nil,
	)
	routingCtx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorSmartScheduleExecutionDetails(routingCtx)
	require.NoError(t, common.Unmarshal(routingRecorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 2)
	assert.Equal(t, []int{71, 73}, []int{
		response.Data.Items[0].ChannelId,
		response.Data.Items[1].ChannelId,
	})

	searchCtx, searchRecorder := newChannelMonitorControllerContext(
		t, http.MethodGet,
		"/api/channel_monitor/tasks/schedule-detail-page/details?p=1&page_size=50&q=model-a&action=updated",
		nil,
	)
	searchCtx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorSmartScheduleExecutionDetails(searchCtx)
	require.NoError(t, common.Unmarshal(searchRecorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 1, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	assert.Equal(t, 71, response.Data.Items[0].ChannelId)

	explicitUnchangedCtx, explicitUnchangedRecorder := newChannelMonitorControllerContext(
		t, http.MethodGet,
		"/api/channel_monitor/tasks/schedule-detail-page/details?p=1&page_size=50&action=unchanged",
		nil,
	)
	explicitUnchangedCtx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorSmartScheduleExecutionDetails(explicitUnchangedCtx)
	require.NoError(t, common.Unmarshal(explicitUnchangedRecorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 0, response.Data.Total)
	assert.Empty(t, response.Data.Items)
}
