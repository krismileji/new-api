package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	ChannelModelDetectorRelayMaxRequestBytes = 4 << 20
	channelModelDetectorUsageEventMaxBytes   = 1 << 20

	ChannelModelDetectorRequestSource = "gpt56_model_detection"
)

var (
	ErrChannelModelDetectorRelayInvalidRequest = errors.New("模型检测内部 Relay 请求无效")
	ErrChannelModelDetectorRelayBusy           = errors.New("模型检测渠道并发已满")
	ErrChannelModelDetectorRelayUnavailable    = errors.New("模型检测固定渠道执行器不可用")
	ErrChannelModelDetectorUsageUnavailable    = errors.New("模型检测响应缺少可用 Usage")
	ErrChannelModelDetectorUsageInvalid        = errors.New("模型检测响应 Usage 无效")
)

// ChannelModelDetectorRelayRequest is deliberately narrow: the caller cannot
// provide a channel, upstream model, base URL, query, or arbitrary headers.
// Those values come exclusively from the authenticated in-memory credential.
type ChannelModelDetectorRelayRequest struct {
	BearerToken       string `json:"-"`
	DetectorRequestID string `json:"detector_request_id"`
	Body              []byte `json:"-"`
}

// ChannelModelDetectorRelayExecution is the immutable plan handed to the
// fixed-channel transport. RequestBody has already had the exact claimed model
// replaced by RequestModel. BearerToken is never forwarded to this boundary.
type ChannelModelDetectorRelayExecution struct {
	Source            string
	RunID             string
	TargetID          int64
	ExecutionID       int64
	ChannelID         int
	RequestModel      string
	ClaimedModel      string
	Preset            string
	RelayBaseURL      string
	DetectorRequestID string
	AttemptNo         int
	RequestBody       []byte
}

// ChannelModelDetectorUsage is the common accounting shape produced from
// OpenAI Responses or Chat Completions usage. Source is authoritative only
// when Available is true.
type ChannelModelDetectorUsage struct {
	Available                  bool   `json:"available"`
	Source                     string `json:"source"`
	InputTokens                int64  `json:"input_tokens"`
	OutputTokens               int64  `json:"output_tokens"`
	TotalTokens                int64  `json:"total_tokens"`
	InputTokenDetailsAvailable bool   `json:"input_token_details_available,omitempty"`
	CachedTokens               int64  `json:"cached_tokens,omitempty"`
	CachedCreationTokens       int64  `json:"cached_creation_tokens,omitempty"`
	CacheWriteTokens           int64  `json:"cache_write_tokens,omitempty"`
}

// ChannelModelDetectorRelayUpstreamResult contains protocol data returned by
// exactly one fixed-channel attempt. UsagePayload may be either a JSON usage
// object, a Chat/Responses response envelope, or a Responses SSE body. When a
// relay adaptor already has authoritative usage, it should set Usage directly.
type ChannelModelDetectorRelayUpstreamResult struct {
	StatusCode        int
	ContentType       string
	ResponseBody      []byte
	UsagePayload      []byte
	Usage             *ChannelModelDetectorUsage
	Dispatched        bool
	RequestID         string
	UpstreamRequestID string
}

type ChannelModelDetectorRelayResult struct {
	Authorization ChannelModelDetectorAttemptAuthorization `json:"-"`
	Upstream      ChannelModelDetectorRelayUpstreamResult  `json:"-"`
	Usage         ChannelModelDetectorUsage                `json:"usage"`
}

// ChannelModelDetectorFixedChannelExecutor is the sole transport seam. An
// implementation must execute the supplied ChannelID only, perform no channel
// selection/failover, and bypass PreConsumeBilling, SettleBilling, and
// BillingSession. Cost attribution is handled by the model-detection cost path.
type ChannelModelDetectorFixedChannelExecutor interface {
	ExecuteChannelModelDetectorAttempt(context.Context, ChannelModelDetectorRelayExecution) (ChannelModelDetectorRelayUpstreamResult, error)
}

