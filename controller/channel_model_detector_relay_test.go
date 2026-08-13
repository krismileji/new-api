package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelModelDetectorRelayEndpointExecutor struct {
	executions []service.ChannelModelDetectorRelayExecution
	result     service.ChannelModelDetectorRelayUpstreamResult
	err        error
}

type channelModelDetectorRelayEndpointRunner struct {
	result service.ChannelModelDetectorRelayResult
	err    error
	seen   []service.ChannelModelDetectorRelayRequest
}

func (runner *channelModelDetectorRelayEndpointRunner) Execute(_ context.Context, request service.ChannelModelDetectorRelayRequest) (service.ChannelModelDetectorRelayResult, error) {
	runner.seen = append(runner.seen, request)
	return runner.result, runner.err
}

func (executor *channelModelDetectorRelayEndpointExecutor) ExecuteChannelModelDetectorAttempt(_ context.Context, execution service.ChannelModelDetectorRelayExecution) (service.ChannelModelDetectorRelayUpstreamResult, error) {
	executor.executions = append(executor.executions, execution)
	return executor.result, executor.err
}

func TestChannelModelDetectorRelayEndpointPassesThroughJSONAndSSE(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "json",
			contentType: "application/json",
			body:        `{"id":"resp-json","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`,
		},
		{
			name:        "sse",
			contentType: "text/event-stream; charset=utf-8",
			body:        "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":4,\"output_tokens\":2,\"total_tokens\":6}}}\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, credential := newChannelModelDetectorEndpointCredential(t, 2)
			runner := &channelModelDetectorRelayEndpointRunner{result: service.ChannelModelDetectorRelayResult{Upstream: service.ChannelModelDetectorRelayUpstreamResult{
				StatusCode: http.StatusOK, ContentType: test.contentType,
				ResponseBody: []byte(test.body), UsagePayload: []byte(test.body),
			}}}
			handler := NewChannelModelDetectorRelayHandler(runner)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/internal/model-detector/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol","input":"hello"}`))
			ctx.Request.Header.Set("Authorization", "Bearer "+credential.BearerToken())
			ctx.Request.Header.Set("Content-Type", "application/json")
			handler.PostChannelModelDetectorRelay(ctx)

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, test.contentType, recorder.Header().Get("Content-Type"))
			assert.Equal(t, test.body, recorder.Body.String())
			require.Len(t, runner.seen, 1)
			assert.Equal(t, credential.BearerToken(), runner.seen[0].BearerToken)
			assert.NotEmpty(t, runner.seen[0].DetectorRequestID)
		})
	}
}

func TestChannelModelDetectorRelayEndpointGeneratesDistinctRequestIDs(t *testing.T) {
	runner := &channelModelDetectorRelayEndpointRunner{}
	handler := NewChannelModelDetectorRelayHandler(runner)

	for range 2 {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/internal/model-detector/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
		ctx.Request.Header.Set("Authorization", "Bearer relay-token")
		ctx.Request.Header.Set("Content-Type", "application/json")
		handler.PostChannelModelDetectorRelay(ctx)
		assert.Equal(t, http.StatusOK, recorder.Code)
	}

	require.Len(t, runner.seen, 2)
	assert.NotEmpty(t, runner.seen[0].DetectorRequestID)
	assert.NotEmpty(t, runner.seen[1].DetectorRequestID)
	assert.NotEqual(t, runner.seen[0].DetectorRequestID, runner.seen[1].DetectorRequestID)
}

func TestChannelModelDetectorRelayEndpointMapsRelayErrorsWithoutLeakingDetails(t *testing.T) {
	_, credential := newChannelModelDetectorEndpointCredential(t, 2)
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "invalid request", err: service.ErrChannelModelDetectorRelayInvalidRequest, wantStatus: http.StatusBadRequest},
		{name: "invalid token", err: service.ErrChannelModelDetectorTokenInvalid, wantStatus: http.StatusUnauthorized},
		{name: "busy", err: service.ErrChannelModelDetectorRelayBusy, wantStatus: http.StatusTooManyRequests},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &channelModelDetectorRelayEndpointRunner{err: test.err}
			handler := NewChannelModelDetectorRelayHandler(runner)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/internal/model-detector/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
			ctx.Request.Header.Set("Authorization", "Bearer "+credential.BearerToken())
			ctx.Request.Header.Set("Content-Type", "application/json")
			handler.PostChannelModelDetectorRelay(ctx)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.NotContains(t, recorder.Body.String(), credential.BearerToken())
			assert.NotContains(t, recorder.Body.String(), credential.Claims.Nonce)
		})
	}
}

