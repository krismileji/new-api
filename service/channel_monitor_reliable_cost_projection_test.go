package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReliableDailyCostProjectionAdvancesBeforeMinuteLedgerAndReplaysOnce(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelDailyCostOutbox{}, &model.ChannelMonitorDailyCostDetail{}))
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	day := model.ChannelDailyCostDayStart(at)
	delta := model.ChannelDailyCostDelta{
		ChannelId: 7, OccurredAt: at, CostNanoCNY: 2_000_000_000, SettledDelta: 1,
		UserId: 9, UserAttribution: "request", APIKeyId: 11, APIKeyName: "test",
		ModelName: "model-a", SourceKind: "business",
	}
	require.NoError(t, model.AddChannelDailyCostBatch(ctx, []model.ChannelDailyCostDelta{delta}))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	delta.EventId = "reliable-cost-after-snapshot"
	delta.CostNanoCNY = 1_000_000_000
	delta.OccurredAt = at + 1
	require.NoError(t, model.StoreChannelDailyCostOutboxEvents(ctx, []model.ChannelDailyCostDelta{delta}))
	var rows []model.ChannelDailyCostOutbox
	require.NoError(t, db.Order("id ASC").Find(&rows).Error)
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	field := "channel:7:" + channelMonitorRedisSharedMetricSettledCost
	assert.Equal(t, "3000000000", client.HGet(ctx, ChannelMonitorRedisCostDayKey(day), field).Val())
	ledger, err := model.GetChannelDailyCostsForChannel(ctx, day, day+86400, 7)
	require.NoError(t, err)
	require.Len(t, ledger, 1)
	assert.Equal(t, int64(2_000_000_000), ledger[0].CostNanoCNY)
	require.NoError(t, client.Del(ctx, ChannelMonitorRedisCostDayKey(day)+":events").Err())
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	assert.Equal(t, "3000000000", client.HGet(ctx, ChannelMonitorRedisCostDayKey(day), field).Val(), "losing only dedup state must rebuild before replay")
	require.NoError(t, recoverChannelDailyCostOutboxBatch(ctx, "projection-test"))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at+60))
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	assert.Equal(t, "3000000000", client.HGet(ctx, ChannelMonitorRedisCostDayKey(day), field).Val())
	assert.Equal(t, "2", client.HGet(ctx, ChannelMonitorRedisCostDayKey(day), "channel:7:"+channelMonitorRedisSharedMetricSettledRequests).Val())
}

func TestReliableDailyCostRebuildDoesNotWaitForMonitorConsumerLease(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	_, client := useChannelMonitorRedisConsumerTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	require.NoError(t, model.AddChannelDailyCost(ctx, 7, at, 2_000_000_000, 1, 0))
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	require.NoError(t, model.AddChannelDailyCost(ctx, 7, at+10, 1_000_000_000, 1, 0))
	consumer := newChannelMonitorRedisConsumerForTest(t, client, "cost-rebuild", nil, channelMonitorRedisConsumerTestConfig())
	lease, acquired, err := consumer.acquireLease(ctx)
	require.NoError(t, err)
	require.True(t, acquired)
	defer lease.release()
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at+60))
	assert.Equal(t, "3000000000", client.HGet(ctx, ChannelMonitorRedisCostDayKey(model.ChannelDailyCostDayStart(at)), "channel:7:"+channelMonitorRedisSharedMetricSettledCost).Val())
}

func TestReliableDailyCostTaskCorrectionKeepsOriginalDayAfterRedisLoss(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelTaskCostEvent{}, &model.ChannelMonitorDailyCostDetail{}, &model.ChannelDailyCostOutbox{}))
	server, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	ctx := context.Background()
	const at = int64(1_750_000_000)
	day := model.ChannelDailyCostDayStart(at)
	_, err := model.RegisterChannelTaskCostEvent(ctx, model.ChannelTaskCostEventInput{
		CostEventId: "task:projection-correction", ChannelId: 7, OccurredAt: at,
		InitialQuota: 6, CostNanoCNY: 6_000_000_000, ModelName: "model-a",
	})
	require.NoError(t, err)
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	_, err = model.SetChannelTaskCostEventCost(ctx, "task:projection-correction", 2_000_000_000, at+86400)
	require.NoError(t, err)
	var rows []model.ChannelDailyCostOutbox
	require.NoError(t, db.Order("id DESC").Find(&rows).Error)
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	field := "channel:7:" + channelMonitorRedisSharedMetricSettledCost
	assert.Equal(t, "2000000000", client.HGet(ctx, ChannelMonitorRedisCostDayKey(day), field).Val())
	assert.False(t, server.Exists(ChannelMonitorRedisCostDayKey(day+86400)))
	server.FlushAll()
	require.NoError(t, rebuildChannelMonitorRedisDailyCosts(ctx, client, at))
	require.NoError(t, projectChannelDailyCostOutboxRows(ctx, client, rows))
	assert.Equal(t, "2000000000", client.HGet(ctx, ChannelMonitorRedisCostDayKey(day), field).Val())
	assert.Equal(t, "1", client.HGet(ctx, ChannelMonitorRedisCostDayKey(day), "channel:7:"+channelMonitorRedisSharedMetricSettledRequests).Val())
}
