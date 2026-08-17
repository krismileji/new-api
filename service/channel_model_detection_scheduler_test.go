package service

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func setupChannelModelDetectionSchedulerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-scheduler.db")+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionBatch{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
	))
	t.Cleanup(func() {
		sqlDB, dbErr := db.DB()
		require.NoError(t, dbErr)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func seedChannelModelDetectionSchedule(t *testing.T, db *gorm.DB, channelID int, status int, preset string, scheduledFor int64) model.ChannelModelDetectionGlobalConfig {
	t.Helper()
	global := model.ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://127.0.0.1:18080", ScheduledPreset: preset, ScheduleEnabled: true,
		IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai",
		ScheduleAnchorAt: scheduledFor, NextBatchAt: scheduledFor,
	}
	require.NoError(t, db.Create(&global).Error)
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "test", Key: "secret", Status: status}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: channelID, ScheduleEnabled: true}
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: channelID, RequestModel: "channel-alias",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true,
	}).Error)
	return global
}

func TestChannelModelDetectionScheduleUsesIANAWallClockAndSkipsCatchUp(t *testing.T) {
	anchor, err := CalculateChannelModelDetectionScheduleAnchor(
		time.Date(2026, time.March, 7, 12, 0, 0, 0, time.UTC), "02:30", "America/New_York",
	)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC), anchor)

	base := time.Date(2026, time.August, 1, 2, 30, 0, 0, time.UTC)
	scheduled, next, err := NextChannelModelDetectionSchedule(base, 24, base.Add(6*24*time.Hour+time.Minute))
	require.NoError(t, err)
	assert.Equal(t, base.Add(6*24*time.Hour), scheduled)
	assert.Equal(t, base.Add(7*24*time.Hour), next)

	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	dstAnchor := time.Date(2026, time.March, 7, 2, 30, 0, 0, ny)
	due, following, err := NextChannelModelDetectionScheduleInTimezone(dstAnchor, 24, time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC), "America/New_York")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.March, 9, 6, 30, 0, 0, time.UTC), due)
	assert.Equal(t, time.Date(2026, time.March, 10, 6, 30, 0, 0, time.UTC), following)
}

func TestNextChannelModelDetectionScheduleMinutesAlignsToIntervalBoundaries(t *testing.T) {
	base := time.Date(2026, time.August, 13, 2, 30, 37, 0, time.UTC)
	scheduled, next, err := NextChannelModelDetectionScheduleMinutes(
		base, 15, time.Date(2026, time.August, 13, 3, 2, 59, 0, time.UTC),
	)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, time.August, 13, 3, 0, 0, 0, time.UTC), scheduled)
	assert.Equal(t, time.Date(2026, time.August, 13, 3, 15, 0, 0, time.UTC), next)

	_, next, err = NextChannelModelDetectionScheduleMinutes(base, 15, base.Add(5*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, base.Truncate(time.Minute).Add(15*time.Minute), next)

	scheduled, next, err = NextChannelModelDetectionScheduleMinutes(
		time.Date(2026, time.August, 13, 10, 23, 0, 0, time.UTC),
		60,
		time.Date(2026, time.August, 13, 10, 30, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	assert.True(t, scheduled.IsZero())
	assert.Equal(t, time.Date(2026, time.August, 13, 11, 0, 0, 0, time.UTC), next)

	_, _, err = NextChannelModelDetectionScheduleMinutes(base, model.ChannelModelDetectionMinIntervalMinutes-1, base)
	assert.ErrorIs(t, err, ErrChannelModelDetectionScheduleInvalid)
}

func TestChannelModelDetectionScheduleCreatesFrozenRunsAndSkipsManualDisabled(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	global := seedChannelModelDetectionSchedule(t, db, 101, common.ChannelStatusAutoDisabled, model.ChannelModelDetectionPresetMedium, now.Add(-72*time.Hour).Unix())

	require.NoError(t, db.Create(&model.Channel{Id: 102, Name: "manual-disabled", Key: "secret", Status: common.ChannelStatusManuallyDisabled}).Error)
	manualDisabledConfig := model.ChannelModelDetectionConfig{ChannelId: 102, ScheduleEnabled: true}
	require.NoError(t, db.Create(&manualDisabledConfig).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{
		ConfigId: manualDisabledConfig.Id, ChannelId: 102, RequestModel: "disabled-alias",
		ClaimedModel: model.ChannelModelDetectionClaimedModelTerra, Enabled: true,
	}).Error)

	result, err := RunChannelModelDetectionScheduleOnce(context.Background(), db, now)
	require.NoError(t, err)
	assert.True(t, result.Created)
	assert.Len(t, result.RunIDs, 1)
	assert.Equal(t, now.Unix(), result.ScheduledFor)
	assert.Equal(t, now.Add(24*time.Hour).Unix(), result.NextBatchAt)

	var run model.ChannelModelDetectionRun
	require.NoError(t, db.Where("run_id = ?", result.RunIDs[0]).First(&run).Error)
	assert.Equal(t, 101, run.ChannelId)
	assert.Equal(t, global.Revision, run.GlobalConfigRevision)
	assert.Equal(t, model.ChannelModelDetectionPresetMedium, run.Preset)
	assert.Equal(t, model.ChannelModelDetectionPresetSourceScheduledDefault, run.PresetSource)

	require.NoError(t, db.Model(&model.ChannelModelDetectionGlobalConfig{}).Where("id = ?", global.Id).
		Updates(map[string]any{"scheduled_preset": model.ChannelModelDetectionPresetHigh, "revision": global.Revision + 1}).Error)
	require.NoError(t, db.First(&run, run.Id).Error)
	assert.Equal(t, model.ChannelModelDetectionPresetMedium, run.Preset)
}

func TestChannelModelDetectionScheduleLeaseAndUniqueBatchPreventDuplicateCreation(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	seedChannelModelDetectionSchedule(t, db, 201, common.ChannelStatusEnabled, model.ChannelModelDetectionPresetLow, now.Unix())

	results := make(chan ChannelModelDetectionScheduleResult, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := RunChannelModelDetectionScheduleOnce(context.Background(), db, now)
			results <- result
			errs <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			assert.Contains(t, err.Error(), "locked")
		}
	}
	var batches, runs int64
	require.NoError(t, db.Model(&model.ChannelModelDetectionBatch{}).Count(&batches).Error)
	require.NoError(t, db.Model(&model.ChannelModelDetectionRun{}).Count(&runs).Error)
	assert.EqualValues(t, 1, batches)
	assert.EqualValues(t, 1, runs)
}

