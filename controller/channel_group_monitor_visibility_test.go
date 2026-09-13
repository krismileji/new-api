package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
					{GroupName: "restricted", ProbeModel: "gpt-4", Category: "隐藏分类"},
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
			assert.NotContains(t, recorder.Body.String(), "隐藏分类")
			assert.NotContains(t, recorder.Body.String(), "channel_id")
		})
	}
}
