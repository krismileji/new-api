package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayRetryRoutingExcludesFailedChannelsImmediately(t *testing.T) {
	routing := newRelayRetryRouting()

	routing.exclude(26)
	routing.exclude(7)

	options, ok := routing.selectionOptions()
	require.True(t, ok)
	assert.Equal(t, []int{26, 7}, options.ExcludedChannelIds)
	assert.False(t, routing.candidatesExhausted())
}

func TestRelayRetryRoutingRetriesPinnedChannelOnceWithoutExcludingIt(t *testing.T) {
	routing := newRelayRetryRouting()
	channel := &model.Channel{Id: 26, Name: "same-channel"}

	routing.retrySameChannel(channel, "vip")
	options, hasExcludedChannels := routing.selectionOptions()
	assert.False(t, hasExcludedChannels)
	assert.Empty(t, options.ExcludedChannelIds)

	selected, group, err := routing.selectChannel(&service.RetryParam{})
	require.NoError(t, err)
	assert.Same(t, channel, selected)
	assert.Equal(t, "vip", group)
	assert.Nil(t, routing.sameChannel)

	routing.retrySameChannel(channel, "vip")
	routing.exclude(channel.Id)
	assert.Nil(t, routing.sameChannel)
	options, hasExcludedChannels = routing.selectionOptions()
	require.True(t, hasExcludedChannels)
	assert.Equal(t, []int{channel.Id}, options.ExcludedChannelIds)
}

func TestRelayRetryRoutingRestartsRoundsUntilRetryBudgetIsUsed(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	channelIDs := []int{26, 7, 8, 9, 10}
	priorities := []int64{500, 400, 300, 200, 100}
	weight := uint(10)
	channels := make([]model.Channel, 0, len(channelIDs))
	abilities := make([]model.Ability, 0, len(channelIDs))
	for i, channelID := range channelIDs {
		channels = append(channels, model.Channel{
			Id: channelID, Name: "retry-channel", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: common.GetPointer(priorities[i]), Weight: &weight,
		})
		abilities = append(abilities, model.Ability{
			Group: "vip", Model: "model-a", ChannelId: channelID, Enabled: true,
			Priority: common.GetPointer(priorities[i]), Weight: weight,
		})
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&abilities).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	currentChannelID := channelIDs[0]
	attemptedChannelIDs := []int{currentChannelID}
	for retry := 1; retry <= 10; retry++ {
		routing.exclude(currentChannelID)
		retryParam.SetRetry(retry)
		channel, group, err := routing.selectChannel(retryParam)
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, "vip", group)
		assert.False(t, routing.candidatesExhausted())
		currentChannelID = channel.Id
		attemptedChannelIDs = append(attemptedChannelIDs, currentChannelID)
	}

	assert.Equal(t, []int{26, 7, 8, 9, 10, 26, 7, 8, 9, 10, 26}, attemptedChannelIDs)
}

func TestRelayRetryRoutingTriesSamePriorityChannelsWithoutReplacement(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	channelIDs := []int{31, 32, 33}
	priority := int64(100)
	weights := []uint{1, 3, 6}
	channels := make([]model.Channel, 0, len(channelIDs))
	abilities := make([]model.Ability, 0, len(channelIDs))
	for index, channelID := range channelIDs {
		weight := weights[index]
		channels = append(channels, model.Channel{
			Id: channelID, Name: "same-priority", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
		})
		abilities = append(abilities, model.Ability{
			Group: "vip", Model: "model-a", ChannelId: channelID, Enabled: true,
			Priority: &priority, Weight: weight,
		})
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&abilities).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	attempted := make(map[int]struct{}, len(channelIDs))
	for range channelIDs {
		selected, group, err := routing.selectChannel(retryParam)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, "vip", group)
		_, repeated := attempted[selected.Id]
		assert.False(t, repeated)
		attempted[selected.Id] = struct{}{}
		routing.exclude(selected.Id)
		retryParam.IncreaseRetry()
	}
	assert.Len(t, attempted, len(channelIDs))

	selected, group, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, "vip", group)
	assert.Contains(t, attempted, selected.Id)
}

func TestRelayRetryRoutingRepeatsTheOnlyAvailableChannel(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 27, Name: "only-channel", Key: "key", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 27, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	routing := newRelayRetryRouting()
	current := &model.Channel{Id: 27}
	attempted := []int{current.Id}
	for retry := 1; retry <= 3; retry++ {
		routing.exclude(current.Id)
		retryParam.SetRetry(retry)
		selected, group, err := routing.selectChannel(retryParam)
		require.NoError(t, err)
		require.NotNil(t, selected)
		assert.Equal(t, "vip", group)
		assert.Equal(t, 27, selected.Id)
		attempted = append(attempted, selected.Id)
		current = selected
	}
	assert.Equal(t, []int{27, 27, 27, 27}, attempted)
}

func TestRelayRetryRoutingStopsWhenAllCandidatesBecomeUnavailable(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 28, Name: "removed-channel", Key: "key", Group: "vip", Models: "model-a",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 28, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(1),
	}
	routing := newRelayRetryRouting()
	routing.exclude(28)
	model.CacheUpdateChannelStatus(28, common.ChannelStatusAutoDisabled)

	selected, group, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	assert.Nil(t, selected)
	assert.Equal(t, "vip", group)
	assert.True(t, routing.candidatesExhausted())
}

func TestRelayRetryRoutingReleasesLimitedSpecialRouteAfterPreferredCandidates(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priorityExploration := int64(500)
	priorityStable := int64(400)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 29, Name: "exploration", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityExploration, Weight: &weight,
		},
		{
			Id: 30, Name: "stable", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priorityStable, Weight: &weight,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{
			Group: "vip", Model: "model-a", ChannelId: 29, Enabled: true,
			Priority: &priorityExploration, Weight: weight,
		},
		{
			Group: "vip", Model: "model-a", ChannelId: 30, Enabled: true,
			Priority: &priorityStable, Weight: weight,
		},
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 29, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		TemporaryTrafficKind:       model.ChannelSmartScheduleTemporaryTrafficExploration,
		ExplorationMaxPromptTokens: 100,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:              ctx,
		TokenGroup:       "vip",
		ModelName:        "model-a",
		RequestPath:      ctx.Request.URL.Path,
		Retry:            common.GetPointer(0),
		SelectionOptions: model.ChannelSelectionOptions{EstimatedPromptTokens: 101},
	}
	routing := newRelayRetryRouting()
	selected, _, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 30, selected.Id)

	routing.exclude(selected.Id)
	retryParam.IncreaseRetry()
	selected, _, err = routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 29, selected.Id)
}
