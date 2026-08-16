package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	metric06RouteCount  = 1000
	metric06APIKeyCount = 4
	metric06ChannelBase = 600000
)

type metric06StorageStat struct {
	TableName  string
	DataBytes  int64
	IndexBytes int64
}

func metric06TableName(db *gorm.DB, value any) string {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(value); err != nil {
		return ""
	}
	return stmt.Schema.Table
}

func metric06StorageStats(
	t *testing.T,
	db *gorm.DB,
	databaseType common.DatabaseType,
	tableNames []string,
) []metric06StorageStat {
	t.Helper()
	stats := make([]metric06StorageStat, 0, len(tableNames))
	for _, tableName := range tableNames {
		stat := metric06StorageStat{TableName: tableName}
		switch databaseType {
		case common.DatabaseTypeMySQL:
			require.NoError(t, db.Raw(
				"SELECT COALESCE(data_length, 0) AS data_bytes, COALESCE(index_length, 0) AS index_bytes "+
					"FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
				tableName,
			).Scan(&stat).Error)
		case common.DatabaseTypePostgreSQL:
			require.NoError(t, db.Raw(
				"SELECT pg_relation_size(to_regclass(?)) AS data_bytes, "+
					"pg_indexes_size(to_regclass(?)) AS index_bytes",
				tableName, tableName,
			).Scan(&stat).Error)
		case common.DatabaseTypeSQLite:
			var indexNames []string
			require.NoError(t, db.Raw(
				"SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ?",
				tableName,
			).Scan(&indexNames).Error)
			if err := db.Raw("SELECT COALESCE(SUM(pgsize), 0) FROM dbstat WHERE name = ?", tableName).
				Scan(&stat.DataBytes).Error; err != nil {
				t.Logf("storage table=%s sqlite_dbstat_unavailable=%q", tableName, err)
				stat.DataBytes = -1
				stat.IndexBytes = -1
				stats = append(stats, stat)
				continue
			}
			for _, indexName := range indexNames {
				var indexBytes int64
				require.NoError(t, db.Raw(
					"SELECT COALESCE(SUM(pgsize), 0) FROM dbstat WHERE name = ?",
					indexName,
				).Scan(&indexBytes).Error)
				stat.IndexBytes += indexBytes
			}
		}
		stats = append(stats, stat)
	}
	return stats
}

func openMetric06ValidationDB(t *testing.T) (*gorm.DB, common.DatabaseType) {
	t.Helper()
	dialect := strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_METRIC06_DIALECT"))
	switch dialect {
	case "sqlite":
		return openMetric06GormDB(t, sqlite.Open(filepath.Join(t.TempDir(), "metric06.db"))), common.DatabaseTypeSQLite
	case "mysql":
		dsn := strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_METRIC06_DSN"))
		require.Contains(t, dsn, "new_api_metric06", "METRIC-06 只能使用独立测试库")
		return openMetric06GormDB(t, mysql.Open(dsn)), common.DatabaseTypeMySQL
	case "postgres":
		dsn := strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_METRIC06_DSN"))
		require.Contains(t, dsn, "new_api_metric06", "METRIC-06 只能使用独立测试库")
		return openMetric06GormDB(t, postgres.Open(dsn)), common.DatabaseTypePostgreSQL
	default:
		t.Skip("设置 CHANNEL_MONITOR_METRIC06_DIALECT=sqlite|mysql|postgres 运行高基数验收")
		return nil, ""
	}
}

func openMetric06GormDB(t *testing.T, dialector gorm.Dialector) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}

func requireMetric06ValidationDatabase(
	t *testing.T,
	db *gorm.DB,
	databaseType common.DatabaseType,
) {
	t.Helper()
	if databaseType == common.DatabaseTypeSQLite {
		return
	}
	var databaseName string
	switch databaseType {
	case common.DatabaseTypeMySQL:
		require.NoError(t, db.Raw("SELECT DATABASE()").Scan(&databaseName).Error)
	case common.DatabaseTypePostgreSQL:
		require.NoError(t, db.Raw("SELECT current_database()").Scan(&databaseName).Error)
	}
	require.Equal(t, "new_api_metric06", databaseName, "METRIC-06 禁止连接非独立测试库")
}

