package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelDailyCostOutboxRecordsPreserveCommittedProjectionIdentity(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	ctx := context.Background()
	event := ChannelDailyCostDelta{EventId: " cost-projection-id ", ChannelId: 7, OccurredAt: time.Now().Unix(), CostNanoCNY: 125, SettledDelta: 1}
	records, inserted, err := StoreChannelDailyCostOutboxEventsWithRecords(ctx, []ChannelDailyCostDelta{event, event})
	require.NoError(t, err)
	assert.EqualValues(t, 1, inserted)
	require.Len(t, records, 2)
	assert.Positive(t, records[0].Id)
	assert.Equal(t, records[0].Id, records[1].Id, "duplicate delivery must keep the same projection version")
	assert.Equal(t, "cost-projection-id", records[0].EventId)
	assert.Equal(t, " cost-projection-id ", event.EventId, "normalization must not change the caller's event")
	var stored ChannelDailyCostOutbox
	require.NoError(t, db.First(&stored, records[0].Id).Error)
	assert.Equal(t, stored, records[0], "returned records must already be committed")

	require.NoError(t, MarkChannelDailyCostProjectionsApplied(ctx, []int64{stored.Id}, 123))
	replayed, inserted, err := StoreChannelDailyCostOutboxEventsWithRecords(ctx, []ChannelDailyCostDelta{event})
	require.NoError(t, err)
	assert.Zero(t, inserted)
	require.Len(t, replayed, 1)
	assert.Equal(t, stored.Id, replayed[0].Id)
	assert.EqualValues(t, 123, replayed[0].RedisProjectedAt, "a replay must return the durable delivery state")
}

func TestChannelDailyCostOutboxRecordsNeverExposeRolledBackBatch(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	ctx := context.Background()
	existing := ChannelDailyCostDelta{EventId: "projection-existing", ChannelId: 7, OccurredAt: time.Now().Unix(), CostNanoCNY: 100, SettledDelta: 1}
	require.NoError(t, StoreChannelDailyCostOutboxEvents(ctx, []ChannelDailyCostDelta{existing}))
	first := existing
	first.EventId = "projection-rolled-back"
	collision := existing
	collision.CostNanoCNY = 999
	records, inserted, err := StoreChannelDailyCostOutboxEventsWithRecords(ctx, []ChannelDailyCostDelta{first, collision})
	require.ErrorIs(t, err, ErrChannelDailyCostOutboxEventIDCollision)
	assert.Nil(t, records, "uncommitted records must never reach a Redis projection")
	assert.Zero(t, inserted)
	var rows []ChannelDailyCostOutbox
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, existing.EventId, rows[0].EventId)
	assert.Equal(t, existing.CostNanoCNY, rows[0].CostNanoCNY)
}
