package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const maxChannelSmartScheduleRuntimeRequestEvents = 1000

const channelSmartScheduleAdaptiveRefreshDelay = time.Second

type channelSmartScheduleRuntimeRequestEvent struct {
	Timestamp int64
	Failure   bool
}

type channelSmartScheduleRuntimeHealthSnapshot struct {
	ConsecutiveFailures int
	FailureTimes        []int64
	RequestEvents       []channelSmartScheduleRuntimeRequestEvent
	WindowComplete      bool
}

type channelSmartScheduleAdaptiveRefreshEvent struct {
	database  any
	channelId int
	group     string
	modelName string
}

var channelSmartScheduleAdaptiveRefreshQueue = struct {
	sync.Mutex
	pending map[channelSmartScheduleAdaptiveRefreshEvent]struct{}
	running bool
}{
	pending: make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{}),
}

func init() {
	service.RegisterChannelRateLimitCooldownExpiredHandler(
		func(channelId int, modelName string) {
			enqueueChannelSmartScheduleAdaptiveRefresh(channelId, modelName)
		},
	)
}

func channelSmartScheduleRuntimeHealthModelName(modelName string) string {
	return ratio_setting.FormatMatchingModelName(strings.TrimSpace(modelName))
}

func channelSmartScheduleProbeRecoveryRequest(
	channelId int,
	modelName string,
	resultTime int64,
	scheduled bool,
	failureReason string,
) (*model.ChannelSmartScheduleProbeRecoveryRequest, error) {
	settings := getChannelMonitorRuntimeSettings()
	if !settings.SmartScheduleEnabled || channelId <= 0 || strings.TrimSpace(modelName) == "" {
		return nil, nil
	}
	runtimeRoutes, err := model.GetChannelSmartScheduleRuntimeRoutes(channelId, modelName)
	if err != nil {
		return nil, err
	}
	request := &model.ChannelSmartScheduleProbeRecoveryRequest{
		ExpectedControlRevision: settings.SmartScheduleControlRevision,
		FailureReason:           failureReason,
	}
	for _, configured := range settings.SmartScheduleGroupPolicies {
		route, exists := runtimeRoutes[configured.Group]
		if !exists || (route.StabilityState != model.ChannelSmartScheduleStabilityDegraded &&
			route.StabilityState != model.ChannelSmartScheduleStabilityProbing) {
			continue
		}
		policy := configured.policy()
		if !policy.StabilityEnabled ||
			(len(policy.Models) > 0 && !slices.Contains(policy.Models, route.ModelName)) ||
			(scheduled && route.StabilityState == model.ChannelSmartScheduleStabilityDegraded &&
				!policy.DegradedProbeEnabled) {
			continue
		}
		cooldownMinutes := max(policy.CooldownMinutes, 1)
		request.Routes = append(request.Routes, model.ChannelSmartScheduleProbeRecoveryRoute{
			Group:                    configured.Group,
			Model:                    route.ModelName,
			RecoverySuccessThreshold: max(policy.RecoverySuccessThreshold, 1),
			CooldownUntil:            resultTime + int64(cooldownMinutes)*60,
		})
	}
	if len(request.Routes) == 0 {
		return nil, nil
	}
	failureReason = strings.TrimSpace(failureReason)
	if failureReason != "" {
		prefix := "手动测试失败，已清零恢复成功次数并续满稳定性保护"
		if scheduled {
			prefix = "降级期间定时探测失败，已延长稳定性保护"
		}
		request.FailureReason = prefix + "：" + failureReason
	}
	return request, nil
}

func applyChannelSmartScheduleProbeRecoveryResult(
	channelId int,
	modelName string,
	request *model.ChannelSmartScheduleProbeRecoveryRequest,
) {
	if request == nil {
		enqueueChannelSmartScheduleAdaptiveRefresh(channelId, modelName)
		return
	}
	result := request.Result
	if result.RoutingChanged {
		refreshed := true
		seen := make(map[channelSmartScheduleRoutePoolKey]struct{}, len(result.Recovered)+len(result.Renewed))
		keys := append(append([]model.ChannelSmartScheduleRouteKey(nil), result.Recovered...), result.Renewed...)
		for _, key := range keys {
			pool := channelSmartScheduleRoutePoolKey{group: key.Group, model: key.Model}
			if _, exists := seen[pool]; exists {
				continue
			}
			seen[pool] = struct{}{}
			if err := model.RefreshChannelSmartScheduleRoutePoolCache(key.Group, key.Model); err != nil {
				common.SysError("刷新探测恢复路由缓存失败: " + err.Error())
				refreshed = false
			}
		}
		if !refreshed {
			model.InitChannelCache()
		}
	}
	enqueueChannelSmartScheduleAdaptiveRefresh(channelId, modelName)
}

func channelSmartScheduleRuntimeConsecutiveFailures(
	events []channelSmartScheduleRuntimeRequestEvent,
) int {
	count := 0
	for index := len(events) - 1; index >= 0; index-- {
		if !events[index].Failure {
			break
		}
		count++
	}
	return count
}

func channelSmartScheduleRuntimeFailureCount(
	snapshot channelSmartScheduleRuntimeHealthSnapshot,
	now int64,
	windowSeconds int,
) int {
	count, _, _ := channelSmartScheduleRuntimeWindowFailureRate(
		snapshot, now, windowSeconds, maxChannelSmartScheduleRuntimeRequestEvents,
	)
	return count
}

func channelSmartScheduleRuntimeWindowFailureRate(
	snapshot channelSmartScheduleRuntimeHealthSnapshot,
	now int64,
	windowSeconds int,
	maxRequests int,
) (failureCount, requestCount int, events []channelSmartScheduleRuntimeRequestEvent) {
	return channelSmartScheduleRuntimeWindowFailureRateSince(
		snapshot, now, windowSeconds, maxRequests, 0,
	)
}

func channelSmartScheduleRuntimeWindowFailureRateSince(
	snapshot channelSmartScheduleRuntimeHealthSnapshot,
	now int64,
	windowSeconds int,
	maxRequests int,
	observationSince int64,
) (failureCount, requestCount int, events []channelSmartScheduleRuntimeRequestEvent) {
	if windowSeconds <= 0 || maxRequests <= 0 {
		return 0, 0, nil
	}
	allEvents := snapshot.RequestEvents
	if len(allEvents) == 0 && len(snapshot.FailureTimes) > 0 {
		allEvents = make([]channelSmartScheduleRuntimeRequestEvent, 0, len(snapshot.FailureTimes))
		for _, timestamp := range snapshot.FailureTimes {
			allEvents = append(allEvents, channelSmartScheduleRuntimeRequestEvent{
				Timestamp: timestamp,
				Failure:   true,
			})
		}
	}
	cutoff := max(now-int64(windowSeconds), observationSince)
	filtered := make([]channelSmartScheduleRuntimeRequestEvent, 0, len(allEvents))
	for _, event := range allEvents {
		if event.Timestamp >= cutoff && event.Timestamp <= now {
			filtered = append(filtered, event)
		}
	}
	if len(filtered) > maxRequests {
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Timestamp < filtered[j].Timestamp
		})
		filtered = filtered[len(filtered)-maxRequests:]
	}
	for _, event := range filtered {
		if event.Failure {
			failureCount++
		}
	}
	return failureCount, len(filtered), filtered
}

