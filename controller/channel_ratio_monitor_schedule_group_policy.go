package controller

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelMonitorSmartScheduleGroupPoliciesOption                     = model.ChannelMonitorSmartScheduleGroupPoliciesOption
	maxChannelMonitorSmartScheduleFailureSeconds                       = 60
	maxChannelMonitorSmartScheduleJitterTolerancePercent               = 50
	maxChannelMonitorSmartScheduleJitterSlowThresholdSeconds           = 60
	maxChannelMonitorSmartScheduleBurstFailureWindowMinutes            = 60
	maxChannelMonitorSmartScheduleBurstFailureWindowRequests           = 1000
	maxChannelMonitorSmartScheduleBurstFailureThresholdPercent         = 100
	maxChannelMonitorSmartScheduleBurstFailureWindowSeconds            = 300
	maxChannelMonitorSmartScheduleRuntimeFailureThreshold              = 100
	maxChannelMonitorSmartScheduleFastFailureSameChannelRetryCount     = 10
	maxChannelMonitorSmartScheduleFastRetryDelayMs                     = 60_000
	defaultChannelMonitorSmartScheduleBurstFailureWindowMinutes        = 1
	defaultChannelMonitorSmartScheduleBurstFailureWindowRequests       = 100
	defaultChannelMonitorSmartScheduleBurstFailureThresholdPercent     = 3.0
	defaultChannelMonitorSmartScheduleBurstFailureWindowSeconds        = 30
	defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold      = 2
	defaultChannelMonitorSmartScheduleBurstFailureThreshold            = 3
	defaultChannelMonitorSmartScheduleRecoverySuccessThreshold         = 2
	defaultChannelMonitorSmartScheduleFastFailureSameChannelRetryCount = 0
	defaultChannelMonitorSmartScheduleFastRetryDelayMs                 = 1_000
	minChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes        = 1
	maxChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes        = 60
	maxChannelMonitorSmartScheduleAdaptiveSamplingWindowRequests       = 1000
	defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes    = 10
	defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowRequests   = 100
	minChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds        = 60
	maxChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds        = 3600
	maxChannelMonitorSmartScheduleAdaptiveSamplingMinComparable        = 10
)

type channelSmartScheduleGroupPolicy struct {
	Group                                           string                       `json:"group"`
	Strategy                                        *string                      `json:"strategy,omitempty"`
	StabilityEnabled                                *bool                        `json:"stability_enabled,omitempty"`
	StabilityWindowMinutes                          *int                         `json:"stability_window_minutes,omitempty"`
	Scoring                                         *channelSmartScheduleScoring `json:"scoring,omitempty"`
	ApplyMode                                       *string                      `json:"apply_mode,omitempty"`
	Models                                          *[]string                    `json:"models,omitempty"`
	ModelOrder                                      []string                     `json:"model_order,omitempty"`
	MinSamples                                      *int                         `json:"min_samples,omitempty"`
	RecoveryStabilityScore                          *float64                     `json:"recovery_stability_score,omitempty"`
	FastFailurePenaltyPercent                       *float64                     `json:"fast_failure_penalty_percent,omitempty"`
	FastFailureSeconds                              *float64                     `json:"fast_failure_seconds,omitempty"`
	FastFailureSameChannelRetryCount                *int                         `json:"fast_failure_same_channel_retry_count,omitempty"`
	FastFailureRetryDelayMs                         *int                         `json:"fast_failure_same_channel_retry_delay_ms,omitempty"`
	SlowFailureSeconds                              *float64                     `json:"slow_failure_seconds,omitempty"`
	BurstFailureWindowMinutes                       *int                         `json:"burst_failure_window_minutes,omitempty"`
	BurstFailureWindowRequests                      *int                         `json:"burst_failure_window_requests,omitempty"`
	BurstFailureThresholdPercent                    *float64                     `json:"burst_failure_threshold_percent,omitempty"`
	BurstFailureWindowSeconds                       *int                         `json:"burst_failure_window_seconds,omitempty"`
	ConsecutiveFailureThreshold                     *int                         `json:"consecutive_failure_threshold,omitempty"`
	BurstFailureThreshold                           *int                         `json:"burst_failure_threshold,omitempty"`
	RecoverySuccessThreshold                        *int                         `json:"recovery_success_threshold,omitempty"`
	JitterEnabled                                   *bool                        `json:"jitter_enabled,omitempty"`
	JitterTolerancePercent                          *float64                     `json:"jitter_tolerance_percent,omitempty"`
	JitterSlowThresholdSeconds                      *float64                     `json:"jitter_slow_threshold_seconds,omitempty"`
	CooldownMinutes                                 *int                         `json:"cooldown_minutes,omitempty"`
	SampleMode                                      *string                      `json:"sample_mode,omitempty"`
	SamplingOrder                                   *string                      `json:"sampling_order,omitempty"`
	ExplorationTrafficPercent                       *float64                     `json:"exploration_traffic_percent,omitempty"`
	ExplorationMaxPromptTokens                      *int                         `json:"exploration_max_prompt_tokens,omitempty"`
	StabilityReleaseMaxPromptTokens                 *int                         `json:"stability_release_max_prompt_tokens,omitempty"`
	ProbeIntervalMinutes                            *int                         `json:"probe_interval_minutes,omitempty"`
	DegradedProbeEnabled                            *bool                        `json:"degraded_probe_enabled,omitempty"`
	AdaptiveSamplingEnabled                         *bool                        `json:"adaptive_sampling_enabled,omitempty"`
	AdaptiveSamplingBasePercent                     *float64                     `json:"adaptive_sampling_base_percent,omitempty"`
	AdaptiveSamplingMaxPercent                      *float64                     `json:"adaptive_sampling_max_percent,omitempty"`
	AdaptiveSamplingErrorWarningPercent             *float64                     `json:"adaptive_sampling_error_warning_percent,omitempty"`
	AdaptiveSamplingErrorCriticalPercent            *float64                     `json:"adaptive_sampling_error_critical_percent,omitempty"`
	AdaptiveSamplingFirstTokenWarningSeconds        *float64                     `json:"adaptive_sampling_first_token_warning_seconds,omitempty"`
	AdaptiveSamplingFirstTokenCriticalSeconds       *float64                     `json:"adaptive_sampling_first_token_critical_seconds,omitempty"`
	AdaptiveSamplingWindowMinutes                   *int                         `json:"adaptive_sampling_window_minutes,omitempty"`
	AdaptiveSamplingWindowRequests                  *int                         `json:"adaptive_sampling_window_requests,omitempty"`
	AdaptiveSamplingWindowSeconds                   *int                         `json:"adaptive_sampling_window_seconds,omitempty"`
	AdaptiveSamplingFirstTokenWarningRequestPercent *float64                     `json:"adaptive_sampling_first_token_warning_request_percent,omitempty"`
	AdaptiveSamplingRecoverRequestPercent           *float64                     `json:"adaptive_sampling_recover_request_percent,omitempty"`
	AdaptiveSamplingSwitchConfirmRequestPercent     *float64                     `json:"adaptive_sampling_switch_confirm_request_percent,omitempty"`
	AdaptiveSamplingMinComparableChannels           *int                         `json:"adaptive_sampling_min_comparable_channels,omitempty"`
	legacyBurstFailureWindow                        bool
	legacyBurstFailureThreshold                     bool
	legacyAdaptiveSamplingWindow                    bool
}

