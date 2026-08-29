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

func setupChannelMonitorEventOutboxTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-monitor-event-outbox.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorEventOutbox{}))
	previous := DB
	DB = db
	t.Cleanup(func() {
		DB = previous
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestChannelMonitorEventOutboxStoresIdempotentlyAndClaimsForReplay(t *testing.T) {
	setupChannelMonitorEventOutboxTestDB(t)
	ctx := context.Background()
	payload := []byte(`{"event_id":"event-outbox"}`)
	inserted, err := StoreChannelMonitorEventOutbox(ctx, "event-outbox", payload)
	require.NoError(t, err)
	assert.True(t, inserted)
	inserted, err = StoreChannelMonitorEventOutbox(ctx, "event-outbox", payload)
	require.NoError(t, err)
	assert.False(t, inserted)
	_, err = StoreChannelMonitorEventOutbox(ctx, "event-outbox", []byte(`{"event_id":"different"}`))
	assert.ErrorIs(t, err, ErrChannelMonitorEventOutboxEventIDCollision)

	now := time.Now().Unix()
	claimed, err := ClaimChannelMonitorEventOutbox(ctx, "worker-a", now, time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, "event-outbox", claimed[0].EventId)
	require.NoError(t, FailChannelMonitorEventOutbox(ctx, "worker-a", []int64{claimed[0].Id}, now, assert.AnError))
	claimed, err = ClaimChannelMonitorEventOutbox(ctx, "worker-b", now, time.Minute, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, int64(1), claimed[0].AttemptCount)
	require.NoError(t, MarkChannelMonitorEventOutboxProcessed(ctx, "worker-b", []int64{claimed[0].Id}, 101))
	claimed, err = ClaimChannelMonitorEventOutbox(ctx, "worker-c", 102, time.Minute, 10)
	require.NoError(t, err)
	assert.Empty(t, claimed)
}

func TestChannelMonitorEventOutboxClaimSkipsCandidateLostBeforeLeaseUpdate(t *testing.T) {
	db := setupChannelMonitorEventOutboxTestDB(t)
	ctx := context.Background()
	now := time.Now().Unix()
	inserted, err := StoreChannelMonitorEventOutbox(ctx, "event-outbox-race", []byte(`{"event_id":"event-outbox-race"}`))
	require.NoError(t, err)
	require.True(t, inserted)

	callbackName := "test:channel_monitor_event_outbox_lose_claim"
	callbackFired := false
	var callbackErr error
	var callbackRows int64
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if callbackFired || tx.Statement.Table != "channel_monitor_event_outboxes" {
			return
		}
		candidates, ok := tx.Statement.Dest.(*[]ChannelMonitorEventOutbox)
		if !ok || len(*candidates) == 0 {
			return
		}
		callbackFired = true
		result := tx.Session(&gorm.Session{NewDB: true}).Model(&ChannelMonitorEventOutbox{}).
			Where("id = ?", (*candidates)[0].Id).
			Updates(map[string]interface{}{"processed_at": now + 1, "lease_owner": "worker-other", "lease_until": now + 3600})
		callbackRows = result.RowsAffected
		callbackErr = result.Error
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	claimed, err := ClaimChannelMonitorEventOutbox(ctx, "worker-a", now, time.Minute, 10)
	require.NoError(t, err)
	require.NoError(t, callbackErr)
	assert.True(t, callbackFired)
	assert.Equal(t, int64(1), callbackRows)
	assert.Empty(t, claimed, "a candidate whose conditional lease update affected zero rows must not be replayed")

	var stored ChannelMonitorEventOutbox
	require.NoError(t, db.First(&stored).Error)
	assert.Equal(t, "worker-other", stored.LeaseOwner)
}

func TestChannelMonitorEventOutboxRequiresDatabase(t *testing.T) {
	previous := DB
	DB = nil
	t.Cleanup(func() { DB = previous })
	_, err := StoreChannelMonitorEventOutbox(context.Background(), "event", []byte("payload"))
	assert.Error(t, err)
	_, err = ClaimChannelMonitorEventOutbox(context.Background(), "worker", 1, time.Second, 1)
	assert.Error(t, err)
}

func TestChannelMonitorEventOutboxRejectsInvalidFinalizeOwner(t *testing.T) {
	setupChannelMonitorEventOutboxTestDB(t)
	ctx := context.Background()
	err := MarkChannelMonitorEventOutboxProcessed(ctx, "", []int64{1}, 1)
	assert.Error(t, err)
	err = MarkChannelMonitorEventOutboxProcessed(ctx, " ", []int64{1}, 1)
	assert.Error(t, err)
	err = FailChannelMonitorEventOutbox(ctx, "", []int64{1}, 1, assert.AnError)
	assert.Error(t, err)
}
