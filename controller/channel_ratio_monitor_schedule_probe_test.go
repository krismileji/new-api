package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSmartScheduleProbeHandlerUsesMinimumGroupInterval(t *testing.T) {
	fastProbe := channelSmartScheduleTestGroupPolicy(
		"fast", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, nil, 5, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	fastProbe.SampleMode = &probeMode
	fastInterval := 5
	fastProbe.ProbeIntervalMinutes = &fastInterval
	slowProbe := channelSmartScheduleTestGroupPolicy(
		"slow", channelMonitorSmartScheduleStrategyTPS, false,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	slowProbe.SampleMode = &probeMode
	slowInterval := 20
	slowProbe.ProbeIntervalMinutes = &slowInterval
	off := channelSmartScheduleTestGroupPolicy(
		"off", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, nil, 5, 80, 30,
	)
	offInterval := 1
	off.ProbeIntervalMinutes = &offInterval
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:  "true",
		channelMonitorSmartScheduleIntervalOption: "60",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, fastProbe, slowProbe, off,
		),
	})

	handler := channelSmartScheduleProbeTaskHandler{}
	assert.True(t, handler.Enabled())
	assert.Equal(t, 5*time.Minute, handler.Interval())
}

func TestChannelSmartScheduleMergeProbePerformanceCombinesBusinessAndProbeSamples(t *testing.T) {
	firstToken := 200.0
	tps := 10.0
	probeFirstToken := 100.0
	probeTPS := 30.0
	rawProbeSamples, err := common.Marshal([]map[string]any{
		{"time": int64(100), "success": true, "first_token_ms": probeFirstToken, "tps": probeTPS},
		{"time": int64(110), "success": true, "first_token_ms": probeFirstToken, "tps": probeTPS},
	})
	require.NoError(t, err)
	performance := &channelSmartSchedulePerformance{
		FirstTokenSampleCount:         2,
		FirstTokenDurationSampleCount: 1,
		TPSSampleCount:                2,
		AverageFirstTokenMs:           &firstToken,
		FirstTokenDurationBuckets: []model.ChannelMonitorDurationBucket{
			{LowerBoundMs: 150, UpperBoundMs: 200, Count: 1, TotalMs: 200},
		},
		AverageTPS:            &tps,
		StabilitySampleCount:  2,
		StabilitySuccessCount: 1,
		StabilityFailureCount: 1,
	}
	state := model.ChannelSmartScheduleRouteState{
		ProbeSamples: string(rawProbeSamples),
	}
	probeMetrics := state.ProbeMetricsSince(90)
	assert.Equal(t, int64(2), probeMetrics.SampleCount)
	assert.Equal(t, int64(2), probeMetrics.FirstTokenSampleCount)
	require.NotNil(t, probeMetrics.AverageFirstTokenMs)

	merged := channelSmartScheduleMergeProbePerformance(performance, state, 90)
	require.NotNil(t, merged)
	assert.Equal(t, 4, merged.FirstTokenSampleCount)
	assert.Equal(t, int64(3), merged.FirstTokenDurationSampleCount)
	require.NotNil(t, merged.AverageFirstTokenMs)
	assert.InDelta(t, 150, *merged.AverageFirstTokenMs, 1e-9)
	assert.Equal(t, 4, merged.TPSSampleCount)
	require.NotNil(t, merged.AverageTPS)
	assert.InDelta(t, 20, *merged.AverageTPS, 1e-9)
	assert.Equal(t, int64(4), merged.StabilitySampleCount)
	assert.Equal(t, int64(3), merged.StabilitySuccessCount)
	assert.Equal(t, int64(1), merged.StabilityFailureCount)
	assert.Nil(t, merged.Stability)

	windowed := channelSmartScheduleMergeProbePerformance(nil, state, 101)
	require.NotNil(t, windowed)
	assert.Equal(t, 1, windowed.FirstTokenSampleCount)
	assert.Equal(t, int64(1), windowed.StabilitySampleCount)
	assert.Nil(t, channelSmartScheduleMergeProbePerformance(nil, state, 111))
}

