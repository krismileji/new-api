package controller

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelMonitorScorePercentageTotal   = 100.0
	channelMonitorDefaultPrimaryTraffic  = 90.0
	channelMonitorMinPrimaryTraffic      = 51.0
	channelMonitorMaxPrimaryTraffic      = 99.0
	channelMonitorDefaultSwitchThreshold = 3.0
)

type channelSmartScheduleMetricPercentages struct {
	CostRatioPercent  float64 `json:"cost_ratio_percent"`
	FirstTokenPercent float64 `json:"first_token_percent"`
	TPSPercent        float64 `json:"tps_percent"`
}

type channelSmartScheduleScoring struct {
	StabilityPercent              float64                               `json:"stability_percent"`
	PrimaryTrafficPercent         float64                               `json:"primary_traffic_percent"`
	PrimarySwitchThresholdPercent float64                               `json:"primary_switch_threshold_percent"`
	Smart                         channelSmartScheduleMetricPercentages `json:"smart"`
	Ratio                         channelSmartScheduleMetricPercentages `json:"ratio"`
}

func defaultChannelSmartScheduleScoring() channelSmartScheduleScoring {
	return channelSmartScheduleScoring{
		StabilityPercent:              50,
		PrimaryTrafficPercent:         channelMonitorDefaultPrimaryTraffic,
		PrimarySwitchThresholdPercent: channelMonitorDefaultSwitchThreshold,
		Smart: channelSmartScheduleMetricPercentages{
			CostRatioPercent:  40,
			FirstTokenPercent: 40,
			TPSPercent:        20,
		},
		Ratio: channelSmartScheduleMetricPercentages{
			CostRatioPercent:  70,
			FirstTokenPercent: 20,
			TPSPercent:        10,
		},
	}
}

func (scoring *channelSmartScheduleScoring) UnmarshalJSON(data []byte) error {
	type scoringAlias channelSmartScheduleScoring
	normalized := scoringAlias(defaultChannelSmartScheduleScoring())
	if err := common.Unmarshal(data, &normalized); err != nil {
		return err
	}
	*scoring = channelSmartScheduleScoring(normalized)
	return nil
}

func validateChannelSmartScheduleScoring(scoring channelSmartScheduleScoring) error {
	if err := validateChannelSmartSchedulePercentage(scoring.StabilityPercent); err != nil {
		return errors.New("稳定性占比必须在 0% 到 100% 之间")
	}
	if math.IsNaN(scoring.PrimaryTrafficPercent) || math.IsInf(scoring.PrimaryTrafficPercent, 0) ||
		scoring.PrimaryTrafficPercent < channelMonitorMinPrimaryTraffic ||
		scoring.PrimaryTrafficPercent > channelMonitorMaxPrimaryTraffic {
		return errors.New("主渠道目标流量占比必须在 51% 到 99% 之间")
	}
	if err := validateChannelSmartSchedulePercentage(scoring.PrimarySwitchThresholdPercent); err != nil {
		return errors.New("主渠道切换分差必须在 0% 到 100% 之间")
	}
	if err := validateChannelSmartScheduleMetricPercentages(scoring.Smart); err != nil {
		return errors.New("智能调度的成本倍率、首字和 TPS 占比合计必须为 100%")
	}
	if err := validateChannelSmartScheduleMetricPercentages(scoring.Ratio); err != nil {
		return errors.New("按成本倍率调度的成本倍率、首字和 TPS 占比合计必须为 100%")
	}
	if scoring.Ratio.CostRatioPercent <= 0 {
		return errors.New("按成本倍率调度的成本倍率占比必须大于 0%")
	}
	return nil
}

func validateChannelSmartScheduleMetricPercentages(percentages channelSmartScheduleMetricPercentages) error {
	values := []float64{
		percentages.CostRatioPercent,
		percentages.FirstTokenPercent,
		percentages.TPSPercent,
	}
	total := 0.0
	for _, value := range values {
		if err := validateChannelSmartSchedulePercentage(value); err != nil {
			return err
		}
		total += value
	}
	if math.Abs(total-channelMonitorScorePercentageTotal) > channelMonitorRatioEpsilon {
		return errors.New("占比合计必须为 100%")
	}
	return nil
}

func validateChannelSmartSchedulePercentage(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > channelMonitorScorePercentageTotal {
		return errors.New("占比超出范围")
	}
	return nil
}

func channelSmartScheduleWeightedScore(parts ...channelSmartScheduleScorePart) float64 {
	weightedScore := 0.0
	totalPercent := 0.0
	for _, part := range parts {
		if !part.Available || part.Percent <= 0 {
			continue
		}
		weightedScore += part.Score * part.Percent
		totalPercent += part.Percent
	}
	if totalPercent <= channelMonitorRatioEpsilon {
		return 0
	}
	return weightedScore / totalPercent
}

func channelSmartScheduleUsesBusinessScore(stabilityEnabled bool, scoring channelSmartScheduleScoring) bool {
	return !stabilityEnabled ||
		channelMonitorScorePercentageTotal-scoring.StabilityPercent > channelMonitorRatioEpsilon
}

type channelSmartScheduleScorePart struct {
	Score     float64
	Percent   float64
	Available bool
}

type channelSmartScheduleJitterMeasurement struct {
	Available    bool
	ThresholdMs  float64
	SampleCount  int64
	SlowCount    int64
	AllowedCount int64
	Penalty      float64
}

