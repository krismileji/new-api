package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelModelDetectorRelayExecutorStub struct {
	mu         sync.Mutex
	executions []ChannelModelDetectorRelayExecution
	result     ChannelModelDetectorRelayUpstreamResult
	err        error
}

func (stub *channelModelDetectorRelayExecutorStub) ExecuteChannelModelDetectorAttempt(_ context.Context, execution ChannelModelDetectorRelayExecution) (ChannelModelDetectorRelayUpstreamResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.executions = append(stub.executions, execution)
	return stub.result, stub.err
}

type channelModelDetectorRelayLeaseStub struct {
	mu       sync.Mutex
	released int
}

func (stub *channelModelDetectorRelayLeaseStub) Release() {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.released++
}

func TestChannelModelDetectorRelayUsesOnlyCredentialChannelAndRewritesExactClaimedModel(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential := issueTestChannelModelDetectorCredential(t, store, now, 2)
	executor := &channelModelDetectorRelayExecutorStub{result: ChannelModelDetectorRelayUpstreamResult{
		StatusCode:   200,
		Dispatched:   true,
		ResponseBody: []byte(`{"id":"resp-1","usage":{"input_tokens":12,"output_tokens":4,"total_tokens":16}}`),
	}}
	lease := &channelModelDetectorRelayLeaseStub{}
	var acquiredChannelID int
	relay, err := newChannelModelDetectorRelay(store, executor, func(_ context.Context, channelID int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		acquiredChannelID = channelID
		return lease, true, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)

	result, err := relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken:       credential.BearerToken(),
		DetectorRequestID: "detector-request-1",
		Body:              []byte(`{"model":"gpt-5.6-sol","input":"hello","stream":false}`),
	})
	require.NoError(t, err)
	require.Len(t, executor.executions, 1)
	execution := executor.executions[0]
	assert.Equal(t, 23, acquiredChannelID)
	assert.Equal(t, 1, lease.released)
	assert.Equal(t, ChannelModelDetectorRequestSource, execution.Source)
	assert.Equal(t, "run-1", execution.RunID)
	assert.EqualValues(t, 11, execution.TargetID)
	assert.EqualValues(t, 1011, execution.ExecutionID)
	assert.Equal(t, 23, execution.ChannelID)
	assert.Equal(t, "channel-alias", execution.RequestModel)
	assert.Equal(t, model.ChannelModelDetectionClaimedModelSol, execution.ClaimedModel)
	assert.Equal(t, model.ChannelModelDetectionPresetLow, execution.Preset)
	assert.Equal(t, "http://127.0.0.1:3000/internal/model-detector", execution.RelayBaseURL)
	assert.Equal(t, "detector-request-1", execution.DetectorRequestID)
	assert.Equal(t, 1, execution.AttemptNo)
	assert.NotContains(t, string(execution.RequestBody), credential.BearerToken())
	assert.NotContains(t, string(execution.RequestBody), model.ChannelModelDetectionClaimedModelSol)

	var body map[string]any
	require.NoError(t, common.Unmarshal(execution.RequestBody, &body))
	assert.Equal(t, "channel-alias", body["model"])
	assert.Equal(t, "hello", body["input"])
	assert.Equal(t, ChannelModelDetectorUsage{
		Available: true, Source: model.ChannelModelDetectionUsageUpstreamAuthoritative,
		InputTokens: 12, OutputTokens: 4, TotalTokens: 16,
	}, result.Usage)
	publicResult, err := common.Marshal(result)
	require.NoError(t, err)
	assert.NotContains(t, string(publicResult), credential.Claims.Nonce)
	assert.NotContains(t, string(publicResult), credential.Claims.RelayBaseURL)
}

