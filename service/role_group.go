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
	for _, group := range setting.GetAutoGroups() {
		if _, ok := groups[group]; ok {
			autoGroups = append(autoGroups, group)
		}
	}
	return autoGroups
}
