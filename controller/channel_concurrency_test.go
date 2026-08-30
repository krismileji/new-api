package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

func TestGetChannelMonitorConcurrencyReturnsCurrentStateForAllChannels(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create([]model.Channel{
		{Id: 109, Name: "limited snapshot", Key: "key-1", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled},
		{Id: 110, Name: "unlimited snapshot", Key: "key-2", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create([]model.Log{
		{ChannelId: 109, CreatedAt: now, Type: model.LogTypeConsume},
		{ChannelId: 109, CreatedAt: now - 59, Type: model.LogTypeConsume},
		{ChannelId: 109, CreatedAt: now - 61, Type: model.LogTypeConsume},
		{ChannelId: 109, CreatedAt: now, Type: model.LogTypeConsume, IsRetryAttempt: true},
		{ChannelId: 109, CreatedAt: now, Type: model.LogTypeConsume, Other: `{"channel_monitor_channel_test":true}`},
		{ChannelId: 110, CreatedAt: now, Type: model.LogTypeConsume},
	}).Error)
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), 109, 1)
	require.NoError(t, err)
	lease, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), 109)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(lease.Release)
	unlimitedLease, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), 110)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(unlimitedLease.Release)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/concurrency", nil)
	GetChannelMonitorConcurrency(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Channels map[string]service.ChannelConcurrencyStatus `json:"channels"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Len(t, response.Data.Channels, 2)
	assert.Equal(t, service.ChannelConcurrencyStatus{Active: 1, Limit: 1, CurrentRPM: 2}, response.Data.Channels["109"])
	assert.Equal(t, service.ChannelConcurrencyStatus{Active: 1, Limit: 0, CurrentRPM: 1}, response.Data.Channels["110"])

	var monitor model.ChannelRatioMonitor
	require.NoError(t, db.Where("channel_id = ?", 109).First(&monitor).Error)
	monitor.ConcurrencyLimit = 3
	monitor.ConcurrencyRevision++
	require.NoError(t, db.Save(&monitor).Error)
	require.NoError(t, db.Delete(&model.Channel{}, 110).Error)

	refreshedRecorder := httptest.NewRecorder()
	refreshedContext, _ := gin.CreateTestContext(refreshedRecorder)
	refreshedContext.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/concurrency", nil)
	GetChannelMonitorConcurrency(refreshedContext)
	require.Equal(t, http.StatusOK, refreshedRecorder.Code)

	var refreshedResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Channels map[string]service.ChannelConcurrencyStatus `json:"channels"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(refreshedRecorder.Body.Bytes(), &refreshedResponse))
	require.True(t, refreshedResponse.Success)
	assert.Len(t, refreshedResponse.Data.Channels, 1)
	assert.Equal(t, service.ChannelConcurrencyStatus{Active: 1, Limit: 3, CurrentRPM: 2}, refreshedResponse.Data.Channels["109"])
	assert.NotContains(t, refreshedResponse.Data.Channels, "110")
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
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorChannelConcurrencyWaitSecondsOption: "0",
	})
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

func TestChannelMonitorSettingsParsesChannelConcurrencyWaitSeconds(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "default", want: defaultChannelMonitorChannelConcurrencyWaitSeconds},
		{name: "configured", raw: "17", want: 17},
		{name: "negative uses default", raw: "-1", want: defaultChannelMonitorChannelConcurrencyWaitSeconds},
		{name: "above maximum uses default", raw: "601", want: defaultChannelMonitorChannelConcurrencyWaitSeconds},
		{name: "invalid uses default", raw: "invalid", want: defaultChannelMonitorChannelConcurrencyWaitSeconds},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := channelMonitorSettingsFromOptions(map[string]string{
				channelMonitorChannelConcurrencyWaitSecondsOption: test.raw,
			})
			assert.Equal(t, test.want, settings.ChannelConcurrencyWaitSeconds)
		})
	}
}

func TestAcquireRelayChannelConcurrencySkipsAlternativeWithoutEnabledKeys(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority300 := int64(300)
	priority200 := int64(200)
	priority100 := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 106, Name: "limited", Key: "key-1", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority300, Weight: &weight},
		{
			Id: 107, Name: "no-enabled-keys", Key: "disabled-key", Group: "vip", Models: "model-a",
			Status: common.ChannelStatusEnabled, Priority: &priority200, Weight: &weight,
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:         true,
				MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			},
		},
		{Id: 108, Name: "available", Key: "key-3", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority100, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "vip", Model: "model-a", ChannelId: 106, Enabled: true, Priority: &priority300, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 107, Enabled: true, Priority: &priority200, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 108, Enabled: true, Priority: &priority100, Weight: weight},
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	_, err := service.SaveChannelConcurrencyLimit(t.Context(), 106, 1)
	require.NoError(t, err)
	heldLease, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), 106)
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
	info := &relaycommon.RelayInfo{OriginModelName: "model-a", TokenGroup: "vip", UsingGroup: "vip"}
	limited, err := model.GetChannelById(106, true)
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
	assert.Equal(t, 108, selected.Id)
	assert.Equal(t, 108, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
}