// ChannelModelDetectorRelayRunner is the small HTTP-facing seam. Keeping the
// interface separate from the concrete coordinator allows protocol tests to
// avoid constructing a database-backed concurrency registry.
type ChannelModelDetectorRelayRunner interface {
	Execute(context.Context, ChannelModelDetectorRelayRequest) (ChannelModelDetectorRelayResult, error)
}

type channelModelDetectorConcurrencyLease interface {
	Release()
}

type channelModelDetectorConcurrencyAcquirer func(context.Context, int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error)

// ChannelModelDetectorRelay authorizes one detector request, obtains the
// selected channel's concurrency lease, and delegates one fixed-channel
// attempt. It contains no user/token/subscription billing dependency.
type ChannelModelDetectorRelay struct {
	tokens             *ChannelModelDetectorTokenStore
	executor           ChannelModelDetectorFixedChannelExecutor
	acquireConcurrency channelModelDetectorConcurrencyAcquirer
}

func NewChannelModelDetectorRelay(tokens *ChannelModelDetectorTokenStore, executor ChannelModelDetectorFixedChannelExecutor) (*ChannelModelDetectorRelay, error) {
	return newChannelModelDetectorRelay(tokens, executor, func(ctx context.Context, channelID int) (channelModelDetectorConcurrencyLease, bool, ChannelConcurrencyStatus, error) {
		// A process-local relay can still be exercised before the application
		// database is initialized (for example during a transport contract
		// check). The real fixed-channel executor performs its own DB-backed
		// run/execution validation; avoid calling the normal concurrency loader
		// here because it dereferences the global GORM handle.
		if model.DB == nil {
			return nil, true, ChannelConcurrencyStatus{}, nil
		}
		return AcquireChannelConcurrency(ctx, channelID)
	})
}

func newChannelModelDetectorRelay(tokens *ChannelModelDetectorTokenStore, executor ChannelModelDetectorFixedChannelExecutor, acquire channelModelDetectorConcurrencyAcquirer) (*ChannelModelDetectorRelay, error) {
	if tokens == nil || executor == nil || acquire == nil {
		return nil, ErrChannelModelDetectorRelayUnavailable
	}
	return &ChannelModelDetectorRelay{
		tokens:             tokens,
		executor:           executor,
		acquireConcurrency: acquire,
	}, nil
}

