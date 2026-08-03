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

func TestChannelSmartScheduleMergeSharedSamplePerformanceCombinesBusinessAndSharedSamples(t *testing.T) {
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
	state := model.ChannelSmartScheduleModelSampleState{
		SamplesJSON: model.ChannelSmartScheduleSamplesJSON(rawProbeSamples),
	}
	probeMetrics := state.MetricsSince(90)
	assert.Equal(t, int64(2), probeMetrics.SampleCount)
	assert.Equal(t, int64(2), probeMetrics.FirstTokenSampleCount)
	require.NotNil(t, probeMetrics.AverageFirstTokenMs)

	merged := channelSmartScheduleMergeSharedSamplePerformance(performance, state, 90)
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

	windowed := channelSmartScheduleMergeSharedSamplePerformance(nil, state, 101)
	require.NotNil(t, windowed)
	assert.Equal(t, 1, windowed.FirstTokenSampleCount)
	assert.Equal(t, int64(1), windowed.StabilitySampleCount)
	assert.Nil(t, channelSmartScheduleMergeSharedSamplePerformance(nil, state, 111))
}

func TestChannelSmartScheduleModelSampleStateCanSelectManualSamples(t *testing.T) {
	rawSamples, err := common.Marshal([]map[string]any{
		{
			"time": int64(100), "success": true,
			"source":         model.ChannelSmartScheduleSampleSourceScheduledProbe,
			"first_token_ms": 100.0,
		},
		{
			"time": int64(110), "success": false,
			"source":              model.ChannelSmartScheduleSampleSourceManualTest,
			"failure_duration_ms": 750.0,
		},
	})
	require.NoError(t, err)
	state := model.ChannelSmartScheduleModelSampleState{
		SamplesJSON: model.ChannelSmartScheduleSamplesJSON(rawSamples),
	}

	all := channelSmartScheduleMergeSharedSamplePerformance(nil, state, 90)
	require.NotNil(t, all)
	assert.Equal(t, int64(2), all.StabilitySampleCount)
	assert.Equal(t, int64(1), all.StabilitySuccessCount)
	assert.Equal(t, int64(1), all.StabilityFailureCount)
	assert.Equal(t, 1, all.FirstTokenSampleCount)

	manual := state.ManualTestMetricsSince(90)
	assert.Equal(t, int64(1), manual.SampleCount)
	assert.Zero(t, manual.SuccessCount)
	assert.Equal(t, int64(1), manual.FailureCount)
	assert.Zero(t, manual.FirstTokenSampleCount)
}