func channelSmartScheduleRuntimeWindowFailureThresholdReached(
	snapshot channelSmartScheduleRuntimeHealthSnapshot,
	now int64,
	windowSeconds int,
	maxRequests int,
	thresholdPercent float64,
) (bool, int, int) {
	failureCount, requestCount, _ := channelSmartScheduleRuntimeWindowFailureRate(
		snapshot, now, windowSeconds, maxRequests,
	)
	if requestCount == 0 || thresholdPercent <= 0 || math.IsNaN(thresholdPercent) || math.IsInf(thresholdPercent, 0) {
		return false, failureCount, requestCount
	}
	return float64(failureCount)*100 >= float64(requestCount)*thresholdPercent,
		failureCount, requestCount
}

func maxChannelSmartScheduleRuntimeRetentionSeconds() int {
	return maxChannelMonitorSmartScheduleBurstFailureWindowMinutes * 60
}

func getChannelSmartScheduleRuntimeHealth(
	channelId int,
	modelName string,
	now int64,
	_ int,
	_ string,
) channelSmartScheduleRuntimeHealthSnapshot {
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if channelId <= 0 || modelName == "" {
		return channelSmartScheduleRuntimeHealthSnapshot{}
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}
	windowStart := now - int64(maxChannelSmartScheduleRuntimeRetentionSeconds())
	events, snapshot := channelSmartScheduleRealtimeEvents(
		channelId,
		modelName,
		windowStart,
		0,
		maxChannelSmartScheduleRuntimeRequestEvents,
	)
	runtimeEvents := make([]channelSmartScheduleRuntimeRequestEvent, 0, len(events))
	failureTimes := make([]int64, 0, len(events))
	for _, event := range events {
		if event.Source != model.ChannelMonitorEventSourceBusiness {
			continue
		}
		failure := event.Outcome == model.ChannelMonitorEventOutcomeFailure
		if failure && !event.RuntimeProtectionEligible {
			continue
		}
		runtimeEvents = append(runtimeEvents, channelSmartScheduleRuntimeRequestEvent{
			Timestamp: event.OccurredAt,
			Failure:   failure,
		})
		if failure {
			failureTimes = append(failureTimes, event.OccurredAt)
		}
	}
	return channelSmartScheduleRuntimeHealthSnapshot{
		ConsecutiveFailures: channelSmartScheduleRuntimeConsecutiveFailures(runtimeEvents),
		FailureTimes:        failureTimes,
		RequestEvents:       runtimeEvents,
		WindowComplete:      snapshot.CoverageStart <= windowStart,
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

func protectChannelSmartScheduleRuntimeFailure(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, false, false, true)
}

func protectChannelSmartScheduleProjectedRateLimit(channelId int, modelName string) {
	err := types.NewErrorWithStatusCode(
		errors.New("上游触发请求频率限制"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusTooManyRequests,
	)
	protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, false, false, false)
}

func protectChannelSmartScheduleProjectedFailure(channelId int, modelName string) {
	window, available := service.GetChannelMonitorRealtimeWindow(channelId, modelName)
	if !available {
		return
	}
	for index := len(window.Events) - 1; index >= 0; index-- {
		event := window.Events[index]
		if event.Source != model.ChannelMonitorEventSourceBusiness ||
			!event.RuntimeProtectionEligible || event.FinalRetrySummary ||
			event.Outcome != model.ChannelMonitorEventOutcomeFailure {
			continue
		}
		statusCode := http.StatusInternalServerError
		if event.StatusCode != nil {
			statusCode = *event.StatusCode
		}
		if statusCode == http.StatusTooManyRequests {
			continue
		}
		message := strings.TrimSpace(event.ErrorMessage)
		if message == "" {
			message = "渠道请求失败"
		}
		errorCode := types.ErrorCodeBadResponseStatusCode
		if strings.TrimSpace(event.ErrorCode) != "" {
			errorCode = types.ErrorCode(event.ErrorCode)
		}
		err := types.NewErrorWithStatusCode(errors.New(message), errorCode, statusCode)
		protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, false, false, false)
		return
	}
}

func protectChannelSmartScheduleScheduledProbeFailure(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, true, false, true)
}

func protectChannelSmartScheduleScheduledProbeFailureAfterRecoveryResult(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, true, true, true)
}

func protectChannelSmartScheduleManualProbeFailureAfterRecoveryResult(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, false, true, true)
}

