package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockingCooldownPipelineHook struct {
	once    sync.Once
	after   chan struct{}
	release chan struct{}
}

type channelRateLimitCooldownEvalErrorHook struct {
	script string
	err    error
	calls  atomic.Int64
}

type channelRateLimitCooldownPipelineErrorHook struct {
	err error
}

type channelRateLimitBypassReadErrorHook struct {
	err error
}

func (hook *channelRateLimitBypassReadErrorHook) BeforeProcess(
	ctx context.Context,
	command redis.Cmder,
) (context.Context, error) {
	if command.Name() == "zrangebyscore" {
		return ctx, hook.err
	}
	return ctx, nil
}

func (hook *channelRateLimitBypassReadErrorHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *channelRateLimitBypassReadErrorHook) BeforeProcessPipeline(
	ctx context.Context,
	_ []redis.Cmder,
) (context.Context, error) {
	return ctx, nil
}

func (hook *channelRateLimitBypassReadErrorHook) AfterProcessPipeline(
	context.Context,
	[]redis.Cmder,
) error {
	return nil
}

func (hook *channelRateLimitCooldownPipelineErrorHook) BeforeProcess(
	ctx context.Context,
	_ redis.Cmder,
) (context.Context, error) {
	return ctx, nil
}

func (hook *channelRateLimitCooldownPipelineErrorHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *channelRateLimitCooldownPipelineErrorHook) BeforeProcessPipeline(
	ctx context.Context,
	_ []redis.Cmder,
) (context.Context, error) {
	return ctx, hook.err
}

func (hook *channelRateLimitCooldownPipelineErrorHook) AfterProcessPipeline(
	context.Context,
	[]redis.Cmder,
) error {
	return nil
}

func (hook *channelRateLimitCooldownEvalErrorHook) BeforeProcess(
	ctx context.Context,
	command redis.Cmder,
) (context.Context, error) {
	if hook.calls.Load() > 0 || command.Name() != "eval" {
		return ctx, nil
	}
	arguments := command.Args()
	if len(arguments) < 2 || fmt.Sprint(arguments[1]) != hook.script {
		return ctx, nil
	}
	hook.calls.Add(1)
	return ctx, hook.err
}

func (hook *channelRateLimitCooldownEvalErrorHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *channelRateLimitCooldownEvalErrorHook) BeforeProcessPipeline(
	ctx context.Context,
	_ []redis.Cmder,
) (context.Context, error) {
	return ctx, nil
}

func (hook *channelRateLimitCooldownEvalErrorHook) AfterProcessPipeline(
	context.Context,
	[]redis.Cmder,
) error {
	return nil
}

func (hook *blockingCooldownPipelineHook) BeforeProcess(ctx context.Context, _ redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (hook *blockingCooldownPipelineHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *blockingCooldownPipelineHook) BeforeProcessPipeline(
	ctx context.Context,
	_ []redis.Cmder,
) (context.Context, error) {
	return ctx, nil
}

func (hook *blockingCooldownPipelineHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	hook.once.Do(func() {
		close(hook.after)
		<-hook.release
	})
	return nil
}

func useChannelRateLimitCooldownRedis(t *testing.T) {
	t.Helper()
	stopChannelRateLimitCooldownRedisSync()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	ClearChannelRateLimitCooldowns()
	t.Cleanup(func() {
		stopChannelRateLimitCooldownRedisSync()
		ClearChannelRateLimitCooldowns()
		require.NoError(t, client.Close())
		common.RedisEnabled = originalEnabled
		common.RDB = originalClient
	})
}

func setChannelRateLimitCooldownControlRevision(t *testing.T, revision string) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	option := model.Option{Key: model.ChannelSmartScheduleControlRevisionOption}
	require.NoError(t, model.DB.FirstOrCreate(&option, model.Option{
		Key: model.ChannelSmartScheduleControlRevisionOption,
	}).Error)
	require.NoError(t, model.DB.Model(&model.Option{}).
		Where("key = ?", model.ChannelSmartScheduleControlRevisionOption).
		Update("value", revision).Error)
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousRevision, existed := common.OptionMap[model.ChannelSmartScheduleControlRevisionOption]
	common.OptionMap[model.ChannelSmartScheduleControlRevisionOption] = revision
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		if existed {
			common.OptionMap[model.ChannelSmartScheduleControlRevisionOption] = previousRevision
		} else {
			delete(common.OptionMap, model.ChannelSmartScheduleControlRevisionOption)
		}
		common.OptionMapRWMutex.Unlock()
	})
}

