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
