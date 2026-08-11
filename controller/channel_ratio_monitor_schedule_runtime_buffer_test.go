package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRuntimeSettingsCacheReusesParsedSnapshot(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
		channelMonitorSmartScheduleIntervalOption:      "10",
	})
	channelMonitorRuntimeSettingsCache.Store(nil)

	first := getChannelMonitorRuntimeSettings()
	firstSnapshot := channelMonitorRuntimeSettingsCache.Load()
	require.NotNil(t, firstSnapshot)
	second := getChannelMonitorRuntimeSettings()
	secondSnapshot := channelMonitorRuntimeSettingsCache.Load()
	require.NotNil(t, secondSnapshot)
	assert.Same(t, firstSnapshot, secondSnapshot)
	assert.True(t, first.SmartScheduleEnabled)
	assert.Equal(t, 10, second.SmartScheduleIntervalMinutes)

	common.OptionMapRWMutex.Lock()
	common.OptionMap[channelMonitorSmartScheduleEnabledOption] = "false"
	common.OptionMapRWMutex.Unlock()
	changed := getChannelMonitorRuntimeSettings()
	assert.False(t, changed.SmartScheduleEnabled)
	assert.NotSame(t, firstSnapshot, channelMonitorRuntimeSettingsCache.Load())
}

func TestChannelSmartScheduleRuntimeSuccessQueueFlushesInBatch(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, client.Close())
	})
	resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	channelSmartScheduleRuntimeRedisSuccessQueue.running = true
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()

	const (
		channelId = 1701
		modelName = "model-a"
		revision  = "buffer-test"
	)
	enqueueChannelSmartScheduleRuntimeRedisSuccess(
		channelId, modelName, common.GetTimestamp(), 3600, revision,
	)
	assert.Equal(t, 1, channelSmartScheduleRuntimeRedisSuccessPendingForTest())
	key := channelSmartScheduleRuntimeFailureRedisKey(channelId, modelName, revision)
	assert.Equal(t, int64(0), client.ZCard(context.Background(), key).Val())

	flushChannelSmartScheduleRuntimeRedisSuccesses()
	assert.Equal(t, 0, channelSmartScheduleRuntimeRedisSuccessPendingForTest())
	assert.Equal(t, int64(1), client.ZCard(context.Background(), key).Val())
}

func TestChannelSmartScheduleRuntimeSuccessQueueRetryIsIdempotent(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, client.Close())
	})
	resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	channelSmartScheduleRuntimeRedisSuccessQueue.running = true
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()

	key := channelSmartScheduleRuntimeRedisSuccessKey{
		channelId: 1704, modelName: "model-a", revision: "retry-test", retentionSecond: 3600,
	}
	event := channelSmartScheduleRuntimeRedisSuccessEvent{timestamp: 100, sequence: 42}
	requeueChannelSmartScheduleRuntimeRedisSuccesses(map[channelSmartScheduleRuntimeRedisSuccessKey][]channelSmartScheduleRuntimeRedisSuccessEvent{
		key: {event},
	})
	requeueChannelSmartScheduleRuntimeRedisSuccesses(map[channelSmartScheduleRuntimeRedisSuccessKey][]channelSmartScheduleRuntimeRedisSuccessEvent{
		key: {event},
	})
	assert.Equal(t, 2, channelSmartScheduleRuntimeRedisSuccessPendingForTest())

	flushChannelSmartScheduleRuntimeRedisSuccesses()
	assert.Zero(t, channelSmartScheduleRuntimeRedisSuccessPendingForTest())
	redisKey := channelSmartScheduleRuntimeFailureRedisKey(key.channelId, key.modelName, key.revision)
	assert.Equal(t, int64(1), client.ZCard(context.Background(), redisKey).Val())
}

func TestChannelSmartScheduleRuntimeSuccessQueueRequeuesPipelineFailure(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	goodClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	badClient := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 10 * time.Millisecond,
		ReadTimeout: 10 * time.Millisecond, WriteTimeout: 10 * time.Millisecond,
	})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = badClient
	t.Cleanup(func() {
		resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, badClient.Close())
		require.NoError(t, goodClient.Close())
	})
	resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	channelSmartScheduleRuntimeRedisSuccessQueue.running = true
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()

	enqueueChannelSmartScheduleRuntimeRedisSuccess(1706, "model-a", 100, 3600, "pipeline-retry")
	flushChannelSmartScheduleRuntimeRedisSuccesses()
	assert.Equal(t, 1, channelSmartScheduleRuntimeRedisSuccessPendingForTest())

	common.RDB = goodClient
	flushChannelSmartScheduleRuntimeRedisSuccesses()
	assert.Zero(t, channelSmartScheduleRuntimeRedisSuccessPendingForTest())
	redisKey := channelSmartScheduleRuntimeFailureRedisKey(1706, "model-a", "pipeline-retry")
	assert.Equal(t, int64(1), goodClient.ZCard(context.Background(), redisKey).Val())
}

