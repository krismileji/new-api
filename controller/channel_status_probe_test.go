package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusProbeTestConfig(models []string, interval int) *channelStatusProbeConfigResponse {
	return &channelStatusProbeConfigResponse{
		Enabled: true, Models: models, IntervalSeconds: interval,
	}
}

func TestChannelStatusProbeHealthSeparatesPausedStaleAndPartial(t *testing.T) {
	now := int64(10_000)
	config := statusProbeTestConfig([]string{"model-a", "model-b"}, 60)
	states := map[string]model.ChannelStatusProbeState{
		"model-a": {
			ModelName: "model-a", LastHealthResult: model.ChannelStatusProbeResultSuccess,
			LastHealthFinishedAt: now - 10,
		},
		"model-b": {
			ModelName: "model-b", LastHealthResult: model.ChannelStatusProbeResultUpstreamFailure,
			LastHealthFinishedAt: now - 10,
		},
	}
	assert.Equal(t, channelStatusProbeHealthPartial, channelStatusProbeHealth(config, common.ChannelStatusEnabled, states, now))
	assert.Equal(t, channelStatusProbeHealthPaused, channelStatusProbeHealth(config, common.ChannelStatusManuallyDisabled, states, now))

	states["model-a"] = model.ChannelStatusProbeState{
		ModelName: "model-a", LastHealthResult: model.ChannelStatusProbeResultSuccess,
		LastHealthFinishedAt: now - 121,
	}
	states["model-b"] = model.ChannelStatusProbeState{
		ModelName: "model-b", LastHealthResult: model.ChannelStatusProbeResultSuccess,
		LastHealthFinishedAt: now - 121,
	}
	assert.Equal(t, channelStatusProbeHealthStale, channelStatusProbeHealth(config, common.ChannelStatusEnabled, states, now))
}

func TestMergeChannelStatusProbeRecentWindowReturnsConfiguredWindowAndWorstMinuteResult(t *testing.T) {
	now := int64(20_000)
	minute := now - now%60
	stateA := model.ChannelStatusProbeState{MinuteBucketsJSON: `[{"started_at":19980,"success":1,"models":["model-a"],"first_token_total_ms":100,"first_token_sample_count":1}]`}
	stateB := model.ChannelStatusProbeState{MinuteBucketsJSON: `[{"started_at":19980,"rate_limited":1,"models":["model-b"]}]`}

	summary, err := mergeChannelStatusProbeRecentWindow(
		[]model.ChannelStatusProbeState{stateA, stateB},
		now,
		15,
		model.ChannelStatusProbeDisplayUnitMinute,
	)
	require.NoError(t, err)
	require.Len(t, summary.Buckets, 15)
	latest := summary.Buckets[len(summary.Buckets)-1]
	assert.Equal(t, minute, latest.StartedAt)
	assert.Equal(t, 1, latest.Success)
	assert.Equal(t, 1, latest.RateLimited)
	assert.Equal(t, model.ChannelStatusProbeResultRateLimited, latest.Result)
	assert.Equal(t, []string{"model-a", "model-b"}, latest.Models)
	assert.InDelta(t, 100, summary.FirstTokenTotalMs, 0.001)
	assert.EqualValues(t, 1, summary.FirstTokenSampleCount)
}

func TestMergeChannelStatusProbeRecentWindowUsesConfiguredHourAndDayBuckets(t *testing.T) {
	now := int64(1_725_888_000)
	tests := []struct {
		name  string
		value int
		unit  string
	}{
		{name: "hours", value: 24, unit: model.ChannelStatusProbeDisplayUnitHour},
		{name: "days", value: 30, unit: model.ChannelStatusProbeDisplayUnitDay},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentBucket := model.ChannelStatusProbeDisplayBucketStart(now, test.unit)
			encoded, err := common.Marshal([]model.ChannelStatusProbeBucket{{
				StartedAt: currentBucket,
				Success:   1,
			}})
			require.NoError(t, err)
			state := model.ChannelStatusProbeState{}
			if test.unit == model.ChannelStatusProbeDisplayUnitHour {
				state.HourBucketsJSON = string(encoded)
			} else {
				state.DayBucketsJSON = string(encoded)
			}
			summary, err := mergeChannelStatusProbeRecentWindow(
				[]model.ChannelStatusProbeState{state},
				now,
				test.value,
				test.unit,
			)
			require.NoError(t, err)
			require.Len(t, summary.Buckets, test.value)
			latest := summary.Buckets[len(summary.Buckets)-1]
			assert.Equal(t, currentBucket, latest.StartedAt)
			assert.Equal(t, model.ChannelStatusProbeResultSuccess, latest.Result)
		})
	}
}

