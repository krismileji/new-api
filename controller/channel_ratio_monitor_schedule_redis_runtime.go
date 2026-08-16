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

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var errChannelSmartScheduleRedisAdaptiveRefreshDeferred = errors.New("完整调度运行期间延后 Redis 自适应刷新")

type channelSmartScheduleRedisRuntimeRouteKey struct {
	channelId int
	modelName string
}

func init() {
	service.RegisterChannelMonitorRedisRuntimeEffectHandler(applyChannelSmartScheduleRedisRuntimeEffects)
}

func applyChannelSmartScheduleRedisRuntimeEffects(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	affected := make(map[channelSmartScheduleRedisRuntimeRouteKey][]model.ChannelMonitorEvent)
	for _, event := range events {
		if !event.SchedulingEligible {
			continue
		}
		modelName := ratio_setting.FormatMatchingModelName(strings.TrimSpace(event.ModelName))
		if event.ChannelId <= 0 || modelName == "" {
			continue
		}
		event.ModelName = modelName
		key := channelSmartScheduleRedisRuntimeRouteKey{channelId: event.ChannelId, modelName: modelName}
		affected[key] = append(affected[key], event)
	}
	keys := make([]channelSmartScheduleRedisRuntimeRouteKey, 0, len(affected))
	for key := range affected {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i int, j int) bool {
		if keys[i].modelName != keys[j].modelName {
			return keys[i].modelName < keys[j].modelName
		}
		return keys[i].channelId < keys[j].channelId
	})
	for _, key := range keys {
		window, available, err := service.GetChannelMonitorRedisRouteHealthWindow(ctx, key.channelId, key.modelName)
		if err != nil {
			return err
		}
		if !available {
			return service.ErrChannelMonitorRedisRouteHealthUnavailable
		}
		effectAt, eventSequence, err := channelSmartScheduleRedisRuntimeEffectPosition(affected[key])
		if err != nil {
			return err
		}
		windowStart := effectAt - int64(maxChannelSmartScheduleRuntimeRetentionSeconds())
		runtimeEvents := channelSmartScheduleRedisWindowEvents(
			window,
			key.channelId,
			key.modelName,
			windowStart,
			0,
			maxChannelSmartScheduleRuntimeRequestEvents,
			eventSequence,
		)
		health := channelSmartScheduleRuntimeHealthFromEvents(
			runtimeEvents,
			window.Snapshot.CoverageStart,
			windowStart,
		)
		rateLimited, failure := channelSmartScheduleRedisRuntimeFailures(affected[key])
		if rateLimited != nil {
			if err := applyChannelSmartScheduleRuntimeFailureWithSource(
				key.channelId,
				key.modelName,
				channelSmartScheduleRedisEventError(*rateLimited),
				false,
				false,
				false,
				&health,
				false,
				rateLimited.OccurredAt,
				int64(rateLimited.EventSequence),
			); err != nil {
				return err
			}
		}
		if failure != nil {
			if err := applyChannelSmartScheduleRuntimeFailureWithSource(
				key.channelId,
				key.modelName,
				channelSmartScheduleRedisEventError(*failure),
				false,
				false,
				false,
				&health,
				false,
				failure.OccurredAt,
				int64(failure.EventSequence),
			); err != nil {
				return err
			}
		}
		if err := refreshChannelSmartScheduleRedisAdaptiveRoute(
			ctx,
			key.channelId,
			key.modelName,
			effectAt,
			eventSequence,
		); err != nil {
			return err
		}
	}
	return nil
}

func channelSmartScheduleRedisRuntimeEffectPosition(
	events []model.ChannelMonitorEvent,
) (int64, int64, error) {
	var latest *model.ChannelMonitorEvent
	for index := range events {
		event := &events[index]
		if event.EventSequence == 0 || event.EventSequence > math.MaxInt64 {
			return 0, 0, errors.New("渠道监控 Redis 运行时事件顺序无效")
		}
		if latest == nil || event.EventSequence > latest.EventSequence {
			latest = event
		}
	}
	if latest == nil {
		return 0, 0, errors.New("渠道监控 Redis 运行时事件为空")
	}
	return latest.OccurredAt, int64(latest.EventSequence), nil
}

