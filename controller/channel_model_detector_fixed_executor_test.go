package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureChannelModelDetectorStreamUsage(t *testing.T) {
	stream := true
	nonStream := false

	tests := []struct {
		name          string
		request       any
		wantUsage     bool
		wantObfuscate bool
	}{
		{
			name:      "streamed chat request enables usage",
			request:   &dto.GeneralOpenAIRequest{Stream: &stream},
			wantUsage: true,
		},
		{
			name:          "streamed chat request preserves existing options",
			request:       &dto.GeneralOpenAIRequest{Stream: &stream, StreamOptions: &dto.StreamOptions{IncludeObfuscation: true}},
			wantUsage:     true,
			wantObfuscate: true,
		},
		{
			name:    "non streamed chat request is unchanged",
			request: &dto.GeneralOpenAIRequest{Stream: &nonStream},
		},
		{
			name:    "native responses request is unchanged",
			request: &dto.OpenAIResponsesRequest{Stream: &stream},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ensureChannelModelDetectorStreamUsage(test.request)
			responsesRequest, isResponsesRequest := got.(*dto.OpenAIResponsesRequest)
			if isResponsesRequest {
				assert.Nil(t, responsesRequest.StreamOptions)
				return
			}
			chatRequest, ok := got.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)
			if test.wantUsage || test.wantObfuscate {
				require.NotNil(t, chatRequest.StreamOptions)
			}
			if chatRequest.StreamOptions != nil {
				assert.Equal(t, test.wantUsage, chatRequest.StreamOptions.IncludeUsage)
				assert.Equal(t, test.wantObfuscate, chatRequest.StreamOptions.IncludeObfuscation)
			}
		})
	}
}