func TestChannelModelDetectorRelayRejectsChannelSelectionModelMismatchAndReplayBeforeExecutor(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		requestID string
		wantErr   error
	}{
		{name: "wrong claimed model", body: `{"model":"gpt-5.6-terra","input":"x"}`, requestID: "wrong-model", wantErr: ErrChannelModelDetectorTokenModelMismatch},
		{name: "channel id", body: `{"model":"gpt-5.6-sol","channel_id":99}`, requestID: "channel-id", wantErr: ErrChannelModelDetectorRelayInvalidRequest},
		{name: "mixed case channel id", body: `{"model":"gpt-5.6-sol","Channel_ID":99}`, requestID: "mixed-channel-id", wantErr: ErrChannelModelDetectorRelayInvalidRequest},
		{name: "base url", body: `{"model":"gpt-5.6-sol","base_url":"https://attacker.example"}`, requestID: "base-url", wantErr: ErrChannelModelDetectorRelayInvalidRequest},
		{name: "missing model", body: `{"input":"x"}`, requestID: "missing-model", wantErr: ErrChannelModelDetectorRelayInvalidRequest},
		{name: "missing request id", body: `{"model":"gpt-5.6-sol","input":"x"}`, requestID: "", wantErr: ErrChannelModelDetectorRelayInvalidRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0).UTC()
			store := newTestChannelModelDetectorTokenStore(t, &now)
			credential := issueTestChannelModelDetectorCredential(t, store, now, 3)
			executor := &channelModelDetectorRelayExecutorStub{}
			relay, err := newChannelModelDetectorRelay(store, executor, func(context.Context, int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
				return &channelModelDetectorRelayLeaseStub{}, true, ChannelConcurrencyStatus{}, nil
			})
			require.NoError(t, err)
			_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
				BearerToken: credential.BearerToken(), DetectorRequestID: test.requestID, Body: []byte(test.body),
			})
			assert.ErrorIs(t, err, test.wantErr)
			assert.Empty(t, executor.executions)
		})
	}

	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential := issueTestChannelModelDetectorCredential(t, store, now, 2)
	executor := &channelModelDetectorRelayExecutorStub{}
	relay, err := newChannelModelDetectorRelay(store, executor, func(context.Context, int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		return &channelModelDetectorRelayLeaseStub{}, true, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)
	request := ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "replay-id", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	}
	_, err = relay.Execute(context.Background(), request)
	require.NoError(t, err)
	_, err = relay.Execute(context.Background(), request)
	assert.ErrorIs(t, err, ErrChannelModelDetectorTokenReplay)
	assert.Len(t, executor.executions, 1)
}

func TestChannelModelDetectorRelayHonorsConcurrencyAndReleasesOnExecutorError(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential := issueTestChannelModelDetectorCredential(t, store, now, 2)
	executor := &channelModelDetectorRelayExecutorStub{}
	relay, err := newChannelModelDetectorRelay(store, executor, func(_ context.Context, channelID int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		assert.Equal(t, 23, channelID)
		return nil, false, ChannelConcurrencyStatus{Active: 2, Limit: 2}, nil
	})
	require.NoError(t, err)
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "busy", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	})
	assert.ErrorIs(t, err, ErrChannelModelDetectorRelayBusy)
	assert.Empty(t, executor.executions)

	credential = issueTestChannelModelDetectorCredential(t, store, now, 2)
	executor.err = errors.New("upstream failed")
	lease := &channelModelDetectorRelayLeaseStub{}
	relay, err = newChannelModelDetectorRelay(store, executor, func(context.Context, int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		return lease, true, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)
	_, err = relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "upstream-error", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	})
	assert.EqualError(t, err, "upstream failed")
	assert.Equal(t, 1, lease.released)
}

