package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelTaskCostEventCorrectionsAreIdempotentAndStayOnSubmissionDay(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "task-cost-event.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelDailyAPIKeyCost{}, &ChannelTaskCostEvent{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	submittedAt := time.Date(2026, 8, 15, 15, 59, 59, 0, time.UTC).Unix()
	correctedAt := submittedAt + channelDailyCostDaySeconds + 10
	fingerprint, display := ChannelDailyCostAPIKeyIdentityForToken(11, "sk-task-cost")
	input := ChannelTaskCostEventInput{
		CostEventId:    "task:task-cost-1",
		ChannelId:      7,
		OccurredAt:     submittedAt,
		APIKeyId:       11,
		APIKeyName:     "任务 Key",
		KeyFingerprint: fingerprint,
		KeyDisplay:     display,
		InitialQuota:   200,
		CostNanoCNY:    100,
	}

	stored, err := RegisterChannelTaskCostEvent(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, int64(100), stored)
	stored, err = RegisterChannelTaskCostEvent(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, int64(100), stored)
	require.NoError(t, AddChannelDailyCost(context.Background(), 7, submittedAt, 300, 1, 0))

	stored, err = SetChannelTaskCostEventQuota(context.Background(), input.CostEventId, 50, correctedAt)
	require.NoError(t, err)
	assert.Equal(t, int64(25), stored)
	stored, err = SetChannelTaskCostEventQuota(context.Background(), input.CostEventId, 50, correctedAt+1)
	require.NoError(t, err)
	assert.Equal(t, int64(25), stored)
	stored, err = RegisterChannelTaskCostEvent(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, int64(25), stored)

	var total ChannelDailyCost
	require.NoError(t, db.First(&total).Error)
	assert.Equal(t, ChannelDailyCostDayStart(submittedAt), total.DayStart)
	assert.Equal(t, int64(325), total.CostNanoCNY)
	assert.Equal(t, int64(2), total.SettledCount)
	var keyCost ChannelDailyAPIKeyCost
	require.NoError(t, db.First(&keyCost).Error)
	assert.Equal(t, int64(25), keyCost.CostNanoCNY)
	assert.Equal(t, int64(1), keyCost.SettledCount)

	stored, err = SetChannelTaskCostEventCost(context.Background(), input.CostEventId, 0, correctedAt+2)
	require.NoError(t, err)
	assert.Zero(t, stored)
	stored, err = SetChannelTaskCostEventCost(context.Background(), input.CostEventId, 0, correctedAt+3)
	require.NoError(t, err)
	assert.Zero(t, stored)

	require.NoError(t, db.First(&total, total.Id).Error)
	assert.Equal(t, int64(300), total.CostNanoCNY)
	assert.Equal(t, int64(2), total.SettledCount)
	require.NoError(t, db.First(&keyCost, keyCost.Id).Error)
	assert.Zero(t, keyCost.CostNanoCNY)
	assert.Equal(t, int64(1), keyCost.SettledCount)
	var event ChannelTaskCostEvent
	require.NoError(t, db.First(&event).Error)
	assert.NotEmpty(t, event.RegistrationToken)
	assert.Zero(t, event.CostNanoCNY)
	assert.Equal(t, int64(100), event.InitialCostNanoCNY)
	assert.Equal(t, int64(200), event.InitialQuota)
}

func TestChannelTaskCostEventRejectsIdentityReuseAndUnsafeDecrease(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "task-cost-event-guard.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelTaskCostEvent{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	when := time.Date(2026, 8, 15, 4, 0, 0, 0, time.UTC).Unix()
	input := ChannelTaskCostEventInput{
		CostEventId:  "task:guard",
		ChannelId:    3,
		OccurredAt:   when,
		InitialQuota: 100,
		CostNanoCNY:  80,
	}
	_, err = RegisterChannelTaskCostEvent(context.Background(), input)
	require.NoError(t, err)

	reused := input
	reused.ChannelId = 4
	_, err = RegisterChannelTaskCostEvent(context.Background(), reused)
	require.Error(t, err)

	require.NoError(t, db.Model(&ChannelDailyCost{}).Where("channel_id = ?", input.ChannelId).Update("cost_nano_cny", 40).Error)
	_, err = SetChannelTaskCostEventCost(context.Background(), input.CostEventId, 0, when+1)
	require.Error(t, err)

	var event ChannelTaskCostEvent
	require.NoError(t, db.First(&event).Error)
	assert.Equal(t, int64(80), event.CostNanoCNY)
	var total ChannelDailyCost
	require.NoError(t, db.First(&total).Error)
	assert.Equal(t, int64(40), total.CostNanoCNY)
}

func TestChannelTaskCostEventRejectsStaleUpdatedAt(t *testing.T) {
	originalDB := DB
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "task-cost-event-monotonic.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&ChannelDailyCost{}, &ChannelTaskCostEvent{}))
	t.Cleanup(func() {
		DB = originalDB
		require.NoError(t, sqlDB.Close())
	})

	when := time.Date(2026, 8, 15, 4, 0, 0, 0, time.UTC).Unix()
	input := ChannelTaskCostEventInput{
		CostEventId:  "task:monotonic",
		ChannelId:    9,
		OccurredAt:   when,
		InitialQuota: 100,
		CostNanoCNY:  80,
	}
	_, err = RegisterChannelTaskCostEvent(context.Background(), input)
	require.NoError(t, err)

	newer := when + 2_000
	cost, err := SetChannelTaskCostEventCost(context.Background(), input.CostEventId, 40, newer)
	require.NoError(t, err)
	assert.Equal(t, int64(40), cost)
	// A same-value retry still advances the durable version, so an older
	// callback cannot subsequently overwrite it.
	latest := when + 3_000
	cost, err = SetChannelTaskCostEventCost(context.Background(), input.CostEventId, 40, latest)
	require.NoError(t, err)
	assert.Equal(t, int64(40), cost)
	_, err = SetChannelTaskCostEventCost(context.Background(), input.CostEventId, 20, when+1_000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stale")

	var event ChannelTaskCostEvent
	require.NoError(t, db.First(&event).Error)
	assert.Equal(t, int64(40), event.CostNanoCNY)
	assert.Equal(t, latest, event.UpdatedAt)
}
