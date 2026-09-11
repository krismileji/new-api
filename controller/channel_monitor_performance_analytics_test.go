package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedChannelMonitorPerformanceAnalytics(t *testing.T) {
	t.Helper()
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	require.NoError(t, db.Create(&model.User{Id: 31, Username: "alice", Password: "fixture"}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 201, UserId: 31, Key: "performance-key", Name: "production"}).Error)
	minute := common.GetTimestamp() / 60 * 60
	var events []model.ChannelMonitorEvent
	for _, fixture := range []struct {
		name, model string
		key         int
		ttft, tps   float64
		tokens      int64
		at          int64
		source      model.ChannelMonitorEventSource
	}{
		{"fast", "model-a", 201, 100.5, 100, 100, minute - 120, model.ChannelMonitorEventSourceBusiness},
		{"slow", "model-b", 201, 1200.5, 10, 900, minute - 60, model.ChannelMonitorEventSourceBusiness},
		{"keyless", "model-a", 0, 300.25, 50, 100, minute - 60, model.ChannelMonitorEventSourceBusiness},
		{"unmeasured", "model-a", 201, 0, 0, 0, minute - 60, model.ChannelMonitorEventSourceBusiness},
		{"expired", "model-a", 201, 9000, 1, 9000, minute - 2400, model.ChannelMonitorEventSourceBusiness},
		{"probe", "model-a", 201, 9000, 1, 9000, minute - 60, model.ChannelMonitorEventSourceStatusProbe},
	} {
		event := model.NewChannelMonitorEvent(7, fixture.source, model.ChannelMonitorEventOutcomeSuccess, fixture.at)
		event.EventId, event.ModelName, event.GroupName = fixture.name, fixture.model, "vip"
		event.APIKeyId, event.APIKeyName, event.UserId = fixture.key, "production", 31
		event.RequestDispatched, event.IsFinalAttempt, event.IsStream = true, true, true
		if fixture.tokens > 0 {
			event.FirstTokenMs, event.TPS, event.CompletionTokens = &fixture.ttft, &fixture.tps, &fixture.tokens
		} else {
			event.Outcome = model.ChannelMonitorEventOutcomeFailure
		}
		events = append(events, event)
	}
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).HandleChannelMonitorEvents(context.Background(), events))
}

func TestChannelMonitorPerformanceAnalyticsWeightsMeasurementsAndPreservesDrilldown(t *testing.T) {
	seedChannelMonitorPerformanceAnalytics(t)
	for _, tc := range []struct {
		name, groupBy string
		filters       url.Values
		attempts      float64
		samples       float64
		ttft, tps     float64
		tokens        float64
	}{
		{"channel", "channel", nil, 4, 3, 533.75, 1100.0 / 93, 1100},
		{"model", "model", url.Values{"model": {"model-a"}}, 3, 2, 200.375, 200.0 / 3, 200},
		{"user", "user", url.Values{"model": {"model-a"}, "user_id": {"31"}}, 2, 1, 100.5, 100, 100},
		{"key", "api_key", url.Values{"model": {"model-a"}, "user_id": {"31"}, "api_key_id": {"201"}}, 2, 1, 100.5, 100, 100},
		{"unknown owner", "api_key", url.Values{"user_id": {"0"}}, 1, 1, 300.25, 50, 100},
		{"group", "channel", url.Values{"group": {"vip"}}, 4, 3, 533.75, 1100.0 / 93, 1100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := url.Values{"metric": {"performance"}, "minutes": {"30"}, "channel_id": {"7"}, "group_by": {tc.groupBy}}
			for key, values := range tc.filters {
				params[key] = values
			}
			response := requestChannelMonitorAnalytics(t, params)
			assert.Equal(t, "redis_minutes", response.Source)
			assert.Equal(t, 30, response.RangeMinutes)
			assert.Equal(t, tc.attempts, response.ScopeSummary["actual_sample_count"])
			assert.Equal(t, tc.samples, response.ScopeSummary["first_token_sample_count"])
			assert.Equal(t, tc.samples, response.ScopeSummary["tps_sample_count"])
			assert.Equal(t, tc.tokens, response.ScopeSummary["tps_output_tokens"])
			assert.InDelta(t, tc.ttft, response.ScopeSummary["average_first_token_ms"], 1e-9)
			assert.InDelta(t, tc.tps, response.ScopeSummary["average_tps"], 1e-9)
			require.Len(t, response.Items, 1)
			assert.Equal(t, response.ScopeSummary["average_tps"], response.Items[0]["average_tps"])
			assert.Empty(t, response.FailureCategories)
		})
	}
}