func resetChannelRateLimitCooldownLocalState() {
	channelRateLimitCooldowns.Lock()
	channelRateLimitCooldowns.untilByRoute = make(map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry)
	publishChannelRateLimitCooldownSnapshotLocked()
	channelRateLimitCooldowns.Unlock()
}

func TestChannelRateLimitCooldownAppliesOnlyToMatchingModelAndPreservesExclusions(t *testing.T) {
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)

	StartChannelRateLimitCooldown(12, "model-a", 30)
	StartChannelRateLimitCooldown(13, "model-b", 30)

	options := applyChannelRateLimitCooldowns("model-a", model.ChannelSelectionOptions{
		ExcludedChannelIds: []int{14, 12},
	})
	assert.Equal(t, []int{12, 14}, options.ExcludedChannelIds)

	otherModelOptions := applyChannelRateLimitCooldowns("model-c", model.ChannelSelectionOptions{
		ExcludedChannelIds: []int{14},
	})
	assert.Equal(t, []int{14}, otherModelOptions.ExcludedChannelIds)
}

func TestChannelRateLimitCooldownExpiresAndCannotBeShortened(t *testing.T) {
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)

	StartChannelRateLimitCooldown(21, "model-a", 60)
	firstUntil := ChannelRateLimitCooldownUntil(21, "model-a")
	StartChannelRateLimitCooldown(21, "model-a", 10)
	assert.Equal(t, firstUntil, ChannelRateLimitCooldownUntil(21, "model-a"))

	assert.Empty(t, channelRateLimitCooldownChannelIds("model-a", common.GetTimestamp()+61))
}

func TestUpdateChannelRateLimitCooldownAllowsManualPauseAndRouteClear(t *testing.T) {
	stopChannelRateLimitCooldownRedisSync()
	resetChannelRateLimitCooldownLocalState()
	setChannelRateLimitCooldownControlRevision(t, "revision-manual")
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalEnabled
		common.RDB = originalClient
		resetChannelRateLimitCooldownLocalState()
	})

	paused, err := UpdateChannelRateLimitCooldown(
		context.Background(), 31, "model-manual", 30,
	)
	require.NoError(t, err)
	assert.True(t, paused.Changed)
	assert.Greater(t, paused.CooldownUntil, common.GetTimestamp())
	assert.Contains(t, channelRateLimitCooldownChannelIds("model-manual", common.GetTimestamp()), 31)

	cleared, err := UpdateChannelRateLimitCooldown(
		context.Background(), 31, "model-manual", 0,
	)
	require.NoError(t, err)
	assert.True(t, cleared.Changed)
	assert.Zero(t, cleared.CooldownUntil)
	assert.NotContains(t, channelRateLimitCooldownChannelIds("model-manual", common.GetTimestamp()), 31)
}

func TestChannelRateLimitBypassClearsAndPreventsCooldown(t *testing.T) {
	stopChannelRateLimitCooldownRedisSync()
	resetChannelRateLimitCooldownLocalState()
	ClearChannelRateLimitBypasses()
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalEnabled
		common.RDB = originalClient
		resetChannelRateLimitCooldownLocalState()
		ClearChannelRateLimitBypasses()
	})

	StartChannelRateLimitCooldown(33, "model-a", 60)
	require.Positive(t, ChannelRateLimitCooldownUntilMatching(33, "model-a"))
	bypassed, err := UpdateChannelRateLimitBypass(context.Background(), 33, "model-a", 120)
	require.NoError(t, err)
	assert.True(t, bypassed.Changed)
	assert.Positive(t, bypassed.BypassUntil)
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(33, "model-a"))

	StartChannelRateLimitCooldown(33, "model-a", 60)
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(33, "model-a"))
}

