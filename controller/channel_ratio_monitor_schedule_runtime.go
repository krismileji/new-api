package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

type channelSmartScheduleRuntimeHealthSnapshot struct {
	ConsecutiveFailures int
	FailureTimes        []int64
	RecoverySuccesses   int
}

type channelSmartScheduleRuntimeHealthState struct {
	Revision            string
	FailureTimes        []int64
	ConsecutiveFailures int
	RecoverySuccesses   int
}

type channelSmartScheduleRuntimeSuccessMode uint8

const (
	channelSmartScheduleRuntimeRequestSuccess channelSmartScheduleRuntimeSuccessMode = iota
	channelSmartScheduleRuntimeRecoveryProbeSuccess
	channelSmartScheduleRuntimeRegularProbeSuccess
)

var channelSmartScheduleRuntimeHealth = struct {
	sync.Mutex
	database any
	states   map[string]channelSmartScheduleRuntimeHealthState
}{
	states: make(map[string]channelSmartScheduleRuntimeHealthState),
}

func channelSmartScheduleRuntimeHealthKey(channelId int, modelName string) string {
	return strings.TrimSpace(modelName) + "#" + strings.TrimSpace(strconv.Itoa(channelId))
}

func resetChannelSmartScheduleRuntimeHealthIfDatabaseChangedLocked() {
	if channelSmartScheduleRuntimeHealth.database == model.DB {
		return
	}
	channelSmartScheduleRuntimeHealth.database = model.DB
	channelSmartScheduleRuntimeHealth.states = make(map[string]channelSmartScheduleRuntimeHealthState)
}

func channelSmartScheduleRuntimeHealthModelName(modelName string) string {
	return ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
}

func observeChannelSmartScheduleRuntimeRequestSuccess(channelId int, modelName string) {
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return
	}
	observeChannelSmartScheduleRuntimeSuccess(
		channelId,
		modelName,
		common.GetTimestamp(),
		settings.SmartScheduleControlRevision,
		channelSmartScheduleRuntimeRequestSuccess,
	)
}

func observeChannelSmartScheduleRuntimeProbeSuccess(channelId int, modelName string) {
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return
	}
	runtimeRoutes, err := model.GetChannelSmartScheduleRuntimeRoutes(channelId, modelName)
	if err != nil {
		common.SysError("智能调度恢复探针状态读取失败: " + err.Error())
		return
	}
	successMode := channelSmartScheduleRuntimeRegularProbeSuccess
	for _, route := range runtimeRoutes {
		if route.StabilityState == model.ChannelSmartScheduleStabilityProbing {
			successMode = channelSmartScheduleRuntimeRecoveryProbeSuccess
			break
		}
	}
	observeChannelSmartScheduleRuntimeSuccess(
		channelId,
		modelName,
		common.GetTimestamp(),
		settings.SmartScheduleControlRevision,
		successMode,
	)
}

func pruneChannelSmartScheduleRuntimeFailureTimes(times []int64, cutoff int64) []int64 {
	retained := times[:0]
	for _, timestamp := range times {
		if timestamp >= cutoff {
			retained = append(retained, timestamp)
		}
	}
	if len(retained) > 1000 {
		retained = retained[len(retained)-1000:]
	}
	return retained
}

func channelSmartScheduleRuntimeFailureCount(
	snapshot channelSmartScheduleRuntimeHealthSnapshot,
	now int64,
	windowSeconds int,
) int {
	if windowSeconds <= 0 {
		return 0
	}
	cutoff := now - int64(windowSeconds)
	count := 0
	for _, timestamp := range snapshot.FailureTimes {
		if timestamp >= cutoff {
			count++
		}
	}
	return count
}

