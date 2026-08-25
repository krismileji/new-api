package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func statusProbeTestConfig(models []string, interval int) *channelStatusProbeConfigResponse {
	return &channelStatusProbeConfigResponse{
		Enabled: true, Models: models, IntervalSeconds: interval,
	}
}

func drainChannelStatusProbeWorkerWake() {
	for {
		select {
		case <-channelStatusProbeWake:
		default:
			return
		}
	}
}

func takeChannelStatusProbeWorkerWake() bool {
	select {
	case <-channelStatusProbeWake:
		return true
	default:
		return false
	}
}

func TestChannelStatusProbeScanIntervalConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{name: "default", value: "", expected: time.Second},
		{name: "minimum", value: "200", expected: 200 * time.Millisecond},
		{name: "maximum", value: "30000", expected: 30 * time.Second},
		{name: "below minimum", value: "199", expected: time.Second},
		{name: "above maximum", value: "30001", expected: time.Second},
		{name: "invalid", value: "invalid", expected: time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CHANNEL_STATUS_PROBE_SCAN_INTERVAL_MS", test.value)
			assert.Equal(t, test.expected, channelStatusProbeScanIntervalDuration())
		})
	}
}

func TestChannelStatusProbeScanIntervalSecondsMatchesConfiguredWorkerInterval(t *testing.T) {
	tests := []struct {
		value    string
		expected int
	}{
		{value: "200", expected: 1},
		{value: "1000", expected: 1},
		{value: "1500", expected: 2},
		{value: "30000", expected: 30},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("CHANNEL_STATUS_PROBE_SCAN_INTERVAL_MS", test.value)
			assert.Equal(t, test.expected, channelStatusProbeScanIntervalSeconds())
		})
	}
}

func TestChannelStatusProbeDispatchesDuringActive429Cooldown(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)

	user := model.User{
		Username: "status-probe-cooldown-user", Password: "status-probe-cooldown-password",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)

	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, err := w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	channel := &model.Channel{
		Id: 441, Type: constant.ChannelTypeOpenAI, Key: "sk-status-probe-cooldown",
		Name: "status probe cooldown", Status: common.ChannelStatusEnabled,
		BaseURL: common.GetPointer(upstream.URL), Models: "gpt-3.5-turbo", Group: "default",
	}
	service.StartChannelRateLimitCooldown(channel.Id, "gpt-3.5-turbo", 60)

	outcome := executeChannelStatusProbeModel(context.Background(), channel, user.Id, "gpt-3.5-turbo")

	assert.Equal(t, model.ChannelStatusProbeResultRateLimited, outcome.Result)
	assert.True(t, outcome.TestExecuted)
	assert.True(t, outcome.ProbeResult.requestDispatched)
	assert.Equal(t, int64(1), requestCount.Load())
	assert.Positive(t, service.ChannelRateLimitCooldownUntilMatching(channel.Id, "gpt-3.5-turbo"))
}

func TestChannelStatusProbeDispatchesForDisabledChannels(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()

	user := model.User{
		Username: "disabled-status-probe-user", Password: "disabled-status-probe-password",
		Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)

	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, err := w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	statuses := []int{common.ChannelStatusManuallyDisabled, common.ChannelStatusAutoDisabled}
	for index, status := range statuses {
		channel := &model.Channel{
			Id: 451 + index, Type: constant.ChannelTypeOpenAI, Key: "sk-disabled-status-probe",
			Name: "disabled status probe", Status: status,
			BaseURL: common.GetPointer(upstream.URL), Models: "gpt-3.5-turbo", Group: "default",
		}
		outcome := executeChannelStatusProbeModel(context.Background(), channel, user.Id, "gpt-3.5-turbo")
		assert.Equal(t, model.ChannelStatusProbeResultRateLimited, outcome.Result)
		assert.True(t, outcome.ProbeResult.requestDispatched)
	}
	assert.Equal(t, int64(len(statuses)), requestCount.Load())
}

