package service

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelBalanceFixture struct {
	t       *testing.T
	server  *miniredis.Miniredis
	client  *redis.Client
	monitor model.ChannelRatioMonitor
	config  ChannelBalanceConfig
	now     time.Time
}

func newChannelBalanceFixture(t *testing.T) *channelBalanceFixture {
	t.Helper()
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	oldWrite, oldRead, oldEnabled, oldDB := common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB = client, client, true, nil
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled, model.DB = oldWrite, oldRead, oldEnabled, oldDB
	})
	monitor := model.ChannelRatioMonitor{ChannelId: 41, UpstreamType: "new-api", UpstreamRevision: 1,
		BalanceWarningThreshold: common.GetPointer(30.0), BalanceAutoDisableThreshold: common.GetPointer(5.0)}
	f := &channelBalanceFixture{t: t, server: server, client: client, monitor: monitor,
		config: ChannelBalanceConfigForMonitor(monitor), now: time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)}
	server.SetTime(f.now)
	return f
}

func (f *channelBalanceFixture) advance(duration time.Duration) {
	f.now = f.now.Add(duration)
	f.server.SetTime(f.now)
}

func (f *channelBalanceFixture) sync(balance float64) ChannelBalanceEstimate {
	f.t.Helper()
	token, err := BeginChannelBalanceSync(f.t.Context(), f.monitor)
	require.NoError(f.t, err)
	f.advance(time.Millisecond)
	estimate, err := CommitChannelBalanceSync(f.t.Context(), token, balance, true)
	require.NoError(f.t, err)
	f.advance(time.Millisecond)
	return estimate
}

func (f *channelBalanceFixture) operation(op, id, sample string, amount int64, eligible bool, at int64) ChannelBalanceEstimate {
	f.t.Helper()
	useSample := "0"
	if eligible {
		useSample = "1"
	}
	raw, err := runChannelBalanceOperation(f.t.Context(), f.config, op, id, sample,
		common.GetUUID(), amount, id, useSample, at)
	require.NoError(f.t, err)
	estimate, err := decodeChannelBalanceEstimate(raw)
	require.NoError(f.t, err)
	f.advance(time.Millisecond)
	return estimate
}

func TestChannelBalanceCompletionReplacesReservationAndRefreshKeepsInflight(t *testing.T) {
	f := newChannelBalanceFixture(t)
	assert.Equal(t, 24.0, f.sync(24).EstimatedBalance)
	estimate := f.operation("start", "first", "model", 3_000_000, true, 0)
	assert.Equal(t, 21.0, estimate.EstimatedBalance)
	f.operation("start", "second", "model", 1_500_000, true, 0)
	estimate = f.operation("finish", "first", "model", 2_000_000, true, 0)
	assert.Equal(t, 20.5, estimate.EstimatedBalance)
	assert.Equal(t, int64(1), estimate.InFlightCount)
	assert.Equal(t, 2.0, estimate.CompletedConsumption)
	estimate = f.operation("finish", "first", "model", 2_000_000, true, 0)
	assert.Equal(t, 20.5, estimate.EstimatedBalance, "duplicate callbacks cannot charge twice")
	estimate = f.sync(22)
	assert.Zero(t, estimate.CompletedConsumption)
	assert.Equal(t, 1.5, estimate.InFlightConsumption)
	assert.Equal(t, 20.5, estimate.EstimatedBalance)
	estimate = f.operation("release", "second", "model", 0, false, 0)
	assert.Equal(t, 22.0, estimate.EstimatedBalance)
	assert.Zero(t, estimate.InFlightCount)
}

func TestChannelBalanceLateLedgerAndCompletedEventsCannotRedeductOldCosts(t *testing.T) {
	f := newChannelBalanceFixture(t)
	finishedAt := f.now.Add(-time.Minute).UnixMilli()
	f.sync(24.374499)
	// The original bug was 60 x 2 credits arriving in the daily ledger late.
	// A delayed completion must also use its occurrence boundary, not delivery.
	for n := 0; n < 60; n++ {
		f.operation("finish", fmt.Sprint(n), "model", 2_000_000, true, finishedAt)
	}
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Zero(t, estimate.CompletedConsumption)
	assert.Equal(t, 24.374499, estimate.EstimatedBalance)
	assert.Equal(t, "ok", estimate.Decision)
	assert.Nil(t, model.DB, "estimate operations have no database dependency")
}