func (relay *ChannelModelDetectorRelay) Execute(ctx context.Context, request ChannelModelDetectorRelayRequest) (ChannelModelDetectorRelayResult, error) {
	if relay == nil || relay.tokens == nil || relay.executor == nil || relay.acquireConcurrency == nil {
		return ChannelModelDetectorRelayResult{}, ErrChannelModelDetectorRelayUnavailable
	}
	if ctx == nil || len(request.Body) == 0 || len(request.Body) > ChannelModelDetectorRelayMaxRequestBytes {
		return ChannelModelDetectorRelayResult{}, ErrChannelModelDetectorRelayInvalidRequest
	}

	requestedModel, payload, err := prepareChannelModelDetectorRequestBody(request.Body)
	if err != nil {
		return ChannelModelDetectorRelayResult{}, err
	}
	detectorRequestID := strings.TrimSpace(request.DetectorRequestID)
	if detectorRequestID == "" || len(detectorRequestID) > 256 {
		return ChannelModelDetectorRelayResult{}, ErrChannelModelDetectorRelayInvalidRequest
	}
	authorization, err := relay.tokens.AuthorizeAttempt(request.BearerToken, requestedModel, detectorRequestID)
	if err != nil {
		return ChannelModelDetectorRelayResult{}, err
	}
	if authorization.Replay {
		return ChannelModelDetectorRelayResult{Authorization: authorization}, ErrChannelModelDetectorTokenReplay
	}

	requestModelJSON, err := common.Marshal(authorization.Claims.RequestModel)
	if err != nil {
		return ChannelModelDetectorRelayResult{Authorization: authorization}, fmt.Errorf("编码模型检测固定模型失败: %w", err)
	}
	payload["model"] = requestModelJSON
	rewrittenBody, err := common.Marshal(payload)
	if err != nil {
		return ChannelModelDetectorRelayResult{Authorization: authorization}, fmt.Errorf("编码模型检测请求失败: %w", err)
	}

	lease, acquired, _, err := relay.acquireConcurrency(ctx, authorization.Claims.ChannelID)
	if err != nil {
		return ChannelModelDetectorRelayResult{Authorization: authorization}, fmt.Errorf("获取模型检测渠道并发租约失败: %w", err)
	}
	if !acquired {
		return ChannelModelDetectorRelayResult{Authorization: authorization}, ErrChannelModelDetectorRelayBusy
	}
	if lease != nil {
		defer lease.Release()
	}

	execution := ChannelModelDetectorRelayExecution{
		Source:            ChannelModelDetectorRequestSource,
		RunID:             authorization.Claims.RunID,
		TargetID:          authorization.Claims.TargetID,
		ExecutionID:       authorization.Claims.ExecutionID,
		ChannelID:         authorization.Claims.ChannelID,
		RequestModel:      authorization.Claims.RequestModel,
		ClaimedModel:      authorization.Claims.ClaimedModel,
		Preset:            authorization.Claims.Preset,
		RelayBaseURL:      authorization.Claims.RelayBaseURL,
		DetectorRequestID: authorization.DetectorRequestID,
		AttemptNo:         authorization.AttemptNo,
		RequestBody:       rewrittenBody,
	}
	upstream, err := relay.executor.ExecuteChannelModelDetectorAttempt(ctx, execution)
	result := ChannelModelDetectorRelayResult{Authorization: authorization, Upstream: upstream}
	if err != nil {
		return result, err
	}

	if upstream.Usage != nil {
		usage, err := validateChannelModelDetectorUsage(*upstream.Usage)
		if err != nil {
			if errors.Is(err, ErrChannelModelDetectorUsageUnavailable) {
				result.Usage.Source = model.ChannelModelDetectionUsageUnavailable
				return result, nil
			}
			return result, err
		}
		result.Usage = usage
		return result, nil
	}
	usagePayload := upstream.UsagePayload
	if len(usagePayload) == 0 {
		usagePayload = upstream.ResponseBody
	}
	usage, err := NormalizeChannelModelDetectorUsage(usagePayload)
	if err != nil {
		if errors.Is(err, ErrChannelModelDetectorUsageUnavailable) {
			result.Usage.Source = model.ChannelModelDetectionUsageUnavailable
			return result, nil
		}
		return result, err
	}
	result.Usage = usage
	return result, nil
}

func prepareChannelModelDetectorRequestBody(body []byte) (string, map[string]json.RawMessage, error) {
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(body, &payload); err != nil || payload == nil {
		return "", nil, ErrChannelModelDetectorRelayInvalidRequest
	}
	for field := range payload {
		switch strings.ToLower(field) {
		case "channel", "channel_id", "channelid", "specific_channel_id", "base_url", "upstream_base_url":
			return "", nil, ErrChannelModelDetectorRelayInvalidRequest
		}
	}
	modelJSON, exists := payload["model"]
	if !exists {
		return "", nil, ErrChannelModelDetectorRelayInvalidRequest
	}
	var requestedModel string
	if err := common.Unmarshal(modelJSON, &requestedModel); err != nil || requestedModel == "" || strings.TrimSpace(requestedModel) != requestedModel {
		return "", nil, ErrChannelModelDetectorRelayInvalidRequest
	}
	return requestedModel, payload, nil
}

