package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPricingGroupMonitorDisplaysConfiguredCategoriesForEveryRole(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorState{}, &model.ChannelGroupMonitorExecution{},
	))
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":0.03,"vip":1}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"默认分组"}`))
	_, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled: true, Categories: []string{"88", "77", "未分类"},
		Groups: []model.ChannelGroupMonitorGroup{
			{GroupName: "default", ProbeModel: "gpt-4", Category: "88"},
			{GroupName: "调度验收-低成本", ProbeModel: "smart-seed-chat-balanced", Category: "88"},
			{GroupName: "smart-cache-priority-demo", ProbeModel: "cache-demo-model", Category: "77"},
			{GroupName: "smart-cache-weight-demo", ProbeModel: "cache-demo-model", Category: "未分类"},
		},
		IntervalSeconds: 300, DisplayValue: 60, DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
	}, common.GetTimestamp())
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		role int
	}{
		{"root", common.RoleRootUser},
		{"administrator", common.RoleAdminUser},
		{"regular user", common.RoleCommonUser},
		{"guest", common.RoleGuestUser},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/group-monitor", nil)
			c.Set("role", tc.role)
			GetPricingGroupMonitor(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			var payload struct {
				Success bool `json:"success"`
				Data    struct {
					Categories []string         `json:"categories"`
					Items      []map[string]any `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			require.True(t, payload.Success)
			var groupNames, categories []string
			for _, item := range payload.Data.Items {
				groupNames = append(groupNames, item["group"].(string))
				categories = append(categories, item["category"].(string))
				for _, field := range []string{"channel_id", "config_valid", "error_message", "settled_cost_nano_cny"} {
					assert.NotContains(t, item, field)
				}
			}
			assert.Equal(t, []string{"default", "调度验收-低成本", "smart-cache-priority-demo", "smart-cache-weight-demo"}, groupNames)
			assert.Equal(t, []string{"88", "88", "77", "未分类"}, categories)
			assert.Equal(t, []string{"88", "77", "未分类"}, payload.Data.Categories)
			assert.NotContains(t, recorder.Body.String(), "admin_preview")
		})
	}
}

func TestGetPricingGroupMonitorShowsSavedEmptyCategories(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorState{}, &model.ChannelGroupMonitorExecution{},
	))
	_, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Categories: []string{"空分类"}, Groups: []model.ChannelGroupMonitorGroup{},
		IntervalSeconds: 300, DisplayValue: 60, DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
	}, common.GetTimestamp())
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/group-monitor", nil)
	GetPricingGroupMonitor(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Categories []string                          `json:"categories"`
			Items      []pricingGroupMonitorItemResponse `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	assert.Equal(t, []string{"空分类"}, payload.Data.Categories)
	assert.Empty(t, payload.Data.Items)
}

func TestGetPricingGroupMonitorKeepsVisibleGroupsWhenRoutesUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name           string
		channelStatus  int
		abilityEnabled bool
		abilityModel   string
		monitorEnabled bool
		wantStatus     string
	}{
		{
			name: "active route keeps latest health", channelStatus: common.ChannelStatusEnabled,
			abilityEnabled: true, abilityModel: "gpt-4", monitorEnabled: true,
			wantStatus: channelGroupMonitorHealthHealthy,
		},
		{
			name: "manually disabled route stays visible", channelStatus: common.ChannelStatusManuallyDisabled,
			abilityEnabled: false, abilityModel: "gpt-4", monitorEnabled: true,
			wantStatus: channelGroupMonitorHealthUnavailable,
		},
		{
			name: "automatically disabled route stays visible", channelStatus: common.ChannelStatusAutoDisabled,
			abilityEnabled: true, abilityModel: "gpt-4", monitorEnabled: true,
			wantStatus: channelGroupMonitorHealthUnavailable,
		},
		{
			name: "disabled ability stays visible", channelStatus: common.ChannelStatusEnabled,
			abilityEnabled: false, abilityModel: "gpt-4", monitorEnabled: true,
			wantStatus: channelGroupMonitorHealthUnavailable,
		},
		{
			name: "removed probe model stays visible", channelStatus: common.ChannelStatusEnabled,
			abilityEnabled: true, abilityModel: "gpt-4.1", monitorEnabled: true,
			wantStatus: channelGroupMonitorHealthUnavailable,
		},
		{
			name: "paused monitor stays paused without a route", channelStatus: common.ChannelStatusManuallyDisabled,
			abilityEnabled: false, abilityModel: "gpt-4", monitorEnabled: false,
			wantStatus: channelGroupMonitorHealthPaused,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			originalUsableGroups := setting.UserUsableGroups2JSONString()
			t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups)) })
			require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"默认分组"}`))
			require.NoError(t, db.AutoMigrate(
				&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorState{}, &model.ChannelGroupMonitorExecution{},
			))
			require.NoError(t, db.Create(&[]model.Channel{
				{Id: 910, Name: "可见分组渠道", Type: constant.ChannelTypeOpenAI, Status: tc.channelStatus},
				{Id: 911, Name: "受限分组渠道", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled},
			}).Error)
			require.NoError(t, db.Create(&[]model.Ability{
				{Group: "default", Model: tc.abilityModel, ChannelId: 910, Enabled: tc.abilityEnabled},
				{Group: "restricted", Model: "gpt-4", ChannelId: 911, Enabled: true},
			}).Error)
			now := common.GetTimestamp()
			_, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
				Enabled: tc.monitorEnabled,
				Groups: []model.ChannelGroupMonitorGroup{
					{GroupName: "default", ProbeModel: "gpt-4", Category: "通用模型"},
				},
				IntervalSeconds: 300, DisplayValue: 60, DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
			}, now)
			require.NoError(t, err)
			firstToken := 215.0
			_, err = model.SaveChannelGroupMonitorExecution(&model.ChannelGroupMonitorExecution{
				RunId: "previous-probe", GroupName: "default", ProbeModel: "gpt-4", ChannelId: 910,
				Result: model.ChannelGroupMonitorResultSuccess, FirstTokenMs: &firstToken,
				StartedAt: now - 10, FinishedAt: now - 1,
			})
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/group-monitor", nil)
			c.Set("role", common.RoleCommonUser)
			GetPricingGroupMonitor(c)

			require.Equal(t, http.StatusOK, recorder.Code)
			var payload struct {
				Success bool `json:"success"`
				Data    struct {
					Items []pricingGroupMonitorItemResponse `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			require.True(t, payload.Success)
			require.Len(t, payload.Data.Items, 1, "configured visible groups must not disappear when routes fail")
			item := payload.Data.Items[0]
			assert.Equal(t, "default", item.Group)
			assert.Equal(t, "通用模型", item.Category)
			assert.Equal(t, "gpt-4", item.ProbeModel)
			assert.Equal(t, tc.wantStatus, item.Status)
			require.NotNil(t, item.SuccessRate)
			assert.Equal(t, 100.0, *item.SuccessRate)
			assert.Equal(t, now-1, item.LastFinishedAt)
			assert.Equal(t, &firstToken, item.LatestFirstTokenMs)
			assert.Len(t, item.RecentWindow, 60)
			assert.NotContains(t, recorder.Body.String(), "restricted")
			assert.NotContains(t, recorder.Body.String(), "channel_id")
		})
	}
}