func TestChannelBalanceQueryWindowCannotAloneDisableAndNextSyncAbsorbsIt(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(8)
	token, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	f.advance(time.Millisecond)
	f.operation("finish", "during-http", "model", 2_000_000, true, 0)
	estimate, err := CommitChannelBalanceSync(t.Context(), token, 6, true)
	require.NoError(t, err)
	assert.Equal(t, 4.0, estimate.EstimatedBalance)
	assert.Equal(t, 2.0, estimate.UncertainConsumption)
	assert.Equal(t, "unknown", estimate.Decision)
	f.advance(time.Millisecond)
	estimate = f.sync(6)
	assert.Equal(t, 6.0, estimate.EstimatedBalance)
	assert.Zero(t, estimate.UncertainConsumption)
	assert.Equal(t, "ok", estimate.Decision)
}

func TestChannelBalanceQueryWithoutDebitAndLaterTopupRetainInflight(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(8)
	f.operation("start", "long-request", "model", 3_000_000, false, 0)
	token, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	f.operation("finish", "during-query", "model", 2_000_000, false, 0)
	// Here the provider took the balance snapshot before charging the request.
	estimate, err := CommitChannelBalanceSync(t.Context(), token, 8, true)
	require.NoError(t, err)
	assert.Equal(t, 3.0, estimate.EstimatedBalance)
	assert.Equal(t, "unknown", estimate.Decision, "query-window fees alone cannot trigger disable")
	estimate = f.sync(6)
	assert.Equal(t, 3.0, estimate.EstimatedBalance)
	assert.Equal(t, "low", estimate.Decision, "the next snapshot confirms the debit")
	estimate = f.sync(500)
	assert.Equal(t, 497.0, estimate.EstimatedBalance, "a top-up or daily reset preserves the outstanding request once")
	assert.Equal(t, int64(1), estimate.InFlightCount)
	estimate = f.operation("finish", "long-request", "model", 2_000_000, false, 0)
	assert.Equal(t, 498.0, estimate.EstimatedBalance)
	assert.Zero(t, estimate.InFlightCount)
}

func TestChannelBalanceDecimalEqualityDoesNotCrossTheThreshold(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.monitor.BalanceAutoDisableThreshold = common.GetPointer(0.1)
	f.config = ChannelBalanceConfigForMonitor(f.monitor)
	f.sync(0.3)
	estimate := f.operation("finish", "decimal", "model", 200_000, true, 0)
	assert.Equal(t, 0.1, estimate.PolicyBalance, "0.3 minus 0.2 must not become 0.09999999999999998")
	assert.Equal(t, 0.2, estimate.PolicyConsumption)
	assert.Equal(t, "ok", estimate.Decision)
}

func TestChannelBalanceOverviewReadsRedisAggregatesAndFencesStaleSnapshots(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	f.operation("start", "pending", "model", 3_000_000, false, 0)
	monitor := f.monitor
	monitor.UpstreamBalance = common.GetPointer(24.0)
	result := GetChannelBalanceEstimates(t.Context(), []model.ChannelRatioMonitor{monitor})
	require.Contains(t, result, monitor.ChannelId)
	assert.True(t, result[monitor.ChannelId].Available)
	assert.Equal(t, 24.0, result[monitor.ChannelId].UpstreamBalance)
	assert.Equal(t, 21.0, result[monitor.ChannelId].EstimatedBalance)
	monitor.UpstreamRevision++
	result = GetChannelBalanceEstimates(t.Context(), []model.ChannelRatioMonitor{monitor})
	assert.False(t, result[monitor.ChannelId].Available)
	assert.NotEmpty(t, result[monitor.ChannelId].Reason)
	assert.Nil(t, model.DB)
}

