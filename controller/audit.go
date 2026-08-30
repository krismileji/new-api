package controller

import (
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// auditContentTemplates 将稳定的操作标识 action 映射为英文兜底模板，渲染后写入
// Log.Content（供导出等非本地化消费者使用）。占位符为 ${name}，由该
// action 的 params 填充。本地化展示文案在前端 i18n 模板中维护，本表是语言中立的
// 英文基线——调用方因此无需在每个埋点处手写句子（避免与 params 重复书写同一份值）。
var auditContentTemplates = map[string]string{
	"user.create":           "Created user ${username} (role ${role})",
	"user.update":           "Updated user ${username} (ID: ${id})",
	"user.delete":           "Deleted user ${username} (ID: ${id})",
	"user.manage":           "Performed ${action} on user ${username} (ID: ${id})",
	"user.quota_add":        "Increased user quota by ${quota}",
	"user.quota_subtract":   "Decreased user quota by ${quota}",
	"user.quota_override":   "Overrode user quota from ${from} to ${to}",
	"user.binding_clear":    "Cleared ${bindingType} binding for user ${username}",
	"user.2fa_disable":      "Force-disabled two-factor authentication for the user",
	"user.passkey_register": "Registered a passkey",
	"user.passkey_delete":   "Deleted a passkey",
	"user.reset_passkey":    "Reset the user passkey",
	"option.update":         "Updated system setting ${key}",

	"channel.create":             "Created channel ${name} (type ${type}, count ${count})",
	"channel.update":             "Updated channel ${name} (ID: ${id})",
	"channel.delete":             "Deleted channel ${name} (ID: ${id})",
	"channel.delete_batch":       "Batch deleted ${count} channels",
	"channel.delete_disabled":    "Deleted all disabled channels (${count})",
	"channel.key_view":           "Viewed channel key ${name} (ID: ${id})",
	"channel.tag_disable":        "Disabled channels with tag ${tag}",
	"channel.tag_enable":         "Enabled channels with tag ${tag}",
	"channel.tag_edit":           "Edited channels with tag ${tag}",
	"channel.tag_batch_set":      "Batch set tag for ${count} channels",
	"channel.copy":               "Copied channel (source ID: ${sourceId}) to ${name} (new ID: ${id})",
	"channel.multi_key_manage":   "Multi-key management ${action} on channel (ID: ${id})",
	"channel.upstream_apply":     "Applied upstream model changes to channel (ID: ${id})",
	"channel.upstream_apply_all": "Applied upstream model changes to ${count} channels",

	"redemption.create": "Created ${count} redemption codes named ${name} (${quota} each)",

	"subscription.plan_reset":      "Reset active subscriptions for plan ${plan_id}",
	"subscription.user_plan_reset": "Reset active plan ${plan_id} subscriptions for user ${target_user_id}",
}

// channelMonitorAuditContentTemplates 是自定义渠道监控功能的固定中文日志模板。
// 该页面仅供内部使用，不跟随系统语言切换。
var channelMonitorAuditContentTemplates = map[string]string{
	"channel.status_update":                                  "已将渠道 ${channel_label} 的状态更新为 ${status}",
	"channel.status_update_batch":                            "已将 ${count} 个渠道的状态更新为 ${status}",
	"channel.status_changed":                                 "已${status_label}渠道 ${channel_label}",
	"channel.status_changed_batch":                           "已${status_label} ${count} 个渠道",
	"channel.monitor_concurrency_limit_update":               "已将渠道 ${channel_label} 的并发限制更新为 ${concurrency_limit}、RPM 限制更新为 ${rpm_limit}（0 表示不限制）",
	"channel.monitor_smart_schedule_config_update":           "已将渠道 ${channel_label} 在分组 ${group}、模型 ${model} 的主渠道固定时间更新为 ${duration_minutes} 分钟",
	"channel.monitor_smart_schedule_stability_clear":         "已手动解除渠道 ${channel_label} 的稳定性保护，恢复优先级 ${priority}、权重 ${weight}",
	"channel.monitor_smart_schedule_channel_config_update":   "已更新渠道 ${channel_label} 的智能调度参与设置（影响 ${updated} 条路由）",
	"channel.monitor_smart_schedule_route_config_update":     "已更新渠道 ${channel_label} 在分组 ${group}、模型 ${model} 的智能调度参与设置",
	"channel.monitor_smart_schedule_group_pause_update":      "已将渠道 ${channel_label} 在分组 ${group}、模型 ${model} 的流量暂停时间更新为 ${duration_minutes} 分钟",
	"channel.monitor_smart_schedule_route_stability_clear":   "已解除渠道 ${channel_label} 在分组 ${group}、模型 ${model} 的稳定性保护，恢复优先级 ${priority}、权重 ${weight}",
	"channel.monitor_smart_schedule_route_exploration_clear": "已解除渠道 ${channel_label} 在分组 ${group}、模型 ${model} 的临时探索状态，恢复优先级 ${priority}、权重 ${weight}",
	"channel.monitor_group_ratio_sync":                       "已根据成本倍率 ${cost_ratio}（上游倍率 ${upstream_ratio} × 换算系数 ${conversion_factor}）和分组系数 ${coefficient}，将分组 ${group} 的倍率更新为 ${ratio}",
	"channel.monitor_group_ratio_update":                     "已将分组 ${group} 的倍率更新为 ${ratio}",
	"channel.monitor_group_channels_update":                  "已更新分组 ${group} 的关联渠道（新增 ${added_count} 个，移除 ${removed_count} 个）",
	"channel.monitor_ratio_update":                           "已将渠道 ${channel_label} 的倍率更新为 ${ratio}",
	"channel.monitor_ratio_update_run":                       "已启动上游倍率更新任务 ${task_id}",
	"channel.monitor_upstream_config_update":                 "已更新渠道 ${channel_label} 的上游配置（${upstream_type_label}，成本换算：${cost_conversion}，换算系数 ${conversion_factor}）",
	"channel.monitor_upstream_ratio_fetch":                   "已获取渠道 ${channel_label} 的上游倍率 ${ratio}，换算后成本倍率 ${cost_ratio}（系数 ${conversion_factor}）",
	"channel.monitor_upstream_balance_fetch":                 "已获取渠道 ${channel_label} 的上游余额 ${balance}",
	"channel.monitor_upstream_group_apply":                   "已将上游分组 ${group} 应用于渠道 ${channel_label}（已更新 ${keys_updated} 个令牌，上游倍率 ${ratio}，成本倍率 ${cost_ratio}）",
	"channel.monitor_smart_schedule_run":                     "已启动智能调度任务 ${task_id}",
	"channel.monitor_order_update":                           "已更新 ${channel_count} 个监控渠道的自定义顺序",
	"channel.monitor_settings_update":                        "已更新渠道监控设置",
	"channel.monitor_settings_changed":                       "已更新渠道监控设置（自动倍率更新间隔 ${auto_update_interval_minutes} 分钟，智能调度：${smart_schedule_status}，邮件通知：${email_notification_status}，本地探针：${probe_response_status}）",
	"channel.status_probe_config_update":                     "已更新渠道 ${channel_label} 的状态探测配置（启用：${enabled}，间隔 ${interval_seconds} 秒）",
	"channel.status_probe_config_changed":                    "已更新渠道 ${channel_label} 的状态探测配置（${status}，${model_count} 个模型，间隔 ${interval_seconds} 秒）",
	"channel.status_probe_run":                               "已请求立即探测渠道 ${channel_label}（请求 ${manual_request_id}）",
	"channel.model_detection_settings_update":                "已更新模型检测设置（定时检测：${schedule_status}，预设：${scheduled_preset}，间隔 ${interval_minutes} 分钟）",
	"channel.model_detection_config_update":                  "已更新渠道 ${channel_label} 的模型检测配置（定时检测：${schedule_status}，${target_count} 个目标）",
}

var channelMonitorRateLimitAuditContentTemplates = map[string]string{
	"channel.monitor_smart_schedule_rate_limit_cooldown_update": "已将渠道 ${channel_label} 在分组 ${group}、模型 ${model} 的 429 限制暂停时间更新为 ${duration_minutes} 分钟",
}

// auditContentEN 渲染日志兜底文本；渠道监控使用固定中文，其余操作使用英文基线。
// 未登记的 action 退回 action 本身。
func auditContentEN(action string, params map[string]interface{}) string {
	tmpl, ok := channelMonitorAuditContentTemplates[action]
	if !ok {
		tmpl, ok = channelMonitorRateLimitAuditContentTemplates[action]
	}
	if !ok {
		tmpl, ok = auditContentTemplates[action]
	}
	if !ok {
		return action
	}
	if _, hasChannelName := params["channel_name"]; hasChannelName {
		tmpl = strings.ReplaceAll(tmpl, "${channel_label} ", "${channel_label}")
	}
	return os.Expand(tmpl, func(key string) string {
		if v, ok := params[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	})
}

// auditOperatorInfo 从上下文构建操作者身份信息（管理员 id/用户名/角色）。
func auditOperatorInfo(c *gin.Context) map[string]interface{} {
	return map[string]interface{}{
		"admin_id":       c.GetInt("id"),
		"admin_username": c.GetString("username"),
		"admin_role":     c.GetInt("role"),
		"auth_method":    auditAuthMethod(c),
	}
}

func auditAuthMethod(c *gin.Context) string {
	if c.GetBool("use_access_token") {
		return "access_token"
	}
	return "session"
}

// markAuditLogged 标记当前请求已在 handler 内手动记录审计日志，
// 使鉴权链路中的审计兜底（finishAdminAudit）跳过兜底记录，避免重复。
func markAuditLogged(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
}

// recordManageAudit 记录一条由操作者本人归属的管理/高危审计日志（资源类操作：
// 渠道 / 系统设置 / 兑换码等）。content 由 action+params 自动渲染。
func recordManageAudit(c *gin.Context, action string, params map[string]interface{}) {
	recordManageAuditFor(c, c.GetInt("id"), action, params)
}

// recordManageAuditFor 记录一条管理审计日志，日志归属于操作者；targetUserId
// 只表示被操作用户，用于在结构化参数中保留目标上下文。
func recordManageAuditFor(c *gin.Context, targetUserId int, action string, params map[string]interface{}) {
	if params == nil {
		params = map[string]interface{}{}
	}
	_, isChannelMonitorAction := channelMonitorAuditContentTemplates[action]
	if !isChannelMonitorAction {
		_, isChannelMonitorAction = channelMonitorRateLimitAuditContentTemplates[action]
	}
	if isChannelMonitorAction {
		channelId, _ := params["channel_id"].(int)
		if channelId == 0 {
			channelId, _ = params["id"].(int)
		}
		if channelId > 0 {
			params["channel_label"] = channelId
			if channel, err := model.GetChannelById(channelId, false); err == nil && channel.Name != "" {
				params["channel_name"] = channel.Name
				params["channel_label"] = fmt.Sprintf("%s（ID: %d）", channel.Name, channelId)
			}
		}
	}
	operatorUserId := c.GetInt("id")
	if _, ok := params["target_user_id"]; !ok && targetUserId > 0 && targetUserId != operatorUserId {
		params["target_user_id"] = targetUserId
	}
	model.RecordOperationAuditLog(operatorUserId, auditContentEN(action, params), c.ClientIP(), action, params, auditOperatorInfo(c), nil)
	markAuditLogged(c)
}

// recordUserSecurityAudit 记录普通用户自己的安全敏感操作（如 passkey 绑定/解绑）。
// 这类日志没有管理员操作者，不写 admin_info；同时不依赖 AdminAuth/RootAuth 的兜底。
func recordUserSecurityAudit(c *gin.Context, userId int, action string, params map[string]interface{}) {
	model.RecordOperationAuditLog(userId, auditContentEN(action, params), c.ClientIP(), action, params, nil, nil)
}