func setupChannelStatusProbeControllerTest(t *testing.T) *model.Channel {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelStatusProbeConfig{},
		&model.ChannelStatusProbeState{},
		&model.ChannelStatusProbeExecution{},
	))
	channel := &model.Channel{
		Id: 8801, Name: "状态探测测试渠道", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Models: "model-a,model-b", Group: "default, vip",
	}
	require.NoError(t, db.Create(channel).Error)
	return channel
}

func TestUpdateChannelStatusProbeConfigValidatesAndUsesOptimisticRevision(t *testing.T) {
	channel := setupChannelStatusProbeControllerTest(t)
	request := map[string]any{
		"enabled": true, "models": []string{"model-a", "model-a"},
		"interval_seconds": 300, "display_value": 12, "display_unit": "hour",
		"record_sample": false, "revision": 0,
	}
	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/status/channel/8801/config", request,
	)
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: "8801"})

	UpdateChannelStatusProbeConfig(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	stored, err := model.GetChannelStatusProbeConfig(channel.Id)
	require.NoError(t, err)
	models, err := stored.Models()
	require.NoError(t, err)
	assert.Equal(t, []string{"model-a"}, models)
	assert.EqualValues(t, 1, stored.Revision)
	assert.Equal(t, 12, stored.DisplayValue)
	assert.Equal(t, model.ChannelStatusProbeDisplayUnitHour, stored.DisplayUnit)

	staleContext, staleRecorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/status/channel/8801/config", request,
	)
	staleContext.Params = append(staleContext.Params, gin.Param{Key: "id", Value: "8801"})
	UpdateChannelStatusProbeConfig(staleContext)
	assert.Equal(t, http.StatusConflict, staleRecorder.Code)
}

