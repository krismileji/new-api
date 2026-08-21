package service

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

type ChannelMonitorSuccessEventInput struct {
	PromptTokens      int
	CompletionTokens  int
	CacheReadTokens   int
	CacheWriteTokens  int
	InputTokens       int
	CostEventId       string
	PerformanceTiming *RelayPerformanceTiming
}

func EmitChannelMonitorSuccessEvent(
	ctx *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	input ChannelMonitorSuccessEventInput,
) ChannelMonitorEventPublishStatus {
	if relayInfo == nil || relayInfo.ChannelId <= 0 {
		return ChannelMonitorEventPublishStatusInvalid
	}

	now := time.Now()
	outputTokens := max(input.CompletionTokens, 0)
	if input.PerformanceTiming != nil {
		if !input.PerformanceTiming.CompletedAt.IsZero() {
			now = input.PerformanceTiming.CompletedAt
		}
	}
	performanceTiming := BuildChannelMonitorPerformanceTiming(ctx, relayInfo, outputTokens, now)
	source := channelMonitorEventSource(ctx)
	if relayInfo.IsChannelTest && source != model.ChannelMonitorEventSourceModelDetection {
		return ChannelMonitorEventPublishStatusInvalid
	}
	event := model.NewChannelMonitorEvent(
		relayInfo.ChannelId,
		source,
		model.ChannelMonitorEventOutcomeSuccess,
		now.Unix(),
	)
	event.GroupName = strings.TrimSpace(relayInfo.UsingGroup)
	event.ModelName = strings.TrimSpace(relayInfo.OriginModelName)
	event.RequestId = channelMonitorRequestId(ctx, relayInfo.RequestId)
	event.APIKeyId = relayInfo.TokenId
	if ctx != nil {
		event.APIKeyName = channelMonitorBoundedString(
			strings.TrimSpace(ctx.GetString("token_name")),
			model.ChannelMonitorEventMaxNameLength,
		)
	}
	event.IsStream = relayInfo.IsStream
	event.IsRetryAttempt = relayInfo.RetryIndex > 0
	event.IsFinalAttempt = true
	event.RequestDispatched = true
	event.SchedulingEligible = channelMonitorEventSchedulingEligible(ctx, source)
	event.PromptTokens = channelMonitorNonNegativeIntPointer(input.PromptTokens)
	event.CompletionTokens = channelMonitorNonNegativeIntPointer(input.CompletionTokens)
	event.CacheReadTokens = channelMonitorNonNegativeIntPointer(input.CacheReadTokens)
	event.CacheWriteTokens = channelMonitorNonNegativeIntPointer(input.CacheWriteTokens)
	event.InputTokens = channelMonitorNonNegativeIntPointer(input.InputTokens)
	if costEventId := strings.TrimSpace(input.CostEventId); costEventId != "" {
		if other, err := common.Marshal(map[string]string{"cost_event_id": costEventId}); err == nil {
			event.OtherJson = string(other)
		}
	}

	durationMs := performanceTiming.AttemptDurationMs
	event.AttemptDurationMs = &durationMs
	if performanceTiming.FirstTokenMs != nil {
		value := *performanceTiming.FirstTokenMs
		event.FirstTokenMs = &value
	}
	if performanceTiming.TokensPerSecond != nil {
		value := *performanceTiming.TokensPerSecond
		event.TPS = &value
	}

	if source != model.ChannelMonitorEventSourceModelDetection {
		if settledCost := ChannelDailyCostAttemptSettledCost(ctx, relayInfo.ChannelId); settledCost != nil {
			event.CostStatus = model.ChannelMonitorEventCostSettled
			event.SettledCostNanoCNY = *settledCost
		} else {
			event.CostStatus = model.ChannelMonitorEventCostUnresolved
		}
	}
	status, _ := PublishChannelMonitorEvent(channelMonitorPublishContext(ctx), event)
	return status
}

