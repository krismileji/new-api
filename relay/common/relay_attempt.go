package common

import (
	"fmt"
	"net/http"
	"reflect"
	"time"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"
)

const EventStreamHeadersSetContextKey = "event_stream_headers_set"

// RelayAttemptState captures request-level relay state and restores it before
// every upstream attempt. Adaptors are allowed to mutate RelayInfo while
// converting a request or parsing a response, so retries must not reuse that
// attempt-local state on another channel.
type RelayAttemptState struct {
	originModelName         string
	channelMeta             *ChannelMeta
	requestURLPath          string
	relayMode               int
	relayFormat             types.RelayFormat
	isStream                bool
	isGeminiBatchEmbedding  bool
	shouldIncludeUsage      bool
	disablePing             bool
	audioUsage              bool
	reasoningEffort         string
	inputAudioFormat        string
	outputAudioFormat       string
	realtimeTools           []dto.RealTimeTool
	isFirstRequest          bool
	firstResponseTime       time.Time
	isFirstResponse         bool
	requestConversionChain  []types.RelayFormat
	finalRequestRelayFormat types.RelayFormat
	thinkingContentInfo     ThinkingContentInfo
	claudeConvertInfo       *ClaudeConvertInfo
	responsesUsageInfo      *ResponsesUsageInfo
	tieredBillingSnapshot   *billingexpr.BillingSnapshot
	billingRequestInput     *billingexpr.RequestInput
	quotaClamp              *rootcommon.QuotaClamp
	taskAction              string
	contextAction           string
	priceData               hosttypes.PriceData
	requestInfoHeaders      map[string]string
	requestHeaders          http.Header
	responseHeaders         http.Header
	request                 dto.Request
	requestSnapshot         dto.Request
}

func NewRelayAttemptState(c *gin.Context, info *RelayInfo) (*RelayAttemptState, error) {
	state := &RelayAttemptState{}
	if c != nil && c.Writer != nil {
		state.responseHeaders = c.Writer.Header().Clone()
	}
	if c != nil {
		state.contextAction = c.GetString("action")
		if c.Request != nil {
			state.requestHeaders = c.Request.Header.Clone()
		}
	}
	if info == nil {
		return state, nil
	}

	if info.Request != nil {
		requestValue := reflect.ValueOf(info.Request)
		if requestValue.Kind() != reflect.Ptr || requestValue.IsNil() {
			return nil, fmt.Errorf("relay request must be a non-nil pointer, got %T", info.Request)
		}
		requestSnapshotValue := reflect.New(requestValue.Elem().Type())
		if err := copier.CopyWithOption(
			requestSnapshotValue.Interface(),
			info.Request,
			copier.Option{DeepCopy: true, IgnoreEmpty: true},
		); err != nil {
			return nil, fmt.Errorf("copy relay request: %w", err)
		}
		requestSnapshot, ok := requestSnapshotValue.Interface().(dto.Request)
		if !ok {
			return nil, fmt.Errorf("copied relay request does not implement dto.Request: %T", requestSnapshotValue.Interface())
		}
		state.request = info.Request
		state.requestSnapshot = requestSnapshot
	}

	state.originModelName = info.OriginModelName
	if info.ChannelMeta != nil {
		channelMeta, err := cloneChannelMeta(info.ChannelMeta)
		if err != nil {
			return nil, fmt.Errorf("copy channel metadata: %w", err)
		}
		state.channelMeta = channelMeta
	}
	state.requestURLPath = info.RequestURLPath
	state.relayMode = info.RelayMode
	state.relayFormat = info.RelayFormat
	state.isStream = info.IsStream
	state.isGeminiBatchEmbedding = info.IsGeminiBatchEmbedding
	state.shouldIncludeUsage = info.ShouldIncludeUsage
	state.disablePing = info.DisablePing
	state.audioUsage = info.AudioUsage
	state.reasoningEffort = info.ReasoningEffort
	state.inputAudioFormat = info.InputAudioFormat
	state.outputAudioFormat = info.OutputAudioFormat
	state.realtimeTools = append([]dto.RealTimeTool(nil), info.RealtimeTools...)
	state.isFirstRequest = info.IsFirstRequest
	state.firstResponseTime = info.FirstResponseTime
	state.isFirstResponse = info.isFirstResponse
	state.requestConversionChain = append([]types.RelayFormat(nil), info.RequestConversionChain...)
	state.finalRequestRelayFormat = info.FinalRequestRelayFormat
	state.thinkingContentInfo = info.ThinkingContentInfo
	state.claudeConvertInfo = cloneClaudeConvertInfo(info.ClaudeConvertInfo)
	state.responsesUsageInfo = cloneResponsesUsageInfo(info.ResponsesUsageInfo)
	if info.TieredBillingSnapshot != nil {
		snapshot := *info.TieredBillingSnapshot
		state.tieredBillingSnapshot = &snapshot
	}
	if info.BillingRequestInput != nil {
		input := *info.BillingRequestInput
		input.Headers = cloneStringMap(info.BillingRequestInput.Headers)
		input.Body = append([]byte(nil), info.BillingRequestInput.Body...)
		state.billingRequestInput = &input
	}
	if info.QuotaClamp != nil {
		clamp := *info.QuotaClamp
		state.quotaClamp = &clamp
	}
	state.priceData = info.PriceData
	state.priceData.ReplaceOtherRatios(info.PriceData.OtherRatios())
	state.requestInfoHeaders = cloneStringMap(info.RequestHeaders)
	if info.TaskRelayInfo != nil {
		state.taskAction = info.Action
	}
	return state, nil
}

