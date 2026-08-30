package controller

import (
	"context"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorCostOverviewBoundsHighCardinalityAPIKeysAndPreservesTotals(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	const channelID = 99001
	const keyCount = channelMonitorCostAPIKeyMaxRows + 1
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "高基数测试渠道", Key: "high-cardinality"}).Error)

	now := common.GetTimestamp()
	dayStart := channelMonitorCostDayStart(now)
	const costPerKey = int64(1_000_000_000)
	require.NoError(t, model.AddChannelDailyCost(
		context.Background(), channelID, now, int64(keyCount)*costPerKey, keyCount, 0,
	))
	rows := make([]model.ChannelDailyAPIKeyCost, 0, keyCount)
	for index := 1; index <= keyCount; index++ {
		fingerprint, display := model.ChannelDailyCostAPIKeyIdentityForToken(index, "sk-high-cardinality-"+strconv.Itoa(index))
		rows = append(rows, model.ChannelDailyAPIKeyCost{
			ChannelId: channelID, DayStart: dayStart, APIKeyId: index,
			APIKeyName: "key", KeyFingerprint: fingerprint, KeyDisplay: display,
			CostNanoCNY: costPerKey, SettledCount: 1, CreatedAt: now, UpdatedAt: now,
		})
	}
	require.NoError(t, db.Create(&rows).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?days=2&channel_id=99001", nil)
	GetChannelMonitorCostOverview(ctx)
	require.Equal(t, 200, recorder.Code)
	var response struct {
		Data channelMonitorCostOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Data.APIKeysTruncated)
	assert.LessOrEqual(t, len(response.Data.APIKeys), channelMonitorCostAPIKeyMaxRows)
	assert.InDelta(t, float64(keyCount), response.Data.TotalCostCNY, 1e-9)

	var apiKeyTotal float64
	for _, key := range response.Data.APIKeys {
		apiKeyTotal += key.CostCNY
	}
	assert.InDelta(t, response.Data.TotalCostCNY, apiKeyTotal, 1e-9)
}
