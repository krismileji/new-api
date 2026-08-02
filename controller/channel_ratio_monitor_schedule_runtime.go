package controller

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

func isChannelSmartScheduleRuntimeFailure(err *types.NewAPIError) bool {
	if err == nil || types.IsSkipRetryError(err) || err.StatusCode == http.StatusTooManyRequests {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	switch err.StatusCode {
	case 408, 425:
		return true
	default:
		return err.StatusCode >= 500 && err.StatusCode <= 599
	}
}

// protectChannelSmartScheduleRuntimeFailure is intentionally called from the
// relay error path. It protects every configured group that exposes the same
// channel/model route because the upstream failure is shared across groups.
func protectChannelSmartScheduleRuntimeFailure(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	if err == nil || types.IsSkipRetryError(err) {
		return
	}
	modelName = strings.TrimSpace(modelName)
	if err.StatusCode == http.StatusTooManyRequests {
		settings := getChannelMonitorSettings()
		if channelId > 0 && modelName != "" && settings.SmartScheduleEnabled &&
			settings.SmartScheduleRateLimitCooldownSeconds > 0 {
			service.StartChannelRateLimitCooldown(
				channelId,
				modelName,
				settings.SmartScheduleRateLimitCooldownSeconds,
			)
		}
		return
	}
	if channelId <= 0 || !isChannelSmartScheduleRuntimeFailure(err) {
		return
	}
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled || len(settings.SmartScheduleGroupPolicies) == 0 {
		return
	}
	if modelName == "" {
		return
	}
	reason := "渠道运行时错误，稳定性样本已达到最少样本数，已停止临时流量并进入稳定性保护"
	if message := strings.TrimSpace(err.MaskSensitiveErrorWithStatusCode()); message != "" {
		reason += "：" + message
	}
	now := common.GetTimestamp()
	stabilityWindowMinutes := settings.SmartScheduleStabilityWindowMinutes
	if stabilityWindowMinutes <= 0 {
		stabilityWindowMinutes = 1
	}
	sampleWindowStart := now - int64(stabilityWindowMinutes)*60
	routingChanged := false
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		if len(policy.Models) > 0 && !slices.Contains(policy.Models, modelName) {
			continue
		}
		sampleCount, sampleErr := model.GetChannelSmartScheduleRouteSampleCount(
			context.Background(), sampleWindowStart, channelId, modelName,
		)
		if sampleErr != nil {
			common.SysError("智能调度运行时错误样本统计失败: " + sampleErr.Error())
			continue
		}
		if sampleCount < int64(policy.MinSamples) {
			continue
		}
		cooldownMinutes := policy.CooldownMinutes
		if cooldownMinutes <= 0 {
			cooldownMinutes = 1
		}
		result, protectErr := model.ProtectChannelSmartScheduleRouteOnRuntimeFailure(
			channelId,
			configured.Group,
			modelName,
			now+int64(cooldownMinutes)*60,
			reason,
		)
		if protectErr != nil {
			common.SysError("智能调度运行时错误保护失败: " + protectErr.Error())
			continue
		}
		routingChanged = routingChanged || result.RoutingChanged
	}
	if routingChanged {
		model.InitChannelCache()
	}
}
