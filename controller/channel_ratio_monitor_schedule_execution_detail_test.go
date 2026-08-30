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
	assert.Equal(t, []string{"default", "vip"}, response.Data.Groups)
	assert.Equal(t, []string{"model-a", "model-b", "model-c"}, response.Data.Models)
	assert.Equal(t, map[string][]string{
		"default": {"model-b"},
		"vip":     {"model-a", "model-c"},
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
	require.Len(t, response.Data.Items, 3)
	assert.Equal(t, []int{71, 73, 72}, []int{
		response.Data.Items[0].ChannelId,
		response.Data.Items[1].ChannelId,
		response.Data.Items[2].ChannelId,
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
}