func (policy *channelSmartScheduleGroupPolicy) UnmarshalJSON(data []byte) error {
	type policyAlias channelSmartScheduleGroupPolicy
	var fields map[string]any
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	for field := range fields {
		switch field {
		case "priority_sampling_enabled",
			"priority_sampling_interval_minutes",
			"priority_sampling_base_percent",
			"priority_sampling_decay_percent",
			"priority_sampling_min_percent",
			"adaptive_sampling_enter_rounds",
			"adaptive_sampling_recover_rounds",
			"adaptive_sampling_switch_confirm_rounds",
			"adaptive_sampling_exploration_lease_minutes":
			return errors.New("分组调度策略包含已删除的轮转、轮次或探索租约字段")
		case "adaptive_sampling_enter_request_percent":
			return errors.New("分组调度策略包含已删除的自适应采样统一进入请求比例字段")
		}
	}
	var decoded policyAlias
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	decoded.legacyBurstFailureWindow = decoded.BurstFailureWindowMinutes == nil &&
		decoded.BurstFailureWindowSeconds != nil
	decoded.legacyBurstFailureThreshold = decoded.BurstFailureThresholdPercent == nil &&
		decoded.BurstFailureThreshold != nil
	decoded.legacyAdaptiveSamplingWindow = decoded.AdaptiveSamplingWindowMinutes == nil &&
		decoded.AdaptiveSamplingWindowSeconds != nil
	*policy = channelSmartScheduleGroupPolicy(decoded)
	return nil
}

// MarshalJSON keeps old persisted policies readable while making newly
// normalized policies use the minute/request based contract. Legacy aliases
// are deliberately omitted when a policy was configured with the new fields.
func (policy channelSmartScheduleGroupPolicy) MarshalJSON() ([]byte, error) {
	type policyAlias channelSmartScheduleGroupPolicy
	raw, err := common.Marshal(policyAlias(policy))
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := common.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	legacyWindow := policy.legacyBurstFailureWindow ||
		(policy.BurstFailureWindowMinutes == nil && policy.BurstFailureWindowSeconds != nil)
	legacyThreshold := policy.legacyBurstFailureThreshold ||
		(policy.BurstFailureThresholdPercent == nil && policy.BurstFailureThreshold != nil)
	legacyAdaptiveWindow := policy.legacyAdaptiveSamplingWindow ||
		(policy.AdaptiveSamplingWindowMinutes == nil && policy.AdaptiveSamplingWindowSeconds != nil)
	if legacyWindow {
		delete(fields, "burst_failure_window_minutes")
		delete(fields, "burst_failure_window_requests")
	} else {
		delete(fields, "burst_failure_window_seconds")
	}
	if legacyThreshold {
		delete(fields, "burst_failure_threshold_percent")
	} else {
		delete(fields, "burst_failure_threshold")
	}
	if legacyAdaptiveWindow {
		delete(fields, "adaptive_sampling_window_minutes")
		delete(fields, "adaptive_sampling_window_requests")
	} else {
		delete(fields, "adaptive_sampling_window_seconds")
	}
	return common.Marshal(fields)
}

