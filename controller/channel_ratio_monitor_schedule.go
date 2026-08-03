package controller

import (
	"context"
	"errors"
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
	channelMonitorSmartScheduleTaskType                    = "channel_smart_schedule"
	channelSmartScheduleAdjustmentUpdated                  = "updated"
	channelSmartScheduleAdjustmentUnchanged                = "unchanged"
	channelSmartScheduleAdjustmentSkipped                  = "skipped"
	channelSmartScheduleAdjustmentFailed                   = "failed"
	channelMonitorSmartScheduleMinWeight                   = 10
	channelMonitorSmartScheduleMaxWeight                   = 100
	channelMonitorSmartScheduleAllocationWeightTotal       = 1000
	channelMonitorSmartScheduleTemporaryWeightTotal        = 10000
	channelMonitorSmartScheduleAllocationScoreFloor        = 0.01
	channelMonitorSmartScheduleFallbackMinSamples          = 5
	channelMonitorSmartScheduleBaselinePriority      int64 = 80
	channelMonitorSmartScheduleDegradedPriority      int64 = 0
	channelMonitorSmartScheduleDegradedWeight        uint  = 0
	maxChannelSmartScheduleTaskFailureDetails              = 100
)

type channelSmartScheduleTaskHandler struct{}

type channelSmartScheduleTaskPayload struct {
	ForceReset bool `json:"force_reset,omitempty"`
}

type channelSmartSchedulePerformance struct {
	SampleGroupCount                     int
	FirstTokenSampleCount                int
	FirstTokenDurationSampleCount        int64
	TPSSampleCount                       int
	AverageFirstTokenMs                  *float64
	WinsorizedAverageFirstTokenMs        *float64
	FirstTokenP50Ms                      *float64
	FirstTokenP95Ms                      *float64
	FirstTokenDurationBuckets            []model.ChannelMonitorDurationBucket
	AverageTPS                           *float64
	StabilitySampleCount                 int64
	Stability                            *float64
	StabilitySuccessCount                int64
	StabilityFailureCount                int64
	StabilityFinalFailureCount           int64
	StabilityRetryFailureCount           int64
	StabilityRetryFailureDurationTotalMs float64
	StabilityFailureDurationBuckets      []model.ChannelMonitorFailureDurationBucket
	JitterAvailable                      bool
	JitterBaselineMs                     *float64
	JitterThresholdMs                    *float64
	JitterSampleCount                    int64
	JitterSlowCount                      int64
	JitterAllowedCount                   int64
	JitterPenalty                        float64
}

type channelSmartScheduleCandidate struct {
	ChannelId             int
	SampleGroupCount      int
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
	ManualPrimary         bool
	PreviousBaseRank      int
	ManualTargetPriority  int64
}

type channelSmartSchedulePlanItem struct {
	ChannelId        int
	Score            float64
	ScoreDetails     *model.ChannelSmartScheduleScoreDetails
	CurrentPriority  int64
	CurrentWeight    uint
	TargetPriority   int64
	TargetWeight     uint
	Scored           bool
	SkipReason       string
	BaseRank         int
	BasePriority     int64
	BaseWeight       uint
	PreviousBaseRank int
}

type channelSmartSchedulePlan struct {
	Items           []channelSmartSchedulePlanItem
	Skipped         map[int]string
	Details         map[int]*model.ChannelSmartScheduleScoreDetails
	RawWinnerId     int
	ActualPrimaryId int
}

type channelSmartScheduleTaskFailure struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Group       string `json:"group"`
	Model       string `json:"model"`
	Stage       string `json:"failure_stage"`
	Error       string `json:"error"`
}

