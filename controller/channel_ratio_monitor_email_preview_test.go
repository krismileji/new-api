package controller

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

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

func TestBuildChannelRatioMonitorNotificationEmailIncludesTaskTimeoutDetails(t *testing.T) {
	startedAt := time.Date(2026, 11, 20, 10, 4, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	failures := []channelRatioMonitorTaskFailure{
		{
			ChannelId: 81, ChannelName: "unavailable-group", Kind: model.ChannelRatioFailureAlertRatio,
			Error: "Sub2API 当前账号不可见分组",
		},
		{
			ChannelId: 82, ChannelName: "slow & ratio", ChannelRemark: "<备用>", Kind: model.ChannelRatioFailureAlertRatio,
			Error: "Sub2API 读取用户专属倍率失败: context deadline exceeded",
		},
	}
	for _, test := range []struct {
		name        string
		types       []string
		alerts      []channelRatioMonitorTaskFailure
		wantSubject string
	}{
		{
			name:        "task failure only",
			types:       []string{channelMonitorEmailTypeTaskFailed},
			alerts:      []channelRatioMonitorTaskFailure{},
			wantSubject: "渠道监控：定时更新任务超时",
		},
		{
			name:        "a channel also reaches the alert threshold",
			types:       []string{channelMonitorEmailTypeTaskFailed, channelMonitorEmailTypeUpstreamSyncFailed},
			alerts:      failures[:1],
			wantSubject: "渠道监控：定时更新任务超时，1 项更新失败",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary := channelRatioMonitorTaskResult{
				Total: 70, BalanceUpdated: 31, Failed: len(failures), Skipped: 34,
				Failures: failures, notificationFailures: test.alerts,
				notificationTiming: channelRatioMonitorTaskTiming{
					startedAt: startedAt,
					deadline:  startedAt.Add(time.Minute).UTC(),
					elapsed:   63 * time.Second,
				},
			}
			subject, content := buildChannelRatioMonitorNotificationEmail(test.types, nil, nil, nil, nil, summary, context.DeadlineExceeded)
			assert.Equal(t, test.wantSubject, subject)
			assert.Contains(t, content, "2026-11-20 10:04:00 UTC+08:00，耗时 63.0 秒")
			assert.Contains(t, content, "本轮截止时间为 2026-11-20 10:05:00 UTC+08:00")
			assert.Contains(t, content, "计划检查 70 个渠道，已刷新 31 个渠道的余额")
			assert.NotContains(t, content, "本轮渠道失败明细")
			assert.NotContains(t, content, "slow &amp; ratio（ID: 82）")
			assert.NotContains(t, content, "&lt;备用&gt;")
			assert.NotContains(t, content, "Sub2API 读取用户专属倍率失败: 请求超时")
			assert.Equal(t, len(test.alerts), strings.Count(content, "unavailable-group（ID: 81）"))
			assert.NotContains(t, content, context.DeadlineExceeded.Error())
		})
	}
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
	assert.Contains(t, response.Data.Subject, "定时更新任务超时")
	assert.Contains(t, response.Data.HTML, "上游余额预警")
	assert.Contains(t, response.Data.HTML, "本轮截止时间")
	assert.NotContains(t, response.Data.HTML, "示例渠道 F（ID: 1006）")
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

func TestPreviewChannelMonitorNotificationEmailIncludesSelectedScheduleFailure(t *testing.T) {
	for _, types := range [][]string{{"smart_schedule_failed"}, {"balance_warning", "smart_schedule_failed"}} {
		t.Run(types[0], func(t *testing.T) {
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/settings/email-preview", map[string]any{
				"notification_types": types,
			})
			PreviewChannelMonitorNotificationEmail(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Data struct {
					Subject string `json:"subject"`
					HTML    string `json:"html"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Contains(t, response.Data.Subject, "智能调度失败")
			assert.Contains(t, response.Data.HTML, "配置冲突")
			assert.Contains(t, response.Data.HTML, "路由状态版本已变化")
			assert.Contains(t, response.Data.HTML, "model-a")
			assert.NotContains(t, response.Data.HTML, "上游同步失败")
			if len(types) > 1 {
				assert.Contains(t, response.Data.HTML, "上游余额预警")
			}
		})
	}
}

func TestPreviewChannelMonitorNotificationEmailShowsMonitoringHealth(t *testing.T) {
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/settings/email-preview", map[string]any{
		"notification_types": []string{channelMonitorEmailTypeMonitoringHealth},
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
	assert.Equal(t, []string{channelMonitorEmailTypeMonitoringHealth}, response.Data.NotificationTypes)
	assert.Equal(t, "渠道监控异常：Redis 操作超时、成本记录保存延迟", response.Data.Subject)
	assert.Contains(t, response.Data.HTML, "原因：</strong>Redis 操作超时；成本记录保存延迟。")
	assert.Contains(t, response.Data.HTML, "建议：</strong>")
	assert.NotContains(t, response.Data.HTML, "累计丢弃")
	assert.NotContains(t, response.Data.HTML, "redis_context_deadline")
	assert.NotContains(t, response.Data.HTML, "available")
	assert.NotContains(t, response.Data.HTML, "渠道倍率变更")
}
