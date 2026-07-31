package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListChannelMonitorTasksHydratesPersistedScheduleExecutionDetails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))

	storedSummary := channelSmartScheduleTaskResult{
		Total: 1, Planned: 1, Updated: 1, PerformanceMinutes: 60,
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
	require.Len(t, response.Data.Items[0].Result.Adjustments, 1)
	loaded := response.Data.Items[0].Result.Adjustments[0]
	assert.Equal(t, adjustment.ChannelId, loaded.ChannelId)
	assert.Equal(t, adjustment.Reason, loaded.Reason)
	require.NotNil(t, loaded.ScoreDetails)
	assert.InDelta(t, finalScore, *loaded.ScoreDetails.FinalScore, 1e-9)

	var persisted model.SystemTask
	require.NoError(t, db.Where("task_id = ?", task.TaskID).First(&persisted).Error)
	assert.NotContains(t, persisted.Result, "score_details")
	assert.NotContains(t, persisted.Result, "adjustments")
}
