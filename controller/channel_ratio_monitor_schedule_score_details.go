package controller

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/model"
)

type channelSmartScheduleNormalization struct {
	ratioMin        float64
	ratioMax        float64
	ratioCount      int
	firstTokenMin   float64
	firstTokenMax   float64
	firstTokenCount int
	tpsMin          float64
	tpsMax          float64
	tpsCount        int
}

func channelSmartScheduleMinimumComparableChannels(candidates []channelSmartScheduleCandidate) int {
	minimum := 2
	for _, candidate := range candidates {
		if candidate.MinComparableChannels > minimum {
			minimum = candidate.MinComparableChannels
		}
	}
	return minimum
}

func channelSmartScheduleBuildNormalization(
	candidates []channelSmartScheduleCandidate,
	minSamples int,
) channelSmartScheduleNormalization {
	normalization := channelSmartScheduleNormalization{
		ratioMin:      math.Inf(1),
		ratioMax:      math.Inf(-1),
		firstTokenMin: math.Inf(1),
		firstTokenMax: math.Inf(-1),
		tpsMin:        math.Inf(1),
		tpsMax:        math.Inf(-1),
	}
	for _, candidate := range candidates {
		if candidate.Ratio != nil {
			normalization.ratioMin = math.Min(normalization.ratioMin, *candidate.Ratio)
			normalization.ratioMax = math.Max(normalization.ratioMax, *candidate.Ratio)
			normalization.ratioCount++
		}
		if candidate.FirstTokenMs != nil && candidate.FirstTokenSampleCount >= minSamples {
			normalization.firstTokenMin = math.Min(normalization.firstTokenMin, *candidate.FirstTokenMs)
			normalization.firstTokenMax = math.Max(normalization.firstTokenMax, *candidate.FirstTokenMs)
			normalization.firstTokenCount++
		}
		if candidate.TPS != nil && candidate.TPSSampleCount >= minSamples {
			normalization.tpsMin = math.Min(normalization.tpsMin, *candidate.TPS)
			normalization.tpsMax = math.Max(normalization.tpsMax, *candidate.TPS)
			normalization.tpsCount++
		}
	}
	return normalization
}

func channelSmartScheduleComparisonState(count, minimum int) string {
	if count <= 0 {
		return model.ChannelSmartScheduleComparisonNone
	}
	if count < max(minimum, 2) {
		return model.ChannelSmartScheduleComparisonInsufficient
	}
	return model.ChannelSmartScheduleComparisonComparable
}