func TestChannelMonitorPerformanceAnalyticsSortsMeasurementsAndKeepsScopeTotals(t *testing.T) {
	seedChannelMonitorPerformanceAnalytics(t)
	params := url.Values{"metric": {"performance"}, "minutes": {"30"}, "channel_id": {"7"}, "group_by": {"model"}, "sort": {"first_token"}, "direction": {"desc"}, "page_size": {"1"}}
	first := requestChannelMonitorAnalytics(t, params)
	require.Len(t, first.Items, 1)
	assert.Equal(t, "model-b", first.Items[0]["model_name"])
	assert.Equal(t, int64(2), first.Total)
	params.Set("page", "2")
	second := requestChannelMonitorAnalytics(t, params)
	assert.Equal(t, first.ScopeSummary, second.ScopeSummary)
	params.Set("page", "1")
	params.Set("sort", "tps")
	fast := requestChannelMonitorAnalytics(t, params)
	require.Len(t, fast.Items, 1)
	assert.Equal(t, "model-a", fast.Items[0]["model_name"])
}

func TestChannelMonitorPerformanceAnalyticsKeepsMissingMeasurementsNullAndLast(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	var events []model.ChannelMonitorEvent
	for i, name := range []string{"unmeasured", "zero-first-token", "measured"} {
		event := model.NewChannelMonitorEvent(7, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, common.GetTimestamp()-60)
		event.ModelName, event.RequestDispatched = name, true
		if i > 0 {
			ttft, tps, tokens := float64(i-1)*100, float64(i)*10, int64(100)
			event.FirstTokenMs, event.TPS, event.CompletionTokens = &ttft, &tps, &tokens
		}
		events = append(events, event)
	}
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).HandleChannelMonitorEvents(context.Background(), events))
	for _, direction := range []string{"asc", "desc"} {
		response := requestChannelMonitorAnalytics(t, url.Values{"metric": {"performance"}, "minutes": {"30"}, "group_by": {"model"}, "sort": {"first_token"}, "direction": {direction}})
		require.Len(t, response.Items, 3)
		assert.Equal(t, "unmeasured", response.Items[2]["model_name"])
		assert.Nil(t, response.Items[2]["average_first_token_ms"])
		assert.Nil(t, response.Items[2]["average_tps"])
		if direction == "asc" {
			assert.Equal(t, float64(0), response.Items[0]["average_first_token_ms"])
		}
	}
	empty := requestChannelMonitorAnalytics(t, url.Values{"metric": {"performance"}, "minutes": {"30"}, "channel_id": {"999"}})
	assert.Empty(t, empty.Items)
	assert.Equal(t, float64(0), empty.Summary["actual_sample_count"])
	assert.Nil(t, empty.Summary["average_first_token_ms"])
	assert.Nil(t, empty.Summary["average_tps"])
}

func TestChannelMonitorPerformanceAnalyticsRejectsUnsupportedRangesAndModes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"", "&from=2026-09-01", "&minutes=30&group_by=day", "&minutes=30&success_mode=final", "&minutes=30&sort=success_rate"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/analytics/rows?metric=performance"+query, nil)
		GetChannelMonitorAnalyticsRows(ctx)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, query)
	}
}
