package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireRelayChannelConcurrencySelectsAnotherChannel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority100 := int64(100)
	priority90 := int64(90)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 101, Name: "limited", Key: "key-1", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority100, Weight: &weight},
		{Id: 102, Name: "available", Key: "key-2", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority90, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 101, Enabled: true, Priority: &priority100, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 102, Enabled: true, Priority: &priority90, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), 101, 1)
	require.NoError(t, err)
	heldLease, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), 101)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(heldLease.Release)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "model-a",
		TokenGroup:      "vip",
		UsingGroup:      "vip",
	}
	limited, err := model.GetChannelById(101, true)
	require.NoError(t, err)

	selected, lease, apiErr := acquireRelayChannelConcurrency(
		ctx,
		info,
		retryParam,
		newRelayRetryRouting(),
		limited,
		true,
	)
	require.Nil(t, apiErr)
	require.NotNil(t, lease)
	defer lease.Release()
	assert.Equal(t, 102, selected.Id)
	assert.Equal(t, 102, ctx.GetInt("channel_id"))
}

func TestAcquireRelayChannelConcurrencyDoesNotRerouteSpecificChannel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 103, Name: "specific", Key: "key", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled,
	}).Error)
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), 103, 1)
	require.NoError(t, err)
	heldLease, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), 103)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(heldLease.Release)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("specific_channel_id", "103")
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	info := &relaycommon.RelayInfo{OriginModelName: "model-a", TokenGroup: "vip", UsingGroup: "vip"}
	channel, err := model.GetChannelById(103, true)
	require.NoError(t, err)

	selected, lease, apiErr := acquireRelayChannelConcurrency(
		ctx,
		info,
		retryParam,
		newRelayRetryRouting(),
		channel,
		true,
	)
	assert.Nil(t, selected)
	assert.Nil(t, lease)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
}

func TestAcquireRelayChannelConcurrencyReturns429WhenAllChannelsAreSaturated(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority100 := int64(100)
	priority90 := int64(90)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 104, Name: "limited-1", Key: "key-1", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority100, Weight: &weight},
		{Id: 105, Name: "limited-2", Key: "key-2", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority90, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 104, Enabled: true, Priority: &priority100, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 105, Enabled: true, Priority: &priority90, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	for _, channelID := range []int{104, 105} {
		_, err := service.SaveChannelConcurrencyLimit(t.Context(), channelID, 1)
		require.NoError(t, err)
		lease, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), channelID)
		require.NoError(t, err)
		require.True(t, acquired)
		t.Cleanup(lease.Release)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	retryParam := &service.RetryParam{
		Ctx:         ctx,
		TokenGroup:  "vip",
		ModelName:   "model-a",
		RequestPath: ctx.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	info := &relaycommon.RelayInfo{OriginModelName: "model-a", TokenGroup: "vip", UsingGroup: "vip"}
	channel, err := model.GetChannelById(104, true)
	require.NoError(t, err)

	selected, lease, apiErr := acquireRelayChannelConcurrency(
		ctx,
		info,
		retryParam,
		newRelayRetryRouting(),
		channel,
		true,
	)
	assert.Nil(t, selected)
	assert.Nil(t, lease)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
	assert.Zero(t, retryParam.GetRetry())
}