func TestChannelBalanceSupersededRefreshAndFailuresKeepCurrentState(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	f.operation("start", "active", "model", 3_000_000, false, 0)
	old, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	f.advance(time.Millisecond)
	newer, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	_, err = CommitChannelBalanceSync(t.Context(), newer, 20, true)
	require.NoError(t, err)
	_, err = CommitChannelBalanceSync(t.Context(), old, 100, true)
	require.ErrorIs(t, err, model.ErrChannelRatioMonitorConfigChanged)
	_, err = BeginChannelBalanceSync(t.Context(), f.monitor) // failed HTTP: no commit
	require.NoError(t, err)
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, 17.0, estimate.EstimatedBalance)
	assert.Equal(t, 3.0, estimate.InFlightConsumption)
}

func TestChannelBalanceRecentAverageIsBoundedSeparatedAndFrozen(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(500)
	for n := 0; n < 5; n++ {
		f.operation("finish", fmt.Sprint(n), "model-a-price1", 1_500_000, true, 0)
	}
	f.sync(500) // a sync must not empty the rolling samples
	for n := 0; n < 10; n++ {
		f.operation("start", fmt.Sprintf("live-%d", n), "model-a-price1", 30_000_000, true, 0)
	}
	estimate := f.operation("finish", "live-0", "model-a-price1", 2_000_000, true, 0)
	assert.Equal(t, 15.5, estimate.InFlightConsumption+estimate.CompletedConsumption)
	assert.Equal(t, int64(9), estimate.AverageCount)
	assert.Equal(t, 13.5, estimate.InFlightConsumption, "new samples cannot reprice remaining requests")
	estimate = f.operation("start", "other-model", "model-b", 3_000_000, true, 0)
	assert.Equal(t, int64(1), estimate.BudgetCount)
	estimate = f.operation("start", "new-price", "model-a-price2", 4_000_000, true, 0)
	assert.Equal(t, int64(2), estimate.BudgetCount)
	otherMonitor := f.monitor
	otherMonitor.ChannelId++
	otherConfig := ChannelBalanceConfigForMonitor(otherMonitor)
	raw, err := runChannelBalanceOperation(t.Context(), otherConfig, "start", "other-channel", "model-a-price1",
		common.GetUUID(), 7_000_000, "other-channel", "1")
	require.NoError(t, err)
	other, err := decodeChannelBalanceEstimate(raw)
	require.NoError(t, err)
	assert.Equal(t, 7.0, other.InFlightConsumption, "another channel cannot borrow this channel's samples")
	assert.Zero(t, other.AverageCount)
	f.advance(31 * time.Minute)
	estimate = f.operation("start", "expired", "model-a-price1", 5_000_000, true, 0)
	assert.Equal(t, int64(3), estimate.BudgetCount)
	assert.Equal(t, int64(9), estimate.AverageCount)
}

func TestChannelBalanceAverageEvictsOldestAndIncludesFreeRequests(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(1000)
	for n := 0; n < 101; n++ {
		amount := int64(2_000_000)
		if n == 0 {
			amount = 100_000_000
		}
		f.operation("finish", fmt.Sprint(n), "bounded", amount, true, 0)
	}
	estimate := f.operation("start", "mean", "bounded", 30_000_000, true, 0)
	assert.Equal(t, 2.0, estimate.InFlightConsumption)
	count, err := f.client.ZCard(t.Context(), f.config.key()+":samples:bounded").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(100), count)
	for n := 0; n < 5; n++ {
		f.operation("finish", fmt.Sprintf("free-%d", n), "free", 0, true, 0)
	}
	estimate = f.operation("start", "free-mean", "free", 30_000_000, true, 0)
	assert.Equal(t, 2.0, estimate.InFlightConsumption)
	assert.Equal(t, int64(2), estimate.AverageCount)
}