// protectChannelSmartScheduleRuntimeFailureWithSource protects every
// configured group that exposes the same channel/model route because the
// upstream failure is shared across groups. A scheduled probe failure renews
// an active degraded route immediately instead of waiting for traffic failure
// thresholds that may be shorter than the configured probe interval.
func protectChannelSmartScheduleRuntimeFailureWithSource(
	channelId int,
	modelName string,
	err *types.NewAPIError,
	scheduledProbe bool,
	probeRecoveryHandled bool,
	includeCurrentFailure bool,
) {
	if err == nil || types.IsSkipRetryError(err) {
		return
	}
	requestModelName := strings.TrimSpace(modelName)
	if err.StatusCode == http.StatusTooManyRequests {
		settings := getChannelMonitorRuntimeSettings()
		cooldownStarted := false
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
				cooldownStarted = service.StartChannelRateLimitCooldownIfControlRevision(
					channelId,
					requestModelName,
					settings.SmartScheduleRateLimitCooldownSeconds,
					settings.SmartScheduleControlRevision,
				)
				break
			}
		}
		if cooldownStarted {
			enqueueChannelSmartScheduleAdaptiveRefresh(channelId, requestModelName)
		}
		return
	}
	if channelId <= 0 || !isChannelSmartScheduleRuntimeFailure(err) {
		return
	}
	settings := getChannelMonitorRuntimeSettings()
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
	defer enqueueChannelSmartScheduleAdaptiveRefresh(channelId, requestModelName)
	type matchingPolicyRoute struct {
		configured channelSmartScheduleGroupPolicy
		route      model.ChannelSmartScheduleRuntimeRoute
	}
	matchingPolicies := make([]matchingPolicyRoute, 0, len(settings.SmartScheduleGroupPolicies))
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
	}
	if len(matchingPolicies) == 0 {
		return
	}
	now := common.GetTimestamp()
	health := getChannelSmartScheduleRuntimeHealth(
		channelId,
		requestModelName,
		now,
		maxChannelSmartScheduleRuntimeRetentionSeconds(),
		settings.SmartScheduleControlRevision,
	)
	if includeCurrentFailure {
		health.RequestEvents = append(health.RequestEvents, channelSmartScheduleRuntimeRequestEvent{
			Timestamp: now, Failure: true,
		})
		health.ConsecutiveFailures = channelSmartScheduleRuntimeConsecutiveFailures(health.RequestEvents)
	}
	routingChanged := false
	protectionApplied := false
	changedPools := make(map[channelSmartScheduleRoutePoolKey]struct{})
	for _, matched := range matchingPolicies {
		policy := matched.configured.policy()
		windowRequestLimit := policy.BurstFailureWindowRequests
		if policy.BurstFailureThresholdLegacy > 0 {
			windowRequestLimit = maxChannelSmartScheduleRuntimeRequestEvents
		}
		windowReached := false
		failureCount, requestCount, requestEvents := channelSmartScheduleRuntimeWindowFailureRateSince(
			health,
			now,
			policy.BurstFailureWindowSeconds,
			windowRequestLimit,
			matched.route.SampleSince,
		)
		consecutiveFailures := channelSmartScheduleRuntimeConsecutiveFailures(requestEvents)
		windowStart := max(
			now-int64(policy.BurstFailureWindowSeconds),
			matched.route.SampleSince,
		)
		windowComplete := service.GetChannelMonitorRealtimeProjectionStartedAt() <= windowStart ||
			requestCount >= windowRequestLimit
		if policy.BurstFailureThresholdLegacy > 0 {
			windowReached = failureCount >= policy.BurstFailureThresholdLegacy
		} else if windowComplete && requestCount > 0 && policy.BurstFailureThresholdPercent > 0 {
			windowReached = float64(failureCount)*100 >=
				float64(requestCount)*policy.BurstFailureThresholdPercent
		}
		thresholdReached := consecutiveFailures >= policy.ConsecutiveFailureThreshold ||
			windowReached
		probing := matched.route.StabilityState == model.ChannelSmartScheduleStabilityProbing
		temporaryTraffic := matched.route.TemporaryTrafficKind != ""
		degradedProbe := scheduledProbe &&
			matched.route.StabilityState == model.ChannelSmartScheduleStabilityDegraded &&
			policy.StabilityEnabled && policy.DegradedProbeEnabled
		if probeRecoveryHandled &&
			(probing || matched.route.StabilityState == model.ChannelSmartScheduleStabilityDegraded) {
			continue
		}
		if !probing && !degradedProbe && !thresholdReached {
			continue
		}
		failureRatePercent := 0.0
		if requestCount > 0 {
			failureRatePercent = float64(failureCount) / float64(requestCount) * 100
		}
		cooldownMinutes := policy.CooldownMinutes
		if cooldownMinutes <= 0 {
			cooldownMinutes = 1
		}
		reason := "渠道短期稳定性保护已触发"
		if probing {
			reason = "稳定性试放请求失败，已重新进入降级保护"
		} else if degradedProbe {
			reason = "降级期间定时探测失败，已延长稳定性保护"
		} else if temporaryTraffic {
			if policy.BurstFailureThresholdLegacy > 0 {
				reason = fmt.Sprintf(
					"临时流量渠道突发失败达到保护阈值（连续 %d 次，%d 秒内 %d 次），已停止临时流量并进入稳定性保护",
					consecutiveFailures, policy.BurstFailureWindowSeconds, failureCount,
				)
			} else {
				reason = fmt.Sprintf(
					"临时流量渠道窗口失败率达到保护阈值（连续 %d 次，最近 %d 分钟内最多 %d 次请求，实际 %d 次中失败 %d 次，失败率 %.1f%%，阈值 %.1f%%），已停止临时流量并进入稳定性保护",
					consecutiveFailures, policy.BurstFailureWindowMinutes,
					windowRequestLimit, requestCount, failureCount, failureRatePercent,
					policy.BurstFailureThresholdPercent,
				)
			}
		} else {
			if policy.BurstFailureThresholdLegacy > 0 {
				reason = fmt.Sprintf(
					"渠道短期失败达到保护阈值（连续 %d 次，%d 秒内 %d 次），已进入稳定性保护",
					consecutiveFailures, policy.BurstFailureWindowSeconds, failureCount,
				)
			} else {
				reason = fmt.Sprintf(
					"渠道窗口失败率达到保护阈值（连续 %d 次，最近 %d 分钟内最多 %d 次请求，实际 %d 次中失败 %d 次，失败率 %.1f%%，阈值 %.1f%%），已进入稳定性保护",
					consecutiveFailures, policy.BurstFailureWindowMinutes,
					windowRequestLimit, requestCount, failureCount, failureRatePercent,
					policy.BurstFailureThresholdPercent,
				)
			}
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
		} else if degradedProbe {
			result, protectErr = model.ProtectChannelSmartScheduleRouteOnRecoveryProbeFailure(
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
		if result.RoutingChanged {
			changedPools[channelSmartScheduleRoutePoolKey{
				group: matched.configured.Group, model: matched.route.ModelName,
			}] = struct{}{}
		}
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
		refreshed := true
		for pool := range changedPools {
			if err := model.RefreshChannelSmartScheduleRoutePoolCache(pool.group, pool.model); err != nil {
				common.SysError("刷新运行时保护路由池缓存失败: " + err.Error())
				refreshed = false
			}
		}
		if !refreshed {
			model.InitChannelCache()
		}
	}
}

func enqueueChannelSmartScheduleAdaptiveRefresh(channelId int, modelName string) {
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if channelId <= 0 || modelName == "" {
		return
	}
	enqueueChannelSmartScheduleAdaptiveRefreshEvent(channelSmartScheduleAdaptiveRefreshEvent{
		database: model.DB, channelId: channelId, modelName: modelName,
	})
}

func enqueueChannelSmartScheduleAdaptivePoolRefresh(group string, modelName string) {
	group = strings.TrimSpace(group)
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if group == "" || modelName == "" {
		return
	}
	enqueueChannelSmartScheduleAdaptiveRefreshEvent(channelSmartScheduleAdaptiveRefreshEvent{
		database: model.DB, group: group, modelName: modelName,
	})
}

func enqueueChannelSmartScheduleAdaptiveRefreshEvent(event channelSmartScheduleAdaptiveRefreshEvent) {
	channelSmartScheduleAdaptiveRefreshQueue.Lock()
	channelSmartScheduleAdaptiveRefreshQueue.pending[event] = struct{}{}
	if channelSmartScheduleAdaptiveRefreshQueue.running {
		channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		return
	}
	channelSmartScheduleAdaptiveRefreshQueue.running = true
	channelSmartScheduleAdaptiveRefreshQueue.Unlock()
	go runChannelSmartScheduleAdaptiveRefreshWorker()
}

func runChannelSmartScheduleAdaptiveRefreshWorker() {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		common.SysError(fmt.Sprintf("自适应备援刷新异常: %v", recovered))
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		channelSmartScheduleAdaptiveRefreshQueue.running = false
		restart := len(channelSmartScheduleAdaptiveRefreshQueue.pending) > 0
		if restart {
			channelSmartScheduleAdaptiveRefreshQueue.running = true
		}
		channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		if restart {
			go runChannelSmartScheduleAdaptiveRefreshWorker()
		}
	}()
	for {
		timer := time.NewTimer(channelSmartScheduleAdaptiveRefreshDelay)
		<-timer.C
		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		events := make([]channelSmartScheduleAdaptiveRefreshEvent, 0, len(channelSmartScheduleAdaptiveRefreshQueue.pending))
		for event := range channelSmartScheduleAdaptiveRefreshQueue.pending {
			events = append(events, event)
		}
		channelSmartScheduleAdaptiveRefreshQueue.pending = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
		channelSmartScheduleAdaptiveRefreshQueue.Unlock()
		processChannelSmartScheduleAdaptiveRefreshEventsWithThrottle(events, true)

		channelSmartScheduleAdaptiveRefreshQueue.Lock()
		pending := len(channelSmartScheduleAdaptiveRefreshQueue.pending) > 0
		if !pending {
			channelSmartScheduleAdaptiveRefreshQueue.running = false
			channelSmartScheduleAdaptiveRefreshQueue.Unlock()
			return
		}
		channelSmartScheduleAdaptiveRefreshQueue.Unlock()
	}
}

