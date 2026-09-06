package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRebuildChannelMonitorRedisDailySuccessFromDailyLedger(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-rebuild.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&model.Token{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
		&model.ChannelMonitorDailySuccessLedger{},
	))
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		require.NoError(t, sqlDB.Close())
	})

	dayStart := int64(1_750_003_200)
	require.NoError(t, db.Create(&model.ChannelMonitorDailySuccessLedger{
		DayStart:        dayStart,
		ChannelId:       7,
		UserId:          31,
		UserAttribution: string(model.ChannelMonitorEventUserAttributionInferred),
		APIKeyId:        201,
		APIKeyKey:       "api-key-key",
		APIKeyName:      "production",
		ModelKey:        "model-key",
		ModelName:       "gpt-test",
		GroupKey:        "group-key",
		GroupName:       "default",

		ActualSuccessCount: 3,
		ActualFailureCount: 1,
		FinalSuccessCount:  2,
		FinalFailureCount:  2,
		CacheHitCount:      2,
		CacheSampleCount:   4,
		CacheReadTokens:    50,
		InputTokens:        100,
		CacheWriteCount:    1,
	}).Error)
	require.NoError(t, client.HSet(
		context.Background(),
		ChannelMonitorRedisSuccessDayKey(dayStart),
		"stale:field", "stale",
	).Err())

	require.NoError(t, rebuildChannelMonitorRedisDailySuccess(
		context.Background(), client, dayStart+180,
	))

	values, err := client.HGetAll(context.Background(), ChannelMonitorRedisSuccessDayKey(dayStart)).Result()
	require.NoError(t, err)
	assert.Equal(t, "3", values[channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricActualSuccess])
	assert.Equal(t, "3", values[channelMonitorRedisSharedScopeChannel+":7:"+channelMonitorRedisSharedMetricActualSuccess])
	assert.Equal(t, "3", values["user:31:"+channelMonitorRedisSharedMetricActualSuccess])
	assert.Equal(t, "3", values[channelMonitorRedisSharedScopeAPIKey+":201:"+channelMonitorRedisSharedMetricActualSuccess])
	assert.Equal(t, "production", values[channelMonitorRedisSharedScopeAPIKey+":201:"+channelMonitorRedisSharedMetricAPIKeyName])
	assert.Equal(t, "1", values[channelMonitorRedisSharedScopeMetadata+":rebuild_version"])
	assert.NotContains(t, values, "stale:field")
	assert.Equal(t, int64(1), client.Exists(context.Background(), ChannelMonitorRedisSuccessDayKey(dayStart)).Val())
}

