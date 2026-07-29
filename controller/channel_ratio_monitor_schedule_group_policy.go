package controller

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const (
	channelMonitorSmartScheduleGroupPoliciesOption       = "ChannelMonitorSmartScheduleGroupPolicies"
	maxChannelMonitorSmartScheduleFailureSeconds         = 60
	maxChannelMonitorSmartScheduleJitterTolerancePercent = 50
	maxChannelMonitorSmartScheduleJitterMultiplier       = 20
	maxChannelMonitorSmartScheduleJitterToleranceMs      = 60000
	maxChannelMonitorSmartScheduleJitterBaselineHours    = 720
)

type channelSmartScheduleGroupPolicy struct {
	Group                     string                       `json:"group"`
	Strategy                  *string                      `json:"strategy,omitempty"`
	StabilityEnabled          *bool                        `json:"stability_enabled,omitempty"`
	Scoring                   *channelSmartScheduleScoring `json:"scoring,omitempty"`
	ApplyMode                 *string                      `json:"apply_mode,omitempty"`
	Models                    *[]string                    `json:"models,omitempty"`
	MinSamples                *int                         `json:"min_samples,omitempty"`
	DegradeStabilityScore     *float64                     `json:"degrade_stability_score,omitempty"`
	RecoveryStabilityScore    *float64                     `json:"recovery_stability_score,omitempty"`
	FastFailurePenaltyPercent *float64                     `json:"fast_failure_penalty_percent,omitempty"`
	FastFailureSeconds        *float64                     `json:"fast_failure_seconds,omitempty"`
	SlowFailureSeconds        *float64                     `json:"slow_failure_seconds,omitempty"`
	JitterEnabled             *bool                        `json:"jitter_enabled,omitempty"`
	JitterTolerancePercent    *float64                     `json:"jitter_tolerance_percent,omitempty"`
	JitterThresholdMultiplier *float64                     `json:"jitter_threshold_multiplier,omitempty"`
	JitterAbsoluteToleranceMs *int                         `json:"jitter_absolute_tolerance_ms,omitempty"`
	JitterBaselineHours       *int                         `json:"jitter_baseline_hours,omitempty"`
	CooldownMinutes           *int                         `json:"cooldown_minutes,omitempty"`
	SampleMode                *string                      `json:"sample_mode,omitempty"`
	ExplorationTrafficPercent *float64                     `json:"exploration_traffic_percent,omitempty"`
	ProbeIntervalMinutes      *int                         `json:"probe_interval_minutes,omitempty"`
}

type smartScheduleGroupPolicies []channelSmartScheduleGroupPolicy

type channelSmartSchedulePolicy struct {
	Strategy                  string
	StabilityEnabled          bool
	Scoring                   channelSmartScheduleScoring
	ApplyMode                 string
	Models                    []string
	MinSamples                int
	DegradeStabilityScore     float64
	RecoveryStabilityScore    float64
	FastFailurePenaltyPercent float64
	FastFailureSeconds        float64
	SlowFailureSeconds        float64
	JitterEnabled             bool
	JitterTolerancePercent    float64
	JitterThresholdMultiplier float64
	JitterAbsoluteToleranceMs int
	JitterBaselineHours       int
	CooldownMinutes           int
	SampleMode                string
	ExplorationTrafficPercent float64
	ProbeIntervalMinutes      int
}

func parseChannelSmartScheduleGroupPolicies(raw string) []channelSmartScheduleGroupPolicy {
	if strings.TrimSpace(raw) == "" {
		return []channelSmartScheduleGroupPolicy{}
	}
	var policies []channelSmartScheduleGroupPolicy
	if common.UnmarshalJsonStr(raw, &policies) != nil {
		return []channelSmartScheduleGroupPolicy{}
	}
	normalized, err := normalizeChannelSmartScheduleGroupPolicies(policies)
	if err != nil {
		return []channelSmartScheduleGroupPolicy{}
	}
	return normalized
}

