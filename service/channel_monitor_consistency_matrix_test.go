package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorConsistencyDatabaseMatrix(t *testing.T) {
	engine := os.Getenv("CHANNEL_MONITOR_CONSISTENCY_DIALECT")
	if engine == "" {
		t.Skip("设置 CHANNEL_MONITOR_CONSISTENCY_DIALECT 运行真实数据库与 Redis 验证")
	}
	dsn := os.Getenv("SQL_DSN")
	path := os.Getenv("CM_UPGRADE_SQLITE_PATH")
	require.True(t, strings.Contains(dsn, "new_api_cm_schema_") || strings.Contains(path, "cm-consistency-verification"))
	require.Equal(t, "127.0.0.1:26380", os.Getenv("CHANNEL_MONITOR_CONSISTENCY_REDIS"))
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis, oldRedisEnabled, oldMaster := common.RDB, common.RedisEnabled, common.IsMasterNode
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldSQLite := common.SQLitePath
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.IsMasterNode = true
	common.RedisEnabled = false
	if path != "" {
		common.SQLitePath = filepath.Clean(path)
	}
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	db := model.DB
	sqlDB, err := db.DB()
	require.NoError(t, err)
	logSQL, err := model.LOG_DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, sqlDB.Close())
		if logSQL != sqlDB {
			assert.NoError(t, logSQL.Close())
		}
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RDB, common.RedisEnabled, common.IsMasterNode = oldRedis, oldRedisEnabled, oldMaster
		common.SQLitePath = oldSQLite
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})
	baseline := os.Getenv("CHANNEL_MONITOR_CONSISTENCY_BASELINE")
	if baseline == "fresh" {
		require.NoError(t, db.Create(&model.Channel{Id: 900001, Name: "released-fixture-channel", Key: "test-fixture", Type: 1}).Error)
		require.NoError(t, db.Create(&model.User{Id: 900002, Username: "cm_schema_user", Password: "test-fixture", Quota: 1234567, Group: "default"}).Error)
	}
	if baseline == "downstream" {
		var previous model.ChannelDailyCostOutbox
		require.NoError(t, db.First(&previous, "event_id = ?", "migration-old-outbox").Error)
		assert.Equal(t, int64(123), previous.CostNanoCNY)
		assert.Zero(t, previous.RedisProjectedAt)
		assert.Empty(t, previous.ProjectionEventId)
		var task model.ChannelTaskCostEvent
		require.NoError(t, db.First(&task, "cost_event_id = ?", "migration-old-task").Error)
		assert.Zero(t, task.ProjectionVersion)
		var statistics model.ChannelMonitorDailySuccessLedger
		require.NoError(t, db.First(&statistics, "channel_id = ?", 900101).Error)
		assert.Equal(t, int64(5), statistics.ActualSuccessCount)
		assert.Empty(t, statistics.AggregateJSON)
		assert.Zero(t, statistics.ProjectionRevision)
	}
	if model.LOG_DB != db {
		assert.False(t, model.LOG_DB.Migrator().HasTable(&model.ChannelMonitorDailyCheckpoint{}), "日统计只持久化到主库")
		assert.True(t, model.LOG_DB.Migrator().HasTable(&model.Log{}))
	}
	var original model.Channel
	require.NoError(t, db.First(&original, "id = ?", 900001).Error)
	assert.Equal(t, "released-fixture-channel", original.Name)
	var user model.User
	require.NoError(t, db.First(&user, "id = ?", 900002).Error)
	assert.EqualValues(t, 1234567, user.Quota)
	redisDB := 0
	if engine == "mysql" {
		redisDB = 1
	}
	if engine == "postgres" {
		redisDB = 2
	}
	client := redis.NewClient(&redis.Options{Addr: os.Getenv("CHANNEL_MONITOR_CONSISTENCY_REDIS"), DB: redisDB})
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	ctx := context.Background()
	require.NoError(t, client.FlushDB(ctx).Err())
	common.RDB, common.RedisEnabled = client, true
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 900001).Updates(map[string]any{"group": "default", "models": "model-a"}).Error)
	require.NoError(t, db.Where(&model.Ability{ChannelId: 900001, Group: "default", Model: "model-a"}).FirstOrCreate(&model.Ability{ChannelId: 900001, Group: "default", Model: "model-a", Enabled: true, Weight: 1}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	_, err = model.GetChannelSmartScheduleRoutesWithContext(ctx)
	require.NoError(t, err)
	_, err = model.GetChannelSmartScheduleEconomicSnapshotWithContext(ctx)
	require.NoError(t, err)
	common.MemoryCacheEnabled = false
	const at = int64(1_760_000_000)
	day := model.ChannelDailyCostDayStart(at)
	// Clear only verification rows in the dedicated database, preserving the
	// records produced by the actual released-version startup.
	for _, table := range []any{&model.ChannelDailyCostOutbox{}, &model.ChannelTaskCostEvent{}, &model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}, &model.ChannelMonitorDailyCostDetail{}} {
		require.NoError(t, db.Where("channel_id = ?", 7).Delete(table).Error)
	}
	require.NoError(t, db.Where("day_start = ?", day).Delete(&model.ChannelMonitorDailySuccessLedger{}).Error)
	require.NoError(t, db.Where("day_start = ?", day).Delete(&model.ChannelMonitorDailyCheckpoint{}).Error)
	fingerprint, display := model.ChannelDailyCostAPIKeyIdentityForToken(11, "test-only-key")
	delta := model.ChannelDailyCostDelta{ChannelId: 7, OccurredAt: at, CostNanoCNY: 200, SettledDelta: 1,
		APIKeyId: 11, APIKeyName: "matrix-key", KeyFingerprint: fingerprint, KeyDisplay: display,
		UserId: 9, UserAttribution: "request", ModelName: "model-a", SourceKind: "business"}
	require.NoError(t, model.AddChannelDailyCostBatch(ctx, []model.ChannelDailyCostDelta{delta}))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	delta.EventId = "matrix-pending-" + engine
	delta.OccurredAt = at + 1
	delta.CostNanoCNY = 100
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{delta}))
	var outbox []model.ChannelDailyCostOutbox
	require.NoError(t, db.Where("channel_id = ?", 7).Order("id ASC").Find(&outbox).Error)
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, outbox))
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, outbox))
	view, err := QueryChannelMonitorRedisDailyCosts(ctx, day)
	require.NoError(t, err)
	assert.Equal(t, int64(300), view.Global.SettledCostNanoCNY)
	require.Len(t, view.KeyCosts, 1)
	assert.Equal(t, int64(300), view.KeyCosts[0].CostNanoCNY)
	assert.Equal(t, int64(200), matrixChannelDailyCost(t, db, day))
	require.NoError(t, recoverChannelDailyCostOutboxBatch(ctx, "matrix-"+engine))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at+60))
	assert.Equal(t, int64(300), matrixChannelDailyCost(t, db, day))
	_, err = model.RegisterChannelTaskCostEvent(ctx, model.ChannelTaskCostEventInput{
		CostEventId: "task:matrix-" + engine, ChannelId: 7, OccurredAt: at, InitialQuota: 6, CostNanoCNY: 600,
		APIKeyId: 11, APIKeyName: "matrix-key", KeyFingerprint: fingerprint, KeyDisplay: display, ModelName: "model-a",
	})
	require.NoError(t, err)
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	_, err = model.SetChannelTaskCostEventCost(ctx, "task:matrix-"+engine, 200, at+86400)
	require.NoError(t, err)
	outbox = nil
	require.NoError(t, db.Where("channel_id = ?", 7).Order("id DESC").Find(&outbox).Error)
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, outbox))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	view, err = QueryChannelMonitorRedisDailyCosts(ctx, day)
	require.NoError(t, err)
	assert.Equal(t, int64(500), view.Global.SettledCostNanoCNY)
	assert.Equal(t, int64(3), view.Global.SettledRequestCount)

	event := newChannelMonitorRedisSharedProjectionTestEvent("matrix-stat-first-"+engine, at)
	event.EventSequence = 1001
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{event}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	var daily []model.ChannelMonitorDailySuccessLedger
	require.NoError(t, db.Where("day_start = ?", day).Find(&daily).Error)
	require.Len(t, daily, 1)
	id := daily[0].Id
	event.EventId = "matrix-stat-next-" + engine
	event.EventSequence = 1002
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{event}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	daily = nil
	require.NoError(t, db.Where("day_start = ?", day).Find(&daily).Error)
	require.Len(t, daily, 1)
	assert.Equal(t, id, daily[0].Id)
	assert.Equal(t, int64(2), daily[0].ActualSuccessCount)
	stale := model.ChannelMonitorDailyCheckpoint{DayStart: day, Revision: 1, EventWatermark: 1}
	require.NoError(t, model.PersistChannelMonitorDailySnapshot(ctx, stale, nil))
	assert.Equal(t, int64(500), matrixChannelDailyCost(t, db, day))
	var count int64
	require.NoError(t, db.Model(&model.ChannelMonitorDailySuccessLedger{}).Where("day_start = ?", day).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	now := common.GetTimestamp()
	require.NoError(t, rebuildChannelMonitorReliableDailyCosts(ctx, client, model.ChannelDailyCostDayStart(now)))
	detection, err := GetChannelModelDetectionOverview(ctx, db, now)
	require.NoError(t, err)
	assert.Equal(t, "redis_daily", detection.CostSource)
	// A second complete startup must keep the new data, existing identities
	// and all uniqueness guarantees after upgrading the released database.
	common.RedisEnabled = false
	require.NoError(t, sqlDB.Close())
	if logSQL != sqlDB {
		require.NoError(t, logSQL.Close())
	}
	require.NoError(t, model.InitDB())
	require.NoError(t, model.InitLogDB())
	sqlDB, err = model.DB.DB()
	require.NoError(t, err)
	logSQL, err = model.LOG_DB.DB()
	require.NoError(t, err)
	require.NoError(t, model.DB.First(&original, "id = ?", 900001).Error)
	assert.Equal(t, "released-fixture-channel", original.Name)
	var duplicate model.ChannelMonitorDailySuccessLedger
	require.NoError(t, model.DB.First(&duplicate, "id = ?", id).Error)
	duplicate.Id = 0
	assert.Error(t, model.DB.Create(&duplicate).Error, "日维度唯一约束必须保留")
	var version string
	if engine == "sqlite" {
		require.NoError(t, model.DB.Raw("SELECT sqlite_version()").Scan(&version).Error)
	} else {
		require.NoError(t, model.DB.Raw("SELECT VERSION()").Scan(&version).Error)
	}
	t.Logf("engine=%s version=%s baseline=%s separate_log=%t verified_at=%s", engine, version, baseline, model.DB != model.LOG_DB, time.Now().UTC().Format(time.RFC3339))
}

func matrixChannelDailyCost(t *testing.T, db *gorm.DB, day int64) int64 {
	t.Helper()
	var row model.ChannelDailyCost
	require.NoError(t, db.First(&row, "channel_id = ? AND day_start = ?", 7, day).Error)
	return row.CostNanoCNY
}