func TestRunChannelSmartScheduleUsesProbeSamplesInFormalScoring(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 2, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1401, Name: "probe slow", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1402, Name: "business fast", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1401, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1402, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	now := common.GetTimestamp()
	probeFirstToken := 500.0
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 1401, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		},
		{ChannelId: 1402, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	for _, probeTime := range []int64{now - 60, now - 30} {
		_, err := model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
			ChannelId: 1401, Group: "vip", Model: "model-a",
			WindowStart: now - 3600, Time: probeTime, Success: true, FirstTokenMs: &probeFirstToken,
		})
		require.NoError(t, err)
	}
	minuteStart := now - now%60 - 60
	require.NoError(t, db.Create(&model.ChannelMonitorMinuteMetric{
		MinuteStart: minuteStart, ChannelId: 1402, ModelKey: "model-a", GroupKey: "vip",
		APIKeyKey: "all", ModelName: "model-a", GroupName: "vip",
		SampleCount: 2, FirstTokenSampleCount: 2, FirstTokenTotalMs: 200, LastUsedTime: minuteStart,
	}).Error)

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Zero(t, result.Failed)
	var abilities []model.Ability
	require.NoError(t, db.Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.Less(t, abilities[0].Weight, abilities[1].Weight)
	assert.NotZero(t, abilities[0].Weight)
}

func TestRunChannelSmartScheduleUsesProbeStabilityWithoutLogMetrics(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 2, 0, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	policy.Scoring.StabilityPercent = 100
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
	})
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1411, Name: "probe less stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1412, Name: "probe stable", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1411, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1412, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1411, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1412, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	for index, succeeded := range []bool{true, false} {
		_, err := model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
			ChannelId: 1411, Group: "vip", Model: "model-a",
			WindowStart: now - 3600, Time: now - int64(60-index*30), Success: succeeded,
		})
		require.NoError(t, err)
	}
	for index := range 2 {
		_, err := model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
			ChannelId: 1412, Group: "vip", Model: "model-a",
			WindowStart: now - 3600, Time: now - int64(60-index*30), Success: true,
		})
		require.NoError(t, err)
	}

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Zero(t, result.Failed)
	var abilities []model.Ability
	require.NoError(t, db.Order("channel_id ASC").Find(&abilities).Error)
	require.Len(t, abilities, 2)
	assert.Less(t, abilities[0].Weight, abilities[1].Weight)
}

func TestRunChannelSmartScheduleProbeRecordsMetricsAndConsumeLog(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = originalStreamingTimeout
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}
		assert.Equal(t, "/v1/responses", r.URL.Path)
		var request dto.OpenAIResponsesRequest
		if !assert.NoError(t, common.Unmarshal(body, &request)) {
			return
		}
		assert.Equal(t, "gpt-3.5-turbo", request.Model)
		if !assert.NotNil(t, request.Stream) {
			return
		}
		assert.True(t, *request.Stream)
		if assert.NotNil(t, request.MaxOutputTokens) {
			assert.Equal(t, uint(16), *request.MaxOutputTokens)
		}
		assert.JSONEq(t, `[{"role":"user","content":"hi"}]`, string(request.Input))
		w.Header().Set("Content-Type", "text/event-stream")
		_, err = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp-probe","model":"gpt-3.5-turbo","created_at":1}}`,
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			"",
		}, "\n\n")))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)
	user := model.User{
		Username: "smart-probe-root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 1421, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "probe channel",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL),
		Models: "gpt-3.5-turbo", Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "gpt-3.5-turbo", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-3.5-turbo",
		ParticipationSet: true, Revision: 5,
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{"gpt-3.5-turbo"}, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	probeInterval := 1
	policy.ProbeIntervalMinutes = &probeInterval
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
	})
	previousFirstToken := 100.0
	previousTPS := 10.0
	_, err := model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
		ChannelId: channel.Id, Group: "vip", Model: "gpt-3.5-turbo",
		WindowStart: common.GetTimestamp() - 3600, Time: common.GetTimestamp() - 120,
		Success: true, FirstTokenMs: &previousFirstToken, TPS: &previousTPS,
	})
	require.NoError(t, err)

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Succeeded)
	assert.Zero(t, result.Failed)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "vip", "gpt-3.5-turbo",
	).First(&state).Error)
	assert.Equal(t, int64(2), state.ProbeSampleCount)
	assert.Equal(t, int64(2), state.ProbeSuccessCount)
	assert.Equal(t, int64(2), state.ProbeFirstTokenSampleCount)
	require.NotNil(t, state.ProbeAverageFirstTokenMs)
	assert.Equal(t, int64(2), state.ProbeTPSSampleCount)
	require.NotNil(t, state.ProbeAverageTPS)
	assert.Equal(t, int64(5), state.Revision)
	var consumeLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
	assert.Equal(t, "智能调度探测", consumeLog.TokenName)
	assert.Equal(t, "智能调度定时探测", consumeLog.Content)
	assert.Equal(t, "vip", consumeLog.Group)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
	assert.Equal(t, true, other[model.ChannelMonitorSmartScheduleProbeLogKey])
}