func TestChannelStatusProbeChannelAllowed(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		allowed bool
	}{
		{name: "enabled", status: common.ChannelStatusEnabled, allowed: true},
		{name: "manually disabled", status: common.ChannelStatusManuallyDisabled, allowed: true},
		{name: "automatically disabled", status: common.ChannelStatusAutoDisabled, allowed: true},
		{name: "unknown", status: common.ChannelStatusUnknown, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.allowed, channelStatusProbeChannelAllowed(test.status))
		})
	}
}

func TestWakeChannelStatusProbeWorkerCoalescesPendingSignals(t *testing.T) {
	drainChannelStatusProbeWorkerWake()
	t.Cleanup(drainChannelStatusProbeWorkerWake)

	wakeChannelStatusProbeWorker()
	wakeChannelStatusProbeWorker()
	assert.True(t, takeChannelStatusProbeWorkerWake())
	assert.False(t, takeChannelStatusProbeWorkerWake())
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
	assert.Equal(t, channelStatusProbeHealthPartial, channelStatusProbeHealth(config, states, now))
	config.Enabled = false
	assert.Equal(t, channelStatusProbeHealthPaused, channelStatusProbeHealth(config, states, now))
	config.Enabled = true

	states["model-a"] = model.ChannelStatusProbeState{
		ModelName: "model-a", LastHealthResult: model.ChannelStatusProbeResultSuccess,
		LastHealthFinishedAt: now - 121,
	}
	states["model-b"] = model.ChannelStatusProbeState{
		ModelName: "model-b", LastHealthResult: model.ChannelStatusProbeResultSuccess,
		LastHealthFinishedAt: now - 121,
	}
	assert.Equal(t, channelStatusProbeHealthStale, channelStatusProbeHealth(config, states, now))
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

func TestMergeChannelStatusProbeExecutionRecentWindowUsesLatestExecution(t *testing.T) {
	firstToken := 240.0
	tps := 42.5
	responseTime := 1_250.0
	summary := mergeChannelStatusProbeExecutionRecentWindow([]model.ChannelStatusProbeExecution{
		{
			Id: 1, ChannelId: 1, ModelName: "model-a", Result: model.ChannelStatusProbeResultSuccess,
			FinishedAt: 1_021, FirstTokenMs: &firstToken, TPS: &tps, ResponseTimeMs: &responseTime,
		},
		{
			Id: 2, ChannelId: 1, ModelName: "model-a", Result: model.ChannelStatusProbeResultUpstreamFailure,
			FinishedAt: 1_029,
		},
	}, 1_029, 1, model.ChannelStatusProbeDisplayUnitMinute)

	require.Len(t, summary.Buckets, 1)
	bucket := summary.Buckets[0]
	assert.Equal(t, model.ChannelStatusProbeResultUpstreamFailure, bucket.LatestResult)
	assert.EqualValues(t, 1, bucket.Success)
	assert.EqualValues(t, 1, bucket.UpstreamFailure)
	assert.EqualValues(t, 1, bucket.FirstTokenSampleCount)
	assert.EqualValues(t, 1, bucket.TPSSampleCount)
	assert.EqualValues(t, 1, bucket.ResponseTimeSampleCount)
	assert.InDelta(t, responseTime, bucket.ResponseTimeTotalMs, 0.001)
	assert.Nil(t, bucket.LatestFirstTokenMs)
	assert.Nil(t, bucket.LatestTPS)
	assert.Nil(t, bucket.LatestResponseTimeMs)
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
	drainChannelStatusProbeWorkerWake()
	db := setupChannelMonitorControllerTestDB(t)
	StartChannelStatusProbeOverviewRefreshRuntime()
	invalidateChannelStatusProbeOverviewCache()
	t.Cleanup(invalidateChannelStatusProbeOverviewCache)
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

func getChannelStatusProbeOverviewResponse(
	t *testing.T,
	target string,
) channelStatusProbeOverviewResponse {
	t.Helper()
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, target, nil)
	GetChannelStatusProbeOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                               `json:"success"`
		Data    channelStatusProbeOverviewResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	return response.Data
}

func TestChannelStatusProbeOverviewCachesEachModelFilterAndInvalidates(t *testing.T) {
	channel := setupChannelStatusProbeControllerTest(t)
	modelsJSON, err := common.Marshal([]string{"model-a"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ChannelStatusProbeConfig{
		ChannelId: channel.Id, Enabled: true, ModelsJSON: string(modelsJSON),
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
	}).Error)

	var queryCount atomic.Int64
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(
		"test:channel_status_probe_overview_cache",
		func(*gorm.DB) { queryCount.Add(1) },
	))

	getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	unfilteredQueryCount := queryCount.Load()
	require.Positive(t, unfilteredQueryCount)
	getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	assert.Equal(t, unfilteredQueryCount, queryCount.Load())

	getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status?model=model-a")
	filteredQueryCount := queryCount.Load()
	assert.Greater(t, filteredQueryCount, unfilteredQueryCount)
	getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status?model=model-a")
	assert.Equal(t, filteredQueryCount, queryCount.Load())

	invalidateChannelStatusProbeOverviewCache()
	invalidated := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	assert.True(t, invalidated.Stale)
	require.Eventually(t, func() bool {
		return queryCount.Load() > filteredQueryCount
	}, time.Second, 5*time.Millisecond)
}