func TestChannelRateLimitBypassUsesSharedRedisStateBeforeCooldown(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	ClearChannelRateLimitCooldowns()
	ClearChannelRateLimitBypasses()
	t.Cleanup(ClearChannelRateLimitCooldowns)
	t.Cleanup(ClearChannelRateLimitBypasses)

	_, err := UpdateChannelRateLimitBypass(context.Background(), 34, "model-a", 120)
	require.NoError(t, err)
	channelRateLimitBypasses.Lock()
	channelRateLimitBypasses.untilByRoute = make(map[channelRateLimitCooldownKey]int64)
	channelRateLimitBypassGeneration.Add(1)
	channelRateLimitBypasses.Unlock()

	assert.True(t, ChannelRateLimitBypassActive(context.Background(), 34, "model-a"))
	StartChannelRateLimitCooldown(34, "model-a", 60)
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(34, "model-a"))
}

func TestChannelRateLimitBypassUpdateFailurePreservesExistingCooldown(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	ClearChannelRateLimitBypasses()
	t.Cleanup(ClearChannelRateLimitBypasses)
	StartChannelRateLimitCooldown(35, "model-a", 60)
	require.Positive(t, ChannelRateLimitCooldownUntilMatching(35, "model-a"))
	stopChannelRateLimitCooldownRedisSync()

	writeErr := errors.New("redis bypass update failed")
	hook := &channelRateLimitCooldownEvalErrorHook{
		script: channelRateLimitBypassRedisUpdateScript,
		err:    writeErr,
	}
	common.RDB.AddHook(hook)

	_, err := UpdateChannelRateLimitBypass(context.Background(), 35, "model-a", 120)
	assert.ErrorIs(t, err, writeErr)
	assert.Equal(t, int64(1), hook.calls.Load())
	assert.Positive(t, ChannelRateLimitCooldownUntilMatching(35, "model-a"))
	_, err = common.RDB.ZScore(
		context.Background(),
		channelRateLimitBypassRedisKey,
		channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
			channelId: 35,
			modelName: "model-a",
		}),
	).Result()
	assert.ErrorIs(t, err, redis.Nil)
}

func TestChannelRateLimitBypassPreservesConsumedCooldownSequence(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	ClearChannelRateLimitBypasses()
	t.Cleanup(ClearChannelRateLimitBypasses)
	now := common.GetTimestamp()
	member := channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
		channelId: 39,
		modelName: "model-a",
	})
	require.NoError(t, common.RDB.ZAdd(
		context.Background(),
		channelRateLimitCooldownRedisKey,
		&redis.Z{Score: float64(now + 60), Member: member},
	).Err())
	require.NoError(t, common.RDB.HSet(
		context.Background(),
		channelRateLimitCooldownRedisEventSequenceKey,
		member,
		"115000000000000021",
	).Err())

	_, err := UpdateChannelRateLimitBypass(context.Background(), 39, "model-a", 120)
	require.NoError(t, err)
	_, err = common.RDB.ZScore(
		context.Background(), channelRateLimitCooldownRedisKey, member,
	).Result()
	assert.ErrorIs(t, err, redis.Nil)
	sequence, err := common.RDB.HGet(
		context.Background(), channelRateLimitCooldownRedisEventSequenceKey, member,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, "115000000000000021", sequence)
}

func TestChannelRateLimitBypassImmediatelyOverridesRemoteCooldown(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	ClearChannelRateLimitBypasses()
	t.Cleanup(ClearChannelRateLimitBypasses)
	StartChannelRateLimitCooldown(36, "model-a", 60)
	require.Equal(t, []int{36}, applyChannelRateLimitCooldowns(
		"model-a", model.ChannelSelectionOptions{},
	).ExcludedChannelIds)

	now := common.GetTimestamp()
	require.NoError(t, common.RDB.ZAdd(
		context.Background(),
		channelRateLimitBypassRedisKey,
		&redis.Z{
			Score: float64(now + 120),
			Member: channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
				channelId: 36,
				modelName: "model-a",
			}),
		},
	).Err())
	channelRateLimitBypasses.Lock()
	channelRateLimitBypasses.untilByRoute = make(map[channelRateLimitCooldownKey]int64)
	channelRateLimitBypassGeneration.Add(1)
	channelRateLimitBypasses.Unlock()

	assert.Empty(t, applyChannelRateLimitCooldowns(
		"model-a", model.ChannelSelectionOptions{},
	).ExcludedChannelIds)
	StartChannelRateLimitCooldown(36, "model-a", 300)
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(36, "model-a"))
	_, err := common.RDB.ZScore(
		context.Background(),
		channelRateLimitCooldownRedisKey,
		channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
			channelId: 36,
			modelName: "model-a",
		}),
	).Result()
	assert.ErrorIs(t, err, redis.Nil)
}

