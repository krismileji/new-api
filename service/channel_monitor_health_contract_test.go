package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveChannelMonitorMonitoringHealthTracksDegradationAndRecovery(t *testing.T) {
	healthy, state := DeriveChannelMonitorMonitoringHealth(ChannelMonitorHealthInput{
		Now:             100,
		RedisAvailable:  true,
		ConsumerRunning: true,
	}, ChannelMonitorHealthState{})
	require.Equal(t, ChannelMonitorHealthHealthy, healthy.Status)
	assert.Empty(t, healthy.DegradedReasons)

	degraded, state := DeriveChannelMonitorMonitoringHealth(ChannelMonitorHealthInput{
		Now:                110,
		RedisAvailable:     true,
		ConsumerRunning:    true,
		DroppedSampleCount: 3,
		PendingCount:       4,
		ConsumerLagSeconds: 8,
	}, state)
	require.Equal(t, ChannelMonitorHealthDegraded, degraded.Status)
	assert.Equal(t, int64(110), degraded.FirstDegradedAt)
	assert.Equal(t, int64(110), degraded.LastChangedAt)
	assert.Contains(t, degraded.DegradedReasons, "samples_dropped")
	assert.Contains(t, degraded.DegradedReasons, ChannelMonitorRedisDegradedReasonEventBacklog)

	recovered, _ := DeriveChannelMonitorMonitoringHealth(ChannelMonitorHealthInput{
		Now:             120,
		RedisAvailable:  true,
		ConsumerRunning: true,
	}, state)
	require.Equal(t, ChannelMonitorHealthHealthy, recovered.Status)
	assert.Zero(t, recovered.FirstDegradedAt)
	assert.Equal(t, int64(120), recovered.LastChangedAt)
}

func TestDeriveChannelMonitorMonitoringHealthReportsRedisUnavailable(t *testing.T) {
	health, _ := DeriveChannelMonitorMonitoringHealth(ChannelMonitorHealthInput{
		Now:             200,
		RedisAvailable:  false,
		ConsumerRunning: false,
	}, ChannelMonitorHealthState{})

	require.Equal(t, ChannelMonitorHealthUnavailable, health.Status)
	assert.Contains(t, health.DegradedReasons, ChannelMonitorRedisDegradedReasonRedisUnavailable)
	assert.NotContains(t, health.DegradedReasons, ChannelMonitorRedisDegradedReasonConsumerStopped)
}

func TestDeriveChannelMonitorCoverageDoesNotTurnLagIntoComplete(t *testing.T) {
	coverage := DeriveChannelMonitorCoverage(true, 100, 200, 100, 180, nil)
	require.Equal(t, ChannelMonitorCoveragePartial, coverage.Status)
	assert.Contains(t, coverage.Reasons, "processing_lag")

	complete := DeriveChannelMonitorCoverage(true, 100, 200, 100, 200, nil)
	require.Equal(t, ChannelMonitorCoverageComplete, complete.Status)
	assert.Empty(t, complete.Reasons)

	unavailable := DeriveChannelMonitorCoverage(false, 100, 200, 0, 0, nil)
	require.Equal(t, ChannelMonitorCoverageUnavailable, unavailable.Status)
	assert.Contains(t, unavailable.Reasons, "data_source_unavailable")
}

func TestChannelMonitorHealthContractUsesStableJSONFields(t *testing.T) {
	health := ChannelMonitorMonitoringHealth{
		Status:             ChannelMonitorHealthDegraded,
		DegradedReasons:    []string{"samples_dropped"},
		FirstDegradedAt:    100,
		LastChangedAt:      110,
		DroppedSampleCount: 2,
	}
	payload, err := common.Marshal(health)
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"status":"degraded"`)
	assert.Contains(t, string(payload), `"dropped_sample_count":2`)
}