func TestRebuildChannelMonitorRedisDailyCostsFromDatabaseLedgers(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-cost-rebuild.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelDailyCost{},
		&model.ChannelDailyAPIKeyCost{},
		&model.ChannelMonitorDailyCostDetail{},
	))
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		require.NoError(t, sqlDB.Close())
	})

	dayStart := int64(1_750_003_200)
	require.NoError(t, db.Create(&model.ChannelDailyCost{
		ChannelId:                 7,
		DayStart:                  dayStart,
		CostNanoCNY:               900,
		ProbeCostNanoCNY:          100,
		GroupProbeCostNanoCNY:     50,
		ModelDetectionCostNanoCNY: 20,
		SettledCount:              3,
		UnresolvedCount:           1,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelDailyAPIKeyCost{
		ChannelId:       7,
		DayStart:        dayStart,
		APIKeyId:        201,
		APIKeyName:      "production",
		KeyFingerprint:  "fingerprint-201",
		KeyDisplay:      "prod",
		CostNanoCNY:     700,
		SettledCount:    2,
		UnresolvedCount: 0,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorDailyCostDetail{
		DayStart:        dayStart,
		ChannelId:       7,
		UserId:          31,
		UserAttribution: string(model.ChannelMonitorEventUserAttributionRequest),
		APIKeyId:        201,
		APIKeyKey:       "fingerprint-201",
		APIKeyName:      "production",
		ModelKey:        model.ChannelMonitorDailyCostModelKey("gpt-test"),
		ModelName:       "gpt-test",
		SourceKind:      "business",
		CostNanoCNY:     800,
		SettledCount:    2,
		UnresolvedCount: 0,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelMonitorDailyCostDetail{
		DayStart:     dayStart,
		ChannelId:    7,
		APIKeyId:     0,
		ModelKey:     model.ChannelMonitorDailyCostModelKey("detector"),
		ModelName:    "detector",
		SourceKind:   string(model.ChannelMonitorEventSourceModelDetection),
		CostNanoCNY:  20,
		SettledCount: 1,
	}).Error)

	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(context.Background(), client, dayStart+180))
	values, err := client.HGetAll(context.Background(), ChannelMonitorRedisCostDayKey(dayStart)).Result()
	require.NoError(t, err)
	assert.Equal(t, "900", values[channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricSettledCost])
	assert.Equal(t, "3", values[channelMonitorRedisSharedScopeChannel+":7:"+channelMonitorRedisSharedMetricSettledRequests])
	assert.Equal(t, "100", values[channelMonitorRedisSharedScopeChannel+":7:"+channelMonitorRedisSharedMetricProbeSettledCost])
	assert.Equal(t, "50", values[channelMonitorRedisSharedScopeChannel+":7:"+channelMonitorRedisSharedMetricGroupProbeSettledCost])
	assert.Equal(t, "20", values[channelMonitorRedisSharedScopeChannel+":7:"+channelMonitorRedisSharedMetricDetectionSettledCost])
	assert.Equal(t, "800", values[channelMonitorRedisSharedScopeModel+":"+channelMonitorRedisSharedDimension("gpt-test")+":"+channelMonitorRedisSharedMetricSettledCost])
	assert.Equal(t, "20", values[channelMonitorRedisSharedScopeModel+":"+channelMonitorRedisSharedDimension("detector")+":"+channelMonitorRedisSharedMetricDetectionSettledCost])
	assert.Equal(t, "production", values[channelMonitorRedisSharedScopeAPIKey+":201:"+channelMonitorRedisSharedMetricAPIKeyName])
	assert.Equal(t, "800", values[channelMonitorRedisSharedScopeAPIKey+":201:"+channelMonitorRedisSharedMetricSettledCost])
}

func TestDatabaseRebuiltDailySnapshotsSuppressOlderStreamDeltas(t *testing.T) {
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	occurredAt := int64(1_750_000_000)
	dayStart := model.ChannelDailyCostDayStart(occurredAt)
	ctx := context.Background()

	require.NoError(t, client.HSet(ctx,
		ChannelMonitorRedisSuccessDayKey(dayStart),
		channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricDatabaseThrough, occurredAt+60,
		channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricActualSuccess, 100,
	).Err())
	successEvent := newChannelMonitorRedisSharedProjectionTestEvent("rebuilt-success", occurredAt)
	require.NoError(t, projection.HandleChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{successEvent}))
	value, err := client.HGet(ctx, ChannelMonitorRedisSuccessDayKey(dayStart), channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricActualSuccess).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(100), value)
	value, err = client.HGet(ctx, ChannelMonitorRedisDashboardMinuteKey(occurredAt-occurredAt%60), channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricActualSuccess).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), value)

	require.NoError(t, client.HSet(ctx,
		ChannelMonitorRedisCostDayKey(dayStart),
		channelMonitorRedisSharedScopeMetadata+":"+channelMonitorRedisSharedMetricDatabaseSnapshotAt, occurredAt,
		channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricSettledCost, 200,
	).Err())
	costEvent := newChannelMonitorRedisSharedProjectionTestEvent("rebuilt-cost", occurredAt)
	costEvent.OtherJson = `{"cost_event_id":"rebuilt-cost"}`
	costEvent.CostStatus = model.ChannelMonitorEventCostSettled
	costEvent.SettledCostNanoCNY = 10
	require.NoError(t, projection.HandleChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{costEvent}))
	value, err = client.HGet(ctx, ChannelMonitorRedisCostDayKey(dayStart), channelMonitorRedisSharedScopeGlobal+":"+channelMonitorRedisSharedMetricSettledCost).Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(200), value)
}