func channelSmartScheduleRedisRuntimeFailures(
	events []model.ChannelMonitorEvent,
) (*model.ChannelMonitorEvent, *model.ChannelMonitorEvent) {
	sort.SliceStable(events, func(i int, j int) bool {
		if events[i].EventSequence != events[j].EventSequence {
			return events[i].EventSequence < events[j].EventSequence
		}
		if events[i].OccurredAt != events[j].OccurredAt {
			return events[i].OccurredAt < events[j].OccurredAt
		}
		return events[i].EventId < events[j].EventId
	})
	var rateLimited *model.ChannelMonitorEvent
	var failure *model.ChannelMonitorEvent
	for index := range events {
		event := events[index]
		if event.Source != model.ChannelMonitorEventSourceBusiness ||
			!event.RuntimeProtectionEligible || !event.RequestDispatched ||
			event.FinalRetrySummary || event.Outcome != model.ChannelMonitorEventOutcomeFailure {
			continue
		}
		if event.StatusCode != nil && *event.StatusCode == http.StatusTooManyRequests {
			copy := event
			rateLimited = &copy
			continue
		}
		copy := event
		failure = &copy
	}
	return rateLimited, failure
}

func channelSmartScheduleRedisEventError(event model.ChannelMonitorEvent) *types.NewAPIError {
	statusCode := http.StatusInternalServerError
	if event.StatusCode != nil {
		statusCode = *event.StatusCode
	}
	message := strings.TrimSpace(event.ErrorMessage)
	if message == "" {
		message = "渠道请求失败"
	}
	errorCode := types.ErrorCodeBadResponseStatusCode
	if strings.TrimSpace(event.ErrorCode) != "" {
		errorCode = types.ErrorCode(event.ErrorCode)
	}
	return types.NewErrorWithStatusCode(errors.New(message), errorCode, statusCode)
}

func channelSmartScheduleRedisWindowEvents(
	window service.ChannelMonitorRedisRouteHealthWindow,
	channelId int,
	modelName string,
	windowStart int64,
	observationSince int64,
	maxRequests int,
	maxEventSequence int64,
) []model.ChannelMonitorEvent {
	windowStart = max(windowStart, observationSince)
	events := make([]model.ChannelMonitorEvent, 0, len(window.Samples))
	for _, sample := range window.Samples {
		if maxEventSequence > 0 && sample.EventSequence > uint64(maxEventSequence) {
			continue
		}
		if sample.OccurredAt < windowStart || !sample.SchedulingEligible ||
			!sample.RequestDispatched || sample.FinalRetrySummary ||
			sample.Outcome == model.ChannelMonitorEventOutcomeCanceled ||
			sample.Outcome == model.ChannelMonitorEventOutcomeUnresolved {
			continue
		}
		if sample.StatusCode != nil && *sample.StatusCode == http.StatusTooManyRequests {
			continue
		}
		events = append(events, model.ChannelMonitorEvent{
			EventId: sample.EventID, EventSequence: sample.EventSequence,
			OccurredAt: sample.OccurredAt, ChannelId: channelId, ModelName: modelName,
			GroupName: sample.GroupName,
			Source: sample.Source, Outcome: sample.Outcome,
			IsRetryAttempt: sample.IsRetryAttempt, IsFinalAttempt: sample.IsFinalAttempt,
			FinalRetrySummary: sample.FinalRetrySummary, RequestDispatched: sample.RequestDispatched,
			SchedulingEligible:        sample.SchedulingEligible,
			RuntimeProtectionEligible: sample.RuntimeProtectionEligible,
			StatusCode:                sample.StatusCode, ErrorCode: sample.ErrorCode, ErrorMessage: sample.ErrorMessage,
			FirstTokenMs: sample.FirstTokenMs, TPS: sample.TPS, AttemptDurationMs: sample.AttemptDurationMs,
		})
	}
	if maxRequests > 0 && len(events) > maxRequests {
		events = events[len(events)-maxRequests:]
	}
	return events
}

