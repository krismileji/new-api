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

func TestChannelMonitorMinuteAnalyticsPreservesWindowAndDrilldownScopes(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	require.NoError(t, db.Create(&model.User{Id: 31, Username: "alice", DisplayName: "Alice", Password: "fixture"}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 201, UserId: 31, Key: "minute-analytics-key", Name: "production"}).Error)
	now := common.GetTimestamp()
	minute := now - now%60
	fixtures := []struct {
		id, group, model string
		channel, key     int
		at               int64
		outcome          model.ChannelMonitorEventOutcome
		final            bool
	}{
		{"success", "vip", "model-a", 7, 201, minute - 60, model.ChannelMonitorEventOutcomeSuccess, true},
		{"retry", "vip", "model-a", 7, 201, minute - 60, model.ChannelMonitorEventOutcomeFailure, false},
		{"other-model", "vip", "model-b", 7, 201, minute - 60, model.ChannelMonitorEventOutcomeSuccess, true},
		{"other-group", "default", "model-a", 7, 201, minute - 60, model.ChannelMonitorEventOutcomeSuccess, true},
		{"other-channel", "vip", "model-a", 8, 201, minute - 60, model.ChannelMonitorEventOutcomeSuccess, true},
		{"unknown-key", "vip", "model-a", 7, 0, minute - 60, model.ChannelMonitorEventOutcomeFailure, true},
		{"old", "vip", "model-a", 7, 201, minute - 2400, model.ChannelMonitorEventOutcomeFailure, true},
	}
	events := make([]model.ChannelMonitorEvent, 0, len(fixtures))
	for _, fixture := range fixtures {
		event := model.NewChannelMonitorEvent(fixture.channel, model.ChannelMonitorEventSourceBusiness, fixture.outcome, fixture.at)
		event.EventId, event.GroupName, event.ModelName = fixture.id, fixture.group, fixture.model
		event.APIKeyId, event.UserId, event.APIKeyName = fixture.key, 31, "production"
		event.RequestDispatched, event.IsFinalAttempt = true, fixture.final
		events = append(events, event)
	}
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).HandleChannelMonitorEvents(context.Background(), events))

	for _, tc := range []struct {
		name, groupBy string
		filters       url.Values
		samples       float64
		success       float64
		total         int64
	}{
		{"channel includes every model but excludes old samples", "channel", url.Values{"channel_id": {"7"}}, 5, 3, 1},
		{"model stays in channel", "model", url.Values{"channel_id": {"7"}}, 5, 3, 2},
		{"user stays in model", "user", url.Values{"channel_id": {"7"}, "model": {"model-a"}}, 4, 2, 2},
		{"key stays in user", "api_key", url.Values{"channel_id": {"7"}, "model": {"model-a"}, "user_id": {"31"}}, 3, 2, 1},
		{"unknown ownership does not widen filter", "api_key", url.Values{"channel_id": {"7"}, "user_id": {"0"}}, 1, 0, 1},
		{"group stays in scope", "channel", url.Values{"group": {"vip"}}, 5, 3, 2},
		{"group and channel stay in scope", "model", url.Values{"group": {"vip"}, "channel_id": {"7"}}, 4, 2, 3},
		{"group model and key stay in scope", "channel", url.Values{"group": {"vip"}, "model": {"model-a"}, "api_key_id": {"201"}}, 3, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := tc.filters
			params.Set("minutes", "30")
			params.Set("group_by", tc.groupBy)
			response := requestChannelMonitorAnalytics(t, params)
			assert.Equal(t, "redis_minutes", response.Source)
			assert.Equal(t, tc.samples, response.Summary["actual_sample_count"])
			assert.Equal(t, tc.success, response.Summary["actual_success_count"])
			assert.Equal(t, tc.total, response.Total)
		})
	}
}

func TestChannelMonitorMinuteAnalyticsRejectsInvalidRanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{
		"minutes=", "minutes=0", "minutes=-1", "minutes=1441", "minutes=1.5",
		"minutes=30&metric=cost", "minutes=30&from=2026-09-01", "minutes=30&group_by=day",
		"group=vip",
	} {
		t.Run(query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/analytics/rows?"+query, nil)
			GetChannelMonitorAnalyticsRows(ctx)
			assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		})
	}
}

