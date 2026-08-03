package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func manualProbeTestPolicy(group string, models []string, sampleMode string) channelSmartScheduleGroupPolicy {
	applyMode := channelMonitorSmartScheduleApplyWeight
	if sampleMode == channelMonitorSmartScheduleSampleTraffic {
		applyMode = channelMonitorSmartScheduleApplyPriorityWeight
	}
	policy := channelSmartScheduleTestGroupPolicy(
		group,
		channelMonitorSmartScheduleStrategyRatio,
		true,
		applyMode,
		models,
		1,
		80,
		30,
	)
	policy.SampleMode = &sampleMode
	return policy
}

func TestRecordManualChannelSmartScheduleProbeResultStoresOneSharedSampleForEligibleModel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policies := []channelSmartScheduleGroupPolicy{
		manualProbeTestPolicy("vip", []string{"model-a"}, channelMonitorSmartScheduleSampleProbe),
		manualProbeTestPolicy("shared", nil, channelMonitorSmartScheduleSampleProbe),
		manualProbeTestPolicy("traffic", nil, channelMonitorSmartScheduleSampleTraffic),
		manualProbeTestPolicy("off", nil, channelMonitorSmartScheduleSampleOff),
		manualProbeTestPolicy("excluded", nil, channelMonitorSmartScheduleSampleProbe),
		manualProbeTestPolicy("degraded", nil, channelMonitorSmartScheduleSampleProbe),
		manualProbeTestPolicy("disabled", nil, channelMonitorSmartScheduleSampleProbe),
		manualProbeTestPolicy("wrong-model", []string{"model-b"}, channelMonitorSmartScheduleSampleProbe),
	}
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policies...),
	})

	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 2501, Type: constant.ChannelTypeOpenAI, Name: "manual probe",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	groups := []string{"vip", "shared", "traffic", "off", "excluded", "degraded", "disabled", "wrong-model", "unconfigured"}
	for _, group := range groups {
		require.NoError(t, db.Create(&model.Ability{
			ChannelId: channel.Id, Group: group, Model: "model-a", Enabled: group != "disabled",
			Priority: &priority, Weight: weight,
		}).Error)
		stabilityState := ""
		if group == "degraded" {
			stabilityState = model.ChannelSmartScheduleStabilityDegraded
		}
		require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
			ChannelId: channel.Id, GroupName: group, ModelName: "model-a",
			ParticipationSet: true, Excluded: group == "excluded", StabilityState: stabilityState,
		}).Error)
	}

	firstTokenMs := 125.0
	tps := 30.0
	manualResult := testResult{
		requestDispatched:         true,
		originalModelName:         "model-a",
		firstResponseMilliseconds: &firstTokenMs,
		tokensPerSecond:           &tps,
	}
	recordManualChannelSmartScheduleProbeResultForGroup(&channel, manualResult, 400, "excluded")
	var sampleCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleModelSampleState{}).Count(&sampleCount).Error)
	assert.Zero(t, sampleCount)
	recordManualChannelSmartScheduleProbeResult(&channel, manualResult, 400)

	var samples []model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Find(&samples).Error)
	require.Len(t, samples, 1)
	sharedSample := samples[0]
	assert.Equal(t, channel.Id, sharedSample.ChannelId)
	assert.Equal(t, "model-a", sharedSample.ModelName)
	assert.Equal(t, int64(1), sharedSample.SampleCount)
	assert.Equal(t, int64(1), sharedSample.SuccessCount)
	assert.Equal(t, int64(1), sharedSample.FirstTokenSampleCount)
	assert.Equal(t, int64(1), sharedSample.TPSSampleCount)
	assert.Equal(t, int64(1), sharedSample.ManualTestMetricsSince(0).SampleCount)

	routes, err := model.GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, len(groups))
	for _, route := range routes {
		assert.Equal(t, sharedSample.Id, route.SharedSamples.Id)
		assert.Equal(t, int64(1), route.SharedSamples.SampleCount)
	}

	localFailure := errors.New("local request conversion failed")
	recordManualChannelSmartScheduleProbeResult(&channel, testResult{
		localErr:          localFailure,
		originalModelName: "model-a",
	}, 500)
	recordManualChannelSmartScheduleProbeResult(&channel, testResult{
		localErr:          errors.New("upstream connection failed"),
		requestDispatched: true,
		originalModelName: "model-a",
	}, 850)

	var reloadedSample model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", channel.Id, "model-a",
	).First(&reloadedSample).Error)
	assert.Equal(t, sharedSample.Id, reloadedSample.Id)
	assert.Equal(t, int64(2), reloadedSample.SampleCount)
	assert.Equal(t, int64(1), reloadedSample.SuccessCount)
	assert.Equal(t, int64(1), reloadedSample.FailureDurationSampleCount)
	require.NotNil(t, reloadedSample.AverageFailureDurationMs)
	assert.InDelta(t, 850, *reloadedSample.AverageFailureDurationMs, 1e-9)
	require.NoError(t, db.Find(&samples).Error)
	require.Len(t, samples, 1)

	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	rateLimitError := types.NewErrorWithStatusCode(
		errors.New("upstream rate limited"),
		types.ErrorCodeGetChannelFailed,
		http.StatusTooManyRequests,
	)
	recorded, message := recordManualChannelSmartScheduleProbeResult(&channel, testResult{
		requestDispatched: true,
		newAPIError:       rateLimitError,
		originalModelName: "model-a",
	}, 250)
	assert.False(t, recorded)
	assert.Contains(t, message, "不计入稳定性样本")
	assert.Greater(t, service.ChannelRateLimitCooldownUntil(channel.Id, "model-a"), common.GetTimestamp())
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", channel.Id, "model-a",
	).First(&reloadedSample).Error)
	assert.Equal(t, int64(2), reloadedSample.SampleCount)

	var logCount int64
	require.NoError(t, db.Model(&model.Log{}).Count(&logCount).Error)
	assert.Zero(t, logCount)
}

