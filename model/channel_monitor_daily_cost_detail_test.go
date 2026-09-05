package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorDailyCostDetailAggregatesCostDimensions(t *testing.T) {
	db := setupChannelDailyCostOutboxTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelMonitorDailyCostDetail{}))

	delta := ChannelDailyCostDelta{
		ChannelId: 7, OccurredAt: 1_750_000_000, CostNanoCNY: 1200, SettledDelta: 1,
		APIKeyId: 201, APIKeyName: "生产 Key", KeyFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		KeyDisplay: "prod", UserId: 31, UserAttribution: string(ChannelMonitorEventUserAttributionRequest), ModelName: "gpt-4.1", SourceKind: "business",
	}
	require.NoError(t, AddChannelDailyCostBatch(context.Background(), []ChannelDailyCostDelta{delta}))
	require.NoError(t, AddChannelDailyCostBatch(context.Background(), []ChannelDailyCostDelta{delta}))

	var detail ChannelMonitorDailyCostDetail
	require.NoError(t, db.Where("channel_id = ? AND api_key_id = ?", 7, 201).First(&detail).Error)
	require.Equal(t, int64(2400), detail.CostNanoCNY)
	require.Equal(t, int64(2), detail.SettledCount)
	require.Equal(t, 31, detail.UserId)
	require.Equal(t, "gpt-4.1", detail.ModelName)
}
