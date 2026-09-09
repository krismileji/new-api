package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// A conflict must abort the transaction even when it is detected after an
// earlier route was written. Apply translates it back to unapplied outcomes.
type channelSmartScheduleRouteConflict struct {
	channelID int
	reason    string
	cause     error
}

func (conflict *channelSmartScheduleRouteConflict) Unwrap() error { return conflict.cause }

func (conflict *channelSmartScheduleRouteConflict) Error() string {
	if conflict.channelID > 0 {
		return fmt.Sprintf("渠道 %d：%s；整池保留上一轮结果", conflict.channelID, conflict.reason)
	}
	return conflict.reason + "；整池保留上一轮结果"
}

func channelSmartScheduleRouteGuardConflict(
	result ChannelSmartScheduleRouteResultUpdate,
	state ChannelSmartScheduleRouteState,
	ability Ability,
	channel Channel,
	controlRevision, economicRevision string,
) string {
	participationSet, excluded := result.ExpectedParticipationSet, result.ExpectedExcluded
	enabled, status := result.ExpectedAbilityEnabled, result.ExpectedChannelStatus
	if !result.PoolGuard {
		participationSet, excluded = true, false
		enabled, status = true, common.ChannelStatusEnabled
	}
	priority, weight := channelSmartScheduleAbilityRouting(ability)
	switch {
	case controlRevision != result.ExpectedControlRevision:
		return fmt.Sprintf("调度配置版本已变化（快照 %q，当前 %q）", result.ExpectedControlRevision, controlRevision)
	case economicRevision != result.ExpectedEconomicRevision:
		return fmt.Sprintf("成本或分组倍率版本已变化（快照 %q，当前 %q）", result.ExpectedEconomicRevision, economicRevision)
	case state.ParticipationSet != participationSet:
		return fmt.Sprintf("调度参与标记已变化（快照 %t，当前 %t）", participationSet, state.ParticipationSet)
	case state.Excluded != excluded:
		return fmt.Sprintf("排除调度设置已变化（快照 %t，当前 %t）", excluded, state.Excluded)
	case channel.Status != status:
		return fmt.Sprintf("渠道状态已变化（快照状态码 %d，当前状态码 %d）", status, channel.Status)
	case ability.Enabled != enabled:
		return fmt.Sprintf("分组和模型路由的启用状态已变化（快照 %t，当前 %t）", enabled, ability.Enabled)
	case priority != result.ExpectedPriority:
		return fmt.Sprintf("路由优先级已变化（快照 %d，当前 %d）", result.ExpectedPriority, priority)
	case weight != result.ExpectedWeight:
		return fmt.Sprintf("路由权重已变化（快照 %d，当前 %d）", result.ExpectedWeight, weight)
	case state.Revision != result.ExpectedRevision:
		if result.ExpectedRevision == 0 && state.Revision > 0 {
			return fmt.Sprintf("调度快照缺少有效路由修订号（快照 0，当前 %d），请检查快照版本并刷新", state.Revision)
		}
		return fmt.Sprintf("路由状态版本已变化（快照 %d，当前 %d）", result.ExpectedRevision, state.Revision)
	default:
		return ""
	}
}
