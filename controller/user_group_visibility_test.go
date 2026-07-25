package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type userGroupsVisibilityResponse struct {
	Success bool `json:"success"`
	Data    map[string]struct {
		Ratio any    `json:"ratio"`
		Desc  string `json:"desc"`
	} `json:"data"`
}

func preserveUserGroupVisibilitySettings(t *testing.T) {
	t.Helper()

	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
	})

	ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Clear()
}

func decodeUserGroupsVisibilityResponse(t *testing.T, recorder *httptest.ResponseRecorder) userGroupsVisibilityResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload userGroupsVisibilityResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload
}

func TestGetUserGroupsAdminIncludesEveryConfiguredGroup(t *testing.T) {
	preserveUserGroupVisibilitySettings(t)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"hidden":1.75}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default group"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["hidden","default"]`))

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       2101,
		Username: "admin-group-visibility-user",
		Password: "password",
		Group:    "default",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/groups", nil)
	context.Set("id", 2101)
	context.Set("role", common.RoleAdminUser)

	GetUserGroups(context)

	payload := decodeUserGroupsVisibilityResponse(t, recorder)
	require.Contains(t, payload.Data, "default")
	require.Contains(t, payload.Data, "hidden")
	assert.Equal(t, 1.75, payload.Data["hidden"].Ratio)
	assert.Equal(t, "hidden", payload.Data["hidden"].Desc)
	assert.NotContains(t, payload.Data, "auto")
}

func TestGetUserGroupsCommonUserStillUsesConfiguredUsableGroups(t *testing.T) {
	preserveUserGroupVisibilitySettings(t)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"hidden":1.75}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default group"}`))

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       2102,
		Username: "common-group-visibility-user",
		Password: "password",
		Group:    "default",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	}).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/user/self/groups", nil)
	context.Set("id", 2102)
	context.Set("role", common.RoleCommonUser)

	GetUserGroups(context)

	payload := decodeUserGroupsVisibilityResponse(t, recorder)
	assert.Equal(t, "Default group", payload.Data["default"].Desc)
	assert.NotContains(t, payload.Data, "hidden")
}

func TestGetUserModelsAdminIncludesEveryConfiguredGroup(t *testing.T) {
	preserveUserGroupVisibilitySettings(t)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"hidden":1.75}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default group"}`))

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       2201,
		Username: "admin-model-visibility-user",
		Password: "password",
		Group:    "default",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-default-visible-model", ChannelId: 1, Enabled: true},
		{Group: "hidden", Model: "zz-hidden-admin-model", ChannelId: 2, Enabled: true},
		{Group: "not-configured", Model: "zz-unconfigured-model", ChannelId: 3, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/user/models", nil)
	context.Set("id", 2201)
	context.Set("role", common.RoleAdminUser)

	GetUserModels(context)

	models := decodeUserModelsResponse(t, recorder)
	assert.ElementsMatch(t, []string{"zz-default-visible-model", "zz-hidden-admin-model"}, models)
	assert.NotContains(t, models, "zz-unconfigured-model")
}

func TestGetUserModelsPreservesAutoVisibilityWhileExpandingAdminGroups(t *testing.T) {
	preserveUserGroupVisibilitySettings(t)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"hidden":1.75}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"auto":"Auto group","default":"Default group"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["hidden","default","not-configured"]`))

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.User{
		{
			Id:       2202,
			Username: "root-auto-model-visibility-user",
			Password: "password",
			AffCode:  "root-auto-2202",
			Group:    "default",
			Role:     common.RoleRootUser,
			Status:   common.UserStatusEnabled,
		},
		{
			Id:       2203,
			Username: "common-auto-model-visibility-user",
			Password: "password",
			AffCode:  "common-auto-2203",
			Group:    "default",
			Role:     common.RoleCommonUser,
			Status:   common.UserStatusEnabled,
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "hidden", Model: "zz-hidden-auto-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-default-auto-model", ChannelId: 2, Enabled: true},
		{Group: "not-configured", Model: "zz-unconfigured-auto-model", ChannelId: 3, Enabled: true},
	}).Error)

	adminRecorder := httptest.NewRecorder()
	adminContext, _ := gin.CreateTestContext(adminRecorder)
	adminContext.Request = httptest.NewRequest(http.MethodGet, "/api/user/models?group=auto", nil)
	adminContext.Set("id", 2202)
	adminContext.Set("role", common.RoleRootUser)

	GetUserModels(adminContext)

	assert.Equal(t, []string{"zz-hidden-auto-model", "zz-default-auto-model"}, decodeUserModelsResponse(t, adminRecorder))

	commonRecorder := httptest.NewRecorder()
	commonContext, _ := gin.CreateTestContext(commonRecorder)
	commonContext.Request = httptest.NewRequest(http.MethodGet, "/api/user/models?group=auto", nil)
	commonContext.Set("id", 2203)
	commonContext.Set("role", common.RoleCommonUser)

	GetUserModels(commonContext)

	assert.Equal(t, []string{"zz-default-auto-model"}, decodeUserModelsResponse(t, commonRecorder))
}