func TestChannelModelDetectorFixedExecutorCostBoundary(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)
	originalStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalStreamingTimeout })
	require.NoError(t, db.AutoMigrate(
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
	))

	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"detector-fixed-model":0.01}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	pricingUser := model.User{
		Id: 701, Username: "detector-pricing", Password: "not-used-in-test", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: 987654, UsedQuota: 1234, RequestCount: 19,
	}
	require.NoError(t, db.Create(&pricingUser).Error)

	tests := []struct {
		name             string
		responseBody     string
		responseStatus   int
		contentType      string
		requestBody      string
		baseURL          func(string) string
		wantErr          bool
		wantRequestCount int64
		wantDispatch     string
		wantSettlement   string
		wantUsageSource  string
		wantDailyCost    int64
	}{
		{
			name:             "authoritative usage settles after real http dispatch",
			responseBody:     "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-settled\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"detector-fixed-model\",\"output\":[],\"usage\":{\"input_tokens\":4,\"output_tokens\":2,\"total_tokens\":6}}}\n\ndata: [DONE]\n\n",
			contentType:      "text/event-stream",
			requestBody:      `{"model":"detector-fixed-model","input":"hello","stream":true}`,
			baseURL:          func(serverURL string) string { return serverURL },
			wantRequestCount: 1,
			wantDispatch:     model.ChannelModelDetectionDispatchDispatched,
			wantSettlement:   model.ChannelModelDetectionSettlementSettled,
			wantUsageSource:  model.ChannelModelDetectionUsageUpstreamAuthoritative,
			wantDailyCost:    1,
		},
		{
			name:             "chat-style usage aliases settle after real http dispatch",
			responseBody:     "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-chat-alias\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"detector-fixed-model\",\"output\":[],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2,\"total_tokens\":6}}}\n\ndata: [DONE]\n\n",
			contentType:      "text/event-stream",
			requestBody:      `{"model":"detector-fixed-model","input":"hello","stream":true}`,
			baseURL:          func(serverURL string) string { return serverURL },
			wantRequestCount: 1,
			wantDispatch:     model.ChannelModelDetectionDispatchDispatched,
			wantSettlement:   model.ChannelModelDetectionSettlementSettled,
			wantUsageSource:  model.ChannelModelDetectionUsageUpstreamAuthoritative,
			wantDailyCost:    1,
		},
		{
			name:             "missing usage stays unresolved without an estimated cost",
			responseBody:     "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-no-usage\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"detector-fixed-model\",\"output\":[]}}\n\ndata: [DONE]\n\n",
			contentType:      "text/event-stream",
			requestBody:      `{"model":"detector-fixed-model","input":"hello","stream":true}`,
			baseURL:          func(serverURL string) string { return serverURL },
			wantRequestCount: 1,
			wantDispatch:     model.ChannelModelDetectionDispatchDispatched,
			wantSettlement:   model.ChannelModelDetectionSettlementUnresolved,
			wantUsageSource:  model.ChannelModelDetectionUsageUnavailable,
			wantDailyCost:    1,
		},
		{
			name:             "upstream http error stays unresolved without an estimated cost",
			responseBody:     `{"error":{"message":"upstream failed"}}`,
			responseStatus:   http.StatusInternalServerError,
			baseURL:          func(serverURL string) string { return serverURL },
			wantErr:          true,
			wantRequestCount: 1,
			wantDispatch:     model.ChannelModelDetectionDispatchDispatched,
			wantSettlement:   model.ChannelModelDetectionSettlementUnresolved,
			wantUsageSource:  model.ChannelModelDetectionUsageUnavailable,
			wantDailyCost:    1,
		},
		{
			name:             "invalid upstream url never crosses transport",
			responseBody:     `{}`,
			baseURL:          func(string) string { return "not-a-valid-upstream-url" },
			wantErr:          true,
			wantRequestCount: 0,
			wantDispatch:     model.ChannelModelDetectionDispatchNotStarted,
			wantSettlement:   model.ChannelModelDetectionSettlementNotApplicable,
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var upstreamRequests atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				upstreamRequests.Add(1)
				assert.Equal(t, "/v1/responses", request.URL.Path)
				assert.Equal(t, "Bearer detector-upstream-secret", request.Header.Get("Authorization"))
				contentType := test.contentType
				if contentType == "" {
					contentType = "application/json"
				}
				w.Header().Set("Content-Type", contentType)
				status := test.responseStatus
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				_, err := w.Write([]byte(test.responseBody))
				assert.NoError(t, err)
			}))
			defer upstream.Close()

			channelID := 710 + index
			targetID := int64(810 + index)
			executionID := int64(910 + index)
			runID := "fixed-executor-run-" + string(rune('a'+index))
			baseURL := test.baseURL(upstream.URL)
			channel := model.Channel{
				Id: channelID, Type: constant.ChannelTypeOpenAI, Key: "detector-upstream-secret",
				Status: common.ChannelStatusEnabled, Name: test.name, BaseURL: &baseURL,
				Models: "detector-fixed-model", Group: "default",
			}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: channelID, Ratio: 0.8, UpdatedTime: common.GetTimestamp(),
			}).Error)
			run := model.ChannelModelDetectionRun{
				RunId: runID, ChannelId: channelID, Trigger: model.ChannelModelDetectionTriggerManual,
				Preset: model.ChannelModelDetectionPresetLow, PricingContextUserId: pricingUser.Id,
			}
			require.NoError(t, db.Create(&run).Error)
			execution := model.ChannelModelDetectionExecution{
				Id: executionID, RunId: runID, TargetKey: "target", TargetId: targetID, ChannelId: channelID,
				RequestModel: "detector-fixed-model", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
				Preset: model.ChannelModelDetectionPresetLow,
			}
			require.NoError(t, db.Create(&execution).Error)

			requestBody := test.requestBody
			if requestBody == "" {
				requestBody = `{"model":"detector-fixed-model","input":"hello"}`
			}
			result, err := NewChannelModelDetectorFixedExecutor(db).ExecuteChannelModelDetectorAttempt(context.Background(), service.ChannelModelDetectorRelayExecution{
				Source: service.ChannelModelDetectorRequestSource, RunID: runID, TargetID: targetID,
				ExecutionID: executionID, ChannelID: channelID, RequestModel: execution.RequestModel,
				ClaimedModel: execution.ClaimedModel, Preset: execution.Preset, DetectorRequestID: "detector-request-" + runID,
				AttemptNo: 1, RequestBody: []byte(requestBody),
			})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.wantRequestCount, upstreamRequests.Load())
			assert.Equal(t, test.wantDispatch == model.ChannelModelDetectionDispatchDispatched, result.Dispatched)

			var event model.ChannelModelDetectionCostEvent
			require.NoError(t, db.Where("execution_id = ?", executionID).First(&event).Error)
			assert.Equal(t, test.wantDispatch, event.DispatchState)
			assert.Equal(t, test.wantSettlement, event.SettlementStatus)
			if test.wantUsageSource != "" {
				assert.Equal(t, test.wantUsageSource, event.UsageSource)
			}
			assert.Zero(t, event.EstimatedQuota)
			assert.Nil(t, event.EstimatedCostNanoCNY)
			assert.NotContains(t, event.UpstreamKeyId, channel.Key)
			assert.NotContains(t, event.UpstreamKeyDisplay, channel.Key)

			var storedUser model.User
			require.NoError(t, db.First(&storedUser, pricingUser.Id).Error)
			assert.Equal(t, pricingUser.Quota, storedUser.Quota)
			assert.Equal(t, pricingUser.UsedQuota, storedUser.UsedQuota)
			assert.Equal(t, pricingUser.RequestCount, storedUser.RequestCount)
			var dailyCostCount int64
			require.NoError(t, db.Model(&model.ChannelDailyCost{}).Where("channel_id = ?", channelID).Count(&dailyCostCount).Error)
			assert.Equal(t, test.wantDailyCost, dailyCostCount)
			if test.wantDailyCost > 0 {
				var dailyCost model.ChannelDailyCost
				require.NoError(t, db.Where("channel_id = ?", channelID).First(&dailyCost).Error)
				if test.wantSettlement == model.ChannelModelDetectionSettlementSettled {
					assert.Positive(t, dailyCost.CostNanoCNY)
					assert.Equal(t, dailyCost.CostNanoCNY, dailyCost.ModelDetectionCostNanoCNY)
					assert.Equal(t, int64(1), dailyCost.SettledCount)
					assert.Zero(t, dailyCost.UnresolvedCount)
				} else {
					assert.Zero(t, dailyCost.CostNanoCNY)
					assert.Zero(t, dailyCost.ModelDetectionCostNanoCNY)
					assert.Zero(t, dailyCost.SettledCount)
					assert.Equal(t, int64(1), dailyCost.UnresolvedCount)
				}
			}
		})
	}
}
