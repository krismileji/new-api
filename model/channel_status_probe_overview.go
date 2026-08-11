package model

// GetChannelsForStatusProbeOverview loads only the channel fields used by the
// status probe overview. Large provider settings and runtime metadata stay out
// of the polling path.
func GetChannelsForStatusProbeOverview() ([]*Channel, error) {
	var channels []*Channel
	err := resolveChannelSortOptions(false, nil).Apply(DB).
		Select("id", "type", "status", "name", "models", commonGroupCol, "remark").
		Find(&channels).Error
	return channels, err
}
