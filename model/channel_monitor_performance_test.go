package model

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelMonitorPerformanceMetricsUsesUsageLogTimingRules(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "performance.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	useChannelMonitorMinuteTestDB(t, db)
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	logs := []*Log{
		{ChannelId: 1, ModelName: "model-a", CreatedAt: 121, Type: LogTypeConsume, IsStream: true, CompletionTokens: 100, UseTime: 10, Other: `{"frt":1000}`},
		{ChannelId: 1, ModelName: "model-a", CreatedAt: 122, Type: LogTypeConsume, IsStream: true, CompletionTokens: 90, UseTime: 3, Other: `{"frt":3000}`},
		{ChannelId: 1, ModelName: "model-b", CreatedAt: 123, Type: LogTypeConsume, IsStream: true, Other: `{"frt":500}`},
		{ChannelId: 2, ModelName: "model-a", CreatedAt: 124, Type: LogTypeConsume, IsStream: true, CompletionTokens: 40, UseTime: 2, Other: "not-json"},
		{ChannelId: 1, ModelName: "non-stream", CreatedAt: 125, Type: LogTypeConsume, CompletionTokens: 100, UseTime: 1, Other: `{"frt":100}`},
		{ChannelId: 1, ModelName: "error-log", CreatedAt: 126, Type: LogTypeError, IsStream: true, CompletionTokens: 100, UseTime: 1, Other: `{"frt":100}`},
		{ChannelId: 1, ModelName: "too-old", CreatedAt: 119, Type: LogTypeConsume, IsStream: true, CompletionTokens: 100, UseTime: 1, Other: `{"frt":100}`},
		{ChannelId: 0, ModelName: "no-channel", CreatedAt: 127, Type: LogTypeConsume, IsStream: true, CompletionTokens: 100, UseTime: 1, Other: `{"frt":100}`},
		{ChannelId: 1, ModelName: "", CreatedAt: 128, Type: LogTypeConsume, IsStream: true, CompletionTokens: 100, UseTime: 1, Other: `{"frt":100}`},
	}
	require.NoError(t, db.Create(&logs).Error)
	aggregateChannelMonitorMinuteTestRange(t, 60, 180)

	metrics, err := GetChannelMonitorPerformanceMetrics(context.Background(), 120)
	require.NoError(t, err)
	require.Len(t, metrics, 3)

	assert.Equal(t, "model-a", metrics[0].ModelName)
	assert.Equal(t, 1, metrics[0].ChannelId)
	assert.Equal(t, 2, metrics[0].SampleCount)
	assert.Equal(t, 2, metrics[0].FirstTokenSampleCount)
	assert.Equal(t, 2, metrics[0].TPSSampleCount)
	require.NotNil(t, metrics[0].AverageFirstTokenMs)
	assert.InDelta(t, 2000, *metrics[0].AverageFirstTokenMs, 0.001)
	require.NotNil(t, metrics[0].AverageTPS)
	assert.InDelta(t, 20, *metrics[0].AverageTPS, 0.001)
	require.NotNil(t, metrics[0].LatestFirstTokenMs)
	assert.InDelta(t, 3000, *metrics[0].LatestFirstTokenMs, 0.001)
	require.NotNil(t, metrics[0].LatestTPS)
	assert.InDelta(t, 30, *metrics[0].LatestTPS, 0.001)
	assert.Equal(t, int64(122), metrics[0].LastUsedTime)

	assert.Equal(t, "model-a", metrics[1].ModelName)
	assert.Equal(t, 2, metrics[1].ChannelId)
	assert.Nil(t, metrics[1].AverageFirstTokenMs)
	require.NotNil(t, metrics[1].AverageTPS)
	assert.InDelta(t, 20, *metrics[1].AverageTPS, 0.001)

	assert.Equal(t, "model-b", metrics[2].ModelName)
	assert.Equal(t, 1, metrics[2].ChannelId)
	assert.Equal(t, 1, metrics[2].FirstTokenSampleCount)
	assert.Equal(t, 0, metrics[2].TPSSampleCount)
	require.NotNil(t, metrics[2].AverageFirstTokenMs)
	assert.InDelta(t, 500, *metrics[2].AverageFirstTokenMs, 0.001)
	assert.Nil(t, metrics[2].AverageTPS)
}

