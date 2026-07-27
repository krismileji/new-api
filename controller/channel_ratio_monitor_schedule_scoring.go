package controller

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
)

const (
	channelMonitorSmartScheduleScoringOption = "ChannelMonitorSmartScheduleScoring"
	channelMonitorScorePercentageTotal       = 100.0
	channelMonitorMinScoreCurveExponent      = 0.1
	channelMonitorMaxScoreCurveExponent      = 5.0
	channelMonitorDefaultWeightSpreadStart   = 3.0
	channelMonitorDefaultWeightSpreadFull    = 10.0
)

type channelSmartScheduleMetricPercentages struct {
	CostRatioPercent  float64 `json:"cost_ratio_percent"`
	FirstTokenPercent float64 `json:"first_token_percent"`
	TPSPercent        float64 `json:"tps_percent"`
}

type channelSmartScheduleScoring struct {
	StabilityPercent           float64                               `json:"stability_percent"`
	CurveExponent              float64                               `json:"curve_exponent"`
	RelativeWeightEnabled      bool                                  `json:"relative_weight_enabled"`
	RelativeWeightStartPercent float64                               `json:"relative_weight_start_percent"`
	RelativeWeightFullPercent  float64                               `json:"relative_weight_full_percent"`
	Smart                      channelSmartScheduleMetricPercentages `json:"smart"`
	Ratio                      channelSmartScheduleMetricPercentages `json:"ratio"`
}

func defaultChannelSmartScheduleScoring() channelSmartScheduleScoring {
	return channelSmartScheduleScoring{
		StabilityPercent:           50,
		CurveExponent:              1,
		RelativeWeightEnabled:      true,
		RelativeWeightStartPercent: channelMonitorDefaultWeightSpreadStart,
		RelativeWeightFullPercent:  channelMonitorDefaultWeightSpreadFull,
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

func parseChannelSmartScheduleScoring(raw string) channelSmartScheduleScoring {
	defaults := defaultChannelSmartScheduleScoring()
	if raw == "" {
		return defaults
	}
	var scoring channelSmartScheduleScoring
	if common.UnmarshalJsonStr(raw, &scoring) != nil {
		return defaults
	}
	scoring = normalizeChannelSmartScheduleScoring(scoring)
	if validateChannelSmartScheduleScoring(scoring) != nil {
		return defaults
	}
	return scoring
}

func normalizeChannelSmartScheduleScoring(scoring channelSmartScheduleScoring) channelSmartScheduleScoring {
	if scoring.RelativeWeightStartPercent != 0 || scoring.RelativeWeightFullPercent != 0 {
		return scoring
	}
	defaults := defaultChannelSmartScheduleScoring()
	scoring.RelativeWeightEnabled = defaults.RelativeWeightEnabled
	scoring.RelativeWeightStartPercent = defaults.RelativeWeightStartPercent
	scoring.RelativeWeightFullPercent = defaults.RelativeWeightFullPercent
	return scoring
}

func validateChannelSmartScheduleScoring(scoring channelSmartScheduleScoring) error {
	if err := validateChannelSmartSchedulePercentage(scoring.StabilityPercent); err != nil {
		return errors.New("稳定性占比必须在 0% 到 100% 之间")
	}
	if math.IsNaN(scoring.CurveExponent) || math.IsInf(scoring.CurveExponent, 0) ||
		scoring.CurveExponent < channelMonitorMinScoreCurveExponent ||
		scoring.CurveExponent > channelMonitorMaxScoreCurveExponent {
		return errors.New("得分曲线指数必须在 0.1 到 5 之间")
	}
	if err := validateChannelSmartSchedulePercentage(scoring.RelativeWeightStartPercent); err != nil {
		return errors.New("相对权重拉伸起始分差必须在 0% 到 100% 之间")
	}
	if err := validateChannelSmartSchedulePercentage(scoring.RelativeWeightFullPercent); err != nil ||
		scoring.RelativeWeightFullPercent <= scoring.RelativeWeightStartPercent {
		return errors.New("相对权重拉伸完整分差必须大于起始分差且不超过 100%")
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