func TestChannelMonitorMinuteAnalyticsKeepsFinalRetrySummaryOutOfAttemptTotals(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	event := model.NewChannelMonitorEvent(7, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, now-60)
	event.ModelName, event.GroupName = "model-a", "vip"
	event.RequestDispatched = true
	status := 503
	event.StatusCode = &status
	event.ErrorCode, event.ErrorMessage = "upstream_error", "upstream unavailable"
	final := event.Clone()
	final.EventId = "final-summary"
	final.FinalRetrySummary, final.RequestDispatched = true, false
	probe := event.Clone()
	probe.EventId, probe.Source = "probe", model.ChannelMonitorEventSourceStatusProbe
	local := event.Clone()
	local.EventId, local.RequestDispatched = "local", false
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event, final, probe, local}))
	response := requestChannelMonitorAnalytics(t, url.Values{"minutes": {"30"}, "channel_id": {"7"}, "model": {"model-a"}})
	assert.Equal(t, float64(1), response.Summary["actual_failure_count"])
	assert.Equal(t, float64(1), response.Summary["final_failure_count"])
	require.Len(t, response.FailureCategories, 1)
	assert.Equal(t, int64(1), response.FailureCategories[0].ActualCount)
	assert.Equal(t, int64(1), response.FailureCategories[0].FinalCount)
	assert.Equal(t, "upstream unavailable", response.FailureCategories[0].SampleContent)
}

func TestChannelMonitorMinuteAnalyticsFinalSortingKeepsPaginationSummary(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	success := model.NewChannelMonitorEvent(7, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now-60)
	success.RequestDispatched, success.IsFinalAttempt = true, true
	retry := success.Clone()
	retry.EventId, retry.Outcome, retry.IsFinalAttempt = "retry-failed", model.ChannelMonitorEventOutcomeFailure, false
	failure := retry.Clone()
	failure.EventId, failure.ChannelId, failure.IsFinalAttempt = "final-failed", 8, true
	otherSuccess := success.Clone()
	otherSuccess.EventId, otherSuccess.ChannelId = "other-success", 8
	secondSuccess := otherSuccess.Clone()
	secondSuccess.EventId = "second-success"
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{success, retry, failure, otherSuccess, secondSuccess}))
	params := url.Values{"minutes": {"30"}, "success_mode": {"final"}, "sort": {"success_rate"}, "page_size": {"1"}}
	first := requestChannelMonitorAnalytics(t, params)
	params.Set("page", "2")
	second := requestChannelMonitorAnalytics(t, params)
	assert.Equal(t, int64(2), first.Total)
	assert.Equal(t, first.ScopeSummary, second.ScopeSummary)
	require.Len(t, first.Items, 1)
	require.Len(t, second.Items, 1)
	assert.Equal(t, float64(7), first.Items[0]["channel_id"])
	assert.Equal(t, float64(1), first.Items[0]["final_success_rate"])
	assert.Equal(t, 0.5, first.Items[0]["actual_success_rate"])
	assert.Equal(t, float64(8), second.Items[0]["channel_id"])
	params.Set("page", "1")
	params.Set("success_mode", "actual")
	actual := requestChannelMonitorAnalytics(t, params)
	require.Len(t, actual.Items, 1)
	assert.Equal(t, float64(8), actual.Items[0]["channel_id"])
}

func TestChannelMonitorMinuteAnalyticsMergesFailureCategoriesAcrossMinutesAndModels(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	minute := now - now%60
	events := make([]model.ChannelMonitorEvent, 0, 4)
	for i, fixture := range []struct {
		model, group string
		at           int64
	}{
		{"model-a", "vip", minute - 180},
		{"model-a", "vip", minute - 120},
		{"model-b", "vip", minute - 60},
		{"model-b", "default", minute},
	} {
		event := model.NewChannelMonitorEvent(7, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, fixture.at)
		event.ModelName, event.GroupName = fixture.model, fixture.group
		event.RequestDispatched, event.IsFinalAttempt = true, true
		event.EventSequence = uint64(i + 1)
		status := 503
		event.StatusCode, event.ErrorType, event.ErrorCode = &status, "upstream_error", "service_unavailable"
		event.ErrorMessage = "older failure"
		if i == 3 {
			event.ErrorMessage = "latest failure"
		}
		events = append(events, event)
	}
	require.NoError(t, service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).HandleChannelMonitorEvents(context.Background(), events))
	params := url.Values{"minutes": {"30"}, "channel_id": {"7"}}
	response := requestChannelMonitorAnalytics(t, params)
	assert.Equal(t, float64(4), response.Summary["actual_failure_count"])
	var failureTotal int64
	for _, category := range response.FailureCategories {
		failureTotal += category.ActualCount
	}
	assert.Equal(t, int64(4), failureTotal)
	require.Len(t, response.FailureCategories, 1)
	assert.Equal(t, "latest failure", response.FailureCategories[0].SampleContent)
	assert.Equal(t, minute, response.FailureCategories[0].LastOccurred)
	params.Set("model", "model-a")
	scoped := requestChannelMonitorAnalytics(t, params)
	require.Len(t, scoped.FailureCategories, 1)
	assert.Equal(t, int64(2), scoped.FailureCategories[0].ActualCount)
}
