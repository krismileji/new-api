package controller

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	channelMonitorSmartScheduleGroupPoliciesOption                     = "ChannelMonitorSmartScheduleGroupPolicies"
	maxChannelMonitorSmartScheduleFailureSeconds                       = 60
	maxChannelMonitorSmartScheduleJitterTolerancePercent               = 50
	maxChannelMonitorSmartScheduleJitterSlowThresholdSeconds           = 60
	maxChannelMonitorSmartScheduleBurstFailureWindowSeconds            = 300
	maxChannelMonitorSmartScheduleRuntimeFailureThreshold              = 100
	maxChannelMonitorSmartScheduleFastFailureSameChannelRetryCount     = 10
	maxChannelMonitorSmartScheduleFastRetryDelayMs                     = 60_000
	defaultChannelMonitorSmartScheduleBurstFailureWindowSeconds        = 30
	defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold      = 2
	defaultChannelMonitorSmartScheduleBurstFailureThreshold            = 3
	defaultChannelMonitorSmartScheduleRecoverySuccessThreshold         = 2
	defaultChannelMonitorSmartScheduleFastFailureSameChannelRetryCount = 0
	defaultChannelMonitorSmartScheduleFastRetryDelayMs                 = 1_000
	maxChannelMonitorSmartScheduleSamplingInterval                     = 1440
	minChannelMonitorSmartScheduleSamplingBasePercent                  = 0.1
	maxChannelMonitorSmartScheduleSamplingBasePercent                  = 20.0
	minChannelMonitorSmartScheduleSamplingDecayPercent                 = 1.0
	maxChannelMonitorSmartScheduleSamplingDecayPercent                 = 100.0
	minChannelMonitorSmartScheduleSamplingFloorPercent                 = 0.01
	maxChannelMonitorSmartScheduleSamplingFloorPercent                 = 5.0
	minChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds        = 60
	maxChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds        = 3600
	maxChannelMonitorSmartScheduleAdaptiveSamplingMinComparable        = 10
)

