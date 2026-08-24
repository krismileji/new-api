package controller

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelStatusProbeOverviewSnapshotMetadataAndStaleWhileRevalidate(t *testing.T) {
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS", "10")
	t.Setenv("CHANNEL_STATUS_PROBE_OVERVIEW_STALE_TTL_MS", "5000")
	channel := setupChannelStatusProbeControllerTest(t)

	initial := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	assert.Equal(t, channelStatusProbeOverviewSnapshotSchemaVersion, initial.SnapshotVersion)
	assert.Positive(t, initial.SnapshotRevision)
	assert.Positive(t, initial.EventWatermark)
	assert.Positive(t, initial.GeneratedAt)
	assert.False(t, initial.Stale)

	modelsJSON, err := common.Marshal([]string{"model-a"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.ChannelStatusProbeConfig{
		ChannelId: channel.Id, Enabled: true, ModelsJSON: string(modelsJSON),
		IntervalSeconds: 300, DisplayValue: 60, DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
		Revision: 1,
	}).Error)
	var stale channelStatusProbeOverviewResponse
	require.Eventually(t, func() bool {
		stale = getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
		return stale.Stale
	}, time.Second, 10*time.Millisecond)
	assert.True(t, stale.Stale)
	assert.Equal(t, initial.SnapshotVersion, stale.SnapshotVersion)
	assert.Equal(t, initial.SnapshotRevision, stale.SnapshotRevision)
	assert.Equal(t, initial.EventWatermark, stale.EventWatermark)
	assert.Equal(t, initial.GeneratedAt, stale.GeneratedAt)

	var refreshed channelStatusProbeOverviewResponse
	require.Eventually(t, func() bool {
		refreshed = getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
		return !refreshed.Stale && len(refreshed.Channels) == 1 && refreshed.Channels[0].Config != nil
	}, time.Second, 10*time.Millisecond)
	assert.GreaterOrEqual(t, refreshed.GeneratedAt, initial.GeneratedAt)
	assert.Greater(t, refreshed.SnapshotRevision, initial.SnapshotRevision)
	assert.GreaterOrEqual(t, refreshed.EventWatermark, initial.EventWatermark)
}

func TestChannelStatusProbeOverviewSnapshotMetadataUsesResponseTime(t *testing.T) {
	generated := time.Now().Add(-2 * time.Second)
	snapshot := channelStatusProbeOverviewRedisSnapshot{
		SchemaVersion: channelStatusProbeOverviewSnapshotSchemaVersion,
		Revision:      1, EventWatermark: 1,
		GeneratedAt: generated.Unix(), GeneratedAtUnixMillis: generated.UnixMilli(),
		Response: channelStatusProbeOverviewResponse{ServerNow: 1},
	}

	response := channelStatusProbeOverviewResponseWithMetadata(snapshot, true)
	assert.GreaterOrEqual(t, response.ServerNow, generated.Unix())
	assert.GreaterOrEqual(t, response.SnapshotAgeSeconds, int64(2))
	assert.True(t, response.Stale)
}

func TestChannelStatusProbeOverviewUsesRedisSnapshotAfterLocalCacheLoss(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	invalidateChannelStatusProbeOverviewCache()
	require.NoError(t, model.DB.AutoMigrate(&model.ChannelStatusProbeConfig{}, &model.ChannelStatusProbeState{}))
	channel := &model.Channel{Id: 8811, Name: "Redis 快照测试渠道", Status: common.ChannelStatusEnabled, Models: "model-a", Group: "default"}
	require.NoError(t, model.DB.Create(channel).Error)

	var queryCount atomic.Int64
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(
		"test:channel_status_probe_overview_redis_snapshot",
		func(*gorm.DB) { queryCount.Add(1) },
	))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("test:channel_status_probe_overview_redis_snapshot") })
	first := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	firstQueryCount := queryCount.Load()
	require.Positive(t, firstQueryCount)

	channelStatusProbeOverviewCache.Lock()
	channelStatusProbeOverviewCache.items = make(map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry)
	channelStatusProbeOverviewCacheGeneration.Add(1)
	channelStatusProbeOverviewCache.Unlock()

	second := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	assert.Equal(t, first.GeneratedAt, second.GeneratedAt)
	assert.Equal(t, first.SnapshotVersion, second.SnapshotVersion)
	assert.Equal(t, first.SnapshotRevision, second.SnapshotRevision)
	assert.Equal(t, first.EventWatermark, second.EventWatermark)
	assert.False(t, second.Stale)
	assert.Equal(t, firstQueryCount, queryCount.Load())
}

func TestChannelStatusProbeOverviewUsesLocalSnapshotWhenRedisIsDisabled(t *testing.T) {
	setupChannelStatusProbeControllerTest(t)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })

	var queryCount atomic.Int64
	callbackName := "test:channel_status_probe_overview_redis_disabled"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(
		callbackName,
		func(*gorm.DB) { queryCount.Add(1) },
	))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove(callbackName) })

	first := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	firstQueryCount := queryCount.Load()
	require.Positive(t, firstQueryCount)
	second := getChannelStatusProbeOverviewResponse(t, "/api/channel_monitor/status")
	assert.Equal(t, first.SnapshotRevision, second.SnapshotRevision)
	assert.Equal(t, first.EventWatermark, second.EventWatermark)
	assert.Equal(t, firstQueryCount, queryCount.Load())
}