func TestChannelRateLimitBypassConsumesGuardedCooldownEventSequence(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	ClearChannelRateLimitBypasses()
	t.Cleanup(ClearChannelRateLimitBypasses)
	setChannelRateLimitCooldownControlRevision(t, "revision-bypass-sequence")
	updated, err := UpdateChannelRateLimitCooldownControlRevision(
		"revision-bypass-sequence", "",
	)
	require.NoError(t, err)
	require.True(t, updated)

	now := common.GetTimestamp()
	member := channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
		channelId: 37,
		modelName: "model-a",
	})
	require.NoError(t, common.RDB.ZAdd(
		context.Background(),
		channelRateLimitBypassRedisKey,
		&redis.Z{Score: float64(now + 120), Member: member},
	).Err())
	sequence := int64(115_000_000_000_000_020)
	accepted, err := StartChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(), 37, "model-a", now+300,
		"revision-bypass-sequence", sequence,
	)
	require.NoError(t, err)
	assert.False(t, accepted)
	assert.Zero(t, ChannelRateLimitCooldownUntil(37, "model-a"))
	storedSequence, err := common.RDB.HGet(
		context.Background(), channelRateLimitCooldownRedisEventSequenceKey, member,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%019d", sequence), storedSequence)

	require.NoError(t, common.RDB.ZRem(
		context.Background(), channelRateLimitBypassRedisKey, member,
	).Err())
	accepted, err = StartChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(), 37, "model-a", now+300,
		"revision-bypass-sequence", sequence,
	)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.Zero(t, ChannelRateLimitCooldownUntil(37, "model-a"))
}

func TestChannelRateLimitBypassReadFailureFailsSafe(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	ClearChannelRateLimitBypasses()
	t.Cleanup(ClearChannelRateLimitBypasses)
	StartChannelRateLimitCooldown(38, "model-a", 60)
	require.Positive(t, ChannelRateLimitCooldownUntilMatching(38, "model-a"))
	stopChannelRateLimitCooldownRedisSync()

	readErr := errors.New("redis bypass read failed")
	common.RDB.AddHook(&channelRateLimitBypassReadErrorHook{err: readErr})

	assert.True(t, ChannelRateLimitBypassActive(context.Background(), 38, "model-a"))
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(38, "model-a"))
	assert.Empty(t, applyChannelRateLimitCooldowns(
		"model-a", model.ChannelSelectionOptions{},
	).ExcludedChannelIds)
}

func TestChannelRateLimitCooldownMatchesAndClearsWildcardRoutes(t *testing.T) {
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)

	StartChannelRateLimitCooldown(32, "model-wild-*", 30)
	StartChannelRateLimitCooldown(32, "model-other", 30)
	assert.Contains(t, channelRateLimitCooldownChannelIds("model-wild-v2", common.GetTimestamp()), 32)
	clearedExact, err := ClearChannelRateLimitCooldownRoute(
		context.Background(), 32, "model-other",
	)
	require.NoError(t, err)
	assert.True(t, clearedExact.Changed)
	assert.Contains(t, channelRateLimitCooldownChannelIds("model-wild-v2", common.GetTimestamp()), 32)

	cleared, err := ClearChannelRateLimitCooldownRoute(
		context.Background(), 32, "model-wild-*",
	)
	require.NoError(t, err)
	assert.True(t, cleared.Changed)
	assert.Empty(t, channelRateLimitCooldownChannelIds("model-wild-v2", common.GetTimestamp()))
}

