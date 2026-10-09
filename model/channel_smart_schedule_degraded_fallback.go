package model

import "math"

// channelSmartScheduleDegradedRank preserves the last usable ranking while
// protection temporarily replaces the effective routing with P0/W0.
type channelSmartScheduleDegradedRank struct {
	Score            *float64 `json:"score,omitempty"`
	ScoreAt          int64    `json:"score_at,omitempty"`
	StabilityScore   *float64 `json:"stability_score,omitempty"`
	StabilityScoreAt int64    `json:"stability_score_at,omitempty"`
	Priority         int64    `json:"priority,omitempty"`
	Weight           uint     `json:"weight,omitempty"`
}

// Degraded scores travel in the existing companion payload so older nodes can
// still verify the unchanged routing snapshot checksum during a rolling update.
type channelLogicalSmartScheduleDegradedRank struct {
	LogicalID       int64
	LogicalRevision int64
	Group           string
	Model           string
	Rank            *channelSmartScheduleDegradedRank
}

func restoreChannelSmartScheduleDegradedRanks(
	routes map[string]map[string][]channelSmartScheduleCachedRoute,
	logicalRouting map[channelLogicalSmartScheduleRouteKey]channelLogicalSmartScheduleRouteOverlay,
	monitor *channelSmartScheduleMonitorReadModel,
) {
	if monitor == nil {
		return
	}
	ranks := make(map[ChannelSmartScheduleRouteKey]*channelSmartScheduleDegradedRank, len(monitor.Routes))
	for _, route := range monitor.Routes {
		ranks[channelSmartScheduleRouteKey(route.ChannelId, route.Group, route.Model)] = channelSmartScheduleDegradedRankFromState(route.State)
	}
	for group, modelRoutes := range routes {
		for modelName, poolRoutes := range modelRoutes {
			for i := range poolRoutes {
				poolRoutes[i].degradedRank = ranks[channelSmartScheduleRouteKey(poolRoutes[i].channelId, group, modelName)]
			}
		}
	}
	for _, item := range monitor.LogicalDegradedRanks {
		key := channelLogicalSmartScheduleRouteKey{
			logicalID: item.LogicalID, revision: item.LogicalRevision, group: item.Group, model: item.Model,
		}
		overlay, exists := logicalRouting[key]
		if !exists || item.Rank == nil {
			continue
		}
		overlay.state.LastScheduleScore = item.Rank.Score
		overlay.state.LastScheduleScoreAt = item.Rank.ScoreAt
		overlay.state.RollingStabilityScore = item.Rank.StabilityScore
		overlay.state.RollingStabilityUpdatedAt = item.Rank.StabilityScoreAt
		overlay.state.StabilitySavedPriority = item.Rank.Priority
		overlay.state.StabilitySavedWeight = item.Rank.Weight
		logicalRouting[key] = overlay
	}
}

func channelSmartScheduleDegradedRankFromState(state ChannelSmartScheduleRouteState) *channelSmartScheduleDegradedRank {
	rank := &channelSmartScheduleDegradedRank{
		ScoreAt: state.LastScheduleScoreAt, StabilityScoreAt: state.RollingStabilityUpdatedAt,
		Priority: state.StabilitySavedPriority, Weight: state.StabilitySavedWeight,
	}
	for i, score := range []*float64{state.LastScheduleScore, state.RollingStabilityScore} {
		if score != nil && !math.IsNaN(*score) && !math.IsInf(*score, 0) && *score >= 0 && *score <= 1 {
			value := *score
			if i == 0 {
				rank.Score = &value
			} else {
				rank.StabilityScore = &value
			}
		}
	}
	if rank.Priority == 0 {
		rank.Priority = state.BasePriority
	}
	if rank.Weight == 0 {
		rank.Weight = state.BaseWeight
	}
	return rank
}

// bestChannelSmartScheduleDegradedRoute applies only after eligibility filters.
// It does not clear protection or change persisted priorities and weights.
// Healthy/probing candidates keep the normal selection and retry rules.
func bestChannelSmartScheduleDegradedRoute(routes []channelSmartScheduleCachedRoute, now int64) []channelSmartScheduleCachedRoute {
	if len(routes) == 0 {
		return routes
	}
	for _, route := range routes {
		if route.stabilityState != ChannelSmartScheduleStabilityDegraded {
			return routes
		}
	}
	// Evaluate against the live retention setting on every selection: neither
	// an old cache snapshot nor a refresh can extend the score's lifetime.
	retentionSeconds := int64(GetChannelMonitorSmartScheduleRealtimeSettings().RetentionMinutes) * 60
	best := -1
	var bestRank channelSmartScheduleDegradedRank
	for i, route := range routes {
		rank := channelSmartScheduleDegradedRank{}
		if route.degradedRank != nil {
			rank = *route.degradedRank
		}
		if rank.ScoreAt <= 0 || rank.ScoreAt > now || now-rank.ScoreAt >= retentionSeconds {
			rank.Score = nil
		}
		if rank.Score == nil && rank.StabilityScoreAt > 0 && rank.StabilityScoreAt <= now && now-rank.StabilityScoreAt < retentionSeconds {
			rank.Score = rank.StabilityScore
		}
		better := best < 0
		switch {
		case (rank.Score != nil) != (bestRank.Score != nil):
			better = rank.Score != nil
		case rank.Score != nil && bestRank.Score != nil && *rank.Score != *bestRank.Score:
			better = *rank.Score > *bestRank.Score
		case rank.Priority != bestRank.Priority:
			better = rank.Priority > bestRank.Priority
		case rank.Weight != bestRank.Weight:
			better = rank.Weight > bestRank.Weight
		case best >= 0:
			better = route.channelId < routes[best].channelId
		}
		if best < 0 || better {
			best, bestRank = i, rank
		}
	}
	if best < 0 {
		return routes
	}
	return routes[best : best+1]
}
