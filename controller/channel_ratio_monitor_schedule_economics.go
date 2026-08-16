package controller

import (
	"context"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

const (
	channelSmartScheduleEconomicRoleUnknown           = "unknown"
	channelSmartScheduleEconomicRoleNormal            = "normal"
	channelSmartScheduleEconomicRoleBreakEvenFallback = "break_even_fallback"
)

type channelSmartScheduleEconomics struct {
	CostRatio    *float64
	GroupRatio   *float64
	GrossMargin  *float64
	EconomicRole string
}

func requestChannelSmartScheduleRun(ctx context.Context) error {
	return requestChannelSmartScheduleRunWithSource(ctx, channelSmartScheduleTriggerFallback, "smart_schedule_input_changed")
}

func requestChannelSmartScheduleRunWithSource(ctx context.Context, source string, dirtyReasons ...string) error {
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled || len(settings.SmartScheduleGroupPolicies) == 0 {
		return nil
	}
	payload := newChannelSmartScheduleTaskPayload(source, dirtyReasons...)
	_, _, err := service.EnqueueRequiredSystemTask(channelMonitorSmartScheduleTaskType, payload)
	if err == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogWarn(ctx, fmt.Sprintf("智能调度输入已变化，但请求调度任务失败: %v", err))
	return err
}

func channelSmartScheduleClassifyEconomics(
	monitor model.ChannelRatioMonitor,
	monitorAvailable bool,
	groupRatio float64,
	groupRatioAvailable bool,
) channelSmartScheduleEconomics {
	economics := channelSmartScheduleEconomics{
		EconomicRole: channelSmartScheduleEconomicRoleUnknown,
	}
	if groupRatioAvailable && validateChannelMonitorRatio(&groupRatio) {
		economics.GroupRatio = channelSmartScheduleCopyFloat(&groupRatio)
	}
	if !monitorAvailable || monitor.UpdatedTime <= 0 || !validateChannelMonitorRatio(&monitor.Ratio) {
		return economics
	}
	costRatio, _, err := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
	if err != nil || !validateChannelMonitorRatio(&costRatio) {
		return economics
	}
	economics.CostRatio = channelSmartScheduleCopyFloat(&costRatio)
	if economics.GroupRatio == nil {
		return economics
	}
	grossMargin := *economics.GroupRatio - costRatio
	economics.GrossMargin = channelSmartScheduleCopyFloat(&grossMargin)
	switch {
	case math.Abs(grossMargin) <= channelMonitorRatioEpsilon:
		economics.EconomicRole = channelSmartScheduleEconomicRoleBreakEvenFallback
	case grossMargin > channelMonitorRatioEpsilon:
		economics.EconomicRole = channelSmartScheduleEconomicRoleNormal
	}
	return economics
}

func channelSmartScheduleCandidateEconomics(
	candidate channelSmartScheduleCandidate,
) channelSmartScheduleEconomics {
	costRatio := candidate.CostRatio
	if costRatio == nil {
		costRatio = candidate.Ratio
	}
	role := candidate.EconomicRole
	if role == "" {
		role = channelSmartScheduleEconomicRoleUnknown
	}
	return channelSmartScheduleEconomics{
		CostRatio:    channelSmartScheduleCopyFloat(costRatio),
		GroupRatio:   channelSmartScheduleCopyFloat(candidate.GroupRatio),
		GrossMargin:  channelSmartScheduleCopyFloat(candidate.GrossMargin),
		EconomicRole: role,
	}
}
