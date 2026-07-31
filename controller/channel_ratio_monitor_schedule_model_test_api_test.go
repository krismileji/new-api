package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelSmartScheduleModelTestAPIResponse struct {
	Success bool                                `json:"success"`
	Message string                              `json:"message"`
	Data    channelSmartScheduleModelTestResult `json:"data"`
}

func TestChannelSmartScheduleModelTestReturnsEveryRouteAndRecordsSelectedGroup(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	vipPolicy := manualProbeTestPolicy("vip", []string{"model-a"}, channelMonitorSmartScheduleSampleProbe)
	sharedPolicy := manualProbeTestPolicy("shared", []string{"model-a"}, channelMonitorSmartScheduleSampleProbe)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(
			t, vipPolicy, sharedPolicy,
		),
	})

	priority := int64(80)
	weight := uint(50)
	channels := []model.Channel{
		{Id: 2601, Type: constant.ChannelTypeOpenAI, Key: "success", Name: "成功渠道", Status: common.ChannelStatusEnabled, Models: "model-a", Group: "vip", Priority: &priority, Weight: &weight},
		{Id: 2602, Type: constant.ChannelTypeOpenAI, Key: "failure", Name: "失败渠道", Status: common.ChannelStatusEnabled, Models: "model-a", Group: "vip", Priority: &priority, Weight: &weight},
		{Id: 2603, Type: constant.ChannelTypeOpenAI, Key: "excluded", Name: "未参与渠道", Status: common.ChannelStatusEnabled, Models: "model-a", Group: "vip", Priority: &priority, Weight: &weight},
		{Id: 2604, Type: constant.ChannelTypeOpenAI, Key: "ability-disabled", Name: "路由禁用渠道", Status: common.ChannelStatusEnabled, Models: "model-a", Group: "vip", Priority: &priority, Weight: &weight},
		{Id: 2605, Type: constant.ChannelTypeOpenAI, Key: "channel-disabled", Name: "渠道禁用", Status: common.ChannelStatusManuallyDisabled, Models: "model-a", Group: "vip", Priority: &priority, Weight: &weight},
	}
	require.NoError(t, db.Create(&channels).Error)
	for _, channel := range channels {
		require.NoError(t, db.Create(&model.Ability{
			ChannelId: channel.Id,
			Group:     "vip",
			Model:     "model-a",
			Enabled:   channel.Id != 2604,
			Priority:  &priority,
			Weight:    weight,
		}).Error)
		require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
			ChannelId:        channel.Id,
			GroupName:        "vip",
			ModelName:        "model-a",
			ParticipationSet: true,
			Excluded:         channel.Id == 2603,
		}).Error)
	}
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 2601, Group: "shared", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 2601, GroupName: "shared", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	type capturedCall struct {
		channelId      int
		group          string
		endpointType   string
		stream         bool
		scheduledProbe bool
	}
	var callsMutex sync.Mutex
	calls := make([]capturedCall, 0, 2)
	firstTokenMs := 135.5
	tps := 42.25
	executor := func(
		ctx context.Context,
		channel *model.Channel,
		_ int,
		modelName string,
		endpointType string,
		stream bool,
	) testResult {
		options, _ := ctx.Value(channelSmartScheduleProbeTestContextKey{}).(channelSmartScheduleProbeTestOptions)
		callsMutex.Lock()
		calls = append(calls, capturedCall{
			channelId:      channel.Id,
			group:          options.Group,
			endpointType:   endpointType,
			stream:         stream,
			scheduledProbe: options.ScheduledProbe,
		})
		callsMutex.Unlock()
		if channel.Id == 2602 {
			apiErr := types.NewError(errors.New("上游拒绝请求"), types.ErrorCodeBadResponse)
			return testResult{
				localErr:          apiErr,
				newAPIError:       apiErr,
				originalModelName: modelName,
			}
		}
		return testResult{
			originalModelName:         modelName,
			firstResponseMilliseconds: &firstTokenMs,
			tokensPerSecond:           &tps,
		}
	}

	ctx, recorder := newChannelMonitorControllerContext(
		t,
		http.MethodPost,
		"/api/channel_monitor/schedule/model-test",
		map[string]any{"group": "vip", "model": "model-a"},
	)
	serveChannelMonitorSmartScheduleModelTest(ctx, executor)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var response channelSmartScheduleModelTestAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
	assert.Equal(t, "vip", response.Data.Group)
	assert.Equal(t, "model-a", response.Data.Model)
	assert.True(t, response.Data.Stream)
	assert.Equal(t, "auto", response.Data.EndpointType)
	assert.Equal(t, 5, response.Data.Total)
	assert.Equal(t, 1, response.Data.Succeeded)
	assert.Equal(t, 1, response.Data.Failed)
	assert.Equal(t, 3, response.Data.Skipped)
	require.Len(t, response.Data.Results, 5)

	byChannel := make(map[int]channelSmartScheduleModelTestItem, len(response.Data.Results))
	for _, item := range response.Data.Results {
		byChannel[item.ChannelId] = item
	}
	assert.Equal(t, "success", byChannel[2601].Status)
	assert.True(t, byChannel[2601].Participates)
	assert.True(t, byChannel[2601].Available)
	require.NotNil(t, byChannel[2601].FirstTokenMs)
	assert.InDelta(t, firstTokenMs, *byChannel[2601].FirstTokenMs, 1e-9)
	require.NotNil(t, byChannel[2601].TPS)
	assert.InDelta(t, tps, *byChannel[2601].TPS, 1e-9)
	assert.Equal(t, "failure", byChannel[2602].Status)
	assert.Equal(t, string(types.ErrorCodeBadResponse), byChannel[2602].ErrorCode)
	assert.Equal(t, "上游拒绝请求", byChannel[2602].Error)
	assert.Equal(t, "skipped", byChannel[2603].Status)
	assert.False(t, byChannel[2603].Participates)
	assert.Equal(t, "渠道未参与智能调度", byChannel[2603].Error)
	assert.Equal(t, "skipped", byChannel[2604].Status)
	assert.False(t, byChannel[2604].Available)
	assert.Equal(t, "分组模型路由未启用", byChannel[2604].Error)
	assert.Equal(t, "skipped", byChannel[2605].Status)
	assert.False(t, byChannel[2605].Available)
	assert.Equal(t, "渠道未启用", byChannel[2605].Error)

	callsMutex.Lock()
	capturedCalls := append([]capturedCall(nil), calls...)
	callsMutex.Unlock()
	require.Len(t, capturedCalls, 2)
	calledChannels := make(map[int]capturedCall, len(capturedCalls))
	for _, call := range capturedCalls {
		calledChannels[call.channelId] = call
		assert.Equal(t, "vip", call.group)
		assert.Equal(t, "auto", call.endpointType)
		assert.True(t, call.stream)
		assert.False(t, call.scheduledProbe)
	}
	assert.Contains(t, calledChannels, 2601)
	assert.Contains(t, calledChannels, 2602)

	var successfulState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 2601, "vip", "model-a",
	).First(&successfulState).Error)
	assert.Equal(t, int64(1), successfulState.ProbeSampleCount)
	assert.Equal(t, int64(1), successfulState.ProbeSuccessCount)
	assert.Equal(t, int64(1), successfulState.ManualTestMetricsSince(0).SampleCount)
	var failedState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 2602, "vip", "model-a",
	).First(&failedState).Error)
	assert.Equal(t, int64(1), failedState.ProbeSampleCount)
	assert.Zero(t, failedState.ProbeSuccessCount)
	var unrelatedGroupState model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 2601, "shared", "model-a",
	).First(&unrelatedGroupState).Error)
	assert.Zero(t, unrelatedGroupState.ProbeSampleCount)

	callsMutex.Lock()
	calls = calls[:0]
	callsMutex.Unlock()
	retryCtx, retryRecorder := newChannelMonitorControllerContext(
		t,
		http.MethodPost,
		"/api/channel_monitor/schedule/model-test",
		map[string]any{
			"group": "vip", "model": "model-a", "stream": false,
			"endpoint_type": "openai", "channel_ids": []int{2601, 2601},
		},
	)
	serveChannelMonitorSmartScheduleModelTest(retryCtx, executor)
	var retryResponse channelSmartScheduleModelTestAPIResponse
	require.NoError(t, common.Unmarshal(retryRecorder.Body.Bytes(), &retryResponse))
	require.True(t, retryResponse.Success, retryResponse.Message)
	assert.False(t, retryResponse.Data.Stream)
	assert.Equal(t, "openai", retryResponse.Data.EndpointType)
	assert.Equal(t, 1, retryResponse.Data.Total)
	require.Len(t, retryResponse.Data.Results, 1)
	assert.Equal(t, 2601, retryResponse.Data.Results[0].ChannelId)
	callsMutex.Lock()
	require.Len(t, calls, 1)
	assert.False(t, calls[0].stream)
	assert.Equal(t, "openai", calls[0].endpointType)
	callsMutex.Unlock()
}

