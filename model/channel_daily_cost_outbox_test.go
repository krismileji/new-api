package model

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func setupChannelDailyCostOutboxTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cost-outbox.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}, &ChannelDailyCostOutbox{}))
	previous := DB
	DB = db
	t.Cleanup(func() {
		DB = previous
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestChannelDailyCostOutboxAppliesDuplicateEventExactlyOnce(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(17, "sk-outbox")
	event := ChannelDailyCostDelta{
		EventId: "request-cost-1", ChannelId: 9, OccurredAt: 1_700_000_000,
		CostNanoCNY: 120, SettledDelta: 1, APIKeyId: 17, APIKeyName: "生产 Key",
		KeyFingerprint: fingerprint, KeyDisplay: display,
	}

	inserted, err := StoreChannelDailyCostOutboxEventsWithResult(context.Background(), []ChannelDailyCostDelta{event, event})
	require.NoError(t, err)
	assert.Equal(t, int64(1), inserted)
	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-a", 100, 100, time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, ApplyClaimedChannelDailyCostOutboxEvents(context.Background(), "worker-a", []int64{claimed[0].Id}, 101))

	inserted, err = StoreChannelDailyCostOutboxEventsWithResult(context.Background(), []ChannelDailyCostDelta{event})
	require.NoError(t, err)
	assert.Zero(t, inserted)
	claimed, err = ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-b", 200, 200, time.Minute, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	var total ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", 9).First(&total).Error)
	assert.Equal(t, int64(120), total.CostNanoCNY)
	assert.Equal(t, int64(1), total.SettledCount)
	var keyTotal ChannelDailyAPIKeyCost
	require.NoError(t, db.Where("key_fingerprint = ?", fingerprint).First(&keyTotal).Error)
	assert.Equal(t, int64(120), keyTotal.CostNanoCNY)
	assert.Equal(t, int64(1), keyTotal.SettledCount)
}

func TestChannelDailyCostOutboxOperationsReportUnavailableDatabase(t *testing.T) {
	previous := DB
	DB = nil
	t.Cleanup(func() { DB = previous })

	_, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker", 1, 1, time.Second, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database is unavailable")
	require.Error(t, ApplyClaimedChannelDailyCostOutboxEvents(context.Background(), "worker", []int64{1}, 2))
	require.Error(t, FailClaimedChannelDailyCostOutboxEvents(context.Background(), "worker", []int64{1}, 3, assert.AnError))
	_, err = DeleteProcessedChannelDailyCostOutboxEvents(context.Background(), 4, 1)
	require.Error(t, err)
}

func TestChannelDailyCostOutboxRejectsEventIDCollision(t *testing.T) {
	setupChannelDailyCostOutboxTestDB(t)
	first := ChannelDailyCostDelta{EventId: "collision", ChannelId: 1, OccurredAt: 100, CostNanoCNY: 10, SettledDelta: 1}
	second := first
	second.CostNanoCNY = 11

	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{first}))
	err := StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{second})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChannelDailyCostOutboxEventIDCollision)
	assert.Contains(t, err.Error(), "collision")
}

func TestChannelDailyCostDeltaValidatesUnidentifiedAPIKeyMetadata(t *testing.T) {
	base := ChannelDailyCostDelta{
		EventId: "unidentified-key-metadata", ChannelId: 1, OccurredAt: 100,
		CostNanoCNY: 10, SettledDelta: 1,
	}

	for _, test := range []struct {
		name   string
		mutate func(*ChannelDailyCostDelta)
	}{
		{name: "negative id", mutate: func(delta *ChannelDailyCostDelta) { delta.APIKeyId = -1 }},
		{name: "oversized name", mutate: func(delta *ChannelDailyCostDelta) { delta.APIKeyName = strings.Repeat("n", 256) }},
		{name: "oversized display", mutate: func(delta *ChannelDailyCostDelta) { delta.KeyDisplay = strings.Repeat("d", 65) }},
		{name: "overflowing timestamp", mutate: func(delta *ChannelDailyCostDelta) { delta.OccurredAt = math.MaxInt64 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			delta := base
			test.mutate(&delta)
			require.Error(t, ValidateChannelDailyCostDelta(delta))
		})
	}
}

func TestChannelDailyCostOutboxLeaseCanBeRecoveredAfterExpiry(t *testing.T) {
	setupChannelDailyCostOutboxTestDB(t)
	event := ChannelDailyCostDelta{EventId: "lease-recovery", ChannelId: 2, OccurredAt: 100, CostNanoCNY: 10, SettledDelta: 1}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event}))

	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-a", 100, 100, 10*time.Second, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	claimed, err = ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-b", 105, 105, 10*time.Second, 1)
	require.NoError(t, err)
	assert.Empty(t, claimed)

	claimed, err = ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-b", 111, 111, 10*time.Second, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, ApplyClaimedChannelDailyCostOutboxEvents(context.Background(), "worker-b", []int64{claimed[0].Id}, 112))

	stats, err := GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Zero(t, stats.PendingCount)
}