func EmitChannelMonitorFailureEvent(
	ctx *gin.Context,
	channelId int,
	modelName string,
	err *relaytypes.NewAPIError,
	isRetryAttempt bool,
	isFinalAttempt bool,
	finalRetrySummary bool,
	requestDispatched bool,
	runtimeProtectionEligible bool,
	attemptDuration *time.Duration,
) ChannelMonitorEventPublishStatus {
	if channelId <= 0 || err == nil {
		return ChannelMonitorEventPublishStatusInvalid
	}
	outcome := model.ChannelMonitorEventOutcomeFailure
	if relaytypes.IsClientGoneError(err) {
		outcome = model.ChannelMonitorEventOutcomeCanceled
	}
	event := model.NewChannelMonitorEvent(
		channelId,
		channelMonitorEventSource(ctx),
		outcome,
		common.GetTimestamp(),
	)
	event.GroupName = channelMonitorUsingGroup(ctx)
	event.ModelName = strings.TrimSpace(modelName)
	event.RequestId = channelMonitorRequestId(ctx, "")
	if ctx != nil {
		event.APIKeyId = ctx.GetInt("token_id")
		event.APIKeyName = channelMonitorBoundedString(
			strings.TrimSpace(ctx.GetString("token_name")),
			model.ChannelMonitorEventMaxNameLength,
		)
	}
	event.IsStream = common.GetContextKeyBool(ctx, constant.ContextKeyIsStream)
	event.IsRetryAttempt = isRetryAttempt
	event.IsFinalAttempt = isFinalAttempt
	event.FinalRetrySummary = finalRetrySummary
	event.RequestDispatched = requestDispatched && !finalRetrySummary
	event.SchedulingEligible = channelMonitorEventSchedulingEligible(ctx, event.Source)
	statusCode := err.StatusCode
	event.ErrorType = channelMonitorBoundedString(
		string(err.GetErrorType()), model.ChannelMonitorEventMaxIdentityLength,
	)
	event.ErrorCode = channelMonitorBoundedString(
		string(err.GetErrorCode()), model.ChannelMonitorEventMaxIdentityLength,
	)
	event.ErrorMessage = channelMonitorBoundedString(err.MaskSensitiveErrorWithStatusCode(), 2048)
	event.RuntimeProtectionEligible = runtimeProtectionEligible && !finalRetrySummary &&
		!relaytypes.IsSkipRetryError(err) &&
		(relaytypes.IsChannelError(err) || statusCode == http.StatusRequestTimeout ||
			statusCode == http.StatusTooEarly || statusCode == http.StatusTooManyRequests ||
			statusCode >= http.StatusInternalServerError && statusCode <= 599)
	if event.RequestDispatched {
		event.CostStatus = model.ChannelMonitorEventCostUnresolved
	}
	event.StatusCode = &statusCode
	if attemptDuration != nil {
		durationMs := attemptDuration.Milliseconds()
		if durationMs < 0 {
			durationMs = 0
		}
		event.AttemptDurationMs = &durationMs
	}
	status, _ := PublishChannelMonitorEvent(channelMonitorPublishContext(ctx), event)
	return status
}

func channelMonitorPublishContext(ctx *gin.Context) context.Context {
	if ctx == nil || ctx.Request == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx.Request.Context())
}

func channelMonitorEventSchedulingEligible(
	ctx *gin.Context,
	source model.ChannelMonitorEventSource,
) bool {
	if source == model.ChannelMonitorEventSourceBusiness || source == model.ChannelMonitorEventSourceSmartProbe {
		return true
	}
	return ctx != nil && ctx.GetBool(model.ChannelMonitorSchedulingEligibleContextKey)
}

func channelMonitorEventSource(ctx *gin.Context) model.ChannelMonitorEventSource {
	if channelModelDetectionTransportStateFromContext(ctx) != nil {
		return model.ChannelMonitorEventSourceModelDetection
	}
	if ctx != nil && ctx.GetBool(model.ChannelMonitorGroupProbeLogKey) {
		return model.ChannelMonitorEventSourceGroupProbe
	}
	if ctx != nil && ctx.GetBool(model.ChannelMonitorStatusProbeLogKey) {
		return model.ChannelMonitorEventSourceStatusProbe
	}
	if ctx != nil && ctx.GetBool(model.ChannelMonitorSmartScheduleProbeLogKey) {
		return model.ChannelMonitorEventSourceSmartProbe
	}
	if ctx != nil && ctx.GetBool("channel_test") {
		return model.ChannelMonitorEventSourceManualTest
	}
	return model.ChannelMonitorEventSourceBusiness
}

func channelMonitorUsingGroup(ctx *gin.Context) string {
	if ctx == nil {
		return ""
	}
	group := common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup)
	if group == "" {
		group = ctx.GetString("group")
	}
	if autoGroup := common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup); autoGroup != "" {
		group = autoGroup
	}
	return strings.TrimSpace(group)
}

func channelMonitorRequestId(ctx *gin.Context, fallback string) string {
	if ctx != nil {
		if requestId := strings.TrimSpace(ctx.GetString(common.RequestIdKey)); requestId != "" {
			return requestId
		}
	}
	return strings.TrimSpace(fallback)
}

func channelMonitorNonNegativeIntPointer(value int) *int64 {
	if value < 0 {
		value = 0
	}
	converted := int64(value)
	return &converted
}

func channelMonitorBoundedString(value string, maximumBytes int) string {
	if maximumBytes <= 0 || len(value) <= maximumBytes {
		return value
	}
	value = value[:maximumBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