func TestChannelStatusProbeOverviewCacheCanBeDisabled(t *testing.T) {
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS", "0")
	setupChannelStatusProbeControllerTest(t)

	var queryCount atomic.Int64
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(
		"test:channel_status_probe_overview_cache_disabled",
		func(*gorm.DB) { queryCount.Add(1) },
	))
	getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	firstQueryCount := queryCount.Load()
	require.Positive(t, firstQueryCount)
	getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	assert.Greater(t, queryCount.Load(), firstQueryCount)
}

func TestChannelStatusProbeOverviewCacheTTLConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{name: "default", value: "", expected: 3 * time.Second},
		{name: "milliseconds", value: "1250", expected: 1250 * time.Millisecond},
		{name: "disabled", value: "0", expected: 0},
		{name: "negative disables", value: "-1", expected: 0},
		{name: "maximum", value: "30000", expected: 30 * time.Second},
		{name: "above maximum is clamped", value: "30001", expected: 30 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS", test.value)
			assert.Equal(t, test.expected, channelStatusProbeOverviewCacheTTL())
		})
	}
}

func TestChannelStatusProbeOverviewInvalidatesAfterConfigAndManualRun(t *testing.T) {
	channel := setupChannelStatusProbeControllerTest(t)
	initial := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	require.Len(t, initial.Channels, 1)
	require.Nil(t, initial.Channels[0].Config)

	request := map[string]any{
		"enabled": true, "models": []string{"model-a"},
		"interval_seconds": 300, "display_value": 60, "display_unit": "minute",
		"record_sample": false, "revision": 0,
	}
	updateContext, updateRecorder := newChannelMonitorControllerContext(
		t, http.MethodPut, "/api/channel_monitor/status/channel/8801/config", request,
	)
	updateContext.Params = append(updateContext.Params, gin.Param{Key: "id", Value: "8801"})
	UpdateChannelStatusProbeConfig(updateContext)
	require.Equal(t, http.StatusOK, updateRecorder.Code)
	assert.True(t, takeChannelStatusProbeWorkerWake())

	var configured channelStatusProbeOverviewResponse
	require.Eventually(t, func() bool {
		configured = getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
		return !configured.Stale && configured.Channels[0].Config != nil
	}, time.Second, 5*time.Millisecond)
	require.NotNil(t, configured.Channels[0].Config)
	assert.Empty(t, configured.Channels[0].Config.ManualRequestId)

	runContext, runRecorder := newChannelMonitorControllerContext(
		t, http.MethodPost, "/api/channel_monitor/status/channel/8801/run", nil,
	)
	runContext.Params = append(runContext.Params, gin.Param{Key: "id", Value: "8801"})
	RunChannelStatusProbeNow(runContext)
	require.Equal(t, http.StatusAccepted, runRecorder.Code)
	assert.True(t, takeChannelStatusProbeWorkerWake())

	var pending channelStatusProbeOverviewResponse
	require.Eventually(t, func() bool {
		pending = getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
		return !pending.Stale && pending.Channels[0].Config != nil &&
			pending.Channels[0].Config.ManualRequestId != ""
	}, time.Second, 5*time.Millisecond)
	require.NotNil(t, pending.Channels[0].Config)
	assert.NotEmpty(t, pending.Channels[0].Config.ManualRequestId)
	assert.Equal(t, channel.Id, pending.Channels[0].Id)
}