func TestChannelSmartScheduleRuntimeRequestSuccessSkipsNonParticipatingCachedRoute(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
	})
	model.InitChannelCache()
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{"model-a"}, 2, 80, 30,
	)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t, policy),
	})
	channelMonitorRuntimeSettingsCache.Store(nil)
	resetChannelSmartScheduleRuntimeHealthForTest()
	resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()

	observeChannelSmartScheduleRuntimeRequestSuccess(1705, "model-a")

	assert.Zero(t, channelSmartScheduleRuntimeHealthStateCountForTest())
	assert.Zero(t, channelSmartScheduleRuntimeRedisSuccessPendingForTest())
}

func TestChannelSmartScheduleRuntimeFailureFlushesPendingSuccessBeforeReadingWindow(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, client.Close())
	})
	resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	channelSmartScheduleRuntimeRedisSuccessQueue.running = true
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()

	const revision = "failure-flush-test"
	enqueueChannelSmartScheduleRuntimeRedisSuccess(1702, "model-a", 100, 3600, revision)
	failure := observeChannelSmartScheduleRuntimeFailure(
		1702, "model-a", 100, 3600, revision,
	)
	require.Len(t, failure.RequestEvents, 2)
	assert.False(t, failure.RequestEvents[0].Failure)
	assert.True(t, failure.RequestEvents[1].Failure)

	runtimeError := types.NewErrorWithStatusCode(errors.New("upstream"), types.ErrorCodeGetChannelFailed, 503)
	assert.True(t, isChannelSmartScheduleRuntimeFailure(runtimeError))
}

func TestChannelSmartScheduleRuntimeDroppedSuccessKeepsSharedWindowNonAuthoritative(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	originalMaxPending := channelSmartScheduleRuntimeRedisMaxPending
	common.RedisEnabled = true
	common.RDB = client
	channelSmartScheduleRuntimeRedisMaxPending = 1
	t.Cleanup(func() {
		resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
		resetChannelSmartScheduleRuntimeHealthForTest()
		channelSmartScheduleRuntimeRedisMaxPending = originalMaxPending
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		require.NoError(t, client.Close())
	})
	resetChannelSmartScheduleRuntimeRedisSuccessQueueForTest()
	resetChannelSmartScheduleRuntimeHealthForTest()
	channelSmartScheduleRuntimeRedisSuccessQueue.Lock()
	channelSmartScheduleRuntimeRedisSuccessQueue.running = true
	channelSmartScheduleRuntimeRedisSuccessQueue.Unlock()

	const revision = "incomplete-window-test"
	now := common.GetTimestamp()
	recordChannelSmartScheduleRuntimeSuccessLocal(1707, "model-a", now, revision)
	enqueueChannelSmartScheduleRuntimeRedisSuccess(1707, "model-a", now, 3600, revision)
	recordChannelSmartScheduleRuntimeSuccessLocal(1707, "model-a", now+1, revision)
	enqueueChannelSmartScheduleRuntimeRedisSuccess(1707, "model-a", now+1, 3600, revision)

	snapshot := observeChannelSmartScheduleRuntimeFailure(
		1707, "model-a", now+2, 3600, revision,
	)
	assert.False(t, snapshot.WindowComplete)
	require.Len(t, snapshot.RequestEvents, 3)
	assert.False(t, snapshot.RequestEvents[0].Failure)
	assert.False(t, snapshot.RequestEvents[1].Failure)
	assert.True(t, snapshot.RequestEvents[2].Failure)
	assert.Equal(t, 1, snapshot.ConsecutiveFailures)

	redisKey := channelSmartScheduleRuntimeFailureRedisKey(1707, "model-a", revision)
	assert.Equal(t, int64(2), client.ZCard(context.Background(), redisKey).Val())
	assert.Equal(
		t,
		int64(1),
		client.Exists(
			context.Background(), channelSmartScheduleRuntimeRedisIncompleteKey(redisKey),
		).Val(),
	)
}

func TestChannelSmartScheduleRuntimeHealthCleanupRemovesOnlyInactiveRoutes(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	resetChannelSmartScheduleRuntimeHealthForTest()
	t.Cleanup(resetChannelSmartScheduleRuntimeHealthForTest)

	now := common.GetTimestamp()
	staleKey := channelSmartScheduleRuntimeHealthRouteKey(1708, "stale-model")
	activeKey := channelSmartScheduleRuntimeHealthRouteKey(1709, "active-model")
	shard := channelSmartScheduleRuntimeHealthShardForKey(staleKey)
	shard.Lock()
	resetChannelSmartScheduleRuntimeHealthIfDatabaseChangedLocked(shard)
	shard.states[staleKey] = channelSmartScheduleRuntimeHealthState{
		Revision: "cleanup-test",
		LastSeen: now - int64(maxChannelSmartScheduleRuntimeRetentionSeconds()) - 1,
	}
	shard.states[activeKey] = channelSmartScheduleRuntimeHealthState{
		Revision: "cleanup-test",
		LastSeen: now,
	}
	shard.Unlock()

	cleanupChannelSmartScheduleRuntimeHealthShard(shard, now)

	shard.Lock()
	_, staleExists := shard.states[staleKey]
	_, activeExists := shard.states[activeKey]
	shard.Unlock()
	assert.False(t, staleExists)
	assert.True(t, activeExists)
}
