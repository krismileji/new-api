package controller

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

const channelMonitorSmartScheduleGroupPoliciesOption = "ChannelMonitorSmartScheduleGroupPolicies"

type channelSmartScheduleGroupPolicy struct {
	Group            string                       `json:"group"`
	Strategy         *string                      `json:"strategy,omitempty"`
	StabilityEnabled *bool                        `json:"stability_enabled,omitempty"`
	Scoring          *channelSmartScheduleScoring `json:"scoring,omitempty"`
	ApplyMode        *string                      `json:"apply_mode,omitempty"`
	Models           *[]string                    `json:"models,omitempty"`
	MinSamples       *int                         `json:"min_samples,omitempty"`
	MinSuccessRate   *float64                     `json:"min_success_rate,omitempty"`
	CooldownMinutes  *int                         `json:"cooldown_minutes,omitempty"`
}

type smartScheduleGroupPolicies []channelSmartScheduleGroupPolicy

type channelSmartSchedulePolicy struct {
	Strategy         string
	StabilityEnabled bool
	Scoring          channelSmartScheduleScoring
	ApplyMode        string
	Models           []string
	MinSamples       int
	MinSuccessRate   float64
	CooldownMinutes  int
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

		if policy.Strategy != nil {
			strategy := strings.TrimSpace(*policy.Strategy)
			if normalizeChannelMonitorSmartScheduleStrategy(strategy) != strategy {
				return nil, errors.New("分组调度方式无效")
			}
			policy.Strategy = &strategy
		}
		if policy.Scoring != nil {
			scoring := normalizeChannelSmartScheduleScoring(*policy.Scoring)
			if err := validateChannelSmartScheduleScoring(scoring); err != nil {
				return nil, err
			}
			policy.Scoring = &scoring
		}
		if policy.ApplyMode != nil {
			applyMode := strings.TrimSpace(*policy.ApplyMode)
			if normalizeChannelMonitorSmartScheduleApplyMode(applyMode) != applyMode {
				return nil, errors.New("分组调度调整方式无效")
			}
			policy.ApplyMode = &applyMode
		}
		if policy.Models != nil {
			models, err := normalizeChannelMonitorSmartScheduleModels(*policy.Models)
			if err != nil {
				return nil, err
			}
			policy.Models = &models
		}
		if policy.MinSamples != nil && (*policy.MinSamples <= 0 || *policy.MinSamples > maxChannelMonitorSmartScheduleMinSamples) {
			return nil, errors.New("分组调度最少样本数必须在 1 到 100000 之间")
		}
		if policy.MinSuccessRate != nil &&
			(math.IsNaN(*policy.MinSuccessRate) || math.IsInf(*policy.MinSuccessRate, 0) ||
				*policy.MinSuccessRate < 0 || *policy.MinSuccessRate > maxChannelMonitorSmartScheduleSuccessRate) {
			return nil, errors.New("分组调度最低成功率必须在 0% 到 100% 之间")
		}
		if policy.CooldownMinutes != nil &&
			(*policy.CooldownMinutes <= 0 || *policy.CooldownMinutes > maxChannelMonitorAutoUpdateIntervalMinutes) {
			return nil, errors.New("分组调度降级时长必须在 1 到 525600 分钟之间")
		}
		normalized = append(normalized, policy)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Group < normalized[j].Group
	})
	return normalized, nil
}

func materializeChannelSmartScheduleGroupPolicies(
	policies []channelSmartScheduleGroupPolicy,
	defaults channelSmartSchedulePolicy,
) []channelSmartScheduleGroupPolicy {
	materialized := make([]channelSmartScheduleGroupPolicy, 0, len(policies))
	for _, policy := range policies {
		effective := policy.resolve(defaults)
		models := make([]string, len(effective.Models))
		copy(models, effective.Models)
		materialized = append(materialized, channelSmartScheduleGroupPolicy{
			Group:            policy.Group,
			Strategy:         &effective.Strategy,
			StabilityEnabled: &effective.StabilityEnabled,
			Scoring:          &effective.Scoring,
			ApplyMode:        &effective.ApplyMode,
			Models:           &models,
			MinSamples:       &effective.MinSamples,
			MinSuccessRate:   &effective.MinSuccessRate,
			CooldownMinutes:  &effective.CooldownMinutes,
		})
	}
	return materialized
}

func channelSmartSchedulePolicyFromSettings(settings channelMonitorSettings) channelSmartSchedulePolicy {
	return channelSmartSchedulePolicy{
		Strategy:         settings.SmartScheduleStrategy,
		StabilityEnabled: settings.SmartScheduleStabilityEnabled,
		Scoring:          settings.SmartScheduleScoring,
		ApplyMode:        settings.SmartScheduleApplyMode,
		Models:           settings.SmartScheduleModels,
		MinSamples:       settings.SmartScheduleMinSamples,
		MinSuccessRate:   settings.SmartScheduleMinSuccessRate,
		CooldownMinutes:  settings.SmartScheduleCooldownMinutes,
	}
}

func (override channelSmartScheduleGroupPolicy) resolve(defaults channelSmartSchedulePolicy) channelSmartSchedulePolicy {
	policy := defaults
	if override.Strategy != nil {
		policy.Strategy = *override.Strategy
	}
	if override.StabilityEnabled != nil {
		policy.StabilityEnabled = *override.StabilityEnabled
	}
	if override.Scoring != nil {
		policy.Scoring = *override.Scoring
	}
	if override.ApplyMode != nil {
		policy.ApplyMode = *override.ApplyMode
	}
	if override.Models != nil {
		policy.Models = *override.Models
	}
	if override.MinSamples != nil {
		policy.MinSamples = *override.MinSamples
	}
	if override.MinSuccessRate != nil {
		policy.MinSuccessRate = *override.MinSuccessRate
	}
	if override.CooldownMinutes != nil {
		policy.CooldownMinutes = *override.CooldownMinutes
	}
	return policy
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
