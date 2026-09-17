package service

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startChannelBalanceOrphanTestAttempt(t *testing.T, f *channelBalanceFixture, id string, amount int64, marker any) {
	t.Helper()
	encoded := ""
	if marker != nil {
		data, err := common.Marshal(marker)
		require.NoError(t, err)
		encoded = string(data)
	}
	_, err := runChannelBalanceOperation(t.Context(), f.config, "start", id, "model", common.GetUUID(), amount, id, "0", encoded)
	require.NoError(t, err)
	f.advance(time.Millisecond)
}

func TestChannelBalanceSyncReclaimsIdleOrphan(t *testing.T) {
	f := newChannelBalanceFixture(t)
	require.NoError(t, f.client.HSet(t.Context(), channelConcurrencyRedisConfigKey, channelConcurrencyRedisLoadedField, 1).Err())
	f.sync(0)
	marker := channelBalanceAttempt{Config: f.config, ConversionFactor: 0.136}
	startChannelBalanceOrphanTestAttempt(t, f, "known", 1456, marker)
	startChannelBalanceOrphanTestAttempt(t, f, "unknown", -1, marker)
	f.advance(6 * time.Hour)
	estimate := f.sync(49.740274)
	assert.True(t, estimate.Complete)
	assert.Zero(t, estimate.InFlightCount)
	assert.Zero(t, estimate.UnknownCount)
	assert.Zero(t, estimate.InFlightConsumption)
	assert.Equal(t, 49.740274, estimate.EstimatedBalance)
	assert.Equal(t, "ok", estimate.Decision)

	// A callback arriving after the fresh baseline must not debit the orphan
	// again or turn an absorbed unknown request back into an active reservation.
	f.operation("finish", "known", "model", 1000, false, 0)
	estimate = f.operation("finish", "unknown", "model", -1, false, 0)
	assert.True(t, estimate.Complete)
	assert.Zero(t, estimate.CompletedConsumption)
	assert.Zero(t, estimate.InFlightCount)
	assert.Equal(t, 49.740274, estimate.EstimatedBalance)
	assert.Nil(t, model.DB)
}

func TestChannelBalanceSyncOrphanRequiresConfirmedSynchronousRequest(t *testing.T) {
	for _, test := range []struct {
		name   string
		marker any
	}{
		{name: "missing recovery marker"},
		{name: "legacy marker lacks completion flag", marker: map[string]any{}},
		{name: "async task or websocket", marker: map[string]any{"CompletionUncertain": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newChannelBalanceFixture(t)
			require.NoError(t, f.client.HSet(t.Context(), channelConcurrencyRedisConfigKey, channelConcurrencyRedisLoadedField, 1).Err())
			f.sync(0)
			startChannelBalanceOrphanTestAttempt(t, f, "pending", -1, test.marker)
			f.advance(6 * time.Hour)
			estimate := f.sync(50)
			assert.False(t, estimate.Complete)
			assert.EqualValues(t, 1, estimate.InFlightCount)
			assert.EqualValues(t, 1, estimate.UnknownCount)
		})
	}
}

func TestChannelBalanceSyncOrphanRequiresIdleAtBothQueryBoundaries(t *testing.T) {
	for _, boundary := range []string{"start", "end", "uninitialized", "query_gap"} {
		t.Run(boundary, func(t *testing.T) {
			f := newChannelBalanceFixture(t)
			if boundary != "uninitialized" {
				require.NoError(t, f.client.HSet(t.Context(), channelConcurrencyRedisConfigKey, channelConcurrencyRedisLoadedField, 1).Err())
			}
			f.sync(0)
			startChannelBalanceOrphanTestAttempt(t, f, "pending", -1, channelBalanceAttempt{Config: f.config})
			f.advance(6 * time.Hour)
			leaseKey := channelConcurrencyRedisActivePrefix + fmt.Sprint(f.config.ChannelID)
			if boundary == "start" {
				require.NoError(t, f.client.ZAdd(t.Context(), leaseKey, &redis.Z{Score: float64(f.now.UnixMilli()), Member: "live"}).Err())
			}
			sync, err := BeginChannelBalanceSync(t.Context(), f.monitor)
			require.NoError(t, err)
			if boundary == "start" {
				require.NoError(t, f.client.Del(t.Context(), leaseKey).Err())
			}
			if boundary == "end" {
				require.NoError(t, f.client.ZAdd(t.Context(), leaseKey, &redis.Z{Score: float64(f.now.UnixMilli()), Member: "live"}).Err())
			}
			if boundary == "query_gap" {
				f.operation("gap", "", "", 0, false, 0)
			}
			f.advance(time.Millisecond)
			estimate, err := CommitChannelBalanceSync(t.Context(), sync, 50, true)
			require.NoError(t, err)
			assert.False(t, estimate.Complete)
			assert.EqualValues(t, 1, estimate.InFlightCount)
			assert.EqualValues(t, 1, estimate.UnknownCount)
		})
	}
}