type smartScheduleGroupPolicies []channelSmartScheduleGroupPolicy

func (policies smartScheduleGroupPolicies) maxStabilityWindowMinutes() int {
	maximum := 0
	for _, policy := range policies {
		if policy.StabilityWindowMinutes != nil && *policy.StabilityWindowMinutes > maximum {
			maximum = *policy.StabilityWindowMinutes
		}
	}
	return maximum
}

func channelSmartScheduleMinutesFromSeconds(seconds int) int {
	if seconds <= 0 {
		return 1
	}
	maxInt := int(^uint(0) >> 1)
	if seconds > maxInt-59 {
		return maxInt / 60
	}
	return max(1, (seconds+59)/60)
}

type channelSmartSchedulePolicy struct {
	Strategy                                        string
	StabilityEnabled                                bool
	StabilityWindowMinutes                          int
	Scoring                                         channelSmartScheduleScoring
	ApplyMode                                       string
	Models                                          []string
	MinSamples                                      int
	RecoveryStabilityScore                          float64
	FastFailurePenaltyPercent                       float64
	FastFailureSeconds                              float64
	FastFailureSameChannelRetryCount                int
	FastFailureRetryDelayMs                         int
	SlowFailureSeconds                              float64
	BurstFailureWindowMinutes                       int
	BurstFailureWindowRequests                      int
	BurstFailureThresholdPercent                    float64
	BurstFailureWindowSeconds                       int
	ConsecutiveFailureThreshold                     int
	BurstFailureThreshold                           int
	BurstFailureThresholdLegacy                     int
	RecoverySuccessThreshold                        int
	JitterEnabled                                   bool
	JitterTolerancePercent                          float64
	JitterSlowThresholdSeconds                      float64
	CooldownMinutes                                 int
	SampleMode                                      string
	SamplingOrder                                   string
	ExplorationTrafficPercent                       float64
	ExplorationMaxPromptTokens                      int
	StabilityReleaseMaxPromptTokens                 int
	ProbeIntervalMinutes                            int
	DegradedProbeEnabled                            bool
	AdaptiveSamplingEnabled                         bool
	AdaptiveSamplingBasePercent                     float64
	AdaptiveSamplingMaxPercent                      float64
	AdaptiveSamplingErrorWarningPercent             float64
	AdaptiveSamplingErrorCriticalPercent            float64
	AdaptiveSamplingFirstTokenWarningSeconds        float64
	AdaptiveSamplingFirstTokenCriticalSeconds       float64
	AdaptiveSamplingWindowMinutes                   int
	AdaptiveSamplingWindowRequests                  int
	AdaptiveSamplingWindowSeconds                   int
	AdaptiveSamplingFirstTokenWarningRequestPercent float64
	AdaptiveSamplingRecoverRequestPercent           float64
	AdaptiveSamplingSwitchConfirmRequestPercent     float64
	AdaptiveSamplingMinComparableChannels           int
}

func parseChannelSmartScheduleGroupPolicies(raw string) []channelSmartScheduleGroupPolicy {
	policies, _ := parseChannelSmartScheduleGroupPoliciesWithError(raw)
	return policies
}