func channelSmartScheduleRedisAdaptiveMetric(
	ctx context.Context,
	channelId int,
	modelName string,
	windowStart int64,
	observationSince int64,
	maxRequests int,
	warningSeconds float64,
	criticalSeconds float64,
) (model.ChannelSmartScheduleAdaptiveHealthMetric, error) {
	window, available, err := service.GetChannelMonitorRedisRouteHealthWindow(
		ctx,
		channelId,
		modelName,
	)
	if err != nil {
		return model.ChannelSmartScheduleAdaptiveHealthMetric{}, err
	}
	if !available {
		return model.ChannelSmartScheduleAdaptiveHealthMetric{}, nil
	}
	events := channelSmartScheduleRedisWindowEvents(
		window,
		channelId,
		modelName,
		windowStart,
		observationSince,
		maxRequests,
		0,
	)
	return channelSmartScheduleRealtimeAdaptiveMetricFromEvents(events, warningSeconds, criticalSeconds), nil
}

func refreshChannelSmartScheduleRedisAdaptiveRoute(
	ctx context.Context,
	channelId int,
	modelName string,
	effectAt int64,
	redisEventSequence int64,
) error {
	settings := getChannelMonitorRuntimeSettings()
	if !settings.SmartScheduleEnabled || len(settings.SmartScheduleGroupPolicies) == 0 {
		return nil
	}
	running, err := model.IsSystemTaskRunning(channelMonitorSmartScheduleTaskType)
	if err != nil {
		return fmt.Errorf("自适应备援检查完整调度任务失败: %w", err)
	}
	if running {
		return errChannelSmartScheduleRedisAdaptiveRefreshDeferred
	}
	participatingRoutes, err := model.GetChannelSmartScheduleRuntimeParticipatingRoutes(channelId, modelName)
	if err != nil {
		return fmt.Errorf("自适应备援参与路由读取失败: %w", err)
	}
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policyByGroup[configured.Group] = configured.policy()
	}
	pools := make([]channelSmartScheduleRoutePoolKey, 0, len(participatingRoutes))
	for group, routeModelName := range participatingRoutes {
		policy, configured := policyByGroup[group]
		softRoutingEnabled := policy.ApplyMode == channelMonitorSmartScheduleApplyPriorityWeight &&
			(policy.AdaptiveSamplingEnabled || policy.SampleMode == channelMonitorSmartScheduleSampleTraffic)
		if !configured || (!softRoutingEnabled && !policy.StabilityEnabled) ||
			(len(policy.Models) > 0 && !slices.Contains(policy.Models, routeModelName)) {
			continue
		}
		pools = append(pools, channelSmartScheduleRoutePoolKey{group: group, model: routeModelName})
	}
	sort.Slice(pools, func(i int, j int) bool {
		if pools[i].group != pools[j].group {
			return pools[i].group < pools[j].group
		}
		return pools[i].model < pools[j].model
	})
	for _, pool := range pools {
		readMetric := channelSmartScheduleRedisAdaptiveMetric
		if redisEventSequence > 0 {
			readMetric = func(
				ctx context.Context,
				channelId int,
				modelName string,
				windowStart int64,
				observationSince int64,
				maxRequests int,
				warningSeconds float64,
				criticalSeconds float64,
			) (model.ChannelSmartScheduleAdaptiveHealthMetric, error) {
				window, available, err := service.GetChannelMonitorRedisRouteHealthWindow(
					ctx, channelId, modelName,
				)
				if err != nil || !available {
					return model.ChannelSmartScheduleAdaptiveHealthMetric{}, err
				}
				events := channelSmartScheduleRedisWindowEvents(
					window, channelId, modelName, windowStart, observationSince,
					maxRequests, redisEventSequence,
				)
				return channelSmartScheduleRealtimeAdaptiveMetricFromEvents(
					events, warningSeconds, criticalSeconds,
				), nil
			}
		}
		conflict, err := refreshChannelSmartScheduleAdaptivePoolWithMetricReader(
			ctx,
			pool,
			policyByGroup[pool.group],
			settings.SmartScheduleControlRevision,
			model.DB,
			readMetric,
			service.ChannelRateLimitCooldownUntilMatchingFromRedis,
			effectAt,
			redisEventSequence,
		)
		if err != nil {
			return fmt.Errorf("智能调度池级 Redis 软刷新失败: %w", err)
		}
		if conflict {
			return fmt.Errorf("智能调度池级 Redis 软刷新发生配置冲突: group=%s model=%s", pool.group, pool.model)
		}
	}
	return nil
}
