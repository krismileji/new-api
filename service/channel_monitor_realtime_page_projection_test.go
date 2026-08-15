package service

import (
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRealtimePageProjectionDoesNotTruncateHighVolumeRoute(t *testing.T) {
	projection := newChannelMonitorRealtimePageProjection()
	now := time.Now().Unix()
	events := make([]model.ChannelMonitorEvent, 0, 1503)
	for index := 0; index < 1500; index++ {
		event := realtimeProjectionTestEvent(
			"page-high-volume-"+strconv.Itoa(index), 701, "model-page", now-int64(index%60), uint64(index+1),
			model.ChannelMonitorEventOutcomeSuccess, true,
		)
		event.GroupName = "page-group"
		event.APIKeyId = 91
		event.APIKeyName = "页面 Key"
		events = append(events, event)
	}
	failure := realtimeProjectionTestEvent("page-failure", 701, "model-page", now, 1501, model.ChannelMonitorEventOutcomeFailure, false)
	failure.GroupName = "page-group"
	failure.APIKeyId = 91
	failure.APIKeyName = "页面 Key"
	failure.ErrorType = "upstream_error"
	failure.ErrorCode = "overloaded"
	failure.ErrorMessage = "上游过载"
	statusCode := 503
	failure.StatusCode = &statusCode
	canceled := realtimeProjectionTestEvent("page-canceled", 701, "model-page", now, 1502, model.ChannelMonitorEventOutcomeCanceled, false)
	canceled.GroupName = "page-group"
	canceled.APIKeyId = 91
	canceledInput := int64(999)
	canceled.InputTokens = &canceledInput
	summary := failure
	summary.EventId = "page-final-summary"
	summary.EventSequence = 1503
	summary.FinalRetrySummary = true
	summary.RequestDispatched = false
	summary.IsFinalAttempt = true
	events = append(events, failure, canceled, summary)

	projection.consume(events, now)
	view := projection.query(now-15*60, now+1)
	require.Len(t, view.Routes, 1)
	assert.Equal(t, 1501, view.Routes[0].SampleCount)
	assert.Equal(t, int64(1500), view.Routes[0].Summary.ActualSuccessCount)
	assert.Equal(t, int64(1), view.Routes[0].Summary.ActualFailureCount)
	assert.Equal(t, int64(1), view.Routes[0].Summary.FinalFailureCount)
	require.Len(t, view.APIKeys, 1)
	assert.Equal(t, int64(1501), view.APIKeys[0].Summary.ActualSampleCount)

	detailView := projection.successDetail(now-15*60, now+1, model.ChannelMonitorSuccessFilter{
		ChannelId: 701,
		ModelName: "model-page",
	})
	detail := detailView.Detail
	assert.Equal(t, int64(1501), detail.Summary.ActualSampleCount)
	require.Len(t, detail.APIKeyItems, 1)
	assert.Equal(t, 91, detail.APIKeyItems[0].APIKeyId)
	require.Len(t, detail.FailureCategories, 1)
	assert.Equal(t, int64(1), detail.FailureCategories[0].ActualCount)
	assert.Equal(t, int64(1), detail.FailureCategories[0].FinalCount)
	assert.Equal(t, "upstream_error", detail.FailureCategories[0].ErrorType)
	assert.Equal(t, "overloaded", detail.FailureCategories[0].ErrorCode)
	assert.Equal(t, "上游过载", detail.FailureCategories[0].SampleContent)
	assert.Equal(t, now, view.ProcessedAt)
	assert.Equal(t, uint64(1503), view.EventWatermark)
	assert.Equal(t, view.ProcessedAt, detailView.ProcessedAt)
	assert.Equal(t, view.EventWatermark, detailView.EventWatermark)
}

func TestChannelMonitorRealtimePageProjectionIgnoresDuplicateEventIds(t *testing.T) {
	projection := newChannelMonitorRealtimePageProjection()
	event := model.NewChannelMonitorEvent(
		702,
		model.ChannelMonitorEventSourceBusiness,
		model.ChannelMonitorEventOutcomeSuccess,
		1_700_000_001,
	)
	event.EventId = "page-duplicate-event"
	event.ModelName = "model-duplicate"
	event.APIKeyId = 92
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	event.CostStatus = model.ChannelMonitorEventCostSettled

	projection.consume([]model.ChannelMonitorEvent{event, event}, 1_700_000_060)
	view := projection.query(1_700_000_000, 1_700_000_120)

	assert.Equal(t, int64(1), view.Summary.Summary.ActualSampleCount)
	require.Len(t, view.Routes, 1)
	assert.Equal(t, int64(1), view.Routes[0].Summary.ActualSampleCount)
	require.Len(t, view.APIKeys, 1)
	assert.Equal(t, int64(1), view.APIKeys[0].SettledRequestCount)
}
