package controller

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	channelMonitorSmartScheduleTaskType                          = "channel_smart_schedule"
	channelMonitorSmartScheduleMinWeight                         = 10
	channelMonitorSmartScheduleMaxWeight                         = 100
	channelMonitorSmartScheduleWeightStep                        = 5
	channelMonitorSmartScheduleMinWeightChange                   = 10
	channelMonitorSmartScheduleMaxWeightChange                   = 20
	channelMonitorSmartScheduleSingleMetricMaxWeightChange       = 30
	channelMonitorSmartScheduleBaselinePriority            int64 = 80
	channelMonitorSmartScheduleDegradedPriority            int64 = 0
	channelMonitorSmartScheduleDegradedWeight              uint  = 0
	maxChannelSmartScheduleTaskFailureDetails                    = 100
)

type channelSmartScheduleTaskHandler struct{}

type channelSmartScheduleTaskPayload struct {
	ForceReset bool `json:"force_reset,omitempty"`
}

type channelSmartSchedulePerformance struct {
	FirstTokenSampleCount int
	TPSSampleCount        int
	AverageFirstTokenMs   *float64
	AverageTPS            *float64
	StabilitySampleCount  int64
	Stability             *float64
}

type channelSmartScheduleCandidate struct {
	ChannelId             int
	CurrentPriority       int64
	CurrentWeight         uint
	Ratio                 *float64
	FirstTokenMs          *float64
	TPS                   *float64
	FirstTokenSampleCount int
	TPSSampleCount        int
	StabilitySampleCount  int64
	Stability             *float64
	StabilityAvailable    bool
}

type channelSmartSchedulePlanItem struct {
	ChannelId       int
	Score           float64
	CurrentPriority int64
	CurrentWeight   uint
	TargetPriority  int64
	TargetWeight    uint
}

type channelSmartSchedulePlan struct {
	Items   []channelSmartSchedulePlanItem
	Skipped map[int]string
}

type channelSmartScheduleTaskFailure struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Error       string `json:"error"`
}

type channelSmartScheduleTaskResult struct {
	Strategy                string                            `json:"strategy"`
	StabilityEnabled        bool                              `json:"stability_enabled"`
	Scoring                 channelSmartScheduleScoring       `json:"scoring"`
	ForceReset              bool                              `json:"force_reset"`
	ApplyMode               string                            `json:"apply_mode"`
	Groups                  []string                          `json:"groups,omitempty"`
	GroupPolicies           smartScheduleGroupPolicies        `json:"group_policies,omitempty"`
	Model                   string                            `json:"model"`
	Models                  []string                          `json:"models,omitempty"`
	PerformanceMinutes      int                               `json:"performance_minutes"`
	MinSamples              int                               `json:"min_samples"`
	MinSuccessRate          float64                           `json:"min_success_rate"`
	CooldownMinutes         int                               `json:"cooldown_minutes"`
	Total                   int                               `json:"total"`
	Planned                 int                               `json:"planned"`
	Updated                 int                               `json:"updated"`
	Unchanged               int                               `json:"unchanged"`
	Skipped                 int                               `json:"skipped"`
	Failed                  int                               `json:"failed"`
	Failures                []channelSmartScheduleTaskFailure `json:"failures,omitempty"`
	FailureDetailsTruncated bool                              `json:"failure_details_truncated,omitempty"`
}

func init() {
	service.RegisterSystemTaskHandler(channelSmartScheduleTaskHandler{})
}

func (channelSmartScheduleTaskHandler) Type() string {
	return channelMonitorSmartScheduleTaskType
}

func (channelSmartScheduleTaskHandler) Enabled() bool {
	return getChannelMonitorSettings().SmartScheduleEnabled
}

func (channelSmartScheduleTaskHandler) Interval() time.Duration {
	minutes := getChannelMonitorSettings().SmartScheduleIntervalMinutes
	if minutes <= 0 {
		minutes = defaultChannelMonitorSmartScheduleInterval
	}
	return time.Duration(minutes) * time.Minute
}

