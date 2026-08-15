package controller

import (
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