func TestRecordManualChannelSmartScheduleProbeResultMatchesParameterizedModelRoute(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	const routeModel = "gemini-2.5-pro-thinking-*"
	const requestModel = "gemini-2.5-pro-thinking-2048"
	policy := manualProbeTestPolicy("vip", []string{routeModel}, channelMonitorSmartScheduleSampleProbe)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 2505, Type: constant.ChannelTypeGemini, Name: "parameterized manual probe",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: routeModel, Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: routeModel, ParticipationSet: true,
	}).Error)

	recorded, message := recordManualChannelSmartScheduleProbeResult(&channel, testResult{
		requestDispatched: true,
		originalModelName: requestModel,
	}, 250)
	assert.True(t, recorded)
	assert.Equal(t, "已计入渠道 + 模型共享样本", message)

	var sample model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", channel.Id, routeModel,
	).First(&sample).Error)
	assert.Equal(t, int64(1), sample.SampleCount)
	assert.Equal(t, int64(1), sample.SuccessCount)
	var unformattedCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleModelSampleState{}).
		Where("channel_id = ? AND model_name = ?", channel.Id, requestModel).
		Count(&unformattedCount).Error)
	assert.Zero(t, unformattedCount)
}

func TestRecordManualChannelSmartScheduleProbeResultPrefersExactParameterizedRoute(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	const exactModel = "gemini-2.5-pro-thinking-2048"
	const sharedModel = "gemini-2.5-pro-thinking-*"
	policy := manualProbeTestPolicy("vip", []string{exactModel}, channelMonitorSmartScheduleSampleProbe)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 2506, Type: constant.ChannelTypeGemini, Name: "exact parameterized manual probe",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: channel.Id, Group: "vip", Model: exactModel, Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: channel.Id, Group: "vip", Model: sharedModel, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{ChannelId: channel.Id, GroupName: "vip", ModelName: exactModel, ParticipationSet: true},
		{ChannelId: channel.Id, GroupName: "vip", ModelName: sharedModel, ParticipationSet: true},
	}).Error)

	recorded, message := recordManualChannelSmartScheduleProbeResult(&channel, testResult{
		requestDispatched: true,
		originalModelName: exactModel,
	}, 250)
	assert.True(t, recorded)
	assert.Equal(t, "已计入渠道 + 模型共享样本", message)

	var samples []model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Find(&samples).Error)
	require.Len(t, samples, 1)
	assert.Equal(t, sharedModel, samples[0].ModelName)
	assert.Equal(t, int64(1), samples[0].SampleCount)
}

func TestRecordManualChannelSmartScheduleProbeFailureProtectsAfterMinimumSamples(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := manualProbeTestPolicy("vip", []string{"model-a"}, channelMonitorSmartScheduleSampleOff)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})

	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 2504, Type: constant.ChannelTypeOpenAI, Name: "manual failure",
		Status: common.ChannelStatusEnabled, Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
		TemporaryTrafficKind: model.ChannelSmartScheduleTemporaryTrafficExploration,
		BasePriority:         priority,
		BaseWeight:           weight,
	}).Error)

	upstreamError := types.NewErrorWithStatusCode(
		errors.New("upstream unavailable"),
		types.ErrorCodeBadResponse,
		http.StatusServiceUnavailable,
	)
	recorded, message := recordManualChannelSmartScheduleProbeResult(&channel, testResult{
		requestDispatched: true,
		newAPIError:       upstreamError,
		originalModelName: "model-a",
	}, 250)
	assert.True(t, recorded)
	assert.Equal(t, "已计入渠道 + 模型共享样本", message)

	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a",
	}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Zero(t, *ability.Priority)
	assert.Zero(t, ability.Weight)
	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", channel.Id, "vip", "model-a",
	).First(&state).Error)
	assert.Equal(t, model.ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Greater(t, state.RuntimeProtectionUntil, common.GetTimestamp())
}