func normalizeChannelSmartScheduleGroupPolicies(policies []channelSmartScheduleGroupPolicy) ([]channelSmartScheduleGroupPolicy, error) {
	if len(policies) > maxChannelMonitorSmartScheduleGroupCount {
		return nil, errors.New("分组调度策略不能超过 100 个")
	}
	normalized := make([]channelSmartScheduleGroupPolicy, 0, len(policies))
	seenGroups := make(map[string]struct{}, len(policies))
	for _, policy := range policies {
		policy.Group = strings.TrimSpace(policy.Group)
		if policy.Group == "" {
			return nil, errors.New("分组调度策略的分组名称不能为空")
		}
		if utf8.RuneCountInString(policy.Group) > maxChannelMonitorSmartScheduleGroupLength {
			return nil, errors.New("分组调度策略的分组名称不能超过 64 个字符")
		}
		if _, exists := seenGroups[policy.Group]; exists {
			return nil, errors.New("同一分组不能配置多个调度策略")
		}
		seenGroups[policy.Group] = struct{}{}
		if policy.Strategy == nil || policy.StabilityEnabled == nil || policy.Scoring == nil ||
			policy.ApplyMode == nil || policy.Models == nil || policy.MinSamples == nil ||
			policy.DegradeStabilityScore == nil || policy.RecoveryStabilityScore == nil ||
			policy.FastFailurePenaltyPercent == nil || policy.FastFailureSeconds == nil ||
			policy.SlowFailureSeconds == nil || policy.JitterEnabled == nil ||
			policy.JitterTolerancePercent == nil || policy.JitterThresholdMultiplier == nil ||
			policy.JitterAbsoluteToleranceMs == nil || policy.JitterBaselineHours == nil ||
			policy.CooldownMinutes == nil ||
			policy.SampleMode == nil || policy.ExplorationTrafficPercent == nil ||
			policy.ProbeIntervalMinutes == nil {
			return nil, errors.New("分组调度策略必须完整配置调度方式、稳定性保护、评分、调整方式、参与模型、最少样本数、稳定性阈值、失败耗时、成功延迟抖动、降级时长和采样方式")
		}

		strategy := strings.TrimSpace(*policy.Strategy)
		if !isChannelMonitorSmartScheduleStrategySupported(strategy) {
			return nil, errors.New("分组调度方式无效")
		}
		policy.Strategy = &strategy
		scoring := *policy.Scoring
		if err := validateChannelSmartScheduleScoring(scoring); err != nil {
			return nil, err
		}
		policy.Scoring = &scoring
		applyMode := strings.TrimSpace(*policy.ApplyMode)
		if !isChannelMonitorSmartScheduleApplyModeSupported(applyMode) {
			return nil, errors.New("分组调度调整方式无效")
		}
		policy.ApplyMode = &applyMode
		models, err := normalizeChannelMonitorSmartScheduleModels(*policy.Models)
		if err != nil {
			return nil, err
		}
		policy.Models = &models
		if *policy.MinSamples <= 0 || *policy.MinSamples > maxChannelMonitorSmartScheduleMinSamples {
			return nil, errors.New("分组调度最少样本数必须在 1 到 100000 之间")
		}
		if math.IsNaN(*policy.DegradeStabilityScore) || math.IsInf(*policy.DegradeStabilityScore, 0) ||
			*policy.DegradeStabilityScore < 0 || *policy.DegradeStabilityScore > maxChannelMonitorSmartScheduleSuccessRate {
			return nil, errors.New("分组调度降级稳定性得分必须在 0% 到 100% 之间")
		}
		if math.IsNaN(*policy.RecoveryStabilityScore) || math.IsInf(*policy.RecoveryStabilityScore, 0) ||
			*policy.RecoveryStabilityScore <= *policy.DegradeStabilityScore ||
			*policy.RecoveryStabilityScore > maxChannelMonitorSmartScheduleSuccessRate {
			return nil, errors.New("分组调度恢复稳定性得分必须大于降级得分且不超过 100%")
		}
		if math.IsNaN(*policy.FastFailurePenaltyPercent) || math.IsInf(*policy.FastFailurePenaltyPercent, 0) ||
			*policy.FastFailurePenaltyPercent < 0 || *policy.FastFailurePenaltyPercent > maxChannelMonitorSmartScheduleSuccessRate {
			return nil, errors.New("分组调度快速失败惩罚必须在 0% 到 100% 之间")
		}
		if math.IsNaN(*policy.FastFailureSeconds) || math.IsInf(*policy.FastFailureSeconds, 0) ||
			*policy.FastFailureSeconds <= 0 || *policy.FastFailureSeconds >= maxChannelMonitorSmartScheduleFailureSeconds {
			return nil, errors.New("分组调度快速失败界限必须大于 0 秒且小于 60 秒")
		}
		if math.IsNaN(*policy.SlowFailureSeconds) || math.IsInf(*policy.SlowFailureSeconds, 0) ||
			*policy.SlowFailureSeconds <= *policy.FastFailureSeconds ||
			*policy.SlowFailureSeconds > maxChannelMonitorSmartScheduleFailureSeconds {
			return nil, errors.New("分组调度慢失败界限必须大于快速失败界限且不超过 60 秒")
		}
		if math.IsNaN(*policy.JitterTolerancePercent) || math.IsInf(*policy.JitterTolerancePercent, 0) ||
			*policy.JitterTolerancePercent < 0 ||
			*policy.JitterTolerancePercent > maxChannelMonitorSmartScheduleJitterTolerancePercent {
			return nil, errors.New("分组调度允许抖动比例必须在 0% 到 50% 之间")
		}
		if math.IsNaN(*policy.JitterThresholdMultiplier) || math.IsInf(*policy.JitterThresholdMultiplier, 0) ||
			*policy.JitterThresholdMultiplier <= 1 ||
			*policy.JitterThresholdMultiplier > maxChannelMonitorSmartScheduleJitterMultiplier {
			return nil, errors.New("分组调度抖动阈值倍数必须大于 1 且不超过 20")
		}
		if *policy.JitterAbsoluteToleranceMs < 0 ||
			*policy.JitterAbsoluteToleranceMs > maxChannelMonitorSmartScheduleJitterToleranceMs {
			return nil, errors.New("分组调度抖动绝对容差必须在 0 到 60000 毫秒之间")
		}
		if *policy.JitterBaselineHours <= 0 ||
			*policy.JitterBaselineHours > maxChannelMonitorSmartScheduleJitterBaselineHours {
			return nil, errors.New("分组调度抖动基准学习周期必须在 1 到 720 小时之间")
		}
		if *policy.CooldownMinutes <= 0 || *policy.CooldownMinutes > maxChannelMonitorAutoUpdateIntervalMinutes {
			return nil, errors.New("分组调度降级时长必须在 1 到 525600 分钟之间")
		}
		sampleMode := strings.TrimSpace(*policy.SampleMode)
		if !isChannelMonitorSmartScheduleSampleModeSupported(sampleMode) {
			return nil, errors.New("分组调度采样方式无效")
		}
		if sampleMode == channelMonitorSmartScheduleSampleTraffic &&
			applyMode != channelMonitorSmartScheduleApplyPriorityWeight {
			return nil, errors.New("探索流量仅支持按优先级和权重调整")
		}
		policy.SampleMode = &sampleMode
		if math.IsNaN(*policy.ExplorationTrafficPercent) || math.IsInf(*policy.ExplorationTrafficPercent, 0) ||
			*policy.ExplorationTrafficPercent <= 0 ||
			*policy.ExplorationTrafficPercent > maxChannelMonitorSmartScheduleExplorationPercent {
			return nil, errors.New("分组调度探索流量必须大于 0% 且不超过 20%")
		}
		if *policy.ProbeIntervalMinutes <= 0 ||
			*policy.ProbeIntervalMinutes > maxChannelMonitorAutoUpdateIntervalMinutes {
			return nil, errors.New("分组调度探测间隔必须在 1 到 525600 分钟之间")
		}
		normalized = append(normalized, policy)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Group < normalized[j].Group
	})
	return normalized, nil
}