func TestChannelModelDetectorRelayEndpointRejectsAuthContentTypeAndOversizeWithoutLeakingSecrets(t *testing.T) {
	_, credential := newChannelModelDetectorEndpointCredential(t, 2)
	runner := &channelModelDetectorRelayEndpointRunner{}
	handler := NewChannelModelDetectorRelayHandler(runner)
	handler.MaxRequestBytes = 64

	tests := []struct {
		name        string
		auth        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "missing bearer", contentType: "application/json", body: `{}`, wantStatus: http.StatusUnauthorized},
		{name: "wrong content type", auth: "Bearer " + credential.BearerToken(), contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "oversized", auth: "Bearer " + credential.BearerToken(), contentType: "application/json", body: strings.Repeat("x", 65), wantStatus: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/internal/model-detector/v1/responses", strings.NewReader(test.body))
			ctx.Request.Header.Set("Authorization", test.auth)
			ctx.Request.Header.Set("Content-Type", test.contentType)
			handler.PostChannelModelDetectorRelay(ctx)

			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Contains(t, recorder.Body.String(), `"type":"model_detector_relay_error"`)
			assert.NotContains(t, recorder.Body.String(), credential.BearerToken())
			assert.NotContains(t, recorder.Body.String(), credential.Claims.Nonce)
			assert.NotContains(t, recorder.Body.String(), "channel-secret")
		})
	}
	assert.Empty(t, runner.seen)
}

func TestChannelModelDetectorRelayEndpointSanitizesExecutionErrors(t *testing.T) {
	_, credential := newChannelModelDetectorEndpointCredential(t, 2)
	runner := &channelModelDetectorRelayEndpointRunner{err: errors.New("upstream key channel-secret leaked")}
	handler := NewChannelModelDetectorRelayHandler(runner)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/internal/model-detector/v1/responses", strings.NewReader(`{"model":"gpt-5.6-sol"}`))
	ctx.Request.Header.Set("Authorization", "Bearer "+credential.BearerToken())
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler.PostChannelModelDetectorRelay(ctx)
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "模型检测渠道请求失败")
	assert.NotContains(t, recorder.Body.String(), "channel-secret")
}

func TestChannelModelDetectorChannelAllowed(t *testing.T) {
	tests := []struct {
		name    string
		trigger string
		status  int
		allowed bool
	}{
		{name: "scheduled enabled", trigger: model.ChannelModelDetectionTriggerScheduled, status: common.ChannelStatusEnabled, allowed: true},
		{name: "scheduled manual disabled", trigger: model.ChannelModelDetectionTriggerScheduled, status: common.ChannelStatusManuallyDisabled, allowed: false},
		{name: "manual enabled", trigger: model.ChannelModelDetectionTriggerManual, status: common.ChannelStatusEnabled, allowed: true},
		{name: "manual manually disabled", trigger: model.ChannelModelDetectionTriggerManual, status: common.ChannelStatusManuallyDisabled, allowed: true},
		{name: "manual auto disabled", trigger: model.ChannelModelDetectionTriggerManual, status: common.ChannelStatusAutoDisabled, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.allowed, channelModelDetectorChannelAllowed(test.trigger, test.status))
		})
	}
}

func newChannelModelDetectorEndpointCredential(t *testing.T, attempts int) (*service.ChannelModelDetectorTokenStore, service.ChannelModelDetectorCredential) {
	t.Helper()
	store, err := service.NewChannelModelDetectorTokenStore()
	require.NoError(t, err)
	credential, err := store.Issue(service.ChannelModelDetectorTokenSpec{
		RunID: "endpoint-run", TargetID: 11, ExecutionID: 1011, ChannelID: 23,
		RequestModel: "channel-alias", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
		Preset: model.ChannelModelDetectionPresetLow, RelayBaseURL: "http://127.0.0.1/internal/model-detector/v1",
		MaxHTTPAttempts: attempts, ExpiresAt: time.Now().UTC().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	return store, credential
}
