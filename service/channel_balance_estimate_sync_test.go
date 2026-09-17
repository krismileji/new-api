package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelBalanceSyncAbsorbsEndedUnknownUsageAndKeepsLiveRequests(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{MaxPending: 64, MaxBatchSize: 16},
		func(context.Context, []model.ChannelDailyCostDelta) error { return nil })
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})
	snapshot := channelDailyCostSnapshot{ChannelId: f.config.ChannelID, BalanceConfig: f.config,
		CostRatioCNY: 0.1088, ConversionFactor: 0.0272, QuotaPerUnit: 500_000, Configured: true}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: f.config.ChannelID},
		OriginModelName: "sync-model", PriceData: types.PriceData{UsePrice: true, ModelPrice: 0.5,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	ctx := newChannelDailyCostTestContext()
	ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
	BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
	PrepareChannelBalanceAttempt(ctx, info)
	MarkChannelDailyCostRequestDispatched(ctx)
	f.advance(time.Millisecond)
	FinalizeChannelDailyCostAttempt(ctx, f.config.ChannelID, false)
	id := channelDailyCostEventId(ctx, f.config.ChannelID)
	estimate := f.operation("start", "still-running", "other-model", 3_000_000, false, 0)
	assert.Equal(t, int64(2), estimate.InFlightCount)
	assert.Equal(t, int64(1), estimate.UnknownCount)
	assert.Equal(t, 5.0, estimate.InFlightConsumption)
	assert.False(t, estimate.Complete)

	// A failed query cannot release the ended attempt's estimated cost.
	sync, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	require.NoError(t, FailChannelBalanceSync(t.Context(), sync))
	estimate, err = GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, 5.0, estimate.InFlightConsumption)

	// The returned balance already covers the ended attempt's upstream debit.
	estimate = f.sync(22)
	assert.True(t, estimate.Complete)
	assert.Equal(t, int64(1), estimate.InFlightCount)
	assert.Equal(t, int64(1), estimate.BudgetCount)
	assert.Zero(t, estimate.UnknownCount)
	assert.Zero(t, estimate.CompletedConsumption)
	assert.Equal(t, 3.0, estimate.InFlightConsumption)
	assert.Equal(t, 19.0, estimate.EstimatedBalance)

	// Replayed usage and outbox delivery cannot re-add an absorbed debit.
	f.operation("finish", id, "sync-model", 2_000_000, true, 0)
	require.NoError(t, reconcileChannelBalanceCostEvents(t.Context(), f.client, []model.ChannelDailyCostOutbox{
		{EventId: id, ChannelId: f.config.ChannelID, SettledDelta: 1},
	}))
	estimate, err = GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, 19.0, estimate.EstimatedBalance)
	assert.Zero(t, estimate.CompletedConsumption)
	assert.Nil(t, model.DB, "reconciling estimates must not query the ledger")
}

func TestChannelBalanceSyncKeepsUnknownUsageFromQueryWindow(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	f.operation("start", "ended-during-query", "model", 3_000_000, false, 0)
	sync, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	f.advance(time.Millisecond)
	f.operation("finish", "ended-during-query", "model", -1, false, 0)
	estimate, err := CommitChannelBalanceSync(t.Context(), sync, 22, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), estimate.UnknownCount)
	assert.Equal(t, 3.0, estimate.InFlightConsumption)
	assert.False(t, estimate.Complete)

	estimate = f.sync(22)
	assert.True(t, estimate.Complete)
	assert.Zero(t, estimate.InFlightCount)
	assert.Equal(t, 22.0, estimate.EstimatedBalance)
}