func TestChannelStatusProbeOverviewInvalidatesAfterExecutionResult(t *testing.T) {
	channel := setupChannelStatusProbeControllerTest(t)
	modelsJSON, err := common.Marshal([]string{"model-a"})
	require.NoError(t, err)
	config := model.ChannelStatusProbeConfig{
		ChannelId: channel.Id, Enabled: true, ModelsJSON: string(modelsJSON),
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute, Revision: 1,
	}
	require.NoError(t, model.DB.Create(&config).Error)
	initial := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	require.Nil(t, initial.Channels[0].Latest)

	now := common.GetTimestamp()
	settledCostNanoCNY := int64(250_000_000)
	require.NoError(t, model.AddChannelDailyCostWithProbe(
		context.Background(), channel.Id, now, 1_000_000_000, settledCostNanoCNY, 1, 0,
	))
	err = persistChannelStatusProbeOutcome(
		channel,
		model.ChannelStatusProbeClaim{
			Config: config, Models: []string{"model-a"}, RunId: "cache-invalidation-run",
			Trigger: model.ChannelStatusProbeTriggerManual,
		},
		"model-a",
		channelStatusProbeOutcome{
			Result: model.ChannelStatusProbeResultSuccess, StartedAt: now - 1, FinishedAt: now,
			SettledCostNanoCNY: &settledCostNanoCNY,
		},
	)
	require.NoError(t, err)

	var updated channelStatusProbeOverviewResponse
	require.Eventually(t, func() bool {
		updated = getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
		return !updated.Stale && updated.Channels[0].Latest != nil
	}, time.Second, 5*time.Millisecond)
	require.NotNil(t, updated.Channels[0].Latest)
	assert.Equal(t, "model-a", updated.Channels[0].Latest.ModelName)
	require.NotNil(t, updated.Channels[0].Latest.SettledCostNanoCNY)
	assert.Equal(t, settledCostNanoCNY, *updated.Channels[0].Latest.SettledCostNanoCNY)
	assert.InDelta(t, 0.25, updated.Channels[0].TodayProbeCostCNY, 1e-9)
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
		ResponseTimeTotalMs: 900, ResponseTimeSampleCount: 1,
	}})
	require.NoError(t, err)
	bucketsB, err := common.Marshal([]model.ChannelStatusProbeBucket{{
		StartedAt: minute, Success: 1, UpstreamFailure: 1, Models: []string{"model-b"},
		FirstTokenTotalMs: 300, FirstTokenSampleCount: 1, TPSTotal: 40, TPSSampleCount: 1,
		ResponseTimeTotalMs: 1_500, ResponseTimeSampleCount: 1,
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
	assert.InDelta(t, 900, item.ModelStatuses[0].RecentWindow[14].ResponseTimeTotalMs, 0.001)
	assert.EqualValues(t, 1, item.ModelStatuses[0].RecentWindow[14].ResponseTimeSampleCount)
	assert.Equal(t, "model-b", item.ModelStatuses[1].ModelName)
	require.Len(t, item.ModelStatuses[1].RecentWindow, 15)
	assert.Equal(t, model.ChannelStatusProbeResultUpstreamFailure, item.ModelStatuses[1].RecentWindow[14].Result)
	require.NotNil(t, item.ModelStatuses[1].AvgTPS)
	assert.InDelta(t, 40, *item.ModelStatuses[1].AvgTPS, 0.001)
	assert.InDelta(t, 1_500, item.ModelStatuses[1].RecentWindow[14].ResponseTimeTotalMs, 0.001)
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