func TestGetChannelMonitorMetricsCachedReusesStableWindowAndReturnsCopies(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		resetChannelMonitorMetricsCache()
	})
	resetChannelMonitorMetricsCache()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "performance-cache.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	useChannelMonitorMinuteTestDB(t, db)
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.Create(&Log{
		ChannelId:        1,
		ModelName:        "model-a",
		Group:            "vip",
		CreatedAt:        900,
		Type:             LogTypeConsume,
		IsStream:         true,
		CompletionTokens: 100,
		UseTime:          10,
		Other:            `{"frt":1000}`,
	}).Error)
	aggregateChannelMonitorMinuteTestRange(t, 0, 960)

	performance, err := GetChannelMonitorPerformanceMetricsCached(context.Background(), 1000, 15)
	require.NoError(t, err)
	require.Len(t, performance, 1)
	success, groups, err := GetChannelMonitorSuccessMetricsCached(context.Background(), 1000, 15)
	require.NoError(t, err)
	require.Len(t, success, 1)
	require.Len(t, groups, 1)

	performance[0].ModelName = "mutated"
	require.NotNil(t, performance[0].AverageFirstTokenMs)
	*performance[0].AverageFirstTokenMs = -1
	success[0].ModelName = "mutated"
	groups[0].Group = "mutated"
	require.NoError(t, db.Create(&Log{
		ChannelId:        1,
		ModelName:        "model-a",
		Group:            "vip",
		CreatedAt:        901,
		Type:             LogTypeConsume,
		IsStream:         true,
		CompletionTokens: 200,
		UseTime:          10,
		Other:            `{"frt":2000}`,
	}).Error)

	performance, err = GetChannelMonitorPerformanceMetricsCached(context.Background(), 1001, 15)
	require.NoError(t, err)
	require.Len(t, performance, 1)
	assert.Equal(t, "model-a", performance[0].ModelName)
	assert.Equal(t, 1, performance[0].SampleCount)
	require.NotNil(t, performance[0].AverageFirstTokenMs)
	assert.InDelta(t, 1000, *performance[0].AverageFirstTokenMs, 0.001)
	success, groups, err = GetChannelMonitorSuccessMetricsCached(context.Background(), 1001, 15)
	require.NoError(t, err)
	assert.Equal(t, "model-a", success[0].ModelName)
	assert.Equal(t, int64(1), success[0].ActualSuccessCount)
	assert.Equal(t, "vip", groups[0].Group)
	aggregateChannelMonitorMinuteTestRange(t, 0, 1020)

	performance, err = GetChannelMonitorPerformanceMetricsCached(context.Background(), 1020, 15)
	require.NoError(t, err)
	require.Len(t, performance, 1)
	assert.Equal(t, 2, performance[0].SampleCount)
	success, _, err = GetChannelMonitorSuccessMetricsCached(context.Background(), 1020, 15)
	require.NoError(t, err)
	assert.Equal(t, int64(2), success[0].ActualSuccessCount)
}

func TestGetChannelMonitorPerformanceMetricsCachedCoalescesConcurrentQueries(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		resetChannelMonitorMetricsCache()
	})
	resetChannelMonitorMetricsCache()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "performance-singleflight.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	useChannelMonitorMinuteTestDB(t, db)
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.Create(&Log{
		ChannelId:        1,
		ModelName:        "model-a",
		CreatedAt:        900,
		Type:             LogTypeConsume,
		IsStream:         true,
		CompletionTokens: 100,
		UseTime:          10,
	}).Error)
	aggregateChannelMonitorMinuteTestRange(t, 0, 960)

	queryStarted := make(chan struct{})
	releaseQuery := make(chan struct{})
	var queryCount atomic.Int32
	var blockFirstQuery sync.Once
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register(
		"channel_monitor_performance_cache_test",
		func(*gorm.DB) {
			queryCount.Add(1)
			blockFirstQuery.Do(func() {
				close(queryStarted)
				<-releaseQuery
			})
		},
	))

	const callers = 8
	results := make(chan error, callers)
	go func() {
		_, callErr := GetChannelMonitorPerformanceMetricsCached(context.Background(), 1000, 15)
		results <- callErr
	}()
	<-queryStarted
	for range callers - 1 {
		go func() {
			_, callErr := GetChannelMonitorPerformanceMetricsCached(context.Background(), 1000, 15)
			results <- callErr
		}()
	}
	close(releaseQuery)
	for range callers {
		require.NoError(t, <-results)
	}
	assert.Equal(t, int32(3), queryCount.Load())
}

