package service

import (
	"math"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const relayPerformanceTimingVersion = 1

// RelayPerformanceTiming is the canonical per-request timing result shared by
// consume logs and channel-monitor events.
type RelayPerformanceTiming struct {
	CompletedAt       time.Time
	AttemptDurationMs int64
	FirstTokenMs      *float64
	OutputTokens      int
	TokensPerSecond   *float64
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