func channelSmartScheduleNewScoreDetails(
	candidate channelSmartScheduleCandidate,
	strategy string,
	stabilityEnabled bool,
	applyMode string,
	minSamples int,
	forceReset bool,
	scoring channelSmartScheduleScoring,
) *model.ChannelSmartScheduleScoreDetails {
	if minSamples <= 0 {
		minSamples = channelMonitorSmartScheduleFallbackMinSamples
	}
	costRatioPercent, firstTokenPercent, tpsPercent := channelSmartScheduleConfiguredMetricWeights(strategy, scoring)
	economics := channelSmartScheduleCandidateEconomics(candidate)
	costRatio := candidate.Ratio
	if costRatio == nil {
		costRatio = economics.CostRatio
	}
	costSamples := int64(0)
	if costRatio != nil {
		// Cost ratio is a current configuration snapshot rather than an
		// aggregated latency sample, so presence is represented as one input.
		costSamples = 1
	}
	details := &model.ChannelSmartScheduleScoreDetails{
		Version:               model.ChannelSmartScheduleScoreDetailsVersion,
		Strategy:              strategy,
		MinSamples:            minSamples,
		MinComparableChannels: max(candidate.MinComparableChannels, 2),
		ComparisonState:       model.ChannelSmartScheduleComparisonNone,
		SampleScope:           model.ChannelSmartScheduleSampleScopeChannelModel,
		SampleGroupCount:      candidate.SampleGroupCount,
		Economics: &model.ChannelSmartScheduleEconomicsDetails{
			CostRatio:    economics.CostRatio,
			GroupRatio:   economics.GroupRatio,
			GrossMargin:  economics.GrossMargin,
			EconomicRole: economics.EconomicRole,
		},
		Inputs: model.ChannelSmartScheduleScoreInputs{
			CostRatio: model.ChannelSmartScheduleScoreInput{
				Value: channelSmartScheduleCopyFloat(costRatio), SampleCount: costSamples,
			},
			FirstTokenMs: model.ChannelSmartScheduleScoreInput{
				Value:       channelSmartScheduleCopyFloat(candidate.FirstTokenMs),
				SampleCount: int64(candidate.FirstTokenSampleCount),
			},
			TPS: model.ChannelSmartScheduleScoreInput{
				Value: channelSmartScheduleCopyFloat(candidate.TPS), SampleCount: int64(candidate.TPSSampleCount),
			},
			Stability: model.ChannelSmartScheduleScoreInput{
				Value: channelSmartScheduleCopyFloat(candidate.Stability), SampleCount: candidate.StabilitySampleCount,
			},
		},
		Components: model.ChannelSmartScheduleScoreComponents{
			CostRatio: model.ChannelSmartScheduleScoreComponent{
				RawValue:                channelSmartScheduleCopyFloat(costRatio),
				ComparisonState:         model.ChannelSmartScheduleComparisonNone,
				ConfiguredWeightPercent: costRatioPercent,
			},
			FirstTokenMs: model.ChannelSmartScheduleScoreComponent{
				RawValue:                channelSmartScheduleCopyFloat(candidate.FirstTokenMs),
				ComparisonState:         model.ChannelSmartScheduleComparisonNone,
				ConfiguredWeightPercent: firstTokenPercent,
			},
			TPS: model.ChannelSmartScheduleScoreComponent{
				RawValue:                channelSmartScheduleCopyFloat(candidate.TPS),
				ComparisonState:         model.ChannelSmartScheduleComparisonNone,
				ConfiguredWeightPercent: tpsPercent,
			},
		},
		Stability: model.ChannelSmartScheduleStabilityScoreDetails{
			Enabled:                 stabilityEnabled,
			Available:               candidate.Stability != nil && candidate.StabilitySampleCount >= int64(minSamples),
			RawScore:                channelSmartScheduleCopyFloat(candidate.Stability),
			ConfiguredWeightPercent: scoring.StabilityPercent,
		},
		Health: model.ChannelSmartScheduleHealthDetails{
			State:                           candidate.HealthState,
			Evidence:                        candidate.HealthEvidence,
			Pressure:                        candidate.HealthPressure,
			ErrorPressure:                   candidate.HealthErrorPressure,
			LatencyPressure:                 candidate.HealthLatencyPressure,
			SampleCount:                     candidate.HealthSampleCount,
			WindowSeconds:                   candidate.HealthWindowSeconds,
			ErrorRequestPercent:             candidate.HealthErrorRequestPercent,
			RiskRequestPercent:              candidate.HealthRiskRequestPercent,
			FirstTokenWarningRequestPercent: candidate.HealthFirstTokenWarningRequestPercent,
			HealthyRequestPercent:           candidate.HealthHealthyRequestPercent,
		},
		Decision: model.ChannelSmartScheduleScoreDecision{
			ApplyMode:                     applyMode,
			PrimarySwitchThresholdPercent: scoring.PrimarySwitchThresholdPercent,
			PrimaryTrafficPercent:         scoring.PrimaryTrafficPercent,
			ForceReset:                    forceReset,
			ManualPrimary:                 candidate.ManualPrimary,
		},
	}
	if candidate.ManualPrimary {
		details.Decision.ManualPrimaryChannelId = candidate.ChannelId
	}
	return details
}

