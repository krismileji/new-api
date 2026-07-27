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

func TestRelayRetryRoutingSelectsAlternativeThenStopsAfterExhaustion(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create([]model.Channel{
		{
			Id: 26, Name: "failed", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
		},
		{
			Id: 7, Name: "alternative", Key: "key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
		},
	}).Error)
	require.NoError(t, db.Create([]model.Ability{
		{
			Group: "vip", Model: "model-a", ChannelId: 26, Enabled: true,
			Priority: &priority, Weight: weight,
		},
		{
			Group: "vip", Model: "model-a", ChannelId: 7, Enabled: true,
			Priority: &priority, Weight: weight,
		},
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
	routing.exclude(26)

	channel, group, err := routing.selectChannel(retryParam)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 7, channel.Id)
	assert.Equal(t, "vip", group)
	assert.False(t, routing.candidatesExhausted())

	routing.exclude(7)
	channel, group, err = routing.selectChannel(retryParam)
	require.NoError(t, err)
	assert.Nil(t, channel)
	assert.Equal(t, "vip", group)
	assert.True(t, routing.candidatesExhausted())
}
