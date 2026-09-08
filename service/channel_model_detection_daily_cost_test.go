package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestModelDetectionTodayCostReadsRedisWithoutDailyLedgerQuery(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	_, client := newChannelMonitorRedisSharedProjectionTestClient(t)
	originalClient, originalEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = originalClient, originalEnabled })
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&model.Channel{Id: 7, Name: "检测渠道", Group: "default", Models: "model-a"}).Error)
	require.NoError(t, db.Create(&model.ChannelDailyCost{ChannelId: 7, DayStart: model.ChannelDailyCostDayStart(now), CostNanoCNY: 999, ModelDetectionCostNanoCNY: 999}).Error)
	fields := map[string]string{channelMonitorReliableCostVersionField: "1", "meta:revision": "7"}
	require.NoError(t, applyChannelMonitorReliableCostDelta(fields, channelMonitorReliableCostState{
		Identity: channelMonitorReliableCostIdentity{ChannelID: 7, Model: "model-a", Source: string(model.ChannelMonitorEventSourceModelDetection)},
		Cost:     25_680_000, Detection: 25_680_000, Settled: 1, Unresolved: 1,
	}, 1))
	ctx := context.Background()
	require.NoError(t, client.HSet(ctx, ChannelMonitorRedisCostDayKey(model.ChannelDailyCostDayStart(now)), fields).Err())
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:no_detection_daily_sql", func(tx *gorm.DB) {
		if tx.Statement.Table == "channel_daily_costs" {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove("test:no_detection_daily_sql")) })
	response, err := GetChannelModelDetectionOverview(ctx, db, now)
	require.NoError(t, err)
	require.Len(t, response.Channels, 1)
	assert.Equal(t, "redis_daily", response.CostSource)
	assert.Equal(t, int64(7), response.CostRevision)
	assert.InDelta(t, 0.02568, response.Channels[0].TodayModelDetectionCostCNY, 1e-12)
	cost := response.Channels[0].TodayModelDetectionCost
	require.NotNil(t, cost)
	require.NotNil(t, cost.SettledCostCNY)
	assert.Equal(t, "0.025680000", *cost.SettledCostCNY)
	assert.Equal(t, int64(1), cost.UnresolvedRequestCount)
	assert.Equal(t, ChannelModelDetectionCostStatusPartial, cost.Status)
}
