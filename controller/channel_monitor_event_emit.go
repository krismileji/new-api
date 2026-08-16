package controller

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type channelMonitorSchedulingEligibleContextKey struct{}

func withChannelMonitorSchedulingEligibility(ctx context.Context, eligible bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, channelMonitorSchedulingEligibleContextKey{}, eligible)
}

func applyChannelMonitorSchedulingEligibility(source context.Context, target *gin.Context) {
	if source == nil || target == nil {
		return
	}
	eligible, ok := source.Value(channelMonitorSchedulingEligibleContextKey{}).(bool)
	if ok {
		target.Set(model.ChannelMonitorSchedulingEligibleContextKey, eligible)
	}
}

func emitChannelTestMonitorEvent(
	ctx *gin.Context,
	channelId int,
	modelName string,
	source model.ChannelMonitorEventSource,
	startedAt time.Time,
	result testResult,
) {
	if ctx == nil || channelId <= 0 || !result.requestDispatched {
		return
	}
	outcome := model.ChannelMonitorEventOutcomeSuccess
	if result.newAPIError != nil || result.localErr != nil {
		outcome = model.ChannelMonitorEventOutcomeFailure
	}
	if result.newAPIError != nil && result.newAPIError.StatusCode == 499 {
		outcome = model.ChannelMonitorEventOutcomeCanceled
	}
	now := time.Now()
	duration := now.Sub(startedAt)
	if duration < 0 {
		duration = 0
	}
	if outcome != model.ChannelMonitorEventOutcomeSuccess {
		monitorErr := result.newAPIError
		if monitorErr == nil {
			monitorErr = relaytypes.NewError(result.localErr, relaytypes.ErrorCodeDoRequestFailed)
		}
		if outcome == model.ChannelMonitorEventOutcomeCanceled && !relaytypes.IsClientGoneError(monitorErr) {
			monitorErr = relaytypes.NewClientGoneError(context.Canceled)
		}
		service.EmitChannelMonitorFailureEvent(
			ctx, channelId, modelName, monitorErr,
			false, true, false, true, false, &duration,
		)
		return
	}
	event := model.NewChannelMonitorEvent(channelId, source, outcome, now.Unix())
	event.ModelName = ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
	event.GroupName = common.GetContextKeyString(ctx, constant.ContextKeyUsingGroup)
	event.RequestId = strings.TrimSpace(ctx.GetString(common.RequestIdKey))
	event.IsFinalAttempt = true
	event.RequestDispatched = true
	event.SchedulingEligible = ctx.GetBool(model.ChannelMonitorSchedulingEligibleContextKey)
	event.IsStream = common.GetContextKeyBool(ctx, constant.ContextKeyIsStream)
	durationMs := duration.Milliseconds()
	event.AttemptDurationMs = &durationMs
	if result.firstResponseMilliseconds != nil {
		value := *result.firstResponseMilliseconds
		event.FirstTokenMs = &value
	}
	if result.tokensPerSecond != nil {
		value := *result.tokensPerSecond
		event.TPS = &value
	}
	if result.usageMetrics.available {
		promptTokens := int64(result.usageMetrics.inputTokens)
		completionTokens := int64(result.usageMetrics.outputTokens)
		cacheReadTokens := int64(result.usageMetrics.cachedTokens)
		cacheWriteTokens := int64(result.usageMetrics.cacheWriteTokens)
		inputTokens := int64(result.usageMetrics.inputTokens)
		event.PromptTokens = &promptTokens
		event.CompletionTokens = &completionTokens
		event.CacheReadTokens = &cacheReadTokens
		event.CacheWriteTokens = &cacheWriteTokens
		event.InputTokens = &inputTokens
	}
	if settledCost := service.ChannelDailyCostAttemptSettledCost(ctx, channelId); settledCost != nil {
		event.CostStatus = model.ChannelMonitorEventCostSettled
		event.SettledCostNanoCNY = *settledCost
	} else {
		event.CostStatus = model.ChannelMonitorEventCostUnresolved
	}
	_, _ = service.PublishChannelMonitorEvent(context.WithoutCancel(ctx.Request.Context()), event)
}