func TestPruneExpiredChannelRateLimitCooldownsPublishesBoundedSnapshot(t *testing.T) {
	stopChannelRateLimitCooldownRedisSync()
	setChannelRateLimitCooldownControlRevision(t, "revision-prune")
	expiredEvents := make([]channelRateLimitCooldownKey, 0)
	RegisterChannelRateLimitCooldownExpiredHandler(func(channelId int, modelName string) {
		expiredEvents = append(expiredEvents, channelRateLimitCooldownKey{
			channelId: channelId,
			modelName: modelName,
		})
	})
	t.Cleanup(func() {
		RegisterChannelRateLimitCooldownExpiredHandler(nil)
		stopChannelRateLimitCooldownRedisSync()
		resetChannelRateLimitCooldownLocalState()
	})

	now := common.GetTimestamp()
	expiredKey := channelRateLimitCooldownKey{channelId: 22, modelName: "expired"}
	activeKey := channelRateLimitCooldownKey{channelId: 23, modelName: "active"}
	staleKey := channelRateLimitCooldownKey{channelId: 24, modelName: "stale"}
	channelRateLimitCooldowns.Lock()
	channelRateLimitCooldowns.untilByRoute = map[channelRateLimitCooldownKey]channelRateLimitCooldownEntry{
		expiredKey: {until: now, revision: "revision-prune"},
		activeKey:  {until: now + 60, revision: "revision-prune"},
		staleKey:   {until: now + 60, revision: "revision-old"},
	}
	publishChannelRateLimitCooldownSnapshotLocked()
	channelRateLimitCooldowns.Unlock()

	pruneExpiredChannelRateLimitCooldowns()

	snapshot := loadChannelRateLimitCooldownSnapshot()
	assert.NotContains(t, snapshot.untilByRoute, expiredKey)
	assert.NotContains(t, snapshot.untilByRoute, staleKey)
	assert.Contains(t, snapshot.untilByRoute, activeKey)
	assert.Equal(t, []channelRateLimitCooldownKey{expiredKey}, expiredEvents)
}

func TestChannelRateLimitCooldownRejectsRedisSnapshotOlderThanLocalWrite(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-generation")
	resetChannelRateLimitCooldownLocalState()
	require.NoError(t, common.RDB.Set(
		context.Background(),
		channelRateLimitCooldownRedisRevisionKey,
		"revision-generation",
		0,
	).Err())

	hook := &blockingCooldownPipelineHook{
		after:   make(chan struct{}),
		release: make(chan struct{}),
	}
	common.RDB.AddHook(hook)
	channelRateLimitCooldownRedisSync.client.Store(common.RDB)
	channelRateLimitCooldownRedisSync.running.Store(true)
	t.Cleanup(func() {
		channelRateLimitCooldownRedisSync.running.Store(false)
		channelRateLimitCooldownRedisSync.client.Store(nil)
	})

	done := make(chan struct{})
	go func() {
		syncChannelRateLimitCooldownsFromRedis(context.Background(), common.RDB)
		close(done)
	}()
	<-hook.after
	StartChannelRateLimitCooldown(25, "model-a", 60)
	close(hook.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Redis 429 冷却同步未结束")
	}

	assert.Positive(t, ChannelRateLimitCooldownUntil(25, "model-a"))
}

func TestChannelRateLimitCooldownUsesTheSharedMatchingModelName(t *testing.T) {
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)

	StartChannelRateLimitCooldown(31, "gemini-2.5-pro-thinking-128", 30)

	assert.Positive(t, ChannelRateLimitCooldownUntil(31, "gemini-2.5-pro-thinking-512"))
	options := applyChannelRateLimitCooldowns(
		"gemini-2.5-pro-thinking-1024",
		model.ChannelSelectionOptions{},
	)
	assert.Equal(t, []int{31}, options.ExcludedChannelIds)
}

func TestChannelRateLimitCooldownIsSharedAcrossInstances(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)

	StartChannelRateLimitCooldown(41, "model-a", 30)
	resetChannelRateLimitCooldownLocalState()

	require.Eventually(t, func() bool {
		return ChannelRateLimitCooldownUntil(41, "model-a") > 0
	}, 2*time.Second, 10*time.Millisecond)
	options := applyChannelRateLimitCooldowns("model-a", model.ChannelSelectionOptions{})
	assert.Equal(t, []int{41}, options.ExcludedChannelIds)

	require.NoError(t, common.RDB.Del(context.Background(), channelRateLimitCooldownRedisKey).Err())
	require.Eventually(t, func() bool {
		return ChannelRateLimitCooldownUntil(41, "model-a") == 0
	}, 2*time.Second, 10*time.Millisecond)
	assert.Empty(t, applyChannelRateLimitCooldowns("model-a", model.ChannelSelectionOptions{}).ExcludedChannelIds)
}

