package controller

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

func recordManualChannelSmartScheduleProbeResult(
	channel *model.Channel,
	result testResult,
	durationMs float64,
) {
	recordManualChannelSmartScheduleProbeResultForGroup(channel, result, durationMs, "")
}

func recordManualChannelSmartScheduleProbeResultForGroup(
	channel *model.Channel,
	result testResult,
	durationMs float64,
	group string,
) {
	if channel == nil {
		return
	}
	group = strings.TrimSpace(group)
	modelName := strings.TrimSpace(result.originalModelName)
	if modelName == "" && result.context != nil {
		modelName = strings.TrimSpace(common.GetContextKeyString(result.context, constant.ContextKeyOriginalModel))
	}
	if modelName == "" {
		return
	}

	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return
	}
	probeTime := common.GetTimestamp()
	windowStart := probeTime - int64(settings.SmartSchedulePerformanceMinutes*60)
	sampleId := ""
	if result.context != nil {
		sampleId = strings.TrimSpace(result.context.GetString(common.RequestIdKey))
		if sampleId == "" && result.context.Request != nil {
			if value, ok := result.context.Request.Context().Value(common.RequestIdKey).(string); ok {
				sampleId = strings.TrimSpace(value)
			}
		}
	}
	succeeded := result.localErr == nil && result.newAPIError == nil
	message := ""
	if result.localErr != nil {
		message = result.localErr.Error()
	} else if result.newAPIError != nil {
		message = result.newAPIError.Error()
	}

	for _, configured := range settings.SmartScheduleGroupPolicies {
		if group != "" && configured.Group != group {
			continue
		}
		policy := configured.policy()
		if len(policy.Models) > 0 && !slices.Contains(policy.Models, modelName) {
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
			!route.State.Participates() {
			continue
		}
		routeWindowStart := windowStart
		if policy.StabilityEnabled && route.State.StabilitySince > routeWindowStart {
			routeWindowStart = route.State.StabilitySince
		}
		var sampleDurationMs *float64
		if !math.IsNaN(durationMs) && !math.IsInf(durationMs, 0) && durationMs >= 0 {
			value := durationMs
			sampleDurationMs = &value
		}
		_, err = model.SaveChannelSmartScheduleProbeResult(model.ChannelSmartScheduleProbeResult{
			ChannelId:    channel.Id,
			Group:        configured.Group,
			Model:        modelName,
			Source:       model.ChannelSmartScheduleSampleSourceManualTest,
			SampleId:     sampleId,
			WindowStart:  routeWindowStart,
			Time:         probeTime,
			Success:      succeeded,
			Error:        message,
			DurationMs:   sampleDurationMs,
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
