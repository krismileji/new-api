package service

import (
	"math"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const channelMonitorPerformanceAttemptStartedAtKey = "channel_monitor_performance_attempt_started_at"

// RelayPerformanceTiming carries the official performance metrics calculated
// from the current channel attempt instead of the whole retry chain.
type RelayPerformanceTiming struct {
	CompletedAt       time.Time
	AttemptDurationMs int64
	FirstTokenMs      *float64
	TokensPerSecond   *float64
}

func BeginChannelMonitorPerformanceAttempt(ctx *gin.Context, startedAt time.Time) {
	if ctx == nil {
		return
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	ctx.Set(channelMonitorPerformanceAttemptStartedAtKey, startedAt)
}

func BuildChannelMonitorPerformanceTiming(
	ctx *gin.Context,
	relayInfo *relaycommon.RelayInfo,
	outputTokens int,
	completedAt time.Time,
) RelayPerformanceTiming {
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	timing := RelayPerformanceTiming{
		CompletedAt: completedAt,
	}
	if relayInfo == nil {
		return timing
	}

	startedAt := relayInfo.StartTime
	if ctx != nil {
		if value, exists := ctx.Get(channelMonitorPerformanceAttemptStartedAtKey); exists {
			if attemptStartedAt, ok := value.(time.Time); ok && !attemptStartedAt.IsZero() {
				startedAt = attemptStartedAt
			}
		}
	}
	if startedAt.IsZero() {
		return timing
	}

	duration := completedAt.Sub(startedAt)
	if duration < 0 {
		duration = 0
	}
	timing.AttemptDurationMs = duration.Milliseconds()
	hasTtft := relayInfo.IsStream && relayInfo.HasSendResponse() &&
		!relayInfo.FirstResponseTime.Before(startedAt) &&
		!relayInfo.FirstResponseTime.After(completedAt)
	if hasTtft {
		firstTokenMs := float64(relayInfo.FirstResponseTime.Sub(startedAt)) / float64(time.Millisecond)
		if firstTokenMs >= 0 && !math.IsNaN(firstTokenMs) && !math.IsInf(firstTokenMs, 0) {
			timing.FirstTokenMs = &firstTokenMs
		}
	}
	generationDuration := duration
	if hasTtft {
		generationDuration = completedAt.Sub(relayInfo.FirstResponseTime)
	}
	if generationDuration <= 0 {
		generationDuration = duration
	}
	if outputTokens > 0 && generationDuration > 0 {
		tokensPerSecond := float64(outputTokens) / generationDuration.Seconds()
		if tokensPerSecond >= 0 && !math.IsNaN(tokensPerSecond) && !math.IsInf(tokensPerSecond, 0) {
			timing.TokensPerSecond = &tokensPerSecond
		}
	}
	return timing
}