func TestChannelRateLimitCooldownCannotBeShortenedInRedis(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)

	StartChannelRateLimitCooldown(42, "model-a", 60)
	firstUntil, err := common.RDB.ZScore(
		context.Background(),
		channelRateLimitCooldownRedisKey,
		channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{channelId: 42, modelName: "model-a"}),
	).Result()
	require.NoError(t, err)
	resetChannelRateLimitCooldownLocalState()
	StartChannelRateLimitCooldown(42, "model-a", 10)
	secondUntil, err := common.RDB.ZScore(
		context.Background(),
		channelRateLimitCooldownRedisKey,
		channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{channelId: 42, modelName: "model-a"}),
	).Result()
	require.NoError(t, err)
	assert.Equal(t, firstUntil, secondUntil)
}

func TestChannelRateLimitCooldownRevisionChangeAlwaysClearsSharedEntries(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)

	setChannelRateLimitCooldownControlRevision(t, "revision-a")
	updated, err := UpdateChannelRateLimitCooldownControlRevision("revision-a", "")
	require.NoError(t, err)
	assert.True(t, updated)
	StartChannelRateLimitCooldown(43, "model-a", 60)
	assert.Positive(t, ChannelRateLimitCooldownUntil(43, "model-a"))

	setChannelRateLimitCooldownControlRevision(t, "revision-b")
	updated, err = UpdateChannelRateLimitCooldownControlRevision("revision-b", "revision-a")
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Zero(t, ChannelRateLimitCooldownUntil(43, "model-a"))
	revision, err := common.RDB.Get(
		context.Background(), channelRateLimitCooldownRedisRevisionKey,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, "revision-b", revision)
	count, err := common.RDB.ZCard(context.Background(), channelRateLimitCooldownRedisKey).Result()
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestChannelRateLimitCooldownMissingRedisRevisionClearsUnknownEntries(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)

	StartChannelRateLimitCooldown(48, "model-a", 60)
	require.NoError(t, common.RDB.Del(
		context.Background(), channelRateLimitCooldownRedisRevisionKey,
	).Err())
	setChannelRateLimitCooldownControlRevision(t, "revision-current")

	updated, err := UpdateChannelRateLimitCooldownControlRevision(
		"revision-current", "revision-previous",
	)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Zero(t, ChannelRateLimitCooldownUntil(48, "model-a"))
	revision, err := common.RDB.Get(
		context.Background(), channelRateLimitCooldownRedisRevisionKey,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, "revision-current", revision)
}

func TestChannelRateLimitCooldownRejectsStaleRevisionUpdate(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)

	setChannelRateLimitCooldownControlRevision(t, "revision-a")
	updated, err := UpdateChannelRateLimitCooldownControlRevision("revision-a", "")
	require.NoError(t, err)
	assert.True(t, updated)
	StartChannelRateLimitCooldown(44, "model-a", 60)
	setChannelRateLimitCooldownControlRevision(t, "revision-b")
	updated, err = UpdateChannelRateLimitCooldownControlRevision("revision-b", "revision-a")
	require.NoError(t, err)
	assert.True(t, updated)

	updated, err = UpdateChannelRateLimitCooldownControlRevision("revision-stale", "revision-a")
	require.NoError(t, err)
	assert.False(t, updated)
	revision, err := common.RDB.Get(
		context.Background(), channelRateLimitCooldownRedisRevisionKey,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, "revision-b", revision)
	assert.Zero(t, ChannelRateLimitCooldownUntil(44, "model-a"))
}

func TestChannelRateLimitCooldownConcurrentRevisionRecoveryKeepsNewestRevisionEmpty(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-0")
	updated, err := UpdateChannelRateLimitCooldownControlRevision("revision-0", "")
	require.NoError(t, err)
	assert.True(t, updated)
	StartChannelRateLimitCooldown(45, "model-a", 60)

	setChannelRateLimitCooldownControlRevision(t, "revision-1")
	updated, err = UpdateChannelRateLimitCooldownControlRevision("revision-1", "revision-0")
	require.NoError(t, err)
	assert.True(t, updated)
	setChannelRateLimitCooldownControlRevision(t, "revision-2")
	updated, err = UpdateChannelRateLimitCooldownControlRevision(
		"revision-2", "revision-0",
	)
	require.NoError(t, err)
	assert.True(t, updated)
	revision, err := common.RDB.Get(
		context.Background(), channelRateLimitCooldownRedisRevisionKey,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, "revision-2", revision)
	assert.Zero(t, ChannelRateLimitCooldownUntil(45, "model-a"))
}

