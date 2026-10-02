package controller

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func runChannelMonitorSystemProbeAnalyticsCases(t *testing.T, db *gorm.DB, day int64, source, name string) {
	t.Helper()
	require.NoError(t, db.Where("id = ?", 1).Assign(model.ChannelMonitorIncomeState{StartedAt: day - 86400}).FirstOrCreate(&model.ChannelMonitorIncomeState{ID: 1}).Error)
	for index, fixture := range []struct {
		source string
		name   string
		keyID  int
		cost   int64
	}{
		{"business", name, 0, 10},
		{"business", name, 0, 20},
		{source, name, 0, 30},
		{"business", name, 201, 40},
		{"business", "其他请求", 0, 50},
		{"status_probe", name, 0, 60},
	} {
		modelName := "probe-model-" + strconv.Itoa(index)
		require.NoError(t, db.Create(&model.ChannelMonitorDailyCostDetail{
			DayStart: day, ChannelId: 900301 + index%2, UserId: 913,
			APIKeyId: fixture.keyID, APIKeyKey: "upstream-key-" + strconv.Itoa(index), APIKeyName: fixture.name,
			ModelName: modelName, ModelKey: model.ChannelMonitorDailyCostModelKey(modelName),
			SourceKind: fixture.source, CostNanoCNY: fixture.cost, SettledCount: 1,
		}).Error)
	}
	require.NoError(t, db.Create(&[]model.ChannelDailyCost{
		{DayStart: day, ChannelId: 900301, CostNanoCNY: 90, SettledCount: 3},
		{DayStart: day, ChannelId: 900302, CostNanoCNY: 120, SettledCount: 3},
	}).Error)
	if day == model.ChannelDailyCostDayStart(common.GetTimestamp()) {
		require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(context.Background(), day+1))
	}
	zone := time.FixedZone("UTC+8", 8*60*60)
	for _, metric := range []string{"cost", "profit"} {
		t.Run(metric, func(t *testing.T) {
			params := url.Values{
				"metric": {metric}, "group_by": {"api_key"}, "user_id": {"913"},
				"from":      {time.Unix(day, 0).In(zone).Format("2006-01-02")},
				"to":        {time.Unix(day+86400, 0).In(zone).Format("2006-01-02")},
				"page_size": {"20"},
			}
			response := requestChannelMonitorAnalytics(t, params)
			assert.Equal(t, int64(4), response.Total, "legacy and canonical probes merge; real keys and other sources stay separate")
			assert.Equal(t, float64(210), response.ScopeSummary["cost_nano_cny"])
			var probes []map[string]any
			for _, item := range response.Items {
				if item["api_key_key"] == source {
					probes = append(probes, item)
				}
			}
			require.Len(t, probes, 1)
			assert.Equal(t, float64(60), probes[0]["cost_nano_cny"])
			assert.Equal(t, float64(3), probes[0]["settled_count"])

			params.Set("api_key_id", "0")
			params.Set("api_key_key", source)
			params.Set("page_size", "1")
			filtered := requestChannelMonitorAnalytics(t, params)
			assert.Equal(t, int64(1), filtered.Total)
			require.Len(t, filtered.Items, 1)
			assert.Equal(t, float64(60), filtered.Items[0]["cost_nano_cny"])

			params.Set("group_by", "model")
			params.Set("page_size", "20")
			models := requestChannelMonitorAnalytics(t, params)
			assert.Equal(t, int64(3), models.Total)
			assert.Equal(t, float64(60), models.ScopeSummary["cost_nano_cny"])
			for _, item := range models.Items {
				params.Set("group_by", "channel")
				params.Set("model_key", item["model_key"].(string))
				channels := requestChannelMonitorAnalytics(t, params)
				require.Len(t, channels.Items, 1)
				assert.Equal(t, item["cost_nano_cny"], channels.ScopeSummary["cost_nano_cny"])
			}
		})
	}
}

func TestChannelMonitorSystemProbeAnalytics(t *testing.T) {
	for _, scenario := range []struct {
		source string
		name   string
		offset int64
	}{
		{"smart_probe", "智能调度探测", -86400},
		{"smart_probe", "智能调度探测", 0},
		{"manual_test", "模型测试", -86400},
		{"manual_test", "模型测试", 0},
	} {
		t.Run(scenario.source+"/"+strconv.FormatInt(scenario.offset, 10), func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailyCostDetail{}, &model.ChannelMonitorIncome{}, &model.ChannelMonitorIncomeState{}, &model.ChannelMonitorIncomeGap{}, &model.ChannelDailyCostOutbox{}))
			runChannelMonitorSystemProbeAnalyticsCases(t, db, model.ChannelDailyCostDayStart(common.GetTimestamp())+scenario.offset, scenario.source, scenario.name)
			if scenario.offset == 0 {
				var details []model.ChannelMonitorDailyCostDetail
				require.NoError(t, db.Find(&details).Error)
				for _, detail := range details {
					detail.Id, detail.DayStart = 0, detail.DayStart-86400
					require.NoError(t, db.Create(&detail).Error)
				}
				day := model.ChannelDailyCostDayStart(common.GetTimestamp())
				identity := scenario.source
				query := channelMonitorAnalyticsQuery{
					Metric: "cost", GroupBy: "api_key", From: day - 86400, To: day + 86400,
					User: 913, UserSet: true, APIKeySet: true, APIKeyKey: &identity, Page: 1, PageSize: 1,
				}
				response, err := queryChannelMonitorHistoricalAnalytics(context.Background(), query)
				require.NoError(t, err)
				assert.Equal(t, "redis_and_database_daily", response.Source)
				assert.Equal(t, int64(1), response.Total)
				require.Len(t, response.Items, 1)
				assert.Equal(t, int64(120), response.Items[0]["cost_nano_cny"])
				assert.Equal(t, int64(6), response.Items[0]["settled_count"])
			}
		})
	}
}
