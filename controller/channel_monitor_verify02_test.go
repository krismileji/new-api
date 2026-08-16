package controller

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerify02ColdStartSingleScheduleSnapshotAndDetails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelSmartScheduleExecutionDetail{}))
	useChannelSmartScheduleGroupRatio(t, `{"vip":100}`)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 1, 90, 30,
			),
		),
	})

	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 3201, Name: "verify02-low-cost", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 3202, Name: "verify02-high-cost", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 3201, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 3202, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 3201, Ratio: 1, UpdatedTime: 1},
		{ChannelId: 3202, Ratio: 2, UpdatedTime: 1},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 3201, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 3202, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	task, err := model.CreateSystemTask(
		channelMonitorSmartScheduleTaskType,
		newChannelSmartScheduleTaskPayload("verify02_cold_start", "verify02_acceptance"),
		nil,
	)
	require.NoError(t, err)
	const runnerID = "verify02-cold-start-runner"
	claimedTask, claimed, err := model.ClaimSystemTask(
		task.ID,
		channelMonitorSmartScheduleTaskType,
		runnerID,
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)

	channelSmartScheduleTaskHandler{}.Run(context.Background(), claimedTask, runnerID)

	storedTask, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, storedTask)
	assert.Equal(t, model.SystemTaskStatusSucceeded, storedTask.Status)
	assert.NotContains(t, storedTask.Result, `"adjustments"`)

	var snapshot model.ChannelSmartScheduleExecutionDetail
	require.NoError(t, db.Where("task_id = ?", task.TaskID).First(&snapshot).Error)
	assert.Equal(t, 2, snapshot.ItemCount)
	require.GreaterOrEqual(t, len(snapshot.PayloadBlob), 2)
	assert.Equal(t, []byte{0x1f, 0x8b}, snapshot.PayloadBlob[:2])

	detailsByTask, err := model.GetChannelSmartScheduleExecutionDetails([]string{task.TaskID})
	require.NoError(t, err)
	require.Len(t, detailsByTask[task.TaskID], 2)
	for index, detail := range detailsByTask[task.TaskID] {
		assert.Equal(t, index, detail.AdjustmentIndex)
		var adjustment channelSmartScheduleTaskAdjustment
		require.NoError(t, common.UnmarshalJsonStr(detail.Payload, &adjustment))
		assert.Equal(t, "vip", adjustment.Group)
		assert.Equal(t, "model-a", adjustment.Model)
		assert.NotNil(t, adjustment.ScoreDetails)
	}

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet,
		"/api/channel_monitor/tasks/"+task.TaskID+"/details?p=1&page_size=1&group=vip",
		nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	GetChannelMonitorSmartScheduleExecutionDetails(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Page     int                                  `json:"page"`
			PageSize int                                  `json:"page_size"`
			Total    int                                  `json:"total"`
			Items    []channelSmartScheduleTaskAdjustment `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 1, response.Data.Page)
	assert.Equal(t, 1, response.Data.PageSize)
	assert.Equal(t, 2, response.Data.Total)
	require.Len(t, response.Data.Items, 1)
	assert.NotNil(t, response.Data.Items[0].ScoreDetails)
}