func TestChannelRateLimitCooldownDoesNotRestoreStaleDatabaseRevision(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-current")
	require.NoError(t, common.RDB.Set(
		context.Background(), channelRateLimitCooldownRedisRevisionKey, "revision-current", 0,
	).Err())

	updated, err := UpdateChannelRateLimitCooldownControlRevision(
		"revision-stale", "revision-previous",
	)
	require.NoError(t, err)
	assert.False(t, updated)
	revision, err := common.RDB.Get(
		context.Background(), channelRateLimitCooldownRedisRevisionKey,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, "revision-current", revision)
}

func TestChannelRateLimitCooldownRepairsMismatchedRedisRevision(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-current")
	require.NoError(t, common.RDB.Set(
		context.Background(), channelRateLimitCooldownRedisRevisionKey, "revision-restored", 0,
	).Err())
	StartChannelRateLimitCooldown(46, "stale-model", 60)

	assert.True(t, StartChannelRateLimitCooldownIfControlRevision(
		47, "model-a", 30, "revision-current",
	))
	assert.Positive(t, ChannelRateLimitCooldownUntil(47, "model-a"))
	resetChannelRateLimitCooldownLocalState()
	assert.Zero(t, ChannelRateLimitCooldownUntil(46, "stale-model"))
	require.Eventually(t, func() bool {
		return ChannelRateLimitCooldownUntil(47, "model-a") > 0
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []int{47}, applyChannelRateLimitCooldowns(
		"model-a", model.ChannelSelectionOptions{},
	).ExcludedChannelIds)
}

func TestChannelRateLimitCooldownStrictRedisSequenceIsIdempotent(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-sequence")
	updated, err := UpdateChannelRateLimitCooldownControlRevision("revision-sequence", "")
	require.NoError(t, err)
	assert.True(t, updated)

	now := common.GetTimestamp()
	member := channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
		channelId: 49,
		modelName: "model-a",
	})
	firstSequence := int64(115_000_000_000_000_001)
	accepted, err := StartChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(), 49, "model-a", now+60, "revision-sequence", firstSequence,
	)
	require.NoError(t, err)
	assert.True(t, accepted)
	firstUntil := ChannelRateLimitCooldownUntil(49, "model-a")
	assert.Equal(t, now+60, firstUntil)

	accepted, err = StartChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(), 49, "model-a", now+600, "revision-sequence", firstSequence,
	)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.Equal(t, firstUntil, ChannelRateLimitCooldownUntil(49, "model-a"))

	accepted, err = StartChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(), 49, "model-a", now+900, "revision-sequence", firstSequence-1,
	)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.Equal(t, firstUntil, ChannelRateLimitCooldownUntil(49, "model-a"))

	accepted, err = StartChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(), 49, "model-a", now+120, "revision-sequence", firstSequence+1,
	)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.Equal(t, now+120, ChannelRateLimitCooldownUntil(49, "model-a"))
	sequence, err := common.RDB.HGet(
		context.Background(), channelRateLimitCooldownRedisEventSequenceKey, member,
	).Result()
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%019d", firstSequence+1), sequence)
}

func TestChannelRateLimitCooldownStrictRedisFailureIsReturnedAndRolledBack(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-write-failure")
	updated, err := UpdateChannelRateLimitCooldownControlRevision("revision-write-failure", "")
	require.NoError(t, err)
	assert.True(t, updated)
	writeErr := errors.New("redis cooldown write failed")
	hook := &channelRateLimitCooldownEvalErrorHook{
		script: channelRateLimitCooldownRedisGuardedExtendScript,
		err:    writeErr,
	}
	common.RDB.AddHook(hook)

	accepted, err := StartChannelRateLimitCooldownUntilIfControlRevision(
		context.Background(),
		50,
		"model-a",
		common.GetTimestamp()+60,
		"revision-write-failure",
		115_000_000_000_000_010,
	)
	assert.ErrorIs(t, err, writeErr)
	assert.False(t, accepted)
	assert.Equal(t, int64(1), hook.calls.Load())
	assert.Zero(t, ChannelRateLimitCooldownUntil(50, "model-a"))
}