func parseChannelSmartScheduleGroupPoliciesWithError(raw string) ([]channelSmartScheduleGroupPolicy, error) {
	if strings.TrimSpace(raw) == "" {
		return []channelSmartScheduleGroupPolicy{}, nil
	}
	var policies []channelSmartScheduleGroupPolicy
	if err := common.UnmarshalJsonStr(raw, &policies); err != nil {
		return []channelSmartScheduleGroupPolicy{}, fmt.Errorf("分组调度策略 JSON 无效: %w", err)
	}
	normalized, err := normalizeChannelSmartScheduleGroupPolicies(policies)
	if err != nil {
		return []channelSmartScheduleGroupPolicy{}, err
	}
	return normalized, nil
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
		policy.legacyBurstFailureWindow = policy.BurstFailureWindowMinutes == nil &&
			policy.BurstFailureWindowSeconds != nil
		policy.legacyBurstFailureThreshold = policy.BurstFailureThresholdPercent == nil &&
			policy.BurstFailureThreshold != nil
		policy.legacyAdaptiveSamplingWindow = policy.AdaptiveSamplingWindowMinutes == nil &&
			policy.AdaptiveSamplingWindowSeconds != nil
		if policy.BurstFailureWindowMinutes == nil {
			value := defaultChannelMonitorSmartScheduleBurstFailureWindowMinutes
			if policy.BurstFailureWindowSeconds != nil {
				value = channelSmartScheduleMinutesFromSeconds(*policy.BurstFailureWindowSeconds)
			}
			policy.BurstFailureWindowMinutes = &value
		}
		if policy.BurstFailureWindowRequests == nil {
			value := defaultChannelMonitorSmartScheduleBurstFailureWindowRequests
			policy.BurstFailureWindowRequests = &value
		}
		if policy.BurstFailureThresholdPercent == nil {
			value := defaultChannelMonitorSmartScheduleBurstFailureThresholdPercent
			policy.BurstFailureThresholdPercent = &value
		}
		if policy.BurstFailureWindowSeconds == nil {
			value := defaultChannelMonitorSmartScheduleBurstFailureWindowSeconds
			policy.BurstFailureWindowSeconds = &value
		}
		if policy.FastFailureSameChannelRetryCount == nil {
			value := defaultChannelMonitorSmartScheduleFastFailureSameChannelRetryCount
			policy.FastFailureSameChannelRetryCount = &value
		}
		if policy.FastFailureRetryDelayMs == nil {
			value := defaultChannelMonitorSmartScheduleFastRetryDelayMs
			policy.FastFailureRetryDelayMs = &value
		}
		if policy.ConsecutiveFailureThreshold == nil {
			value := defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold
			policy.ConsecutiveFailureThreshold = &value
		}
		if policy.BurstFailureThreshold == nil {
			value := defaultChannelMonitorSmartScheduleBurstFailureThreshold
			policy.BurstFailureThreshold = &value
		}
		if policy.RecoverySuccessThreshold == nil {
			value := defaultChannelMonitorSmartScheduleRecoverySuccessThreshold
			policy.RecoverySuccessThreshold = &value
		}
		if policy.ExplorationMaxPromptTokens == nil {
			value := model.DefaultChannelSmartScheduleExplorationMaxPromptTokens
			policy.ExplorationMaxPromptTokens = &value
		}
		if policy.StabilityReleaseMaxPromptTokens == nil {
			value := model.DefaultChannelSmartScheduleStabilityReleaseMaxPromptTokens
			policy.StabilityReleaseMaxPromptTokens = &value
		}
		if policy.DegradedProbeEnabled == nil {
			value := false
			policy.DegradedProbeEnabled = &value
		}
		if policy.AdaptiveSamplingWindowMinutes == nil {
			value := defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes
			if policy.AdaptiveSamplingWindowSeconds != nil {
				value = channelSmartScheduleMinutesFromSeconds(*policy.AdaptiveSamplingWindowSeconds)
			}
			policy.AdaptiveSamplingWindowMinutes = &value
		}
		if policy.AdaptiveSamplingWindowRequests == nil {
			value := defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowRequests
			policy.AdaptiveSamplingWindowRequests = &value
		}
		if policy.Strategy == nil || policy.StabilityEnabled == nil || policy.StabilityWindowMinutes == nil || policy.Scoring == nil ||
			policy.ApplyMode == nil || policy.Models == nil || policy.MinSamples == nil ||
			policy.RecoveryStabilityScore == nil ||
			policy.FastFailurePenaltyPercent == nil || policy.FastFailureSeconds == nil ||
			policy.SlowFailureSeconds == nil || policy.JitterEnabled == nil ||
			policy.JitterTolerancePercent == nil || policy.JitterSlowThresholdSeconds == nil ||
			policy.CooldownMinutes == nil ||
			policy.SampleMode == nil || policy.SamplingOrder == nil || policy.ExplorationTrafficPercent == nil ||
			policy.ExplorationMaxPromptTokens == nil || policy.StabilityReleaseMaxPromptTokens == nil ||
			policy.ProbeIntervalMinutes == nil || policy.DegradedProbeEnabled == nil ||
			policy.AdaptiveSamplingEnabled == nil || policy.AdaptiveSamplingBasePercent == nil ||
			policy.AdaptiveSamplingMaxPercent == nil ||
			policy.AdaptiveSamplingErrorWarningPercent == nil || policy.AdaptiveSamplingErrorCriticalPercent == nil ||
			policy.AdaptiveSamplingFirstTokenWarningSeconds == nil || policy.AdaptiveSamplingFirstTokenCriticalSeconds == nil ||
			policy.AdaptiveSamplingWindowMinutes == nil || policy.AdaptiveSamplingWindowRequests == nil ||
			policy.AdaptiveSamplingFirstTokenWarningRequestPercent == nil ||
			policy.AdaptiveSamplingRecoverRequestPercent == nil ||
			policy.AdaptiveSamplingSwitchConfirmRequestPercent == nil || policy.AdaptiveSamplingMinComparableChannels == nil {
			return nil, errors.New("分组调度策略必须完整配置调度方式、稳定性保护、稳定性评分窗口、评分、调整方式、参与模型、最少样本数、稳定性阈值、失败耗时、成功延迟抖动、探索请求上限、降级时长、统一样本补充和自适应采样")
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
		models, err := normalizeChannelMonitorSmartScheduleModels(*policy.Models, "参与模型")
		if err != nil {
			return nil, err
		}
		policy.Models = &models
		modelOrder, err := normalizeChannelMonitorSmartScheduleModels(policy.ModelOrder, "模型卡片顺序")
		if err != nil {
			return nil, err
		}
		policy.ModelOrder = modelOrder
		if *policy.MinSamples <= 0 || *policy.MinSamples > maxChannelMonitorSmartScheduleMinSamples {
			return nil, errors.New("分组调度最少样本数必须在 1 到 100000 之间")
		}
		if !isChannelMonitorSmartScheduleWindowSupported(*policy.StabilityWindowMinutes) {
			return nil, errors.New("分组调度稳定性评分窗口必须在 1 到 1440 分钟之间")
		}
		if math.IsNaN(*policy.RecoveryStabilityScore) || math.IsInf(*policy.RecoveryStabilityScore, 0) ||
			*policy.RecoveryStabilityScore < 0 ||
			*policy.RecoveryStabilityScore > maxChannelMonitorSmartScheduleSuccessRate {
			return nil, errors.New("分组调度恢复稳定性得分必须在 0% 到 100% 之间")
		}
		if math.IsNaN(*policy.FastFailurePenaltyPercent) || math.IsInf(*policy.FastFailurePenaltyPercent, 0) ||
			*policy.FastFailurePenaltyPercent < 0 || *policy.FastFailurePenaltyPercent > maxChannelMonitorSmartScheduleSuccessRate {
			return nil, errors.New("分组调度快速失败惩罚必须在 0% 到 100% 之间")
		}
		if math.IsNaN(*policy.FastFailureSeconds) || math.IsInf(*policy.FastFailureSeconds, 0) ||
			*policy.FastFailureSeconds <= 0 || *policy.FastFailureSeconds >= maxChannelMonitorSmartScheduleFailureSeconds {
			return nil, errors.New("分组调度快速失败界限必须大于 0 秒且小于 60 秒")
		}
		if *policy.FastFailureSameChannelRetryCount < 0 ||
			*policy.FastFailureSameChannelRetryCount > maxChannelMonitorSmartScheduleFastFailureSameChannelRetryCount {
			return nil, errors.New("分组调度快速失败同渠道重试次数必须在 0 到 10 次之间")
		}
		if *policy.FastFailureRetryDelayMs < 0 ||
			*policy.FastFailureRetryDelayMs > maxChannelMonitorSmartScheduleFastRetryDelayMs {
			return nil, errors.New("分组调度快速失败同渠道重试延迟必须在 0 到 60000 毫秒之间")
		}
		if math.IsNaN(*policy.SlowFailureSeconds) || math.IsInf(*policy.SlowFailureSeconds, 0) ||
			*policy.SlowFailureSeconds <= *policy.FastFailureSeconds ||
			*policy.SlowFailureSeconds > maxChannelMonitorSmartScheduleFailureSeconds {
			return nil, errors.New("分组调度慢失败界限必须大于快速失败界限且不超过 60 秒")
		}
		if *policy.BurstFailureWindowMinutes < 1 ||
			*policy.BurstFailureWindowMinutes > maxChannelMonitorSmartScheduleBurstFailureWindowMinutes {
			return nil, errors.New("分组调度保护失败窗口分钟数必须在 1 到 60 分钟之间")
		}
		if *policy.BurstFailureWindowRequests < 1 ||
			*policy.BurstFailureWindowRequests > maxChannelMonitorSmartScheduleBurstFailureWindowRequests {
			return nil, errors.New("分组调度保护失败窗口请求数必须在 1 到 1000 次之间")
		}
		if math.IsNaN(*policy.BurstFailureThresholdPercent) ||
			math.IsInf(*policy.BurstFailureThresholdPercent, 0) ||
			*policy.BurstFailureThresholdPercent <= 0 ||
			*policy.BurstFailureThresholdPercent > maxChannelMonitorSmartScheduleBurstFailureThresholdPercent {
			return nil, errors.New("分组调度窗口失败阈值必须大于 0% 且不超过 100%")
		}
		if policy.legacyBurstFailureWindow && (policy.BurstFailureWindowSeconds == nil ||
			*policy.BurstFailureWindowSeconds <= 0 ||
			*policy.BurstFailureWindowSeconds > maxChannelMonitorSmartScheduleBurstFailureWindowSeconds) {
			return nil, errors.New("分组调度保护失败窗口必须在 1 到 300 秒之间")
		}
		if *policy.ConsecutiveFailureThreshold <= 0 ||
			*policy.ConsecutiveFailureThreshold > maxChannelMonitorSmartScheduleRuntimeFailureThreshold {
			return nil, errors.New("分组调度连续失败阈值必须在 1 到 100 次之间")
		}
		if policy.legacyBurstFailureThreshold && (policy.BurstFailureThreshold == nil ||
			*policy.BurstFailureThreshold <= 0 ||
			*policy.BurstFailureThreshold > maxChannelMonitorSmartScheduleRuntimeFailureThreshold) {
			return nil, errors.New("分组调度窗口失败阈值必须在 1 到 100 次之间")
		}
		if *policy.RecoverySuccessThreshold <= 0 ||
			*policy.RecoverySuccessThreshold > maxChannelMonitorSmartScheduleRuntimeFailureThreshold {
			return nil, errors.New("分组调度恢复探测成功次数必须在 1 到 100 次之间")
		}
		if math.IsNaN(*policy.JitterTolerancePercent) || math.IsInf(*policy.JitterTolerancePercent, 0) ||
			*policy.JitterTolerancePercent < 0 ||
			*policy.JitterTolerancePercent > maxChannelMonitorSmartScheduleJitterTolerancePercent {
			return nil, errors.New("分组调度允许抖动比例必须在 0% 到 50% 之间")
		}
		if math.IsNaN(*policy.JitterSlowThresholdSeconds) ||
			math.IsInf(*policy.JitterSlowThresholdSeconds, 0) ||
			*policy.JitterSlowThresholdSeconds < 0 ||
			*policy.JitterSlowThresholdSeconds > maxChannelMonitorSmartScheduleJitterSlowThresholdSeconds {
			return nil, errors.New("分组调度慢成功阈值必须在 0 到 60 秒之间")
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
		if *policy.AdaptiveSamplingEnabled && applyMode != channelMonitorSmartScheduleApplyPriorityWeight {
			return nil, errors.New("自适应采样仅支持按优先级和权重调整")
		}
		policy.SampleMode = &sampleMode
		samplingOrder := strings.TrimSpace(*policy.SamplingOrder)
		if !isChannelMonitorSmartScheduleSamplingOrderSupported(samplingOrder) {
			return nil, errors.New("分组调度采样顺序无效")
		}
		policy.SamplingOrder = &samplingOrder
		if math.IsNaN(*policy.ExplorationTrafficPercent) || math.IsInf(*policy.ExplorationTrafficPercent, 0) ||
			*policy.ExplorationTrafficPercent <= 0 ||
			*policy.ExplorationTrafficPercent > maxChannelMonitorSmartScheduleExplorationPercent {
			return nil, errors.New("分组调度探索流量必须大于 0% 且不超过 20%")
		}
		if *policy.ExplorationMaxPromptTokens < 0 ||
			*policy.ExplorationMaxPromptTokens > model.MaxChannelSmartScheduleExplorationPromptTokens ||
			*policy.ExplorationMaxPromptTokens%model.ChannelSmartSchedulePromptTokensPerK != 0 {
			return nil, errors.New("分组调度探索请求上限必须为 0 或 1000 的整数倍，且不能超过 1000000 Token")
		}
		if *policy.StabilityReleaseMaxPromptTokens < 0 ||
			*policy.StabilityReleaseMaxPromptTokens > model.MaxChannelSmartScheduleExplorationPromptTokens ||
			*policy.StabilityReleaseMaxPromptTokens%model.ChannelSmartSchedulePromptTokensPerK != 0 {
			return nil, errors.New("分组调度稳定性释放请求上限必须为 0 或 1000 的整数倍，且不能超过 1000000 Token")
		}
		if *policy.ProbeIntervalMinutes <= 0 ||
			*policy.ProbeIntervalMinutes > maxChannelMonitorAutoUpdateIntervalMinutes {
			return nil, errors.New("分组调度探测间隔必须在 1 到 525600 分钟之间")
		}
		if math.IsNaN(*policy.AdaptiveSamplingBasePercent) || math.IsInf(*policy.AdaptiveSamplingBasePercent, 0) ||
			*policy.AdaptiveSamplingBasePercent < 0 || *policy.AdaptiveSamplingBasePercent > 10 {
			return nil, errors.New("自适应采样基础预算必须在 0% 到 10% 之间")
		}
		if math.IsNaN(*policy.AdaptiveSamplingMaxPercent) || math.IsInf(*policy.AdaptiveSamplingMaxPercent, 0) ||
			*policy.AdaptiveSamplingMaxPercent < 1 || *policy.AdaptiveSamplingMaxPercent > 49 {
			return nil, errors.New("自适应采样最大预算必须在 1% 到 49% 之间")
		}
		if *policy.AdaptiveSamplingBasePercent > *policy.AdaptiveSamplingMaxPercent {
			return nil, errors.New("自适应采样基础预算不能大于最大预算")
		}
		for _, threshold := range []*float64{
			policy.AdaptiveSamplingErrorWarningPercent,
			policy.AdaptiveSamplingErrorCriticalPercent,
		} {
			if math.IsNaN(*threshold) || math.IsInf(*threshold, 0) || *threshold < 0 || *threshold > 100 {
				return nil, errors.New("自适应采样错误阈值必须在 0% 到 100% 之间")
			}
		}
		if *policy.AdaptiveSamplingErrorCriticalPercent <= *policy.AdaptiveSamplingErrorWarningPercent {
			return nil, errors.New("自适应采样错误高风险阈值必须大于告警阈值")
		}
		for _, threshold := range []*float64{
			policy.AdaptiveSamplingFirstTokenWarningSeconds,
			policy.AdaptiveSamplingFirstTokenCriticalSeconds,
		} {
			if math.IsNaN(*threshold) || math.IsInf(*threshold, 0) || *threshold < 0 || *threshold > 60 {
				return nil, errors.New("自适应采样首字阈值必须在 0 到 60 秒之间")
			}
		}
		if *policy.AdaptiveSamplingFirstTokenCriticalSeconds <= *policy.AdaptiveSamplingFirstTokenWarningSeconds {
			return nil, errors.New("自适应采样首字高风险阈值必须大于告警阈值")
		}
		if policy.legacyAdaptiveSamplingWindow && (policy.AdaptiveSamplingWindowSeconds == nil ||
			*policy.AdaptiveSamplingWindowSeconds < minChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds ||
			*policy.AdaptiveSamplingWindowSeconds > maxChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds) {
			return nil, errors.New("自适应采样统计窗口必须在 60 到 3600 秒之间")
		}
		if *policy.AdaptiveSamplingWindowMinutes < minChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes ||
			*policy.AdaptiveSamplingWindowMinutes > maxChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes {
			return nil, errors.New("自适应采样统计窗口分钟数必须在 1 到 60 分钟之间")
		}
		if *policy.AdaptiveSamplingWindowRequests < 1 ||
			*policy.AdaptiveSamplingWindowRequests > maxChannelMonitorSmartScheduleAdaptiveSamplingWindowRequests {
			return nil, errors.New("自适应采样统计窗口请求数必须在 1 到 1000 次之间")
		}
		for _, threshold := range []*float64{
			policy.AdaptiveSamplingFirstTokenWarningRequestPercent,
			policy.AdaptiveSamplingRecoverRequestPercent,
			policy.AdaptiveSamplingSwitchConfirmRequestPercent,
		} {
			if math.IsNaN(*threshold) || math.IsInf(*threshold, 0) || *threshold <= 0 || *threshold > 100 {
				return nil, errors.New("自适应采样请求占比阈值必须大于 0% 且不超过 100%")
			}
		}
		if *policy.AdaptiveSamplingFirstTokenWarningRequestPercent+
			*policy.AdaptiveSamplingRecoverRequestPercent <= 100 {
			return nil, errors.New("自适应采样首字告警和恢复健康请求占比必须保留滞回区间")
		}
		if *policy.AdaptiveSamplingSwitchConfirmRequestPercent < *policy.AdaptiveSamplingRecoverRequestPercent {
			return nil, errors.New("自适应采样切换确认请求占比不能低于恢复请求占比")
		}
		if *policy.AdaptiveSamplingMinComparableChannels < 2 ||
			*policy.AdaptiveSamplingMinComparableChannels > maxChannelMonitorSmartScheduleAdaptiveSamplingMinComparable {
			return nil, errors.New("自适应采样最少可比渠道数必须在 2 到 10 之间")
		}
		normalized = append(normalized, policy)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Group < normalized[j].Group
	})
	return normalized, nil
}