func TestRunChannelSmartScheduleUsesSharedScheduledSamplesInFormalScoring(t *testing.T) {
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
		_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1401, Model: "model-a",
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

func TestRunChannelSmartScheduleSharesSamplesAcrossGroupsWithIndependentStrategies(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	common.LogConsumeEnabled = false
	constant.ErrorLogEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})
	probeMode := channelMonitorSmartScheduleSampleProbe
	offMode := channelMonitorSmartScheduleSampleOff
	goldPolicy := channelSmartScheduleTestGroupPolicy(
		"gold", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 2, 80, 30,
	)
	goldPolicy.SampleMode = &probeMode
	silverPolicy := channelSmartScheduleTestGroupPolicy(
		"silver", channelMonitorSmartScheduleStrategyTPS, false,
		channelMonitorSmartScheduleApplyWeight, []string{"model-a"}, 2, 80, 30,
	)
	silverPolicy.SampleMode = &offMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, goldPolicy, silverPolicy,
		),
	})

	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1403, Name: "fast first token", Group: "gold,silver", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
		{Id: 1404, Name: "high throughput", Group: "gold,silver", Models: "model-a", Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1403, Group: "gold", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1404, Group: "gold", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1403, Group: "silver", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1404, Group: "silver", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1403, GroupName: "gold", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1404, GroupName: "gold", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1403, GroupName: "silver", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 1404, GroupName: "silver", ModelName: "model-a", ParticipationSet: true},
	}).Error)

	now := common.GetTimestamp()
	for index := range 2 {
		fastFirstTokenMs := 100.0
		fastFirstTokenTPS := 10.0
		_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1403, Model: "model-a", WindowStart: now - 60,
			Time: now - int64(2-index), Success: true,
			FirstTokenMs: &fastFirstTokenMs, TPS: &fastFirstTokenTPS,
		})
		require.NoError(t, err)
		highThroughputFirstTokenMs := 500.0
		highThroughputTPS := 50.0
		_, err = model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1404, Model: "model-a", WindowStart: now - 60,
			Time: now - int64(2-index), Success: true,
			FirstTokenMs: &highThroughputFirstTokenMs, TPS: &highThroughputTPS,
		})
		require.NoError(t, err)
	}

	result, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.NoError(t, err)
	assert.Equal(t, 4, result.Planned)
	var sampleStates []model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Order("channel_id ASC").Find(&sampleStates).Error)
	require.Len(t, sampleStates, 2)
	routes, err := model.GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 4)
	sharedSampleByChannel := make(map[int]int64)
	for _, route := range routes {
		if existingId, exists := sharedSampleByChannel[route.ChannelId]; exists {
			assert.Equal(t, existingId, route.SharedSamples.Id)
		} else {
			sharedSampleByChannel[route.ChannelId] = route.SharedSamples.Id
		}
		assert.Equal(t, int64(2), route.SharedSamples.SampleCount)
	}

	var abilities []model.Ability
	require.NoError(t, db.Find(&abilities).Error)
	type routeKey struct {
		group     string
		channelId int
	}
	abilityByRoute := make(map[routeKey]model.Ability, len(abilities))
	for _, ability := range abilities {
		abilityByRoute[routeKey{group: ability.Group, channelId: ability.ChannelId}] = ability
	}
	assert.Greater(t,
		abilityByRoute[routeKey{group: "gold", channelId: 1403}].Weight,
		abilityByRoute[routeKey{group: "gold", channelId: 1404}].Weight,
	)
	assert.Less(t,
		abilityByRoute[routeKey{group: "silver", channelId: 1403}].Weight,
		abilityByRoute[routeKey{group: "silver", channelId: 1404}].Weight,
	)
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
		_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1411, Model: "model-a",
			WindowStart: now - 3600, Time: now - int64(60-index*30), Success: succeeded,
		})
		require.NoError(t, err)
	}
	for index := range 2 {
		_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1412, Model: "model-a",
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
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
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
		Models: "gpt-3.5-turbo", Group: "vip,zeta", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: channel.Id, Group: "vip", Model: "gpt-3.5-turbo", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: channel.Id, Group: "zeta", Model: "gpt-3.5-turbo", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-3.5-turbo", ParticipationSet: true, Revision: 5},
		{ChannelId: channel.Id, GroupName: "zeta", ModelName: "gpt-3.5-turbo", ParticipationSet: true, Revision: 7},
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{"gpt-3.5-turbo"}, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	probeInterval := 1
	policy.ProbeIntervalMinutes = &probeInterval
	zetaPolicy := channelSmartScheduleTestGroupPolicy(
		"zeta", channelMonitorSmartScheduleStrategyTPS, false,
		channelMonitorSmartScheduleApplyWeight, []string{"gpt-3.5-turbo"}, 1, 80, 30,
	)
	zetaPolicy.SampleMode = &probeMode
	zetaProbeInterval := 5
	zetaPolicy.ProbeIntervalMinutes = &zetaProbeInterval
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, policy, zetaPolicy,
		),
	})
	previousFirstToken := 100.0
	previousTPS := 10.0
	_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
		ChannelId: channel.Id, Model: "gpt-3.5-turbo",
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
	assert.Equal(t, 1, requestCount)
	var state model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", channel.Id, "gpt-3.5-turbo",
	).First(&state).Error)
	assert.Equal(t, int64(2), state.SampleCount)
	assert.Equal(t, int64(2), state.SuccessCount)
	assert.Equal(t, int64(2), state.FirstTokenSampleCount)
	require.NotNil(t, state.AverageFirstTokenMs)
	assert.Equal(t, int64(2), state.TPSSampleCount)
	require.NotNil(t, state.AverageTPS)
	var routeStates []model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Order("group_name ASC").Find(&routeStates).Error)
	require.Len(t, routeStates, 2)
	assert.Equal(t, int64(5), routeStates[0].Revision)
	assert.Equal(t, int64(7), routeStates[1].Revision)
	routes, err := model.GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 2)
	assert.Equal(t, state.Id, routes[0].SharedSamples.Id)
	assert.Equal(t, state.Id, routes[1].SharedSamples.Id)
	var consumeLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
	assert.Equal(t, "智能调度探测", consumeLog.TokenName)
	assert.Equal(t, "智能调度定时探测", consumeLog.Content)
	assert.Equal(t, "vip", consumeLog.Group)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
	assert.Equal(t, true, other[model.ChannelMonitorSmartScheduleProbeLogKey])
}