// NormalizeChannelModelDetectorUsage accepts authoritative usage in the
// common OpenAI shapes: a direct usage object, top-level response.usage,
// Responses stream-event response.usage, or SSE data events containing one of
// those JSON envelopes.
func NormalizeChannelModelDetectorUsage(payload []byte) (ChannelModelDetectorUsage, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	if bytes.HasPrefix(payload, []byte("data:")) || bytes.Contains(payload, []byte("\ndata:")) {
		return normalizeChannelModelDetectorSSEUsage(payload)
	}
	usageJSON, found, err := channelModelDetectorUsageJSON(payload)
	if err != nil {
		return ChannelModelDetectorUsage{}, err
	}
	if !found {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	return normalizeChannelModelDetectorUsageObject(usageJSON)
}

// NormalizeChannelModelDetectorDTOUsage bridges the usage already returned by
// existing relay adaptors without serializing it through another protocol
// envelope.
func NormalizeChannelModelDetectorDTOUsage(usage *relaydto.Usage) (ChannelModelDetectorUsage, error) {
	if usage == nil {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	if usage.BillingUsage != nil && usage.BillingUsage.Estimated {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	if usage.InputTokens == 0 && usage.PromptTokens == 0 &&
		usage.OutputTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	if usage.InputTokens != 0 && usage.PromptTokens != 0 && usage.InputTokens != usage.PromptTokens {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	if usage.OutputTokens != 0 && usage.CompletionTokens != 0 && usage.OutputTokens != usage.CompletionTokens {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	input := usage.InputTokens
	if input == 0 && usage.PromptTokens != 0 {
		input = usage.PromptTokens
	}
	output := usage.OutputTokens
	if output == 0 && usage.CompletionTokens != 0 {
		output = usage.CompletionTokens
	}
	normalized := ChannelModelDetectorUsage{
		Available:    true,
		Source:       model.ChannelModelDetectionUsageUpstreamAuthoritative,
		InputTokens:  int64(input),
		OutputTokens: int64(output),
		TotalTokens:  int64(usage.TotalTokens),
	}
	if normalized.TotalTokens == 0 && (normalized.InputTokens != 0 || normalized.OutputTokens != 0) {
		if normalized.InputTokens > math.MaxInt64-normalized.OutputTokens {
			return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
		}
		normalized.TotalTokens = normalized.InputTokens + normalized.OutputTokens
	}
	return validateChannelModelDetectorUsage(normalized)
}

type channelModelDetectorWireUsage struct {
	InputTokens         *int64                                `json:"input_tokens"`
	OutputTokens        *int64                                `json:"output_tokens"`
	PromptTokens        *int64                                `json:"prompt_tokens"`
	CompletionTokens    *int64                                `json:"completion_tokens"`
	TotalTokens         *int64                                `json:"total_tokens"`
	InputTokensDetails  *channelModelDetectorWireTokenDetails `json:"input_tokens_details"`
	PromptTokensDetails *channelModelDetectorWireTokenDetails `json:"prompt_tokens_details"`
}

type channelModelDetectorWireTokenDetails struct {
	CachedTokens         *int64 `json:"cached_tokens"`
	CachedCreationTokens *int64 `json:"cached_creation_tokens"`
	CacheWriteTokens     *int64 `json:"cache_write_tokens"`
	CacheCreationTokens  *int64 `json:"cache_creation_tokens"`
}

func channelModelDetectorUsageJSON(payload []byte) ([]byte, bool, error) {
	var object map[string]json.RawMessage
	if err := common.Unmarshal(payload, &object); err != nil || object == nil {
		return nil, false, ErrChannelModelDetectorUsageInvalid
	}
	if usage, exists := object["usage"]; exists {
		return usage, true, nil
	}
	// Claude Messages streams put input usage under message_start.message,
	// while output usage arrives later under message_delta. The stream
	// normalizer merges those partial records below.
	if message, exists := object["message"]; exists {
		var messageObject map[string]json.RawMessage
		if err := common.Unmarshal(message, &messageObject); err != nil || messageObject == nil {
			return nil, false, ErrChannelModelDetectorUsageInvalid
		}
		if usage, exists := messageObject["usage"]; exists {
			return usage, true, nil
		}
	}
	if response, exists := object["response"]; exists {
		var responseObject map[string]json.RawMessage
		if err := common.Unmarshal(response, &responseObject); err != nil || responseObject == nil {
			return nil, false, ErrChannelModelDetectorUsageInvalid
		}
		if usage, exists := responseObject["usage"]; exists {
			return usage, true, nil
		}
		return nil, false, nil
	}
	for _, field := range []string{"input_tokens", "output_tokens", "prompt_tokens", "completion_tokens", "total_tokens"} {
		if _, exists := object[field]; exists {
			return payload, true, nil
		}
	}
	return nil, false, nil
}

func normalizeChannelModelDetectorUsageObject(payload []byte) (ChannelModelDetectorUsage, error) {
	var wire channelModelDetectorWireUsage
	if err := common.Unmarshal(payload, &wire); err != nil {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	if wire.InputTokens == nil && wire.OutputTokens == nil && wire.PromptTokens == nil && wire.CompletionTokens == nil {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	for _, value := range []*int64{wire.InputTokens, wire.OutputTokens, wire.PromptTokens, wire.CompletionTokens, wire.TotalTokens} {
		if value != nil && *value < 0 {
			return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
		}
	}
	if wire.InputTokens != nil && wire.PromptTokens != nil && *wire.InputTokens != 0 && *wire.PromptTokens != 0 && *wire.InputTokens != *wire.PromptTokens {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	if wire.OutputTokens != nil && wire.CompletionTokens != nil && *wire.OutputTokens != 0 && *wire.CompletionTokens != 0 && *wire.OutputTokens != *wire.CompletionTokens {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	details, detailsAvailable, err := normalizeChannelModelDetectorTokenDetails(wire.InputTokensDetails, wire.PromptTokensDetails)
	if err != nil {
		return ChannelModelDetectorUsage{}, err
	}

	var inputTokens, outputTokens int64
	if wire.InputTokens != nil && *wire.InputTokens != 0 {
		inputTokens = *wire.InputTokens
	} else if wire.PromptTokens != nil {
		inputTokens = *wire.PromptTokens
	} else if wire.InputTokens != nil {
		inputTokens = *wire.InputTokens
	}
	if wire.OutputTokens != nil && *wire.OutputTokens != 0 {
		outputTokens = *wire.OutputTokens
	} else if wire.CompletionTokens != nil {
		outputTokens = *wire.CompletionTokens
	} else if wire.OutputTokens != nil {
		outputTokens = *wire.OutputTokens
	}
	if inputTokens > math.MaxInt64-outputTokens {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	calculatedTotal := inputTokens + outputTokens
	totalTokens := calculatedTotal
	if wire.TotalTokens != nil {
		totalTokens = *wire.TotalTokens
		if totalTokens != calculatedTotal {
			return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
		}
	}
	return ChannelModelDetectorUsage{
		Available:                  true,
		Source:                     model.ChannelModelDetectionUsageUpstreamAuthoritative,
		InputTokens:                inputTokens,
		OutputTokens:               outputTokens,
		TotalTokens:                totalTokens,
		InputTokenDetailsAvailable: detailsAvailable,
		CachedTokens:               channelModelDetectorTokenDetailValue(details.CachedTokens),
		CachedCreationTokens:       channelModelDetectorTokenDetailValue(details.CachedCreationTokens),
		CacheWriteTokens:           channelModelDetectorTokenDetailValue(details.CacheWriteTokens),
	}, nil
}

func channelModelDetectorTokenDetailValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func normalizeChannelModelDetectorTokenDetails(input, prompt *channelModelDetectorWireTokenDetails) (channelModelDetectorWireTokenDetails, bool, error) {
	if input == nil && prompt == nil {
		return channelModelDetectorWireTokenDetails{}, false, nil
	}
	if input == nil {
		input = prompt
	} else if prompt != nil {
		merged := *input
		for _, pair := range []struct {
			left  **int64
			right *int64
		}{
			{&merged.CachedTokens, prompt.CachedTokens},
			{&merged.CachedCreationTokens, prompt.CachedCreationTokens},
			{&merged.CacheWriteTokens, prompt.CacheWriteTokens},
			{&merged.CacheCreationTokens, prompt.CacheCreationTokens},
		} {
			if *pair.left != nil && pair.right != nil && **pair.left != *pair.right {
				return channelModelDetectorWireTokenDetails{}, false, ErrChannelModelDetectorUsageInvalid
			}
			if *pair.left == nil {
				*pair.left = pair.right
			}
		}
		input = &merged
	}

	for _, value := range []*int64{
		input.CachedTokens,
		input.CachedCreationTokens,
		input.CacheWriteTokens,
		input.CacheCreationTokens,
	} {
		if value != nil && *value < 0 {
			return channelModelDetectorWireTokenDetails{}, false, ErrChannelModelDetectorUsageInvalid
		}
	}

	details := channelModelDetectorWireTokenDetails{
		CachedTokens:         input.CachedTokens,
		CachedCreationTokens: input.CachedCreationTokens,
		CacheWriteTokens:     input.CacheWriteTokens,
	}
	if details.CacheWriteTokens == nil {
		details.CacheWriteTokens = input.CacheCreationTokens
	} else if input.CacheCreationTokens != nil && *details.CacheWriteTokens != *input.CacheCreationTokens {
		return channelModelDetectorWireTokenDetails{}, false, ErrChannelModelDetectorUsageInvalid
	}
	return details, true, nil
}

func normalizeChannelModelDetectorSSEUsage(payload []byte) (ChannelModelDetectorUsage, error) {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 16*1024), channelModelDetectorUsageEventMaxBytes)
	var lastUsage ChannelModelDetectorUsage
	found := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		usageJSON, hasUsage, err := channelModelDetectorUsageJSON(data)
		if err != nil {
			return ChannelModelDetectorUsage{}, err
		}
		if !hasUsage {
			continue
		}
		usage, err := normalizeChannelModelDetectorUsageObject(usageJSON)
		if err != nil {
			return ChannelModelDetectorUsage{}, err
		}
		if !found {
			lastUsage = usage
		} else {
			// Claude Messages streams split input and output usage across
			// message_start and message_delta. Keep each non-zero component
			// while allowing a later event to provide the authoritative total.
			if usage.InputTokens != 0 {
				lastUsage.InputTokens = usage.InputTokens
			}
			if usage.OutputTokens != 0 {
				lastUsage.OutputTokens = usage.OutputTokens
			}
			if usage.TotalTokens != 0 {
				lastUsage.TotalTokens = usage.TotalTokens
			}
			if usage.InputTokenDetailsAvailable {
				lastUsage.InputTokenDetailsAvailable = true
				lastUsage.CachedTokens = usage.CachedTokens
				lastUsage.CachedCreationTokens = usage.CachedCreationTokens
				lastUsage.CacheWriteTokens = usage.CacheWriteTokens
			}
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	if !found {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	if lastUsage.InputTokens > math.MaxInt64-lastUsage.OutputTokens {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	lastUsage.TotalTokens = lastUsage.InputTokens + lastUsage.OutputTokens
	return validateChannelModelDetectorUsage(lastUsage)
}

func validateChannelModelDetectorUsage(usage ChannelModelDetectorUsage) (ChannelModelDetectorUsage, error) {
	if !usage.Available {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageUnavailable
	}
	if usage.Source == "" {
		usage.Source = model.ChannelModelDetectionUsageUpstreamAuthoritative
	}
	if usage.Source != model.ChannelModelDetectionUsageUpstreamAuthoritative || usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.TotalTokens < 0 || usage.CachedTokens < 0 || usage.CachedCreationTokens < 0 || usage.CacheWriteTokens < 0 {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	if usage.InputTokens > math.MaxInt64-usage.OutputTokens || usage.TotalTokens != usage.InputTokens+usage.OutputTokens {
		return ChannelModelDetectorUsage{}, ErrChannelModelDetectorUsageInvalid
	}
	return usage, nil
}
