package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
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
	RateLimitCooldownUntil    int64                                   `json:"rate_limit_cooldown_until"`
	RateLimitBypassUntil      int64                                   `json:"rate_limit_bypass_until"`
	CostRatio                 *float64                                `json:"cost_ratio,omitempty"`
	GroupRatio                *float64                                `json:"group_ratio,omitempty"`
	GrossMargin               *float64                                `json:"gross_margin,omitempty"`
	EconomicRole              string                                  `json:"economic_role,omitempty"`
	CurrentWindowScore        *float64                                `json:"current_window_score"`
	CurrentWindowScoreDetails *model.ChannelSmartScheduleScoreDetails `json:"current_window_score_details,omitempty"`
	State                     model.ChannelSmartScheduleRouteState    `json:"state"`
	EffectivePriority         int64                                   `json:"effective_priority"`
	EffectiveWeight           uint                                    `json:"effective_weight"`
	EffectiveState            *model.ChannelSmartScheduleRouteState   `json:"effective_state,omitempty"`
	RoutingCandidateChannelId int                                     `json:"routing_candidate_channel_id"`
	LogicalChannelId          int64                                   `json:"logical_channel_id,omitempty"`
	LogicalRevision           int64                                   `json:"logical_revision,omitempty"`
	LogicalMemberIds          []int                                   `json:"logical_member_ids,omitempty"`
	LogicalMemberWeights      []uint                                  `json:"logical_member_weights,omitempty"`
}

type channelSmartScheduleSampleItem struct {
	ChannelId         int                                        `json:"channel_id"`
	Model             string                                     `json:"model"`
	PerformanceWindow model.ChannelSmartScheduleModelSampleState `json:"performance_window"`
	StabilityWindow   model.ChannelSmartScheduleModelSampleState `json:"stability_window"`
}

func channelSmartScheduleRouteResponses(
	routes []model.ChannelSmartScheduleRoute,
	runtimeViews ...map[model.ChannelSmartScheduleRouteKey]model.ChannelSmartScheduleRouteRuntimeView,
) []channelSmartScheduleRouteResponse {
	responses := make([]channelSmartScheduleRouteResponse, 0, len(routes))
	var runtimeByKey map[model.ChannelSmartScheduleRouteKey]model.ChannelSmartScheduleRouteRuntimeView
	if len(runtimeViews) > 0 {
		runtimeByKey = runtimeViews[0]
	}
	for _, route := range routes {
		runtimeView := model.ChannelSmartScheduleRouteRuntimeView{
			Priority:           route.Priority,
			Weight:             route.Weight,
			CandidateChannelId: route.ChannelId,
		}
		if runtimeByKey != nil {
			if view, ok := runtimeByKey[model.ChannelSmartScheduleRouteKey{
				ChannelId: route.ChannelId, Group: route.Group, Model: route.Model,
			}]; ok {
				runtimeView = view
			}
		}
		var effectiveState *model.ChannelSmartScheduleRouteState
		if runtimeView.State != nil {
			state := *runtimeView.State
			effectiveState = &state
		}
		responses = append(responses, channelSmartScheduleRouteResponse{
			ChannelId: route.ChannelId, ChannelName: route.ChannelName,
			ChannelStatus: route.ChannelStatus, ChannelPriority: route.ChannelPriority,
			ChannelWeight: route.ChannelWeight, Group: route.Group, Model: route.Model,
			SampleModel: ratio_setting.FormatMatchingModelName(route.Model),
			Enabled:     route.Enabled, Priority: runtimeView.Priority, Weight: runtimeView.Weight,
			TrafficPausedUntil:     runtimeView.TrafficPausedUntil,
			RateLimitCooldownUntil: service.ChannelRateLimitCooldownUntilMatching(route.ChannelId, route.Model),
			RateLimitBypassUntil:   service.ChannelRateLimitBypassUntilMatching(route.ChannelId, route.Model),
			CostRatio:              route.CostRatio, GroupRatio: route.GroupRatio,
			GrossMargin: route.GrossMargin, EconomicRole: route.EconomicRole,
			State: route.State, EffectivePriority: runtimeView.Priority,
			EffectiveWeight: runtimeView.Weight, EffectiveState: effectiveState,
			RoutingCandidateChannelId: runtimeView.CandidateChannelId,
			LogicalChannelId:          runtimeView.LogicalChannelId,
			LogicalRevision:           runtimeView.LogicalRevision,
			LogicalMemberIds:          append([]int(nil), runtimeView.LogicalMemberIds...),
			LogicalMemberWeights:      append([]uint(nil), runtimeView.LogicalMemberWeights...),
		})
	}
	return responses
}