func (configured channelSmartScheduleGroupPolicy) policy() channelSmartSchedulePolicy {
	fastFailureSameChannelRetryCount := defaultChannelMonitorSmartScheduleFastFailureSameChannelRetryCount
	if configured.FastFailureSameChannelRetryCount != nil {
		fastFailureSameChannelRetryCount = *configured.FastFailureSameChannelRetryCount
	}
	fastFailureRetryDelayMs := defaultChannelMonitorSmartScheduleFastRetryDelayMs
	if configured.FastFailureRetryDelayMs != nil {
		fastFailureRetryDelayMs = *configured.FastFailureRetryDelayMs
	}
	burstFailureWindowMinutes := defaultChannelMonitorSmartScheduleBurstFailureWindowMinutes
	if configured.BurstFailureWindowMinutes != nil {
		burstFailureWindowMinutes = *configured.BurstFailureWindowMinutes
	} else if configured.BurstFailureWindowSeconds != nil {
		burstFailureWindowMinutes = channelSmartScheduleMinutesFromSeconds(*configured.BurstFailureWindowSeconds)
	}
	burstFailureWindowRequests := defaultChannelMonitorSmartScheduleBurstFailureWindowRequests
	if configured.BurstFailureWindowRequests != nil {
		burstFailureWindowRequests = *configured.BurstFailureWindowRequests
	}
	burstFailureWindowSeconds := burstFailureWindowMinutes * 60
	if configured.legacyBurstFailureWindow ||
		(configured.BurstFailureWindowMinutes == nil && configured.BurstFailureWindowSeconds != nil) {
		burstFailureWindowSeconds = *configured.BurstFailureWindowSeconds
	}
	consecutiveFailureThreshold := defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold
	if configured.ConsecutiveFailureThreshold != nil {
		consecutiveFailureThreshold = *configured.ConsecutiveFailureThreshold
	}
	burstFailureThresholdPercent := defaultChannelMonitorSmartScheduleBurstFailureThresholdPercent
	if configured.BurstFailureThresholdPercent != nil {
		burstFailureThresholdPercent = *configured.BurstFailureThresholdPercent
	}
	burstFailureThreshold := int(math.Round(burstFailureThresholdPercent))
	if configured.BurstFailureThreshold != nil {
		burstFailureThreshold = *configured.BurstFailureThreshold
	}
	legacyBurstFailureThreshold := 0
	if configured.legacyBurstFailureThreshold ||
		(configured.BurstFailureThresholdPercent == nil && configured.BurstFailureThreshold != nil) {
		legacyBurstFailureThreshold = burstFailureThreshold
	}
	recoverySuccessThreshold := defaultChannelMonitorSmartScheduleRecoverySuccessThreshold
	if configured.RecoverySuccessThreshold != nil {
		recoverySuccessThreshold = *configured.RecoverySuccessThreshold
	}
	degradedProbeEnabled := false
	if configured.DegradedProbeEnabled != nil {
		degradedProbeEnabled = *configured.DegradedProbeEnabled
	}
	adaptiveSamplingWindowMinutes := defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes
	if configured.AdaptiveSamplingWindowMinutes != nil {
		adaptiveSamplingWindowMinutes = *configured.AdaptiveSamplingWindowMinutes
	} else if configured.AdaptiveSamplingWindowSeconds != nil {
		adaptiveSamplingWindowMinutes = channelSmartScheduleMinutesFromSeconds(*configured.AdaptiveSamplingWindowSeconds)
	}
	adaptiveSamplingWindowRequests := defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowRequests
	if configured.AdaptiveSamplingWindowRequests != nil {
		adaptiveSamplingWindowRequests = *configured.AdaptiveSamplingWindowRequests
	}
	adaptiveSamplingWindowSeconds := adaptiveSamplingWindowMinutes * 60
	if configured.legacyAdaptiveSamplingWindow ||
		(configured.AdaptiveSamplingWindowMinutes == nil && configured.AdaptiveSamplingWindowSeconds != nil) {
		adaptiveSamplingWindowSeconds = *configured.AdaptiveSamplingWindowSeconds
	}
	return channelSmartSchedulePolicy{
		Strategy:                                        *configured.Strategy,
		StabilityEnabled:                                *configured.StabilityEnabled,
		StabilityWindowMinutes:                          *configured.StabilityWindowMinutes,
		Scoring:                                         *configured.Scoring,
		ApplyMode:                                       *configured.ApplyMode,
		Models:                                          *configured.Models,
		MinSamples:                                      *configured.MinSamples,
		RecoveryStabilityScore:                          *configured.RecoveryStabilityScore,
		FastFailurePenaltyPercent:                       *configured.FastFailurePenaltyPercent,
		FastFailureSeconds:                              *configured.FastFailureSeconds,
		FastFailureSameChannelRetryCount:                fastFailureSameChannelRetryCount,
		FastFailureRetryDelayMs:                         fastFailureRetryDelayMs,
		SlowFailureSeconds:                              *configured.SlowFailureSeconds,
		BurstFailureWindowMinutes:                       burstFailureWindowMinutes,
		BurstFailureWindowRequests:                      burstFailureWindowRequests,
		BurstFailureThresholdPercent:                    burstFailureThresholdPercent,
		BurstFailureWindowSeconds:                       burstFailureWindowSeconds,
		ConsecutiveFailureThreshold:                     consecutiveFailureThreshold,
		BurstFailureThreshold:                           burstFailureThreshold,
		BurstFailureThresholdLegacy:                     legacyBurstFailureThreshold,
		RecoverySuccessThreshold:                        recoverySuccessThreshold,
		JitterEnabled:                                   *configured.JitterEnabled,
		JitterTolerancePercent:                          *configured.JitterTolerancePercent,
		JitterSlowThresholdSeconds:                      *configured.JitterSlowThresholdSeconds,
		CooldownMinutes:                                 *configured.CooldownMinutes,
		SampleMode:                                      *configured.SampleMode,
		SamplingOrder:                                   *configured.SamplingOrder,
		ExplorationTrafficPercent:                       *configured.ExplorationTrafficPercent,
		ExplorationMaxPromptTokens:                      *configured.ExplorationMaxPromptTokens,
		StabilityReleaseMaxPromptTokens:                 *configured.StabilityReleaseMaxPromptTokens,
		ProbeIntervalMinutes:                            *configured.ProbeIntervalMinutes,
		DegradedProbeEnabled:                            degradedProbeEnabled,
		AdaptiveSamplingEnabled:                         *configured.AdaptiveSamplingEnabled,
		AdaptiveSamplingBasePercent:                     *configured.AdaptiveSamplingBasePercent,
		AdaptiveSamplingMaxPercent:                      *configured.AdaptiveSamplingMaxPercent,
		AdaptiveSamplingErrorWarningPercent:             *configured.AdaptiveSamplingErrorWarningPercent,
		AdaptiveSamplingErrorCriticalPercent:            *configured.AdaptiveSamplingErrorCriticalPercent,
		AdaptiveSamplingFirstTokenWarningSeconds:        *configured.AdaptiveSamplingFirstTokenWarningSeconds,
		AdaptiveSamplingFirstTokenCriticalSeconds:       *configured.AdaptiveSamplingFirstTokenCriticalSeconds,
		AdaptiveSamplingWindowMinutes:                   adaptiveSamplingWindowMinutes,
		AdaptiveSamplingWindowRequests:                  adaptiveSamplingWindowRequests,
		AdaptiveSamplingWindowSeconds:                   adaptiveSamplingWindowSeconds,
		AdaptiveSamplingFirstTokenWarningRequestPercent: *configured.AdaptiveSamplingFirstTokenWarningRequestPercent,
		AdaptiveSamplingRecoverRequestPercent:           *configured.AdaptiveSamplingRecoverRequestPercent,
		AdaptiveSamplingSwitchConfirmRequestPercent:     *configured.AdaptiveSamplingSwitchConfirmRequestPercent,
		AdaptiveSamplingMinComparableChannels:           *configured.AdaptiveSamplingMinComparableChannels,
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

func isChannelMonitorSmartScheduleSamplingOrderSupported(order string) bool {
	switch order {
	case channelMonitorSmartScheduleSamplingOrderPriorityWeight,
		channelMonitorSmartScheduleSamplingOrderRatio:
		return true
	default:
		return false
	}
}

func (policy channelSmartSchedulePolicy) needsPerformance() bool {
	if policy.AdaptiveSamplingEnabled {
		return true
	}
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