func (configured channelSmartScheduleGroupPolicy) policy() channelSmartSchedulePolicy {
	return channelSmartSchedulePolicy{
		Strategy:                  *configured.Strategy,
		StabilityEnabled:          *configured.StabilityEnabled,
		Scoring:                   *configured.Scoring,
		ApplyMode:                 *configured.ApplyMode,
		Models:                    *configured.Models,
		MinSamples:                *configured.MinSamples,
		DegradeStabilityScore:     *configured.DegradeStabilityScore,
		RecoveryStabilityScore:    *configured.RecoveryStabilityScore,
		FastFailurePenaltyPercent: *configured.FastFailurePenaltyPercent,
		FastFailureSeconds:        *configured.FastFailureSeconds,
		SlowFailureSeconds:        *configured.SlowFailureSeconds,
		JitterEnabled:             *configured.JitterEnabled,
		JitterTolerancePercent:    *configured.JitterTolerancePercent,
		JitterThresholdMultiplier: *configured.JitterThresholdMultiplier,
		JitterAbsoluteToleranceMs: *configured.JitterAbsoluteToleranceMs,
		JitterBaselineHours:       *configured.JitterBaselineHours,
		CooldownMinutes:           *configured.CooldownMinutes,
		SampleMode:                *configured.SampleMode,
		ExplorationTrafficPercent: *configured.ExplorationTrafficPercent,
		ProbeIntervalMinutes:      *configured.ProbeIntervalMinutes,
	}
}

func isChannelMonitorSmartScheduleSampleModeSupported(mode string) bool {
	switch mode {
	case channelMonitorSmartScheduleSampleOff,
		channelMonitorSmartScheduleSampleTraffic,
		channelMonitorSmartScheduleSampleProbe:
		return true
	default:
		return false
	}
}

func (policy channelSmartSchedulePolicy) needsPerformance() bool {
	if !channelSmartScheduleUsesBusinessScore(policy.StabilityEnabled, policy.Scoring) {
		return false
	}
	return policy.Strategy == channelMonitorSmartScheduleStrategyFirstToken ||
		policy.Strategy == channelMonitorSmartScheduleStrategyTPS ||
		(policy.Strategy == channelMonitorSmartScheduleStrategySmart &&
			(policy.Scoring.Smart.FirstTokenPercent > 0 || policy.Scoring.Smart.TPSPercent > 0)) ||
		(policy.Strategy == channelMonitorSmartScheduleStrategyRatio &&
			(policy.Scoring.Ratio.FirstTokenPercent > 0 || policy.Scoring.Ratio.TPSPercent > 0))
}

func (policy channelSmartSchedulePolicy) needsRatio() bool {
	if !channelSmartScheduleUsesBusinessScore(policy.StabilityEnabled, policy.Scoring) {
		return false
	}
	return (policy.Strategy == channelMonitorSmartScheduleStrategyRatio && policy.Scoring.Ratio.CostRatioPercent > 0) ||
		(policy.Strategy == channelMonitorSmartScheduleStrategySmart && policy.Scoring.Smart.CostRatioPercent > 0)
}
