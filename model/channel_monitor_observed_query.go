package model

import "context"

func channelMonitorObservedWindow(generatedAt int64, rangeMinutes int) (int64, int64) {
	windowEnd := generatedAt - generatedAt%60
	return windowEnd - int64(rangeMinutes*60), windowEnd
}

// GetChannelMonitorObservedPerformanceMetrics reads the completed-minute
// performance window directly from the persisted aggregates.
func GetChannelMonitorObservedPerformanceMetrics(ctx context.Context, generatedAt int64, rangeMinutes int) ([]ChannelMonitorPerformanceMetric, error) {
	windowStart, windowEnd := channelMonitorObservedWindow(generatedAt, rangeMinutes)
	return getChannelMonitorObservedMinutePerformanceMetrics(ctx, windowStart, windowEnd)
}

// GetChannelMonitorObservedSuccessMetrics reads the completed-minute success
// window directly from the persisted aggregates.
func GetChannelMonitorObservedSuccessMetrics(ctx context.Context, generatedAt int64, rangeMinutes int) ([]ChannelMonitorSuccessMetric, []ChannelMonitorGroupSuccessMetric, error) {
	windowStart, windowEnd := channelMonitorObservedWindow(generatedAt, rangeMinutes)
	return getChannelMonitorObservedSuccessMetrics(ctx, windowStart, windowEnd, true)
}

// GetChannelMonitorObservedStabilityMetrics reads the completed-minute
// stability window directly from the persisted aggregates.
func GetChannelMonitorObservedStabilityMetrics(ctx context.Context, generatedAt int64, rangeMinutes int) ([]ChannelMonitorStabilityMetric, error) {
	metrics, _, err := GetChannelMonitorObservedSuccessMetrics(ctx, generatedAt, rangeMinutes)
	if err != nil {
		return nil, err
	}
	return channelMonitorStabilityMetricsFromSuccess(metrics), nil
}
