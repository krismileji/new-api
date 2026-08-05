package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// GetRoleUsableGroups returns the selectable groups for regular users and all
// configured ratio groups for administrators. The auto pseudo-group keeps its
// existing UserUsableGroups opt-in behavior.
func GetRoleUsableGroups(userGroup string, role int) map[string]string {
	groups := GetUserUsableGroups(userGroup)
	if role < common.RoleAdminUser {
		return groups
	}

	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := groups[group]; ok {
			continue
		}
		groups[group] = setting.GetUsableGroupDescription(group)
	}
	return groups
}

// GroupInRoleUsableGroups reports whether a group is available to the role.
func GroupInRoleUsableGroups(userGroup string, role int, groupName string) bool {
	_, ok := GetRoleUsableGroups(userGroup, role)[groupName]
	return ok
}

// GetRoleAutoGroups returns auto-group targets available to the role.
func GetRoleAutoGroups(userGroup string, role int) []string {
	groups := GetRoleUsableGroups(userGroup, role)
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range setting.GetAutoGroups() {
		if group == "" || group == "auto" || !ratio_setting.ContainsGroupRatio(group) {
			continue
		}
		if _, ok := groups[group]; !ok {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

// FilterRoleTokenAutoGroups applies current role permissions before the
// current per-token limit without falling back to the global Auto list.
func FilterRoleTokenAutoGroups(userGroup string, role int, groups []string) []string {
	maxCount := setting.GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	usableGroups := GetRoleUsableGroups(userGroup, role)
	seen := make(map[string]struct{})
	for _, group := range groups {
		if group == "" || group == "auto" || !ratio_setting.ContainsGroupRatio(group) {
			continue
		}
		if _, ok := usableGroups[group]; !ok {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}
