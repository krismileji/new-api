package controller

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func recordManualChannelSmartScheduleProbeResult(
	channel *model.Channel,
	result testResult,
	durationMs float64,
) {
	if channel == nil || !result.requestDispatched ||
		math.IsNaN(durationMs) || math.IsInf(durationMs, 0) || durationMs < 0 {
		return
	}
	modelName := strings.TrimSpace(result.originalModelName)
	if modelName == "" || !channelSmartScheduleSupportsTextProbe(channel, modelName) {
		return
	}

	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return
	}
	probeTime := common.GetTimestamp()
	windowStart := probeTime - int64(settings.SmartSchedulePerformanceMinutes*60)
	succeeded := result.localErr == nil && result.newAPIError == nil
	message := ""
	if result.localErr != nil {
		message = result.localErr.Error()
	} else if result.newAPIError != nil {
		message = result.newAPIError.Error()
	}

	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		if policy.SampleMode != channelMonitorSmartScheduleSampleProbe ||
			(len(policy.Models) > 0 && !slices.Contains(policy.Models, modelName)) {
			continue
		}
		route, _, found, err := model.LookupChannelSmartScheduleProbeRoute(
			channel.Id,
			configured.Group,
			modelName,
		)
		if err != nil {
			common.SysError(fmt.Sprintf(
				"保存手动渠道测试探测样本前读取路由失败: channel_id=%d group=%s model=%s err=%s",
				channel.Id, configured.Group, modelName, err.Error(),
			))
			continue
		}
		if !found || route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled ||
			!route.State.Participates() ||
			route.State.StabilityState == model.ChannelSmartScheduleStabilityDegraded ||
			(route.State.StabilityState != "" && route.State.StabilityState != model.ChannelSmartScheduleStabilityProbing) {
			continue
		}
		routeWindowStart := windowStart
		if policy.StabilityEnabled && route.State.StabilitySince > routeWindowStart {
			routeWindowStart = route.State.StabilitySince
		}
		_, err = model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
			ChannelId:    channel.Id,
			Group:        configured.Group,
			Model:        modelName,
			WindowStart:  routeWindowStart,
			Time:         probeTime,
			Success:      succeeded,
			Error:        message,
			DurationMs:   &durationMs,
			FirstTokenMs: result.firstResponseMilliseconds,
			TPS:          result.tokensPerSecond,
		})
		if err != nil {
			common.SysError(fmt.Sprintf(
				"保存手动渠道测试探测样本失败: channel_id=%d group=%s model=%s err=%s",
				channel.Id, configured.Group, modelName, err.Error(),
			))
		}
	}
}