func (state *RelayAttemptState) Reset(c *gin.Context, info *RelayInfo) error {
	if state == nil || info == nil {
		return nil
	}

	if state.request != nil {
		requestValue := reflect.ValueOf(state.request)
		requestValue.Elem().Set(reflect.Zero(requestValue.Elem().Type()))
		if err := copier.CopyWithOption(
			state.request,
			state.requestSnapshot,
			copier.Option{DeepCopy: true, IgnoreEmpty: true},
		); err != nil {
			return fmt.Errorf("restore relay request: %w", err)
		}
		info.Request = state.request
	}

	info.OriginModelName = state.originModelName
	if state.channelMeta == nil {
		info.ChannelMeta = nil
	} else {
		channelMeta, err := cloneChannelMeta(state.channelMeta)
		if err != nil {
			return fmt.Errorf("restore channel metadata: %w", err)
		}
		info.ChannelMeta = channelMeta
	}
	info.RequestURLPath = state.requestURLPath
	info.RelayMode = state.relayMode
	info.RelayFormat = state.relayFormat
	info.IsStream = state.isStream
	info.IsGeminiBatchEmbedding = state.isGeminiBatchEmbedding
	info.ShouldIncludeUsage = state.shouldIncludeUsage
	info.DisablePing = state.disablePing
	info.AudioUsage = state.audioUsage
	info.ReasoningEffort = state.reasoningEffort
	info.InputAudioFormat = state.inputAudioFormat
	info.OutputAudioFormat = state.outputAudioFormat
	info.RealtimeTools = append([]dto.RealTimeTool(nil), state.realtimeTools...)
	info.IsFirstRequest = state.isFirstRequest
	info.FirstResponseTime = state.firstResponseTime
	info.isFirstResponse = state.isFirstResponse
	info.SendResponseCount = 0
	info.ReceivedResponseCount = 0
	info.RequestConversionChain = append([]types.RelayFormat(nil), state.requestConversionChain...)
	info.FinalRequestRelayFormat = state.finalRequestRelayFormat
	info.RuntimeHeadersOverride = nil
	info.UseRuntimeHeadersOverride = false
	info.ParamOverrideAudit = nil
	info.StreamStatus = nil
	info.TargetWs = nil
	info.RequestHeaders = cloneStringMap(state.requestInfoHeaders)
	info.ThinkingContentInfo = state.thinkingContentInfo
	info.ClaudeConvertInfo = cloneClaudeConvertInfo(state.claudeConvertInfo)
	info.ResponsesUsageInfo = cloneResponsesUsageInfo(state.responsesUsageInfo)
	if state.tieredBillingSnapshot != nil {
		snapshot := *state.tieredBillingSnapshot
		info.TieredBillingSnapshot = &snapshot
	} else {
		info.TieredBillingSnapshot = nil
	}
	if state.billingRequestInput != nil {
		input := *state.billingRequestInput
		input.Headers = cloneStringMap(state.billingRequestInput.Headers)
		input.Body = append([]byte(nil), state.billingRequestInput.Body...)
		info.BillingRequestInput = &input
	} else {
		info.BillingRequestInput = nil
	}
	if state.quotaClamp != nil {
		clamp := *state.quotaClamp
		info.QuotaClamp = &clamp
	} else {
		info.QuotaClamp = nil
	}
	priceData := state.priceData
	priceData.ReplaceOtherRatios(state.priceData.OtherRatios())
	info.PriceData = priceData
	info.convOptions = nil
	if info.TaskRelayInfo != nil {
		info.Action = state.taskAction
	}

	if c == nil {
		return nil
	}
	if c.Request != nil {
		if c.Request.Header == nil {
			c.Request.Header = make(http.Header)
		}
		header := c.Request.Header
		for key := range header {
			delete(header, key)
		}
		for key, values := range state.requestHeaders {
			header[key] = append([]string(nil), values...)
		}
	}
	if c.Writer != nil && !c.Writer.Written() {
		header := c.Writer.Header()
		for key := range header {
			delete(header, key)
		}
		for key, values := range state.responseHeaders {
			header[key] = append([]string(nil), values...)
		}
	}

	rootcommon.SetContextKey(c, constant.ContextKeyOriginalModel, state.originModelName)
	rootcommon.SetContextKey(c, constant.ContextKeyIsStream, state.isStream)
	rootcommon.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)
	rootcommon.SetContextKey(c, constant.ContextKeyLocalCountTokens, false)
	rootcommon.SetContextKey(c, constant.ContextKeyAdminRejectReason, "")
	if c.Keys != nil {
		delete(c.Keys, EventStreamHeadersSetContextKey)
	}
	c.Set(rootcommon.UpstreamRequestIdKey, "")
	c.Set("chat_completion_web_search_context_size", "")
	c.Set("claude_web_search_requests", 0)
	c.Set("gemini_google_search_call", false)
	c.Set("coze_conversation_id", "")
	c.Set("coze_chat_id", "")
	c.Set("coze_token_count", 0)
	c.Set("coze_output_count", 0)
	c.Set("coze_input_count", 0)
	c.Set("response_format", "")
	c.Set("request_model", "")
	c.Set("action", state.contextAction)
	c.Set("task_request", nil)
	c.Set("volcengine_tts_request", nil)
	c.Set("HexPayloadHash", "")
	return nil
}