func TestManualChannelTestRecordsOneSharedSampleWithoutDuplicateConsumeLog(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	policy := manualProbeTestPolicy("default", []string{"model-a"}, channelMonitorSmartScheduleSampleProbe)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelprobe.OptionKey:                         "false",
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"chatcmpl-manual","object":"chat.completion","created":1,"model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)

	user := model.User{
		Username: "manual-probe-root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 2502, Type: constant.ChannelTypeOpenAI, Key: "sk-manual", Name: "manual probe",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL),
		Models: "model-a", Group: "default", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: channel.Id, Group: "default", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: channel.Id, GroupName: "default", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/channel/test/%d?model=model-a&endpoint_type=openai&stream=false", channel.Id),
		nil,
	)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", channel.Id)}}
	c.Set("id", user.Id)
	TestChannel(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ResponseTimeMs              float64  `json:"response_time"`
			FirstTokenMs                *float64 `json:"first_token_ms"`
			TokensPerSecond             *float64 `json:"tokens_per_second"`
			UsageAvailable              bool     `json:"usage_available"`
			InputTokens                 int      `json:"input_tokens"`
			OutputTokens                int      `json:"output_tokens"`
			TotalTokens                 int      `json:"total_tokens"`
			CachedTokens                int      `json:"cached_tokens"`
			CacheWriteTokens            int      `json:"cache_write_tokens"`
			ReasoningTokens             int      `json:"reasoning_tokens"`
			SmartScheduleSampleRecorded bool     `json:"smart_schedule_sample_recorded"`
			SmartScheduleSampleMessage  string   `json:"smart_schedule_sample_message"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success, recorder.Body.String())
	assert.GreaterOrEqual(t, response.Data.ResponseTimeMs, 0.0)
	assert.Nil(t, response.Data.FirstTokenMs)
	assert.Nil(t, response.Data.TokensPerSecond)
	assert.True(t, response.Data.UsageAvailable)
	assert.Equal(t, 1, response.Data.InputTokens)
	assert.Equal(t, 1, response.Data.OutputTokens)
	assert.Equal(t, 2, response.Data.TotalTokens)
	assert.Zero(t, response.Data.CachedTokens)
	assert.Zero(t, response.Data.CacheWriteTokens)
	assert.Zero(t, response.Data.ReasoningTokens)
	assert.True(t, response.Data.SmartScheduleSampleRecorded)
	assert.Equal(t, "已计入渠道 + 模型共享样本", response.Data.SmartScheduleSampleMessage)

	var state model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", channel.Id, "model-a",
	).First(&state).Error)
	assert.Equal(t, int64(1), state.SampleCount)
	assert.Equal(t, int64(1), state.SuccessCount)
	assert.Zero(t, state.FirstTokenSampleCount)
	assert.Zero(t, state.TPSSampleCount)

	var consumeLogCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
	assert.Equal(t, int64(1), consumeLogCount)
	var consumeLog model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&consumeLog).Error)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(consumeLog.Other, &other))
	assert.Equal(t, true, other[model.ChannelMonitorChannelTestLogKey])

	skipRecorder := httptest.NewRecorder()
	skipContext, _ := gin.CreateTestContext(skipRecorder)
	skipContext.Request = httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/channel/test/%d?model=model-a&endpoint_type=openai&stream=false&record_sample=false", channel.Id),
		nil,
	)
	skipContext.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", channel.Id)}}
	skipContext.Set("id", user.Id)
	TestChannel(skipContext)

	assert.Equal(t, http.StatusOK, skipRecorder.Code)
	var skipResponse struct {
		Success bool `json:"success"`
		Data    struct {
			SmartScheduleSampleRecorded bool   `json:"smart_schedule_sample_recorded"`
			SmartScheduleSampleMessage  string `json:"smart_schedule_sample_message"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(skipRecorder.Body.Bytes(), &skipResponse))
	assert.True(t, skipResponse.Success, skipRecorder.Body.String())
	assert.False(t, skipResponse.Data.SmartScheduleSampleRecorded)
	assert.Equal(t, "已关闭渠道样本记录，本次未计入样本", skipResponse.Data.SmartScheduleSampleMessage)
	require.NoError(t, db.Where(
		"channel_id = ? AND model_name = ?", channel.Id, "model-a",
	).First(&state).Error)
	assert.Equal(t, int64(1), state.SampleCount)
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogCount).Error)
	assert.Equal(t, int64(2), consumeLogCount)

	now := common.GetTimestamp()
	_, err := model.AggregateChannelMonitorMinuteRange(context.Background(), now-120, now+120)
	require.NoError(t, err)
	var minuteMetricCount int64
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteMetric{}).Count(&minuteMetricCount).Error)
	assert.Zero(t, minuteMetricCount)
}