func (channelSmartScheduleTaskHandler) NewPayload() any { return nil }

func (channelSmartScheduleTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := channelSmartScheduleTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, channelSmartScheduleTaskResult{}, err)
		return
	}
	summary, err := runChannelSmartScheduleOnce(
		ctx,
		service.NewSystemTaskProgressReporter(task, runnerID),
		payload.ForceReset,
	)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

func RunChannelMonitorSmartSchedule(c *gin.Context) {
	if !getChannelMonitorSettings().SmartScheduleEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "智能调度已禁用"})
		return
	}
	task, created, err := service.EnqueueSystemTask(channelMonitorSmartScheduleTaskType, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.monitor_smart_schedule_run", map[string]interface{}{
		"created": created,
		"task_id": task.TaskID,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"created": created,
			"task":    task.ToResponse(),
		},
	})
}

func (result *channelSmartScheduleTaskResult) recordFailure(channelId int, channelName string, failure error) {
	result.Failed++
	if len(result.Failures) >= maxChannelSmartScheduleTaskFailureDetails {
		result.FailureDetailsTruncated = true
		return
	}
	message := "智能调度更新失败"
	if failure != nil && failure.Error() != "" {
		message = failure.Error()
	}
	messageRunes := []rune(message)
	if len(messageRunes) > 255 {
		message = string(messageRunes[:255])
	}
	nameRunes := []rune(channelName)
	if len(nameRunes) > 128 {
		channelName = string(nameRunes[:128])
	}
	result.Failures = append(result.Failures, channelSmartScheduleTaskFailure{
		ChannelId:   channelId,
		ChannelName: channelName,
		Error:       message,
	})
}

func runChannelSmartScheduleOnce(ctx context.Context, reportProgress func(processed, total int), forceReset bool) (channelSmartScheduleTaskResult, error) {
	if reportProgress == nil {
		reportProgress = func(int, int) {}
	}
	settings := getChannelMonitorSettings()
	result := channelSmartScheduleTaskResult{
		Strategy:           settings.SmartScheduleStrategy,
		StabilityEnabled:   settings.SmartScheduleStabilityEnabled,
		Scoring:            settings.SmartScheduleScoring,
		ForceReset:         forceReset,
		ApplyMode:          settings.SmartScheduleApplyMode,
		Groups:             settings.SmartScheduleGroups,
		GroupPolicies:      settings.SmartScheduleGroupPolicies,
		Model:              settings.SmartScheduleModel,
		Models:             settings.SmartScheduleModels,
		PerformanceMinutes: settings.SmartSchedulePerformanceMinutes,
		MinSamples:         settings.SmartScheduleMinSamples,
		MinSuccessRate:     settings.SmartScheduleMinSuccessRate,
		CooldownMinutes:    settings.SmartScheduleCooldownMinutes,
	}
	if !settings.SmartScheduleEnabled {
		return result, fmt.Errorf("智能调度已禁用")
	}
	return runChannelSmartScheduleByRouteOnce(ctx, reportProgress, forceReset, settings, result)
}
func channelSmartScheduleSavedTarget(priority int64, weight uint) (int64, uint) {
	if priority <= channelMonitorSmartScheduleDegradedPriority {
		priority = channelMonitorSmartScheduleBaselinePriority
	}
	if weight == 0 {
		weight = channelMonitorSmartScheduleMinWeight
	}
	return priority, weight
}

func planChannelSmartSchedule(candidates []channelSmartScheduleCandidate, strategy string, stabilityEnabled bool, applyMode string, minSamples int, forceReset bool) channelSmartSchedulePlan {
	return planChannelSmartScheduleWithScoring(
		candidates,
		strategy,
		stabilityEnabled,
		applyMode,
		minSamples,
		forceReset,
		defaultChannelSmartScheduleScoring(),
	)
}

