package model

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// clearRemovedChannelSmartSchedulePolicyRoutesTx runs under the settings and
// control-revision locks. Removing a policy withdraws all routing intent for
// that group, including models outside its former model filter.
func clearRemovedChannelSmartSchedulePolicyRoutesTx(tx *gorm.DB, previousRaw, currentRaw string) (bool, error) {
	type groupPolicy struct {
		Group string `json:"group"`
	}
	var previous, current []groupPolicy
	if strings.TrimSpace(previousRaw) != "" {
		if err := common.UnmarshalJsonStr(previousRaw, &previous); err != nil {
			// Do not prevent repairing malformed settings or guess which groups
			// they used to own and erase unrelated routing configuration.
			common.SysError("旧智能调度策略无法解析，跳过删除分组清理: " + err.Error())
			previous = nil
		}
	}
	if err := common.UnmarshalJsonStr(currentRaw, &current); err != nil {
		return false, err
	}
	currentGroups := make(map[string]bool, len(current))
	for _, policy := range current {
		currentGroups[strings.TrimSpace(policy.Group)] = true
	}
	removedGroups := make([]string, 0)
	for _, policy := range previous {
		group := strings.TrimSpace(policy.Group)
		if group != "" && !currentGroups[group] {
			removedGroups = append(removedGroups, group)
		}
	}
	if len(removedGroups) == 0 {
		return false, nil
	}
	sort.Strings(removedGroups)

	// Channel locks serialize this cleanup with channel/ability edits. Include
	// disabled abilities so their old overrides cannot return on re-enable.
	var channelIDs []int
	if err := tx.Model(&Ability{}).Where(commonGroupCol+" IN ?", removedGroups).
		Distinct("channel_id").Pluck("channel_id", &channelIDs).Error; err != nil {
		return false, err
	}
	if _, err := lockChannelsForDependentWriteTx(tx, channelIDs); err != nil {
		return false, err
	}
	if tx.Migrator().HasTable(&ChannelSmartScheduleRouteState{}) {
		var states []ChannelSmartScheduleRouteState
		if err := lockForUpdate(tx).Where("group_name IN ?", removedGroups).
			Order("channel_id ASC, group_name ASC, model_name ASC").Find(&states).Error; err != nil {
			return false, err
		}
	}
	// NULL removes the Ability override and makes ordinary selection inherit
	// the channel's current priority and weight, not a historical scheduler value.
	result := tx.Model(&Ability{}).Where(commonGroupCol+" IN ?", removedGroups).
		Where("priority IS NOT NULL OR weight <> ?", 0).
		Updates(map[string]any{"priority": nil, "weight": 0})
	if result.Error != nil {
		return false, result.Error
	}
	changed := result.RowsAffected > 0
	for _, table := range []any{
		&ChannelSmartScheduleRouteState{},
		&ChannelSmartScheduleGroupPause{},
		&ChannelLogicalSmartScheduleRouteState{},
		&ChannelLogicalSmartScheduleSampleState{},
	} {
		if !tx.Migrator().HasTable(table) {
			continue
		}
		result := tx.Where("group_name IN ?", removedGroups).Delete(table)
		if result.Error != nil {
			return false, result.Error
		}
		changed = changed || result.RowsAffected > 0
	}
	return changed, nil
}
