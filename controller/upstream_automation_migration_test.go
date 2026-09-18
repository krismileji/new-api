package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAutomationMigrationSkipsEmptyConfig(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			db := setupChannelMonitorCustomActionRefreshDB(t, engine)
			useChannelMonitorOptionMap(t, map[string]string{channelMonitorAutoUpdateIntervalOption: "5"})
			config := automationTestConfig("https://upstream.example")
			raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config.CustomConfig)
			require.NoError(t, err)
			monitors := []model.ChannelRatioMonitor{
				{ChannelId: 81, UpstreamType: "custom", UpstreamBaseURL: config.BaseURL, UpstreamRevision: 1, CustomUpstreamConfig: raw},
				{ChannelId: 82, UpstreamType: "custom", UpstreamRevision: 1, CustomUpstreamConfig: ""},
				{ChannelId: 83, UpstreamType: "custom", UpstreamRevision: 1, CustomUpstreamConfig: " \t\r\n"},
			}
			for _, monitor := range monitors {
				require.NoError(t, db.Create(&model.Channel{Id: monitor.ChannelId, Name: "旧渠道"}).Error)
				require.NoError(t, db.Create(&monitor).Error)
			}
			var taskID string
			for range 2 {
				ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/automations", nil)
				ListUpstreamAutomations(ctx)
				var response struct {
					Success bool                             `json:"success"`
					Data    []service.UpstreamAutomationView `json:"data"`
					Warning string                           `json:"migration_warning"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				require.True(t, response.Success, recorder.Body.String())
				assert.Empty(t, response.Warning, "没有旧规则的空配置不应提示迁移失败")
				require.Len(t, response.Data, 1, "有效旧规则迁移一次，空配置不创建任务")
				assert.Equal(t, []int{81}, response.Data[0].ChannelIDs)
				require.Len(t, response.Data[0].CustomConfig.Actions, 1)
				assert.Equal(t, "reset", response.Data[0].CustomConfig.Actions[0].ID)
				if taskID != "" {
					assert.Equal(t, taskID, response.Data[0].ID)
				}
				taskID = response.Data[0].ID
			}
			for _, before := range monitors[1:] {
				after, err := model.GetChannelRatioMonitor(before.ChannelId)
				require.NoError(t, err)
				assert.Equal(t, before.CustomUpstreamConfig, after.CustomUpstreamConfig)
				assert.Equal(t, before.UpstreamRevision, after.UpstreamRevision)
			}
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 84, UpstreamType: "custom", CustomUpstreamConfig: "invalid"}).Error)
			err = service.MigrateUpstreamAutomations(t.Context(), 5)
			require.ErrorContains(t, err, "渠道 84 配置无效，无法迁移")
			assert.NotContains(t, err.Error(), "渠道 82")
			assert.NotContains(t, err.Error(), "渠道 83")
		})
	}
}