func planChannelSmartScheduleWithScoring(candidates []channelSmartScheduleCandidate, strategy string, stabilityEnabled bool, applyMode string, minSamples int, forceReset bool, scoring channelSmartScheduleScoring) channelSmartSchedulePlan {
	plan := channelSmartSchedulePlan{
		Skipped: make(map[int]string),
	}
	if minSamples <= 0 {
		minSamples = defaultChannelMonitorSmartScheduleSamples
	}
	if validateChannelSmartScheduleScoring(scoring) != nil {
		scoring = defaultChannelSmartScheduleScoring()
	}
	singleMetricStrategy := strategy == channelMonitorSmartScheduleStrategyRatio ||
		strategy == channelMonitorSmartScheduleStrategyFirstToken ||
		strategy == channelMonitorSmartScheduleStrategyTPS
	maxWeightChange := uint(channelMonitorSmartScheduleMaxWeightChange)
	if singleMetricStrategy {
		maxWeightChange = channelMonitorSmartScheduleSingleMetricMaxWeightChange
	}

	type cohort struct {
		Candidates []channelSmartScheduleCandidate
	}
	cohorts := make(map[int64]*cohort)
	for _, candidate := range candidates {
		if reason := channelSmartScheduleCandidateSkipReasonWithScoring(candidate, strategy, stabilityEnabled, minSamples, scoring); reason != "" {
			plan.Skipped[candidate.ChannelId] = reason
			continue
		}
		var key int64
		if applyMode == channelMonitorSmartScheduleApplyWeight && !forceReset {
			key = candidate.CurrentPriority
		}
		scheduleCohort := cohorts[key]
		if scheduleCohort == nil {
			scheduleCohort = &cohort{}
			cohorts[key] = scheduleCohort
		}
		scheduleCohort.Candidates = append(scheduleCohort.Candidates, candidate)
	}

	for _, scheduleCohort := range cohorts {
		if len(scheduleCohort.Candidates) < 2 {
			reason := "可调渠道不足 2 个"
			if applyMode == channelMonitorSmartScheduleApplyWeight && !forceReset {
				reason = "同优先级可调渠道不足 2 个"
			}
			for _, candidate := range scheduleCohort.Candidates {
				plan.Skipped[candidate.ChannelId] = reason
			}
			continue
		}
		ratioMin, ratioMax := math.Inf(1), math.Inf(-1)
		firstTokenMin, firstTokenMax := math.Inf(1), math.Inf(-1)
		tpsMin, tpsMax := math.Inf(1), math.Inf(-1)
		firstTokenAvailableCount := 0
		tpsAvailableCount := 0
		for _, candidate := range scheduleCohort.Candidates {
			if candidate.Ratio != nil {
				ratioMin = math.Min(ratioMin, *candidate.Ratio)
				ratioMax = math.Max(ratioMax, *candidate.Ratio)
			}
			if candidate.FirstTokenMs != nil && candidate.FirstTokenSampleCount >= minSamples {
				firstTokenMin = math.Min(firstTokenMin, *candidate.FirstTokenMs)
				firstTokenMax = math.Max(firstTokenMax, *candidate.FirstTokenMs)
				firstTokenAvailableCount++
			}
			if candidate.TPS != nil && candidate.TPSSampleCount >= minSamples {
				tpsMin = math.Min(tpsMin, *candidate.TPS)
				tpsMax = math.Max(tpsMax, *candidate.TPS)
				tpsAvailableCount++
			}
		}

		items := make([]channelSmartSchedulePlanItem, 0, len(scheduleCohort.Candidates))
		scoreMin := math.Inf(1)
		scoreMax := math.Inf(-1)
		for _, candidate := range scheduleCohort.Candidates {
			ratioScore := 0.0
			if candidate.Ratio != nil {
				ratioScore = channelSmartScheduleLowerIsBetterScore(*candidate.Ratio, ratioMin, ratioMax)
			}
			firstTokenScore := 0.0
			if candidate.FirstTokenMs != nil {
				firstTokenScore = channelSmartScheduleLowerIsBetterScore(*candidate.FirstTokenMs, firstTokenMin, firstTokenMax)
			}
			tpsScore := 0.0
			if candidate.TPS != nil {
				tpsScore = channelSmartScheduleHigherIsBetterScore(*candidate.TPS, tpsMin, tpsMax)
			}
			businessScore := 0.0
			switch strategy {
			case channelMonitorSmartScheduleStrategyRatio:
				businessScore = channelSmartScheduleWeightedScore(
					channelSmartScheduleScorePart{
						Score: ratioScore, Percent: scoring.Ratio.CostRatioPercent,
						Available: candidate.Ratio != nil,
					},
					channelSmartScheduleScorePart{
						Score: firstTokenScore, Percent: scoring.Ratio.FirstTokenPercent,
						Available: candidate.FirstTokenMs != nil && candidate.FirstTokenSampleCount >= minSamples && firstTokenAvailableCount >= 2,
					},
					channelSmartScheduleScorePart{
						Score: tpsScore, Percent: scoring.Ratio.TPSPercent,
						Available: candidate.TPS != nil && candidate.TPSSampleCount >= minSamples && tpsAvailableCount >= 2,
					},
				)
			case channelMonitorSmartScheduleStrategyFirstToken:
				businessScore = firstTokenScore
			case channelMonitorSmartScheduleStrategyTPS:
				businessScore = tpsScore
			case channelMonitorSmartScheduleStrategySmart:
				businessScore = channelSmartScheduleWeightedScore(
					channelSmartScheduleScorePart{
						Score: ratioScore, Percent: scoring.Smart.CostRatioPercent,
						Available: candidate.Ratio != nil,
					},
					channelSmartScheduleScorePart{
						Score: firstTokenScore, Percent: scoring.Smart.FirstTokenPercent,
						Available: candidate.FirstTokenMs != nil && candidate.FirstTokenSampleCount >= minSamples,
					},
					channelSmartScheduleScorePart{
						Score: tpsScore, Percent: scoring.Smart.TPSPercent,
						Available: candidate.TPS != nil && candidate.TPSSampleCount >= minSamples,
					},
				)
			default:
				continue
			}
			score := businessScore
			if stabilityEnabled && candidate.Stability != nil && candidate.StabilitySampleCount >= int64(minSamples) {
				stabilityScore := *candidate.Stability
				if stabilityScore < 0 {
					stabilityScore = 0
				} else if stabilityScore > 1 {
					stabilityScore = 1
				}
				stabilityWeight := scoring.StabilityPercent / channelMonitorScorePercentageTotal
				score = (1-stabilityWeight)*score + stabilityWeight*stabilityScore
			}
			if score < 0 {
				score = 0
			} else if score > 1 {
				score = 1
			}
			scoreMin = math.Min(scoreMin, score)
			scoreMax = math.Max(scoreMax, score)
			targetPriority := candidate.CurrentPriority
			if forceReset && applyMode == channelMonitorSmartScheduleApplyWeight {
				targetPriority = channelMonitorSmartScheduleBaselinePriority
			}
			items = append(items, channelSmartSchedulePlanItem{
				ChannelId:       candidate.ChannelId,
				Score:           score,
				CurrentPriority: candidate.CurrentPriority,
				CurrentWeight:   candidate.CurrentWeight,
				TargetPriority:  targetPriority,
			})
		}
		for index := range items {
			weightScore := channelSmartScheduleWeightScore(
				items[index].Score,
				scoreMin,
				scoreMax,
				scoring,
			)
			targetWeight := uint(math.Round((channelMonitorSmartScheduleMinWeight+weightScore*(channelMonitorSmartScheduleMaxWeight-channelMonitorSmartScheduleMinWeight))/channelMonitorSmartScheduleWeightStep) * channelMonitorSmartScheduleWeightStep)
			if targetWeight < channelMonitorSmartScheduleMinWeight {
				targetWeight = channelMonitorSmartScheduleMinWeight
			} else if targetWeight > channelMonitorSmartScheduleMaxWeight {
				targetWeight = channelMonitorSmartScheduleMaxWeight
			}
			if !forceReset {
				targetWeight = channelSmartScheduleDampedWeight(items[index].CurrentWeight, targetWeight, maxWeightChange)
			}
			items[index].TargetWeight = targetWeight
		}

		sort.Slice(items, func(i int, j int) bool {
			if math.Abs(items[i].Score-items[j].Score) > channelMonitorRatioEpsilon {
				return items[i].Score > items[j].Score
			}
			return items[i].ChannelId < items[j].ChannelId
		})
		if applyMode == channelMonitorSmartScheduleApplyPriorityWeight {
			priorities := []int64{100, 90, 80}
			for index := range items {
				tier := index * len(priorities) / len(items)
				if tier >= len(priorities) {
					tier = len(priorities) - 1
				}
				items[index].TargetPriority = priorities[tier]
			}
		}
		plan.Items = append(plan.Items, items...)
	}

	sort.Slice(plan.Items, func(i int, j int) bool {
		return plan.Items[i].ChannelId < plan.Items[j].ChannelId
	})
	return plan
}