func processChannelSmartScheduleAdaptiveRefreshEvents(events []channelSmartScheduleAdaptiveRefreshEvent) {
	processChannelSmartScheduleAdaptiveRefreshEventsWithThrottle(events, false)
}

func processChannelSmartScheduleAdaptiveRefreshEventsWithThrottle(
	events []channelSmartScheduleAdaptiveRefreshEvent,
	throttled bool,
) {
	settings := getChannelMonitorRuntimeSettings()
	if !settings.SmartScheduleEnabled || len(events) == 0 {
		return
	}
	fullScheduleRunning, err := model.IsSystemTaskRunning(channelMonitorSmartScheduleTaskType)
	if err != nil {
		common.SysError("自适应备援检查完整调度任务失败: " + err.Error())
	} else if fullScheduleRunning {
		for _, event := range events {
			if event.database == model.DB {
				enqueueChannelSmartScheduleAdaptiveRefreshEvent(event)
			}
		}
		return
	}
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policyByGroup[configured.Group] = configured.policy()
	}
	sort.Slice(events, func(i int, j int) bool {
		if events[i].group != events[j].group {
			return events[i].group < events[j].group
		}
		if events[i].modelName != events[j].modelName {
			return events[i].modelName < events[j].modelName
		}
		return events[i].channelId < events[j].channelId
	})
	eventsByPool := make(map[channelSmartScheduleRoutePoolKey]map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
	for _, event := range events {
		if event.database != model.DB {
			continue
		}
		if event.group != "" {
			policy, configured := policyByGroup[event.group]
			softRoutingEnabled := policy.ApplyMode == channelMonitorSmartScheduleApplyPriorityWeight &&
				(policy.AdaptiveSamplingEnabled || policy.SampleMode == channelMonitorSmartScheduleSampleTraffic)
			if !configured || (!softRoutingEnabled && !policy.StabilityEnabled) ||
				(len(policy.Models) > 0 && !slices.Contains(policy.Models, event.modelName)) {
				continue
			}
			poolKey := channelSmartScheduleRoutePoolKey{group: event.group, model: event.modelName}
			if eventsByPool[poolKey] == nil {
				eventsByPool[poolKey] = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
			}
			eventsByPool[poolKey][event] = struct{}{}
			continue
		}
		runtimeRoutes, err := model.GetChannelSmartScheduleRuntimeParticipatingRoutes(
			event.channelId, event.modelName,
		)
		if err != nil {
			common.SysError("自适应备援参与路由读取失败: " + err.Error())
			continue
		}
		for group, routeModelName := range runtimeRoutes {
			policy, configured := policyByGroup[group]
			softRoutingEnabled := policy.ApplyMode == channelMonitorSmartScheduleApplyPriorityWeight &&
				(policy.AdaptiveSamplingEnabled || policy.SampleMode == channelMonitorSmartScheduleSampleTraffic)
			if !configured || (!softRoutingEnabled && !policy.StabilityEnabled) ||
				(len(policy.Models) > 0 && !slices.Contains(policy.Models, routeModelName)) {
				continue
			}
			poolKey := channelSmartScheduleRoutePoolKey{group: group, model: routeModelName}
			if eventsByPool[poolKey] == nil {
				eventsByPool[poolKey] = make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
			}
			eventsByPool[poolKey][event] = struct{}{}
		}
	}
	poolOrder := make([]channelSmartScheduleRoutePoolKey, 0, len(eventsByPool))
	for poolKey := range eventsByPool {
		poolOrder = append(poolOrder, poolKey)
	}
	sort.Slice(poolOrder, func(i int, j int) bool {
		if poolOrder[i].group != poolOrder[j].group {
			return poolOrder[i].group < poolOrder[j].group
		}
		return poolOrder[i].model < poolOrder[j].model
	})
	for _, poolKey := range poolOrder {
		if throttled && !reserveChannelSmartScheduleAdaptiveRefresh(model.DB, poolKey) {
			continue
		}
		policy := policyByGroup[poolKey.group]
		conflict, err := refreshChannelSmartScheduleAdaptivePool(
			context.Background(), poolKey, policy, settings.SmartScheduleControlRevision, model.DB,
		)
		if err != nil {
			common.SysError("智能调度池级软刷新失败: " + err.Error())
			continue
		}
		if !conflict {
			continue
		}
		for event := range eventsByPool[poolKey] {
			enqueueChannelSmartScheduleAdaptiveRefreshEvent(event)
		}
	}
}

func channelSmartScheduleAdaptiveRouteAvailable(
	route model.ChannelSmartScheduleRoute,
	now int64,
) bool {
	return route.State.Participates() && route.ChannelStatus == common.ChannelStatusEnabled &&
		route.Enabled && !route.TrafficPaused(now) && route.State.StabilityState == "" &&
		route.State.RuntimeProtectionUntil <= now &&
		service.ChannelRateLimitCooldownUntilMatching(route.ChannelId, route.Model) <= 0
}

