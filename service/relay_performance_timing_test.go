package service

import (
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorPerformanceTimingUsesCurrentRetryAttempt(t *testing.T) {
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

	timing := BuildChannelMonitorPerformanceTiming(ctx, info, 40, completedAt)

	assert.Equal(t, int64(2750), timing.AttemptDurationMs)
	require.NotNil(t, timing.FirstTokenMs)
	assert.InDelta(t, 750, *timing.FirstTokenMs, 1e-9)
	require.NotNil(t, timing.TokensPerSecond)
	assert.InDelta(t, 20, *timing.TokensPerSecond, 1e-9)
}

func TestChannelMonitorPerformanceTimingUsesFullAttemptForNonStreamTPS(t *testing.T) {
	startedAt := time.Unix(200, 0)
	info := &relaycommon.RelayInfo{StartTime: startedAt}
	ctx, _ := gin.CreateTestContext(nil)
	BeginChannelMonitorPerformanceAttempt(ctx, startedAt)

	timing := BuildChannelMonitorPerformanceTiming(ctx, info, 40, startedAt.Add(2*time.Second))

	assert.Equal(t, int64(2000), timing.AttemptDurationMs)
	assert.Nil(t, timing.FirstTokenMs)
	require.NotNil(t, timing.TokensPerSecond)
	assert.InDelta(t, 20, *timing.TokensPerSecond, 1e-9)
}

func TestChannelMonitorPerformanceTimingIgnoresPriorAttemptFirstResponse(t *testing.T) {
	requestStartedAt := time.Unix(300, 0)
	attemptStartedAt := requestStartedAt.Add(5 * time.Second)
	info := &relaycommon.RelayInfo{
		StartTime:         requestStartedAt,
		FirstResponseTime: requestStartedAt.Add(time.Second),
		IsStream:          true,
	}
	ctx, _ := gin.CreateTestContext(nil)
	BeginChannelMonitorPerformanceAttempt(ctx, attemptStartedAt)

	timing := BuildChannelMonitorPerformanceTiming(ctx, info, 40, attemptStartedAt.Add(2*time.Second))

	assert.Equal(t, int64(2000), timing.AttemptDurationMs)
	assert.Nil(t, timing.FirstTokenMs)
	require.NotNil(t, timing.TokensPerSecond)
	assert.InDelta(t, 20, *timing.TokensPerSecond, 1e-9)
}
