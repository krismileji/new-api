package controller

import (
	"context"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelSmartScheduleRouteMetricWindowKey struct {
	modelKey    channelSmartScheduleModelKey
	windowStart int64
}

type channelSmartScheduleProbingMetrics struct {
	performanceByWindow map[channelSmartScheduleRouteMetricWindowKey]model.ChannelMonitorRoutePerformanceMetric
	stabilityByWindow   map[channelSmartScheduleRouteMetricWindowKey]model.ChannelMonitorRouteStabilityMetric
}

func loadChannelSmartScheduleProbingMetrics(
	ctx context.Context,
	routes []model.ChannelSmartScheduleRoute,
	policyByGroup map[string]channelSmartSchedulePolicy,
	stabilityStart int64,
	endTimestamp int64,
	logStabilityAvailable bool,
) (channelSmartScheduleProbingMetrics, error) {
	loaded := channelSmartScheduleProbingMetrics{
		performanceByWindow: make(map[channelSmartScheduleRouteMetricWindowKey]model.ChannelMonitorRoutePerformanceMetric),
		stabilityByWindow:   make(map[channelSmartScheduleRouteMetricWindowKey]model.ChannelMonitorRouteStabilityMetric),
	}
	windowsByKey := make(map[channelSmartScheduleRouteMetricWindowKey]model.ChannelMonitorRouteMetricWindow)
	includePerformance := false
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured || !policy.StabilityEnabled ||
			route.State.StabilityState != model.ChannelSmartScheduleStabilityProbing ||
			route.State.StabilitySince <= stabilityStart {
			continue
		}
		windowStart := route.State.StabilitySince - route.State.StabilitySince%60
		key := channelSmartScheduleRouteMetricWindowKey{
			modelKey: channelSmartScheduleModelKey{
				channelId: route.ChannelId,
				model:     ratio_setting.FormatMatchingModelName(route.Model),
			},
			windowStart: windowStart,
		}
		windowsByKey[key] = model.ChannelMonitorRouteMetricWindow{
			ChannelId: route.ChannelId, ModelName: route.Model,
			StartTimestamp: windowStart,
		}
		includePerformance = includePerformance || policy.JitterEnabled
	}
	if len(windowsByKey) == 0 || (!logStabilityAvailable && !includePerformance) {
		return loaded, nil
	}
	windows := make([]model.ChannelMonitorRouteMetricWindow, 0, len(windowsByKey))
	for _, window := range windowsByKey {
		windows = append(windows, window)
	}
	metrics, err := model.GetChannelMonitorRouteMetricsForWindowsCached(
		ctx,
		windows,
		endTimestamp,
		includePerformance,
		logStabilityAvailable,
	)
	if err != nil {
		return loaded, err
	}
	for _, metric := range metrics {
		key := channelSmartScheduleRouteMetricWindowKey{
			modelKey: channelSmartScheduleModelKey{
				channelId: metric.Window.ChannelId,
				model:     ratio_setting.FormatMatchingModelName(metric.Window.ModelName),
			},
			windowStart: metric.Window.StartTimestamp,
		}
		if includePerformance {
			loaded.performanceByWindow[key] = metric.Performance
		}
		if logStabilityAvailable {
			loaded.stabilityByWindow[key] = metric.Stability
		}
	}
	return loaded, nil
}
