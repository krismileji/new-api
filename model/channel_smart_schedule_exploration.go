package model

type channelSmartScheduleExplorationState struct {
	ChannelId                  int
	ExplorationMaxPromptTokens int
}

func loadChannelSmartScheduleExplorationStates(
	group string,
	modelName string,
	channelIDs []int,
) (map[int]channelSmartScheduleExplorationState, error) {
	statesByChannel := make(map[int]channelSmartScheduleExplorationState)
	if DB == nil || len(channelIDs) == 0 || !DB.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		return statesByChannel, nil
	}
	var states []channelSmartScheduleExplorationState
	err := DB.Model(&ChannelSmartScheduleRouteState{}).
		Select("channel_id", "exploration_max_prompt_tokens").
		Where(
			"group_name = ? AND model_name = ? AND channel_id IN ? AND participation_set = ? AND excluded = ? AND temporary_traffic_kind = ?",
			group, modelName, channelIDs, true, false, ChannelSmartScheduleTemporaryTrafficExploration,
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

func filterAbilitiesByExplorationRequest(
	abilities []Ability,
	statesByChannel map[int]channelSmartScheduleExplorationState,
	options ChannelSelectionOptions,
) []Ability {
	if len(abilities) == 0 || len(statesByChannel) == 0 {
		return abilities
	}
	stable := make([]Ability, 0, len(abilities))
	for _, ability := range abilities {
		state, exploring := statesByChannel[ability.ChannelId]
		if !exploring || !options.ShouldAvoidExploration(state.ExplorationMaxPromptTokens) {
			stable = append(stable, ability)
		}
	}
	if len(stable) == 0 {
		return abilities
	}
	return stable
}

func filterChannelSmartScheduleExplorationRoutes(
	routes []channelSmartScheduleCachedRoute,
	options ChannelSelectionOptions,
) []channelSmartScheduleCachedRoute {
	if len(routes) == 0 || !options.HasRequestSize() {
		return routes
	}
	stable := make([]channelSmartScheduleCachedRoute, 0, len(routes))
	for _, route := range routes {
		if route.temporaryTrafficKind != ChannelSmartScheduleTemporaryTrafficExploration ||
			!options.ShouldAvoidExploration(route.explorationMaxPromptTokens) {
			stable = append(stable, route)
		}
	}
	if len(stable) == 0 {
		return routes
	}
	return stable
}
