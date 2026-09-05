package model

import (
	"fmt"
	"strings"
)

type channelLogicalSmartScheduleRouteOverlay struct {
	routing channelLogicalSmartScheduleRouting
	state   ChannelSmartScheduleRouteState
}

func loadLogicalSmartScheduleRouteOverlays(
	logicalIDs []int64,
	groupName string,
	modelName string,
) (map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay, error) {
	return loadLogicalSmartScheduleRouteOverlaysWithDB(
		DB, logicalIDs, strings.TrimSpace(groupName), modelName,
	)
}

func logicalSmartScheduleRouteOverlaysFromStates(
	rows []ChannelLogicalSmartScheduleRouteState,
) (map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay, error) {
	result := make(map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay, len(rows))
	for _, row := range rows {
		payload, err := decodeLogicalSmartScheduleRoutePayload(row.StateJSON)
		if err != nil {
			return nil, fmt.Errorf("解析逻辑智能调度选路状态失败: %w", err)
		}
		result[channelLogicalSmartScheduleRouteKey{
			logicalID: row.LogicalGroupID, revision: row.LogicalRevision,
			group: row.GroupName, model: row.ModelName,
		}] = channelLogicalSmartScheduleRouteOverlay{
			routing: channelLogicalSmartScheduleRouting{
				priority: payload.EffectivePriority, weight: payload.EffectiveWeight,
			},
			state: payload.State,
		}
	}
	return result, nil
}

func filterChannelSmartScheduleParticipatingCachedRoutes(
	routes []channelSmartScheduleCachedRoute,
	group string,
	modelName string,
	policy *channelSmartScheduleTrafficPolicy,
) []channelSmartScheduleCachedRoute {
	if policy == nil || !policy.managesPool(group, modelName) {
		return routes
	}
	filtered := make([]channelSmartScheduleCachedRoute, 0, len(routes))
	for _, route := range routes {
		if route.participates {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func filterChannelSmartScheduleStableCachedRoutes(
	routes []channelSmartScheduleCachedRoute,
	group string,
	modelName string,
	policy *channelSmartScheduleTrafficPolicy,
	allowDegradedFallback bool,
) []channelSmartScheduleCachedRoute {
	if policy == nil || !policy.managesPool(group, modelName) || allowDegradedFallback {
		return routes
	}
	filtered := make([]channelSmartScheduleCachedRoute, 0, len(routes))
	for _, route := range routes {
		if route.stabilityState != ChannelSmartScheduleStabilityDegraded {
			filtered = append(filtered, route)
		}
	}
	return filtered
}

func filterChannelSmartScheduleParticipatingAbilities(
	abilities []Ability,
	group string,
	modelName string,
	policy *channelSmartScheduleTrafficPolicy,
) ([]Ability, error) {
	if policy == nil || !policy.managesPool(group, modelName) || len(abilities) == 0 {
		return abilities, nil
	}
	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	var states []ChannelSmartScheduleRouteState
	if err := DB.Select("channel_id").Where(
		"group_name = ? AND model_name = ? AND channel_id IN ? AND participation_set = ? AND excluded = ?",
		group, modelName, channelIDs, true, false,
	).Find(&states).Error; err != nil {
		return nil, err
	}
	participating := make(map[int]struct{}, len(states))
	for _, state := range states {
		participating[state.ChannelId] = struct{}{}
	}
	filtered := make([]Ability, 0, len(participating))
	for _, ability := range abilities {
		if _, allowed := participating[ability.ChannelId]; allowed {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}