func TestChannelDailyCostOutboxLeaseCanBeRecoveredAtExpiryBoundary(t *testing.T) {
	setupChannelDailyCostOutboxTestDB(t)
	event := ChannelDailyCostDelta{EventId: "lease-boundary", ChannelId: 2, OccurredAt: 100, CostNanoCNY: 10, SettledDelta: 1}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event}))

	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-a", 100, 100, 10*time.Second, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	claimed, err = ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-b", 110, 110, 10*time.Second, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, "worker-b", claimed[0].LeaseOwner)
}

func TestChannelDailyCostOutboxFinalizeResultDoesNotCountLostLease(t *testing.T) {
	setupChannelDailyCostOutboxTestDB(t)
	event := ChannelDailyCostDelta{EventId: "finalize-result", ChannelId: 22, OccurredAt: 1_700_000_000, CostNanoCNY: 10, SettledDelta: 1}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event}))
	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-a", 100, 100, time.Minute, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	applied, err := ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "worker-a", []int64{claimed[0].Id}, 101)
	require.NoError(t, err)
	assert.Equal(t, int64(1), applied)
	applied, err = ApplyClaimedChannelDailyCostOutboxEventsWithResult(context.Background(), "worker-a", []int64{claimed[0].Id}, 102)
	require.NoError(t, err)
	assert.Zero(t, applied)
}

func TestChannelDailyCostOutboxRejectsOverflowingLeaseTimestamp(t *testing.T) {
	setupChannelDailyCostOutboxTestDB(t)
	event := ChannelDailyCostDelta{EventId: "lease-overflow", ChannelId: 23, OccurredAt: 1_700_000_000, CostNanoCNY: 10, SettledDelta: 1}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event}))
	_, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-overflow", math.MaxInt64-1, math.MaxInt64-1, 10*time.Second, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lease timestamp")
}

func TestChannelDailyCostOutboxRejectsExhaustedAttemptCounter(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	event := ChannelDailyCostDelta{EventId: "attempt-exhausted", ChannelId: 24, OccurredAt: 1_700_000_000, CostNanoCNY: 10, SettledDelta: 1}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event}))
	require.NoError(t, db.Model(&ChannelDailyCostOutbox{}).Where("event_id = ?", event.EventId).Update("attempt_count", math.MaxInt64).Error)
	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-exhausted", 100, 100, time.Minute, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attempt count")
	assert.Empty(t, claimed)
}

func TestChannelDailyCostOutboxRefusesNegativeExistingLedger(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	dayStart := ChannelDailyCostDayStart(1_700_000_000)
	seed := ChannelDailyCost{
		ChannelId: 25, DayStart: dayStart, CostNanoCNY: -1, CreatedAt: 1_700_000_000, UpdatedAt: 1_700_000_000,
	}
	require.NoError(t, db.Create(&seed).Error)
	event := ChannelDailyCostDelta{EventId: "negative-ledger", ChannelId: 25, OccurredAt: 1_700_000_000, CostNanoCNY: 10, SettledDelta: 1}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event}))
	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "negative-ledger-worker", 100, 100, time.Minute, 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	err = ApplyClaimedChannelDailyCostOutboxEvents(context.Background(), "negative-ledger-worker", []int64{claimed[0].Id}, 101)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChannelDailyCostLedgerOverflow)
	var row ChannelDailyCostOutbox
	require.NoError(t, db.Where("event_id = ?", event.EventId).First(&row).Error)
	assert.Zero(t, row.ProcessedAt)
}

func TestChannelDailyCostOutboxStatsSaturateRetryGauge(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	rows := []ChannelDailyCostOutbox{
		{EventId: "stats-max", ChannelId: 26, OccurredAt: 1_700_000_000, CostNanoCNY: 1, SettledDelta: 1, AttemptCount: math.MaxInt64, CreatedAt: 10, UpdatedAt: 10},
		{EventId: "stats-one", ChannelId: 27, OccurredAt: 1_700_000_000, CostNanoCNY: 1, SettledDelta: 1, AttemptCount: 1, CreatedAt: 11, UpdatedAt: 11},
	}
	require.NoError(t, db.Create(&rows).Error)
	stats, err := GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.PendingCount)
	assert.Equal(t, int64(math.MaxInt64), stats.RetryCount)
	assert.Equal(t, int64(10), stats.OldestPending)
}

