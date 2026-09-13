package model

// IsEnabled keeps configurations saved before per-group switches enabled.
func (group ChannelGroupMonitorGroup) IsEnabled() bool {
	return group.Enabled == nil || *group.Enabled
}

// EnabledGroups is the ordered probe list; Groups also retains paused groups.
func (config ChannelGroupMonitorConfig) EnabledGroups() ([]ChannelGroupMonitorGroup, error) {
	groups, err := config.Groups()
	if err != nil {
		return nil, err
	}
	enabled := make([]ChannelGroupMonitorGroup, 0, len(groups))
	for _, group := range groups {
		if group.IsEnabled() {
			enabled = append(enabled, group)
		}
	}
	return enabled, nil
}