func TestGetChannelMonitorPerformanceMetricsCachedIsolatesLogDatabases(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
		resetChannelMonitorMetricsCache()
	})
	resetChannelMonitorMetricsCache()
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	firstDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "performance-cache-first.db")), &gorm.Config{})
	require.NoError(t, err)
	firstSQLDB, err := firstDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, firstSQLDB.Close())
	})
	require.NoError(t, firstDB.AutoMigrate(&Log{}))
	useChannelMonitorMinuteTestDB(t, firstDB)
	require.NoError(t, firstDB.Create(&Log{
		ChannelId: 1,
		ModelName: "first-db-model",
		CreatedAt: 900,
		Type:      LogTypeConsume,
		IsStream:  true,
		Other:     `{"frt":1000}`,
	}).Error)
	LOG_DB = firstDB
	aggregateChannelMonitorMinuteTestRange(t, 0, 960)
	metrics, err := GetChannelMonitorPerformanceMetricsCached(context.Background(), 1000, 15)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	assert.Equal(t, "first-db-model", metrics[0].ModelName)

	secondDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "performance-cache-second.db")), &gorm.Config{})
	require.NoError(t, err)
	secondSQLDB, err := secondDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, secondSQLDB.Close())
	})
	require.NoError(t, secondDB.AutoMigrate(&Log{}, &ChannelMonitorMinuteMetric{}))
	DB = secondDB
	require.NoError(t, secondDB.Create(&Log{
		ChannelId: 2,
		ModelName: "second-db-model",
		CreatedAt: 900,
		Type:      LogTypeConsume,
		IsStream:  true,
		Other:     `{"frt":2000}`,
	}).Error)
	LOG_DB = secondDB
	aggregateChannelMonitorMinuteTestRange(t, 0, 960)
	metrics, err = GetChannelMonitorPerformanceMetricsCached(context.Background(), 1000, 15)
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	assert.Equal(t, "second-db-model", metrics[0].ModelName)
}

func TestGetChannelMonitorStabilityMetricsCountsSuccessesAndRetryFailures(t *testing.T) {
	originalLogDB := LOG_DB
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetLogDatabaseType(originalLogDatabaseType)
	})

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stability.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	useChannelMonitorMinuteTestDB(t, db)
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	logs := make([]*Log, 0, 18)
	for range 8 {
		logs = append(logs, &Log{ChannelId: 1, ModelName: "model-a", CreatedAt: 101, Type: LogTypeConsume})
	}
	logs = append(logs,
		&Log{ChannelId: 1, ModelName: "model-a", CreatedAt: 102, Type: LogTypeError},
		&Log{ChannelId: 1, ModelName: "model-a", CreatedAt: 181, Type: LogTypeError, IsRetryAttempt: true},
	)
	for range 2 {
		logs = append(logs, &Log{ChannelId: 2, ModelName: "model-a", CreatedAt: 104, Type: LogTypeConsume})
	}
	for range 3 {
		logs = append(logs, &Log{ChannelId: 2, ModelName: "model-a", CreatedAt: 105, Type: LogTypeError})
	}
	logs = append(logs,
		&Log{ChannelId: 1, ModelName: "model-a", CreatedAt: 59, Type: LogTypeError},
		&Log{ChannelId: 1, ModelName: "model-a", CreatedAt: 106, Type: LogTypeManage},
		&Log{ChannelId: 0, ModelName: "model-a", CreatedAt: 107, Type: LogTypeError},
	)
	require.NoError(t, db.Create(&logs).Error)
	aggregateChannelMonitorMinuteTestRange(t, 0, 240)

	metrics, err := GetChannelMonitorStabilityMetrics(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, metrics, 2)

	assert.Equal(t, 1, metrics[0].ChannelId)
	assert.Equal(t, int64(8), metrics[0].SuccessCount)
	assert.Equal(t, int64(2), metrics[0].FailureCount)
	assert.Equal(t, int64(10), metrics[0].SampleCount)
	assert.InDelta(t, 0.8, metrics[0].SuccessRate, 0.0001)

	assert.Equal(t, 2, metrics[1].ChannelId)
	assert.Equal(t, int64(2), metrics[1].SuccessCount)
	assert.Equal(t, int64(3), metrics[1].FailureCount)
	assert.Equal(t, int64(5), metrics[1].SampleCount)
	assert.InDelta(t, 0.4, metrics[1].SuccessRate, 0.0001)

	probeMetric, err := GetChannelMonitorStabilityMetric(context.Background(), 180, ChannelMonitorSuccessFilter{
		ChannelId: 1,
		ModelName: "model-a",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), probeMetric.SuccessCount)
	assert.Equal(t, int64(1), probeMetric.FailureCount)
	assert.Equal(t, int64(1), probeMetric.SampleCount)
	assert.Zero(t, probeMetric.SuccessRate)
}
