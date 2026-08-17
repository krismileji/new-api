package service

import (
	"math"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

const relayPerformanceTimingVersion = 1

const channelMonitorPerformanceAttemptStartedAtKey = "channel_monitor_performance_attempt_started_at"

// RelayPerformanceTiming carries timing values for consume logs and
// channel-monitor events; each caller chooses its own timing boundary.
type RelayPerformanceTiming struct {
	CompletedAt       time.Time
	AttemptDurationMs int64
	FirstTokenMs      *float64
	OutputTokens      int
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

func RelayPerformanceOutputTokens(completionTokens int, details dto.OutputTokenDetails) int {
	completionTokens = max(completionTokens, 0)
	if details.TextTokens > 0 {
		if completionTokens > 0 {
			return min(details.TextTokens, completionTokens)
		}
		return details.TextTokens
	}

	nonTextTokens := max(details.ReasoningTokens, 0) +
		max(details.AudioTokens, 0) +
		max(details.ImageTokens, 0)
	return max(completionTokens-nonTextTokens, 0)
}

func BuildRelayPerformanceTiming(
	relayInfo *relaycommon.RelayInfo,
	outputTokens int,
	completedAt time.Time,
) RelayPerformanceTiming {
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	timing := RelayPerformanceTiming{
		CompletedAt:  completedAt,
		OutputTokens: max(outputTokens, 0),
	}
	if relayInfo == nil || relayInfo.StartTime.IsZero() {
		return timing
	}

	duration := completedAt.Sub(relayInfo.StartTime)
	if duration < 0 {
		duration = 0
	}
	timing.AttemptDurationMs = duration.Milliseconds()
	if relayInfo.IsStream && relayInfo.HasSendResponse() {
		firstTokenMs := float64(relayInfo.FirstResponseTime.Sub(relayInfo.StartTime)) / float64(time.Millisecond)
		if firstTokenMs >= 0 && !math.IsNaN(firstTokenMs) && !math.IsInf(firstTokenMs, 0) {
			timing.FirstTokenMs = &firstTokenMs
		}
	}
	if relayInfo.IsStream && timing.OutputTokens > 0 && duration > 0 {
		tokensPerSecond := float64(timing.OutputTokens) / duration.Seconds()
		if tokensPerSecond >= 0 && !math.IsNaN(tokensPerSecond) && !math.IsInf(tokensPerSecond, 0) {
			timing.TokensPerSecond = &tokensPerSecond
		}
	}
	return timing
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
		CompletedAt:  completedAt,
		OutputTokens: max(outputTokens, 0),
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
	if !relayInfo.IsStream || relayInfo.FirstResponseTime.Before(startedAt) ||
		relayInfo.FirstResponseTime.After(completedAt) {
		return timing
	}

	firstTokenMs := float64(relayInfo.FirstResponseTime.Sub(startedAt)) / float64(time.Millisecond)
	if firstTokenMs >= 0 && !math.IsNaN(firstTokenMs) && !math.IsInf(firstTokenMs, 0) {
		timing.FirstTokenMs = &firstTokenMs
	}
	generationDuration := completedAt.Sub(relayInfo.FirstResponseTime)
	if timing.OutputTokens > 0 && generationDuration > 0 {
		tokensPerSecond := float64(timing.OutputTokens) / generationDuration.Seconds()
		if tokensPerSecond >= 0 && !math.IsNaN(tokensPerSecond) && !math.IsInf(tokensPerSecond, 0) {
			timing.TokensPerSecond = &tokensPerSecond
		}
	}
	return timing
}

func AppendRelayPerformanceTimingLogInfo(other map[string]interface{}, timing RelayPerformanceTiming) {
	if other == nil {
		return
	}
	other["performance_timing_version"] = relayPerformanceTimingVersion
	other["performance_duration_ms"] = timing.AttemptDurationMs
	other["performance_output_tokens"] = timing.OutputTokens
	if timing.FirstTokenMs != nil {
		other["frt"] = *timing.FirstTokenMs
	} else {
		delete(other, "frt")
	}
	if timing.TokensPerSecond != nil {
		other["tokens_per_second"] = *timing.TokensPerSecond
	} else {
		other["tokens_per_second"] = nil
	}
}
