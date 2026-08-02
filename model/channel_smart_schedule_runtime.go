package model

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// GetChannelSmartScheduleRouteSampleCount returns the total stability samples
// available for one channel/model route in the requested window. Production
// request samples come from minute metrics; manual tests and scheduled probes
// are stored in the shared sample buffer because their logs are excluded from
// the production aggregation.
func GetChannelSmartScheduleRouteSampleCount(
	ctx context.Context,
	startTimestamp int64,
	channelId int,
	modelName string,
) (int64, error) {
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || modelName == "" {
		return 0, nil
	}
	metric, err := GetChannelMonitorRouteStabilityMetric(ctx, startTimestamp, channelId, modelName)
	if err != nil {
		return 0, err
	}

	var state ChannelSmartScheduleModelSampleState
	err = DB.WithContext(ctx).
		Where(&ChannelSmartScheduleModelSampleState{ChannelId: channelId, ModelName: modelName}).
		First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return metric.SampleCount, nil
	}
	if err != nil {
		return 0, err
	}
	return metric.SampleCount + state.MetricsSince(startTimestamp).SampleCount, nil
}
