package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelGroupMonitorSettingsRetainsConfigurationAcrossPauseAndResume(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorConfig{}))
	require.NoError(t, db.Create(&model.Channel{
		Id: 920, Name: "分组监控开关测试", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-4.1",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "gpt-4.1", ChannelId: 920, Enabled: true,
	}).Error)

	revision := int64(0)
	for _, enabled := range []*bool{nil, common.GetPointer(false), common.GetPointer(true)} {
		group := model.ChannelGroupMonitorGroup{
			GroupName: "default", ProbeModel: "gpt-4.1", DisplayInitial: "组", Category: "通用模型", Enabled: enabled,
		}
		body, err := common.Marshal(map[string]any{
			"enabled": true, "groups": []model.ChannelGroupMonitorGroup{group},
			"categories":       []string{"通用模型", "预留分类"},
			"interval_seconds": 60, "display_value": 60, "display_unit": "minute", "revision": revision,
		})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/channel_monitor/group_monitor/settings", bytes.NewReader(body))
		UpdateChannelGroupMonitorSettings(c)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

		recorder = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/group_monitor/settings", nil)
		GetChannelGroupMonitorSettings(c)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Settings channelGroupMonitorConfigResponse `json:"settings"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		assert.Equal(t, []model.ChannelGroupMonitorGroup{group}, response.Data.Settings.Groups)
		assert.Equal(t, []string{"通用模型", "预留分类"}, response.Data.Settings.Categories)
		assert.Equal(t, revision+1, response.Data.Settings.Revision)
		revision = response.Data.Settings.Revision

		if enabled != nil && !*enabled {
			recorder = httptest.NewRecorder()
			c, _ = gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/channel_monitor/group_monitor/run", nil)
			RunChannelGroupMonitorNow(c)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "请先保存并启用至少一个监控分组")
		}
	}
}

func TestChannelGroupMonitorPausedGroupKeepsHistoryAndStatusWithoutAValidRoute(t *testing.T) {
	for _, validRoute := range []bool{true, false} {
		t.Run(map[bool]string{true: "available route", false: "unavailable route"}[validRoute], func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(
				&model.ChannelGroupMonitorConfig{}, &model.ChannelGroupMonitorState{}, &model.ChannelGroupMonitorExecution{},
			))
			require.NoError(t, db.Create(&model.Channel{
				Id: 921, Name: "分组监控展示测试", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&[]model.Ability{
				{Group: "vip", Model: "gpt-4.1", ChannelId: 921, Enabled: validRoute},
				{Group: "default", Model: "gpt-4.1", ChannelId: 921, Enabled: true},
			}).Error)
			now := common.GetTimestamp()
			config, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
				Enabled: true,
				Groups: []model.ChannelGroupMonitorGroup{
					{GroupName: "vip", ProbeModel: "gpt-4.1", Category: "通用模型", Enabled: common.GetPointer(false)},
					{GroupName: "default", ProbeModel: "gpt-4.1", Category: "通用模型"},
				},
				IntervalSeconds: 60, DisplayValue: 60, DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
			}, now)
			require.NoError(t, err)
			for _, groupName := range []string{"vip", "default"} {
				_, err = model.SaveChannelGroupMonitorExecution(&model.ChannelGroupMonitorExecution{
					RunId: "previous-run", GroupName: groupName, ProbeModel: "gpt-4.1",
					Result: model.ChannelGroupMonitorResultSuccess, StartedAt: now - 2, FinishedAt: now - 1,
				})
				require.NoError(t, err)
			}
			candidates, err := getChannelGroupMonitorCandidateModels(context.Background(), true)
			require.NoError(t, err)
			items, err := buildChannelGroupMonitorItems(context.Background(), config, candidates, now)
			require.NoError(t, err)
			require.Len(t, items, 2)
			assert.Equal(t, channelGroupMonitorHealthPaused, items[0].Status)
			assert.Equal(t, channelGroupMonitorHealthHealthy, items[1].Status)
			assert.Equal(t, validRoute, items[0].ConfigValid)

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/group-monitor", nil)
			GetPricingGroupMonitor(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Items []pricingGroupMonitorItemResponse `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			require.Len(t, response.Data.Items, 2)
			paused := response.Data.Items[0]
			assert.Equal(t, "vip", paused.Group)
			assert.Equal(t, "通用模型", paused.Category)
			assert.Equal(t, "gpt-4.1", paused.ProbeModel)
			assert.Equal(t, channelGroupMonitorHealthPaused, paused.Status)
			require.NotNil(t, paused.SuccessRate)
			assert.Equal(t, 100.0, *paused.SuccessRate)
			assert.Equal(t, now-1, paused.LastFinishedAt)
			assert.Equal(t, channelGroupMonitorHealthHealthy, response.Data.Items[1].Status)
		})
	}
}