func channelSmartScheduleWeightScore(score, scoreMin, scoreMax float64, scoring channelSmartScheduleScoring) float64 {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0
	}
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	absoluteScore := math.Pow(score, scoring.CurveExponent)
	if !scoring.RelativeWeightEnabled || scoreMax-scoreMin <= channelMonitorRatioEpsilon ||
		scoring.RelativeWeightFullPercent <= scoring.RelativeWeightStartPercent {
		return absoluteScore
	}
	relativeScore := (score - scoreMin) / (scoreMax - scoreMin)
	if relativeScore < 0 {
		relativeScore = 0
	} else if relativeScore > 1 {
		relativeScore = 1
	}
	relativeScore = math.Pow(relativeScore, scoring.CurveExponent)
	spreadPercent := (scoreMax - scoreMin) * channelMonitorScorePercentageTotal
	blend := (spreadPercent - scoring.RelativeWeightStartPercent) /
		(scoring.RelativeWeightFullPercent - scoring.RelativeWeightStartPercent)
	if blend <= 0 {
		return absoluteScore
	}
	if blend > 1 {
		blend = 1
	}
	return absoluteScore + (relativeScore-absoluteScore)*blend
}

func channelSmartScheduleCandidateSkipReason(candidate channelSmartScheduleCandidate, strategy string, stabilityEnabled bool, minSamples int) string {
	return channelSmartScheduleCandidateSkipReasonWithScoring(
		candidate,
		strategy,
		stabilityEnabled,
		minSamples,
		defaultChannelSmartScheduleScoring(),
	)
}

