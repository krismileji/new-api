package service

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayPerformanceOutputTokensUsesOfficialCompletionTokens(t *testing.T) {
	assert.Equal(t, 120, RelayPerformanceOutputTokens(120, dto.OutputTokenDetails{
		ReasoningTokens: 80,
		AudioTokens:     5,
	}))
	assert.Equal(t, 120, RelayPerformanceOutputTokens(120, dto.OutputTokenDetails{
		TextTokens:      24,
		ReasoningTokens: 80,
	}))
}

func TestBuildRelayPerformanceTimingPreservesUsageLogTotalDuration(t *testing.T) {
	startedAt := time.Unix(100, 0)
	info := &relaycommon.RelayInfo{
		StartTime:         startedAt,
		FirstResponseTime: startedAt.Add(750 * time.Millisecond),
		IsStream:          true,
	}

	timing := BuildRelayPerformanceTiming(info, 40, startedAt.Add(2500*time.Millisecond))

	assert.Equal(t, int64(2500), timing.AttemptDurationMs)
	require.NotNil(t, timing.FirstTokenMs)
	assert.InDelta(t, 750, *timing.FirstTokenMs, 1e-9)
	require.NotNil(t, timing.TokensPerSecond)
	assert.InDelta(t, 40.0/1.75, *timing.TokensPerSecond, 1e-9)
}

func TestBuildRelayPerformanceTimingUsesLatencyForNonStreamTPS(t *testing.T) {
	startedAt := time.Unix(100, 0)
	info := &relaycommon.RelayInfo{StartTime: startedAt}

	timing := BuildRelayPerformanceTiming(info, 40, startedAt.Add(2*time.Second))

	assert.Nil(t, timing.FirstTokenMs)
	require.NotNil(t, timing.TokensPerSecond)
	assert.InDelta(t, 20, *timing.TokensPerSecond, 1e-9)
}

func TestChannelMonitorPerformanceTimingUsesCurrentRetryAttemptWithoutChangingUsageLogTiming(t *testing.T) {
	requestStartedAt := time.Unix(100, 0)
	attemptStartedAt := requestStartedAt.Add(5 * time.Second)
	firstResponseAt := attemptStartedAt.Add(750 * time.Millisecond)
	completedAt := firstResponseAt.Add(2 * time.Second)
	info := &relaycommon.RelayInfo{
		StartTime:         requestStartedAt,
		FirstResponseTime: firstResponseAt,
		IsStream:          true,
	}
	ctx, _ := gin.CreateTestContext(nil)
	BeginChannelMonitorPerformanceAttempt(ctx, attemptStartedAt)

	logTiming := BuildRelayPerformanceTiming(info, 40, completedAt)
	assert.Equal(t, int64(7750), logTiming.AttemptDurationMs)
	require.NotNil(t, logTiming.FirstTokenMs)
	assert.InDelta(t, 5750, *logTiming.FirstTokenMs, 1e-9)
	require.NotNil(t, logTiming.TokensPerSecond)
	assert.InDelta(t, 20, *logTiming.TokensPerSecond, 1e-9)

	timing := BuildChannelMonitorPerformanceTiming(ctx, info, 40, completedAt)

	assert.Equal(t, int64(2750), timing.AttemptDurationMs)
	require.NotNil(t, timing.FirstTokenMs)
	assert.InDelta(t, 750, *timing.FirstTokenMs, 1e-9)
	require.NotNil(t, timing.TokensPerSecond)
	assert.InDelta(t, 20, *timing.TokensPerSecond, 1e-9)
}

func TestAppendRelayPerformanceTimingLogInfoMarksUnavailableTPS(t *testing.T) {
	other := map[string]interface{}{"frt": -1.0}
	timing := RelayPerformanceTiming{AttemptDurationMs: 300, OutputTokens: 0}

	AppendRelayPerformanceTimingLogInfo(other, timing)

	assert.Equal(t, relayPerformanceTimingVersion, other["performance_timing_version"])
	assert.Equal(t, int64(300), other["performance_duration_ms"])
	assert.Equal(t, 0, other["performance_output_tokens"])
	assert.NotContains(t, other, "frt")
	assert.Contains(t, other, "tokens_per_second")
	assert.Nil(t, other["tokens_per_second"])
}