func channelSmartScheduleScoreCandidate(
	candidate channelSmartScheduleCandidate,
	strategy string,
	stabilityEnabled bool,
	applyMode string,
	minSamples int,
	forceReset bool,
	scoring channelSmartScheduleScoring,
	normalization channelSmartScheduleNormalization,
) (float64, *model.ChannelSmartScheduleScoreDetails, bool) {
	details := channelSmartScheduleNewScoreDetails(
		candidate, strategy, stabilityEnabled, applyMode, minSamples, forceReset, scoring,
	)
	details.Cohort.CostRatio = channelSmartScheduleScoreRange(
		normalization.ratioMin, normalization.ratioMax, normalization.ratioCount,
	)
	details.Cohort.FirstTokenMs = channelSmartScheduleScoreRange(
		normalization.firstTokenMin, normalization.firstTokenMax, normalization.firstTokenCount,
	)
	details.Cohort.TPS = channelSmartScheduleScoreRange(
		normalization.tpsMin, normalization.tpsMax, normalization.tpsCount,
	)

	minimumComparable := max(candidate.MinComparableChannels, 2)
	costAvailable := candidate.Ratio != nil && normalization.ratioCount >= minimumComparable
	firstTokenAvailable := candidate.FirstTokenMs != nil &&
		candidate.FirstTokenSampleCount >= minSamples && normalization.firstTokenCount >= minimumComparable
	tpsAvailable := candidate.TPS != nil &&
		candidate.TPSSampleCount >= minSamples && normalization.tpsCount >= minimumComparable

	details.Components.CostRatio.Available = costAvailable
	details.Components.CostRatio.ComparisonState = channelSmartScheduleComparisonState(
		normalization.ratioCount, minimumComparable,
	)
	if costAvailable {
		score := channelSmartScheduleLowerIsBetterScore(
			*candidate.Ratio, normalization.ratioMin, normalization.ratioMax,
		)
		details.Components.CostRatio.NormalizedScore = &score
	}
	details.Components.FirstTokenMs.Available = firstTokenAvailable
	details.Components.FirstTokenMs.ComparisonState = channelSmartScheduleComparisonState(
		normalization.firstTokenCount, minimumComparable,
	)
	if firstTokenAvailable {
		score := channelSmartScheduleLowerIsBetterScore(
			*candidate.FirstTokenMs, normalization.firstTokenMin, normalization.firstTokenMax,
		)
		details.Components.FirstTokenMs.NormalizedScore = &score
	}
	details.Components.TPS.Available = tpsAvailable
	details.Components.TPS.ComparisonState = channelSmartScheduleComparisonState(
		normalization.tpsCount, minimumComparable,
	)
	if tpsAvailable {
		score := channelSmartScheduleHigherIsBetterScore(
			*candidate.TPS, normalization.tpsMin, normalization.tpsMax,
		)
		details.Components.TPS.NormalizedScore = &score
	}

	switch strategy {
	case channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleStrategyFirstToken,
		channelMonitorSmartScheduleStrategyTPS,
		channelMonitorSmartScheduleStrategySmart:
	default:
		return 0, details, false
	}
	channelSmartScheduleSetEffectiveWeights(&details.Components)
	usesBusinessScore := channelSmartScheduleUsesBusinessScore(stabilityEnabled, scoring)
	hasConfiguredBusinessMetric := details.Components.CostRatio.Available &&
		details.Components.CostRatio.ConfiguredWeightPercent > channelMonitorRatioEpsilon
	hasConfiguredBusinessMetric = hasConfiguredBusinessMetric ||
		(details.Components.FirstTokenMs.Available &&
			details.Components.FirstTokenMs.ConfiguredWeightPercent > channelMonitorRatioEpsilon)
	hasConfiguredBusinessMetric = hasConfiguredBusinessMetric ||
		(details.Components.TPS.Available &&
			details.Components.TPS.ConfiguredWeightPercent > channelMonitorRatioEpsilon)
	// A manually fixed primary still needs a score record for the execution
	// detail and traffic allocation path. It is an explicit administrator
	// override, so the zero business contribution is not used to compare it
	// with other channels. Ordinary candidates remain unscored until at least
	// one configured business metric is available.
	if usesBusinessScore && !hasConfiguredBusinessMetric && !candidate.ManualPrimary {
		for _, component := range []model.ChannelSmartScheduleScoreComponent{
			details.Components.CostRatio,
			details.Components.FirstTokenMs,
			details.Components.TPS,
		} {
			if component.ComparisonState == model.ChannelSmartScheduleComparisonInsufficient {
				details.ComparisonState = model.ChannelSmartScheduleComparisonInsufficient
				break
			}
		}
		return 0, details, false
	}
	businessScore := channelSmartScheduleWeightedScore(
		channelSmartScheduleScorePart{
			Score:     channelSmartScheduleScoreValue(details.Components.CostRatio.NormalizedScore),
			Percent:   details.Components.CostRatio.ConfiguredWeightPercent,
			Available: details.Components.CostRatio.Available,
		},
		channelSmartScheduleScorePart{
			Score:     channelSmartScheduleScoreValue(details.Components.FirstTokenMs.NormalizedScore),
			Percent:   details.Components.FirstTokenMs.ConfiguredWeightPercent,
			Available: details.Components.FirstTokenMs.Available,
		},
		channelSmartScheduleScorePart{
			Score:     channelSmartScheduleScoreValue(details.Components.TPS.NormalizedScore),
			Percent:   details.Components.TPS.ConfiguredWeightPercent,
			Available: details.Components.TPS.Available,
		},
	)
	details.BusinessScore = channelSmartScheduleCopyFloat(&businessScore)
	score := businessScore
	details.Stability.BusinessContribution = businessScore
	if stabilityEnabled && candidate.Stability != nil && candidate.StabilitySampleCount >= int64(minSamples) {
		stabilityScore := min(max(*candidate.Stability, 0), 1)
		stabilityWeight := scoring.StabilityPercent / channelMonitorScorePercentageTotal
		details.Stability.Applied = true
		details.Stability.EffectiveWeightPercent = scoring.StabilityPercent
		details.Stability.BusinessContribution = (1 - stabilityWeight) * businessScore
		details.Stability.Contribution = stabilityWeight * stabilityScore
		score = details.Stability.BusinessContribution + details.Stability.Contribution
	}
	score = min(max(score, 0), 1)
	details.FinalScore = channelSmartScheduleCopyFloat(&score)
	if hasConfiguredBusinessMetric || details.Stability.Applied {
		details.ComparisonState = model.ChannelSmartScheduleComparisonComparable
	}
	return score, details, true
}

