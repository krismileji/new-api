package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedChannelMonitorAnalyticsFilterFixture(t *testing.T, db *gorm.DB, day int64) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailyCostDetail{}, &model.ChannelMonitorDailySuccessLedger{}))
	require.NoError(t, db.Create(&[]model.User{
		{Id: 31, Username: "alice", DisplayName: "研发负责人", Password: "fixture", AffCode: "analytics31"},
		{Id: 32, Username: "bob", DisplayName: "运营", Password: "fixture", AffCode: "analytics32"},
	}).Error)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 7, Name: "生产渠道", Type: 1, Key: "fixture"},
		{Id: 8, Name: "备用渠道", Type: 1, Key: "fixture"},
	}).Error)
	fixtures := []struct {
		user, key, channel int
		fingerprint, name  string
		cost               int64
	}{
		{0, 0, 7, "", "", 100},
		{31, 201, 7, "a", "model-a", 200},
		{31, 201, 8, "b", "model-b", 300},
		{32, 201, 7, "a", "model-a", 400},
	}
	for _, fixture := range fixtures {
		name := "生产 Key"
		if fixture.key == 0 {
			name = ""
		}
		require.NoError(t, db.Create(&model.ChannelMonitorDailyCostDetail{
			DayStart: day, ChannelId: fixture.channel, UserId: fixture.user,
			APIKeyId: fixture.key, APIKeyKey: strings.Repeat(fixture.fingerprint, 64), APIKeyName: name,
			ModelName: fixture.name, ModelKey: model.ChannelMonitorDailyCostModelKey(fixture.name),
			SourceKind: "business", CostNanoCNY: fixture.cost, SettledCount: 1,
		}).Error)
		successIdentity := model.ChannelMonitorDailyMetricIdentity{Model: fixture.name}
		require.NoError(t, db.Create(&model.ChannelMonitorDailySuccessLedger{
			DayStart: day, ChannelId: fixture.channel, UserId: fixture.user,
			APIKeyId: fixture.key, APIKeyKey: strings.Repeat(fixture.fingerprint, 32), APIKeyName: name,
			ModelName: fixture.name, ModelKey: successIdentity.LedgerRow(day).ModelKey,
			ActualSuccessCount: fixture.cost, FinalSuccessCount: fixture.cost,
		}).Error)
	}
	require.NoError(t, db.Create(&[]model.ChannelDailyCost{
		{ChannelId: 7, DayStart: day, CostNanoCNY: 700, SettledCount: 3},
		{ChannelId: 8, DayStart: day, CostNanoCNY: 300, SettledCount: 1},
	}).Error)
}

func requestChannelMonitorAnalytics(t *testing.T, params url.Values) channelMonitorAnalyticsResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/analytics/rows?"+params.Encode(), nil)
	GetChannelMonitorAnalyticsRows(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var body struct {
		Success bool                            `json:"success"`
		Message string                          `json:"message"`
		Data    channelMonitorAnalyticsResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success, body.Message)
	return body.Data
}

func runChannelMonitorAnalyticsFilterCases(t *testing.T, day int64) {
	t.Helper()
	zone := time.FixedZone("UTC+8", 8*60*60)
	cases := []struct {
		name   string
		filter url.Values
		total  int64
		amount float64
	}{
		{"all users", url.Values{"group_by": {"user"}}, 3, 1000},
		{"unknown user", url.Values{"user_id": {"0"}}, 1, 100},
		{"unknown key", url.Values{"api_key_id": {"0"}}, 1, 100},
		{"empty key identity", url.Values{"user_id": {"0"}, "api_key_id": {"0"}, "api_key_key": {""}}, 1, 100},
		{"same key ID with another fingerprint", url.Values{"user_id": {"31"}, "api_key_id": {"201"}, "api_key_key": {strings.Repeat("b", 64)}}, 1, 300},
		{"same key identity under another owner", url.Values{"user_id": {"32"}, "api_key_id": {"201"}, "api_key_key": {strings.Repeat("a", 64)}, "model": {"model-a"}}, 1, 400},
		{"unknown model identity", url.Values{"model_key": {"unknown"}}, 1, 100},
		{"username", url.Values{"search": {"ALICE"}}, 2, 500},
		{"display name", url.Values{"search": {"研发负责人"}}, 2, 500},
		{"channel name", url.Values{"search": {"备用渠道"}}, 1, 300},
		{"exact numeric ID", url.Values{"search": {"201"}}, 2, 900},
		{"numeric ID is not a substring", url.Values{"search": {"1"}}, 0, 0},
		{"literal SQL wildcard", url.Values{"search": {"%"}}, 0, 0},
	}
	for _, metric := range []string{"cost", "success"} {
		for _, tc := range cases {
			t.Run(metric+"/"+tc.name, func(t *testing.T) {
				params := url.Values{
					"metric": {metric}, "group_by": {"channel"},
					"from": {time.Unix(day, 0).In(zone).Format("2006-01-02")},
					"to":   {time.Unix(day+86400, 0).In(zone).Format("2006-01-02")},
				}
				for key, values := range tc.filter {
					params[key] = values
				}
				if metric == "success" {
					if fingerprint := params.Get("api_key_key"); len(fingerprint) == 64 {
						params.Set("api_key_key", fingerprint[:32])
					}
					if params.Get("model_key") == "unknown" {
						identity := model.ChannelMonitorDailyMetricIdentity{}
						params.Set("model_key", identity.LedgerRow(day).ModelKey)
					}
				}
				response := requestChannelMonitorAnalytics(t, params)
				assert.Equal(t, tc.total, response.Total)
				field := "cost_nano_cny"
				if metric == "success" {
					field = "actual_sample_count"
				}
				assert.Equal(t, tc.amount, response.Summary[field])
			})
		}
	}
}

func TestChannelMonitorAnalyticsFilterIdentity(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp()) - 2*86400
	seedChannelMonitorAnalyticsFilterFixture(t, db, day)
	runChannelMonitorAnalyticsFilterCases(t, day)
}

func TestChannelMonitorAnalyticsCurrentFilterIdentity(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	day := model.ChannelDailyCostDayStart(common.GetTimestamp())
	seedChannelMonitorAnalyticsFilterFixture(t, db, day)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorDailyCheckpoint{}))
	require.NoError(t, service.RebuildChannelMonitorRedisDailyCosts(context.Background(), day+1))
	require.NoError(t, service.RebuildChannelMonitorRedisDailySuccess(context.Background(), day+1))
	runChannelMonitorAnalyticsFilterCases(t, day)
}

func TestChannelMonitorAnalyticsRejectsNegativeFilterIDs(t *testing.T) {
	for _, parameter := range []string{"user_id", "api_key_id", "channel_id"} {
		t.Run(parameter, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/?"+parameter+"=-1", nil)
			GetChannelMonitorAnalyticsRows(ctx)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}