func channelSmartScheduleCandidateSkipReasonWithScoring(candidate channelSmartScheduleCandidate, strategy string, stabilityEnabled bool, minSamples int, scoring channelSmartScheduleScoring) string {
	if stabilityEnabled && !candidate.StabilityAvailable {
		return "稳定性统计不可用，请开启消费日志和 ERROR_LOG_ENABLED"
	}
	usesBusinessScore := channelSmartScheduleUsesBusinessScore(stabilityEnabled, scoring)
	needsRatio := usesBusinessScore && (strategy == channelMonitorSmartScheduleStrategyRatio ||
		(strategy == channelMonitorSmartScheduleStrategySmart && scoring.Smart.CostRatioPercent > 0))
	if needsRatio {
		if candidate.Ratio == nil {
			return "未记录成本倍率"
		}
	}
	needsFirstToken := usesBusinessScore && (strategy == channelMonitorSmartScheduleStrategyFirstToken ||
		(strategy == channelMonitorSmartScheduleStrategySmart && scoring.Smart.FirstTokenPercent > 0))
	if needsFirstToken {
		if candidate.FirstTokenMs == nil || candidate.FirstTokenSampleCount < minSamples {
			return fmt.Sprintf("首字样本不足（%d/%d）", candidate.FirstTokenSampleCount, minSamples)
		}
	}
	needsTPS := usesBusinessScore && (strategy == channelMonitorSmartScheduleStrategyTPS ||
		(strategy == channelMonitorSmartScheduleStrategySmart && scoring.Smart.TPSPercent > 0))
	if needsTPS {
		if candidate.TPS == nil || candidate.TPSSampleCount < minSamples {
			return fmt.Sprintf("TPS 样本不足（%d/%d）", candidate.TPSSampleCount, minSamples)
		}
	}
	if stabilityEnabled {
		if candidate.Stability == nil || candidate.StabilitySampleCount < int64(minSamples) {
			return fmt.Sprintf("稳定性样本不足（%d/%d）", candidate.StabilitySampleCount, minSamples)
		}
	}
	return ""
}