func TestUpdateChannelStatusProbeConfigRejectsInvalidSampleIntervalAndWildcard(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	tests := []struct {
		name    string
		request map[string]any
	}{
		{
			name: "sample interval below one minute",
			request: map[string]any{
				"enabled": true, "models": []string{"model-a"}, "interval_seconds": 30,
				"display_value": 60, "display_unit": "minute", "record_sample": true, "revision": 0,
			},
		},
		{
			name: "wildcard model",
			request: map[string]any{
				"enabled": true, "models": []string{"model-*"}, "interval_seconds": 300,
				"display_value": 60, "display_unit": "minute", "record_sample": false, "revision": 0,
			},
		},
		{
			name: "display days above maximum",
			request: map[string]any{
				"enabled": true, "models": []string{"model-a"}, "interval_seconds": 300,
				"display_value": 31, "display_unit": "day", "record_sample": false, "revision": 0,
			},
		},
		{
			name: "unsupported display unit",
			request: map[string]any{
				"enabled": true, "models": []string{"model-a"}, "interval_seconds": 300,
				"display_value": 1, "display_unit": "week", "record_sample": false, "revision": 0,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newChannelMonitorControllerContext(
				t, http.MethodPut, "/api/channel_monitor/status/channel/8801/config", test.request,
			)
			ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: "8801"})
			UpdateChannelStatusProbeConfig(ctx)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestListChannelStatusProbeExecutionsRejectsInvalidFilters(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	ctx, recorder := newChannelMonitorControllerContext(
		t,
		http.MethodGet,
		"/api/channel_monitor/status/channel/8801/executions?result=unknown",
		nil,
	)
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: "8801"})

	ListChannelStatusProbeExecutions(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestChannelStatusProbeOverviewIgnoresStatesForRemovedModels(t *testing.T) {
	channel := setupChannelStatusProbeControllerTest(t)
	now := common.GetTimestamp()
	modelsJSON, err := common.Marshal([]string{"model-a"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ChannelStatusProbeConfig{
		ChannelId: channel.Id, Enabled: true, ModelsJSON: string(modelsJSON),
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
	}).Error)
	require.NoError(t, model.DB.Create(&[]model.ChannelStatusProbeState{
		{ChannelId: channel.Id, ModelName: "model-a", ExecutionId: 1, FinishedAt: now - 10, Result: model.ChannelStatusProbeResultSuccess, LastHealthResult: model.ChannelStatusProbeResultSuccess, LastHealthFinishedAt: now - 10},
		{ChannelId: channel.Id, ModelName: "model-b", ExecutionId: 2, FinishedAt: now - 5, Result: model.ChannelStatusProbeResultUpstreamFailure, LastHealthResult: model.ChannelStatusProbeResultUpstreamFailure, LastHealthFinishedAt: now - 5},
	}).Error)
	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/status", nil,
	)

	GetChannelStatusProbeOverview(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                               `json:"success"`
		Data    channelStatusProbeOverviewResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Channels, 1)
	assert.Equal(t, []string{"default", "vip"}, response.Data.Groups)
	require.NotNil(t, response.Data.Channels[0].Latest)
	assert.Equal(t, []string{"default", "vip"}, response.Data.Channels[0].Groups)
	assert.Equal(t, "model-a", response.Data.Channels[0].Latest.ModelName)
	require.Len(t, response.Data.Channels[0].ModelStatuses, 1)
	assert.Equal(t, "model-a", response.Data.Channels[0].ModelStatuses[0].ModelName)
	assert.Len(t, response.Data.Channels[0].ModelStatuses[0].RecentWindow, 60)
}

func TestChannelStatusProbeOverviewReturnsOneStatusWindowPerConfiguredModelAndWindowAverages(t *testing.T) {
	channel := setupChannelStatusProbeControllerTest(t)
	now := common.GetTimestamp()
	modelsJSON, err := common.Marshal([]string{"model-a", "model-b"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ChannelStatusProbeConfig{
		ChannelId: channel.Id, Enabled: true, ModelsJSON: string(modelsJSON),
		IntervalSeconds: 300, DisplayValue: 15,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
	}).Error)
	minute := now - now%60
	bucketsA, err := common.Marshal([]model.ChannelStatusProbeBucket{{
		StartedAt: minute, Success: 1, Models: []string{"model-a"},
		FirstTokenTotalMs: 100, FirstTokenSampleCount: 1, TPSTotal: 20, TPSSampleCount: 1,
	}})
	require.NoError(t, err)
	bucketsB, err := common.Marshal([]model.ChannelStatusProbeBucket{{
		StartedAt: minute, Success: 1, UpstreamFailure: 1, Models: []string{"model-b"},
		FirstTokenTotalMs: 300, FirstTokenSampleCount: 1, TPSTotal: 40, TPSSampleCount: 1,
	}})
	require.NoError(t, err)
	firstTokenA := 100.0
	firstTokenB := 300.0
	tpsA := 20.0
	tpsB := 40.0
	require.NoError(t, model.DB.Create(&[]model.ChannelStatusProbeState{
		{ChannelId: channel.Id, ModelName: "model-a", ExecutionId: 1, FinishedAt: now - 10,
			Result: model.ChannelStatusProbeResultSuccess, FirstTokenMs: &firstTokenA, TPS: &tpsA,
			LastHealthResult: model.ChannelStatusProbeResultSuccess, LastHealthFinishedAt: now - 10,
			MinuteBucketsJSON: string(bucketsA)},
		{ChannelId: channel.Id, ModelName: "model-b", ExecutionId: 2, FinishedAt: now - 5,
			Result: model.ChannelStatusProbeResultUpstreamFailure, FirstTokenMs: &firstTokenB, TPS: &tpsB,
			LastHealthResult: model.ChannelStatusProbeResultUpstreamFailure, LastHealthFinishedAt: now - 5,
			MinuteBucketsJSON: string(bucketsB)},
	}).Error)
	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/status", nil,
	)

	GetChannelStatusProbeOverview(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data channelStatusProbeOverviewResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.Channels, 1)
	item := response.Data.Channels[0]
	require.Len(t, item.ModelStatuses, 2)
	assert.Equal(t, "model-a", item.ModelStatuses[0].ModelName)
	require.Len(t, item.ModelStatuses[0].RecentWindow, 15)
	assert.Equal(t, model.ChannelStatusProbeResultSuccess, item.ModelStatuses[0].RecentWindow[14].Result)
	require.NotNil(t, item.ModelStatuses[0].AvgFirstTokenMs)
	assert.InDelta(t, 100, *item.ModelStatuses[0].AvgFirstTokenMs, 0.001)
	assert.Equal(t, "model-b", item.ModelStatuses[1].ModelName)
	require.Len(t, item.ModelStatuses[1].RecentWindow, 15)
	assert.Equal(t, model.ChannelStatusProbeResultUpstreamFailure, item.ModelStatuses[1].RecentWindow[14].Result)
	require.NotNil(t, item.ModelStatuses[1].AvgTPS)
	assert.InDelta(t, 40, *item.ModelStatuses[1].AvgTPS, 0.001)
	require.NotNil(t, item.AvgFirstTokenMs)
	assert.InDelta(t, 200, *item.AvgFirstTokenMs, 0.001)
	require.NotNil(t, item.AvgTPS)
	assert.InDelta(t, 30, *item.AvgTPS, 0.001)
}