func TestRunChannelSmartScheduleProbeSkipsNonTextModelsWithoutUpstreamRequest(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 1431, Type: constant.ChannelTypeOpenAI, Name: "embedding channel",
		Status: common.ChannelStatusEnabled, Models: "text-embedding-3-small",
		Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "text-embedding-3-small", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "text-embedding-3-small",
		ParticipationSet: true,
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, []string{"text-embedding-3-small"}, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy,
		),
	})

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Zero(t, result.Probed)
	assert.Zero(t, result.Failed)
	assert.Equal(t, 1, result.Skipped)

	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?",
		channel.Id, "vip", "text-embedding-3-small",
	).First(&state).Error)
	assert.Zero(t, state.ProbeLastTime)
}

func TestChannelSmartScheduleTextProbeRequiresResponsesProtocol(t *testing.T) {
	assert.True(t, channelSmartScheduleSupportsTextProbe(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-4o-mini"))
	assert.False(t, channelSmartScheduleSupportsTextProbe(&model.Channel{Type: constant.ChannelTypeOpenAI}, "omni-moderation-latest"))
	assert.False(t, channelSmartScheduleSupportsTextProbe(&model.Channel{Type: constant.ChannelTypeDeepSeek}, "deepseek-chat"))
	assert.False(t, channelSmartScheduleSupportsTextProbe(&model.Channel{Type: constant.ChannelTypeAnthropic}, "claude-sonnet-4"))
}

func TestRunChannelSmartScheduleProbeSkipsUnsupportedResponseChannels(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1432, Type: constant.ChannelTypeDeepSeek, Name: "deepseek", Status: common.ChannelStatusEnabled, Models: "deepseek-chat", Group: "vip", Priority: &priority, Weight: &weight},
		{Id: 1433, Type: constant.ChannelTypeAnthropic, Name: "claude", Status: common.ChannelStatusEnabled, Models: "claude-sonnet-4", Group: "vip", Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1432, Group: "vip", Model: "deepseek-chat", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1433, Group: "vip", Model: "claude-sonnet-4", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1432, GroupName: "vip", ModelName: "deepseek-chat", ParticipationSet: true},
		{ChannelId: 1433, GroupName: "vip", ModelName: "claude-sonnet-4", ParticipationSet: true},
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, nil, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Total)
	assert.Zero(t, result.Probed)
	assert.Zero(t, result.Failed)
	assert.Equal(t, 2, result.Skipped)

	var states []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Order("channel_id ASC").Find(&states).Error)
	require.Len(t, states, 2)
	for _, state := range states {
		assert.Zero(t, state.ProbeLastTime)
		assert.Zero(t, state.ProbeSampleCount)
	}
	var errorLogCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeError).Count(&errorLogCount).Error)
	assert.Zero(t, errorLogCount)
}

func TestRunChannelSmartScheduleProbeRecordsDispatchedFailureErrorLog(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/responses", r.URL.Path)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	user := model.User{Username: "probe-error-root", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 1435, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "error channel",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini",
		Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini", Enabled: true, Priority: &priority, Weight: weight}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, nil, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Failed)
	var errorLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeError).First(&errorLog).Error)
	assert.Equal(t, channel.Id, errorLog.ChannelId)
	assert.Equal(t, "vip", errorLog.Group)
	assert.Equal(t, "gpt-4o-mini", errorLog.ModelName)
	assert.Equal(t, "智能调度探测", errorLog.TokenName)
	assert.True(t, errorLog.IsStream)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(errorLog.Other, &other))
	assert.Equal(t, true, other[model.ChannelMonitorSmartScheduleProbeLogKey])
	assert.NotEmpty(t, other["error_type"])
	assert.NotEmpty(t, other["error_code"])
	assert.Equal(t, float64(http.StatusServiceUnavailable), other["status_code"])
	assert.NotNil(t, other["channel_monitor_attempt_duration_ms"])
	var consumeLogCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
	assert.Zero(t, consumeLogCount)
}

func TestRunChannelSmartScheduleProbeSkipsSaturatedChannel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	t.Cleanup(upstream.Close)
	user := model.User{Username: "probe-concurrency-root", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 1434, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "saturated",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini",
		Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini", Enabled: true, Priority: &priority, Weight: weight}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, nil, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	require.NoError(t, service.ReloadChannelConcurrencyLimits(context.Background()))
	_, err := service.SaveChannelConcurrencyLimit(context.Background(), channel.Id, 1)
	require.NoError(t, err)
	lease, acquired, _, err := service.AcquireChannelConcurrency(context.Background(), channel.Id)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(lease.Release)

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Zero(t, result.Probed)
	assert.Zero(t, result.Failed)
	assert.Equal(t, 1, result.Skipped)
	assert.Zero(t, requestCount)
}