func channelSmartScheduleCandidateNeedsExploration(candidate channelSmartScheduleCandidate, strategy string, stabilityEnabled bool, minSamples int) bool {
	return channelSmartScheduleCandidateNeedsExplorationWithScoring(
		candidate,
		strategy,
		stabilityEnabled,
		minSamples,
		defaultChannelSmartScheduleScoring(),
	)
}

func channelSmartScheduleCandidateNeedsExplorationWithScoring(candidate channelSmartScheduleCandidate, strategy string, stabilityEnabled bool, minSamples int, scoring channelSmartScheduleScoring) bool {
	if minSamples <= 0 {
		minSamples = defaultChannelMonitorSmartScheduleSamples
	}
	if stabilityEnabled && candidate.StabilityAvailable &&
		(candidate.Stability == nil || candidate.StabilitySampleCount < int64(minSamples)) {
		return true
	}
	usesBusinessScore := channelSmartScheduleUsesBusinessScore(stabilityEnabled, scoring)
	needsFirstToken := usesBusinessScore && (strategy == channelMonitorSmartScheduleStrategyFirstToken ||
		(strategy == channelMonitorSmartScheduleStrategySmart && scoring.Smart.FirstTokenPercent > 0))
	if needsFirstToken {
		if candidate.FirstTokenMs == nil || candidate.FirstTokenSampleCount < minSamples {
			return true
		}
	}
	needsTPS := usesBusinessScore && (strategy == channelMonitorSmartScheduleStrategyTPS ||
		(strategy == channelMonitorSmartScheduleStrategySmart && scoring.Smart.TPSPercent > 0))
	if needsTPS {
		if candidate.TPS == nil || candidate.TPSSampleCount < minSamples {
			return true
		}
	}
	return false
}

func channelSmartScheduleLowerIsBetterScore(value float64, minimum float64, maximum float64) float64 {
	if maximum-minimum <= channelMonitorRatioEpsilon {
		return 1
	}
	return (maximum - value) / (maximum - minimum)
}

func channelSmartScheduleHigherIsBetterScore(value float64, minimum float64, maximum float64) float64 {
	if maximum-minimum <= channelMonitorRatioEpsilon {
		return 1
	}
	return (value - minimum) / (maximum - minimum)
}

func channelSmartScheduleDampedWeight(current uint, target uint, maxWeightChange uint) uint {
	if current == 0 {
		return target
	}
	if current > target {
		difference := current - target
		if difference < channelMonitorSmartScheduleMinWeightChange {
			return current
		}
		if difference > maxWeightChange {
			return current - maxWeightChange
		}
		return target
	}
	difference := target - current
	if difference < channelMonitorSmartScheduleMinWeightChange {
		return current
	}
	if difference > maxWeightChange {
		return current + maxWeightChange
	}
	return target
}