func refreshChannelSmartScheduleAdaptivePool(
	ctx context.Context,
	poolKey channelSmartScheduleRoutePoolKey,
	policy channelSmartSchedulePolicy,
	expectedControlRevision string,
	expectedDatabase any,
) (bool, error) {
	softRoutingEnabled := policy.ApplyMode == channelMonitorSmartScheduleApplyPriorityWeight &&
		(policy.AdaptiveSamplingEnabled || policy.SampleMode == channelMonitorSmartScheduleSampleTraffic)
	if expectedDatabase != model.DB || (!softRoutingEnabled && !policy.StabilityEnabled) {
		return false, nil
	}
	routes, err := model.GetChannelSmartScheduleRoutePool(poolKey.group, poolKey.model)
	if err != nil || len(routes) == 0 {
		return false, err
	}
	for _, route := range routes {
		if route.State.Id == 0 {
			return false, nil
		}
	}
	economicSnapshot, err := model.GetChannelSmartScheduleEconomicSnapshot()
	if err != nil {
		return false, err
	}
	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(economicSnapshot.Monitors))
	for _, monitor := range economicSnapshot.Monitors {
		monitorByChannel[monitor.ChannelId] = monitor
	}
	groupRatio, groupRatioAvailable := economicSnapshot.GroupRatios[poolKey.group]
	for index := range routes {
		monitor, monitorAvailable := monitorByChannel[routes[index].ChannelId]
		economics := channelSmartScheduleClassifyEconomics(
			monitor,
			monitorAvailable,
			groupRatio,
			groupRatioAvailable,
		)
		routes[index].CostRatio = economics.CostRatio
		routes[index].GroupRatio = economics.GroupRatio
		routes[index].GrossMargin = economics.GrossMargin
		routes[index].EconomicRole = economics.EconomicRole
	}
	now := common.GetTimestamp()
	settings := getChannelMonitorRuntimeSettings()
	adaptiveWindowStart := now - int64(policy.AdaptiveSamplingWindowSeconds)
	performanceWindowStart := now - int64(settings.SmartSchedulePerformanceWindowMinutes*60)
	stabilityWindowStart := now - int64(settings.SmartScheduleStabilityWindowMinutes*60)
	_ = ctx
	healthByChannel := make(map[int]channelSmartScheduleHealthUpdate, len(routes))
	metricByChannel := make(map[int]model.ChannelSmartScheduleAdaptiveHealthMetric, len(routes))
	rollingStabilityByChannel := make(map[int]*channelSmartSchedulePerformance, len(routes))
	stabilityAvailableByChannel := make(map[int]bool, len(routes))
	stateByChannel := make(map[int]model.ChannelSmartScheduleRouteState, len(routes))
	for _, route := range routes {
		stateByChannel[route.ChannelId] = route.State
		if !route.State.Participates() {
			continue
		}
		metric, _ := channelSmartScheduleRealtimeAdaptiveMetric(
			route.ChannelId,
			route.Model,
			adaptiveWindowStart,
			route.SharedSamples.ObservationSince,
			policy.AdaptiveSamplingWindowRequests,
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		performanceMetric, _ := channelSmartScheduleRealtimeAdaptiveMetric(
			route.ChannelId,
			route.Model,
			performanceWindowStart,
			route.SharedSamples.ObservationSince,
			0,
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		routeStabilityWindowStart := stabilityWindowStart
		if route.State.StabilityState == model.ChannelSmartScheduleStabilityProbing &&
			route.State.StabilitySince > routeStabilityWindowStart {
			routeStabilityWindowStart = route.State.StabilitySince
		}
		stabilityMetric, _ := channelSmartScheduleRealtimeAdaptiveMetric(
			route.ChannelId,
			route.Model,
			routeStabilityWindowStart,
			route.SharedSamples.ObservationSince,
			0,
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		)
		stabilityAvailableByChannel[route.ChannelId] = stabilityMetric.RequestCount > 0
		performanceMetric.RequestCount = stabilityMetric.RequestCount
		performanceMetric.LastUsedTime = max(performanceMetric.LastUsedTime, stabilityMetric.LastUsedTime)
		metricByChannel[route.ChannelId] = performanceMetric
		healthByChannel[route.ChannelId] = channelSmartScheduleEvaluateHealth(route.State, metric, policy)
		rolling := &channelSmartSchedulePerformance{
			StabilitySuccessCount:                stabilityMetric.StabilitySuccessCount,
			StabilityFailureCount:                stabilityMetric.StabilityFailureCount,
			StabilityFinalFailureCount:           stabilityMetric.StabilityFinalFailureCount,
			StabilityRetryFailureCount:           stabilityMetric.StabilityRetryFailureCount,
			StabilityRetryFailureDurationTotalMs: stabilityMetric.RetryFailureDurationTotalMs,
			StabilityFailureDurationBuckets: append(
				[]model.ChannelMonitorFailureDurationBucket(nil),
				stabilityMetric.RetryFailureDurationBuckets...,
			),
			FirstTokenDurationBuckets: append(
				[]model.ChannelMonitorDurationBucket(nil),
				stabilityMetric.FirstTokenDurationBuckets...,
			),
		}
		for _, bucket := range rolling.FirstTokenDurationBuckets {
			rolling.FirstTokenDurationSampleCount += bucket.Count
		}
		rolling.Stability, rolling.StabilitySampleCount = channelSmartScheduleStabilityScore(
			rolling.StabilitySuccessCount,
			rolling.StabilityFailureCount,
			rolling.StabilityFinalFailureCount,
			rolling.StabilityFailureDurationBuckets,
			policy,
		)
		channelSmartScheduleApplyJitterMeasurement(rolling, policy)
		rollingStabilityByChannel[route.ChannelId] = rolling
	}

	primaryIndex := -1
	manualPrimaryActive := false
	for index, route := range routes {
		if route.State.ManualPrimaryUntil > now && channelSmartScheduleAdaptiveRouteAvailable(route, now) {
			primaryIndex = index
			manualPrimaryActive = true
			break
		}
	}
	if primaryIndex < 0 {
		for index, route := range routes {
			if !channelSmartScheduleAdaptiveRouteAvailable(route, now) || route.State.BaseRank <= 0 {
				continue
			}
			if primaryIndex < 0 || route.Priority > routes[primaryIndex].Priority ||
				(route.Priority == routes[primaryIndex].Priority && route.Weight > routes[primaryIndex].Weight) ||
				(route.Priority == routes[primaryIndex].Priority && route.Weight == routes[primaryIndex].Weight &&
					(route.State.BaseRank < routes[primaryIndex].State.BaseRank ||
						(route.State.BaseRank == routes[primaryIndex].State.BaseRank &&
							route.ChannelId < routes[primaryIndex].ChannelId))) {
				primaryIndex = index
			}
		}
	}
	selectedChannelId := 0
	selectedKind := ""
	selectedPercent := 0.0
	debtByChannel := make(map[int]int, len(routes))
	backupCandidates := make([]channelSmartScheduleCandidate, 0, len(routes)-1)
	scoringCandidates := make([]channelSmartScheduleCandidate, 0, len(routes))
	candidateByChannel := make(map[int]channelSmartScheduleCandidate, len(routes))
	if primaryIndex >= 0 {
		primaryRoute := routes[primaryIndex]
		primaryHealth := healthByChannel[primaryRoute.ChannelId]
		for _, route := range routes {
			if !channelSmartScheduleAdaptiveRouteAvailable(route, now) ||
				route.State.BaseRank <= 0 {
				continue
			}
			if route.EconomicRole == channelSmartScheduleEconomicRoleBreakEvenFallback {
				continue
			}
			metric := metricByChannel[route.ChannelId]
			health := healthByChannel[route.ChannelId]
			rolling := rollingStabilityByChannel[route.ChannelId]
			candidate := channelSmartScheduleCandidate{
				ChannelId: route.ChannelId, PreviousBaseRank: route.State.BaseRank,
				Ratio: route.CostRatio, CostRatio: route.CostRatio,
				GroupRatio: route.GroupRatio, GrossMargin: route.GrossMargin,
				EconomicRole:    route.EconomicRole,
				CurrentPriority: route.State.BasePriority, CurrentWeight: route.State.BaseWeight,
				FirstTokenSampleCount: int(metric.FirstTokenCount),
				TPSSampleCount:        int(metric.TPSSampleCount),
				StabilityAvailable:    stabilityAvailableByChannel[route.ChannelId],
				HealthState:           health.State, HealthPressure: health.Pressure,
				HealthErrorPressure:   health.ErrorPressure,
				HealthLatencyPressure: health.LatencyPressure,
				HealthEvidence:        health.Evidence, HealthSampleCount: health.SampleCount,
				HealthLastSampleAt:                    metric.LastUsedTime,
				MinComparableChannels:                 policy.AdaptiveSamplingMinComparableChannels,
				HealthErrorRequestPercent:             health.ErrorRequestPercent,
				HealthRiskRequestPercent:              health.RiskRequestPercent,
				HealthFirstTokenWarningRequestPercent: health.FirstTokenWarningRequestPercent,
				HealthHealthyRequestPercent:           health.HealthyRequestPercent,
				HealthWindowMinutes:                   policy.AdaptiveSamplingWindowMinutes,
				HealthWindowRequests:                  policy.AdaptiveSamplingWindowRequests,
			}
			if metric.FirstTokenCount > 0 {
				value := metric.FirstTokenTotalMs / float64(metric.FirstTokenCount)
				candidate.FirstTokenMs = &value
				if policy.JitterEnabled {
					_, _, _, winsorized := model.SummarizeChannelMonitorDurationBuckets(
						metric.FirstTokenDurationBuckets,
					)
					if winsorized != nil {
						candidate.FirstTokenMs = winsorized
					}
				}
			}
			if metric.TPSSampleCount > 0 {
				value := metric.TPSTotal / float64(metric.TPSSampleCount)
				candidate.TPS = &value
			}
			if rolling != nil {
				candidate.Stability = rolling.Stability
				candidate.StabilitySampleCount = rolling.StabilitySampleCount
			}
			candidate.SampleDebt = channelSmartScheduleCandidateSampleDebt(
				candidate, policy.Strategy, policy.StabilityEnabled, policy.Scoring, policy.MinSamples,
			)
			debtByChannel[route.ChannelId] = candidate.SampleDebt
			candidateByChannel[route.ChannelId] = candidate
			scoringCandidates = append(scoringCandidates, candidate)
			if route.ChannelId != primaryRoute.ChannelId && candidate.SampleDebt > 0 {
				backupCandidates = append(backupCandidates, candidate)
			}
		}
		if len(backupCandidates) > 0 {
			sort.SliceStable(backupCandidates, func(i int, j int) bool {
				left := backupCandidates[i]
				right := backupCandidates[j]
				if policy.SamplingOrder == channelMonitorSmartScheduleSamplingOrderRatio {
					if (left.CostRatio == nil) != (right.CostRatio == nil) {
						return left.CostRatio != nil
					}
					if left.CostRatio != nil && right.CostRatio != nil &&
						math.Abs(*left.CostRatio-*right.CostRatio) > channelMonitorRatioEpsilon {
						return *left.CostRatio < *right.CostRatio
					}
				} else {
					if left.CurrentPriority != right.CurrentPriority {
						return left.CurrentPriority > right.CurrentPriority
					}
					if left.CurrentWeight != right.CurrentWeight {
						return left.CurrentWeight > right.CurrentWeight
					}
				}
				leftState := stateByChannel[left.ChannelId]
				rightState := stateByChannel[right.ChannelId]
				leftActive := leftState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficExploration ||
					leftState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive
				rightActive := rightState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficExploration ||
					rightState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive
				if leftActive != rightActive {
					return leftActive
				}
				if left.HealthLastSampleAt != right.HealthLastSampleAt {
					return left.HealthLastSampleAt < right.HealthLastSampleAt
				}
				if leftState.LastSamplingAt != rightState.LastSamplingAt {
					return leftState.LastSamplingAt < rightState.LastSamplingAt
				}
				return left.ChannelId < right.ChannelId
			})
			selected := backupCandidates[0]
			primaryHasPressure := primaryHealth.State != "" &&
				primaryHealth.State != channelSmartScheduleHealthUnknown &&
				primaryHealth.State != channelSmartScheduleHealthHealthy
			if primaryHasPressure && policy.AdaptiveSamplingEnabled {
				primaryCandidate := channelSmartScheduleCandidate{
					ChannelId:   primaryRoute.ChannelId,
					HealthState: primaryHealth.State, HealthPressure: primaryHealth.Pressure,
				}
				budgetCandidates := append([]channelSmartScheduleCandidate{primaryCandidate}, backupCandidates...)
				selectedPercent = channelSmartScheduleAdaptiveSamplingBudget(
					primaryCandidate, budgetCandidates, policy,
				)
				if selected.HealthEvidence && selected.HealthState != channelSmartScheduleHealthHealthy {
					basePercent := max(policy.AdaptiveSamplingBasePercent, 0)
					if policy.SampleMode == channelMonitorSmartScheduleSampleTraffic {
						basePercent = min(basePercent, max(policy.ExplorationTrafficPercent, 0))
					}
					selectedPercent = min(selectedPercent, basePercent)
				}
				if selectedPercent > channelMonitorRatioEpsilon {
					selectedChannelId = selected.ChannelId
					selectedKind = model.ChannelSmartScheduleTemporaryTrafficAdaptive
				}
			}
			if selectedChannelId == 0 && policy.SampleMode == channelMonitorSmartScheduleSampleTraffic {
				selectedChannelId = selected.ChannelId
				selectedKind = model.ChannelSmartScheduleTemporaryTrafficExploration
				selectedPercent = policy.ExplorationTrafficPercent
			}
		}

		normalPrimaryChannelId := primaryRoute.ChannelId
		if !manualPrimaryActive {
			normalization := channelSmartScheduleBuildNormalization(scoringCandidates, policy.MinSamples)
			scoredItems := make([]channelSmartSchedulePlanItem, 0, len(scoringCandidates))
			currentPrimaryScored := false
			for _, candidate := range scoringCandidates {
				if candidate.SampleDebt > 0 || channelSmartScheduleCandidateSkipReasonWithScoring(
					candidate,
					policy.Strategy,
					policy.StabilityEnabled,
					policy.MinSamples,
					policy.Scoring,
				) != "" {
					continue
				}
				score, _, valid := channelSmartScheduleScoreCandidate(
					candidate,
					policy.Strategy,
					policy.StabilityEnabled,
					policy.ApplyMode,
					policy.MinSamples,
					false,
					policy.Scoring,
					normalization,
				)
				if !valid {
					continue
				}
				scoredItems = append(scoredItems, channelSmartSchedulePlanItem{
					ChannelId: candidate.ChannelId, Score: score,
					PreviousBaseRank: candidate.PreviousBaseRank,
				})
				currentPrimaryScored = currentPrimaryScored || candidate.ChannelId == primaryRoute.ChannelId
			}
			minimumComparable := max(policy.AdaptiveSamplingMinComparableChannels, 2)
			if currentPrimaryScored && len(scoredItems) >= minimumComparable {
				ranked := channelSmartScheduleRankedItemIndexes(scoredItems, primaryRoute.ChannelId)
				if len(ranked) > 0 {
					rawWinnerId := scoredItems[ranked[0]].ChannelId
					effectivePrimaryId := channelSmartScheduleEffectivePrimaryId(
						scoredItems,
						primaryRoute.ChannelId,
						policy.Scoring.PrimarySwitchThresholdPercent/channelMonitorScorePercentageTotal,
						false,
					)
					winner := candidateByChannel[rawWinnerId]
					if rawWinnerId != primaryRoute.ChannelId && effectivePrimaryId == rawWinnerId &&
						winner.SampleDebt == 0 && winner.HealthEvidence &&
						winner.HealthState == channelSmartScheduleHealthHealthy &&
						winner.HealthHealthyRequestPercent+channelMonitorRatioEpsilon >=
							policy.AdaptiveSamplingSwitchConfirmRequestPercent {
						normalPrimaryChannelId = rawWinnerId
					}
				}
			}
		}
		if normalPrimaryChannelId != primaryRoute.ChannelId &&
			selectedKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive {
			if policy.SampleMode == channelMonitorSmartScheduleSampleTraffic && selectedChannelId > 0 {
				selectedKind = model.ChannelSmartScheduleTemporaryTrafficExploration
				selectedPercent = policy.ExplorationTrafficPercent
			} else {
				selectedChannelId = 0
				selectedKind = ""
				selectedPercent = 0
			}
		}
		primaryIndex = slices.IndexFunc(routes, func(route model.ChannelSmartScheduleRoute) bool {
			return route.ChannelId == normalPrimaryChannelId
		})
	}
	desiredPriority := make(map[int]int64, len(routes))
	desiredWeight := make(map[int]uint, len(routes))
	snapshots := make(map[int]*model.ChannelSmartScheduleRoutingSnapshotUpdate)
	runtimeRecoveryByChannel := make(map[int]bool, len(routes))
	if policy.StabilityEnabled {
		recoveryThreshold := policy.RecoveryStabilityScore / channelMonitorScorePercentageTotal
		for _, route := range routes {
			rolling := rollingStabilityByChannel[route.ChannelId]
			if route.State.StabilityState != model.ChannelSmartScheduleStabilityProbing ||
				!route.State.Participates() || route.ChannelStatus != common.ChannelStatusEnabled ||
				!route.Enabled || rolling == nil || rolling.Stability == nil ||
				rolling.StabilitySampleCount < int64(policy.MinSamples) ||
				*rolling.Stability+channelMonitorRatioEpsilon < recoveryThreshold {
				continue
			}
			runtimeRecoveryByChannel[route.ChannelId] = true
		}
	}
	poolHasTemporaryTraffic := false
	for _, route := range routes {
		poolHasTemporaryTraffic = poolHasTemporaryTraffic ||
			route.State.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficExploration ||
			route.State.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive
	}
	for _, route := range routes {
		desiredPriority[route.ChannelId] = route.Priority
		desiredWeight[route.ChannelId] = route.Weight
		if runtimeRecoveryByChannel[route.ChannelId] {
			restorePriority := route.State.StabilitySavedPriority
			if restorePriority <= channelMonitorSmartScheduleDegradedPriority {
				restorePriority = route.State.BasePriority
			}
			restoreWeight := route.State.StabilitySavedWeight
			if restoreWeight == 0 {
				restoreWeight = route.State.BaseWeight
			}
			restorePriority, restoreWeight = channelSmartScheduleSavedTarget(restorePriority, restoreWeight)
			desiredPriority[route.ChannelId] = restorePriority
			desiredWeight[route.ChannelId] = restoreWeight
			snapshots[route.ChannelId] = &model.ChannelSmartScheduleRoutingSnapshotUpdate{}
			continue
		}
		if !route.State.Participates() || route.State.BaseRank <= 0 ||
			route.State.StabilityState != "" || route.State.RuntimeProtectionUntil > now {
			continue
		}
		if poolHasTemporaryTraffic {
			desiredPriority[route.ChannelId] = route.State.BasePriority
			desiredWeight[route.ChannelId] = route.State.BaseWeight
		}
		if route.State.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficExploration ||
			route.State.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive {
			snapshots[route.ChannelId] = &model.ChannelSmartScheduleRoutingSnapshotUpdate{}
		}
		if route.State.ManualPrimaryUntil > now {
			desiredPriority[route.ChannelId] = route.Priority
			desiredWeight[route.ChannelId] = 1000
		}
	}
	if primaryIndex >= 0 && !manualPrimaryActive {
		basePrimaryIndex := -1
		for index, route := range routes {
			if !channelSmartScheduleAdaptiveRouteAvailable(route, now) || route.State.BaseRank <= 0 ||
				route.EconomicRole == channelSmartScheduleEconomicRoleBreakEvenFallback {
				continue
			}
			if basePrimaryIndex < 0 || route.State.BaseRank < routes[basePrimaryIndex].State.BaseRank ||
				(route.State.BaseRank == routes[basePrimaryIndex].State.BaseRank &&
					route.ChannelId < routes[basePrimaryIndex].ChannelId) {
				basePrimaryIndex = index
			}
		}
		if basePrimaryIndex >= 0 && basePrimaryIndex != primaryIndex {
			basePrimary := routes[basePrimaryIndex]
			runtimePrimary := routes[primaryIndex]
			desiredPriority[runtimePrimary.ChannelId] = basePrimary.State.BasePriority
			desiredWeight[runtimePrimary.ChannelId] = basePrimary.State.BaseWeight
			desiredPriority[basePrimary.ChannelId] = runtimePrimary.State.BasePriority
			desiredWeight[basePrimary.ChannelId] = runtimePrimary.State.BaseWeight
		}
	}
	if selectedChannelId > 0 && primaryIndex >= 0 {
		primaryRoute := routes[primaryIndex]
		highestPriority := int64(math.MinInt64)
		topLayerChannelIds := make([]int, 0, len(routes))
		topLayerWeight := uint64(0)
		for _, route := range routes {
			if route.ChannelId == selectedChannelId || route.ChannelStatus != common.ChannelStatusEnabled ||
				!route.Enabled || route.TrafficPaused(now) ||
				service.ChannelRateLimitCooldownUntilMatching(route.ChannelId, route.Model) > 0 {
				continue
			}
			priority := desiredPriority[route.ChannelId]
			weight := desiredWeight[route.ChannelId]
			if priority > highestPriority {
				highestPriority = priority
				topLayerChannelIds = topLayerChannelIds[:0]
				topLayerWeight = 0
			}
			if priority == highestPriority {
				topLayerChannelIds = append(topLayerChannelIds, route.ChannelId)
				if math.MaxUint64-topLayerWeight >= uint64(weight) {
					topLayerWeight += uint64(weight)
				} else {
					topLayerWeight = math.MaxUint64
				}
			}
		}
		if highestPriority == int64(math.MinInt64) || topLayerWeight == 0 {
			selectedChannelId = 0
			selectedKind = ""
			selectedPercent = 0
		} else {
			temporaryWeight := uint(0)
			uniquePrimary := len(topLayerChannelIds) == 1 &&
				topLayerChannelIds[0] == primaryRoute.ChannelId && primaryRoute.State.Participates()
			if uniquePrimary {
				temporaryWeight = uint(math.Round(
					float64(channelMonitorSmartScheduleTemporaryWeightTotal) * selectedPercent /
						channelMonitorScorePercentageTotal,
				))
				temporaryWeight = min(
					max(temporaryWeight, 1), uint(channelMonitorSmartScheduleTemporaryWeightTotal-1),
				)
			} else {
				exactWeight := float64(topLayerWeight) * selectedPercent /
					(channelMonitorScorePercentageTotal - selectedPercent)
				if !math.IsNaN(exactWeight) && !math.IsInf(exactWeight, 0) && exactWeight >= 1 &&
					exactWeight <= float64(^uint(0)) {
					temporaryWeight = uint(math.Round(exactWeight))
				}
			}
			if temporaryWeight == 0 {
				selectedChannelId = 0
				selectedKind = ""
				selectedPercent = 0
			} else {
				if uniquePrimary {
					desiredWeight[primaryRoute.ChannelId] =
						uint(channelMonitorSmartScheduleTemporaryWeightTotal) - temporaryWeight
				}
				desiredPriority[selectedChannelId] = highestPriority
				desiredWeight[selectedChannelId] = temporaryWeight
				for _, route := range routes {
					if route.ChannelId != selectedChannelId {
						continue
					}
					temporarySince := route.State.TemporaryTrafficSince
					if route.State.TemporaryTrafficKind != selectedKind ||
						temporarySince <= 0 {
						temporarySince = now
					}
					snapshots[selectedChannelId] = &model.ChannelSmartScheduleRoutingSnapshotUpdate{
						TemporaryTrafficKind:          selectedKind,
						TemporaryTrafficSince:         temporarySince,
						TemporaryTrafficTargetPercent: selectedPercent,
						ExplorationMaxPromptTokens:    policy.ExplorationMaxPromptTokens,
					}
					break
				}
			}
		}
	}

	updates := make([]model.ChannelSmartScheduleRouteResultUpdate, 0, len(routes))
	hasChanges := false
	trafficStateChanged := false
	for _, route := range routes {
		health, healthSet := healthByChannel[route.ChannelId]
		healthChanged := healthSet &&
			(health.State != route.State.AdaptiveHealthState ||
				math.Abs(health.Pressure-route.State.AdaptiveHealthPressure) > channelMonitorRatioEpsilon ||
				math.Abs(health.FirstTokenWarningRequestPercent-route.State.AdaptiveHealthFirstTokenWarningRequestPercent) > channelMonitorRatioEpsilon ||
				health.SampleCount != route.State.AdaptiveHealthSampleCount ||
				health.LastSampleAt != route.State.AdaptiveHealthLastSampleAt)
		rolling := rollingStabilityByChannel[route.ChannelId]
		var rollingScore *float64
		rollingSampleCount := int64(0)
		rollingSlowCount := int64(0)
		rollingAllowedSlowCount := int64(0)
		if rolling != nil {
			rollingScore = rolling.Stability
			rollingSampleCount = rolling.StabilitySampleCount
			rollingSlowCount = rolling.JitterSlowCount
			rollingAllowedSlowCount = rolling.JitterAllowedCount
		}
		rollingScoreChanged := (rollingScore == nil) != (route.State.RollingStabilityScore == nil)
		if rollingScore != nil && route.State.RollingStabilityScore != nil {
			rollingScoreChanged = math.Abs(*rollingScore-*route.State.RollingStabilityScore) > channelMonitorRatioEpsilon
		}
		rollingChanged := rollingScoreChanged ||
			rollingSampleCount != route.State.RollingStabilitySampleCount ||
			rollingSlowCount != route.State.RollingStabilitySlowCount ||
			rollingAllowedSlowCount != route.State.RollingStabilityAllowedSlowCount ||
			route.State.RollingStabilityUpdatedAt != now
		lastSamplingAt := route.State.LastSamplingAt
		isSamplingCandidate := route.ChannelId == selectedChannelId
		if isSamplingCandidate && !route.State.SamplingCandidate {
			lastSamplingAt = now
		}
		samplingChanged := route.State.SamplingDebt != debtByChannel[route.ChannelId] ||
			route.State.SamplingCandidate != isSamplingCandidate ||
			route.State.SamplingOrder != policy.SamplingOrder ||
			route.State.LastSamplingAt != lastSamplingAt
		snapshot := snapshots[route.ChannelId]
		snapshotChanged := snapshot != nil &&
			(snapshot.TemporaryTrafficKind != route.State.TemporaryTrafficKind ||
				snapshot.TemporaryTrafficSince != route.State.TemporaryTrafficSince ||
				math.Abs(snapshot.TemporaryTrafficTargetPercent-route.State.TemporaryTrafficTargetPercent) > channelMonitorRatioEpsilon ||
				snapshot.ExplorationMaxPromptTokens != route.State.ExplorationMaxPromptTokens)
		runtimeRecovery := runtimeRecoveryByChannel[route.ChannelId]
		applyPriorityWeight := route.State.Participates() && route.Enabled &&
			route.ChannelStatus == common.ChannelStatusEnabled &&
			(route.State.StabilityState == "" || runtimeRecovery) &&
			route.State.RuntimeProtectionUntil <= now &&
			(desiredPriority[route.ChannelId] != route.Priority || desiredWeight[route.ChannelId] != route.Weight)
		changed := healthChanged || rollingChanged || samplingChanged || snapshotChanged ||
			applyPriorityWeight || runtimeRecovery
		hasChanges = hasChanges || changed
		trafficStateChanged = trafficStateChanged || snapshotChanged || runtimeRecovery
		update := model.ChannelSmartScheduleRouteResultUpdate{
			ChannelId: route.ChannelId, Group: route.Group, Model: route.Model,
			Priority: desiredPriority[route.ChannelId], Weight: desiredWeight[route.ChannelId],
			PoolGuard: true, ObservationOnly: !changed, AdaptiveOverlayOnly: true,
			ExpectedRevision:         route.State.Revision,
			ExpectedControlRevision:  expectedControlRevision,
			ExpectedEconomicRevision: economicSnapshot.Revision,
			ExpectedParticipationSet: route.State.ParticipationSet,
			ExpectedExcluded:         route.State.Excluded,
			ExpectedAbilityEnabled:   route.Enabled,
			ExpectedChannelStatus:    route.ChannelStatus,
			ExpectedPriority:         route.Priority, ExpectedWeight: route.Weight,
			ApplyPriorityWeight:              applyPriorityWeight,
			RoutingSnapshot:                  snapshot,
			RollingStabilitySet:              true,
			RollingStabilityScore:            rollingScore,
			RollingStabilitySampleCount:      rollingSampleCount,
			RollingStabilitySlowCount:        rollingSlowCount,
			RollingStabilityAllowedSlowCount: rollingAllowedSlowCount,
			RollingStabilityUpdatedAt:        now,
			SamplingStateSet:                 true,
			SamplingDebt:                     debtByChannel[route.ChannelId],
			SamplingCandidate:                isSamplingCandidate,
			SamplingOrder:                    policy.SamplingOrder,
			LastSamplingAt:                   lastSamplingAt,
		}
		if healthSet {
			channelSmartScheduleAttachHealthUpdate(&update, health)
		}
		if runtimeRecovery {
			protectionUntil := int64(0)
			update.RuntimeStabilityRecovery = true
			update.Status = model.ChannelSmartScheduleStatusSucceeded
			update.Error = "滚动稳定性得分达到恢复阈值，已立即解除稳定性试放"
			update.Time = now
			update.Stability = &model.ChannelSmartScheduleStabilityUpdate{}
			update.RuntimeProtectionUntil = &protectionUntil
		}
		updates = append(updates, update)
	}
	if !hasChanges || expectedDatabase != model.DB {
		return false, nil
	}
	sort.Slice(updates, func(i int, j int) bool { return updates[i].ChannelId < updates[j].ChannelId })
	outcomes, err := model.ApplyChannelSmartScheduleRouteResults(updates)
	if err != nil {
		return false, err
	}
	conflict := len(outcomes) != len(updates)
	routingChanged := false
	runtimeRecovered := false
	for _, outcome := range outcomes {
		conflict = conflict || !outcome.Applied
		routingChanged = routingChanged || outcome.RoutingChanged
		if outcome.Applied && outcome.ObservationSince > 0 {
			runtimeRecovered = true
		}
	}
	if !conflict && (routingChanged || trafficStateChanged) {
		if cacheErr := model.RefreshChannelSmartScheduleRoutePoolCache(poolKey.group, poolKey.model); cacheErr != nil {
			common.SysError("刷新自适应备援路由缓存失败: " + cacheErr.Error())
			model.InitChannelCache()
		}
	}
	if !conflict && runtimeRecovered {
		enqueueChannelSmartScheduleAdaptivePoolRefresh(poolKey.group, poolKey.model)
	}
	return conflict, nil
}
