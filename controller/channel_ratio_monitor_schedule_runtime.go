package controller

import (
	"context"
	"net/http"
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

func isChannelSmartScheduleUpstreamRateLimit(result testResult) bool {
	return result.requestDispatched && result.newAPIError != nil &&
		!types.IsSkipRetryError(result.newAPIError) &&
		result.newAPIError.StatusCode == http.StatusTooManyRequests
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
	requestModelName := strings.TrimSpace(modelName)
	if err.StatusCode == http.StatusTooManyRequests {
		settings := getChannelMonitorSettings()
		if channelId > 0 && requestModelName != "" && settings.SmartScheduleEnabled &&
			settings.SmartScheduleRateLimitCooldownSeconds > 0 {
			participatingRoutes, routeErr := model.GetChannelSmartScheduleRuntimeParticipatingRoutes(
				channelId, requestModelName,
			)
			if routeErr != nil {
				common.SysError("智能调度 429 参与路由读取失败: " + routeErr.Error())
				return
			}
			for _, configured := range settings.SmartScheduleGroupPolicies {
				routeModelName, participating := participatingRoutes[configured.Group]
				if !participating {
					continue
				}
				policy := configured.policy()
				if len(policy.Models) > 0 {
					matched := false
					for _, configuredModelName := range policy.Models {
						if configuredModelName == routeModelName {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}
				service.StartChannelRateLimitCooldownIfControlRevision(
					channelId,
					requestModelName,
					settings.SmartScheduleRateLimitCooldownSeconds,
					settings.SmartScheduleControlRevision,
				)
				break
			}
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
	if requestModelName == "" {
		return
	}
	activeRoutes, routeErr := model.GetChannelSmartScheduleRuntimeTemporaryRoutes(channelId, requestModelName)
	if routeErr != nil {
		common.SysError("智能调度运行时临时路由读取失败: " + routeErr.Error())
		return
	}
	if len(activeRoutes) == 0 {
		return
	}
	type matchingPolicyRoute struct {
		configured  channelSmartScheduleGroupPolicy
		modelName   string
		sampleSince int64
	}
	matchingPolicies := make([]matchingPolicyRoute, 0, len(settings.SmartScheduleGroupPolicies))
	for _, configured := range settings.SmartScheduleGroupPolicies {
		route, active := activeRoutes[configured.Group]
		if !active {
			continue
		}
		policy := configured.policy()
		if len(policy.Models) > 0 {
			matched := false
			for _, configuredModelName := range policy.Models {
				if configuredModelName == route.ModelName {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		matchingPolicies = append(matchingPolicies, matchingPolicyRoute{
			configured:  configured,
			modelName:   route.ModelName,
			sampleSince: route.SampleSince,
		})
	}
	if len(matchingPolicies) == 0 {
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
	protectionApplied := false
	sampleCountByStart := make(map[int64]int64)
	for _, matched := range matchingPolicies {
		policy := matched.configured.policy()
		sampleStart := max(sampleWindowStart, matched.sampleSince)
		sampleCount, exists := sampleCountByStart[sampleStart]
		if !exists {
			var sampleErr error
			sampleCount, sampleErr = model.GetChannelSmartScheduleRouteSampleCount(
				context.Background(), sampleStart, channelId, requestModelName,
			)
			if sampleErr != nil {
				common.SysError("智能调度运行时错误样本统计失败: " + sampleErr.Error())
				return
			}
			sampleCountByStart[sampleStart] = sampleCount
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
			matched.configured.Group,
			matched.modelName,
			now+int64(cooldownMinutes)*60,
			reason,
			settings.SmartScheduleControlRevision,
		)
		if protectErr != nil {
			common.SysError("智能调度运行时错误保护失败: " + protectErr.Error())
			continue
		}
		protectionApplied = protectionApplied || result.Handled
		routingChanged = routingChanged || result.RoutingChanged
	}
	if protectionApplied {
		bridgeSeconds := common.SyncFrequency + 5
		if common.SyncFrequency <= 0 {
			bridgeSeconds = 65
		}
		service.StartChannelRateLimitCooldownIfControlRevision(
			channelId, requestModelName, bridgeSeconds, settings.SmartScheduleControlRevision,
		)
	}
	if routingChanged {
		model.InitChannelCache()
	}
}