func channelSmartScheduleSetAdjustmentReason(
	details *model.ChannelSmartScheduleScoreDetails,
	reason string,
) {
	if details == nil {
		return
	}
	details.Decision.AdjustmentReason = reason
	switch {
	case details.Decision.SelectionReason != "" && reason != "":
		details.Decision.Reason = details.Decision.SelectionReason + "；" + reason
	case details.Decision.SelectionReason != "":
		details.Decision.Reason = details.Decision.SelectionReason
	default:
		details.Decision.Reason = reason
	}
}

func channelSmartScheduleConfiguredMetricWeights(
	strategy string,
	scoring channelSmartScheduleScoring,
) (costRatioPercent float64, firstTokenPercent float64, tpsPercent float64) {
	switch strategy {
	case channelMonitorSmartScheduleStrategyRatio:
		return scoring.Ratio.CostRatioPercent, scoring.Ratio.FirstTokenPercent, scoring.Ratio.TPSPercent
	case channelMonitorSmartScheduleStrategyFirstToken:
		return 0, channelMonitorScorePercentageTotal, 0
	case channelMonitorSmartScheduleStrategyTPS:
		return 0, 0, channelMonitorScorePercentageTotal
	case channelMonitorSmartScheduleStrategySmart:
		return scoring.Smart.CostRatioPercent, scoring.Smart.FirstTokenPercent, scoring.Smart.TPSPercent
	default:
		return 0, 0, 0
	}
}

func channelSmartScheduleSetEffectiveWeights(components *model.ChannelSmartScheduleScoreComponents) {
	total := 0.0
	for _, component := range []*model.ChannelSmartScheduleScoreComponent{
		&components.CostRatio, &components.FirstTokenMs, &components.TPS,
	} {
		if component.Available && component.ConfiguredWeightPercent > 0 {
			total += component.ConfiguredWeightPercent
		}
	}
	if total <= channelMonitorRatioEpsilon {
		return
	}
	for _, component := range []*model.ChannelSmartScheduleScoreComponent{
		&components.CostRatio, &components.FirstTokenMs, &components.TPS,
	} {
		if component.Available && component.ConfiguredWeightPercent > 0 {
			component.EffectiveWeightPercent = component.ConfiguredWeightPercent / total * channelMonitorScorePercentageTotal
		}
	}
}

func channelSmartScheduleScoreRange(minimum float64, maximum float64, count int) model.ChannelSmartScheduleScoreRange {
	result := model.ChannelSmartScheduleScoreRange{AvailableCount: count}
	if count <= 0 {
		return result
	}
	result.Minimum = channelSmartScheduleCopyFloat(&minimum)
	result.Maximum = channelSmartScheduleCopyFloat(&maximum)
	return result
}

func channelSmartScheduleCopyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func channelSmartScheduleScoreValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func channelSmartScheduleSelectionReason(
	items []channelSmartSchedulePlanItem,
	currentPrimaryId int,
	rawWinnerId int,
	selectedPrimaryId int,
	manualPrimaryId int,
	switchThresholdPercent float64,
	forceReset bool,
) string {
	if manualPrimaryId > 0 {
		return "管理员固定主渠道优先于本轮评分结果"
	}
	if rawWinnerId == 0 {
		return "本轮没有可选主渠道"
	}
	if forceReset {
		return "强制重算，选择本轮评分最高的渠道"
	}
	if currentPrimaryId == 0 {
		return "当前没有唯一主渠道，选择本轮评分最高的渠道"
	}
	if rawWinnerId == currentPrimaryId {
		return "当前主渠道仍是本轮评分最高的渠道"
	}
	currentScore, winnerScore := 0.0, 0.0
	for _, item := range items {
		if item.ChannelId == currentPrimaryId {
			currentScore = item.Score
		}
		if item.ChannelId == rawWinnerId {
			winnerScore = item.Score
		}
	}
	differencePercent := (winnerScore - currentScore) * channelMonitorScorePercentageTotal
	if selectedPrimaryId == currentPrimaryId {
		return fmt.Sprintf(
			"最高分渠道仅领先当前主渠道 %.2f%%，未达到 %.2f%% 的切换分差，保持当前主渠道",
			differencePercent, switchThresholdPercent,
		)
	}
	return fmt.Sprintf(
		"最高分渠道领先当前主渠道 %.2f%%，达到 %.2f%% 的切换分差，切换主渠道",
		differencePercent, switchThresholdPercent,
	)
}
