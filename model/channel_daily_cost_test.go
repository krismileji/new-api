package model

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestChannelDailyCostUpsertUsesBeijingDayAndOneRowPerChannel(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})
	require.Error(t, AddChannelDailyCost(context.Background(), 1, time.Now().Unix(), -1, 1, 0))
	require.Error(t, AddChannelDailyCostWithProbe(context.Background(), 1, time.Now().Unix(), 10, 11, 1, 0))
	require.Error(t, AddChannelDailyCostWithModelDetection(context.Background(), db, 1, time.Now().Unix(), 10, 11, 1, 0))

	beforeMidnight := time.Date(2026, 7, 21, 15, 59, 59, 0, time.UTC).Unix()
	afterMidnight := beforeMidnight + 1
	require.NoError(t, AddChannelDailyCostWithProbe(context.Background(), 1, beforeMidnight, 100, 40, 1, 0))
	require.NoError(t, AddChannelDailyCostWithModelDetection(context.Background(), db, 1, beforeMidnight, 60, 60, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, beforeMidnight-60, 25, 1, 1))
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, afterMidnight, 50, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 2, afterMidnight, 75, 1, 0))

	var rows []ChannelDailyCost
	require.NoError(t, db.Order("day_start ASC, channel_id ASC").Find(&rows).Error)
	require.Len(t, rows, 3)
	assert.Equal(t, ChannelDailyCostDayStart(beforeMidnight), rows[0].DayStart)
	assert.Equal(t, int64(185), rows[0].CostNanoCNY)
	assert.Equal(t, int64(40), rows[0].ProbeCostNanoCNY)
	assert.Equal(t, int64(60), rows[0].ModelDetectionCostNanoCNY)
	assert.Equal(t, int64(3), rows[0].SettledCount)
	assert.Equal(t, int64(1), rows[0].UnresolvedCount)
	assert.Equal(t, ChannelDailyCostDayStart(afterMidnight), rows[1].DayStart)
	assert.Equal(t, 1, rows[1].ChannelId)
	assert.Equal(t, 2, rows[2].ChannelId)
}

func TestChannelDailyCostConcurrentFirstWriteConfiguredMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}

	prefix := fmt.Sprintf("mcdc%x_", time.Now().UnixNano())
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	const writes = 8
	sqlDB.SetMaxOpenConns(writes)
	t.Cleanup(func() {
		require.NoError(t, db.Migrator().DropTable(&ChannelDailyCost{}))
		require.NoError(t, sqlDB.Close())
	})

	start := make(chan struct{})
	errs := make(chan error, writes)
	var wait sync.WaitGroup
	for range writes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs <- AddChannelDailyCostWithModelDetection(context.Background(), db, 501, 1_786_896_000, 10, 10, 1, 0)
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for writeErr := range errs {
		require.NoError(t, writeErr)
	}

	var stored ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", 501).First(&stored).Error)
	assert.Equal(t, int64(writes*10), stored.CostNanoCNY)
	assert.Equal(t, int64(writes*10), stored.ModelDetectionCostNanoCNY)
	assert.Equal(t, int64(writes), stored.SettledCount)
	assert.Zero(t, stored.UnresolvedCount)
}

func TestGetChannelDailyCostDayTotalsAggregatesOnlyRequestedRange(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost-totals.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	dayOne := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC).Unix()
	dayTwo := dayOne + channelDailyCostDaySeconds
	dayThree := dayTwo + channelDailyCostDaySeconds
	require.NoError(t, AddChannelDailyCostWithProbe(context.Background(), 1, dayOne, 100, 25, 1, 0))
	require.NoError(t, AddChannelDailyCostWithProbe(context.Background(), 2, dayOne, 50, 10, 1, 1))
	require.NoError(t, AddChannelDailyCostWithModelDetection(context.Background(), db, 2, dayOne, 20, 20, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, dayTwo, 200, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, dayThree, 300, 1, 0))

	totals, err := GetChannelDailyCostDayTotals(context.Background(), dayOne, dayThree, 0)
	require.NoError(t, err)
	require.Len(t, totals, 2)
	assert.Equal(t, dayOne, totals[0].DayStart)
	assert.Equal(t, int64(170), totals[0].CostNanoCNY)
	assert.Equal(t, int64(35), totals[0].ProbeCostNanoCNY)
	assert.Equal(t, int64(20), totals[0].ModelDetectionCostNanoCNY)
	assert.Equal(t, int64(3), totals[0].SettledCount)
	assert.Equal(t, int64(1), totals[0].UnresolvedCount)
	assert.Equal(t, dayTwo, totals[1].DayStart)
	assert.Equal(t, int64(200), totals[1].CostNanoCNY)
	assert.Equal(t, int64(1), totals[1].SettledCount)

	channelTotals, err := GetChannelDailyCostDayTotals(context.Background(), dayOne, dayThree, 2)
	require.NoError(t, err)
	require.Len(t, channelTotals, 1)
	assert.Equal(t, int64(70), channelTotals[0].CostNanoCNY)
	assert.Equal(t, int64(20), channelTotals[0].ModelDetectionCostNanoCNY)

	pageTotals, err := GetChannelDailyCostDayTotalsPage(context.Background(), dayOne, dayThree, 0, 1)
	require.NoError(t, err)
	require.Len(t, pageTotals, 1)
	assert.Equal(t, dayOne, pageTotals[0].DayStart)
	offsetTotals, err := GetChannelDailyCostDayTotalsPageWithOffset(context.Background(), dayOne, dayThree, 0, 1, 1)
	require.NoError(t, err)
	require.Len(t, offsetTotals, 1)
	assert.Equal(t, dayTwo, offsetTotals[0].DayStart)
}

