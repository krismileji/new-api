package model

import "gorm.io/gorm"

const (
	channelMonitorMinuteRouteMetricTable    = "channel_monitor_minute_route_metrics"
	channelMonitorMinuteAPIKeyMetricTable   = "channel_monitor_minute_api_key_metrics"
	channelMonitorMinuteDurationBucketTable = "channel_monitor_minute_duration_buckets"
	channelMonitorObservationStateTable     = "channel_smart_schedule_model_sample_states"
	channelMonitorObservationStateAlias     = "channel_monitor_observation_state"
)

func applyChannelMonitorObservationBoundary(
	query *gorm.DB,
	metricTable string,
) *gorm.DB {
	return query.
		Joins(
			"LEFT JOIN " + channelMonitorObservationStateTable + " AS " + channelMonitorObservationStateAlias +
				" ON " + channelMonitorObservationStateAlias + ".channel_id = " + metricTable + ".channel_id" +
				" AND " + channelMonitorObservationStateAlias + ".model_name = " + metricTable + ".model_name",
		).
		Where(
			"(" + channelMonitorObservationStateAlias + ".observation_since IS NULL OR " +
				metricTable + ".minute_start >= " + channelMonitorObservationStateAlias + ".observation_since)",
		)
}