type channelSmartScheduleTaskAdjustment struct {
	ChannelId                          int                                     `json:"channel_id"`
	ChannelName                        string                                  `json:"channel_name"`
	Group                              string                                  `json:"group"`
	Model                              string                                  `json:"model"`
	Action                             string                                  `json:"action"`
	OldPriority                        int64                                   `json:"old_priority"`
	NewPriority                        int64                                   `json:"new_priority"`
	OldWeight                          uint                                    `json:"old_weight"`
	NewWeight                          uint                                    `json:"new_weight"`
	Score                              *float64                                `json:"score,omitempty"`
	ScoreDetails                       *model.ChannelSmartScheduleScoreDetails `json:"score_details,omitempty"`
	Reason                             string                                  `json:"reason"`
	ManualPrimary                      bool                                    `json:"manual_primary,omitempty"`
	ManualPrimaryUntil                 int64                                   `json:"manual_primary_until,omitempty"`
	ManualPrimaryAllowStabilityDegrade bool                                    `json:"manual_primary_allow_stability_degrade,omitempty"`
	FailureStage                       string                                  `json:"failure_stage,omitempty"`
	PreviousEffectiveTime              int64                                   `json:"previous_effective_time,omitempty"`
	PreviousEffectivePriority          int64                                   `json:"previous_effective_priority"`
	PreviousEffectiveWeight            uint                                    `json:"previous_effective_weight"`
}

type channelSmartScheduleTaskResult struct {
	ForceReset               bool                                 `json:"force_reset"`
	GroupPolicies            smartScheduleGroupPolicies           `json:"group_policies,omitempty"`
	GroupPolicyCount         int                                  `json:"group_policy_count,omitempty"`
	PerformanceWindowMinutes int                                  `json:"performance_window_minutes"`
	StabilityWindowMinutes   int                                  `json:"stability_window_minutes"`
	Total                    int                                  `json:"total"`
	Planned                  int                                  `json:"planned"`
	Updated                  int                                  `json:"updated"`
	Unchanged                int                                  `json:"unchanged"`
	Skipped                  int                                  `json:"skipped"`
	Failed                   int                                  `json:"failed"`
	Failures                 []channelSmartScheduleTaskFailure    `json:"failures,omitempty"`
	FailureDetailsTruncated  bool                                 `json:"failure_details_truncated,omitempty"`
	Adjustments              []channelSmartScheduleTaskAdjustment `json:"adjustments,omitempty"`
}

func init() {
	service.RegisterSystemTaskHandler(channelSmartScheduleTaskHandler{})
}

func (channelSmartScheduleTaskHandler) Type() string {
	return channelMonitorSmartScheduleTaskType
}

func (channelSmartScheduleTaskHandler) Enabled() bool {
	settings := getChannelMonitorSettings()
	return settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) > 0
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
	summary, runErr := runChannelSmartScheduleOnce(
		ctx,
		service.NewSystemTaskProgressReporter(task, runnerID),
		payload.ForceReset,
	)
	detailInputs := make([]model.ChannelSmartScheduleExecutionDetailInput, 0, len(summary.Adjustments))
	for index, adjustment := range summary.Adjustments {
		detailInputs = append(detailInputs, model.ChannelSmartScheduleExecutionDetailInput{
			AdjustmentIndex: index,
			Payload:         adjustment,
		})
	}
	storedSummary := summary
	storedSummary.Adjustments = nil
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if runErr != nil {
		status = model.SystemTaskStatusFailed
		errorMessage = runErr.Error()
	}
	if err := model.FinishChannelSmartScheduleTaskWithExecutionDetails(
		task.TaskID,
		runnerID,
		status,
		storedSummary,
		errorMessage,
		detailInputs,
	); err == nil {
		return
	} else if errors.Is(err, model.ErrSystemTaskLockLost) {
		common.SysLog(fmt.Sprintf("system task %s failed to persist result: %v", task.TaskID, err))
		return
	} else if runErr == nil {
		runErr = fmt.Errorf("保存智能调度任务结果和执行评分明细失败: %w", err)
	} else {
		runErr = fmt.Errorf("%w（保存智能调度任务结果和执行评分明细失败：%v）", runErr, err)
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, storedSummary, runErr)
}

