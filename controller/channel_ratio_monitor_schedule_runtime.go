package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const channelSmartScheduleRuntimeFailureRedisKeyPrefix = "channelSmartScheduleRuntimeFailures:v1:"

const channelSmartScheduleAdaptiveRefreshDelay = time.Second

const channelSmartScheduleRuntimeFailureRedisScript = `
local now = tonumber(ARGV[1]) or 0
local retention = tonumber(ARGV[2]) or 0
local cutoff = now - retention
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', cutoff - 1)
redis.call('ZADD', KEYS[1], now, ARGV[3])
local size = tonumber(redis.call('ZCARD', KEYS[1]) or '0')
if size > 1000 then
  redis.call('ZREMRANGEBYRANK', KEYS[1], 0, size - 1001)
end
redis.call('EXPIRE', KEYS[1], retention + 60)
local entries = redis.call('ZRANGE', KEYS[1], 0, -1, 'WITHSCORES')
local scores = {}
for index = 2, #entries, 2 do
  scores[#scores + 1] = entries[index]
end
return scores
`

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

type channelSmartScheduleAdaptiveRefreshEvent struct {
	database  any
	channelId int
	modelName string
}

var channelSmartScheduleAdaptiveRefreshQueue = struct {
	sync.Mutex
	pending map[channelSmartScheduleAdaptiveRefreshEvent]struct{}
	running bool
}{
	pending: make(map[channelSmartScheduleAdaptiveRefreshEvent]struct{}),
}

func channelSmartScheduleRuntimeHealthKey(channelId int, modelName string) string {
	return strings.TrimSpace(modelName) + "#" + strings.TrimSpace(strconv.Itoa(channelId))
}

func channelSmartScheduleRuntimeFailureRedisKey(channelId int, modelName string, revision string) string {
	key := fmt.Sprintf("%s\x00%d\x00%s", strings.TrimSpace(revision), channelId, modelName)
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s%x", channelSmartScheduleRuntimeFailureRedisKeyPrefix, digest)
}

func observeChannelSmartScheduleRuntimeFailureRedis(
	channelId int,
	modelName string,
	now int64,
	retentionSeconds int,
	revision string,
) ([]int64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return nil, nil
	}
	key := channelSmartScheduleRuntimeFailureRedisKey(channelId, modelName, revision)
	result, err := common.RDB.Eval(
		context.Background(),
		channelSmartScheduleRuntimeFailureRedisScript,
		[]string{key},
		now,
		retentionSeconds,
		fmt.Sprintf("%d-%s", now, common.GetRandomString(12)),
	).StringSlice()
	if err != nil {
		return nil, err
	}
	times := make([]int64, 0, len(result))
	for _, timestamp := range result {
		value, parseErr := strconv.ParseInt(timestamp, 10, 64)
		if parseErr != nil {
			return nil, parseErr
		}
		times = append(times, value)
	}
	return times, nil
}

