package model

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type channelMonitorDirtyMinuteSQLRecorder struct {
	statements []string
}

func (recorder *channelMonitorDirtyMinuteSQLRecorder) LogMode(_ logger.LogLevel) logger.Interface {
	return recorder
}

func (recorder *channelMonitorDirtyMinuteSQLRecorder) Info(context.Context, string, ...any)  {}
func (recorder *channelMonitorDirtyMinuteSQLRecorder) Warn(context.Context, string, ...any)  {}
func (recorder *channelMonitorDirtyMinuteSQLRecorder) Error(context.Context, string, ...any) {}

func (recorder *channelMonitorDirtyMinuteSQLRecorder) Trace(
	_ context.Context,
	_ time.Time,
	sql func() (string, int64),
	_ error,
) {
	statement, _ := sql()
	recorder.statements = append(recorder.statements, statement)
}

func setupChannelMonitorDirtyMinuteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupChannelMonitorMinuteAggregationTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorDirtyMinute{}))
	resetChannelMonitorDirtyMinuteDatabaseState(db)
	t.Cleanup(func() {
		resetChannelMonitorDirtyMinuteDatabaseState(db)
	})
	return db
}

func TestCurrentMinuteLogDoesNotAccessMainDatabase(t *testing.T) {
	originalDB := DB
	originalLogDB := LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	mainDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "dirty-main.db")), &gorm.Config{})
	require.NoError(t, err)
	logDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "dirty-log.db")), &gorm.Config{})
	require.NoError(t, err)
	mainSQLDB, err := mainDB.DB()
	require.NoError(t, err)
	logSQLDB, err := logDB.DB()
	require.NoError(t, err)
	require.NoError(t, mainDB.AutoMigrate(&ChannelMonitorAggregationState{}, &ChannelMonitorDirtyMinute{}))
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	DB = mainDB
	LOG_DB = logDB
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	resetChannelMonitorDirtyMinuteDatabaseState(mainDB)
	t.Cleanup(func() {
		resetChannelMonitorDirtyMinuteDatabaseState(mainDB)
		DB = originalDB
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		require.NoError(t, mainSQLDB.Close())
		require.NoError(t, logSQLDB.Close())
	})

	var mainOperations atomic.Int64
	countOperation := func(*gorm.DB) { mainOperations.Add(1) }
	require.NoError(t, mainDB.Callback().Create().Before("*").Register("test:dirty-main-create", countOperation))
	require.NoError(t, mainDB.Callback().Query().Before("*").Register("test:dirty-main-query", countOperation))
	require.NoError(t, mainDB.Callback().Update().Before("*").Register("test:dirty-main-update", countOperation))
	require.NoError(t, mainDB.Callback().Delete().Before("*").Register("test:dirty-main-delete", countOperation))
	require.NoError(t, mainDB.Callback().Row().Before("*").Register("test:dirty-main-row", countOperation))
	require.NoError(t, mainDB.Callback().Raw().Before("*").Register("test:dirty-main-raw", countOperation))

	require.NoError(t, createLog(&Log{
		ChannelId: 1,
		Type:      LogTypeConsume,
		CreatedAt: common.GetTimestamp(),
	}))
	assert.Zero(t, mainOperations.Load())

	var logCount int64
	require.NoError(t, logDB.Model(&Log{}).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)
}

func TestMarkChannelMonitorDirtyMinuteIsIdempotent(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	ctx := context.Background()
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(ctx, 300))

	require.NoError(t, MarkChannelMonitorDirtyMinute(ctx, 121, "late_log"))
	require.NoError(t, MarkChannelMonitorDirtyMinute(ctx, 179, "cross_minute_retry"))

	var rows []ChannelMonitorDirtyMinute
	require.NoError(t, db.Order("minute_start ASC").Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(120), rows[0].MinuteStart)
	assert.Equal(t, int64(2), rows[0].MarkCount)
	assert.Equal(t, "late_log", rows[0].DirtyReason)
}

func TestClosedMinuteLogMarksDirtyBeforeWatermarkAdvances(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	ctx := context.Background()
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(ctx, 60))

	require.NoError(t, createLog(&Log{
		ChannelId: 1,
		Type:      LogTypeConsume,
		CreatedAt: 121,
	}))
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(ctx, 180))

	var marker ChannelMonitorDirtyMinute
	require.NoError(t, db.Where("minute_start = ?", 120).First(&marker).Error)
	assert.Equal(t, ChannelMonitorDirtyReasonLateLog, marker.DirtyReason)
}

