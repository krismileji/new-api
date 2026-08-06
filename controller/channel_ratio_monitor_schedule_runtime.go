package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const channelSmartScheduleRuntimeFailureRedisKeyPrefix = "channelSmartScheduleRuntimeFailures:v1:"

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