func clearChannelSmartScheduleRuntimeFailureRedis(
	channelId int,
	modelName string,
	recoveryAt int64,
	revision string,
) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	key := channelSmartScheduleRuntimeFailureRedisKey(channelId, modelName, revision)
	var err error
	if recoveryAt <= 0 {
		err = common.RDB.Del(context.Background(), key).Err()
	} else {
		err = common.RDB.ZRemRangeByScore(
			context.Background(), key, "-inf", strconv.FormatInt(recoveryAt, 10),
		).Err()
	}
	if err != nil {
		common.SysError("清理 Redis 智能调度保护失败窗口失败: " + err.Error())
	}
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
	enqueueChannelSmartScheduleAdaptiveRefresh(channelId, modelName)
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
	for _, configured := range settings.SmartScheduleGroupPolicies {
		route, exists := runtimeRoutes[configured.Group]
		if !exists {
			continue
		}
		policy := configured.policy()
		if len(policy.Models) > 0 && !slices.Contains(policy.Models, route.ModelName) {
			continue
		}
		if route.StabilityState == model.ChannelSmartScheduleStabilityProbing ||
			(route.StabilityState == model.ChannelSmartScheduleStabilityDegraded &&
				policy.StabilityEnabled && policy.DegradedProbeEnabled) {
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
	enqueueChannelSmartScheduleAdaptiveRefresh(channelId, modelName)
}

// clearChannelSmartScheduleRuntimeHealth removes failure samples collected
// before a route recovered. Failures recorded after recoveryAt are retained so
// a concurrent request cannot be hidden by the reset.
func clearChannelSmartScheduleRuntimeHealth(channelId int, modelName string, recoveryAt int64) {
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if channelId <= 0 || modelName == "" {
		return
	}
	settings := getChannelMonitorSettings()
	clearChannelSmartScheduleRuntimeFailureRedis(
		channelId, modelName, recoveryAt, settings.SmartScheduleControlRevision,
	)
	channelSmartScheduleRuntimeHealth.Lock()
	defer channelSmartScheduleRuntimeHealth.Unlock()
	resetChannelSmartScheduleRuntimeHealthIfDatabaseChangedLocked()
	key := channelSmartScheduleRuntimeHealthKey(channelId, modelName)
	state, exists := channelSmartScheduleRuntimeHealth.states[key]
	if !exists {
		return
	}
	if recoveryAt <= 0 {
		delete(channelSmartScheduleRuntimeHealth.states, key)
		return
	}
	retained := state.FailureTimes[:0]
	for _, timestamp := range state.FailureTimes {
		if timestamp > recoveryAt {
			retained = append(retained, timestamp)
		}
	}
	state.FailureTimes = retained
	state.RecoverySuccesses = 0
	if len(state.FailureTimes) == 0 {
		delete(channelSmartScheduleRuntimeHealth.states, key)
		return
	}
	state.ConsecutiveFailures = min(state.ConsecutiveFailures, len(state.FailureTimes))
	channelSmartScheduleRuntimeHealth.states[key] = state
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
	snapshot := channelSmartScheduleRuntimeHealthSnapshot{
		ConsecutiveFailures: state.ConsecutiveFailures,
		FailureTimes:        append([]int64(nil), state.FailureTimes...),
	}
	channelSmartScheduleRuntimeHealth.Unlock()

	sharedFailureTimes, err := observeChannelSmartScheduleRuntimeFailureRedis(
		channelId, modelName, now, retentionSeconds, revision,
	)
	if err != nil {
		common.SysError("同步 Redis 智能调度保护失败窗口失败: " + err.Error())
		return snapshot
	}
	if sharedFailureTimes != nil {
		snapshot.FailureTimes = sharedFailureTimes
	}
	return snapshot
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

func protectChannelSmartScheduleRuntimeFailure(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, false)
}

func protectChannelSmartScheduleScheduledProbeFailure(
	channelId int,
	modelName string,
	err *types.NewAPIError,
) {
	protectChannelSmartScheduleRuntimeFailureWithSource(channelId, modelName, err, true)
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
) {
	if err == nil || types.IsSkipRetryError(err) {
		return
	}
	requestModelName := strings.TrimSpace(modelName)
	if err.StatusCode == http.StatusTooManyRequests {
		settings := getChannelMonitorSettings()
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
	// The shared key can serve groups with different windows. Retain the
	// widest supported window here; each policy applies its own narrower count.
	health := observeChannelSmartScheduleRuntimeFailure(
		channelId,
		requestModelName,
		now,
		maxChannelMonitorSmartScheduleBurstFailureWindowSeconds,
		settings.SmartScheduleControlRevision,
	)
	routingChanged := false
	protectionApplied := false
	for _, matched := range matchingPolicies {
		policy := matched.configured.policy()
		failureCount := channelSmartScheduleRuntimeFailureCount(
			health, now, policy.BurstFailureWindowSeconds,
		)
		thresholdReached := health.ConsecutiveFailures >= policy.ConsecutiveFailureThreshold ||
			failureCount >= policy.BurstFailureThreshold
		probing := matched.route.StabilityState == model.ChannelSmartScheduleStabilityProbing
		temporaryTraffic := matched.route.TemporaryTrafficKind != ""
		degradedProbe := scheduledProbe &&
			matched.route.StabilityState == model.ChannelSmartScheduleStabilityDegraded &&
			policy.StabilityEnabled && policy.DegradedProbeEnabled
		if !probing && !degradedProbe && !thresholdReached {
			continue
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
			reason = fmt.Sprintf(
				"临时流量渠道突发失败达到保护阈值（连续 %d 次，%d 秒内 %d 次），已停止临时流量并进入稳定性保护",
				health.ConsecutiveFailures,
				policy.BurstFailureWindowSeconds,
				failureCount,
			)
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

func enqueueChannelSmartScheduleAdaptiveRefresh(channelId int, modelName string) {
	modelName = channelSmartScheduleRuntimeHealthModelName(modelName)
	if channelId <= 0 || modelName == "" {
		return
	}
	event := channelSmartScheduleAdaptiveRefreshEvent{
		database: model.DB, channelId: channelId, modelName: modelName,
	}
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
		processChannelSmartScheduleAdaptiveRefreshEvents(events)

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
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled || len(events) == 0 {
		return
	}
	fullScheduleRunning, err := model.IsSystemTaskRunning(channelMonitorSmartScheduleTaskType)
	if err != nil {
		common.SysError("自适应备援检查完整调度任务失败: " + err.Error())
	} else if fullScheduleRunning {
		for _, event := range events {
			if event.database == model.DB {
				enqueueChannelSmartScheduleAdaptiveRefresh(event.channelId, event.modelName)
			}
		}
		return
	}
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policyByGroup[configured.Group] = configured.policy()
	}
	sort.Slice(events, func(i int, j int) bool {
		if events[i].channelId != events[j].channelId {
			return events[i].channelId < events[j].channelId
		}
		return events[i].modelName < events[j].modelName
	})
	eventsByPool := make(map[channelSmartScheduleRoutePoolKey]map[channelSmartScheduleAdaptiveRefreshEvent]struct{})
	for _, event := range events {
		if event.database != model.DB {
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
			if !configured || !policy.AdaptiveSamplingEnabled ||
				policy.ApplyMode != channelMonitorSmartScheduleApplyPriorityWeight ||
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
		policy := policyByGroup[poolKey.group]
		conflict, err := refreshChannelSmartScheduleAdaptivePool(
			context.Background(), poolKey, policy, settings.SmartScheduleControlRevision, model.DB,
		)
		if err != nil {
			common.SysError("自适应备援刷新失败: " + err.Error())
			continue
		}
		if !conflict {
			continue
		}
		for event := range eventsByPool[poolKey] {
			enqueueChannelSmartScheduleAdaptiveRefresh(event.channelId, event.modelName)
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
	if expectedDatabase != model.DB || !policy.AdaptiveSamplingEnabled ||
		policy.ApplyMode != channelMonitorSmartScheduleApplyPriorityWeight {
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
	now := common.GetTimestamp()
	windowStart := now - int64(policy.AdaptiveSamplingWindowSeconds)
	windows := make([]model.ChannelSmartScheduleAdaptiveHealthMetricWindow, 0, len(routes))
	windowChannelIds := make([]int, 0, len(routes))
	for _, route := range routes {
		if !route.State.Participates() {
			continue
		}
		windows = append(windows, model.ChannelSmartScheduleAdaptiveHealthMetricWindow{
			ChannelId: route.ChannelId, ModelName: route.Model,
			StartTimestamp: windowStart, ObservationSince: route.SharedSamples.ObservationSince,
			WarningSeconds:  policy.AdaptiveSamplingFirstTokenWarningSeconds,
			CriticalSeconds: policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		})
		windowChannelIds = append(windowChannelIds, route.ChannelId)
	}
	productionMetrics := make(map[int]model.ChannelSmartScheduleAdaptiveHealthMetric, len(windows))
	if common.LogConsumeEnabled && constant.ErrorLogEnabled {
		results, metricErr := model.GetChannelSmartScheduleAdaptiveHealthMetrics(ctx, windows, now+1)
		if metricErr != nil {
			return false, metricErr
		}
		for index, result := range results {
			if index >= len(windowChannelIds) {
				break
			}
			productionMetrics[windowChannelIds[index]] = result.Metric
		}
	}
	healthByChannel := make(map[int]channelSmartScheduleHealthUpdate, len(routes))
	metricByChannel := make(map[int]model.ChannelSmartScheduleAdaptiveHealthMetric, len(routes))
	stateByChannel := make(map[int]model.ChannelSmartScheduleRouteState, len(routes))
	for _, route := range routes {
		stateByChannel[route.ChannelId] = route.State
		if !route.State.Participates() {
			continue
		}
		series, seriesErr := route.SharedSamples.SampleSeries()
		if seriesErr != nil {
			return false, seriesErr
		}
		metric := channelSmartScheduleMergeAdaptiveHealthMetric(
			productionMetrics[route.ChannelId],
			series.AdaptiveHealthMetricsSince(
				windowStart,
				policy.AdaptiveSamplingFirstTokenWarningSeconds,
				policy.AdaptiveSamplingFirstTokenCriticalSeconds,
			),
		)
		metricByChannel[route.ChannelId] = metric
		healthByChannel[route.ChannelId] = channelSmartScheduleEvaluateHealth(route.State, metric, policy)
	}

	primaryIndex := -1
	for index, route := range routes {
		if route.State.ManualPrimaryUntil > now && channelSmartScheduleAdaptiveRouteAvailable(route, now) {
			primaryIndex = index
			break
		}
	}
	if primaryIndex < 0 {
		for index, route := range routes {
			if route.State.BaseRank == 1 && channelSmartScheduleAdaptiveRouteAvailable(route, now) {
				primaryIndex = index
				break
			}
		}
	}
	existingAdaptive := false
	for _, route := range routes {
		existingAdaptive = existingAdaptive ||
			route.State.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive
	}
	selectedChannelId := 0
	adaptivePercent := 0.0
	backupCandidates := make([]channelSmartScheduleCandidate, 0, len(routes)-1)
	if primaryIndex >= 0 {
		primaryRoute := routes[primaryIndex]
		primaryHealth := healthByChannel[primaryRoute.ChannelId]
		primaryHasPressure := primaryHealth.State != "" &&
			primaryHealth.State != channelSmartScheduleHealthUnknown &&
			primaryHealth.State != channelSmartScheduleHealthHealthy
		if primaryHasPressure {
			for index, route := range routes {
				if index == primaryIndex ||
					!channelSmartScheduleAdaptiveRouteAvailable(route, now) || route.State.BaseRank <= 0 {
					continue
				}
				details, decodeErr := route.State.LastScheduleScoreDetails.Decode()
				if decodeErr != nil || (details != nil && details.Economics != nil &&
					details.Economics.EconomicRole == channelSmartScheduleEconomicRoleBreakEvenFallback) {
					continue
				}
				metric := metricByChannel[route.ChannelId]
				health := healthByChannel[route.ChannelId]
				candidate := channelSmartScheduleCandidate{
					ChannelId: route.ChannelId, PreviousBaseRank: route.State.BaseRank,
					FirstTokenSampleCount: int(metric.FirstTokenCount),
					TPSSampleCount:        int(metric.TPSSampleCount),
					StabilitySampleCount:  metric.RequestCount,
					HealthState:           health.State, HealthPressure: health.Pressure,
					HealthErrorPressure:   health.ErrorPressure,
					HealthLatencyPressure: health.LatencyPressure,
					HealthEvidence:        health.Evidence, HealthSampleCount: health.SampleCount,
					HealthLastSampleAt:          health.LastSampleAt,
					HealthRiskRequestPercent:    health.RiskRequestPercent,
					HealthHealthyRequestPercent: health.HealthyRequestPercent,
					HealthWindowSeconds:         health.WindowSeconds,
				}
				candidate.SampleDebt = channelSmartScheduleCandidateSampleDebt(
					candidate, policy.Strategy, policy.StabilityEnabled, policy.Scoring, policy.MinSamples,
				)
				if candidate.SampleDebt > 0 {
					backupCandidates = append(backupCandidates, candidate)
				}
			}
			primaryCandidate := channelSmartScheduleCandidate{
				ChannelId: primaryRoute.ChannelId, HealthState: primaryHealth.State,
				HealthPressure: primaryHealth.Pressure,
			}
			budgetCandidates := append([]channelSmartScheduleCandidate{primaryCandidate}, backupCandidates...)
			adaptivePercent = channelSmartScheduleAdaptiveSamplingBudget(
				primaryCandidate, budgetCandidates, policy,
			)
			if adaptivePercent > channelMonitorRatioEpsilon && len(backupCandidates) > 0 {
				sort.SliceStable(backupCandidates, func(i int, j int) bool {
					left := backupCandidates[i]
					right := backupCandidates[j]
					if leftRank, rightRank := channelSmartScheduleAdaptiveCandidateRank(left), channelSmartScheduleAdaptiveCandidateRank(right); leftRank != rightRank {
						return leftRank < rightRank
					}
					if left.SampleDebt != right.SampleDebt {
						return left.SampleDebt > right.SampleDebt
					}
					leftState := stateByChannel[left.ChannelId]
					rightState := stateByChannel[right.ChannelId]
					leftActive := leftState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive
					rightActive := rightState.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive
					if leftActive != rightActive {
						return leftActive
					}
					if left.HealthLastSampleAt != right.HealthLastSampleAt {
						return left.HealthLastSampleAt < right.HealthLastSampleAt
					}
					if left.PreviousBaseRank != right.PreviousBaseRank {
						return left.PreviousBaseRank < right.PreviousBaseRank
					}
					return left.ChannelId < right.ChannelId
				})
				selected := backupCandidates[0]
				selectedChannelId = selected.ChannelId
				if selected.HealthEvidence && selected.HealthState != channelSmartScheduleHealthHealthy {
					basePercent := max(policy.AdaptiveSamplingBasePercent, 0)
					if policy.SampleMode == channelMonitorSmartScheduleSampleTraffic {
						basePercent = min(basePercent, max(policy.ExplorationTrafficPercent, 0))
					}
					adaptivePercent = min(adaptivePercent, basePercent)
				}
				if adaptivePercent <= channelMonitorRatioEpsilon {
					selectedChannelId = 0
				}
			}
		}
	}

	desiredPriority := make(map[int]int64, len(routes))
	desiredWeight := make(map[int]uint, len(routes))
	snapshots := make(map[int]*model.ChannelSmartScheduleRoutingSnapshotUpdate)
	for _, route := range routes {
		desiredPriority[route.ChannelId] = route.Priority
		desiredWeight[route.ChannelId] = route.Weight
	}
	if selectedChannelId > 0 && primaryIndex >= 0 {
		primaryRoute := routes[primaryIndex]
		highestPriority := int64(math.MinInt64)
		topLayerChannelIds := make([]int, 0, len(routes))
		topLayerWeight := uint64(0)
		for _, route := range routes {
			if route.ChannelId == selectedChannelId ||
				!channelSmartScheduleAdaptiveRouteAvailable(route, now) || route.State.BaseRank <= 0 {
				continue
			}
			priority := route.State.BasePriority
			weight := route.State.BaseWeight
			if route.ChannelId == primaryRoute.ChannelId && route.State.ManualPrimaryUntil > now {
				priority = route.Priority
				weight = 1000
			}
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
			adaptivePercent = 0
		} else {
			temporaryWeight := uint(0)
			uniquePrimary := len(topLayerChannelIds) == 1 &&
				topLayerChannelIds[0] == primaryRoute.ChannelId
			if uniquePrimary {
				temporaryWeight = uint(math.Round(
					float64(channelMonitorSmartScheduleTemporaryWeightTotal) * adaptivePercent /
						channelMonitorScorePercentageTotal,
				))
				temporaryWeight = min(
					max(temporaryWeight, 1), uint(channelMonitorSmartScheduleTemporaryWeightTotal-1),
				)
			} else {
				exactWeight := float64(topLayerWeight) * adaptivePercent /
					(channelMonitorScorePercentageTotal - adaptivePercent)
				if !math.IsNaN(exactWeight) && !math.IsInf(exactWeight, 0) && exactWeight >= 1 &&
					exactWeight <= float64(^uint(0)) {
					temporaryWeight = uint(math.Round(exactWeight))
				}
			}
			if temporaryWeight == 0 {
				selectedChannelId = 0
				adaptivePercent = 0
			} else {
				for _, route := range routes {
					if route.State.BaseRank <= 0 || route.State.StabilityState != "" ||
						route.State.RuntimeProtectionUntil > now {
						continue
					}
					desiredPriority[route.ChannelId] = route.State.BasePriority
					desiredWeight[route.ChannelId] = route.State.BaseWeight
					if route.State.TemporaryTrafficKind != "" {
						snapshots[route.ChannelId] = &model.ChannelSmartScheduleRoutingSnapshotUpdate{
							LastPrioritySampleTime: route.State.LastPrioritySampleTime,
						}
					}
				}
				if primaryRoute.State.ManualPrimaryUntil > now {
					desiredPriority[primaryRoute.ChannelId] = primaryRoute.Priority
					desiredWeight[primaryRoute.ChannelId] = 1000
				}
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
					lastPrioritySampleTime := route.State.LastPrioritySampleTime
					if route.State.TemporaryTrafficKind != model.ChannelSmartScheduleTemporaryTrafficAdaptive ||
						temporarySince <= 0 {
						temporarySince = now
						lastPrioritySampleTime = now
					}
					snapshots[selectedChannelId] = &model.ChannelSmartScheduleRoutingSnapshotUpdate{
						TemporaryTrafficKind:          model.ChannelSmartScheduleTemporaryTrafficAdaptive,
						TemporaryTrafficSince:         temporarySince,
						TemporaryTrafficTargetPercent: adaptivePercent,
						ExplorationMaxPromptTokens:    policy.ExplorationMaxPromptTokens,
						LastPrioritySampleTime:        lastPrioritySampleTime,
					}
					break
				}
			}
		}
	}
	if selectedChannelId == 0 && existingAdaptive {
		for _, route := range routes {
			if route.State.BaseRank > 0 && route.State.StabilityState == "" &&
				route.State.RuntimeProtectionUntil <= now {
				desiredPriority[route.ChannelId] = route.State.BasePriority
				desiredWeight[route.ChannelId] = route.State.BaseWeight
				if route.State.ManualPrimaryUntil > now {
					desiredPriority[route.ChannelId] = route.Priority
					desiredWeight[route.ChannelId] = 1000
				}
			}
			if route.State.TemporaryTrafficKind == model.ChannelSmartScheduleTemporaryTrafficAdaptive {
				snapshots[route.ChannelId] = &model.ChannelSmartScheduleRoutingSnapshotUpdate{
					LastPrioritySampleTime: route.State.LastPrioritySampleTime,
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
				math.Abs(health.Pressure-route.State.AdaptiveHealthPressure) > channelMonitorRatioEpsilon)
		snapshot := snapshots[route.ChannelId]
		snapshotChanged := snapshot != nil &&
			(snapshot.TemporaryTrafficKind != route.State.TemporaryTrafficKind ||
				snapshot.TemporaryTrafficSince != route.State.TemporaryTrafficSince ||
				math.Abs(snapshot.TemporaryTrafficTargetPercent-route.State.TemporaryTrafficTargetPercent) > channelMonitorRatioEpsilon ||
				snapshot.ExplorationMaxPromptTokens != route.State.ExplorationMaxPromptTokens ||
				snapshot.LastPrioritySampleTime != route.State.LastPrioritySampleTime)
		applyPriorityWeight := route.State.Participates() && route.Enabled &&
			route.ChannelStatus == common.ChannelStatusEnabled && route.State.StabilityState == "" &&
			route.State.RuntimeProtectionUntil <= now &&
			(desiredPriority[route.ChannelId] != route.Priority || desiredWeight[route.ChannelId] != route.Weight)
		changed := healthChanged || snapshotChanged || applyPriorityWeight
		hasChanges = hasChanges || changed
		trafficStateChanged = trafficStateChanged || snapshotChanged
		update := model.ChannelSmartScheduleRouteResultUpdate{
			ChannelId: route.ChannelId, Group: route.Group, Model: route.Model,
			Priority: desiredPriority[route.ChannelId], Weight: desiredWeight[route.ChannelId],
			PoolGuard: true, ObservationOnly: !changed, AdaptiveOverlayOnly: true,
			ExpectedRevision:         route.State.Revision,
			ExpectedControlRevision:  expectedControlRevision,
			ExpectedParticipationSet: route.State.ParticipationSet,
			ExpectedExcluded:         route.State.Excluded,
			ExpectedAbilityEnabled:   route.Enabled,
			ExpectedChannelStatus:    route.ChannelStatus,
			ExpectedPriority:         route.Priority, ExpectedWeight: route.Weight,
			ApplyPriorityWeight: applyPriorityWeight,
			RoutingSnapshot:     snapshot,
		}
		if healthSet {
			channelSmartScheduleAttachHealthUpdate(&update, health)
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
	for _, outcome := range outcomes {
		conflict = conflict || !outcome.Applied
		routingChanged = routingChanged || outcome.RoutingChanged
	}
	if !conflict && (routingChanged || trafficStateChanged) {
		if cacheErr := model.RefreshChannelSmartScheduleRoutePoolCache(poolKey.group, poolKey.model); cacheErr != nil {
			common.SysError("刷新自适应备援路由缓存失败: " + cacheErr.Error())
			model.InitChannelCache()
		}
	}
	return conflict, nil
}