func TestChannelRateLimitCooldownUntilMatchingCoversWildcardRoute(t *testing.T) {
	ClearChannelRateLimitCooldowns()
	t.Cleanup(ClearChannelRateLimitCooldowns)
	StartChannelRateLimitCooldown(51, "gpt-4o-mini", 60)

	assert.Positive(t, ChannelRateLimitCooldownUntilMatching(51, "gpt-4o*"))
	assert.Positive(t, ChannelRateLimitCooldownUntilMatching(51, "gpt-4o-mini"))
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(51, "claude*"))
	assert.Zero(t, ChannelRateLimitCooldownUntilMatching(52, "gpt-4o*"))
}

func TestChannelRateLimitCooldownStrictRedisReaderIgnoresLocalSnapshot(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-strict-read")
	updated, err := UpdateChannelRateLimitCooldownControlRevision("revision-strict-read", "")
	require.NoError(t, err)
	assert.True(t, updated)

	StartChannelRateLimitCooldown(52, "gpt-4o-mini", 60)
	localUntil := ChannelRateLimitCooldownUntilMatching(52, "gpt-4o*")
	require.Positive(t, localUntil)
	strictExactUntil, err := ChannelRateLimitCooldownUntilMatchingFromRedis(
		context.Background(), 52, "gpt-4o-mini",
	)
	require.NoError(t, err)
	assert.Equal(t, localUntil, strictExactUntil)
	strictWildcardUntil, err := ChannelRateLimitCooldownUntilMatchingFromRedis(
		context.Background(), 52, "gpt-4o*",
	)
	require.NoError(t, err)
	assert.Equal(t, localUntil, strictWildcardUntil)
	require.NoError(t, common.RDB.ZRem(
		context.Background(),
		channelRateLimitCooldownRedisKey,
		channelRateLimitCooldownRedisMember(channelRateLimitCooldownKey{
			channelId: 52,
			modelName: "gpt-4o-mini",
		}),
	).Err())

	strictUntil, err := ChannelRateLimitCooldownUntilMatchingFromRedis(
		context.Background(), 52, "gpt-4o*",
	)
	require.NoError(t, err)
	assert.Zero(t, strictUntil)
	assert.Equal(t, localUntil, ChannelRateLimitCooldownUntilMatching(52, "gpt-4o*"))
}

func TestChannelRateLimitCooldownStrictRedisReaderReturnsReadErrors(t *testing.T) {
	useChannelRateLimitCooldownRedis(t)
	setChannelRateLimitCooldownControlRevision(t, "revision-strict-error")
	updated, err := UpdateChannelRateLimitCooldownControlRevision("revision-strict-error", "")
	require.NoError(t, err)
	assert.True(t, updated)

	readErr := errors.New("redis cooldown read failed")
	common.RDB.AddHook(&channelRateLimitCooldownPipelineErrorHook{err: readErr})
	_, err = ChannelRateLimitCooldownUntilMatchingFromRedis(
		context.Background(), 53, "model-a",
	)
	assert.ErrorIs(t, err, readErr)
}

func TestChannelRateLimitCooldownStrictRedisReaderRequiresRedis(t *testing.T) {
	stopChannelRateLimitCooldownRedisSync()
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	ClearChannelRateLimitCooldowns()
	StartChannelRateLimitCooldown(54, "model-a", 60)
	t.Cleanup(func() {
		stopChannelRateLimitCooldownRedisSync()
		ClearChannelRateLimitCooldowns()
		common.RedisEnabled = originalEnabled
		common.RDB = originalClient
	})

	assert.Positive(t, ChannelRateLimitCooldownUntilMatching(54, "model-a"))
	_, err := ChannelRateLimitCooldownUntilMatchingFromRedis(
		context.Background(), 54, "model-a",
	)
	assert.ErrorIs(t, err, ErrChannelRateLimitCooldownRedisUnavailable)
}