func TestChannelBalanceUnknownAndOverflowNeverEnableOrCredit(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	estimate := f.operation("start", "unknown", "model", -1, true, 0)
	assert.False(t, estimate.Complete)
	assert.Equal(t, int64(1), estimate.UnknownCount)
	assert.Equal(t, "unknown", estimate.Decision)
	estimate = f.operation("finish", "unknown", "model", 2_000_000, true, 0)
	assert.True(t, estimate.Complete)
	assert.Equal(t, 22.0, estimate.EstimatedBalance)
	f.operation("start", "large-one", "", channelBalanceMaxMicro, false, 0)
	estimate = f.operation("start", "large-two", "", channelBalanceMaxMicro, false, 0)
	assert.False(t, estimate.Available)
	assert.NotEqual(t, "ok", estimate.Decision)
	for _, amount := range []float64{math.NaN(), math.Inf(1), math.MaxFloat64} {
		_, err := channelBalanceMicro(amount)
		require.Error(t, err)
	}
	amount, err := channelBalanceCostMicro(54_400_000, 408.0/(500*30))
	require.NoError(t, err)
	assert.Equal(t, int64(2_000_000), amount, "0.0544 CNY is 2 upstream credits, without a second ratio multiplication")
	_, err = channelBalanceEstimateFromFields(map[string]string{"balance": "24000000", "completed": "0", "uncertain": "2000000"})
	require.Error(t, err, "an inconsistent aggregate cannot produce negative consumption")
}

func TestChannelBalanceRealRedisConcurrentAttempts(t *testing.T) {
	address := os.Getenv("TEST_CHANNEL_BALANCE_REDIS_ADDR")
	if address == "" {
		t.Skip("设置 TEST_CHANNEL_BALANCE_REDIS_ADDR 验证真实 Redis 原子并发")
	}
	require.True(t, strings.HasPrefix(address, "127.0.0.1:"), "use a dedicated loopback test Redis")
	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(t.Context()).Err())
	secondNode := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { assert.NoError(t, secondNode.Close()) })
	oldWrite, oldEnabled, oldDB := common.RDBMonitorWrite, common.RedisEnabled, model.DB
	common.RDBMonitorWrite, common.RedisEnabled, model.DB = client, true, nil
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RedisEnabled, model.DB = oldWrite, oldEnabled, oldDB
		assert.NoError(t, client.Close())
	})
	monitor := model.ChannelRatioMonitor{ChannelId: 4101, UpstreamType: "balance-test-" + common.GetUUID(), UpstreamRevision: 1}
	config := ChannelBalanceConfigForMonitor(monitor)
	token, err := BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	_, err = CommitChannelBalanceSync(t.Context(), token, 200, true)
	require.NoError(t, err)
	barrier := make(chan struct{})
	errors := make(chan error, 60)
	var workers sync.WaitGroup
	for n := 0; n < 60; n++ {
		workers.Add(1)
		go func(n int) {
			defer workers.Done()
			<-barrier
			id := fmt.Sprint(n)
			node := client
			if n%2 == 1 {
				node = secondNode
			}
			root := config.key()
			keys := []string{root + ":state", root + ":attempt:" + id, root + ":active",
				root + ":samples:model", root + ":sample_costs:model", root + ":sample_sum:model",
				fmt.Sprintf("channel_balance:{%d}:recovery:%s", config.ChannelID, id),
				fmt.Sprintf("channel_balance:{%d}:config", config.ChannelID)}
			_, err := channelBalanceScript.Run(t.Context(), node, keys, "start", common.GetUUID(), 3_000_000, id, "0").Result()
			if err == nil {
				_, err = channelBalanceScript.Run(t.Context(), node, keys, "finish", common.GetUUID(), 2_000_000, id, "0", 0).Result()
			}
			errors <- err
		}(n)
	}
	close(barrier)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	estimate, err := GetChannelBalanceEstimate(t.Context(), config)
	require.NoError(t, err)
	assert.Equal(t, 120.0, estimate.CompletedConsumption)
	assert.Zero(t, estimate.InFlightConsumption)
	assert.Equal(t, 80.0, estimate.EstimatedBalance)
	assert.Nil(t, model.DB)
}
