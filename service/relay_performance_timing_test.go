package service

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayPerformanceOutputTokensExcludesNonTextUsage(t *testing.T) {
	assert.Equal(t, 35, RelayPerformanceOutputTokens(120, dto.OutputTokenDetails{
		ReasoningTokens: 80,
		AudioTokens:     5,
	}))
	assert.Equal(t, 24, RelayPerformanceOutputTokens(120, dto.OutputTokenDetails{
		TextTokens:      24,
		ReasoningTokens: 80,
	}))
}

func TestBuildRelayPerformanceTimingUsesPreciseTotalDuration(t *testing.T) {
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
	assert.InDelta(t, 16, *timing.TokensPerSecond, 1e-9)
}

func TestBuildRelayPerformanceTimingLeavesNonStreamTPSUnavailable(t *testing.T) {
	startedAt := time.Unix(100, 0)
	info := &relaycommon.RelayInfo{StartTime: startedAt}

	timing := BuildRelayPerformanceTiming(info, 40, startedAt.Add(2*time.Second))

	assert.Nil(t, timing.FirstTokenMs)
	assert.Nil(t, timing.TokensPerSecond)
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
