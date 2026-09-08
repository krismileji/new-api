package service

import (
	"fmt"
	"html"
	"slices"
	"strings"
	"time"
)

// BuildChannelMonitorHealthNotificationEmail is shared by delivery and preview.
// Redis availability alone does not establish that monitoring is healthy.
func BuildChannelMonitorHealthNotificationEmail(status string, reasons []string, dropped int64, observedAt time.Time) (string, string) {
	return buildChannelMonitorHealthEmail(status, reasons, dropped, observedAt, "", "")
}

func buildChannelMonitorHealthEmail(status string, reasons []string, dropped int64, observedAt time.Time, action, nodeID string) (string, string) {
	labels := make([]string, 0, len(reasons))
	checkTargets := make([]string, 0, len(reasons))
	unknownCodes := make([]string, 0)
	seen := make(map[string]bool, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" || seen[reason] {
			continue
		}
		seen[reason] = true
		label, checkTarget := channelMonitorHealthReasonSummary(reason)
		if label == "" {
			label = "未分类监控异常"
			unknownCodes = append(unknownCodes, reason)
		}
		labels = append(labels, label)
		if !slices.Contains(checkTargets, checkTarget) {
			checkTargets = append(checkTargets, checkTarget)
		}
	}

	title := "监控统计需要检查"
	if len(labels) > 0 {
		title = strings.Join(labels[:min(2, len(labels))], "、")
		if len(labels) > 2 {
			title += fmt.Sprintf("等 %d 项问题", len(labels))
		}
	}
	if status == string(ChannelMonitorHealthUnavailable) || seen[ChannelMonitorRedisDegradedReasonRedisUnavailable] {
		title = "实时监控暂不可用"
	}
	subject := "渠道监控异常：" + title
	if len(labels) == 0 {
		labels = append(labels, "未提供具体异常原因")
		checkTargets = append(checkTargets, "监控服务日志")
	}

	var content strings.Builder
	content.WriteString(`<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light"></head><body style="margin:0;background:#ffffff;color:#111827;font-family:Arial,'Microsoft YaHei',sans-serif;font-size:14px;line-height:1.7"><div style="max-width:680px;margin:0 auto;padding:16px;overflow-wrap:anywhere;word-break:break-word">`)
	fmt.Fprintf(&content, "<p><strong>原因：</strong>%s。</p>", html.EscapeString(strings.Join(labels, "；")))
	content.WriteString("<p><strong>影响：</strong>监控统计可能延迟或不完整，不代表接口调用失败。</p>")
	if action != "" {
		fmt.Fprintf(&content, "<p><strong>处理：</strong>%s</p>", html.EscapeString(action))
	} else {
		fmt.Fprintf(&content, "<p><strong>建议：</strong>若持续出现，请检查%s。</p>", html.EscapeString(strings.Join(checkTargets, "、")))
	}
	if dropped > 0 {
		fmt.Fprintf(&content, "<p>累计丢弃的监控记录：<strong>%d 条</strong>（当前节点累计，非失败请求数）。</p>", dropped)
	}
	if len(unknownCodes) > 0 {
		fmt.Fprintf(&content, `<p>未分类异常代码：<code style="word-break:break-all">%s</code></p>`, html.EscapeString(strings.Join(unknownCodes, "、")))
	}
	fmt.Fprintf(&content, `<p style="color:#4b5563">时间：%s`, html.EscapeString(observedAt.Format("2006-01-02 15:04:05 UTC-07:00")))
	if nodeID != "" {
		fmt.Fprintf(&content, "<br>节点：%s", html.EscapeString(nodeID))
	}
	content.WriteString("</p>")
	content.WriteString("</div></body></html>")
	return subject, content.String()
}

func channelMonitorHealthReasonSummary(reason string) (label, checkTarget string) {
	switch reason {
	case ChannelMonitorRedisDegradedReasonRedisUnavailable:
		return "实时监控暂不可用", "Redis 连接和负载"
	case ChannelMonitorRedisDegradedReasonConsumerStopped:
		return "统计处理服务暂未运行", "监控后台任务和应用日志"
	case ChannelMonitorRedisDegradedReasonConsumerGroupMissing:
		return "统计处理尚未准备好", "监控后台任务和应用日志"
	case ChannelMonitorRedisDegradedReasonEventBacklog:
		return "统计更新有延迟", "监控后台任务和 Redis 负载"
	case ChannelMonitorRedisDegradedReasonPublisherUnavailable:
		return "监控记录写入失败", "Redis 连接和负载"
	case ChannelMonitorRedisDegradedReasonMarkerReleaseFailure:
		return "监控处理后的清理失败", "Redis 写入权限和监控清理日志"
	case ChannelMonitorRedisDegradedReasonStreamTrimFailure:
		return "监控队列清理失败", "Redis 写入权限和监控清理日志"
	case ChannelMonitorRedisDegradedReasonWriterQueueFull:
		// Page snapshots also use this code for historical sample drops.
		return "部分监控记录被丢弃", "监控采集队列和丢弃计数"
	case ChannelMonitorRedisDegradedReasonWriterStopped:
		return "监控采集服务暂未运行", "监控后台任务和应用日志"
	case ChannelMonitorRedisDegradedReasonSerializationError:
		return "监控记录格式异常", "监控记录编码失败日志"
	case ChannelMonitorRedisDegradedReasonCostStreamBacklog:
		return "成本统计更新有延迟", "成本后台任务和 Redis 负载"
	case ChannelMonitorRedisDegradedReasonCostOutboxBacklog:
		return "成本记录保存延迟", "成本后台任务和数据库连接"
	case ChannelMonitorRedisDegradedReasonCostPublishFailure:
		return "成本记录同步失败", "Redis 写入和成本同步日志"
	case ChannelMonitorRedisDegradedReasonCostDeadLetter:
		return "部分成本记录需要人工排查", "被隔离的成本记录及错误日志"
	case ChannelMonitorRedisDegradedReasonPoolCongested:
		return "监控连接繁忙", "Redis 连接池和负载"
	case ChannelMonitorRedisDegradedReasonPoolTimeout:
		return "等待监控连接超时", "Redis 连接池和负载"
	case ChannelMonitorRedisDegradedReasonContextDeadline:
		return "Redis 操作超时", "Redis 连接和负载"
	case "samples_dropped":
		return "部分监控记录缺失", "监控采集队列和丢弃计数"
	case "daily_replay_incomplete":
		return "历史统计恢复尚未完成", "每日统计恢复任务和日志"
	case "cost_projection_pending":
		return "成本统计正在追平", "成本后台任务"
	case "cost_projection_unavailable":
		return "成本统计暂不可用", "成本后台任务和 Redis 连接"
	case "health_observation_failed":
		return "监控状态检查失败", "Redis、数据库连接和健康检查日志"
	case "cost_worker_stopped":
		return "成本后台任务未运行", "成本后台任务和应用日志"
	case "events_quarantined":
		return "部分监控记录需要人工复核", "被隔离的监控记录及错误日志"
	default:
		return "", "监控服务日志"
	}
}