func metric06RouteIdentity(route int) (int, string, string) {
	return metric06ChannelBase + route/10,
		fmt.Sprintf("metric06-model-%02d", route%10),
		fmt.Sprintf("metric06-group-%02d", route%5)
}

func metric06SortedRouteRows(t *testing.T, db *gorm.DB, start int64, end int64) []model.ChannelMonitorMinuteRouteMetric {
	t.Helper()
	var rows []model.ChannelMonitorMinuteRouteMetric
	require.NoError(t, db.Where("minute_start >= ? AND minute_start < ?", start, end).Find(&rows).Error)
	for index := range rows {
		rows[index].Id = 0
	}
	sort.Slice(rows, func(i int, j int) bool {
		if rows[i].MinuteStart != rows[j].MinuteStart {
			return rows[i].MinuteStart < rows[j].MinuteStart
		}
		if rows[i].ChannelId != rows[j].ChannelId {
			return rows[i].ChannelId < rows[j].ChannelId
		}
		if rows[i].ModelKey != rows[j].ModelKey {
			return rows[i].ModelKey < rows[j].ModelKey
		}
		return rows[i].GroupKey < rows[j].GroupKey
	})
	return rows
}

func metric06SortedAPIKeyRows(t *testing.T, db *gorm.DB, start int64, end int64) []model.ChannelMonitorMinuteAPIKeyMetric {
	t.Helper()
	var rows []model.ChannelMonitorMinuteAPIKeyMetric
	require.NoError(t, db.Where("minute_start >= ? AND minute_start < ?", start, end).Find(&rows).Error)
	for index := range rows {
		rows[index].Id = 0
	}
	sort.Slice(rows, func(i int, j int) bool {
		if rows[i].MinuteStart != rows[j].MinuteStart {
			return rows[i].MinuteStart < rows[j].MinuteStart
		}
		if rows[i].ChannelId != rows[j].ChannelId {
			return rows[i].ChannelId < rows[j].ChannelId
		}
		if rows[i].ModelKey != rows[j].ModelKey {
			return rows[i].ModelKey < rows[j].ModelKey
		}
		if rows[i].GroupKey != rows[j].GroupKey {
			return rows[i].GroupKey < rows[j].GroupKey
		}
		return rows[i].APIKeyKey < rows[j].APIKeyKey
	})
	return rows
}

func metric06SortedBucketRows(t *testing.T, db *gorm.DB, start int64, end int64) []model.ChannelMonitorMinuteDurationBucket {
	t.Helper()
	var rows []model.ChannelMonitorMinuteDurationBucket
	require.NoError(t, db.Where("minute_start >= ? AND minute_start < ?", start, end).Find(&rows).Error)
	for index := range rows {
		rows[index].Id = 0
	}
	sort.Slice(rows, func(i int, j int) bool {
		if rows[i].MinuteStart != rows[j].MinuteStart {
			return rows[i].MinuteStart < rows[j].MinuteStart
		}
		if rows[i].ChannelId != rows[j].ChannelId {
			return rows[i].ChannelId < rows[j].ChannelId
		}
		if rows[i].ModelKey != rows[j].ModelKey {
			return rows[i].ModelKey < rows[j].ModelKey
		}
		if rows[i].GroupKey != rows[j].GroupKey {
			return rows[i].GroupKey < rows[j].GroupKey
		}
		return rows[i].BucketIndex < rows[j].BucketIndex
	})
	return rows
}