func TestManualChannelTestDefaultsToAutomaticStreamingAndAllowsAutomaticNonStreaming(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	withSelfUseModeEnabled(t)
	service.InitHttpClient()
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = originalStreamingTimeout
	})
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "false",
		channelprobe.OptionKey:                   "false",
	})

	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}

		switch requestCount {
		case 1:
			assert.Equal(t, "/v1/chat/completions", r.URL.Path)
			var request dto.GeneralOpenAIRequest
			if !assert.NoError(t, common.Unmarshal(body, &request)) {
				return
			}
			if assert.NotNil(t, request.Stream) {
				assert.True(t, *request.Stream)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, err = w.Write([]byte(strings.Join([]string{
				`data: {"id":"chatcmpl-manual-default","object":"chat.completion.chunk","created":1,"model":"model-a","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
				`data: {"id":"chatcmpl-manual-default","object":"chat.completion.chunk","created":1,"model":"model-a","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`,
				`data: {"id":"chatcmpl-manual-default","object":"chat.completion.chunk","created":1,"model":"model-a","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				"",
			}, "\n\n")))
			assert.NoError(t, err)
		case 2:
			assert.Equal(t, "/v1/chat/completions", r.URL.Path)
			var request dto.GeneralOpenAIRequest
			if !assert.NoError(t, common.Unmarshal(body, &request)) {
				return
			}
			if assert.NotNil(t, request.Stream) {
				assert.False(t, *request.Stream)
			}
			w.Header().Set("Content-Type", "application/json")
			_, err = w.Write([]byte(`{"id":"chatcmpl-manual-auto","object":"chat.completion","created":1,"model":"model-a","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
			assert.NoError(t, err)
		default:
			t.Fatalf("unexpected upstream request %d", requestCount)
		}
	}))
	t.Cleanup(upstream.Close)

	user := model.User{
		Username: "manual-default-root", Password: "password", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
	}
	require.NoError(t, db.Create(&user).Error)
	priority := int64(80)
	weight := uint(50)
	channel := model.Channel{
		Id: 2503, Type: constant.ChannelTypeOpenAI, Key: "sk-manual-default", Name: "manual default",
		Status: common.ChannelStatusEnabled, BaseURL: common.GetPointer(upstream.URL),
		Models: "model-a", Group: "default", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)

	defaultRecorder := httptest.NewRecorder()
	defaultContext, _ := gin.CreateTestContext(defaultRecorder)
	defaultContext.Request = httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/channel/test/%d?model=model-a", channel.Id),
		nil,
	)
	defaultContext.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", channel.Id)}}
	defaultContext.Set("id", user.Id)
	TestChannel(defaultContext)
	assert.Equal(t, http.StatusOK, defaultRecorder.Code)
	var defaultResponse struct {
		Success bool `json:"success"`
		Data    struct {
			FirstTokenMs     *float64 `json:"first_token_ms"`
			UsageAvailable   bool     `json:"usage_available"`
			InputTokens      int      `json:"input_tokens"`
			OutputTokens     int      `json:"output_tokens"`
			TotalTokens      int      `json:"total_tokens"`
			CachedTokens     int      `json:"cached_tokens"`
			CacheWriteTokens int      `json:"cache_write_tokens"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(defaultRecorder.Body.Bytes(), &defaultResponse))
	assert.True(t, defaultResponse.Success, defaultRecorder.Body.String())
	require.NotNil(t, defaultResponse.Data.FirstTokenMs)
	assert.GreaterOrEqual(t, *defaultResponse.Data.FirstTokenMs, 0.0)
	assert.True(t, defaultResponse.Data.UsageAvailable)
	assert.Equal(t, 1, defaultResponse.Data.InputTokens)
	assert.Equal(t, 1, defaultResponse.Data.OutputTokens)
	assert.Equal(t, 2, defaultResponse.Data.TotalTokens)
	assert.Zero(t, defaultResponse.Data.CachedTokens)
	assert.Zero(t, defaultResponse.Data.CacheWriteTokens)

	autoRecorder := httptest.NewRecorder()
	autoContext, _ := gin.CreateTestContext(autoRecorder)
	autoContext.Request = httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/channel/test/%d?model=model-a&endpoint_type=auto&stream=false", channel.Id),
		nil,
	)
	autoContext.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", channel.Id)}}
	autoContext.Set("id", user.Id)
	TestChannel(autoContext)
	assert.Equal(t, http.StatusOK, autoRecorder.Code)
	var autoResponse struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(autoRecorder.Body.Bytes(), &autoResponse))
	assert.True(t, autoResponse.Success, autoRecorder.Body.String())
	assert.Equal(t, 2, requestCount)
}
