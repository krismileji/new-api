package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelRatioMonitorNotificationEmailUsesSelectedTypes(t *testing.T) {
	summary := channelRatioMonitorTaskResult{
		Failed:            1,
		GroupUpdateFailed: true,
		Failures: []channelRatioMonitorTaskFailure{{
			ChannelId: 6, ChannelName: "failed", Error: "timeout",
		}},
	}
	subject, content := buildChannelRatioMonitorNotificationEmail(
		[]string{channelMonitorEmailTypeRatioChange, channelMonitorEmailTypeBalanceWarning},
		[]channelRatioMonitorEmailChange{{ChannelId: 1, ChannelName: "ratio", OldRatio: 1, NewRatio: 2}},
		[]channelRatioMonitorBalanceWarning{{ChannelId: 2, ChannelName: "balance", Balance: 5, Threshold: 10}},
		[]channelRatioMonitorDisabledChannel{{ChannelId: 3, ChannelName: "disabled"}},
		[]channelRatioMonitorRemovedGroupMembership{{ChannelId: 4, ChannelName: "removed", Group: "vip"}},
		summary,
		assert.AnError,
	)

	assert.Equal(t, "渠道监控：1 个倍率变更，1 个余额预警", subject)
	assert.Contains(t, content, "渠道倍率变更")
	assert.Contains(t, content, "上游余额预警")
	assert.Contains(t, content, `name="color-scheme" content="light only"`)
	assert.Contains(t, content, "background:#ffffff;color:#111827")
	assert.NotContains(t, content, "渠道自动禁用")
	assert.NotContains(t, content, "渠道移出分组")
	assert.NotContains(t, content, "上游同步失败")
	assert.NotContains(t, content, "分组倍率更新失败")
}

func TestPreviewChannelMonitorNotificationEmailMatchesSelectedTypes(t *testing.T) {
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/settings/email-preview", map[string]any{
		"notification_types": []string{channelMonitorEmailTypeBalanceWarning, channelMonitorEmailTypeTaskFailed},
	})
	PreviewChannelMonitorNotificationEmail(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Subject           string   `json:"subject"`
			HTML              string   `json:"html"`
			NotificationTypes []string `json:"notification_types"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, []string{channelMonitorEmailTypeBalanceWarning, channelMonitorEmailTypeTaskFailed}, response.Data.NotificationTypes)
	assert.Contains(t, response.Data.Subject, "1 个余额预警")
	assert.Contains(t, response.Data.Subject, "1 项更新失败")
	assert.Contains(t, response.Data.HTML, "上游余额预警")
	assert.Contains(t, response.Data.HTML, "分组倍率更新失败")
	assert.NotContains(t, response.Data.HTML, "渠道倍率变更")
	assert.NotContains(t, response.Data.HTML, "渠道自动禁用")
}

func TestPreviewChannelMonitorNotificationEmailRejectsEmptyTypes(t *testing.T) {
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/settings/email-preview", map[string]any{
		"notification_types": []string{},
	})
	PreviewChannelMonitorNotificationEmail(ctx)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
