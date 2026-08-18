package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelMonitorAuditContentIsReadable(t *testing.T) {
	tests := []struct {
		name   string
		action string
		params map[string]interface{}
		want   string
	}{
		{
			name: "concurrency", action: "channel.monitor_concurrency_limit_update",
			params: map[string]interface{}{"id": 7, "channel_name": "测试渠道", "channel_label": "测试渠道（ID: 7）", "concurrency_limit": 0},
			want:   "已将渠道 测试渠道（ID: 7）的并发限制更新为 0（0 表示不限制）",
		},
		{
			name: "route pause", action: "channel.monitor_smart_schedule_group_pause_update",
			params: map[string]interface{}{"id": 7, "channel_name": "测试渠道", "channel_label": "测试渠道（ID: 7）", "group": "vip", "model": "gpt-test", "duration_minutes": 30},
			want:   "已将渠道 测试渠道（ID: 7）在分组 vip、模型 gpt-test 的流量暂停时间更新为 30 分钟",
		},
		{
			name: "status probe", action: "channel.status_probe_config_changed",
			params: map[string]interface{}{"channel_id": 7, "channel_name": "测试渠道", "channel_label": "测试渠道（ID: 7）", "status": "开启", "model_count": 2, "interval_seconds": 60},
			want:   "已更新渠道 测试渠道（ID: 7）的状态探测配置（开启，2 个模型，间隔 60 秒）",
		},
		{
			name: "model detection", action: "channel.model_detection_config_update",
			params: map[string]interface{}{"channel_id": 7, "channel_name": "测试渠道", "channel_label": "测试渠道（ID: 7）", "schedule_status": "关闭", "target_count": 3},
			want:   "已更新渠道 测试渠道（ID: 7）的模型检测配置（定时检测：关闭，3 个目标）",
		},
		{
			name: "monitor settings", action: "channel.monitor_settings_changed",
			params: map[string]interface{}{
				"auto_update_interval_minutes": 15,
				"smart_schedule_status":        "开启",
				"email_notification_status":    "关闭",
				"probe_response_status":        "开启",
			},
			want: "已更新渠道监控设置（自动倍率更新间隔 15 分钟，智能调度：开启，邮件通知：关闭，本地探针：开启）",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, auditContentEN(test.action, test.params))
		})
	}
}