func TestChannelBalanceSyncKeepsAsynchronousAndWebSocketReservations(t *testing.T) {
	for _, kind := range []string{"task", "legacy_task", "websocket"} {
		t.Run(kind, func(t *testing.T) {
			f := newChannelBalanceFixture(t)
			f.sync(24)
			ctx := newChannelDailyCostTestContext()
			snapshot := channelDailyCostSnapshot{ChannelId: f.config.ChannelID, BalanceConfig: f.config, ConversionFactor: .0272}
			ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
			BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
			MarkChannelDailyCostRequestDispatched(ctx)
			if kind != "websocket" {
				finishChannelBalanceTaskAttempt(ctx, snapshot)
			} else {
				value, _ := ctx.Get(channelDailyCostAttemptContextKey)
				value.(*channelDailyCostAttemptState).Balance.CompletionUncertain = true
				finishChannelBalanceAttempt(ctx, snapshot, channelDailyCostEventId(ctx, f.config.ChannelID), 54_400_000, true, false)
			}
			if kind == "legacy_task" {
				id := channelDailyCostEventId(ctx, f.config.ChannelID)
				key := f.config.key() + ":attempt:" + id
				encoded, err := f.client.Get(t.Context(), key).Result()
				require.NoError(t, err)
				var attempt map[string]any
				require.NoError(t, common.UnmarshalJsonStr(encoded, &attempt))
				delete(attempt, "completion_uncertain")
				data, err := common.Marshal(attempt)
				require.NoError(t, err)
				require.NoError(t, f.client.Set(t.Context(), key, data, time.Hour).Err())
				data, err = common.Marshal(channelBalanceAttempt{Config: f.config, CompletionUncertain: true})
				require.NoError(t, err)
				require.NoError(t, f.client.Set(t.Context(), fmt.Sprintf("channel_balance:{%d}:recovery:%s", f.config.ChannelID, id), data, time.Hour).Err())
			}
			f.advance(time.Minute)
			estimate := f.sync(22)
			assert.Equal(t, int64(1), estimate.InFlightCount)
			assert.Equal(t, int64(1), estimate.UnknownCount)
			assert.False(t, estimate.Complete, "a submission or incremental usage does not confirm completion")
		})
	}
}

func TestChannelBalanceSyncKeepsTaskWithMissingPriceSnapshot(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{MaxPending: 64, MaxBatchSize: 16},
		func(context.Context, []model.ChannelDailyCostDelta) error { return nil })
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})
	ctx := newChannelDailyCostTestContext()
	ctx.Set(channelDailyCostSnapshotContextKey, channelDailyCostSnapshot{ChannelId: f.config.ChannelID, BalanceConfig: f.config})
	BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
	MarkChannelDailyCostRequestDispatched(ctx)
	_, recorded, err := RecordTaskChannelDailyCost(ctx, f.config.ChannelID, f.now.Unix(), channelDailyCostEventId(ctx, f.config.ChannelID),
		1000, "unpriced-task", types.PriceData{UsePrice: true, ModelPrice: 1})
	require.NoError(t, err)
	assert.False(t, recorded)
	f.advance(time.Minute)
	estimate := f.sync(23)
	assert.Equal(t, int64(1), estimate.InFlightCount)
	assert.Equal(t, int64(1), estimate.UnknownCount)
	assert.False(t, estimate.Complete)
	assert.Nil(t, model.DB)
}

func TestChannelBalanceSyncReconcilesLegacyReservationsAndKeepsModelAverages(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	for n := 0; n < 5; n++ {
		f.operation("finish", fmt.Sprintf("sample-%d", n), "model-a", 1_000_000, true, 0)
	}
	f.operation("start", "legacy", "model-a", 3_000_000, true, 0)
	// Match the released record: no completion flag, with the finalization time
	// in the active index and the async flag only in the outbox recovery marker.
	encoded, err := common.Marshal(map[string]any{"epoch": f.sync(19).Epoch, "status": "unresolved",
		"amount": "1000000", "known": false, "source": "average", "samples": 5})
	require.NoError(t, err)
	require.NoError(t, f.client.Set(t.Context(), f.config.key()+":attempt:legacy", encoded, 48*time.Hour).Err())
	require.NoError(t, f.client.HSet(t.Context(), f.config.key()+":state", "unknown_active", 1).Err())
	encoded, err = common.Marshal(channelBalanceAttempt{Config: f.config})
	require.NoError(t, err)
	require.NoError(t, f.client.Set(t.Context(), fmt.Sprintf("channel_balance:{%d}:recovery:legacy", f.config.ChannelID), encoded, 48*time.Hour).Err())

	estimate := f.sync(18)
	assert.True(t, estimate.Complete)
	assert.Zero(t, estimate.InFlightCount)
	assert.Zero(t, estimate.AverageCount)
	assert.Zero(t, estimate.UnknownCount)
	assert.Equal(t, 18.0, estimate.EstimatedBalance)

	estimate = f.operation("start", "new-model-a", "model-a", 9_000_000, true, 0)
	assert.Equal(t, 1.0, estimate.InFlightConsumption)
	assert.Equal(t, int64(1), estimate.AverageCount)
	estimate = f.operation("start", "new-model-b", "model-b", 3_000_000, true, 0)
	assert.Equal(t, 4.0, estimate.InFlightConsumption)
	assert.Equal(t, int64(1), estimate.BudgetCount)
}

