package model

func InvalidateChannelMonitorAggregateCaches() {
	resetChannelMonitorMetricsCache()
	resetChannelMonitorTodaySuccessCache()
}
