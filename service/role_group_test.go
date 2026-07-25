package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRoleUsableGroupsAdministratorsIncludeConfiguredGroups(t *testing.T) {
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"internal":2}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default group"}`))

	commonGroups := GetRoleUsableGroups("default", common.RoleCommonUser)
	assert.Contains(t, commonGroups, "default")
	assert.NotContains(t, commonGroups, "internal")

	adminGroups := GetRoleUsableGroups("default", common.RoleAdminUser)
	assert.Equal(t, "Default group", adminGroups["default"])
	assert.Equal(t, "internal", adminGroups["internal"])
	assert.NotContains(t, adminGroups, "auto")

	rootGroups := GetRoleUsableGroups("default", common.RoleRootUser)
	assert.Contains(t, rootGroups, "internal")
}

func TestGetRoleAutoGroupsAdministratorsBypassSelectableGroupFilter(t *testing.T) {
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalAutoGroups := setting.AutoGroups2JsonString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"internal":2}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"auto":"Auto group","default":"Default group"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["internal","default"]`))

	assert.Equal(t, []string{"default"}, GetRoleAutoGroups("default", common.RoleCommonUser))
	assert.Equal(t, []string{"internal", "default"}, GetRoleAutoGroups("default", common.RoleAdminUser))
	assert.True(t, GroupInRoleUsableGroups("default", common.RoleAdminUser, "internal"))
	assert.False(t, GroupInRoleUsableGroups("default", common.RoleCommonUser, "internal"))
}