func TestChannelBalanceSyncCleanupProgressesPastLiveRequests(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(1000)
	// Put a finished unknown beyond one cleanup batch of genuinely live calls.
	for n := 0; n < 128; n++ {
		f.operation("start", fmt.Sprintf("live-%d", n), "model", 1_000_000, false, 0)
	}
	f.operation("start", "old-unknown", "model", 2_000_000, false, 0)
	f.operation("finish", "old-unknown", "model", -1, false, 0)
	first := f.sync(998)
	assert.Equal(t, int64(129), first.InFlightCount)
	second := f.sync(998)
	assert.Equal(t, int64(128), second.InFlightCount)
	assert.Zero(t, second.UnknownCount)
	assert.Equal(t, 128.0, second.InFlightConsumption)
	assert.Equal(t, 870.0, second.EstimatedBalance)
}

func TestChannelBalanceSyncCannotHideCoverageGapDuringQuery(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	sync, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	f.operation("gap", "", "", 0, false, 0)
	estimate, err := CommitChannelBalanceSync(t.Context(), sync, 24, true)
	require.NoError(t, err)
	assert.False(t, estimate.Coverage)
	assert.False(t, estimate.Complete)
	assert.Equal(t, "unknown", estimate.Decision)
	assert.True(t, f.sync(24).Complete)
}

func TestChannelBalanceRealRedisAbsorbsUnknownWithoutClearingLiveAttempts(t *testing.T) {
	address := os.Getenv("TEST_CHANNEL_BALANCE_REDIS_ADDR")
	if address == "" {
		t.Skip("设置 TEST_CHANNEL_BALANCE_REDIS_ADDR 验证真实 Redis 同步清理")
	}
	require.True(t, strings.HasPrefix(address, "127.0.0.1:"), "use a dedicated loopback test Redis")
	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(t.Context()).Err())
	oldWrite, oldRead, oldEnabled, oldDB := common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB = client, client, true, nil
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB = oldWrite, oldRead, oldEnabled, oldDB
		assert.NoError(t, client.Close())
	})
	monitor := model.ChannelRatioMonitor{ChannelId: 4102, UpstreamType: "balance-sync-test-" + common.GetUUID(), UpstreamRevision: 1}
	config := ChannelBalanceConfigForMonitor(monitor)
	sync, err := BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	_, err = CommitChannelBalanceSync(t.Context(), sync, 24, true)
	require.NoError(t, err)
	for _, id := range []string{"ended", "live"} {
		_, err = runChannelBalanceOperation(t.Context(), config, "start", id, "model", common.GetUUID(), 3_000_000, id, "0")
		require.NoError(t, err)
	}
	_, err = runChannelBalanceOperation(t.Context(), config, "finish", "ended", "model", common.GetUUID(), -1, "ended", "0", 0)
	require.NoError(t, err)
	// Establish an unambiguous pre-query completion boundary without sleeps.
	now, err := client.Time(t.Context()).Result()
	require.NoError(t, err)
	require.NoError(t, client.ZAdd(t.Context(), config.key()+":active", &redis.Z{Score: float64(now.Add(-time.Second).UnixMilli()), Member: "ended"}).Err())
	sync, err = BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	estimate, err := CommitChannelBalanceSync(t.Context(), sync, 22, true)
	require.NoError(t, err)
	assert.True(t, estimate.Complete)
	assert.Equal(t, int64(1), estimate.InFlightCount)
	assert.Equal(t, 3.0, estimate.InFlightConsumption)
	assert.Zero(t, estimate.UnknownCount)
	assert.Equal(t, 19.0, estimate.EstimatedBalance)
	_, err = runChannelBalanceOperation(t.Context(), config, "finish", "ended", "model", common.GetUUID(), 2_000_000, "ended", "1", 0)
	require.NoError(t, err)
	estimate, err = GetChannelBalanceEstimate(t.Context(), config)
	require.NoError(t, err)
	assert.Zero(t, estimate.CompletedConsumption)
	assert.Equal(t, 19.0, estimate.EstimatedBalance)
}
