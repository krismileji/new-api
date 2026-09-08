package service

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildChannelMonitorHealthNotificationEmailExplainsReasons(t *testing.T) {
	tests := []struct {
		reason, title, checkTarget string
	}{
		{"redis_unavailable", "实时监控暂不可用", "Redis 连接和负载"},
		{"consumer_stopped", "统计处理服务暂未运行", "监控后台任务和应用日志"},
		{"consumer_group_missing", "统计处理尚未准备好", "监控后台任务和应用日志"},
		{"event_backlog", "统计更新有延迟", "监控后台任务和 Redis 负载"},
		{"publisher_unavailable", "监控记录写入失败", "Redis 连接和负载"},
		{"marker_release_failure", "监控处理后的清理失败", "Redis 写入权限和监控清理日志"},
		{"stream_trim_failure", "监控队列清理失败", "Redis 写入权限和监控清理日志"},
		{"writer_queue_full", "部分监控记录被丢弃", "监控采集队列和丢弃计数"},
		{"writer_stopped", "监控采集服务暂未运行", "监控后台任务和应用日志"},
		{"serialization_error", "监控记录格式异常", "监控记录编码失败日志"},
		{"cost_stream_backlog", "成本统计更新有延迟", "成本后台任务和 Redis 负载"},
		{"cost_outbox_backlog", "成本记录保存延迟", "成本后台任务和数据库连接"},
		{"cost_publish_failure", "成本记录同步失败", "Redis 写入和成本同步日志"},
		{"cost_dead_letter", "部分成本记录需要人工排查", "被隔离的成本记录及错误日志"},
		{"redis_pool_congested", "监控连接繁忙", "Redis 连接池和负载"},
		{"redis_pool_timeout", "等待监控连接超时", "Redis 连接池和负载"},
		{"redis_context_deadline", "Redis 操作超时", "Redis 连接和负载"},
		{"samples_dropped", "部分监控记录缺失", "监控采集队列和丢弃计数"},
		{"daily_replay_incomplete", "历史统计恢复尚未完成", "每日统计恢复任务和日志"},
		{"cost_projection_pending", "成本统计正在追平", "成本后台任务"},
		{"cost_projection_unavailable", "成本统计暂不可用", "成本后台任务和 Redis 连接"},
	}
	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			subject, content := BuildChannelMonitorHealthNotificationEmail("available", []string{tt.reason}, 0, time.Unix(0, 0))
			require.Equal(t, "渠道监控异常："+tt.title, subject)
			assert.Contains(t, content, "原因：</strong>"+tt.title)
			assert.Contains(t, content, "若持续出现，请检查"+tt.checkTarget)
			assert.NotContains(t, content, tt.reason)
			assert.Contains(t, content, "监控统计可能延迟或不完整，不代表接口调用失败")
		})
	}
}

func TestBuildChannelMonitorHealthNotificationEmailKeepsAbnormalSummaryWhenRedisConnects(t *testing.T) {
	for _, status := range []string{"available", "healthy", "degraded"} {
		t.Run(status, func(t *testing.T) {
			subject, content := BuildChannelMonitorHealthNotificationEmail(status, []string{
				" redis_context_deadline ", "cost_outbox_backlog", "redis_context_deadline", "",
			}, 0, time.Date(2026, 9, 8, 14, 30, 0, 0, time.FixedZone("CST", 8*60*60)))
			require.Equal(t, "渠道监控异常：Redis 操作超时、成本记录保存延迟", subject)
			assert.Equal(t, 1, strings.Count(content, "Redis 操作超时"))
			assert.Contains(t, content, "原因：</strong>Redis 操作超时；成本记录保存延迟。")
			assert.Contains(t, content, "建议：</strong>若持续出现，请检查Redis 连接和负载、成本后台任务和数据库连接。")
			assert.Contains(t, content, "2026-09-08 14:30:00 UTC+08:00")
			assert.NotContains(t, content, "累计丢弃")
			assert.NotContains(t, content, status)
		})
	}
}

func TestBuildChannelMonitorHealthNotificationEmailHandlesUnknownAndMissingDetails(t *testing.T) {
	t.Run("unknown reason is readable and escaped", func(t *testing.T) {
		reason := `<script>alert("unexpected")</script>&unknown_reason`
		status := `<img src=x onerror="alert(1)">`
		subject, content := BuildChannelMonitorHealthNotificationEmail(status, []string{reason}, 0, time.Unix(0, 0))
		require.Equal(t, "渠道监控异常：未分类监控异常", subject)
		assert.Contains(t, content, "请检查监控服务日志")
		assert.Contains(t, content, html.EscapeString(reason))
		assert.NotContains(t, content, reason)
		assert.NotContains(t, content, status)
		assert.NotContains(t, content, html.EscapeString(status))
	})
	t.Run("unavailable with no reason", func(t *testing.T) {
		subject, content := BuildChannelMonitorHealthNotificationEmail("unavailable", nil, 0, time.Unix(0, 0))
		require.Equal(t, "渠道监控异常：实时监控暂不可用", subject)
		assert.Contains(t, content, "未提供具体异常原因")
		assert.NotContains(t, content, "累计丢弃")
		assert.Contains(t, content, "监控统计可能延迟或不完整")
	})
	t.Run("dropped samples are cumulative rather than failed requests", func(t *testing.T) {
		subject, content := BuildChannelMonitorHealthNotificationEmail("degraded", []string{"samples_dropped"}, 12, time.Unix(0, 0))
		require.Equal(t, "渠道监控异常：部分监控记录缺失", subject)
		assert.Contains(t, content, "累计丢弃的监控记录：<strong>12 条</strong>")
		assert.Contains(t, content, "当前节点累计，非失败请求数")
	})
}