func TestGroupProbeLogDoesNotMarkDirtyMinute(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	ctx := context.Background()
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(ctx, 60))

	require.NoError(t, createLog(&Log{
		ChannelId: 1,
		Type:      LogTypeError,
		CreatedAt: 121,
		Other:     `{"channel_monitor_group_probe":true}`,
	}))

	var markerCount int64
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Count(&markerCount).Error)
	assert.Zero(t, markerCount)
}

func TestCreateLogReturnsMarkerFailureAfterPersistingSourceLog(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	markerErr := errors.New("forced dirty marker failure")
	callbackName := "test:fail-dirty-minute-create"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == channelMonitorDirtyMinuteTable {
			tx.AddError(markerErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	err := createLog(&Log{
		ChannelId: 1,
		Type:      LogTypeConsume,
		CreatedAt: 121,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, markerErr)
	assert.Contains(t, err.Error(), "日志已写入，但渠道监控脏分钟标记失败")

	var logCount int64
	require.NoError(t, db.Model(&Log{}).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)
	var markerCount int64
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Count(&markerCount).Error)
	assert.Zero(t, markerCount)
	var pendingCount int64
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinutePending{}).Count(&pendingCount).Error)
	assert.Equal(t, int64(1), pendingCount)
}

func TestRenewDirtyMinuteLeaseAndRejectStaleComplete(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	ctx := context.Background()
	require.NoError(t, MarkChannelMonitorDirtyMinute(ctx, 121, "late_log"))

	now := common.GetTimestamp()
	claims, err := ClaimChannelMonitorDirtyMinutes(ctx, 1, "worker-a", now+60)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	originalUntil := claims[0].ClaimedUntil
	require.NoError(t, RenewChannelMonitorDirtyMinutes(ctx, "worker-a", claims, now+180))
	var renewed ChannelMonitorDirtyMinute
	require.NoError(t, db.First(&renewed, claims[0].Id).Error)
	assert.Greater(t, renewed.ClaimedUntil, originalUntil)

	// Once the lease expires, a different worker may reclaim it. The stale
	// worker's complete must not delete the replacement claim.
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).
		Where("id = ?", claims[0].Id).
		Update("claimed_until", now-1).Error)
	otherClaims, err := ClaimChannelMonitorDirtyMinutes(ctx, 1, "worker-b", now+180)
	require.NoError(t, err)
	require.Len(t, otherClaims, 1)
	require.NoError(t, CompleteChannelMonitorDirtyMinutes(ctx, "worker-a", claims))
	var remaining ChannelMonitorDirtyMinute
	require.NoError(t, db.First(&remaining, claims[0].Id).Error)
	assert.Equal(t, "worker-b", remaining.ClaimedBy)
	require.NoError(t, CompleteChannelMonitorDirtyMinutes(ctx, "worker-b", otherClaims))
	var count int64
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestDirtyMinutePendingMarkerFailureCanBeRetried(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	ctx := context.Background()
	markerErr := errors.New("forced dirty marker failure")
	callbackName := "test:fail-dirty-minute-pending"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == channelMonitorDirtyMinuteTable {
			tx.AddError(markerErr)
		}
	}))

	err := createLog(&Log{ChannelId: 1, Type: LogTypeConsume, CreatedAt: 121})
	require.Error(t, err)
	var pendingCount int64
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinutePending{}).Count(&pendingCount).Error)
	assert.Equal(t, int64(1), pendingCount)
	require.NoError(t, db.Callback().Create().Remove(callbackName))

	require.NoError(t, RetryChannelMonitorDirtyMinutePending(ctx, 10))
	var markerCount int64
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Count(&markerCount).Error)
	assert.Equal(t, int64(1), markerCount)
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinutePending{}).Count(&pendingCount).Error)
	assert.Zero(t, pendingCount)
}

