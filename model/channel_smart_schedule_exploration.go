package model

type channelSmartScheduleRequestLimitState struct {
	ChannelId                       int
	TemporaryTrafficKind            string
	StabilityState                  string
	ExplorationMaxPromptTokens      int
	StabilityReleaseMaxPromptTokens int
}

func loadChannelSmartScheduleRequestLimitStates(
	group string,
	modelName string,
	channelIDs []int,
) (map[int]channelSmartScheduleRequestLimitState, error) {
	statesByChannel := make(map[int]channelSmartScheduleRequestLimitState)
	if DB == nil || len(channelIDs) == 0 || !DB.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return statesByChannel, nil
	}
	var states []channelSmartScheduleRequestLimitState
	err := DB.Model(&ChannelSmartScheduleRouteState{}).
		Select(
			"channel_id", "temporary_traffic_kind", "stability_state",
			"exploration_max_prompt_tokens", "stability_release_max_prompt_tokens",
		).
		Where(
			"group_name = ? AND model_name = ? AND channel_id IN ? AND participation_set = ? AND excluded = ? AND (temporary_traffic_kind = ? OR temporary_traffic_kind = ? OR stability_state = ?)",
			group, modelName, channelIDs, true, false,
			ChannelSmartScheduleTemporaryTrafficExploration, ChannelSmartScheduleTemporaryTrafficAdaptive,
			ChannelSmartScheduleStabilityProbing,
		).
		Find(&states).Error
	if err != nil {
		return nil, err
	}
	for _, state := range states {
		statesByChannel[state.ChannelId] = state
	}
	return statesByChannel, nil
}

func shouldAvoidChannelSmartScheduleRoute(
	temporaryTrafficKind string,
	stabilityState string,
	explorationMaxPromptTokens int,
	stabilityReleaseMaxPromptTokens int,
	options ChannelSelectionOptions,
) bool {
	if temporaryTrafficKind == ChannelSmartScheduleTemporaryTrafficExploration ||
		temporaryTrafficKind == ChannelSmartScheduleTemporaryTrafficAdaptive {
		return options.ShouldAvoidSmartScheduleRoute(explorationMaxPromptTokens)
	}
	if stabilityState == ChannelSmartScheduleStabilityProbing {
		return options.ShouldAvoidSmartScheduleRoute(stabilityReleaseMaxPromptTokens)
	}
	return false
}

func filterAbilitiesBySmartScheduleRequestLimits(
	abilities []Ability,
	statesByChannel map[int]channelSmartScheduleRequestLimitState,
	options ChannelSelectionOptions,
) []Ability {
	if options.IgnoreSmartScheduleRequestLimits || len(abilities) == 0 || len(statesByChannel) == 0 {
		return abilities
	}
	preferred := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		state, specialRoute := statesByChannel[ability.ChannelId]
		if !specialRoute || !shouldAvoidChannelSmartScheduleRoute(
			state.TemporaryTrafficKind,
			state.StabilityState,
			state.ExplorationMaxPromptTokens,
			state.StabilityReleaseMaxPromptTokens,
			options,
		) {
			preferred = append(preferred, ability)
		}
	}
	if len(preferred) == 0 {
		return abilities
	}
	return preferred
}

func filterChannelSmartScheduleRequestLimits(
	routes []channelSmartScheduleCachedRoute,
	options ChannelSelectionOptions,
) []channelSmartScheduleCachedRoute {
	if options.IgnoreSmartScheduleRequestLimits || len(routes) == 0 || !options.HasRequestSize() {
		return routes
	}
	preferred := make([]channelSmartScheduleCachedRoute, 0, len(routes))
	for _, route := range routes {
		if !shouldAvoidChannelSmartScheduleRoute(
			route.temporaryTrafficKind,
			route.stabilityState,
			route.explorationMaxPromptTokens,
			route.stabilityReleaseMaxPromptTokens,
			options,
		) {
			preferred = append(preferred, route)
		}
	}
	if len(preferred) == 0 {
		return routes
	}
	return preferred
}
