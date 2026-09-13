package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// The existing TEXT field also stores category order and empty categories.
// Legacy arrays remain readable and are converted only when categories are saved.
type channelGroupMonitorGroupConfiguration struct {
	Categories    []string                   `json:"categories"`
	Groups        []ChannelGroupMonitorGroup `json:"groups"`
	ShowCacheRate bool                       `json:"show_cache_rate,omitempty"`
}

func (config ChannelGroupMonitorConfig) groupConfiguration() (channelGroupMonitorGroupConfiguration, error) {
	configuration := channelGroupMonitorGroupConfiguration{
		Categories: []string{}, Groups: []ChannelGroupMonitorGroup{},
	}
	raw := strings.TrimSpace(config.GroupsJSON)
	if raw != "" {
		var err error
		if strings.HasPrefix(raw, "[") {
			err = common.UnmarshalJsonStr(raw, &configuration.Groups)
		} else {
			err = common.UnmarshalJsonStr(raw, &configuration)
		}
		if err != nil {
			return configuration, fmt.Errorf("解析分组监控配置失败: %w", err)
		}
	}
	seen := make(map[string]bool, len(configuration.Categories))
	for _, category := range configuration.Categories {
		seen[category] = true
	}
	for _, group := range configuration.Groups {
		category := strings.TrimSpace(group.Category)
		if category == "" {
			category = "未分类"
		}
		if !seen[category] {
			configuration.Categories = append(configuration.Categories, category)
			seen[category] = true
		}
	}
	return configuration, nil
}

func (config ChannelGroupMonitorConfig) Categories() ([]string, error) {
	configuration, err := config.groupConfiguration()
	return configuration.Categories, err
}

func (config ChannelGroupMonitorConfig) ShowCacheRate() (bool, error) {
	configuration, err := config.groupConfiguration()
	return configuration.ShowCacheRate, err
}
