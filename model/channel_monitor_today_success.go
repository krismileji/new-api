package model

import (
	"context"
	"time"
)

type ChannelMonitorTodaySuccessMetrics struct {
	Summary         ChannelMonitorSuccessSummary          `json:"summary"`
	ChannelItems    []ChannelMonitorChannelSuccessMetric  `json:"channel_items"`
	APIKeyItems     []ChannelMonitorSuccessAPIKeyMetric   `json:"api_key_items"`
	CacheWriteItems []ChannelMonitorTodayCacheWriteMetric `json:"cache_write_items"`
}

type ChannelMonitorTodayCacheWriteMetric struct {
	ChannelId    int   `json:"channel_id"`
	RequestCount int64 `json:"request_count"`
}

type ChannelMonitorDailySuccessMetric struct {
	DayStart               int64
	Summary                ChannelMonitorSuccessSummary
	CacheWriteChannelCount int
	CacheWriteRequestCount int64
}

func getChannelMonitorTodaySuccessMetrics(ctx context.Context, dayStart int64) (ChannelMonitorTodaySuccessMetrics, error) {
	return getChannelMonitorMinuteTodaySuccessMetrics(ctx, dayStart, dayStart+channelMonitorCostDaySeconds)
}

func getChannelMonitorDailySuccessMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorDailySuccessMetric, error) {
	return getChannelMonitorMinuteDailySuccessMetrics(ctx, startTimestamp, endTimestamp)
}

// GetChannelMonitorDailySuccessMetrics reads the requested historical range
// directly from the persisted minute aggregates.
func GetChannelMonitorDailySuccessMetrics(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelMonitorDailySuccessMetric, error) {
	return getChannelMonitorDailySuccessMetrics(ctx, startTimestamp, endTimestamp)
}

// GetChannelMonitorSuccessMetricsForDay reads one completed Beijing calendar
// day directly from the persisted minute aggregates.
func GetChannelMonitorSuccessMetricsForDay(ctx context.Context, dayStart int64) (ChannelMonitorTodaySuccessMetrics, error) {
	dayStart = ChannelDailyCostDayStart(dayStart)
	return getChannelMonitorMinuteTodaySuccessMetrics(ctx, dayStart, time.Now().Unix())
}

// GetChannelMonitorTodaySuccessMetrics reads the current Beijing calendar day
// directly from the persisted minute aggregates up to generatedAt.
func GetChannelMonitorTodaySuccessMetrics(ctx context.Context, generatedAt int64) (ChannelMonitorTodaySuccessMetrics, error) {
	return getChannelMonitorMinuteTodaySuccessMetrics(
		ctx,
		ChannelDailyCostDayStart(generatedAt),
		generatedAt,
	)
}
