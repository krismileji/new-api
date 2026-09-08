package controller

import (
	"context"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func runChannelMonitorCostAPIKeyGroupingCases(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	zone := time.FixedZone("UTC+8", 8*60*60)
	params := url.Values{
		"metric": {"cost"}, "group_by": {"api_key"}, "user_id": {"31"},
		"from":      {time.Unix(day, 0).In(zone).Format("2006-01-02")},
		"to":        {time.Unix(day+86400, 0).In(zone).Format("2006-01-02")},
		"page_size": {"1"},
	}
	t.Run("one inbound key across upstream credentials is one paginated row", func(t *testing.T) {
		response := requestChannelMonitorAnalytics(t, params)
		assert.Equal(t, int64(1), response.Total)
		require.Len(t, response.Items, 1)
		assert.Equal(t, float64(500), response.Items[0]["cost_nano_cny"])
		assert.Equal(t, float64(2), response.Items[0]["settled_count"])
		assert.NotContains(t, response.Items[0], "api_key_key", "an aggregated key must not restrict drill-down to one upstream credential")
	})
	t.Run("API key IDs remain scoped to their owner", func(t *testing.T) {
		all := maps.Clone(params)
		all.Del("user_id")
		all.Set("page_size", "20")
		response := requestChannelMonitorAnalytics(t, all)
		assert.Equal(t, int64(3), response.Total)
		assert.Equal(t, float64(1000), response.Summary["cost_nano_cny"])
		keys := make(map[string]bool)
		for _, item := range response.Items {
			key := item["key"].(string)
			assert.False(t, keys[key], "different owners must have different row keys")
			keys[key] = true
		}
	})
	t.Run("expanding the inbound key retains every model and channel", func(t *testing.T) {
		child := maps.Clone(params)
		child.Set("group_by", "model")
		child.Set("api_key_id", "201")
		child.Set("page_size", "20")
		response := requestChannelMonitorAnalytics(t, child)
		assert.Equal(t, int64(2), response.Total)
		assert.Equal(t, float64(500), response.Summary["cost_nano_cny"])
		for _, item := range response.Items {
			channel := maps.Clone(child)
			channel.Set("group_by", "channel")
			channel.Set("model_key", item["model_key"].(string))
			detail := requestChannelMonitorAnalytics(t, channel)
			require.Len(t, detail.Items, 1)
			assert.Equal(t, item["cost_nano_cny"], detail.Summary["cost_nano_cny"])
		}
	})

	for _, source := range []string{"smart_probe", "status_probe"} {
		for index, channel := range []int{7, 8} {
			modelName := "probe-model-" + strconv.Itoa(channel)
			require.NoError(t, db.Create(&model.ChannelMonitorDailyCostDetail{
				DayStart: day, ChannelId: channel, UserId: 31, APIKeyId: 0,
				APIKeyKey: strings.Repeat(strconv.Itoa(channel), 64), APIKeyName: source,
				ModelName: modelName, ModelKey: model.ChannelMonitorDailyCostModelKey(modelName),
				SourceKind: source, CostNanoCNY: int64(index+1) * 10, SettledCount: int64(index + 1),
			}).Error)
			require.NoError(t, db.Model(&model.ChannelDailyCost{}).
				Where("channel_id = ? AND day_start = ?", channel, day).
				Updates(map[string]any{
					"cost_nano_cny": gorm.Expr("cost_nano_cny + ?", int64(index+1)*10),
					"settled_count": gorm.Expr("settled_count + ?", index+1),
				}).Error)
		}
	}
	if day == model.ChannelDailyCostDayStart(common.GetTimestamp()) {
		require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(context.Background(), day+1))
	}
	t.Run("system probes aggregate by source and keep the source when expanded", func(t *testing.T) {
		probe := maps.Clone(params)
		probe.Set("api_key_id", "0")
		probe.Set("page_size", "20")
		response := requestChannelMonitorAnalytics(t, probe)
		assert.Equal(t, int64(2), response.Total)
		assert.Equal(t, float64(60), response.Summary["cost_nano_cny"])
		for _, item := range response.Items {
			assert.Equal(t, float64(30), item["cost_nano_cny"])
			identity := item["api_key_key"].(string)
			assert.Contains(t, []string{"smart_probe", "status_probe"}, identity)
			child := maps.Clone(probe)
			child.Set("group_by", "model")
			child.Set("api_key_key", identity)
			details := requestChannelMonitorAnalytics(t, child)
			assert.Equal(t, int64(2), details.Total)
			assert.Equal(t, float64(30), details.Summary["cost_nano_cny"])
		}
	})
}

func TestChannelMonitorCostAPIKeyGrouping(t *testing.T) {
	for _, offset := range []int64{-86400, 0} {
		t.Run(strconv.FormatInt(offset, 10), func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			day := model.ChannelDailyCostDayStart(common.GetTimestamp()) + offset
			seedChannelMonitorAnalyticsFilterFixture(t, db, day)
			if offset == 0 {
				require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(context.Background(), day+1))
			}
			runChannelMonitorCostAPIKeyGroupingCases(t, db, day)
		})
	}
}

func TestChannelMonitorCostAPIKeyGroupingMergesTodayAndHistory(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp())
	seedChannelMonitorAnalyticsFilterFixture(t, db, day)
	var details []model.ChannelMonitorDailyCostDetail
	require.NoError(t, db.Find(&details).Error)
	for _, detail := range details {
		detail.Id, detail.DayStart = 0, day-86400
		require.NoError(t, db.Create(&detail).Error)
	}
	var totals []model.ChannelDailyCost
	require.NoError(t, db.Find(&totals).Error)
	for _, total := range totals {
		total.Id, total.DayStart = 0, day-86400
		require.NoError(t, db.Create(&total).Error)
	}
	require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(context.Background(), day+1))
	query := channelMonitorAnalyticsQuery{
		Metric: "cost", GroupBy: "api_key", From: day - 86400, To: day + 86400,
		User: 31, UserSet: true, Sort: "cost", Direction: "desc", Page: 1, PageSize: 1,
	}
	response, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
	require.NoError(t, err)
	assert.Equal(t, "redis_and_database_daily", response.Source)
	assert.Equal(t, int64(1), response.Total)
	require.Len(t, response.Items, 1)
	assert.Equal(t, int64(1000), response.Items[0]["cost_nano_cny"])
	assert.Equal(t, int64(4), response.Items[0]["settled_count"])
	assert.NotContains(t, response.Items[0], "api_key_key")
}