func TestChannelModelDetectionScheduleBacklogAdvancesWithoutCreatingBatch(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	global := seedChannelModelDetectionSchedule(t, db, 301, common.ChannelStatusEnabled, model.ChannelModelDetectionPresetMedium, now.Unix())
	require.NoError(t, db.Create(&model.ChannelModelDetectionRun{
		ChannelId: 999, ConfigRevision: 1, GlobalConfigRevision: global.Revision,
		Trigger: model.ChannelModelDetectionTriggerScheduled, Preset: model.ChannelModelDetectionPresetLow,
		Status: model.ChannelModelDetectionRunStatusRunning,
	}).Error)

	result, err := RunChannelModelDetectionScheduleOnce(context.Background(), db, now)
	require.NoError(t, err)
	assert.True(t, result.Due)
	assert.True(t, result.SkippedForBacklog)
	assert.False(t, result.Created)
	assert.Equal(t, now.Add(24*time.Hour).Unix(), result.NextBatchAt)

	var count int64
	require.NoError(t, db.Model(&model.ChannelModelDetectionBatch{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestChannelModelDetectionScheduleBacklogQuotesTriggerForMySQL(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "root:pass@tcp(127.0.0.1:3306)/new_api",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	require.NoError(t, err)

	activeStatuses := []string{
		model.ChannelModelDetectionRunStatusQueued,
		model.ChannelModelDetectionRunStatusWaitingDetector,
		model.ChannelModelDetectionRunStatusSubmitting,
		model.ChannelModelDetectionRunStatusRunning,
		model.ChannelModelDetectionRunStatusSubmissionUnknown,
		model.ChannelModelDetectionRunStatusCanceling,
	}
	var count int64
	statement := channelModelDetectionActiveScheduledRunsQuery(db, activeStatuses).Count(&count).Statement

	sql := statement.SQL.String()
	assert.Contains(t, sql, "`trigger`")
	assert.NotContains(t, sql, "trigger = ?")
}

func TestChannelModelDetectionScheduleManualPresetIsIndependentAndDoesNotMoveNextBatch(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 5, 0, 0, 0, time.UTC)
	global := seedChannelModelDetectionSchedule(t, db, 401, common.ChannelStatusManuallyDisabled, model.ChannelModelDetectionPresetMedium, now.Add(12*time.Hour).Unix())

	_, err := CreateChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunInput{
		ChannelID: 401, Preset: model.ChannelModelDetectionPresetHigh,
	}, now)
	assert.ErrorIs(t, err, ErrChannelModelDetectionManualHighUnconfirmed)

	run, err := CreateChannelModelDetectionManualRun(context.Background(), db, ChannelModelDetectionManualRunInput{
		ChannelID: 401, Preset: model.ChannelModelDetectionPresetHigh, ConfirmHighCost: true,
		CreatedByUserID: 1, CreatedByUsername: "root",
	}, now)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionPresetHigh, run.Preset)
	assert.Equal(t, model.ChannelModelDetectionPresetSourceManualSelected, run.PresetSource)

	var stored model.ChannelModelDetectionGlobalConfig
	require.NoError(t, db.First(&stored, global.Id).Error)
	assert.Equal(t, global.NextBatchAt, stored.NextBatchAt)
	assert.Equal(t, model.ChannelModelDetectionPresetMedium, stored.ScheduledPreset)
}

func TestChannelModelDetectionScheduledHighConfirmationGatesBatchCreation(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)
	global := seedChannelModelDetectionSchedule(t, db, 501, common.ChannelStatusEnabled, model.ChannelModelDetectionPresetMedium, now.Unix())
	require.NoError(t, db.Model(&model.ChannelModelDetectionGlobalConfig{}).Where("id = ?", global.Id).
		Updates(map[string]any{
			"scheduled_preset":                  model.ChannelModelDetectionPresetHigh,
			"scheduled_high_confirmed_revision": int64(0),
		}).Error)

	_, err := RunChannelModelDetectionScheduleOnce(context.Background(), db, now)
	assert.ErrorIs(t, err, model.ErrChannelModelDetectionScheduledHighUnconfirmed)
	var batchCount int64
	require.NoError(t, db.Model(&model.ChannelModelDetectionBatch{}).Count(&batchCount).Error)
	assert.Zero(t, batchCount)

	require.NoError(t, db.Model(&model.ChannelModelDetectionGlobalConfig{}).Where("id = ?", global.Id).
		Update("scheduled_high_confirmed_revision", global.Revision).Error)
	result, err := RunChannelModelDetectionScheduleOnce(context.Background(), db, now)
	require.NoError(t, err)
	assert.True(t, result.Created)
	require.NotNil(t, result.Batch)
	assert.Equal(t, model.ChannelModelDetectionPresetHigh, result.Batch.Preset)
}