func TestRunChannelSmartScheduleProbeDeduplicatesMatchingModelsAndSkipsWildcardOnly(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = originalStreamingTimeout
	})
	const exactModel = "gemini-2.5-pro-thinking-2048"
	const wildcardModel = "gemini-2.5-pro-thinking-*"
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}
		var request dto.OpenAIResponsesRequest
		if !assert.NoError(t, common.Unmarshal(body, &request)) {
			return
		}
		assert.Equal(t, exactModel, request.Model)
		w.Header().Set("Content-Type", "text/event-stream")
		_, err = w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp-probe","model":"gemini-2.5-pro-thinking-2048","created_at":1}}`,
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			"",
		}, "\n\n")))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)
	user := model.User{
		Username: "probe-model-root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 1422, Type: constant.ChannelTypeOpenAI, Key: "sk-exact", Name: "exact and wildcard",
			Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL),
			Models: exactModel + "," + wildcardModel, Group: "vip", Priority: &priority, Weight: &weight,
		},
		{
			Id: 1423, Type: constant.ChannelTypeOpenAI, Key: "sk-wildcard", Name: "wildcard only",
			Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL),
			Models: wildcardModel, Group: "vip", Priority: &priority, Weight: &weight,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1422, Group: "vip", Model: exactModel, Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1422, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1423, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1422, GroupName: "vip", ModelName: exactModel, ParticipationSet: true},
		{ChannelId: 1422, GroupName: "vip", ModelName: wildcardModel, ParticipationSet: true},
		{ChannelId: 1423, GroupName: "vip", ModelName: wildcardModel, ParticipationSet: true},
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, []string{wildcardModel}, 1, 80, 30,
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
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, result.Skipped)
	assert.Equal(t, 1, requestCount)

	var sampleStates []model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Find(&sampleStates).Error)
	require.Len(t, sampleStates, 1)
	assert.Equal(t, 1422, sampleStates[0].ChannelId)
	assert.Equal(t, wildcardModel, sampleStates[0].ModelName)
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

	var sampleCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleModelSampleState{}).
		Where("channel_id = ? AND model_name = ?", channel.Id, "text-embedding-3-small").
		Count(&sampleCount).Error)
	assert.Zero(t, sampleCount)
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

	var sampleCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleModelSampleState{}).Count(&sampleCount).Error)
	assert.Zero(t, sampleCount)
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
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true,
		StabilityState:         model.ChannelSmartScheduleStabilityProbing,
		StabilitySavedPriority: priority,
		StabilitySavedWeight:   weight,
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
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var routeState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "vip", "gpt-4o-mini",
	).First(&routeState).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, routeState.StabilityState)
	assert.Greater(t, routeState.RuntimeProtectionUntil, common.GetTimestamp())
	var consumeLogCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
	assert.Zero(t, consumeLogCount)
}

func TestRunChannelSmartScheduleProbeRateLimitStartsCooldownWithoutStabilitySample(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(upstream.Close)
	user := model.User{
		Username: "probe-rate-limit-root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 1436, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "rate limited channel",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini",
		Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true,
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, nil, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartScheduleRateLimitCooldownOption: "30",
		channelMonitorSmartScheduleGroupPoliciesOption:     channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Failed)
	assert.Greater(t, service.ChannelRateLimitCooldownUntil(channel.Id, "gpt-4o-mini"), common.GetTimestamp())
	var sampleCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleModelSampleState{}).Count(&sampleCount).Error)
	assert.Zero(t, sampleCount)
	var errorLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeError).First(&errorLog).Error)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(errorLog.Other, &other))
	assert.Equal(t, float64(http.StatusTooManyRequests), other["status_code"])
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
