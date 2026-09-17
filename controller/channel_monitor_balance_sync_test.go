package controller

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func verifyChannelMonitorBalanceIdleRecovery(t *testing.T, db *gorm.DB) {
	monitor := model.ChannelRatioMonitor{ChannelId: 9041, UpstreamRevision: 1, UpstreamType: service.NewAPIUpstreamType,
		BalanceWarningThreshold: common.GetPointer(30.0), BalanceAutoDisableThreshold: common.GetPointer(5.0)}
	require.NoError(t, db.Create(&monitor).Error)
	first, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(24.0), "")
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, first.Complete)
	config := service.ChannelBalanceConfigForMonitor(monitor)
	root := fmt.Sprintf("channel_balance:{%d}:%s", config.ChannelID, config.Account)
	client := common.RedisMonitorWriteClient()
	encoded, err := common.Marshal(map[string]any{"epoch": first.Estimate.Epoch, "status": "unresolved",
		"amount": "3000000", "known": false, "source": "budget"})
	require.NoError(t, err)
	require.NoError(t, client.Set(t.Context(), root+":attempt:legacy-ended", encoded, time.Hour).Err())
	require.NoError(t, client.ZAdd(t.Context(), root+":active", &redis.Z{
		Score: float64(time.Now().Add(-time.Minute).UnixMilli()), Member: "legacy-ended",
	}).Err())
	require.NoError(t, client.HSet(t.Context(), root+":state",
		"coverage", 0, "active", 1, "unknown_active", 1, "budget_active", 1, "inflight", 3_000_000).Err())

	// A request running when the query begins must prevent restoring coverage,
	// even if it ends before the balance response arrives.
	lease, acquired, _, err := service.AcquireChannelConcurrency(t.Context(), monitor.ChannelId)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(lease.Release)
	sync, err := service.BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	lease.Release()
	require.True(t, service.ChannelBalanceHasIdleRequestCoverage(t.Context(), monitor.ChannelId))
	second, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(22.0), "", sync)
	require.NoError(t, err)
	require.True(t, applied)
	assert.False(t, second.Complete)
	assert.False(t, second.Estimate.Coverage)
	assert.Zero(t, second.Estimate.InFlightCount, "the old unresolved reservation is absorbed even while coverage is incomplete")

	// Seed another released-version reservation: idle recovery must not depend
	// on the stale estimate count becoming zero before checking real leases.
	require.NoError(t, client.Set(t.Context(), root+":attempt:legacy-ended", encoded, time.Hour).Err())
	require.NoError(t, client.ZAdd(t.Context(), root+":active", &redis.Z{
		Score: float64(time.Now().Add(-time.Minute).UnixMilli()), Member: "legacy-ended",
	}).Err())
	require.NoError(t, client.HSet(t.Context(), root+":state",
		"active", 1, "unknown_active", 1, "budget_active", 1, "inflight", 3_000_000).Err())
	third, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(20.0), "")
	require.NoError(t, err)
	require.True(t, applied)
	assert.True(t, third.Complete)
	assert.True(t, third.Estimate.Coverage)
	assert.Zero(t, third.Estimate.InFlightCount)
	assert.Zero(t, third.Estimate.UnknownCount)
	assert.Zero(t, third.Estimate.InFlightConsumption)
	assert.Equal(t, 20.0, third.Estimate.EstimatedBalance)
	stored, err := model.GetChannelRatioMonitor(monitor.ChannelId)
	require.NoError(t, err)
	require.NotNil(t, stored.UpstreamBalance)
	assert.Equal(t, 20.0, *stored.UpstreamBalance)
}
