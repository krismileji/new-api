package controller

import (
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelSmartScheduleRouteResponse struct {
	ChannelId                 int                                     `json:"channel_id"`
	ChannelName               string                                  `json:"channel_name"`
	ChannelStatus             int                                     `json:"channel_status"`
	ChannelPriority           int64                                   `json:"channel_priority"`
	ChannelWeight             uint                                    `json:"channel_weight"`
	Group                     string                                  `json:"group"`
	Model                     string                                  `json:"model"`
	SampleModel               string                                  `json:"sample_model"`
	Enabled                   bool                                    `json:"enabled"`
	Priority                  int64                                   `json:"priority"`
	Weight                    uint                                    `json:"weight"`
	TrafficPausedUntil        int64                                   `json:"traffic_paused_until"`
	CostRatio                 *float64                                `json:"cost_ratio,omitempty"`
	GroupRatio                *float64                                `json:"group_ratio,omitempty"`
	GrossMargin               *float64                                `json:"gross_margin,omitempty"`
	EconomicRole              string                                  `json:"economic_role,omitempty"`
	CurrentWindowScore        *float64                                `json:"current_window_score"`
	CurrentWindowScoreDetails *model.ChannelSmartScheduleScoreDetails `json:"current_window_score_details,omitempty"`
	State                     model.ChannelSmartScheduleRouteState    `json:"state"`
}

type channelSmartScheduleSampleItem struct {
	ChannelId         int                                        `json:"channel_id"`
	Model             string                                     `json:"model"`
	PerformanceWindow model.ChannelSmartScheduleModelSampleState `json:"performance_window"`
	StabilityWindow   model.ChannelSmartScheduleModelSampleState `json:"stability_window"`
}

type channelSmartScheduleSampleMetricWindowKey struct {
	modelKey    channelSmartScheduleModelKey
	windowStart int64
}

type channelSmartScheduleSampleMetricCache struct {
	stateByModel    map[channelSmartScheduleModelKey]model.ChannelSmartScheduleModelSampleState
	seriesByModel   map[channelSmartScheduleModelKey]model.ChannelSmartScheduleSampleSeries
	metricsByWindow map[channelSmartScheduleSampleMetricWindowKey]model.ChannelSmartScheduleSampleMetrics
}

func channelSmartScheduleRouteResponses(
	routes []model.ChannelSmartScheduleRoute,
) []channelSmartScheduleRouteResponse {
	responses := make([]channelSmartScheduleRouteResponse, 0, len(routes))
	for _, route := range routes {
		responses = append(responses, channelSmartScheduleRouteResponse{
			ChannelId: route.ChannelId, ChannelName: route.ChannelName,
			ChannelStatus: route.ChannelStatus, ChannelPriority: route.ChannelPriority,
			ChannelWeight: route.ChannelWeight, Group: route.Group, Model: route.Model,
			SampleModel: ratio_setting.FormatMatchingModelName(route.Model),
			Enabled:     route.Enabled, Priority: route.Priority, Weight: route.Weight,
			TrafficPausedUntil: route.TrafficPausedUntil,
			CostRatio:          route.CostRatio, GroupRatio: route.GroupRatio,
			GrossMargin: route.GrossMargin, EconomicRole: route.EconomicRole,
			State: route.State,
		})
	}
	return responses
}

func newChannelSmartScheduleSampleMetricCache(
	routes []model.ChannelSmartScheduleRoute,
) (*channelSmartScheduleSampleMetricCache, error) {
	cache := &channelSmartScheduleSampleMetricCache{
		stateByModel:    make(map[channelSmartScheduleModelKey]model.ChannelSmartScheduleModelSampleState),
		seriesByModel:   make(map[channelSmartScheduleModelKey]model.ChannelSmartScheduleSampleSeries),
		metricsByWindow: make(map[channelSmartScheduleSampleMetricWindowKey]model.ChannelSmartScheduleSampleMetrics),
	}
	for _, route := range routes {
		key := channelSmartScheduleModelKey{
			channelId: route.ChannelId,
			model:     ratio_setting.FormatMatchingModelName(route.Model),
		}
		if _, exists := cache.seriesByModel[key]; exists {
			continue
		}
		series, err := route.SharedSamples.SampleSeries()
		if err != nil {
			return nil, err
		}
		cache.stateByModel[key] = route.SharedSamples
		cache.seriesByModel[key] = series
	}
	return cache, nil
}

func (cache *channelSmartScheduleSampleMetricCache) metrics(
	key channelSmartScheduleModelKey,
	windowStart int64,
) model.ChannelSmartScheduleSampleMetrics {
	windowKey := channelSmartScheduleSampleMetricWindowKey{modelKey: key, windowStart: windowStart}
	if metrics, exists := cache.metricsByWindow[windowKey]; exists {
		return metrics
	}
	series, exists := cache.seriesByModel[key]
	if !exists {
		return model.ChannelSmartScheduleSampleMetrics{}
	}
	metrics := series.MetricsSince(windowStart)
	cache.metricsByWindow[windowKey] = metrics
	return metrics
}

func (cache *channelSmartScheduleSampleMetricCache) adaptiveHealthMetrics(
	key channelSmartScheduleModelKey,
	windowStart int64,
	warningSeconds float64,
	criticalSeconds float64,
) model.ChannelSmartScheduleAdaptiveHealthMetric {
	series, exists := cache.seriesByModel[key]
	if !exists {
		return model.ChannelSmartScheduleAdaptiveHealthMetric{}
	}
	return series.AdaptiveHealthMetricsSince(windowStart, warningSeconds, criticalSeconds)
}

func (cache *channelSmartScheduleSampleMetricCache) items(
	performanceStart int64,
	stabilityStart int64,
) []channelSmartScheduleSampleItem {
	items := make([]channelSmartScheduleSampleItem, 0, len(cache.stateByModel))
	for key, state := range cache.stateByModel {
		items = append(items, channelSmartScheduleSampleItem{
			ChannelId: key.channelId,
			Model:     key.model,
			PerformanceWindow: state.WindowedWithMetrics(
				cache.metrics(key, performanceStart),
			),
			StabilityWindow: state.WindowedWithMetrics(
				cache.metrics(key, stabilityStart),
			),
		})
	}
	sort.Slice(items, func(i int, j int) bool {
		if items[i].Model != items[j].Model {
			return items[i].Model < items[j].Model
		}
		return items[i].ChannelId < items[j].ChannelId
	})
	return items
}

func channelSmartScheduleRoutePerformanceWithSamples(
	route model.ChannelSmartScheduleRoute,
	business *model.ChannelMonitorRoutePerformanceMetric,
	sampleMetrics model.ChannelSmartScheduleSampleMetrics,
) (model.ChannelMonitorRoutePerformanceMetric, bool) {
	var performance *channelSmartSchedulePerformance
	metric := model.ChannelMonitorRoutePerformanceMetric{
		ChannelId:                 route.ChannelId,
		GroupName:                 route.Group,
		ModelName:                 route.Model,
		FirstTokenDurationBuckets: []model.ChannelMonitorDurationBucket{},
	}
	if business != nil {
		performance = channelSmartScheduleSetPerformanceMetric(nil, *business)
		metric.GroupCount = business.GroupCount
		metric.SampleCount = business.SampleCount
		metric.LastUsedTime = business.LastUsedTime
	}
	performance = channelSmartScheduleMergeSampleMetrics(performance, sampleMetrics)
	if performance == nil {
		return metric, false
	}
	metric.SampleCount += int(sampleMetrics.SampleCount)
	metric.FirstTokenSampleCount = performance.FirstTokenSampleCount
	metric.FirstTokenDurationSampleCount = performance.FirstTokenDurationSampleCount
	metric.TPSSampleCount = performance.TPSSampleCount
	metric.AverageFirstTokenMs = performance.AverageFirstTokenMs
	metric.FirstTokenP50Ms = performance.FirstTokenP50Ms
	metric.FirstTokenP95Ms = performance.FirstTokenP95Ms
	metric.WinsorizedAverageFirstTokenMs = performance.WinsorizedAverageFirstTokenMs
	metric.FirstTokenDurationBuckets = append(
		[]model.ChannelMonitorDurationBucket(nil),
		performance.FirstTokenDurationBuckets...,
	)
	metric.AverageTPS = performance.AverageTPS
	metric.LastUsedTime = max(metric.LastUsedTime, sampleMetrics.LastTime)
	return metric, true
}