func TestDirtyMinuteSQLiteBusyRetryIsBoundedAndClassified(t *testing.T) {
	attempts := 0
	require.NoError(t, withChannelMonitorDirtyMinuteRetry(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return errors.New("database is locked")
		}
		return nil
	}))
	assert.Equal(t, 3, attempts)

	attempts = 0
	err := withChannelMonitorDirtyMinuteRetry(context.Background(), func() error {
		attempts++
		return errors.New("constraint failed")
	})
	require.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestMarkChannelMonitorDirtyMinutesQualifiesConflictUpdateColumn(t *testing.T) {
	tests := []struct {
		name          string
		dialector     gorm.Dialector
		qualifiedMark string
	}{
		{
			name: "mysql",
			dialector: mysql.New(mysql.Config{
				DSN:                       "new_api:test@tcp(127.0.0.1:3306)/new_api?charset=utf8mb4&parseTime=True&loc=Local",
				SkipInitializeWithVersion: true,
			}),
			qualifiedMark: "`channel_monitor_dirty_minutes`.`mark_count`",
		},
		{
			name: "postgres",
			dialector: postgres.New(postgres.Config{
				DSN:                  "host=127.0.0.1 user=new_api password=test dbname=new_api port=5432 sslmode=disable",
				PreferSimpleProtocol: true,
			}),
			qualifiedMark: `"channel_monitor_dirty_minutes"."mark_count"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalDB := DB
			recorder := &channelMonitorDirtyMinuteSQLRecorder{}
			db, err := gorm.Open(test.dialector, &gorm.Config{
				DryRun:                 true,
				DisableAutomaticPing:   true,
				SkipDefaultTransaction: true,
				Logger:                 recorder,
			})
			require.NoError(t, err)
			DB = db
			state := channelMonitorDirtyMinuteDatabaseStateFor(db)
			state.tableChecked = true
			state.tableExists = true
			t.Cleanup(func() {
				resetChannelMonitorDirtyMinuteDatabaseState(db)
				DB = originalDB
			})

			require.NoError(t, MarkChannelMonitorDirtyMinute(context.Background(), 121, "late_log"))
			generatedSQL := strings.Join(recorder.statements, "\n")
			assert.Contains(t, generatedSQL, test.qualifiedMark+" + 1")
		})
	}
}

func TestClaimChannelMonitorDirtyMinutesKeepsMarkerOnFailureAndSupportsRelease(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	ctx := context.Background()
	require.NoError(t, MarkChannelMonitorDirtyMinute(ctx, 121, "late_log"))

	now := common.GetTimestamp()
	_, err := ClaimChannelMonitorDirtyMinutes(ctx, 10, "worker-a", now)
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	claims, err := ClaimChannelMonitorDirtyMinutes(ctx, 10, "worker-a", now+60)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.NoError(t, ReleaseChannelMonitorDirtyMinutes(ctx, "worker-a", claims))

	claims, err = ClaimChannelMonitorDirtyMinutes(ctx, 10, "worker-b", now+120)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.NoError(t, MarkChannelMonitorDirtyMinute(ctx, 121, "late_log"))
	require.NoError(t, CompleteChannelMonitorDirtyMinutes(ctx, "worker-b", claims))
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	var remarked ChannelMonitorDirtyMinute
	require.NoError(t, db.First(&remarked).Error)
	assert.Empty(t, remarked.ClaimedBy)
	assert.Zero(t, remarked.ClaimedUntil)

	claims, err = ClaimChannelMonitorDirtyMinutes(ctx, 10, "worker-c", now+180)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.NoError(t, CompleteChannelMonitorDirtyMinutes(ctx, "worker-c", claims))
	require.NoError(t, db.Model(&ChannelMonitorDirtyMinute{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestLateLogsMarkCrossMinuteRetryAndFinalSummaryMinutes(t *testing.T) {
	db := setupChannelMonitorDirtyMinuteTestDB(t)
	ctx := context.Background()
	require.NoError(t, AdvanceChannelMonitorAggregationCompletedThrough(ctx, 300))

	retry := &Log{
		ChannelId:      7,
		ModelName:      "model-a",
		Group:          "vip",
		TokenId:        11,
		TokenName:      "key-a",
		Type:           LogTypeError,
		IsRetryAttempt: true,
		RequestId:      "cross-minute-request",
		CreatedAt:      121,
		Other:          `{"channel_monitor_attempt_duration_ms":1500}`,
	}
	finalSummary := &Log{
		ChannelId: 7,
		ModelName: "model-a",
		Group:     "vip",
		TokenId:   11,
		TokenName: "key-a",
		Type:      LogTypeError,
		RequestId: "cross-minute-request",
		CreatedAt: 181,
		Other:     `{"channel_monitor_attempt_duration_ms":1500,"channel_monitor_final_retry_summary":true}`,
	}
	require.NoError(t, createLog(retry))
	require.NoError(t, createLog(finalSummary))

	var rows []ChannelMonitorDirtyMinute
	require.NoError(t, db.Order("minute_start ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, int64(120), rows[0].MinuteStart)
	assert.Equal(t, int64(180), rows[1].MinuteStart)
	assert.Equal(t, ChannelMonitorDirtyReasonCrossMinuteRetry, rows[0].DirtyReason)
}