func RunChannelMonitorSmartSchedule(c *gin.Context) {
	settings := getChannelMonitorSettings()
	if len(settings.SmartScheduleGroupPolicies) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "智能调度已禁用，请先配置至少一个完整的分组策略"})
		return
	}
	if !settings.SmartScheduleEnabled {
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

func (result *channelSmartScheduleTaskResult) recordFailure(
	channelId int,
	channelName string,
	group string,
	modelName string,
	stage string,
	failure error,
) string {
	result.Failed++
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
	if len(result.Failures) >= maxChannelSmartScheduleTaskFailureDetails {
		result.FailureDetailsTruncated = true
		return message
	}
	result.Failures = append(result.Failures, channelSmartScheduleTaskFailure{
		ChannelId:   channelId,
		ChannelName: channelName,
		Group:       group,
		Model:       modelName,
		Stage:       stage,
		Error:       message,
	})
	return message
}

func (result *channelSmartScheduleTaskResult) recordAdjustment(adjustment channelSmartScheduleTaskAdjustment) {
	nameRunes := []rune(adjustment.ChannelName)
	if len(nameRunes) > 128 {
		adjustment.ChannelName = string(nameRunes[:128])
	}
	groupRunes := []rune(adjustment.Group)
	if len(groupRunes) > 128 {
		adjustment.Group = string(groupRunes[:128])
	}
	modelRunes := []rune(adjustment.Model)
	if len(modelRunes) > 128 {
		adjustment.Model = string(modelRunes[:128])
	}
	reasonRunes := []rune(adjustment.Reason)
	if len(reasonRunes) > 255 {
		adjustment.Reason = string(reasonRunes[:255])
	}
	result.Adjustments = append(result.Adjustments, adjustment)
}

func (result *channelSmartScheduleTaskResult) finalizeAdjustments() {
	sort.SliceStable(result.Adjustments, func(i int, j int) bool {
		left := result.Adjustments[i]
		right := result.Adjustments[j]
		if left.Action != right.Action {
			return channelSmartScheduleAdjustmentActionOrder(left.Action) < channelSmartScheduleAdjustmentActionOrder(right.Action)
		}
		if left.Group != right.Group {
			return left.Group < right.Group
		}
		if left.Model != right.Model {
			return left.Model < right.Model
		}
		return left.ChannelId < right.ChannelId
	})
}

func channelSmartScheduleAdjustmentActionOrder(action string) int {
	switch action {
	case channelSmartScheduleAdjustmentFailed:
		return 0
	case channelSmartScheduleAdjustmentUpdated:
		return 1
	case channelSmartScheduleAdjustmentSkipped:
		return 2
	case channelSmartScheduleAdjustmentUnchanged:
		return 3
	default:
		return 4
	}
}

func runChannelSmartScheduleOnce(ctx context.Context, reportProgress func(processed, total int), forceReset bool) (channelSmartScheduleTaskResult, error) {
	if reportProgress == nil {
		reportProgress = func(int, int) {}
	}
	settings := getChannelMonitorSettings()
	result := channelSmartScheduleTaskResult{
		ForceReset:               forceReset,
		GroupPolicies:            settings.SmartScheduleGroupPolicies,
		PerformanceWindowMinutes: settings.SmartSchedulePerformanceWindowMinutes,
		StabilityWindowMinutes:   settings.SmartScheduleStabilityWindowMinutes,
	}
	if len(settings.SmartScheduleGroupPolicies) == 0 {
		return result, fmt.Errorf("智能调度已禁用，请先配置至少一个完整的分组策略")
	}
	if !settings.SmartScheduleEnabled {
		return result, fmt.Errorf("智能调度已禁用")
	}
	result, err := runChannelSmartScheduleByRouteOnce(ctx, reportProgress, forceReset, settings, result)
	result.finalizeAdjustments()
	return result, err
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
	if applyMode == channelMonitorSmartScheduleApplyPriorityWeight {
		return planChannelSmartSchedulePriorityWeight(
			candidates, strategy, stabilityEnabled, minSamples, forceReset, scoring,
		)
	}
	plan := channelSmartSchedulePlan{
		Skipped: make(map[int]string),
		Details: make(map[int]*model.ChannelSmartScheduleScoreDetails),
	}
	if minSamples <= 0 {
		minSamples = channelMonitorSmartScheduleFallbackMinSamples
	}
	if validateChannelSmartScheduleScoring(scoring) != nil {
		scoring = defaultChannelSmartScheduleScoring()
	}
	type cohort struct {
		Candidates []channelSmartScheduleCandidate
	}
	manualPrimaryPriority := int64(0)
	for _, candidate := range candidates {
		if candidate.CurrentPriority > manualPrimaryPriority {
			manualPrimaryPriority = candidate.CurrentPriority
		}
	}
	cohorts := make(map[int64]*cohort)
	for _, candidate := range candidates {
		details := channelSmartScheduleNewScoreDetails(
			candidate, strategy, stabilityEnabled, applyMode, minSamples, forceReset, scoring,
		)
		plan.Details[candidate.ChannelId] = details
		if reason := channelSmartScheduleCandidateSkipReasonWithScoring(candidate, strategy, stabilityEnabled, minSamples, scoring); reason != "" && !candidate.ManualPrimary {
			plan.Skipped[candidate.ChannelId] = reason
			channelSmartScheduleSetAdjustmentReason(details, reason)
			continue
		}
		var key int64
		if applyMode == channelMonitorSmartScheduleApplyWeight {
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
		if len(scheduleCohort.Candidates) < 2 &&
			(len(scheduleCohort.Candidates) == 0 || !scheduleCohort.Candidates[0].ManualPrimary) {
			reason := "可调渠道不足 2 个"
			if applyMode == channelMonitorSmartScheduleApplyWeight {
				reason = "同优先级可调渠道不足 2 个"
			}
			for _, candidate := range scheduleCohort.Candidates {
				plan.Skipped[candidate.ChannelId] = reason
				channelSmartScheduleSetAdjustmentReason(plan.Details[candidate.ChannelId], reason)
			}
			continue
		}
		normalization := channelSmartScheduleBuildNormalization(scheduleCohort.Candidates, minSamples)

		items := make([]channelSmartSchedulePlanItem, 0, len(scheduleCohort.Candidates))
		for _, candidate := range scheduleCohort.Candidates {
			score, details, valid := channelSmartScheduleScoreCandidate(
				candidate, strategy, stabilityEnabled, applyMode, minSamples, forceReset, scoring, normalization,
			)
			if !valid {
				continue
			}
			if applyMode == channelMonitorSmartScheduleApplyWeight {
				priority := candidate.CurrentPriority
				details.Cohort.Priority = &priority
			}
			plan.Details[candidate.ChannelId] = details
			targetPriority := candidate.CurrentPriority
			if candidate.ManualPrimary && applyMode == channelMonitorSmartScheduleApplyWeight {
				targetPriority = manualPrimaryPriority
			}
			items = append(items, channelSmartSchedulePlanItem{
				ChannelId:       candidate.ChannelId,
				Score:           score,
				ScoreDetails:    details,
				Scored:          true,
				CurrentPriority: candidate.CurrentPriority,
				CurrentWeight:   candidate.CurrentWeight,
				TargetPriority:  targetPriority,
			})
		}

		currentPrimaryId := channelSmartScheduleCurrentPrimaryId(items)
		cohortManualPrimaryId := 0
		for _, candidate := range scheduleCohort.Candidates {
			if candidate.ManualPrimary {
				cohortManualPrimaryId = candidate.ChannelId
				break
			}
		}
		rawWinnerId := 0
		if ranked := channelSmartScheduleRankedItemIndexes(items, currentPrimaryId); len(ranked) > 0 {
			rawWinnerId = items[ranked[0]].ChannelId
		}
		effectivePrimaryId := channelSmartScheduleEffectivePrimaryId(
			items,
			currentPrimaryId,
			scoring.PrimarySwitchThresholdPercent/channelMonitorScorePercentageTotal,
			forceReset,
		)
		if cohortManualPrimaryId > 0 {
			effectivePrimaryId = cohortManualPrimaryId
		}
		if applyMode == channelMonitorSmartScheduleApplyWeight {
			channelSmartScheduleAssignPrimaryTrafficWeights(
				items,
				effectivePrimaryId,
				scoring.PrimaryTrafficPercent,
			)
		} else if applyMode == channelMonitorSmartScheduleApplyPriorityWeight {
			channelSmartScheduleAssignPriorityWeightTargets(items, effectivePrimaryId)
		}
		selectionReason := channelSmartScheduleSelectionReason(
			items, currentPrimaryId, rawWinnerId, effectivePrimaryId, cohortManualPrimaryId,
			scoring.PrimarySwitchThresholdPercent, forceReset,
		)
		for index := range items {
			details := items[index].ScoreDetails
			details.Decision.CurrentPrimaryChannelId = currentPrimaryId
			details.Decision.RawWinnerChannelId = rawWinnerId
			details.Decision.SelectedPrimaryChannelId = effectivePrimaryId
			details.Decision.ActualPrimaryChannelId = effectivePrimaryId
			details.Decision.SelectedPrimary = items[index].ChannelId == effectivePrimaryId
			details.Decision.AppliedPriority = items[index].TargetPriority
			details.Decision.AppliedWeight = items[index].TargetWeight
			details.Decision.ManualPrimaryChannelId = cohortManualPrimaryId
			details.Decision.SelectionReason = selectionReason
			channelSmartScheduleSetAdjustmentReason(details, details.Decision.AdjustmentReason)
		}
		plan.Items = append(plan.Items, items...)
	}

	sort.Slice(plan.Items, func(i int, j int) bool {
		return plan.Items[i].ChannelId < plan.Items[j].ChannelId
	})
	return plan
}

func planChannelSmartSchedulePriorityWeight(
	candidates []channelSmartScheduleCandidate,
	strategy string,
	stabilityEnabled bool,
	minSamples int,
	forceReset bool,
	scoring channelSmartScheduleScoring,
) channelSmartSchedulePlan {
	plan := channelSmartSchedulePlan{
		Skipped: make(map[int]string),
		Details: make(map[int]*model.ChannelSmartScheduleScoreDetails, len(candidates)),
	}
	if minSamples <= 0 {
		minSamples = channelMonitorSmartScheduleFallbackMinSamples
	}
	if validateChannelSmartScheduleScoring(scoring) != nil {
		scoring = defaultChannelSmartScheduleScoring()
	}

	normalization := channelSmartScheduleBuildNormalization(candidates, minSamples)
	scoredItems := make([]channelSmartSchedulePlanItem, 0, len(candidates))
	pendingItems := make([]channelSmartSchedulePlanItem, 0, len(candidates))
	manualPrimaryId := 0
	for _, candidate := range candidates {
		if candidate.ManualPrimary {
			manualPrimaryId = candidate.ChannelId
		}
		reason := channelSmartScheduleCandidateSkipReasonWithScoring(
			candidate, strategy, stabilityEnabled, minSamples, scoring,
		)
		item := channelSmartSchedulePlanItem{
			ChannelId:        candidate.ChannelId,
			CurrentPriority:  candidate.CurrentPriority,
			CurrentWeight:    candidate.CurrentWeight,
			TargetPriority:   candidate.CurrentPriority,
			PreviousBaseRank: candidate.PreviousBaseRank,
			SkipReason:       reason,
		}
		if reason == "" {
			score, details, valid := channelSmartScheduleScoreCandidate(
				candidate, strategy, stabilityEnabled, channelMonitorSmartScheduleApplyPriorityWeight,
				minSamples, forceReset, scoring, normalization,
			)
			if valid {
				item.Score = score
				item.ScoreDetails = details
				item.Scored = true
				scoredItems = append(scoredItems, item)
				plan.Details[candidate.ChannelId] = details
				continue
			}
			reason = "评分计算结果不可用"
			item.SkipReason = reason
		}
		details := channelSmartScheduleNewScoreDetails(
			candidate, strategy, stabilityEnabled, channelMonitorSmartScheduleApplyPriorityWeight,
			minSamples, forceReset, scoring,
		)
		channelSmartScheduleSetAdjustmentReason(details, reason)
		item.ScoreDetails = details
		pendingItems = append(pendingItems, item)
		plan.Details[candidate.ChannelId] = details
	}

	allCurrentItems := append(append([]channelSmartSchedulePlanItem(nil), scoredItems...), pendingItems...)
	currentPrimaryId := 0
	for _, item := range allCurrentItems {
		if item.PreviousBaseRank == 1 {
			currentPrimaryId = item.ChannelId
			break
		}
	}
	if currentPrimaryId == 0 {
		currentItems := allCurrentItems
		if manualPrimaryId > 0 {
			currentItems = make([]channelSmartSchedulePlanItem, 0, len(allCurrentItems)-1)
			for _, item := range allCurrentItems {
				if item.ChannelId != manualPrimaryId {
					currentItems = append(currentItems, item)
				}
			}
		}
		currentPrimaryId = channelSmartScheduleCurrentPrimaryId(currentItems)
	}
	rawWinnerId := 0
	if ranked := channelSmartScheduleRankedItemIndexes(scoredItems, currentPrimaryId); len(ranked) > 0 {
		rawWinnerId = scoredItems[ranked[0]].ChannelId
	}
	automaticPrimaryId := 0
	if len(scoredItems) > 0 {
		automaticPrimaryId = channelSmartScheduleEffectivePrimaryId(
			scoredItems,
			currentPrimaryId,
			scoring.PrimarySwitchThresholdPercent/channelMonitorScorePercentageTotal,
			forceReset,
		)
	} else if currentPrimaryId > 0 {
		automaticPrimaryId = currentPrimaryId
	}
	actualPrimaryId := automaticPrimaryId
	if manualPrimaryId > 0 {
		actualPrimaryId = manualPrimaryId
	}

	rankedItems := make([]channelSmartSchedulePlanItem, 0, len(candidates))
	if automaticPrimaryId > 0 {
		for _, item := range allCurrentItems {
			if item.ChannelId == automaticPrimaryId {
				rankedItems = append(rankedItems, item)
				break
			}
		}
	}
	for _, index := range channelSmartScheduleRankedItemIndexes(scoredItems, currentPrimaryId) {
		item := scoredItems[index]
		if item.ChannelId != automaticPrimaryId {
			rankedItems = append(rankedItems, item)
		}
	}
	sort.SliceStable(pendingItems, func(i int, j int) bool {
		leftRank := pendingItems[i].PreviousBaseRank
		rightRank := pendingItems[j].PreviousBaseRank
		if leftRank > 0 || rightRank > 0 {
			if leftRank <= 0 {
				return false
			}
			if rightRank <= 0 {
				return true
			}
			if leftRank != rightRank {
				return leftRank < rightRank
			}
		}
		return pendingItems[i].ChannelId < pendingItems[j].ChannelId
	})
	for _, item := range pendingItems {
		if item.ChannelId != automaticPrimaryId {
			rankedItems = append(rankedItems, item)
		}
	}
	if actualPrimaryId == 0 && len(rankedItems) > 0 {
		actualPrimaryId = rankedItems[0].ChannelId
	}

	selectionReason := "本轮没有可选主渠道"
	if manualPrimaryId > 0 {
		selectionReason = "管理员固定主渠道优先于本轮评分结果"
	} else if rawWinnerId > 0 {
		selectionReason = channelSmartScheduleSelectionReason(
			scoredItems, currentPrimaryId, rawWinnerId, actualPrimaryId, 0,
			scoring.PrimarySwitchThresholdPercent, forceReset,
		)
	} else if actualPrimaryId > 0 {
		selectionReason = "当前没有足够的评分样本，按上一轮基础排名和渠道 ID 选择实际主渠道"
	}

	normalCount := len(rankedItems)
	manualPrimaryFloor := int64(normalCount)
	if manualPrimaryFloor < math.MaxInt64 {
		manualPrimaryFloor++
	}
	for index := range rankedItems {
		item := &rankedItems[index]
		item.BaseRank = index + 1
		item.BasePriority = int64(normalCount - index)
		item.BaseWeight = channelMonitorSmartScheduleAllocationWeightTotal
		item.TargetPriority = item.BasePriority
		item.TargetWeight = item.BaseWeight
		for _, candidate := range candidates {
			if candidate.ChannelId != item.ChannelId || !candidate.ManualPrimary {
				continue
			}
			item.TargetPriority = max(item.TargetPriority, manualPrimaryFloor, candidate.ManualTargetPriority)
			break
		}
		details := item.ScoreDetails
		details.Decision.CurrentPrimaryChannelId = currentPrimaryId
		details.Decision.RawWinnerChannelId = rawWinnerId
		details.Decision.SelectedPrimaryChannelId = actualPrimaryId
		details.Decision.ActualPrimaryChannelId = actualPrimaryId
		details.Decision.SelectedPrimary = item.ChannelId == actualPrimaryId
		details.Decision.ManualPrimaryChannelId = manualPrimaryId
		details.Decision.BaseRank = item.BaseRank
		details.Decision.BasePriority = item.BasePriority
		details.Decision.BaseWeight = item.BaseWeight
		details.Decision.AppliedPriority = item.TargetPriority
		details.Decision.AppliedWeight = item.TargetWeight
		details.Decision.SelectionReason = selectionReason
		if item.SkipReason != "" {
			channelSmartScheduleSetAdjustmentReason(
				details,
				item.SkipReason+"，已排在有效评分渠道之后并保留独立基础优先级",
			)
		} else {
			channelSmartScheduleSetAdjustmentReason(details, details.Decision.AdjustmentReason)
		}
	}
	plan.Items = rankedItems
	plan.RawWinnerId = rawWinnerId
	plan.ActualPrimaryId = actualPrimaryId
	sort.Slice(plan.Items, func(i int, j int) bool {
		return plan.Items[i].ChannelId < plan.Items[j].ChannelId
	})
	return plan
}

func channelSmartScheduleCurrentPrimaryId(items []channelSmartSchedulePlanItem) int {
	if len(items) == 0 {
		return 0
	}
	primary := items[0]
	matchingRoutingValues := 1
	for _, item := range items[1:] {
		if item.CurrentPriority > primary.CurrentPriority ||
			(item.CurrentPriority == primary.CurrentPriority && item.CurrentWeight > primary.CurrentWeight) {
			primary = item
			matchingRoutingValues = 1
			continue
		}
		if item.CurrentPriority == primary.CurrentPriority && item.CurrentWeight == primary.CurrentWeight {
			matchingRoutingValues++
		}
	}
	if matchingRoutingValues > 1 {
		return 0
	}
	return primary.ChannelId
}

func channelSmartScheduleEffectivePrimaryId(
	items []channelSmartSchedulePlanItem,
	currentPrimaryId int,
	switchThreshold float64,
	forceReset bool,
) int {
	ranked := channelSmartScheduleRankedItemIndexes(items, currentPrimaryId)
	if len(ranked) == 0 {
		return 0
	}
	rawWinner := items[ranked[0]]
	if forceReset || rawWinner.ChannelId == currentPrimaryId {
		return rawWinner.ChannelId
	}
	currentIndex := -1
	for index := range items {
		if items[index].ChannelId == currentPrimaryId {
			currentIndex = index
			break
		}
	}
	if currentIndex < 0 {
		return rawWinner.ChannelId
	}
	if math.IsNaN(switchThreshold) || math.IsInf(switchThreshold, 0) || switchThreshold < 0 {
		switchThreshold = 0
	}
	if rawWinner.Score-items[currentIndex].Score+channelMonitorRatioEpsilon >= switchThreshold {
		return rawWinner.ChannelId
	}
	return currentPrimaryId
}

func channelSmartScheduleRankedItemIndexes(items []channelSmartSchedulePlanItem, preferredChannelId int) []int {
	ranked := make([]int, len(items))
	for index := range items {
		ranked[index] = index
	}
	sort.Slice(ranked, func(i int, j int) bool {
		left := items[ranked[i]]
		right := items[ranked[j]]
		if math.Abs(left.Score-right.Score) > channelMonitorRatioEpsilon {
			return left.Score > right.Score
		}
		if left.ChannelId == preferredChannelId || right.ChannelId == preferredChannelId {
			return left.ChannelId == preferredChannelId
		}
		if left.PreviousBaseRank > 0 || right.PreviousBaseRank > 0 {
			if left.PreviousBaseRank <= 0 {
				return false
			}
			if right.PreviousBaseRank <= 0 {
				return true
			}
			if left.PreviousBaseRank != right.PreviousBaseRank {
				return left.PreviousBaseRank < right.PreviousBaseRank
			}
		}
		return left.ChannelId < right.ChannelId
	})
	return ranked
}

func channelSmartScheduleAssignPrimaryTrafficWeights(
	items []channelSmartSchedulePlanItem,
	primaryChannelId int,
	primaryTrafficPercent float64,
) {
	primaryIndex := -1
	otherIndexes := make([]int, 0, len(items)-1)
	for index := range items {
		if items[index].ChannelId == primaryChannelId {
			primaryIndex = index
			continue
		}
		otherIndexes = append(otherIndexes, index)
	}
	if primaryIndex < 0 {
		return
	}
	if math.IsNaN(primaryTrafficPercent) || math.IsInf(primaryTrafficPercent, 0) {
		primaryTrafficPercent = 0
	}
	primaryTrafficPercent = min(max(primaryTrafficPercent, 0), channelMonitorScorePercentageTotal)
	primaryWeight := uint(math.Round(
		channelMonitorSmartScheduleAllocationWeightTotal * primaryTrafficPercent / channelMonitorScorePercentageTotal,
	))
	items[primaryIndex].TargetWeight = primaryWeight
	channelSmartScheduleAssignProportionalWeights(
		items,
		otherIndexes,
		uint(channelMonitorSmartScheduleAllocationWeightTotal)-primaryWeight,
	)
}

func channelSmartScheduleAssignPriorityWeightTargets(items []channelSmartSchedulePlanItem, primaryChannelId int) {
	ranked := channelSmartScheduleRankedItemIndexes(items, primaryChannelId)
	if len(ranked) == 0 {
		return
	}
	ordered := make([]int, 0, len(ranked))
	for _, index := range ranked {
		if items[index].ChannelId == primaryChannelId {
			ordered = append(ordered, index)
			break
		}
	}
	for _, index := range ranked {
		if items[index].ChannelId != primaryChannelId {
			ordered = append(ordered, index)
		}
	}
	for rank, index := range ordered {
		items[index].TargetPriority = int64(len(ordered) - rank)
		items[index].TargetWeight = channelMonitorSmartScheduleAllocationWeightTotal
	}
}

func channelSmartScheduleAssignProportionalWeights(
	items []channelSmartSchedulePlanItem,
	indexes []int,
	totalWeight uint,
) {
	if len(indexes) == 0 {
		return
	}
	type remainder struct {
		Index     int
		Fraction  float64
		ChannelId int
	}
	effectiveScores := make([]float64, len(indexes))
	scoreTotal := 0.0
	for position, index := range indexes {
		score := items[index].Score
		if math.IsNaN(score) || math.IsInf(score, 0) || score < channelMonitorSmartScheduleAllocationScoreFloor {
			score = channelMonitorSmartScheduleAllocationScoreFloor
		}
		effectiveScores[position] = score
		scoreTotal += score
	}
	remainders := make([]remainder, 0, len(indexes))
	assignedWeight := uint(0)
	for position, index := range indexes {
		exactWeight := float64(totalWeight) * effectiveScores[position] / scoreTotal
		weight := uint(math.Floor(exactWeight))
		items[index].TargetWeight = weight
		assignedWeight += weight
		remainders = append(remainders, remainder{
			Index: index, Fraction: exactWeight - float64(weight), ChannelId: items[index].ChannelId,
		})
	}
	sort.Slice(remainders, func(i int, j int) bool {
		if math.Abs(remainders[i].Fraction-remainders[j].Fraction) > channelMonitorRatioEpsilon {
			return remainders[i].Fraction > remainders[j].Fraction
		}
		return remainders[i].ChannelId < remainders[j].ChannelId
	})
	remainingWeight := uint(0)
	if assignedWeight < totalWeight {
		remainingWeight = totalWeight - assignedWeight
	}
	for index := uint(0); index < remainingWeight; index++ {
		items[remainders[index%uint(len(remainders))].Index].TargetWeight++
	}
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
		minSamples = channelMonitorSmartScheduleFallbackMinSamples
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