type channelSmartScheduleGroupPolicy struct {
	Group                                       string                       `json:"group"`
	Strategy                                    *string                      `json:"strategy,omitempty"`
	StabilityEnabled                            *bool                        `json:"stability_enabled,omitempty"`
	Scoring                                     *channelSmartScheduleScoring `json:"scoring,omitempty"`
	ApplyMode                                   *string                      `json:"apply_mode,omitempty"`
	Models                                      *[]string                    `json:"models,omitempty"`
	ModelOrder                                  []string                     `json:"model_order,omitempty"`
	MinSamples                                  *int                         `json:"min_samples,omitempty"`
	RecoveryStabilityScore                      *float64                     `json:"recovery_stability_score,omitempty"`
	FastFailurePenaltyPercent                   *float64                     `json:"fast_failure_penalty_percent,omitempty"`
	FastFailureSeconds                          *float64                     `json:"fast_failure_seconds,omitempty"`
	FastFailureSameChannelRetryCount            *int                         `json:"fast_failure_same_channel_retry_count,omitempty"`
	FastFailureRetryDelayMs                     *int                         `json:"fast_failure_same_channel_retry_delay_ms,omitempty"`
	SlowFailureSeconds                          *float64                     `json:"slow_failure_seconds,omitempty"`
	BurstFailureWindowSeconds                   *int                         `json:"burst_failure_window_seconds,omitempty"`
	ConsecutiveFailureThreshold                 *int                         `json:"consecutive_failure_threshold,omitempty"`
	BurstFailureThreshold                       *int                         `json:"burst_failure_threshold,omitempty"`
	RecoverySuccessThreshold                    *int                         `json:"recovery_success_threshold,omitempty"`
	JitterEnabled                               *bool                        `json:"jitter_enabled,omitempty"`
	JitterTolerancePercent                      *float64                     `json:"jitter_tolerance_percent,omitempty"`
	JitterSlowThresholdSeconds                  *float64                     `json:"jitter_slow_threshold_seconds,omitempty"`
	CooldownMinutes                             *int                         `json:"cooldown_minutes,omitempty"`
	SampleMode                                  *string                      `json:"sample_mode,omitempty"`
	ExplorationTrafficPercent                   *float64                     `json:"exploration_traffic_percent,omitempty"`
	ExplorationMaxPromptTokens                  *int                         `json:"exploration_max_prompt_tokens,omitempty"`
	StabilityReleaseMaxPromptTokens             *int                         `json:"stability_release_max_prompt_tokens,omitempty"`
	ProbeIntervalMinutes                        *int                         `json:"probe_interval_minutes,omitempty"`
	DegradedProbeEnabled                        *bool                        `json:"degraded_probe_enabled,omitempty"`
	PrioritySamplingEnabled                     *bool                        `json:"priority_sampling_enabled,omitempty"`
	PrioritySamplingIntervalMinutes             *int                         `json:"priority_sampling_interval_minutes,omitempty"`
	PrioritySamplingBasePercent                 *float64                     `json:"priority_sampling_base_percent,omitempty"`
	PrioritySamplingDecayPercent                *float64                     `json:"priority_sampling_decay_percent,omitempty"`
	PrioritySamplingMinPercent                  *float64                     `json:"priority_sampling_min_percent,omitempty"`
	AdaptiveSamplingEnabled                     *bool                        `json:"adaptive_sampling_enabled,omitempty"`
	AdaptiveSamplingBasePercent                 *float64                     `json:"adaptive_sampling_base_percent,omitempty"`
	AdaptiveSamplingMaxPercent                  *float64                     `json:"adaptive_sampling_max_percent,omitempty"`
	AdaptiveSamplingPrimaryMinPercent           *float64                     `json:"adaptive_sampling_primary_min_percent,omitempty"`
	AdaptiveSamplingErrorWarningPercent         *float64                     `json:"adaptive_sampling_error_warning_percent,omitempty"`
	AdaptiveSamplingErrorCriticalPercent        *float64                     `json:"adaptive_sampling_error_critical_percent,omitempty"`
	AdaptiveSamplingFirstTokenWarningSeconds    *float64                     `json:"adaptive_sampling_first_token_warning_seconds,omitempty"`
	AdaptiveSamplingFirstTokenCriticalSeconds   *float64                     `json:"adaptive_sampling_first_token_critical_seconds,omitempty"`
	AdaptiveSamplingWindowSeconds               *int                         `json:"adaptive_sampling_window_seconds,omitempty"`
	AdaptiveSamplingEnterRequestPercent         *float64                     `json:"adaptive_sampling_enter_request_percent,omitempty"`
	AdaptiveSamplingRecoverRequestPercent       *float64                     `json:"adaptive_sampling_recover_request_percent,omitempty"`
	AdaptiveSamplingSwitchConfirmRequestPercent *float64                     `json:"adaptive_sampling_switch_confirm_request_percent,omitempty"`
	AdaptiveSamplingMinComparableChannels       *int                         `json:"adaptive_sampling_min_comparable_channels,omitempty"`
}

type smartScheduleGroupPolicies []channelSmartScheduleGroupPolicy

