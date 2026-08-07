package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSmartScheduleMutationTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{"model-a", "model-b"}, 5, 80, 30,
			),
		),
	})
	running, err := model.CreateSystemTask(channelMonitorSmartScheduleTaskType, nil, nil)
	require.NoError(t, err)
	_, claimed, err := model.ClaimSystemTask(
		running.ID,
		channelMonitorSmartScheduleTaskType,
		"channel-mutation-runner",
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	return db
}

func requireChannelMutationQueuedScheduleSuccessor(t *testing.T, db *gorm.DB) {
	t.Helper()
	var tasks []model.SystemTask
	require.NoError(t, db.Where("type = ?", channelMonitorSmartScheduleTaskType).Order("id ASC").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	assert.Equal(t, model.SystemTaskStatusRunning, tasks[0].Status)
	assert.Equal(t, model.SystemTaskStatusPending, tasks[1].Status)
}

func TestUpdateChannelStatusQueuesSmartScheduleSuccessor(t *testing.T) {
	db := setupChannelSmartScheduleMutationTest(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 2401, Name: "status mutation", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel/2401/status",
		map[string]any{"status": common.ChannelStatusManuallyDisabled},
	)
	ctx.AddParam("id", "2401")
	UpdateChannelStatus(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	requireChannelMutationQueuedScheduleSuccessor(t, db)
}

func TestUpdateChannelRoutingValuesQueuesSmartScheduleSuccessor(t *testing.T) {
	db := setupChannelSmartScheduleMutationTest(t)
	priority := int64(80)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 2402, Name: "routing mutation", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel/2402",
		map[string]any{"id": 2402, "priority": 81},
	)
	UpdateChannel(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	requireChannelMutationQueuedScheduleSuccessor(t, db)
}

func TestEditTagChannelRoutesQueuesSmartScheduleSuccessor(t *testing.T) {
	db := setupChannelSmartScheduleMutationTest(t)
	tag := "batch-routing"
	require.NoError(t, db.Create(&model.Channel{
		Id: 2403, Name: "tag mutation", Tag: &tag, Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel/tag",
		map[string]any{"tag": tag, "models": "model-b"},
	)
	EditTagChannels(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	requireChannelMutationQueuedScheduleSuccessor(t, db)
}

func TestDeleteChannelQueuesSmartScheduleSuccessor(t *testing.T) {
	db := setupChannelSmartScheduleMutationTest(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 2404, Name: "delete mutation", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodDelete, "/api/channel/2404", nil,
	)
	ctx.AddParam("id", "2404")
	DeleteChannel(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	requireChannelMutationQueuedScheduleSuccessor(t, db)
}

func TestFixChannelAbilitiesQueuesSmartScheduleSuccessor(t *testing.T) {
	db := setupChannelSmartScheduleMutationTest(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 2405, Name: "ability repair", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel/fix", nil,
	)
	FixChannelsAbilities(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	requireChannelMutationQueuedScheduleSuccessor(t, db)
}

func TestApplyChannelUpstreamModelsQueuesSmartScheduleSuccessor(t *testing.T) {
	db := setupChannelSmartScheduleMutationTest(t)
	channel := model.Channel{
		Id: 2406, Name: "upstream model mutation", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		UpstreamModelUpdateCheckEnabled:       true,
		UpstreamModelUpdateLastDetectedModels: []string{"model-b"},
	})
	require.NoError(t, channel.Insert())

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel/upstream_updates/apply",
		map[string]any{"id": 2406, "add_models": []string{"model-b"}},
	)
	ApplyChannelUpstreamModelUpdates(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	updated, err := model.GetChannelById(2406, true)
	require.NoError(t, err)
	assert.Equal(t, "model-a,model-b", updated.Models)
	requireChannelMutationQueuedScheduleSuccessor(t, db)
}