func TestChannelModelDetectorRelayPassesOneFixedChannelAttemptWithoutBillingSurface(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	store := newTestChannelModelDetectorTokenStore(t, &now)
	credential := issueTestChannelModelDetectorCredential(t, store, now, 2)
	executor := &channelModelDetectorRelayExecutorStub{result: ChannelModelDetectorRelayUpstreamResult{
		Dispatched: true,
		Usage: &ChannelModelDetectorUsage{
			Available: true, Source: model.ChannelModelDetectionUsageUpstreamAuthoritative,
			InputTokens: 1, OutputTokens: 2, TotalTokens: 3,
		},
	}}
	relay, err := newChannelModelDetectorRelay(store, executor, func(context.Context, int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		return &channelModelDetectorRelayLeaseStub{}, true, ChannelConcurrencyStatus{}, nil
	})
	require.NoError(t, err)
	result, err := relay.Execute(context.Background(), ChannelModelDetectorRelayRequest{
		BearerToken: credential.BearerToken(), DetectorRequestID: "single-attempt", Body: []byte(`{"model":"gpt-5.6-sol"}`),
	})
	require.NoError(t, err)
	assert.Len(t, executor.executions, 1, "service 协调器不得跨渠道或自动重试")
	assert.Equal(t, 23, executor.executions[0].ChannelID)
	assert.EqualValues(t, 11, executor.executions[0].TargetID)
	assert.EqualValues(t, 1011, executor.executions[0].ExecutionID)
	assert.Equal(t, int64(3), result.Usage.TotalTokens)

	executionJSON, err := common.Marshal(executor.executions[0])
	require.NoError(t, err)
	executionText := strings.ToLower(string(executionJSON))
	assert.NotContains(t, executionText, "user")
	assert.NotContains(t, executionText, "tokenid")
	assert.NotContains(t, executionText, "subscription")
	assert.NotContains(t, executionText, "billing")
}

func TestChannelModelDetectorRelayUsageNormalization(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    ChannelModelDetectorUsage
	}{
		{
			name:    "responses envelope",
			payload: `{"id":"resp","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`,
			want:    authoritativeChannelModelDetectorUsage(10, 5),
		},
		{
			name:    "chat envelope",
			payload: `{"id":"chat","usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`,
			want:    authoritativeChannelModelDetectorUsage(7, 3),
		},
		{
			name:    "responses event",
			payload: `{"type":"response.completed","response":{"usage":{"input_tokens":20,"output_tokens":4,"total_tokens":24}}}`,
			want:    authoritativeChannelModelDetectorUsage(20, 4),
		},
		{
			name:    "claude message envelope",
			payload: `{"type":"message_start","message":{"usage":{"input_tokens":8,"output_tokens":0}}}`,
			want:    authoritativeChannelModelDetectorUsage(8, 0),
		},
		{
			name:    "direct chat usage computes total",
			payload: `{"prompt_tokens":2,"completion_tokens":1}`,
			want:    authoritativeChannelModelDetectorUsage(2, 1),
		},
		{
			name:    "responses dto envelope ignores zero chat aliases",
			payload: `{"usage":{"prompt_tokens":0,"completion_tokens":0,"input_tokens":9,"output_tokens":3,"total_tokens":12}}`,
			want:    authoritativeChannelModelDetectorUsage(9, 3),
		},
		{
			name:    "chat dto envelope ignores zero responses aliases",
			payload: `{"usage":{"prompt_tokens":9,"completion_tokens":3,"input_tokens":0,"output_tokens":0,"total_tokens":12}}`,
			want:    authoritativeChannelModelDetectorUsage(9, 3),
		},
		{
			name: "responses sse",
			payload: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":6,\"output_tokens\":2,\"total_tokens\":8}}}\n\n" +
				"data: [DONE]\n\n",
			want: authoritativeChannelModelDetectorUsage(6, 2),
		},
		{
			name: "claude split usage sse",
			payload: "event: message_start\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8}}}\n\n" +
				"event: message_delta\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n",
			want: authoritativeChannelModelDetectorUsage(8, 3),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, err := NormalizeChannelModelDetectorUsage([]byte(test.payload))
			require.NoError(t, err)
			assert.Equal(t, test.want, usage)
		})
	}
}