func observeChannelSmartScheduleRuntimeFailure(
	channelId int,
	modelName string,
	now int64,
	retentionSeconds int,
	revision string,
) channelSmartScheduleRuntimeHealthSnapshot {
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if channelId <= 0 || modelName == "" {
		return channelSmartScheduleRuntimeHealthSnapshot{}
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	if retentionSeconds <= 0 {
		retentionSeconds = maxChannelMonitorSmartScheduleBurstFailureWindowSeconds
	}
	channelSmartScheduleRuntimeHealth.Lock()
	defer channelSmartScheduleRuntimeHealth.Unlock()
	resetChannelSmartScheduleRuntimeHealthIfDatabaseChangedLocked()
	key := channelSmartScheduleRuntimeHealthKey(channelId, modelName)
	state := channelSmartScheduleRuntimeHealth.states[key]
	if state.Revision != revision {
		state = channelSmartScheduleRuntimeHealthState{Revision: revision}
	}
	state.FailureTimes = pruneChannelSmartScheduleRuntimeFailureTimes(
		state.FailureTimes, now-int64(retentionSeconds),
	)
	if len(state.FailureTimes) == 0 {
		state.ConsecutiveFailures = 0
	}
	state.FailureTimes = append(state.FailureTimes, now)
	state.ConsecutiveFailures++
	state.RecoverySuccesses = 0
	channelSmartScheduleRuntimeHealth.states[key] = state
	return channelSmartScheduleRuntimeHealthSnapshot{
		ConsecutiveFailures: state.ConsecutiveFailures,
		FailureTimes:        append([]int64(nil), state.FailureTimes...),
	}
}

func observeChannelSmartScheduleRuntimeSuccess(
	channelId int,
	modelName string,
	now int64,
	revision string,
	successMode channelSmartScheduleRuntimeSuccessMode,
) channelSmartScheduleRuntimeHealthSnapshot {
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if channelId <= 0 || modelName == "" {
		return channelSmartScheduleRuntimeHealthSnapshot{}
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	channelSmartScheduleRuntimeHealth.Lock()
	defer channelSmartScheduleRuntimeHealth.Unlock()
	resetChannelSmartScheduleRuntimeHealthIfDatabaseChangedLocked()
	key := channelSmartScheduleRuntimeHealthKey(channelId, modelName)
	state, exists := channelSmartScheduleRuntimeHealth.states[key]
	if !exists && successMode != channelSmartScheduleRuntimeRecoveryProbeSuccess {
		return channelSmartScheduleRuntimeHealthSnapshot{}
	}
	if state.Revision != revision {
		state = channelSmartScheduleRuntimeHealthState{Revision: revision}
	}
	state.FailureTimes = pruneChannelSmartScheduleRuntimeFailureTimes(
		state.FailureTimes,
		now-int64(maxChannelMonitorSmartScheduleBurstFailureWindowSeconds),
	)
	if successMode == channelSmartScheduleRuntimeRegularProbeSuccess {
		state.RecoverySuccesses = 0
	}
	if successMode == channelSmartScheduleRuntimeRecoveryProbeSuccess {
		state.RecoverySuccesses++
	}
	if successMode != channelSmartScheduleRuntimeRecoveryProbeSuccess && len(state.FailureTimes) == 0 {
		if state.RecoverySuccesses == 0 {
			delete(channelSmartScheduleRuntimeHealth.states, key)
			return channelSmartScheduleRuntimeHealthSnapshot{}
		}
		channelSmartScheduleRuntimeHealth.states[key] = state
		return channelSmartScheduleRuntimeHealthSnapshot{
			ConsecutiveFailures: state.ConsecutiveFailures,
			FailureTimes:        append([]int64(nil), state.FailureTimes...),
			RecoverySuccesses:   state.RecoverySuccesses,
		}
	}
	state.ConsecutiveFailures = 0
	channelSmartScheduleRuntimeHealth.states[key] = state
	return channelSmartScheduleRuntimeHealthSnapshot{
		ConsecutiveFailures: state.ConsecutiveFailures,
		FailureTimes:        append([]int64(nil), state.FailureTimes...),
		RecoverySuccesses:   state.RecoverySuccesses,
	}
}

func getChannelSmartScheduleRuntimeHealth(
	channelId int,
	modelName string,
	now int64,
	_ int,
	revision string,
) channelSmartScheduleRuntimeHealthSnapshot {
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if channelId <= 0 || modelName == "" {
		return channelSmartScheduleRuntimeHealthSnapshot{}
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	channelSmartScheduleRuntimeHealth.Lock()
	defer channelSmartScheduleRuntimeHealth.Unlock()
	resetChannelSmartScheduleRuntimeHealthIfDatabaseChangedLocked()
	key := channelSmartScheduleRuntimeHealthKey(channelId, modelName)
	state := channelSmartScheduleRuntimeHealth.states[key]
	if state.Revision != revision {
		return channelSmartScheduleRuntimeHealthSnapshot{}
	}
	// The state is shared across groups, so retain the largest supported window;
	// each policy filters the returned snapshot with its own configured window.
	state.FailureTimes = pruneChannelSmartScheduleRuntimeFailureTimes(
		state.FailureTimes, now-int64(maxChannelMonitorSmartScheduleBurstFailureWindowSeconds),
	)
	if len(state.FailureTimes) == 0 && state.RecoverySuccesses == 0 {
		delete(channelSmartScheduleRuntimeHealth.states, key)
		return channelSmartScheduleRuntimeHealthSnapshot{}
	}
	channelSmartScheduleRuntimeHealth.states[key] = state
	return channelSmartScheduleRuntimeHealthSnapshot{
		ConsecutiveFailures: state.ConsecutiveFailures,
		FailureTimes:        append([]int64(nil), state.FailureTimes...),
		RecoverySuccesses:   state.RecoverySuccesses,
	}
}

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
	runtimeRoutes, routeErr := model.GetChannelSmartScheduleRuntimeRoutes(channelId, requestModelName)
	if routeErr != nil {
		common.SysError("智能调度运行时路由读取失败: " + routeErr.Error())
		return
	}
	if len(runtimeRoutes) == 0 {
		return
	}
	type matchingPolicyRoute struct {
		configured channelSmartScheduleGroupPolicy
		route      model.ChannelSmartScheduleRuntimeRoute
	}
	matchingPolicies := make([]matchingPolicyRoute, 0, len(settings.SmartScheduleGroupPolicies))
	maxFailureWindowSeconds := 0
	for _, configured := range settings.SmartScheduleGroupPolicies {
		route, participating := runtimeRoutes[configured.Group]
		if !participating {
			continue
		}
		policy := configured.policy()
		if !policy.StabilityEnabled && route.StabilityState != model.ChannelSmartScheduleStabilityProbing &&
			route.TemporaryTrafficKind == "" {
			continue
		}
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
			configured: configured,
			route:      route,
		})
		maxFailureWindowSeconds = max(maxFailureWindowSeconds, policy.BurstFailureWindowSeconds)
	}
	if len(matchingPolicies) == 0 {
		return
	}
	now := common.GetTimestamp()
	health := observeChannelSmartScheduleRuntimeFailure(
		channelId,
		requestModelName,
		now,
		maxFailureWindowSeconds,
		settings.SmartScheduleControlRevision,
	)
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
		failureCount := channelSmartScheduleRuntimeFailureCount(
			health, now, policy.BurstFailureWindowSeconds,
		)
		thresholdReached := health.ConsecutiveFailures >= policy.ConsecutiveFailureThreshold ||
			failureCount >= policy.BurstFailureThreshold
		probing := matched.route.StabilityState == model.ChannelSmartScheduleStabilityProbing
		temporaryTraffic := matched.route.TemporaryTrafficKind != ""
		if temporaryTraffic && !probing {
			sampleStart := max(sampleWindowStart, matched.route.SampleSince)
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
			thresholdReached = sampleCount >= int64(policy.MinSamples)
		}
		if !probing && !thresholdReached {
			continue
		}
		cooldownMinutes := policy.CooldownMinutes
		if cooldownMinutes <= 0 {
			cooldownMinutes = 1
		}
		reason := "渠道短期稳定性保护已触发"
		if probing {
			reason = "稳定性试放请求失败，已重新进入降级保护"
		} else if temporaryTraffic {
			reason = "渠道运行时错误，稳定性样本已达到最少样本数，已停止临时流量并进入稳定性保护"
		} else {
			reason = fmt.Sprintf(
				"渠道短期失败达到保护阈值（连续 %d 次，%d 秒内 %d 次），已进入稳定性保护",
				health.ConsecutiveFailures,
				policy.BurstFailureWindowSeconds,
				failureCount,
			)
		}
		if message := strings.TrimSpace(err.MaskSensitiveErrorWithStatusCode()); message != "" {
			reason += "：" + message
		}
		var result model.ChannelSmartScheduleRuntimeFailureResult
		var protectErr error
		if probing || temporaryTraffic {
			result, protectErr = model.ProtectChannelSmartScheduleRouteOnRuntimeFailure(
				channelId,
				matched.configured.Group,
				matched.route.ModelName,
				now+int64(cooldownMinutes)*60,
				reason,
				settings.SmartScheduleControlRevision,
			)
		} else {
			result, protectErr = model.ProtectChannelSmartScheduleRouteOnShortTermFailure(
				channelId,
				matched.configured.Group,
				matched.route.ModelName,
				now+int64(cooldownMinutes)*60,
				reason,
				settings.SmartScheduleControlRevision,
			)
		}
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
