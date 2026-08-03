package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useChannelRateLimitCooldownRedis(t *testing.T) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	originalEnabled := common.RedisEnabled
	originalClient := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	ClearChannelRateLimitCooldowns()
	t.Cleanup(func() {
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
	assert.Zero(t, ChannelRateLimitCooldownUntil(21, "model-a"))
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

	assert.Positive(t, ChannelRateLimitCooldownUntil(41, "model-a"))
	options := applyChannelRateLimitCooldowns("model-a", model.ChannelSelectionOptions{})
	assert.Equal(t, []int{41}, options.ExcludedChannelIds)

	require.NoError(t, common.RDB.Del(context.Background(), channelRateLimitCooldownRedisKey).Err())
	assert.Zero(t, ChannelRateLimitCooldownUntil(41, "model-a"))
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
	assert.Positive(t, ChannelRateLimitCooldownUntil(47, "model-a"))
	assert.Equal(t, []int{47}, applyChannelRateLimitCooldowns(
		"model-a", model.ChannelSelectionOptions{},
	).ExcludedChannelIds)
}