func channelSmartScheduleMeasureJitter(
	buckets []model.ChannelMonitorDurationBucket,
	minSamples int,
	policy channelSmartSchedulePolicy,
) channelSmartScheduleJitterMeasurement {
	measurement := channelSmartScheduleJitterMeasurement{}
	if !policy.StabilityEnabled || !policy.JitterEnabled || minSamples <= 0 {
		return measurement
	}
	thresholdMs := policy.JitterSlowThresholdSeconds * 1000
	if math.IsNaN(thresholdMs) || math.IsInf(thresholdMs, 0) || thresholdMs < 0 {
		return measurement
	}
	measurement.ThresholdMs = thresholdMs
	for _, bucket := range buckets {
		if bucket.Count <= 0 {
			continue
		}
		measurement.SampleCount += bucket.Count
		lowerMs := float64(bucket.LowerBoundMs)
		upperMs := float64(bucket.UpperBoundMs)
		switch {
		case lowerMs >= thresholdMs:
			measurement.SlowCount += bucket.Count
		case bucket.UpperBoundMs > 0 && upperMs > thresholdMs && bucket.TotalMs > 0:
			// A bucket can straddle the configured threshold. Count only the
			// samples that must be above it given the bucket's total duration,
			// which deliberately favors avoiding false jitter releases.
			excessTotal := bucket.TotalMs - float64(bucket.Count)*thresholdMs
			if excessTotal > 0 {
				count := int64(math.Ceil(excessTotal / (upperMs - thresholdMs)))
				measurement.SlowCount += min(max(count, int64(0)), bucket.Count)
			}
		}
	}
	if measurement.SampleCount < int64(minSamples) {
		return measurement
	}
	measurement.Available = true
	if policy.JitterTolerancePercent > 0 {
		allowed := int64(math.Floor(
			float64(measurement.SampleCount) * policy.JitterTolerancePercent / channelMonitorScorePercentageTotal,
		))
		measurement.AllowedCount = min(max(allowed, int64(1)), measurement.SampleCount)
	}
	measurement.Penalty = float64(max(measurement.SlowCount-measurement.AllowedCount, int64(0)))
	return measurement
}

func channelSmartScheduleApplyJitterPenalty(score *float64, sampleCount int64, penalty float64) *float64 {
	if score == nil || sampleCount <= 0 || math.IsNaN(penalty) || math.IsInf(penalty, 0) || penalty <= 0 {
		return score
	}
	value := *score - penalty/float64(sampleCount)
	if value < 0 {
		value = 0
	} else if value > 1 {
		value = 1
	}
	return &value
}

func channelSmartScheduleRetryFailurePenalty(durationMs float64, policy channelSmartSchedulePolicy) float64 {
	basePenalty := policy.FastFailurePenaltyPercent / channelMonitorScorePercentageTotal
	if basePenalty < 0 {
		basePenalty = 0
	} else if basePenalty > 1 {
		basePenalty = 1
	}
	fastMs := policy.FastFailureSeconds * 1000
	slowMs := policy.SlowFailureSeconds * 1000
	if math.IsNaN(durationMs) || math.IsInf(durationMs, 0) || durationMs < 0 ||
		fastMs <= 0 || slowMs <= fastMs {
		return basePenalty
	}
	if durationMs <= fastMs {
		return basePenalty
	}
	if durationMs >= slowMs {
		return 1
	}
	progress := (durationMs - fastMs) / (slowMs - fastMs)
	return basePenalty + (1-basePenalty)*progress
}

func channelSmartScheduleStabilityScore(
	successCount int64,
	failureCount int64,
	finalFailureCount int64,
	buckets []model.ChannelMonitorFailureDurationBucket,
	policy channelSmartSchedulePolicy,
) (*float64, int64) {
	if successCount < 0 {
		successCount = 0
	}
	if failureCount < 0 {
		failureCount = 0
	}
	sampleCount := successCount + failureCount
	if sampleCount <= 0 {
		return nil, 0
	}
	if finalFailureCount < 0 {
		finalFailureCount = 0
	} else if finalFailureCount > failureCount {
		finalFailureCount = failureCount
	}
	retryFailureLimit := failureCount - finalFailureCount
	retryFailureCount := int64(0)
	penalty := float64(finalFailureCount)
	for _, bucket := range buckets {
		count := bucket.Count
		if count <= 0 || retryFailureCount >= retryFailureLimit {
			continue
		}
		if count > retryFailureLimit-retryFailureCount {
			count = retryFailureLimit - retryFailureCount
		}
		durationMs := float64(bucket.LowerBoundMs)
		if bucket.UpperBoundMs > bucket.LowerBoundMs {
			durationMs = float64(bucket.LowerBoundMs+bucket.UpperBoundMs) / 2
		} else if durationMs < policy.SlowFailureSeconds*1000 {
			durationMs = policy.SlowFailureSeconds * 1000
		}
		penalty += float64(count) * channelSmartScheduleRetryFailurePenalty(durationMs, policy)
		retryFailureCount += count
	}
	if retryFailureCount < retryFailureLimit {
		penalty += float64(retryFailureLimit-retryFailureCount) *
			channelSmartScheduleRetryFailurePenalty(0, policy)
	}
	score := 1 - penalty/float64(sampleCount)
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	return &score, sampleCount
}