func cloneResponsesUsageInfo(info *ResponsesUsageInfo) *ResponsesUsageInfo {
	if info == nil {
		return nil
	}
	clone := &ResponsesUsageInfo{
		BuiltInTools: make(map[string]*BuildInToolInfo, len(info.BuiltInTools)),
	}
	for name, tool := range info.BuiltInTools {
		if tool == nil {
			clone.BuiltInTools[name] = nil
			continue
		}
		toolClone := *tool
		clone.BuiltInTools[name] = &toolClone
	}
	return clone
}

func cloneChannelMeta(meta *ChannelMeta) (*ChannelMeta, error) {
	if meta == nil {
		return nil, nil
	}
	clone := &ChannelMeta{}
	if err := copier.CopyWithOption(
		clone,
		meta,
		copier.Option{DeepCopy: true, IgnoreEmpty: true},
	); err != nil {
		return nil, err
	}
	return clone, nil
}

func cloneClaudeConvertInfo(info *ClaudeConvertInfo) *ClaudeConvertInfo {
	if info == nil {
		return nil
	}
	clone := *info
	if info.Usage != nil {
		usage := dto.CloneBillingUsage(&dto.BillingUsage{OpenAIUsage: info.Usage})
		clone.Usage = usage.OpenAIUsage
	}
	return &clone
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
