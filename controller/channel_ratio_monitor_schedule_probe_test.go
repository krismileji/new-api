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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, fastProbe, slowProbe, off,
		),
	})

	handler := channelSmartScheduleProbeTaskHandler{}
	assert.True(t, handler.Enabled())
	assert.Equal(t, 5*time.Minute, handler.Interval())
}

func TestChannelSmartScheduleProbeHandlerUsesEnabledDegradedProbeInterval(t *testing.T) {
	degradedProbe := channelSmartScheduleTestGroupPolicy(
		"degraded", channelMonitorSmartScheduleStrategyFirstToken, true,
		channelMonitorSmartScheduleApplyWeight, nil, 5, 80, 30,
	)
	degradedProbeEnabled := true
	degradedProbeInterval := 4
	degradedProbe.DegradedProbeEnabled = &degradedProbeEnabled
	degradedProbe.ProbeIntervalMinutes = &degradedProbeInterval
	disabledProbe := channelSmartScheduleTestGroupPolicy(
		"disabled", channelMonitorSmartScheduleStrategyFirstToken, true,
		channelMonitorSmartScheduleApplyWeight, nil, 5, 80, 30,
	)
	disabledProbeInterval := 1
	disabledProbe.ProbeIntervalMinutes = &disabledProbeInterval
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, degradedProbe, disabledProbe,
		),
	})

	handler := channelSmartScheduleProbeTaskHandler{}
	assert.True(t, handler.Enabled())
	assert.Equal(t, 4*time.Minute, handler.Interval())
}

func TestChannelSmartScheduleProbeRouteEnabledForDegradedPolicy(t *testing.T) {
	policy := channelSmartSchedulePolicy{
		StabilityEnabled:     true,
		DegradedProbeEnabled: true,
		SampleMode:           channelMonitorSmartScheduleSampleOff,
	}

	assert.True(t, channelSmartScheduleProbeRouteEnabled(
		model.ChannelSmartScheduleStabilityDegraded, policy,
	))
	policy.DegradedProbeEnabled = false
	assert.False(t, channelSmartScheduleProbeRouteEnabled(
		model.ChannelSmartScheduleStabilityDegraded, policy,
	))
	policy.SampleMode = channelMonitorSmartScheduleSampleProbe
	assert.True(t, channelSmartScheduleProbeRouteEnabled("", policy))
	assert.False(t, channelSmartScheduleProbeRouteEnabled(
		model.ChannelSmartScheduleStabilityDegraded, policy,
	))
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
		_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1401, Model: "model-a",
			WindowStart: now - 3600, Time: probeTime, Success: true, FirstTokenMs: &probeFirstToken,
		})
		require.NoError(t, err)
	}
	minuteStart := now - now%60 - 60
	require.NoError(t, db.Create(&[]model.Log{
		{
			ChannelId: 1402, ModelName: "model-a", Group: "vip",
			CreatedAt: minuteStart + 1, Type: model.LogTypeConsume, IsStream: true,
			CompletionTokens: 10, UseTime: 1, Other: `{"frt":100}`,
		},
		{
			ChannelId: 1402, ModelName: "model-a", Group: "vip",
			CreatedAt: minuteStart + 2, Type: model.LogTypeConsume, IsStream: true,
			CompletionTokens: 10, UseTime: 1, Other: `{"frt":100}`,
		},
	}).Error)
	fastFirstToken := 100.0
	require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
		1402, "vip", "model-a", minuteStart+1, true, &fastFirstToken, nil, nil, false,
	))
	require.NoError(t, projectChannelSmartScheduleMetricEventForTest(
		1402, "vip", "model-a", minuteStart+2, true, &fastFirstToken, nil, nil, false,
	))

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
		_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1403, Model: "model-a", WindowStart: now - 60,
			Time: now - int64(2-index), Success: true,
			FirstTokenMs: &fastFirstTokenMs, TPS: &fastFirstTokenTPS,
		})
		require.NoError(t, err)
		highThroughputFirstTokenMs := 500.0
		highThroughputTPS := 50.0
		_, err = saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
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
		_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
			ChannelId: 1411, Model: "model-a",
			WindowStart: now - 3600, Time: now - int64(60-index*30), Success: succeeded,
		})
		require.NoError(t, err)
	}
	for index := range 2 {
		_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
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
			`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":0,"total_tokens":1}}}`,
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
	_, err := saveChannelSmartScheduleModelSampleForTest(model.ChannelSmartScheduleModelSampleResult{
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
	assert.Equal(t, int64(1), state.TPSSampleCount)
	require.NotNil(t, state.AverageTPS)
	assert.InDelta(t, previousTPS, *state.AverageTPS, 1e-9)
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

func TestRunChannelSmartScheduleDegradedProbeFailureRenewsCooldownAndRecordsError(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/responses", r.URL.Path)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	user := model.User{Username: "probe-error-root", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	degradedPriority := int64(0)
	degradedUntil := common.GetTimestamp() + 60
	channel := model.Channel{
		Id: 1435, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "error channel",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini",
		Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true,
		StabilityState:         model.ChannelSmartScheduleStabilityDegraded,
		StabilityUntil:         degradedUntil,
		StabilitySavedPriority: priority,
		StabilitySavedWeight:   weight,
		RuntimeProtectionUntil: degradedUntil,
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, true,
		channelMonitorSmartScheduleApplyWeight, nil, 1, 80, 30,
	)
	degradedProbeEnabled := true
	failureThreshold := 100
	policy.DegradedProbeEnabled = &degradedProbeEnabled
	policy.ConsecutiveFailureThreshold = &failureThreshold
	policy.BurstFailureThreshold = &failureThreshold
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	probeStartedAt := common.GetTimestamp()
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
	assert.GreaterOrEqual(t, routeState.StabilityUntil, probeStartedAt+30*60)
	assert.Equal(t, routeState.StabilityUntil, routeState.RuntimeProtectionUntil)
	assert.Contains(t, routeState.LastScheduleError, "降级期间定时探测失败，已延长稳定性保护")
	var consumeLogCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
	assert.Zero(t, consumeLogCount)
}

func TestRunChannelSmartScheduleProbeRateLimitStartsCooldownWithoutStabilitySample(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = originalErrorLogEnabled })
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
	degradedPriority := int64(0)
	degradedUntil := common.GetTimestamp() + 10*60
	channel := model.Channel{
		Id: 1436, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "rate limited channel",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini",
		Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini", Enabled: true,
		Priority: &degradedPriority, Weight: 0,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true,
		StabilityState: model.ChannelSmartScheduleStabilityDegraded,
		StabilityUntil: degradedUntil, RuntimeProtectionUntil: degradedUntil,
		StabilitySavedPriority: priority, StabilitySavedWeight: weight,
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, true,
		channelMonitorSmartScheduleApplyWeight, nil, 1, 80, 30,
	)
	degradedProbeEnabled := true
	policy.DegradedProbeEnabled = &degradedProbeEnabled
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
	var routeState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "vip", "gpt-4o-mini",
	).First(&routeState).Error)
	assert.Equal(t, degradedUntil, routeState.StabilityUntil)
	assert.Equal(t, degradedUntil, routeState.RuntimeProtectionUntil)
	var errorLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeError).First(&errorLog).Error)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(errorLog.Other, &other))
	assert.Equal(t, float64(http.StatusTooManyRequests), other["status_code"])
}

