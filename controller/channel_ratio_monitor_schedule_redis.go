package controller

import (
	"context"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

const (
	channelSmartScheduleRedisTriggerSource = "redis.health_event"
	channelSmartScheduleRedisDirtyReason   = "scheduling_eligible_health_changed"
)

func init() {
	service.RegisterChannelMonitorRedisSchedulingTrigger(requestChannelSmartScheduleRunForRedisEvents)
}

func requestChannelSmartScheduleRunForRedisEvents(
	ctx context.Context,
	events []model.ChannelMonitorEvent,
) error {
	if len(events) == 0 {
		return nil
	}
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled || len(settings.SmartScheduleGroupPolicies) == 0 {
		return nil
	}
	var maxEventSequence uint64
	for _, event := range events {
		if !event.SchedulingEligible {
			continue
		}
		if event.EventSequence == 0 || event.EventSequence > math.MaxInt64 {
			return errors.New("渠道监控 Redis 完整调度事件顺序无效")
		}
		maxEventSequence = max(maxEventSequence, event.EventSequence)
	}
	if maxEventSequence == 0 {
		return nil
	}
	payload := newChannelSmartScheduleTaskPayload(
		channelSmartScheduleRedisTriggerSource,
		channelSmartScheduleRedisDirtyReason,
	)
	payload.TriggerCount = len(events)
	_, _, _, err := service.EnqueueRequiredSystemTaskAfterRedisSequence(
		channelMonitorSmartScheduleTaskType,
		payload,
		int64(maxEventSequence),
	)
	return err
}
