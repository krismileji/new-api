package controller

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/model"
)

func normalizeChannelGroupMonitorCategories(raw []string, groups []model.ChannelGroupMonitorGroup) ([]string, error) {
	if len(raw) > model.ChannelGroupMonitorMaxGroups {
		return nil, errors.New("监控分类不能超过 100 个")
	}
	categories := make([]string, 0, len(raw))
	positions := make(map[string]int, len(raw))
	for index, name := range raw {
		name = strings.TrimSpace(name)
		if name == "" || utf8.RuneCountInString(name) > 64 {
			return nil, errors.New("分类名称不能为空且不能超过 64 个字符")
		}
		if _, exists := positions[name]; exists {
			return nil, errors.New("分类名称不能重复")
		}
		positions[name] = index
		categories = append(categories, name)
	}
	// Empty category names in older group records mean the 未分类 category.
	if position, exists := positions["未分类"]; exists {
		positions[""] = position
	}
	for _, group := range groups {
		if _, exists := positions[group.Category]; !exists {
			return nil, errors.New("请先创建监控分组所属的分类")
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return positions[groups[i].Category] < positions[groups[j].Category]
	})
	return categories, nil
}
