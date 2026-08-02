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
) (bool, string) {
	return recordManualChannelSmartScheduleProbeResultForGroup(channel, result, durationMs, "")
}

func recordManualChannelSmartScheduleProbeResultForGroup(
	channel *model.Channel,
	result testResult,
	durationMs float64,
	group string,
) (bool, string) {
	if channel == nil {
		return false, "渠道不可用，未计入智能调度样本"
	}
	group = strings.TrimSpace(group)
	modelName := strings.TrimSpace(result.originalModelName)
	if modelName == "" && result.context != nil {
		modelName = strings.TrimSpace(common.GetContextKeyString(result.context, constant.ContextKeyOriginalModel))
	}
	if modelName == "" {
		return false, "无法确定测试模型，未计入智能调度样本"
	}

	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return false, "智能调度未启用，本次未计入样本"
	}
	probeTime := common.GetTimestamp()
	retentionMinutes := max(
		settings.SmartSchedulePerformanceWindowMinutes,
		settings.SmartScheduleStabilityWindowMinutes,
	)
	windowStart := probeTime - int64(retentionMinutes*60)
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

	eligibleGroup := ""
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
		eligibleGroup = configured.Group
		break
	}
	if eligibleGroup == "" {
		return false, "该渠道模型没有参与智能调度的路由，本次未计入样本"
	}
	var sampleDurationMs *float64
	if !math.IsNaN(durationMs) && !math.IsInf(durationMs, 0) && durationMs >= 0 {
		value := durationMs
		sampleDurationMs = &value
	}
	_, err := model.SaveChannelSmartScheduleModelSample(model.ChannelSmartScheduleModelSampleResult{
		ChannelId:    channel.Id,
		Model:        modelName,
		Source:       model.ChannelSmartScheduleSampleSourceManualTest,
		SampleId:     sampleId,
		WindowStart:  windowStart,
		Time:         probeTime,
		Success:      succeeded,
		Error:        message,
		DurationMs:   sampleDurationMs,
		FirstTokenMs: result.firstResponseMilliseconds,
		TPS:          result.tokensPerSecond,
	})
	if err != nil {
		common.SysError(fmt.Sprintf(
			"保存手动渠道测试共享样本失败: channel_id=%d model=%s matched_group=%s err=%s",
			channel.Id, modelName, eligibleGroup, err.Error(),
		))
		return false, "样本保存失败，请查看服务端日志"
	}
	return true, "已计入渠道 + 模型共享样本"
}