func TestRunChannelSmartScheduleProbeSkipsActive429CooldownAndPreservesRecoveryProgress(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	t.Cleanup(upstream.Close)
	user := model.User{
		Username: "probe-active-rate-limit-root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 1437, Type: constant.ChannelTypeOpenAI, Key: "sk-probe", Name: "active rate limit",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL),
		Models: "gpt-4o-mini", Group: "vip", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "gpt-4o-mini", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleModelSampleState{
		ChannelId: channel.Id, ModelName: "gpt-4o-mini", RecoverySuccessCount: 2,
		RecoverySuccessAt: common.GetTimestamp() - 10,
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
	service.StartChannelRateLimitCooldown(channel.Id, "gpt-4o-mini", 60)

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Zero(t, result.Probed)
	assert.Zero(t, result.Failed)
	assert.Equal(t, 1, result.Skipped)
	assert.Zero(t, requestCount)
	var sampleState model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", channel.Id, "gpt-4o-mini",
	).First(&sampleState).Error)
	assert.Equal(t, 2, sampleState.RecoverySuccessCount)
	assert.NotZero(t, sampleState.RecoverySuccessAt)
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

func setupChannelSmartScheduleLogicalProbeTest(
	t *testing.T,
	onRequest func(*http.Request),
) (*gorm.DB, model.ChannelLogicalGroup) {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "true")
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = originalStreamingTimeout
	})
	require.NoError(t, db.AutoMigrate(
		&model.ChannelLogicalGroup{},
		&model.ChannelLogicalGroupMember{},
		&model.ChannelLogicalSmartScheduleRouteState{},
		&model.ChannelLogicalSmartScheduleSampleState{},
	))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onRequest != nil {
			onRequest(r)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := w.Write([]byte(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp-logical-probe","model":"gpt-4o-mini","created_at":1}}`,
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			"",
		}, "\n\n")))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	user := model.User{
		Username: "logical-scheduled-probe-root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	logicalGroup := model.ChannelLogicalGroup{Name: "scheduled-probe-group"}
	require.NoError(t, db.Create(&logicalGroup).Error)
	priority := int64(80)
	weight := uint(50)
	logicalGroupId := logicalGroup.Id
	channels := []model.Channel{
		{
			Id: 1441, Type: constant.ChannelTypeOpenAI, Key: "sk-zero-weight", Name: "zero weight member",
			Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini",
			Group: "vip", Priority: &priority, Weight: &weight, LogicalChannelID: &logicalGroupId,
		},
		{
			Id: 1442, Type: constant.ChannelTypeOpenAI, Key: "sk-weighted", Name: "weighted member",
			Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL), Models: "gpt-4o-mini",
			Group: "vip", Priority: &priority, Weight: &weight, LogicalChannelID: &logicalGroupId,
		},
	}
	require.NoError(t, db.Create(&channels).Error)
	fingerprint := strings.Repeat("a", 64)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: logicalGroup.Id, ChannelID: 1441, Weight: 0, AddressFingerprint: fingerprint},
		{LogicalGroupID: logicalGroup.Id, ChannelID: 1442, Weight: 10, AddressFingerprint: fingerprint},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 1441, Group: "vip", Model: "gpt-4o-mini", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 1442, Group: "vip", Model: "gpt-4o-mini", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: 1441, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true},
		{ChannelId: 1442, GroupName: "vip", ModelName: "gpt-4o-mini", ParticipationSet: true},
	}).Error)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyFirstToken, false,
		channelMonitorSmartScheduleApplyWeight, []string{"gpt-4o-mini"}, 1, 80, 30,
	)
	probeMode := channelMonitorSmartScheduleSampleProbe
	policy.SampleMode = &probeMode
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	return db, logicalGroup
}