func TestChannelModelDetectorRelayUsageRejectsInvalidOrMissingAccounting(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr error
	}{
		{name: "missing", payload: `{"id":"resp"}`, wantErr: ErrChannelModelDetectorUsageUnavailable},
		{name: "negative", payload: `{"usage":{"input_tokens":-1,"output_tokens":2,"total_tokens":1}}`, wantErr: ErrChannelModelDetectorUsageInvalid},
		{name: "mismatched total", payload: `{"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":99}}`, wantErr: ErrChannelModelDetectorUsageInvalid},
		{name: "conflicting aliases", payload: `{"usage":{"input_tokens":1,"prompt_tokens":2,"output_tokens":3,"total_tokens":4}}`, wantErr: ErrChannelModelDetectorUsageInvalid},
		{name: "non json", payload: `not-json`, wantErr: ErrChannelModelDetectorUsageInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeChannelModelDetectorUsage([]byte(test.payload))
			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestChannelModelDetectorRelayDTOUsageNormalizesResponsesAndChatFields(t *testing.T) {
	responses, err := NormalizeChannelModelDetectorDTOUsage(&relaydto.Usage{InputTokens: 8, OutputTokens: 2, TotalTokens: 10})
	require.NoError(t, err)
	assert.Equal(t, authoritativeChannelModelDetectorUsage(8, 2), responses)

	chat, err := NormalizeChannelModelDetectorDTOUsage(&relaydto.Usage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9})
	require.NoError(t, err)
	assert.Equal(t, authoritativeChannelModelDetectorUsage(5, 4), chat)

	_, err = NormalizeChannelModelDetectorDTOUsage(nil)
	assert.ErrorIs(t, err, ErrChannelModelDetectorUsageUnavailable)

	_, err = NormalizeChannelModelDetectorDTOUsage(&relaydto.Usage{InputTokens: 1, PromptTokens: 2, OutputTokens: 1, TotalTokens: 2})
	assert.ErrorIs(t, err, ErrChannelModelDetectorUsageInvalid)

	_, err = NormalizeChannelModelDetectorDTOUsage(&relaydto.Usage{
		PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9,
		BillingUsage: relaydto.NewEstimatedGeminiChatBillingUsage(&relaydto.Usage{
			PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9,
		}),
	})
	assert.ErrorIs(t, err, ErrChannelModelDetectorUsageUnavailable)
}

func TestChannelModelDetectorRelayUsageNormalizesCacheDetailsAndMergesAliases(t *testing.T) {
	payload := `{"type":"response.completed","response":{"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":4,"cached_creation_tokens":2}}}}`
	authoritative, err := NormalizeChannelModelDetectorUsage([]byte(payload))
	require.NoError(t, err)
	assert.True(t, authoritative.InputTokenDetailsAvailable)
	assert.Equal(t, int64(4), authoritative.CachedTokens)
	assert.Equal(t, int64(2), authoritative.CachedCreationTokens)

	usage, err := MergeChannelModelDetectorAuthoritativeUsage(&relaydto.Usage{
		PromptTokens:     1,
		CompletionTokens: 1,
		TotalTokens:      2,
		BillingUsage: relaydto.NewEstimatedGeminiChatBillingUsage(&relaydto.Usage{
			PromptTokens:     1,
			CompletionTokens: 1,
			TotalTokens:      2,
		}),
	}, authoritative)
	require.NoError(t, err)
	assert.Equal(t, 9, usage.PromptTokens)
	assert.Equal(t, 3, usage.CompletionTokens)
	assert.Equal(t, 12, usage.TotalTokens)
	assert.Equal(t, 4, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 2, usage.PromptTokensDetails.CachedCreationTokens)
	assert.NotNil(t, usage.BillingUsage)
	assert.False(t, usage.BillingUsage.Estimated)
	assert.Equal(t, 9, usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
	assert.Equal(t, 3, usage.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
	assert.Equal(t, 12, usage.BillingUsage.GeminiUsageMetadata.TotalTokenCount)
}

func authoritativeChannelModelDetectorUsage(input, output int64) ChannelModelDetectorUsage {
	return ChannelModelDetectorUsage{
		Available: true, Source: model.ChannelModelDetectionUsageUpstreamAuthoritative,
		InputTokens: input, OutputTokens: output, TotalTokens: input + output,
	}
}
