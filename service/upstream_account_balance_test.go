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

func TestUpstreamAccountDisabledMemberConsumptionStillCounts(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.monitor.UpstreamAccountID, f.monitor.UpstreamAccountRevision = 9, 1
	f.monitor.UpstreamBalanceSyncDisabled = true
	f.monitor.Ratio, f.monitor.UpdatedTime = 2, 1
	f.config = ChannelBalanceConfigForMonitor(f.monitor)
	f.sync(100)
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{MaxPending: 64, MaxBatchSize: 16}, func(context.Context, []model.ChannelDailyCostDelta) error { return nil })
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})
	ctx := newChannelDailyCostTestContext()
	ctx.Set(channelDailyCostSnapshotContextKey, channelDailyCostSnapshot{ChannelId: f.monitor.ChannelId, BalanceConfig: f.config, CostRatioCNY: 2, ConversionFactor: 1, QuotaPerUnit: 500_000, Configured: true})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: f.monitor.ChannelId}, OriginModelName: "shared-wallet-model", PriceData: types.PriceData{UsePrice: true, ModelPrice: .5}}
	BeginChannelDailyCostAttempt(ctx, f.monitor.ChannelId)
	PrepareChannelBalanceAttempt(ctx, info)
	MarkChannelDailyCostRequestDispatched(ctx)
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, 1.0, estimate.InFlightConsumption, "暂停本渠道余额同步不应漏掉同一账户的在途消费")
	f.advance(time.Millisecond)
	RecordPerCallChannelDailyCost(ctx, f.monitor.ChannelId, info.OriginModelName, info.PriceData)
	estimate, err = GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, 1.0, estimate.CompletedConsumption)
	assert.Equal(t, 99.0, estimate.EstimatedBalance)
}

func TestUpstreamAccountBalanceCombinesChannelsAndKeepsOriginalPool(t *testing.T) {
	f := newChannelBalanceFixture(t)
	first := model.ChannelRatioMonitor{ChannelId: 101, UpstreamAccountID: 9, UpstreamAccountRevision: 1}
	second := first
	second.ChannelId, second.Ratio = 102, 2
	a, b := ChannelBalanceConfigForMonitor(first), ChannelBalanceConfigForMonitor(second)
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), first))
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), second))
	args, err := channelBalanceConfigArguments(a, "baseline")
	require.NoError(t, err)
	epoch, err := runChannelBalanceOperation(t.Context(), a, "sync_begin", "", "", args...)
	require.NoError(t, err)
	f.advance(time.Millisecond)
	_, err = CommitChannelBalanceSync(t.Context(), ChannelBalanceSync{Config: a, ID: "baseline", Epoch: epoch}, 100, true)
	require.NoError(t, err)
	f.advance(time.Millisecond)
	_, err = runChannelBalanceOperation(t.Context(), a, "start", "channel-101-request", "price-one", common.GetUUID(), 3_000_000, "channel-101-request", "0", "{}", "model")
	require.NoError(t, err)
	_, err = runChannelBalanceOperation(t.Context(), b, "start", "channel-102-request", "price-two", common.GetUUID(), 5_000_000, "channel-102-request", "0", "{}", "model")
	require.NoError(t, err)
	estimate, err := GetChannelBalanceEstimate(t.Context(), a)
	require.NoError(t, err)
	assert.Equal(t, 92.0, estimate.EstimatedBalance)
	assert.EqualValues(t, 2, estimate.InFlightCount)
	// Duplicate completion uses the originating account even after the channel
	// changes account. It replaces its reservation exactly once.
	second.UpstreamAccountID = 10
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), second))
	for range 2 {
		_, err = runChannelBalanceOperation(t.Context(), b, "finish", "channel-102-request", "price-two", common.GetUUID(), 4_000_000, "channel-102-request", "0", 0, "0")
		require.NoError(t, err)
	}
	estimate, err = GetChannelBalanceEstimate(t.Context(), a)
	require.NoError(t, err)
	assert.Equal(t, 93.0, estimate.EstimatedBalance)
	assert.Equal(t, 4.0, estimate.CompletedConsumption)
	assert.EqualValues(t, 1, estimate.InFlightCount)
	newPool, err := GetChannelBalanceEstimate(t.Context(), ChannelBalanceConfigForMonitor(second))
	require.NoError(t, err)
	assert.Zero(t, newPool.CompletedConsumption)
	assert.False(t, newPool.Available)
	// A late-added channel may have a lower local revision than its account.
	// Detaching must compare membership revisions, not these unrelated counters.
	second.UpstreamAccountRevision, second.UpstreamRevision = 100, 3
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), second))
	second.UpstreamAccountID, second.UpstreamAccountRevision, second.UpstreamRevision = 0, 0, 4
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), second))
	stored, err := f.client.Get(t.Context(), "channel_balance:{102}:config").Result()
	require.NoError(t, err)
	var detached ChannelBalanceConfig
	require.NoError(t, common.UnmarshalJsonStr(stored, &detached))
	assert.Zero(t, detached.AccountID)
	assert.EqualValues(t, 4, detached.ChannelRevision)
}