func TestRunChannelSmartScheduleProbeSharesLogicalExecutionAndUsesMemberWeight(t *testing.T) {
	requestCount := 0
	authorization := ""
	db, logicalGroup := setupChannelSmartScheduleLogicalProbeTest(t, func(r *http.Request) {
		requestCount++
		authorization = r.Header.Get("Authorization")
	})

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, "Bearer sk-weighted", authorization)
	var logicalSamples []model.ChannelLogicalSmartScheduleSampleState
	require.NoError(t, db.Find(&logicalSamples).Error)
	require.Len(t, logicalSamples, 1)
	assert.Equal(t, logicalGroup.Id, logicalSamples[0].LogicalGroupID)
	assert.Equal(t, logicalGroup.Revision, logicalSamples[0].LogicalRevision)
	assert.Equal(t, "vip", logicalSamples[0].GroupName)
	assert.Equal(t, "gpt-4o-mini", logicalSamples[0].ModelName)
	var samples []map[string]any
	require.NoError(t, common.UnmarshalJsonStr(string(logicalSamples[0].SamplesJSON), &samples))
	require.Len(t, samples, 1)
	assert.Equal(t, float64(1442), samples[0]["physical_channel_id"])
	var physicalSamples []model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Find(&physicalSamples).Error)
	assert.Empty(t, physicalSamples)
}

func TestRunChannelSmartScheduleProbeDiscardsStaleLogicalRevisionWithoutPhysicalFallback(t *testing.T) {
	var db *gorm.DB
	var logicalGroup model.ChannelLogicalGroup
	requestCount := 0
	db, logicalGroup = setupChannelSmartScheduleLogicalProbeTest(t, func(*http.Request) {
		requestCount++
		require.NoError(t, db.Model(&model.ChannelLogicalGroup{}).Where("id = ?", logicalGroup.Id).Updates(map[string]any{
			"revision": logicalGroup.Revision + 1, "updated_at": common.GetTimestamp(),
		}).Error)
	})

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, requestCount)
	var logicalSamples []model.ChannelLogicalSmartScheduleSampleState
	require.NoError(t, db.Find(&logicalSamples).Error)
	require.Len(t, logicalSamples, 1)
	assert.Empty(t, logicalSamples[0].SamplesJSON)
	var persistedGroup model.ChannelLogicalGroup
	require.NoError(t, db.First(&persistedGroup, logicalGroup.Id).Error)
	assert.Equal(t, logicalGroup.Revision+1, persistedGroup.Revision)
	var physicalSamples []model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Find(&physicalSamples).Error)
	assert.Empty(t, physicalSamples)
}

func TestRunChannelSmartScheduleProbeUsesLogicalStateInsteadOfPhysicalMemberState(t *testing.T) {
	requestCount := 0
	db, _ := setupChannelSmartScheduleLogicalProbeTest(t, func(*http.Request) {
		requestCount++
	})
	routes, err := model.GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	_, err = model.CoalesceChannelSmartScheduleSchedulingRoutes(routes)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).Where(
		"channel_id IN ?", []int{1441, 1442},
	).Updates(map[string]any{"participation_set": true, "excluded": true}).Error)

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, requestCount)
}

func TestRunChannelSmartScheduleProbeRetriesRemainingMemberWhenSelectedConcurrencyIsFull(t *testing.T) {
	requestCount := 0
	authorization := ""
	_, _ = setupChannelSmartScheduleLogicalProbeTest(t, func(r *http.Request) {
		requestCount++
		authorization = r.Header.Get("Authorization")
	})
	_, err := service.SaveChannelConcurrencyLimit(context.Background(), 1442, 1)
	require.NoError(t, err)
	occupiedLease, acquired, _, err := service.AcquireChannelConcurrency(context.Background(), 1442)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(occupiedLease.Release)

	result, err := runChannelSmartScheduleProbeOnce(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Probed)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, "Bearer sk-zero-weight", authorization)
}