func metric06SnapshotHash(t *testing.T, value any) string {
	t.Helper()
	payload, err := common.Marshal(value)
	require.NoError(t, err)
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func TestChannelMonitorMetricHighCardinalityValidation(t *testing.T) {
	db, databaseType := openMetric06ValidationDB(t)
	requireMetric06ValidationDatabase(t, db, databaseType)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalLogConsumeEnabled := common.LogConsumeEnabled
	originalErrorLogEnabled := constant.ErrorLogEnabled
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(databaseType, databaseType)
	common.LogConsumeEnabled = true
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		key := channelMonitorAggregationDatabaseKey{db: db, logDB: db}
		channelMonitorAggregationStateMu.Lock()
		delete(channelMonitorAggregationLocalCompletedThrough, key)
		channelMonitorAggregationStateMu.Unlock()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.LogConsumeEnabled = originalLogConsumeEnabled
		constant.ErrorLogEnabled = originalErrorLogEnabled
	})

	validationModels := []any{
		&model.Log{},
		&model.ChannelMonitorMinuteRouteMetric{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
		&model.ChannelMonitorMinuteDurationBucket{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelMonitorDirtyMinute{},
		&model.ChannelSmartScheduleRouteState{},
		&model.ChannelSmartScheduleModelSampleState{},
		&model.ChannelDailyCost{},
		&model.ChannelDailyAPIKeyCost{},
	}
	require.NoError(t, db.AutoMigrate(validationModels...))
	metricTableNames := []string{
		metric06TableName(db, &model.ChannelMonitorMinuteRouteMetric{}),
		metric06TableName(db, &model.ChannelMonitorMinuteAPIKeyMetric{}),
		metric06TableName(db, &model.ChannelMonitorMinuteDurationBucket{}),
	}
	for _, tableName := range metricTableNames {
		require.NotEmpty(t, tableName)
	}
	if databaseType != common.DatabaseTypeSQLite {
		t.Cleanup(func() {
			for index := len(validationModels) - 1; index >= 0; index-- {
				assert.NoError(t, db.Migrator().DropTable(validationModels[index]))
			}
			for _, value := range validationModels {
				assert.False(t, db.Migrator().HasTable(value), "METRIC-06 独立测试表必须清理")
			}
		})
	}
	var existingLogs int64
	require.NoError(t, db.Model(&model.Log{}).Count(&existingLogs).Error)
	require.Zero(t, existingLogs, "METRIC-06 验收库必须为空")

	minute0 := int64(1786816800)
	minute1 := minute0 + 60
	targetEnd := minute1 + 60
	logs := make([]model.Log, 0, metric06RouteCount*(metric06APIKeyCount+2))
	for route := 0; route < metric06RouteCount; route++ {
		channelID, modelName, groupName := metric06RouteIdentity(route)
		for apiKey := 0; apiKey < metric06APIKeyCount; apiKey++ {
			logs = append(logs, model.Log{
				ChannelId: channelID, ModelName: modelName, Group: groupName,
				TokenId:   route*metric06APIKeyCount + apiKey + 1,
				TokenName: fmt.Sprintf("metric06-key-%d-%d", route, apiKey),
				CreatedAt: minute0 + int64(apiKey+1), Type: model.LogTypeConsume,
				IsStream: true, CompletionTokens: 100, UseTime: 10,
				Other: `{"frt":150,"cache_tokens":25,"input_tokens_total":100}`,
			})
		}
		requestID := fmt.Sprintf("metric06-retry-%d", route)
		logs = append(logs,
			model.Log{
				ChannelId: channelID, ModelName: modelName, Group: groupName,
				TokenId: route*metric06APIKeyCount + 1, TokenName: fmt.Sprintf("metric06-key-%d-0", route),
				CreatedAt: minute0 + 50, Type: model.LogTypeError, IsRetryAttempt: true,
				RequestId: requestID, Other: `{"channel_monitor_attempt_duration_ms":500}`,
			},
			model.Log{
				ChannelId: channelID, ModelName: modelName, Group: groupName,
				TokenId: route*metric06APIKeyCount + 1, TokenName: fmt.Sprintf("metric06-key-%d-0", route),
				CreatedAt: minute1 + 5, Type: model.LogTypeError, RequestId: requestID,
				Other: `{"channel_monitor_attempt_duration_ms":500,"channel_monitor_final_retry_summary":true}`,
			},
		)
	}
	insertStarted := time.Now()
	require.NoError(t, db.CreateInBatches(&logs, 500).Error)
	t.Logf("insert_logs=%d elapsed_ms=%d", len(logs), time.Since(insertStarted).Milliseconds())

	aggregateStarted := time.Now()
	result, err := model.AggregateChannelMonitorMinuteRangeWithState(
		context.Background(), minute0, targetEnd, true,
	)
	require.NoError(t, err)
	aggregateElapsed := time.Since(aggregateStarted)
	var routeRows int64
	var apiKeyRows int64
	var bucketRows int64
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).Count(&routeRows).Error)
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteAPIKeyMetric{}).Count(&apiKeyRows).Error)
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteDurationBucket{}).Count(&bucketRows).Error)
	assert.Equal(t, int64(metric06RouteCount*2), routeRows)
	assert.Equal(t, int64(metric06RouteCount*(metric06APIKeyCount+1)), apiKeyRows)
	assert.Equal(t, int64(metric06RouteCount), bucketRows)
	assert.Equal(t, len(logs), result.ScannedLogRows)
	assert.Equal(t, metric06RouteCount*2, result.MetricRows)
	assert.Equal(t, metric06RouteCount*(metric06APIKeyCount+1), result.APIKeyMetricRows)
	assert.Equal(t, metric06RouteCount, result.DurationBucketRows)
	assert.Equal(t, 8000, result.GeneratedRows())
	t.Logf(
		"aggregate scanned=%d route_rows=%d api_key_rows=%d bucket_rows=%d elapsed_ms=%d",
		result.ScannedLogRows, routeRows, apiKeyRows, bucketRows, aggregateElapsed.Milliseconds(),
	)

	var minute0RouteRows int64
	var minute0APIKeyRows int64
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).Where("minute_start = ?", minute0).Count(&minute0RouteRows).Error)
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteAPIKeyMetric{}).Where("minute_start = ?", minute0).Count(&minute0APIKeyRows).Error)
	assert.Equal(t, int64(metric06RouteCount), minute0RouteRows)
	assert.Equal(t, int64(metric06RouteCount*metric06APIKeyCount), minute0APIKeyRows)

	windows := make([]model.ChannelMonitorRouteMetricWindow, 0, metric06RouteCount)
	for route := 0; route < metric06RouteCount; route++ {
		channelID, modelName, _ := metric06RouteIdentity(route)
		windows = append(windows, model.ChannelMonitorRouteMetricWindow{
			ChannelId: channelID, ModelName: modelName, StartTimestamp: minute0,
		})
	}
	var scheduleAPIKeyQueries atomic.Int64
	scheduleQueryCallback := "metric06:count_schedule_api_key_queries"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(scheduleQueryCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == metricTableNames[1] {
			scheduleAPIKeyQueries.Add(1)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove(scheduleQueryCallback))
	})
	queryStarted := time.Now()
	windowMetrics, err := model.GetChannelMonitorRouteMetricsForWindows(
		context.Background(), windows, targetEnd, true, true,
	)
	queryElapsed := time.Since(queryStarted)
	require.NoError(t, err)
	assert.Len(t, windowMetrics, metric06RouteCount)
	assert.Zero(t, scheduleAPIKeyQueries.Load(), "调度查询不得读取 API Key 分钟表")
	t.Logf(
		"schedule_windows=%d api_key_table_queries=%d elapsed_ms=%d",
		len(windowMetrics), scheduleAPIKeyQueries.Load(), queryElapsed.Milliseconds(),
	)

	coverageBefore, err := model.GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	idsBefore := make([]int64, 0, routeRows)
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).Order("id ASC").Pluck("id", &idsBefore).Error)
	var repeatedWrites atomic.Int64
	callbackName := "metric06:count_repeated_metric_writes"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.Table, "channel_monitor_minute_") {
			repeatedWrites.Add(1)
		}
	}))
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if strings.HasPrefix(tx.Statement.Table, "channel_monitor_minute_") {
			repeatedWrites.Add(1)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Create().Remove(callbackName))
		require.NoError(t, db.Callback().Delete().Remove(callbackName))
	})
	repeatStarted := time.Now()
	require.NoError(t, runChannelMonitorAggregationAt(context.Background(), targetEnd+20, false))
	coverageAfter, err := model.GetChannelMonitorAggregationCoverage(context.Background())
	require.NoError(t, err)
	idsAfter := make([]int64, 0, routeRows)
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).Order("id ASC").Pluck("id", &idsAfter).Error)
	assert.Equal(t, coverageBefore.Revision, coverageAfter.Revision)
	assert.Equal(t, idsBefore, idsAfter)
	assert.Zero(t, repeatedWrites.Load())
	t.Logf("repeat_worker metric_writes=%d elapsed_ms=%d", repeatedWrites.Load(), time.Since(repeatStarted).Milliseconds())

	lateChannelID, lateModelName, lateGroupName := metric06RouteIdentity(0)
	require.NoError(t, db.Create(&model.Log{
		ChannelId: lateChannelID, ModelName: lateModelName, Group: lateGroupName,
		TokenId: metric06RouteCount*metric06APIKeyCount + 1, TokenName: "metric06-late-key",
		CreatedAt: minute0 + 20, Type: model.LogTypeConsume,
		IsStream: true, CompletionTokens: 100, UseTime: 10, Other: `{"frt":200}`,
	}).Error)
	markDirtyErr := model.MarkChannelMonitorDirtyMinutes(
		context.Background(), []int64{minute0, minute1}, model.ChannelMonitorDirtyReasonCrossMinuteRetry,
	)
	if !assert.NoError(t, markDirtyErr, "三数据库必须都能幂等标记脏分钟") {
		dirtyRows := []model.ChannelMonitorDirtyMinute{
			{
				MinuteStart: minute0, DirtyReason: model.ChannelMonitorDirtyReasonCrossMinuteRetry,
				FirstMarkedAt: targetEnd, LastMarkedAt: targetEnd, MarkCount: 1,
			},
			{
				MinuteStart: minute1, DirtyReason: model.ChannelMonitorDirtyReasonCrossMinuteRetry,
				FirstMarkedAt: targetEnd, LastMarkedAt: targetEnd, MarkCount: 1,
			},
		}
		require.NoError(t, db.Create(&dirtyRows).Error, "记录标记失败后继续采集定点修复证据")
	}
	repairStarted := time.Now()
	require.NoError(t, repairChannelMonitorDirtyMinutes(
		context.Background(), channelMonitorAggregationDatabaseKey{db: db, logDB: db}, targetEnd,
	))
	repairElapsed := time.Since(repairStarted)
	repairedRouteRows := metric06SortedRouteRows(t, db, minute0, targetEnd)
	repairedAPIKeyRows := metric06SortedAPIKeyRows(t, db, minute0, targetEnd)
	repairedBucketRows := metric06SortedBucketRows(t, db, minute0, targetEnd)

	rebuildStarted := time.Now()
	fullResult, err := model.AggregateChannelMonitorMinuteRangeWithResult(
		context.Background(), minute0, targetEnd,
	)
	require.NoError(t, err)
	fullRebuildElapsed := time.Since(rebuildStarted)
	assert.Equal(t, metric06RouteCount*2, fullResult.MetricRows)
	assert.Equal(t, metric06RouteCount*(metric06APIKeyCount+1)+1, fullResult.APIKeyMetricRows)
	assert.Equal(t, metric06RouteCount+1, fullResult.DurationBucketRows)
	assert.Equal(t, 8002, fullResult.GeneratedRows())
	fullRouteRows := metric06SortedRouteRows(t, db, minute0, targetEnd)
	fullAPIKeyRows := metric06SortedAPIKeyRows(t, db, minute0, targetEnd)
	fullBucketRows := metric06SortedBucketRows(t, db, minute0, targetEnd)
	for _, stat := range metric06StorageStats(t, db, databaseType, metricTableNames) {
		var tableRows int64
		require.NoError(t, db.Table(stat.TableName).Count(&tableRows).Error)
		t.Logf(
			"storage table=%s rows=%d data_bytes=%d index_bytes=%d total_bytes=%d",
			stat.TableName, tableRows, stat.DataBytes, stat.IndexBytes, stat.DataBytes+stat.IndexBytes,
		)
	}
	t.Logf(
		"dirty_repair_ms=%d full_rebuild_ms=%d full_scanned=%d",
		repairElapsed.Milliseconds(), fullRebuildElapsed.Milliseconds(), fullResult.ScannedLogRows,
	)

	var repairedRetryCount int64
	for _, row := range repairedRouteRows {
		if row.MinuteStart == minute0 {
			repairedRetryCount += row.RetryFailureCount
		}
	}
	var fullRetryCount int64
	for _, row := range fullRouteRows {
		if row.MinuteStart == minute0 {
			fullRetryCount += row.RetryFailureCount
		}
	}
	t.Logf("cross_minute_retry_count repaired=%d full_rebuild=%d", repairedRetryCount, fullRetryCount)
	repairedRouteHash := metric06SnapshotHash(t, repairedRouteRows)
	fullRouteHash := metric06SnapshotHash(t, fullRouteRows)
	repairedAPIKeyHash := metric06SnapshotHash(t, repairedAPIKeyRows)
	fullAPIKeyHash := metric06SnapshotHash(t, fullAPIKeyRows)
	repairedBucketHash := metric06SnapshotHash(t, repairedBucketRows)
	fullBucketHash := metric06SnapshotHash(t, fullBucketRows)
	t.Logf(
		"repair_hash route=%s full_route=%s api=%s full_api=%s bucket=%s full_bucket=%s",
		repairedRouteHash, fullRouteHash, repairedAPIKeyHash, fullAPIKeyHash, repairedBucketHash, fullBucketHash,
	)
	assert.Equal(t, fullRouteHash, repairedRouteHash, "单分钟定点修复必须与范围全量重建一致")
	assert.Equal(t, fullAPIKeyHash, repairedAPIKeyHash, "API Key 定点修复必须与范围全量重建一致")
	assert.Equal(t, fullBucketHash, repairedBucketHash, "时延桶定点修复必须与范围全量重建一致")

	for day := 0; day <= 31; day++ {
		minuteStart := targetEnd - int64(day)*86400
		require.NoError(t, db.Create(&model.ChannelMonitorMinuteRouteMetric{
			MinuteStart: minuteStart, ChannelId: 700000, ModelKey: "metric06-retention-model",
			GroupKey: "metric06-retention-group", ModelName: "metric06-retention-model", GroupName: "metric06-retention-group",
		}).Error)
		require.NoError(t, db.Create(&model.ChannelMonitorMinuteDurationBucket{
			MinuteStart: minuteStart, ChannelId: 700000, ModelKey: "metric06-retention-model",
			GroupKey: "metric06-retention-group", ModelName: "metric06-retention-model", GroupName: "metric06-retention-group",
			BucketIndex: 1, Count: 1, TotalMs: 100,
		}).Error)
	}
	for day := 0; day <= 8; day++ {
		minuteStart := targetEnd - int64(day)*86400
		require.NoError(t, db.Create(&model.ChannelMonitorMinuteAPIKeyMetric{
			MinuteStart: minuteStart, ChannelId: 700000, ModelKey: "metric06-retention-model",
			GroupKey: "metric06-retention-group", APIKeyKey: "metric06-retention-key",
			ModelName: "metric06-retention-model", GroupName: "metric06-retention-group",
			APIKeyId: 1, APIKeyName: "metric06-retention-key",
		}).Error)
	}
	retentionStarted := time.Now()
	retentionResult, err := model.DeleteChannelMonitorCostsBefore(
		context.Background(), targetEnd-120*86400, targetEnd-30*86400, targetEnd-7*86400,
		1000, model.ChannelMonitorCleanupBudget{},
	)
	require.NoError(t, err)
	var retainedRouteDays int64
	var retainedAPIKeyDays int64
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteRouteMetric{}).Where("channel_id = ?", 700000).Count(&retainedRouteDays).Error)
	require.NoError(t, db.Model(&model.ChannelMonitorMinuteAPIKeyMetric{}).Where("channel_id = ?", 700000).Count(&retainedAPIKeyDays).Error)
	assert.Equal(t, int64(31), retainedRouteDays)
	assert.Equal(t, int64(8), retainedAPIKeyDays)
	assert.Equal(t, int64(1), retentionResult.RouteMetricRowsDeleted)
	assert.Equal(t, int64(1), retentionResult.APIKeyMetricRowsDeleted)
	assert.Equal(t, int64(1), retentionResult.DurationBucketRowsDeleted)
	t.Logf(
		"retention route_days=%d api_key_days=%d deleted_route=%d deleted_api=%d elapsed_ms=%d",
		retainedRouteDays, retainedAPIKeyDays, retentionResult.RouteMetricRowsDeleted,
		retentionResult.APIKeyMetricRowsDeleted, time.Since(retentionStarted).Milliseconds(),
	)
}
