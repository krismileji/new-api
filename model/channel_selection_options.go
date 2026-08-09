package model

import "gorm.io/gorm"

// ChannelSelectionOptions carries request-scoped channel exclusions without
// changing the existing selector call sites.
type ChannelSelectionOptions struct {
	ExcludedChannelIds    []int
	EstimatedPromptTokens int
	RequestBodyBytes      int64
	// IgnoreSmartScheduleRequestLimits is used only for the fallback pass
	// after all non-limited candidates have been tried. It never changes the
	// configured route state or the effective priority and weight.
	IgnoreSmartScheduleRequestLimits bool
}

const (
	ChannelSmartSchedulePromptTokensPerK                       = 1_000
	DefaultChannelSmartScheduleExplorationMaxPromptTokens      = 50_000
	DefaultChannelSmartScheduleStabilityReleaseMaxPromptTokens = 0
	MaxChannelSmartScheduleExplorationPromptTokens             = 1_000_000
	requestBodyBytesPerPromptToken                             = 3
)

func (options ChannelSelectionOptions) HasExcludedChannels() bool {
	return len(options.ExcludedChannelIds) > 0
}

func (options ChannelSelectionOptions) HasRequestSize() bool {
	return options.EstimatedPromptTokens > 0 || options.RequestBodyBytes > 0
}

func (options ChannelSelectionOptions) ShouldAvoidSmartScheduleRoute(maxPromptTokens int) bool {
	if !options.HasRequestSize() {
		return false
	}
	if maxPromptTokens <= 0 {
		return false
	}
	if maxPromptTokens > MaxChannelSmartScheduleExplorationPromptTokens {
		maxPromptTokens = MaxChannelSmartScheduleExplorationPromptTokens
	}
	if options.EstimatedPromptTokens > maxPromptTokens {
		return true
	}
	return options.RequestBodyBytes > int64(maxPromptTokens)*requestBodyBytesPerPromptToken
}

// ShouldAvoidExploration is kept for callers compiled against the original
// exploration-only selector API. A zero limit now follows the shared
// smart-schedule rule and means unlimited.
func (options ChannelSelectionOptions) ShouldAvoidExploration(maxPromptTokens int) bool {
	return options.ShouldAvoidSmartScheduleRoute(maxPromptTokens)
}

func channelSelectionOptions(options []ChannelSelectionOptions) ChannelSelectionOptions {
	if len(options) == 0 {
		return ChannelSelectionOptions{}
	}
	return options[len(options)-1]
}

func filterChannelIDsBySelectionOptions(channelIDs []int, options ChannelSelectionOptions) []int {
	if len(channelIDs) == 0 || !options.HasExcludedChannels() {
		return channelIDs
	}
	excluded := make(map[int]struct{}, len(options.ExcludedChannelIds))
	for _, channelID := range options.ExcludedChannelIds {
		excluded[channelID] = struct{}{}
	}
	filtered := make([]int, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if _, ok := excluded[channelID]; !ok {
			filtered = append(filtered, channelID)
		}
	}
	return filtered
}

func applyChannelSelectionOptions(query *gorm.DB, options ChannelSelectionOptions) *gorm.DB {
	if query == nil || !options.HasExcludedChannels() {
		return query
	}
	return query.Where("channel_id NOT IN ?", options.ExcludedChannelIds)
}
