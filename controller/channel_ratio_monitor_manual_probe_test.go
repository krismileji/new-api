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
	assert.Equal(t, int64(3), reloadedSample.SampleCount)
	assert.Equal(t, int64(1), reloadedSample.SuccessCount)
	assert.Equal(t, int64(2), reloadedSample.FailureDurationSampleCount)
	require.NotNil(t, reloadedSample.AverageFailureDurationMs)
	assert.InDelta(t, 675, *reloadedSample.AverageFailureDurationMs, 1e-9)
	require.NoError(t, db.Find(&samples).Error)
	require.Len(t, samples, 1)

	var logCount int64
	require.NoError(t, db.Model(&model.Log{}).Count(&logCount).Error)
	assert.Zero(t, logCount)
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
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success, recorder.Body.String())

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

	now := common.GetTimestamp()
	_, err := model.AggregateChannelMonitorMinuteRange(context.Background(), now-120, now+120)
	require.NoError(t, err)
	var minuteMetricCount int64
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteMetric{}).Count(&minuteMetricCount).Error)
	assert.Zero(t, minuteMetricCount)
}

func TestManualChannelTestDefaultsToStreamingResponsesAndAllowsAutomaticNonStreaming(t *testing.T) {
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
			assert.Equal(t, "/v1/responses", r.URL.Path)
			var request dto.OpenAIResponsesRequest
			if !assert.NoError(t, common.Unmarshal(body, &request)) {
				return
			}
			if assert.NotNil(t, request.Stream) {
				assert.True(t, *request.Stream)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, err = w.Write([]byte(strings.Join([]string{
				`data: {"type":"response.created","response":{"id":"resp-manual-default","model":"model-a","created_at":1}}`,
				`data: {"type":"response.output_text.delta","delta":"ok"}`,
				`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
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
	}
	require.NoError(t, common.Unmarshal(defaultRecorder.Body.Bytes(), &defaultResponse))
	assert.True(t, defaultResponse.Success, defaultRecorder.Body.String())

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