func TestChannelBalanceProbeLeasePreventsOrphanRecovery(t *testing.T) {
	f := newChannelBalanceFixture(t)
	require.NoError(t, f.client.HSet(t.Context(), channelConcurrencyRedisConfigKey, channelConcurrencyRedisLoadedField, 1).Err())
	f.sync(0)
	startChannelBalanceOrphanTestAttempt(t, f, "pending", -1, channelBalanceAttempt{Config: f.config})
	f.advance(6 * time.Hour)
	lease, err := AcquireChannelBalanceProbeLease(t.Context(), f.config.ChannelID)
	require.NoError(t, err)
	require.NotNil(t, lease)
	t.Cleanup(lease.Release)
	assert.False(t, ChannelBalanceHasIdleRequestCoverage(t.Context(), f.config.ChannelID))
	estimate := f.sync(50)
	assert.False(t, estimate.Complete)
	assert.EqualValues(t, 1, estimate.UnknownCount)
	lease.Release()
	assert.True(t, ChannelBalanceHasIdleRequestCoverage(t.Context(), f.config.ChannelID))
	estimate = f.sync(50)
	assert.True(t, estimate.Complete)
	assert.Zero(t, estimate.InFlightCount)
}

func TestChannelBalanceSyncOrphanRequiresSuccessfulCurrentQuery(t *testing.T) {
	f := newChannelBalanceFixture(t)
	require.NoError(t, f.client.HSet(t.Context(), channelConcurrencyRedisConfigKey, channelConcurrencyRedisLoadedField, 1).Err())
	f.sync(0)
	startChannelBalanceOrphanTestAttempt(t, f, "pending", -1, channelBalanceAttempt{Config: f.config})
	f.advance(6 * time.Hour)
	failed, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	require.NoError(t, FailChannelBalanceSync(t.Context(), failed))
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.EqualValues(t, 1, estimate.UnknownCount)
	older, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	current, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	_, err = CommitChannelBalanceSync(t.Context(), older, 50, true)
	require.ErrorIs(t, err, model.ErrChannelRatioMonitorConfigChanged)
	// A call started during the query must remain, even if a caller supplies
	// complete coverage and the business lease has not yet become visible.
	f.advance(time.Millisecond)
	startChannelBalanceOrphanTestAttempt(t, f, "query-window", -1, channelBalanceAttempt{Config: f.config})
	estimate, err = CommitChannelBalanceSync(t.Context(), current, 50, true)
	require.NoError(t, err)
	assert.False(t, estimate.Complete)
	assert.EqualValues(t, 1, estimate.InFlightCount)
	assert.EqualValues(t, 1, estimate.UnknownCount)
}

func TestChannelBalanceRealRedisReclaimsIdleOrphan(t *testing.T) {
	address := os.Getenv("TEST_CHANNEL_BALANCE_REDIS_ADDR")
	if address == "" {
		t.Skip("设置 TEST_CHANNEL_BALANCE_REDIS_ADDR 验证真实 Redis 残留占用回收")
	}
	require.True(t, strings.HasPrefix(address, "127.0.0.1:"), "use a dedicated loopback test Redis")
	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(t.Context()).Err())
	oldWrite, oldRead, oldEnabled := common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled
	common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled = client, client, true
	t.Cleanup(func() {
		common.RDBMonitorWrite, common.RDBMonitorRead, common.RedisEnabled = oldWrite, oldRead, oldEnabled
		assert.NoError(t, client.Close())
	})
	require.NoError(t, client.HSet(t.Context(), channelConcurrencyRedisConfigKey, channelConcurrencyRedisLoadedField, 1).Err())
	monitor := model.ChannelRatioMonitor{ChannelId: 4103, UpstreamType: "orphan-test-" + common.GetUUID(), UpstreamRevision: 28,
		BalanceWarningThreshold: common.GetPointer(5.0), BalanceAutoDisableThreshold: common.GetPointer(1.0)}
	config := ChannelBalanceConfigForMonitor(monitor)
	sync, err := BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	_, err = CommitChannelBalanceSync(t.Context(), sync, 0, true)
	require.NoError(t, err)
	encoded, err := common.Marshal(channelBalanceAttempt{Config: config, ConversionFactor: 0.136})
	require.NoError(t, err)
	for _, item := range []struct {
		id     string
		amount int64
	}{{"known", 1456}, {"unknown", -1}} {
		_, err = runChannelBalanceOperation(t.Context(), config, "start", item.id, "model", common.GetUUID(), item.amount, item.id, "0", string(encoded))
		require.NoError(t, err)
		// Reproduce pre-query entries without a wall-clock sleep.
		now, err := client.Time(t.Context()).Result()
		require.NoError(t, err)
		require.NoError(t, client.ZAdd(t.Context(), config.key()+":active", &redis.Z{Score: float64(now.Add(-6 * time.Hour).UnixMilli()), Member: item.id}).Err())
	}
	sync, err = BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	estimate, err := CommitChannelBalanceSync(t.Context(), sync, 49.740274, true)
	require.NoError(t, err)
	assert.True(t, estimate.Complete)
	assert.Zero(t, estimate.InFlightCount)
	assert.Zero(t, estimate.UnknownCount)
	assert.Equal(t, 49.740274, estimate.EstimatedBalance)
	_, err = runChannelBalanceOperation(t.Context(), config, "finish", "known", "model", common.GetUUID(), 1000, "known", "0", 0)
	require.NoError(t, err)
	estimate, err = GetChannelBalanceEstimate(t.Context(), config)
	require.NoError(t, err)
	assert.Zero(t, estimate.CompletedConsumption)
	assert.Equal(t, 49.740274, estimate.EstimatedBalance)
}