func TestGetChannelDailyCostChannelTotalsWithDetailReusesRangeAggregation(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost-channel-totals.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	dayOne := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC).Unix()
	dayTwo := dayOne + channelDailyCostDaySeconds
	require.NoError(t, AddChannelDailyCostWithProbe(context.Background(), 1, dayOne, 100, 20, 1, 0))
	require.NoError(t, AddChannelDailyCostWithModelDetection(context.Background(), db, 1, dayTwo, 70, 30, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 2, dayOne, 50, 1, 1))

	totals, err := GetChannelDailyCostChannelTotalsWithDetail(
		context.Background(), dayOne, dayTwo+channelDailyCostDaySeconds, 0, dayTwo,
	)
	require.NoError(t, err)
	require.Len(t, totals, 2)
	assert.Equal(t, 1, totals[0].ChannelId)
	assert.Equal(t, int64(170), totals[0].CostNanoCNY)
	assert.Equal(t, int64(70), totals[0].DetailCostNanoCNY)
	assert.Equal(t, int64(0), totals[0].DetailProbeCostNanoCNY)
	assert.Equal(t, int64(30), totals[0].DetailModelDetectionCostNanoCNY)
	assert.Equal(t, int64(1), totals[0].DetailSettledCount)
	assert.Equal(t, int64(0), totals[0].DetailUnresolvedCount)
	assert.Equal(t, 2, totals[1].ChannelId)
	assert.Zero(t, totals[1].DetailCostNanoCNY)
}

func TestChannelDailyCostRejectsCumulativeOverflowWithoutChangingStoredTotal(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost-overflow.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	occurredAt := int64(1_750_000_000)
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, occurredAt, math.MaxInt64, 1, 0))

	err = AddChannelDailyCost(context.Background(), 1, occurredAt+1, 1, 1, 0)
	require.ErrorContains(t, err, "超过 int64 范围")

	var stored ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", 1).First(&stored).Error)
	assert.Equal(t, int64(math.MaxInt64), stored.CostNanoCNY)
	assert.Equal(t, int64(1), stored.SettledCount)
}

func TestGetChannelDailyCostDayTotalsRejectsCrossChannelOverflow(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost-total-overflow.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	occurredAt := int64(1_750_000_000)
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, occurredAt, math.MaxInt64, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 2, occurredAt, 1, 1, 0))

	_, err = GetChannelDailyCostDayTotals(
		context.Background(),
		ChannelDailyCostDayStart(occurredAt),
		ChannelDailyCostDayStart(occurredAt)+channelDailyCostDaySeconds,
		0,
	)
	require.ErrorContains(t, err, "超过 int64 范围")
}

func TestGetChannelDailyCostDeltaSubtractsSameDayBaseline(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost-baseline.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	dayStart := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC).Unix()
	baselineTime := dayStart + 12*60*60
	capturedAt := baselineTime + 60
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, baselineTime-60, 100, 1, 0))
	previous := ChannelDailyCostBaseline{Timestamp: baselineTime, CostNanoCNY: 100}
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, capturedAt, 4, 1, 0))

	current, delta, err := GetChannelDailyCostDelta(context.Background(), 1, capturedAt, &previous)
	require.NoError(t, err)
	assert.Equal(t, capturedAt, current.Timestamp)
	assert.Equal(t, int64(104), current.CostNanoCNY)
	assert.Equal(t, int64(4), delta)
}

func TestGetChannelDailyCostDeltaIncludesLaterDays(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "daily-cost-baseline-days.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	dayOne := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC).Unix()
	dayTwo := dayOne + channelDailyCostDaySeconds
	baselineTime := dayOne + 12*60*60
	capturedAt := dayTwo + 60
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, baselineTime-60, 100, 1, 0))
	previous := ChannelDailyCostBaseline{Timestamp: baselineTime, CostNanoCNY: 100}
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, baselineTime+60, 4, 1, 0))
	require.NoError(t, AddChannelDailyCost(context.Background(), 1, capturedAt, 6, 1, 0))

	current, delta, err := GetChannelDailyCostDelta(context.Background(), 1, capturedAt, &previous)
	require.NoError(t, err)
	assert.Equal(t, int64(6), current.CostNanoCNY)
	assert.Equal(t, int64(10), delta)
}
