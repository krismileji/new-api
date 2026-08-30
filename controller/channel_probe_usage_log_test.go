package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelTestUsageLogFollowsProbeResponseSetting(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.ChannelRatioMonitor{}))
	withSelfUseModeEnabled(t)
	service.InitHttpClient()

	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	common.OptionMapRWMutex.Lock()
	optionMapWasNil := common.OptionMap == nil
	if optionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	originalProbeResponseEnabled, hadProbeResponseSetting := common.OptionMap[channelprobe.OptionKey]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		common.OptionMapRWMutex.Lock()
		if optionMapWasNil {
			common.OptionMap = nil
		} else if hadProbeResponseSetting {
			common.OptionMap[channelprobe.OptionKey] = originalProbeResponseEnabled
		} else {
			delete(common.OptionMap, channelprobe.OptionKey)
		}
		common.OptionMapRWMutex.Unlock()
		service.ResetChannelDailyCostSnapshotCache()
	})

	user := &model.User{
		Username: "channel-test-user",
		Password: "channel-test-password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000,
	}
	require.NoError(t, db.Create(user).Error)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"gpt-3.5-turbo","choices":[{"index":0,"message":{"role":"assistant","content":"Hi. What are you working on?"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	channel := &model.Channel{
		Id:      42,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-channel-test",
		Name:    "channel test",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
		Models:  "gpt-3.5-turbo",
		Group:   "default",
	}

	tests := []struct {
		name            string
		probeEnabled    string
		wantConsumeLogs int64
	}{
		{name: "enabled", probeEnabled: "true", wantConsumeLogs: 0},
		{name: "disabled", probeEnabled: "false", wantConsumeLogs: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			common.OptionMapRWMutex.Lock()
			common.OptionMap[channelprobe.OptionKey] = test.probeEnabled
			common.OptionMapRWMutex.Unlock()

			result := testChannel(context.Background(), channel, user.Id, "gpt-3.5-turbo", "", false)

			require.NoError(t, result.localErr)
			var consumeLogCount int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
			assert.Equal(t, test.wantConsumeLogs, consumeLogCount)
		})
	}

	t.Run("smart schedule probe records dedicated usage log and uses target group", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[channelprobe.OptionKey] = "true"
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Delete(&model.Log{}).Error)

		probeCtx := withChannelSmartScheduleProbeTestContext(context.Background(), "vip")
		result := testChannel(probeCtx, channel, user.Id, "gpt-3.5-turbo", "", false)

		require.NoError(t, result.localErr)
		require.NotNil(t, result.context)
		assert.Nil(t, result.firstResponseMilliseconds)
		assert.Equal(t, "vip", common.GetContextKeyString(result.context, constant.ContextKeyUsingGroup))
		var consumeLog model.Log
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
		assert.Equal(t, "智能调度探测", consumeLog.TokenName)
		assert.Equal(t, "智能调度定时探测", consumeLog.Content)
		assert.Equal(t, "vip", consumeLog.Group)
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
		assert.Equal(t, true, other[model.ChannelMonitorSmartScheduleProbeLogKey])
		assert.NotContains(t, other, "performance_timing_version")
		assert.NotContains(t, other, "tokens_per_second")
	})

	t.Run("group monitor probe records a dedicated usage marker", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[channelprobe.OptionKey] = "true"
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Delete(&model.Log{}).Error)

		probeCtx := withChannelGroupMonitorTestContext(context.Background(), "vip")
		result := testChannel(probeCtx, channel, user.Id, "gpt-3.5-turbo", "", false)

		require.NoError(t, result.localErr)
		var consumeLog model.Log
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
		assert.Equal(t, "分组监控探测", consumeLog.TokenName)
		assert.Equal(t, "分组监控探测", consumeLog.Content)
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
		assert.Equal(t, true, other[model.ChannelMonitorGroupProbeLogKey])
	})

	t.Run("group monitor probe follows ordinary consume log setting", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[channelprobe.OptionKey] = "true"
		common.OptionMapRWMutex.Unlock()
		originalLogConsumeEnabled := common.LogConsumeEnabled
		common.LogConsumeEnabled = false
		t.Cleanup(func() { common.LogConsumeEnabled = originalLogConsumeEnabled })
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Delete(&model.Log{}).Error)

		probeCtx := withChannelGroupMonitorTestContext(context.Background(), "vip")
		result := testChannel(probeCtx, channel, user.Id, "gpt-3.5-turbo", "", false)

		require.NoError(t, result.localErr)
		var consumeLogCount int64
		require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
		assert.Equal(t, int64(0), consumeLogCount)
	})

	t.Run("automatic health check records success when probe response is enabled", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[channelprobe.OptionKey] = "true"
		common.OptionMapRWMutex.Unlock()
		common.LogConsumeEnabled = true
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Delete(&model.Log{}).Error)

		summary := testChannelForHealthCheck(context.Background(), channel, user.Id, false, 10_000)

		assert.Equal(t, 1, summary.Succeeded)
		var consumeLog model.Log
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
		assert.Equal(t, "模型测试", consumeLog.TokenName)
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
		assert.Equal(t, true, other[model.ChannelMonitorChannelTestLogKey])
	})

	t.Run("automatic health check records failure without auto disable", func(t *testing.T) {
		originalErrorLogEnabled := constant.ErrorLogEnabled
		constant.ErrorLogEnabled = true
		t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })
		require.NoError(t, db.Where("type = ?", model.LogTypeError).Delete(&model.Log{}).Error)

		failedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, err := w.Write([]byte(`{"error":{"message":"temporary health check failure"}}`))
			assert.NoError(t, err)
		}))
		t.Cleanup(failedUpstream.Close)
		failedChannel := &model.Channel{
			Id: 45, Type: constant.ChannelTypeOpenAI, Key: "sk-health-check",
			Name: "health check failure", Status: common.ChannelStatusEnabled,
			BaseURL: common.GetPointer(failedUpstream.URL), Models: "gpt-3.5-turbo", Group: "default",
		}

		summary := testChannelForHealthCheck(context.Background(), failedChannel, user.Id, false, 10_000)

		assert.Equal(t, 1, summary.Failed)
		assert.Zero(t, summary.Disabled)
		var errorLog model.Log
		require.NoError(t, db.Where("type = ?", model.LogTypeError).First(&errorLog).Error)
		assert.Equal(t, failedChannel.Id, errorLog.ChannelId)
		assert.Equal(t, "模型测试", errorLog.TokenName)
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(errorLog.Other, &other))
		assert.Equal(t, true, other[model.ChannelMonitorChannelTestLogKey])
	})

	t.Run("automatic health check does not log pre-dispatch failure", func(t *testing.T) {
		originalErrorLogEnabled := constant.ErrorLogEnabled
		constant.ErrorLogEnabled = true
		t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })
		service.ClearChannelRateLimitCooldowns()
		t.Cleanup(service.ClearChannelRateLimitCooldowns)
		require.NoError(t, db.Where("type = ?", model.LogTypeError).Delete(&model.Log{}).Error)
		service.StartChannelRateLimitCooldown(channel.Id, "gpt-3.5-turbo", 60)

		summary := testChannelForHealthCheck(context.Background(), channel, user.Id, false, 10_000)

		assert.Equal(t, 1, summary.Tested)
		assert.Zero(t, summary.Succeeded)
		assert.Equal(t, 1, summary.Failed)
		var errorLogCount int64
		require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeError).Count(&errorLogCount).Error)
		assert.Zero(t, errorLogCount)
	})

	t.Run("automatic health check does not duplicate slow successful request", func(t *testing.T) {
		originalAutomaticDisableEnabled := common.AutomaticDisableChannelEnabled
		originalErrorLogEnabled := constant.ErrorLogEnabled
		common.AutomaticDisableChannelEnabled = true
		constant.ErrorLogEnabled = true
		t.Cleanup(func() {
			common.AutomaticDisableChannelEnabled = originalAutomaticDisableEnabled
			constant.ErrorLogEnabled = originalErrorLogEnabled
		})
		service.ClearChannelRateLimitCooldowns()
		require.NoError(t, db.Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}).Delete(&model.Log{}).Error)

		summary := testChannelForHealthCheck(context.Background(), channel, user.Id, false, -1)

		assert.Equal(t, 1, summary.Tested)
		assert.Zero(t, summary.Succeeded)
		assert.Equal(t, 1, summary.Failed)
		var logs []model.Log
		require.NoError(t, db.Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}).Find(&logs).Error)
		require.Len(t, logs, 1)
		assert.Equal(t, model.LogTypeConsume, logs[0].Type)
		assert.Equal(t, "模型测试", logs[0].TokenName)
	})
}