func TestChannelDailyCostOutboxApplyRollsBackWholeBatch(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	events := []ChannelDailyCostDelta{
		{EventId: "overflow-a", ChannelId: 3, OccurredAt: 100, CostNanoCNY: math.MaxInt64, SettledDelta: 1},
		{EventId: "overflow-b", ChannelId: 3, OccurredAt: 101, CostNanoCNY: 1, SettledDelta: 1},
	}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), events))
	claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "worker-a", 100, 100, time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	ids := []int64{claimed[0].Id, claimed[1].Id}

	err = ApplyClaimedChannelDailyCostOutboxEvents(context.Background(), "worker-a", ids, 101)
	require.Error(t, err)
	require.NoError(t, FailClaimedChannelDailyCostOutboxEvents(context.Background(), "worker-a", ids, 200, err))

	var ledgerCount int64
	require.NoError(t, db.Model(&ChannelDailyCost{}).Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
	stats, statsErr := GetChannelDailyCostOutboxStats(context.Background())
	require.NoError(t, statsErr)
	assert.Equal(t, int64(2), stats.PendingCount)
	assert.Equal(t, int64(2), stats.RetryCount)
}

func TestChannelDailyCostOutboxCleanupIsBoundedAndPreservesPendingRows(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	rows := []ChannelDailyCostOutbox{
		{EventId: "old-processed-a", ChannelId: 1, ProcessedAt: 100, CreatedAt: 10, UpdatedAt: 100},
		{EventId: "old-processed-b", ChannelId: 1, ProcessedAt: 101, CreatedAt: 11, UpdatedAt: 101},
		{EventId: "recent-processed", ChannelId: 1, ProcessedAt: 300, CreatedAt: 12, UpdatedAt: 300},
		{EventId: "pending", ChannelId: 1, ProcessedAt: 0, CreatedAt: 13, UpdatedAt: 13},
	}
	require.NoError(t, db.Create(&rows).Error)

	deleted, err := DeleteProcessedChannelDailyCostOutboxEvents(context.Background(), 200, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var remaining []ChannelDailyCostOutbox
	require.NoError(t, db.Order("id ASC").Find(&remaining).Error)
	require.Len(t, remaining, 3)
	assert.Equal(t, []string{"old-processed-b", "recent-processed", "pending"}, []string{
		remaining[0].EventId, remaining[1].EventId, remaining[2].EventId,
	})

	deleted, err = DeleteProcessedChannelDailyCostOutboxEvents(context.Background(), 200, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)
	require.NoError(t, db.Order("id ASC").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	assert.Equal(t, "recent-processed", remaining[0].EventId)
	assert.Equal(t, "pending", remaining[1].EventId)
}

func TestChannelDailyCostOutboxConfiguredDatabases(t *testing.T) {
	tests := []struct {
		name         string
		env          string
		databaseType common.DatabaseType
		dialector    func(string) gorm.Dialector
	}{
		{name: "mysql57", env: "TEST_MYSQL_DSN", databaseType: common.DatabaseTypeMySQL, dialector: func(dsn string) gorm.Dialector {
			return mysql.Open(dsn)
		}},
		{name: "postgres96", env: "TEST_POSTGRES_DSN", databaseType: common.DatabaseTypePostgreSQL, dialector: func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := strings.TrimSpace(os.Getenv(test.env))
			if dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			if test.databaseType == common.DatabaseTypeMySQL {
				config, parseErr := mysqlDriver.ParseDSN(dsn)
				require.NoError(t, parseErr)
				config.ClientFoundRows = true
				dsn = config.FormatDSN()
			}
			db, err := gorm.Open(test.dialector(dsn), &gorm.Config{NamingStrategy: schema.NamingStrategy{
				TablePrefix: fmt.Sprintf("cost_outbox_%x_", time.Now().UnixNano()),
			}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(4)
			previousDB := DB
			previousDatabaseType := common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(test.databaseType)
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&ChannelDailyCostOutbox{}, &ChannelDailyAPIKeyCost{}, &ChannelDailyCost{})
				DB = previousDB
				common.SetMainDatabaseType(previousDatabaseType)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}, &ChannelDailyCostOutbox{}))

			event := ChannelDailyCostDelta{EventId: "configured-db-event", ChannelId: 901, OccurredAt: 1_700_000_000, CostNanoCNY: 250, SettledDelta: 1}
			require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event, event}))
			now := time.Now().Unix()
			claimed, err := ClaimChannelDailyCostOutboxEvents(context.Background(), "configured-db-worker", now, now, time.Minute, 10)
			require.NoError(t, err)
			require.Len(t, claimed, 1)
			require.NoError(t, ApplyClaimedChannelDailyCostOutboxEvents(context.Background(), "configured-db-worker", []int64{claimed[0].Id}, time.Now().Unix()))
			require.NoError(t, StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{event}))
			collision := event
			collision.CostNanoCNY++
			collisionErr := StoreChannelDailyCostOutboxEvents(context.Background(), []ChannelDailyCostDelta{collision})
			require.ErrorIs(t, collisionErr, ErrChannelDailyCostOutboxEventIDCollision)

			var ledger ChannelDailyCost
			require.NoError(t, db.Where("channel_id = ?", event.ChannelId).First(&ledger).Error)
			assert.Equal(t, event.CostNanoCNY, ledger.CostNanoCNY)
			assert.Equal(t, int64(1), ledger.SettledCount)
		})
	}
}