func TestChannelSmartScheduleModelTestValidatesRequestBeforeExecution(t *testing.T) {
	tests := []struct {
		name        string
		body        any
		wantMessage string
	}{
		{name: "group required", body: map[string]any{"model": "model-a"}, wantMessage: "分组不能为空"},
		{name: "model required", body: map[string]any{"group": "vip"}, wantMessage: "模型不能为空"},
		{name: "endpoint rejected", body: map[string]any{"group": "vip", "model": "model-a", "endpoint_type": "unknown"}, wantMessage: "不支持的测试端点类型"},
		{name: "channel id rejected", body: map[string]any{"group": "vip", "model": "model-a", "channel_ids": []int{0}}, wantMessage: "渠道 ID 必须大于 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newChannelMonitorControllerContext(
				t, http.MethodPost, "/api/channel_monitor/schedule/model-test", test.body,
			)
			serveChannelMonitorSmartScheduleModelTest(ctx, func(
				context.Context, *model.Channel, int, string, string, bool,
			) testResult {
				t.Fatal("executor must not be called for an invalid request")
				return testResult{}
			})
			var response channelSmartScheduleModelTestAPIResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.Equal(t, test.wantMessage, response.Message)
		})
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost, "/api/channel_monitor/schedule/model-test", strings.NewReader("{"),
	)
	serveChannelMonitorSmartScheduleModelTest(ctx, func(
		context.Context, *model.Channel, int, string, string, bool,
	) testResult {
		t.Fatal("executor must not be called for malformed JSON")
		return testResult{}
	})
	var malformedResponse channelSmartScheduleModelTestAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &malformedResponse))
	assert.False(t, malformedResponse.Success)
	assert.Equal(t, "请求参数格式错误", malformedResponse.Message)
}