type channelSmartSchedulePolicy struct {
	Strategy                                    string
	StabilityEnabled                            bool
	Scoring                                     channelSmartScheduleScoring
	ApplyMode                                   string
	Models                                      []string
	MinSamples                                  int
	RecoveryStabilityScore                      float64
	FastFailurePenaltyPercent                   float64
	FastFailureSeconds                          float64
	FastFailureSameChannelRetryCount            int
	FastFailureRetryDelayMs                     int
	SlowFailureSeconds                          float64
	BurstFailureWindowSeconds                   int
	ConsecutiveFailureThreshold                 int
	BurstFailureThreshold                       int
	RecoverySuccessThreshold                    int
	JitterEnabled                               bool
	JitterTolerancePercent                      float64
	JitterSlowThresholdSeconds                  float64
	CooldownMinutes                             int
	SampleMode                                  string
	ExplorationTrafficPercent                   float64
	ExplorationMaxPromptTokens                  int
	StabilityReleaseMaxPromptTokens             int
	ProbeIntervalMinutes                        int
	DegradedProbeEnabled                        bool
	PrioritySamplingEnabled                     bool
	PrioritySamplingIntervalMinutes             int
	PrioritySamplingBasePercent                 float64
	PrioritySamplingDecayPercent                float64
	PrioritySamplingMinPercent                  float64
	AdaptiveSamplingEnabled                     bool
	AdaptiveSamplingBasePercent                 float64
	AdaptiveSamplingMaxPercent                  float64
	AdaptiveSamplingPrimaryMinPercent           float64
	AdaptiveSamplingErrorWarningPercent         float64
	AdaptiveSamplingErrorCriticalPercent        float64
	AdaptiveSamplingFirstTokenWarningSeconds    float64
	AdaptiveSamplingFirstTokenCriticalSeconds   float64
	AdaptiveSamplingWindowSeconds               int
	AdaptiveSamplingEnterRequestPercent         float64
	AdaptiveSamplingRecoverRequestPercent       float64
	AdaptiveSamplingSwitchConfirmRequestPercent float64
	AdaptiveSamplingMinComparableChannels       int
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
		if policy.Strategy == nil || policy.StabilityEnabled == nil || policy.Scoring == nil ||
			policy.ApplyMode == nil || policy.Models == nil || policy.MinSamples == nil ||
			policy.RecoveryStabilityScore == nil ||
			policy.FastFailurePenaltyPercent == nil || policy.FastFailureSeconds == nil ||
			policy.SlowFailureSeconds == nil || policy.JitterEnabled == nil ||
			policy.JitterTolerancePercent == nil || policy.JitterSlowThresholdSeconds == nil ||
			policy.CooldownMinutes == nil ||
			policy.SampleMode == nil || policy.ExplorationTrafficPercent == nil ||
			policy.ExplorationMaxPromptTokens == nil || policy.StabilityReleaseMaxPromptTokens == nil ||
			policy.ProbeIntervalMinutes == nil || policy.DegradedProbeEnabled == nil ||
			policy.PrioritySamplingEnabled == nil ||
			policy.PrioritySamplingIntervalMinutes == nil || policy.PrioritySamplingBasePercent == nil ||
			policy.PrioritySamplingDecayPercent == nil || policy.PrioritySamplingMinPercent == nil ||
			policy.AdaptiveSamplingEnabled == nil || policy.AdaptiveSamplingBasePercent == nil ||
			policy.AdaptiveSamplingMaxPercent == nil || policy.AdaptiveSamplingPrimaryMinPercent == nil ||
			policy.AdaptiveSamplingErrorWarningPercent == nil || policy.AdaptiveSamplingErrorCriticalPercent == nil ||
			policy.AdaptiveSamplingFirstTokenWarningSeconds == nil || policy.AdaptiveSamplingFirstTokenCriticalSeconds == nil ||
			policy.AdaptiveSamplingWindowSeconds == nil || policy.AdaptiveSamplingEnterRequestPercent == nil ||
			policy.AdaptiveSamplingRecoverRequestPercent == nil ||
			policy.AdaptiveSamplingSwitchConfirmRequestPercent == nil || policy.AdaptiveSamplingMinComparableChannels == nil {
			return nil, errors.New("分组调度策略必须完整配置调度方式、稳定性保护、评分、调整方式、参与模型、最少样本数、稳定性阈值、失败耗时、成功延迟抖动、探索请求上限、降级时长、样本补充、低优先级轮转和自适应采样")
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
		if *policy.BurstFailureWindowSeconds <= 0 ||
			*policy.BurstFailureWindowSeconds > maxChannelMonitorSmartScheduleBurstFailureWindowSeconds {
			return nil, errors.New("分组调度保护失败窗口必须在 1 到 300 秒之间")
		}
		if *policy.ConsecutiveFailureThreshold <= 0 ||
			*policy.ConsecutiveFailureThreshold > maxChannelMonitorSmartScheduleRuntimeFailureThreshold {
			return nil, errors.New("分组调度连续失败阈值必须在 1 到 100 次之间")
		}
		if *policy.BurstFailureThreshold <= 0 ||
			*policy.BurstFailureThreshold > maxChannelMonitorSmartScheduleRuntimeFailureThreshold {
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
		if math.IsNaN(*policy.ExplorationTrafficPercent) || math.IsInf(*policy.ExplorationTrafficPercent, 0) ||
			*policy.ExplorationTrafficPercent <= 0 ||
			*policy.ExplorationTrafficPercent > maxChannelMonitorSmartScheduleExplorationPercent {
			return nil, errors.New("分组调度探索流量必须大于 0% 且不超过 20%")
		}
		if *policy.ExplorationMaxPromptTokens < 0 ||
			*policy.ExplorationMaxPromptTokens > model.MaxChannelSmartScheduleExplorationPromptTokens {
			return nil, errors.New("分组调度探索请求上限必须在 0 到 1000000 Token 之间")
		}
		if *policy.StabilityReleaseMaxPromptTokens < 0 ||
			*policy.StabilityReleaseMaxPromptTokens > model.MaxChannelSmartScheduleExplorationPromptTokens {
			return nil, errors.New("分组调度稳定性释放请求上限必须在 0 到 1000000 Token 之间")
		}
		if *policy.ProbeIntervalMinutes <= 0 ||
			*policy.ProbeIntervalMinutes > maxChannelMonitorAutoUpdateIntervalMinutes {
			return nil, errors.New("分组调度探测间隔必须在 1 到 525600 分钟之间")
		}
		if *policy.PrioritySamplingIntervalMinutes <= 0 ||
			*policy.PrioritySamplingIntervalMinutes > maxChannelMonitorSmartScheduleSamplingInterval {
			return nil, errors.New("低优先级轮转间隔必须在 1 到 1440 分钟之间")
		}
		if math.IsNaN(*policy.PrioritySamplingBasePercent) || math.IsInf(*policy.PrioritySamplingBasePercent, 0) ||
			*policy.PrioritySamplingBasePercent < minChannelMonitorSmartScheduleSamplingBasePercent ||
			*policy.PrioritySamplingBasePercent > maxChannelMonitorSmartScheduleSamplingBasePercent {
			return nil, errors.New("低优先级轮转基础流量必须在 0.1% 到 20% 之间")
		}
		if math.IsNaN(*policy.PrioritySamplingDecayPercent) || math.IsInf(*policy.PrioritySamplingDecayPercent, 0) ||
			*policy.PrioritySamplingDecayPercent < minChannelMonitorSmartScheduleSamplingDecayPercent ||
			*policy.PrioritySamplingDecayPercent > maxChannelMonitorSmartScheduleSamplingDecayPercent {
			return nil, errors.New("低优先级轮转递减比例必须在 1% 到 100% 之间")
		}
		if math.IsNaN(*policy.PrioritySamplingMinPercent) || math.IsInf(*policy.PrioritySamplingMinPercent, 0) ||
			*policy.PrioritySamplingMinPercent < minChannelMonitorSmartScheduleSamplingFloorPercent ||
			*policy.PrioritySamplingMinPercent > maxChannelMonitorSmartScheduleSamplingFloorPercent {
			return nil, errors.New("低优先级轮转最小流量必须在 0.01% 到 5% 之间")
		}
		if *policy.PrioritySamplingMinPercent > *policy.PrioritySamplingBasePercent {
			return nil, errors.New("低优先级轮转最小流量不能大于基础流量")
		}
		if math.IsNaN(*policy.AdaptiveSamplingBasePercent) || math.IsInf(*policy.AdaptiveSamplingBasePercent, 0) ||
			*policy.AdaptiveSamplingBasePercent < 0 || *policy.AdaptiveSamplingBasePercent > 10 {
			return nil, errors.New("自适应采样基础预算必须在 0% 到 10% 之间")
		}
		if math.IsNaN(*policy.AdaptiveSamplingMaxPercent) || math.IsInf(*policy.AdaptiveSamplingMaxPercent, 0) ||
			*policy.AdaptiveSamplingMaxPercent < 1 || *policy.AdaptiveSamplingMaxPercent > 49 {
			return nil, errors.New("自适应采样最大预算必须在 1% 到 49% 之间")
		}
		if math.IsNaN(*policy.AdaptiveSamplingPrimaryMinPercent) || math.IsInf(*policy.AdaptiveSamplingPrimaryMinPercent, 0) ||
			*policy.AdaptiveSamplingPrimaryMinPercent < 51 || *policy.AdaptiveSamplingPrimaryMinPercent > 99 {
			return nil, errors.New("自适应采样主渠道最低流量必须在 51% 到 99% 之间")
		}
		if *policy.AdaptiveSamplingBasePercent > *policy.AdaptiveSamplingMaxPercent {
			return nil, errors.New("自适应采样基础预算不能大于最大预算")
		}
		if *policy.AdaptiveSamplingMaxPercent > 100-*policy.AdaptiveSamplingPrimaryMinPercent {
			return nil, errors.New("自适应采样最大预算与主渠道最低流量冲突")
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
		if *policy.AdaptiveSamplingWindowSeconds < minChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds ||
			*policy.AdaptiveSamplingWindowSeconds > maxChannelMonitorSmartScheduleAdaptiveSamplingWindowSeconds {
			return nil, errors.New("自适应采样统计窗口必须在 60 到 3600 秒之间")
		}
		for _, threshold := range []*float64{
			policy.AdaptiveSamplingEnterRequestPercent,
			policy.AdaptiveSamplingRecoverRequestPercent,
			policy.AdaptiveSamplingSwitchConfirmRequestPercent,
		} {
			if math.IsNaN(*threshold) || math.IsInf(*threshold, 0) || *threshold <= 0 || *threshold > 100 {
				return nil, errors.New("自适应采样请求占比阈值必须大于 0% 且不超过 100%")
			}
		}
		if *policy.AdaptiveSamplingEnterRequestPercent+*policy.AdaptiveSamplingRecoverRequestPercent <= 100 {
			return nil, errors.New("自适应采样进入和恢复请求占比必须保留滞回区间")
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
	burstFailureWindowSeconds := defaultChannelMonitorSmartScheduleBurstFailureWindowSeconds
	if configured.BurstFailureWindowSeconds != nil {
		burstFailureWindowSeconds = *configured.BurstFailureWindowSeconds
	}
	consecutiveFailureThreshold := defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold
	if configured.ConsecutiveFailureThreshold != nil {
		consecutiveFailureThreshold = *configured.ConsecutiveFailureThreshold
	}
	burstFailureThreshold := defaultChannelMonitorSmartScheduleBurstFailureThreshold
	if configured.BurstFailureThreshold != nil {
		burstFailureThreshold = *configured.BurstFailureThreshold
	}
	recoverySuccessThreshold := defaultChannelMonitorSmartScheduleRecoverySuccessThreshold
	if configured.RecoverySuccessThreshold != nil {
		recoverySuccessThreshold = *configured.RecoverySuccessThreshold
	}
	degradedProbeEnabled := false
	if configured.DegradedProbeEnabled != nil {
		degradedProbeEnabled = *configured.DegradedProbeEnabled
	}
	return channelSmartSchedulePolicy{
		Strategy:                                    *configured.Strategy,
		StabilityEnabled:                            *configured.StabilityEnabled,
		Scoring:                                     *configured.Scoring,
		ApplyMode:                                   *configured.ApplyMode,
		Models:                                      *configured.Models,
		MinSamples:                                  *configured.MinSamples,
		RecoveryStabilityScore:                      *configured.RecoveryStabilityScore,
		FastFailurePenaltyPercent:                   *configured.FastFailurePenaltyPercent,
		FastFailureSeconds:                          *configured.FastFailureSeconds,
		FastFailureSameChannelRetryCount:            fastFailureSameChannelRetryCount,
		FastFailureRetryDelayMs:                     fastFailureRetryDelayMs,
		SlowFailureSeconds:                          *configured.SlowFailureSeconds,
		BurstFailureWindowSeconds:                   burstFailureWindowSeconds,
		ConsecutiveFailureThreshold:                 consecutiveFailureThreshold,
		BurstFailureThreshold:                       burstFailureThreshold,
		RecoverySuccessThreshold:                    recoverySuccessThreshold,
		JitterEnabled:                               *configured.JitterEnabled,
		JitterTolerancePercent:                      *configured.JitterTolerancePercent,
		JitterSlowThresholdSeconds:                  *configured.JitterSlowThresholdSeconds,
		CooldownMinutes:                             *configured.CooldownMinutes,
		SampleMode:                                  *configured.SampleMode,
		ExplorationTrafficPercent:                   *configured.ExplorationTrafficPercent,
		ExplorationMaxPromptTokens:                  *configured.ExplorationMaxPromptTokens,
		StabilityReleaseMaxPromptTokens:             *configured.StabilityReleaseMaxPromptTokens,
		ProbeIntervalMinutes:                        *configured.ProbeIntervalMinutes,
		DegradedProbeEnabled:                        degradedProbeEnabled,
		PrioritySamplingEnabled:                     *configured.PrioritySamplingEnabled,
		PrioritySamplingIntervalMinutes:             *configured.PrioritySamplingIntervalMinutes,
		PrioritySamplingBasePercent:                 *configured.PrioritySamplingBasePercent,
		PrioritySamplingDecayPercent:                *configured.PrioritySamplingDecayPercent,
		PrioritySamplingMinPercent:                  *configured.PrioritySamplingMinPercent,
		AdaptiveSamplingEnabled:                     *configured.AdaptiveSamplingEnabled,
		AdaptiveSamplingBasePercent:                 *configured.AdaptiveSamplingBasePercent,
		AdaptiveSamplingMaxPercent:                  *configured.AdaptiveSamplingMaxPercent,
		AdaptiveSamplingPrimaryMinPercent:           *configured.AdaptiveSamplingPrimaryMinPercent,
		AdaptiveSamplingErrorWarningPercent:         *configured.AdaptiveSamplingErrorWarningPercent,
		AdaptiveSamplingErrorCriticalPercent:        *configured.AdaptiveSamplingErrorCriticalPercent,
		AdaptiveSamplingFirstTokenWarningSeconds:    *configured.AdaptiveSamplingFirstTokenWarningSeconds,
		AdaptiveSamplingFirstTokenCriticalSeconds:   *configured.AdaptiveSamplingFirstTokenCriticalSeconds,
		AdaptiveSamplingWindowSeconds:               *configured.AdaptiveSamplingWindowSeconds,
		AdaptiveSamplingEnterRequestPercent:         *configured.AdaptiveSamplingEnterRequestPercent,
		AdaptiveSamplingRecoverRequestPercent:       *configured.AdaptiveSamplingRecoverRequestPercent,
		AdaptiveSamplingSwitchConfirmRequestPercent: *configured.AdaptiveSamplingSwitchConfirmRequestPercent,
		AdaptiveSamplingMinComparableChannels:       *configured.AdaptiveSamplingMinComparableChannels,
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