func TestChannelTestRecordsDispatchedFailuresAsUnresolvedCost(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.ChannelRatioMonitor{},
		&model.ChannelDailyCost{},
		&model.ChannelDailyAPIKeyCost{},
	))
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	service.ResetChannelDailyCostSnapshotCache()
	t.Cleanup(service.ResetChannelDailyCostSnapshotCache)

	user := &model.User{
		Username: "failed-channel-test-user",
		Password: "failed-channel-test-password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    1_000_000,
	}
	require.NoError(t, db.Create(user).Error)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, err := w.Write([]byte(`{"error":{"message":"temporary upstream failure"}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	conversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{
		Mode: service.ChannelMonitorCostConversionNone,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:      43,
		Ratio:          1,
		UpdatedTime:    1,
		CostConversion: conversion,
	}).Error)
	channel := &model.Channel{
		Id:      43,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "sk-failed-channel-test",
		Name:    "failed channel test",
		Status:  common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL),
		Models:  "gpt-3.5-turbo",
		Group:   "default",
	}

	result := testChannel(context.Background(), channel, user.Id, "gpt-3.5-turbo", "", false)
	require.Error(t, result.localErr)
	assert.True(t, result.requestDispatched)
	require.NoError(t, service.FlushChannelDailyCostEvents())

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", channel.Id).Error)
	assert.Zero(t, cost.CostNanoCNY)
	assert.Zero(t, cost.SettledCount)
	assert.Equal(t, int64(1), cost.UnresolvedCount)
}

func TestChannelTestSkipsActive429CooldownBeforeDispatch(t *testing.T) {
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	t.Cleanup(upstream.Close)

	channel := &model.Channel{
		Id: 44, Type: constant.ChannelTypeOpenAI, Key: "sk-rate-limited-test",
		Name: "rate limited channel test", Status: common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL), Models: "gpt-3.5-turbo", Group: "default",
	}
	service.StartChannelRateLimitCooldown(channel.Id, "gpt-3.5-turbo", 60)

	result := testChannel(context.Background(), channel, 0, "gpt-3.5-turbo", "", false)

	require.Error(t, result.localErr)
	assert.Contains(t, result.localErr.Error(), "429 冷却")
	assert.False(t, result.requestDispatched)
	assert.Zero(t, requestCount)
}
