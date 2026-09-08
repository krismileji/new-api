package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDailyPersistenceUpdatesOneRowAndAcceptsLateEventsAfterRestore(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}))
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	day := model.ChannelDailyCostDayStart(at)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	first := newChannelMonitorRedisSharedProjectionTestEvent("daily-first", at)
	first.EventSequence = 1001
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{first}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	var stored []model.ChannelMonitorDailySuccessLedger
	require.NoError(t, db.Where("day_start = ?", day).Find(&stored).Error)
	require.Len(t, stored, 1)
	id := stored[0].Id
	assert.Equal(t, int64(1), stored[0].ActualSuccessCount)
	second := first
	second.EventId, second.EventSequence, second.OccurredAt = "daily-second", 1002, at+60
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{second}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	stored = nil
	require.NoError(t, db.Where("day_start = ?", day).Find(&stored).Error)
	require.Len(t, stored, 1)
	assert.Equal(t, id, stored[0].Id)
	assert.Equal(t, int64(2), stored[0].ActualSuccessCount)
	assert.NotEmpty(t, stored[0].AggregateJSON)
	server.FlushAll()
	require.NoError(t, rebuildChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{first}))
	late := first
	late.EventId, late.EventSequence, late.OccurredAt = "daily-late", 1003, at-15
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, []model.ChannelMonitorEvent{late}))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	stored = nil
	require.NoError(t, db.Where("day_start = ?", day).Find(&stored).Error)
	require.Len(t, stored, 1)
	assert.Equal(t, id, stored[0].Id)
	assert.Equal(t, int64(3), stored[0].ActualSuccessCount)
	var checkpoints int64
	require.NoError(t, db.Model(&model.ChannelMonitorDailyCheckpoint{}).Count(&checkpoints).Error)
	assert.Equal(t, int64(1), checkpoints)
	var checkpoint model.ChannelMonitorDailyCheckpoint
	require.NoError(t, db.First(&checkpoint, "day_start = ?", day).Error)
	assert.True(t, checkpoint.CoveragePartial, "losing the stream tail must remain visible after the next daily flush")
}

func TestDailyPersistenceWaitsForHolesAndReplaysRetainedTailWithoutDoubleCounting(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailySuccessLedger{}, &model.ChannelMonitorDailyCheckpoint{}))
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	day := model.ChannelDailyCostDayStart(at)
	projection := NewChannelMonitorRedisSharedProjectionWithClient(client)
	events := []model.ChannelMonitorEvent{
		newChannelMonitorRedisSharedProjectionTestEvent("daily-hole-first", at),
		newChannelMonitorRedisSharedProjectionTestEvent("daily-hole-second", at+1),
		newChannelMonitorRedisSharedProjectionTestEvent("daily-retained-tail", at-60),
	}
	for index := range events {
		id := fmt.Sprintf("%d-%d", at*1000, index)
		sequence, err := channelMonitorRedisEventSequenceFromStreamID(id)
		require.NoError(t, err)
		events[index].EventSequence = sequence
		payload, err := common.Marshal(events[index])
		require.NoError(t, err)
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: ChannelMonitorRedisEventStream, ID: id, Values: map[string]any{
			ChannelMonitorRedisEventFieldEventID: events[index].EventId,
			ChannelMonitorRedisEventFieldPayload: string(payload),
		}}).Err())
	}
	require.NoError(t, client.XGroupCreate(ctx, ChannelMonitorRedisEventStream, ChannelMonitorRedisConsumerGroup, "0").Err())
	_, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: ChannelMonitorRedisConsumerGroup, Consumer: "daily-test", Streams: []string{ChannelMonitorRedisEventStream, ">"}, Count: 2, Block: -1}).Result()
	require.NoError(t, err)
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events[1:2]))
	require.ErrorContains(t, persistChannelMonitorDailyMetrics(ctx, client, day), "未聚合事件")
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events[:1]))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events[2:]))
	require.NoError(t, client.Del(ctx, ChannelMonitorRedisSuccessDayKey(day)).Err())
	require.NoError(t, rebuildChannelMonitorDailyMetrics(ctx, client, day))
	require.NoError(t, projection.WriteChannelMonitorEvents(ctx, events))
	require.NoError(t, persistChannelMonitorDailyMetrics(ctx, client, day))
	var rows []model.ChannelMonitorDailySuccessLedger
	require.NoError(t, db.Where("day_start = ?", day).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(3), rows[0].ActualSuccessCount)
	view, err := queryChannelMonitorRedisDailySuccessWithClient(ctx, client, day, nil)
	require.NoError(t, err)
	assert.False(t, view.CoveragePartial)
}

func TestReliableDailyCostModelDetectionResolvesOnlyOnce(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelDailyCostOutbox{}, &model.ChannelMonitorDailyCostDetail{}))
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	key := ChannelMonitorRedisCostDayKey(model.ChannelDailyCostDayStart(at))
	require.NoError(t, model.AddChannelDailyCostWithModelDetectionAndModel(ctx, nil, 7, at, 0, 0, 0, 1, "model-a"))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	require.NoError(t, model.SettleUnresolvedChannelDailyModelDetectionCostWithModel(ctx, nil, 7, at, 120, "model-a"))
	var rows []model.ChannelDailyCostOutbox
	require.NoError(t, db.Order("id ASC").Find(&rows).Error)
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	assert.Equal(t, "120", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricSettledCost).Val())
	assert.Equal(t, "120", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricDetectionSettledCost).Val())
	assert.Equal(t, "1", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricSettledRequests).Val())
	assert.Equal(t, "0", client.HGet(ctx, key, "global:"+channelMonitorRedisSharedMetricUnresolvedRequests).Val())
}