func TestUpstreamAccountRealRedisSharedPool(t *testing.T) {
	address := os.Getenv("TEST_CHANNEL_BALANCE_REDIS_ADDR")
	if address == "" {
		t.Skip("set TEST_CHANNEL_BALANCE_REDIS_ADDR to an isolated loopback Redis")
	}
	require.True(t, strings.HasPrefix(address, "127.0.0.1:"))
	client := redis.NewClient(&redis.Options{Addr: address})
	oldWrite, oldRead, oldEnabled, oldDB := common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB = client, client, true, nil
	accountID := int(time.Now().UnixNano() % 1_000_000_000)
	first := model.ChannelRatioMonitor{ChannelId: accountID + 1, UpstreamAccountID: accountID, UpstreamAccountRevision: 1, UpstreamRevision: 10}
	second := first
	second.ChannelId++
	a, b := ChannelBalanceConfigForMonitor(first), ChannelBalanceConfigForMonitor(second)
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB = oldWrite, oldRead, oldEnabled, oldDB
		for _, pattern := range []string{a.key() + ":*", fmt.Sprintf("channel_balance:{%d}:*", first.ChannelId), fmt.Sprintf("channel_balance:{%d}:*", second.ChannelId)} {
			keys, err := client.Keys(context.Background(), pattern).Result()
			assert.NoError(t, err)
			if len(keys) > 0 {
				assert.NoError(t, client.Del(context.Background(), keys...).Err())
			}
		}
		assert.NoError(t, client.Close())
	})
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), first))
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), second))
	token, err := BeginChannelBalanceSync(t.Context(), first)
	require.NoError(t, err)
	_, err = CommitChannelBalanceSync(t.Context(), token, 100, true)
	require.NoError(t, err)
	_, err = runChannelBalanceOperation(t.Context(), a, "start", "first", "model", common.GetUUID(), 3_000_000, "first", "0", "{}", "model")
	require.NoError(t, err)
	_, err = runChannelBalanceOperation(t.Context(), b, "start", "second", "model", common.GetUUID(), 5_000_000, "second", "0", "{}", "model")
	require.NoError(t, err)
	estimate, err := GetChannelBalanceEstimate(t.Context(), b)
	require.NoError(t, err)
	assert.Equal(t, 92.0, estimate.EstimatedBalance)
	for range 2 {
		_, err = runChannelBalanceOperation(t.Context(), b, "finish", "second", "model", common.GetUUID(), 4_000_000, "second", "0", 0, "0")
		require.NoError(t, err)
	}
	estimate, err = GetChannelBalanceEstimate(t.Context(), a)
	require.NoError(t, err)
	assert.Equal(t, 93.0, estimate.EstimatedBalance)
	assert.EqualValues(t, 1, estimate.InFlightCount)
	// A delayed publisher from an old association cannot overwrite the locator.
	encoded, err := common.Marshal(b)
	require.NoError(t, err)
	stale := b
	stale.AccountID, stale.ChannelRevision = accountID+10, 9
	staleEncoded, err := common.Marshal(stale)
	require.NoError(t, err)
	_, err = channelBalanceLocatorScript.Run(t.Context(), client, []string{fmt.Sprintf("channel_balance:{%d}:config", second.ChannelId)}, string(staleEncoded), 9).Result()
	require.NoError(t, err)
	stored, err := client.Get(t.Context(), fmt.Sprintf("channel_balance:{%d}:config", second.ChannelId)).Result()
	require.NoError(t, err)
	assert.JSONEq(t, string(encoded), stored)
}