func TestChannelStatusProbeOverviewReturnsConfiguredModelsByGroup(t *testing.T) {
	channel := setupChannelStatusProbeControllerTest(t)
	require.NoError(t, model.DB.Model(channel).Update("group", "default").Error)
	defaultModelsJSON, err := common.Marshal([]string{"model-a"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ChannelStatusProbeConfig{
		ChannelId: channel.Id, Enabled: true, ModelsJSON: string(defaultModelsJSON),
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
	}).Error)

	vipChannel := &model.Channel{
		Id: 8802, Name: "VIP 状态探测测试渠道", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Models: "model-b,model-c", Group: "vip",
	}
	require.NoError(t, model.DB.Create(vipChannel).Error)
	vipModelsJSON, err := common.Marshal([]string{"model-b", "model-c"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ChannelStatusProbeConfig{
		ChannelId: vipChannel.Id, Enabled: true, ModelsJSON: string(vipModelsJSON),
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t, http.MethodGet, "/api/channel_monitor/status?model=model-a", nil,
	)
	GetChannelStatusProbeOverview(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data struct {
			ModelsByGroup map[string][]string `json:"models_by_group"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, []string{"model-a"}, response.Data.ModelsByGroup["default"])
	assert.Equal(t, []string{"model-b", "model-c"}, response.Data.ModelsByGroup["vip"])
}

func TestChannelStatusProbeSampleDecisionRetriesOnlyStorageFailures(t *testing.T) {
	assert.Equal(t, model.ChannelStatusProbeSampleRecorded, channelStatusProbeSampleDecision(true, ""))
	assert.Equal(t, model.ChannelStatusProbeSamplePending, channelStatusProbeSampleDecision(false, "样本保存失败，请查看服务端日志"))
	assert.Equal(t, model.ChannelStatusProbeSamplePending, channelStatusProbeSampleDecision(false, "恢复状态读取失败，请查看服务端日志"))
	assert.Equal(t, model.ChannelStatusProbeSampleSkipped, channelStatusProbeSampleDecision(false, "智能调度未启用"))
}
