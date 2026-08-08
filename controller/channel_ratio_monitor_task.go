package controller

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type channelRatioMonitorTaskHandler struct{}

const maxChannelRatioMonitorTaskFailureDetails = 100

type channelRatioMonitorTaskResult struct {
	Total                         int                              `json:"total"`
	Updated                       int                              `json:"updated"`
	Changed                       int                              `json:"changed"`
	BalanceUpdated                int                              `json:"balance_updated"`
	BalanceWarnings               int                              `json:"balance_warnings,omitempty"`
	Skipped                       int                              `json:"skipped,omitempty"`
	Failed                        int                              `json:"failed"`
	GroupsUpdated                 int                              `json:"groups_updated"`
	GroupMembershipsRemoved       int                              `json:"group_memberships_removed"`
	GroupUpdateFailed             bool                             `json:"group_update_failed,omitempty"`
	ChannelsDisabled              int                              `json:"channels_disabled"`
	ChannelsEnabled               int                              `json:"channels_enabled,omitempty"`
	GroupsSkipped                 int                              `json:"groups_skipped"`
	Retried                       int                              `json:"retried,omitempty"`
	RecoveredAfterRetry           int                              `json:"recovered_after_retry,omitempty"`
	Failures                      []channelRatioMonitorTaskFailure `json:"failures,omitempty"`
	FailureDetailsTruncated       bool                             `json:"failure_details_truncated,omitempty"`
	EmailStatus                   string                           `json:"email_status,omitempty"`
	EmailError                    string                           `json:"email_error,omitempty"`
	notificationFailures          []channelRatioMonitorTaskFailure
	notificationFailuresTruncated bool
}

type channelRatioMonitorTaskFailure struct {
	ChannelId     int    `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	ChannelRemark string `json:"-"`
	Error         string `json:"error"`
}

func (result *channelRatioMonitorTaskResult) recordFailure(channelId int, channelName string, channelRemark string, failure error) {
	result.Failed++
	if len(result.Failures) >= maxChannelRatioMonitorTaskFailureDetails {
		result.FailureDetailsTruncated = true
		return
	}

	result.Failures = append(result.Failures, newChannelRatioMonitorTaskFailure(channelId, channelName, channelRemark, failure))
}

func newChannelRatioMonitorTaskFailure(channelId int, channelName string, channelRemark string, failure error) channelRatioMonitorTaskFailure {
	nameRunes := []rune(strings.TrimSpace(channelName))
	if len(nameRunes) > 128 {
		nameRunes = nameRunes[:128]
	}
	remarkRunes := []rune(strings.TrimSpace(channelRemark))
	if len(remarkRunes) > 255 {
		remarkRunes = remarkRunes[:255]
	}
	errorMessage := "上游同步失败"
	if failure != nil && strings.TrimSpace(failure.Error()) != "" {
		errorMessage = strings.TrimSpace(failure.Error())
	}
	errorRunes := []rune(errorMessage)
	if len(errorRunes) > 255 {
		errorMessage = string(errorRunes[:255])
	}
	return channelRatioMonitorTaskFailure{
		ChannelId:     channelId,
		ChannelName:   string(nameRunes),
		ChannelRemark: string(remarkRunes),
		Error:         errorMessage,
	}
}

type channelRatioMonitorFailureNotification struct {
	Detail channelRatioMonitorTaskFailure
	Guard  model.ChannelRatioMonitorFailureAlertGuard
}

func appendChannelRatioMonitorFailureNotification(
	notifications *[]channelRatioMonitorFailureNotification,
	channelId int,
	channelName string,
	channelRemark string,
	failure error,
	failureType string,
	upstreamRevision int64,
	failureCount int,
) bool {
	if len(*notifications) >= maxChannelRatioMonitorTaskFailureDetails {
		return false
	}
	*notifications = append(*notifications, channelRatioMonitorFailureNotification{
		Detail: newChannelRatioMonitorTaskFailure(channelId, channelName, channelRemark, failure),
		Guard: model.ChannelRatioMonitorFailureAlertGuard{
			ChannelId:        channelId,
			UpstreamRevision: upstreamRevision,
			FailureType:      failureType,
			FailureCount:     failureCount,
		},
	})
	return true
}

func channelRatioMonitorFailureAlertState(monitor model.ChannelRatioMonitor, failureType string) (count int, notified bool, failureMessage string, active bool) {
	switch failureType {
	case model.ChannelRatioFailureAlertRatio:
		return monitor.ConsecutiveFailures, monitor.FetchFailureAlertNotified, monitor.LastFetchError,
			monitor.LastFetchStatus == model.ChannelRatioFetchStatusFailed
	case model.ChannelRatioFailureAlertBalance:
		return monitor.BalanceConsecutiveFailures, monitor.BalanceFailureAlertNotified, monitor.LastBalanceError,
			strings.TrimSpace(monitor.LastBalanceError) != ""
	default:
		return 0, false, "", false
	}
}

func channelRatioMonitorFailureAlertReady(monitor model.ChannelRatioMonitor, failureType string, failureLimit int) bool {
	failureCount, notified, _, active := channelRatioMonitorFailureAlertState(monitor, failureType)
	return active && failureCount >= failureLimit && !notified
}

func appendReadyChannelRatioMonitorFailureNotification(
	notifications *[]channelRatioMonitorFailureNotification,
	monitor model.ChannelRatioMonitor,
	channelName string,
	channelRemark string,
	failureType string,
	failureLimit int,
	failure error,
) (ready bool, truncated bool) {
	failureCount, notified, storedFailure, active := channelRatioMonitorFailureAlertState(monitor, failureType)
	if !active || failureCount < failureLimit || notified {
		return false, false
	}
	if failure == nil {
		failure = errors.New(storedFailure)
	}
	if !appendChannelRatioMonitorFailureNotification(
		notifications,
		monitor.ChannelId,
		channelName,
		channelRemark,
		failure,
		failureType,
		monitor.UpstreamRevision,
		failureCount,
	) {
		return true, true
	}
	return true, false
}

type channelRatioMonitorEmailChange struct {
	ChannelId        int
	ChannelName      string
	ChannelRemark    string
	UpstreamType     string
	UpstreamGroup    string
	OldRatio         float64
	NewRatio         float64
	ConversionFactor float64
	OldCostRatio     float64
	NewCostRatio     float64
}

type channelRatioMonitorBalanceWarning struct {
	ChannelId        int
	UpstreamRevision int64
	ChannelName      string
	ChannelRemark    string
	UpstreamType     string
	Balance          float64
	Threshold        float64
}

type channelRatioMonitorDisabledChannel struct {
	ChannelId     int
	ChannelName   string
	ChannelRemark string
	Reason        string
}

type channelRatioMonitorRemovedGroupMembership struct {
	ChannelId     int
	ChannelName   string
	ChannelRemark string
	Group         string
}

func ListChannelMonitorTasks(c *gin.Context) {
	taskType := model.SystemTaskTypeChannelRatioMonitor
	switch c.DefaultQuery("kind", "ratio") {
	case "ratio":
	case "schedule":
		taskType = channelMonitorSmartScheduleTaskType
	default:
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "定时任务类型无效"})
		return
	}
	pageInfo := common.GetPageQuery(c)
	tasks, total, err := model.GetChannelMonitorTasksByType(taskType, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	responses := make([]model.SystemTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		response := task.ToResponse()
		if taskType == channelMonitorSmartScheduleTaskType && strings.TrimSpace(task.Result) != "" {
			var result channelSmartScheduleTaskResult
			if err := common.UnmarshalJsonStr(task.Result, &result); err == nil {
				// Execution details are loaded by the dedicated detail endpoint.
				result.GroupPolicyCount = len(result.GroupPolicies)
				result.GroupPolicies = nil
				result.Adjustments = nil
				response.Result = result
			} else {
				// Do not allow a legacy or malformed result payload to reintroduce
				// an unbounded adjustments array into the paginated list response.
				response.Result = nil
			}
		}
		responses = append(responses, response)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(responses)
	common.ApiSuccess(c, pageInfo)
}

func RunChannelMonitorRatioUpdate(c *gin.Context) {
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeChannelRatioMonitor, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.monitor_ratio_update_run", map[string]interface{}{
		"created": created,
		"task_id": task.TaskID,
	})
	common.ApiSuccess(c, gin.H{
		"created": created,
		"task":    task.ToResponse(),
	})
}

func (channelRatioMonitorTaskHandler) Type() string {
	return model.SystemTaskTypeChannelRatioMonitor
}

func (channelRatioMonitorTaskHandler) Enabled() bool {
	return getChannelMonitorSettings().AutoUpdateIntervalMinutes > 0
}

func (channelRatioMonitorTaskHandler) Interval() time.Duration {
	minutes := getChannelMonitorSettings().AutoUpdateIntervalMinutes
	if minutes <= 0 {
		minutes = 1
	}
	return time.Duration(minutes) * time.Minute
}

func (channelRatioMonitorTaskHandler) NewPayload() any { return nil }

func (channelRatioMonitorTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := runChannelRatioMonitorTaskOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID), common.SendEmail)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

func runChannelRatioMonitorTaskOnce(ctx context.Context, reportProgress func(processed, total int), sendEmail func(subject string, receiver string, content string) error) (summary channelRatioMonitorTaskResult, taskErr error) {
	if reportProgress == nil {
		reportProgress = func(int, int) {}
	}
	settings := getChannelMonitorSettings()
	requestTimeout := settings.upstreamRequestTimeout()
	emailChanges := make([]channelRatioMonitorEmailChange, 0)
	balanceWarnings := make([]channelRatioMonitorBalanceWarning, 0)
	disabledChannels := make([]channelRatioMonitorDisabledChannel, 0)
	removedGroupMemberships := make([]channelRatioMonitorRemovedGroupMembership, 0)
	failureNotifications := make([]channelRatioMonitorFailureNotification, 0)
	failureNotificationsTruncated := false
	channelStatusChanged := false
	economicInputsChanged := false
	defer func() {
		if economicInputsChanged || channelStatusChanged {
			_ = requestChannelSmartScheduleRun(ctx)
		}
	}()
	defer func() {
		if channelStatusChanged {
			model.InitChannelCache()
			service.ResetProxyClientCache()
		}
	}()
	defer func() {
		summary.notificationFailures = make([]channelRatioMonitorTaskFailure, 0, len(failureNotifications))
		for _, notification := range failureNotifications {
			summary.notificationFailures = append(summary.notificationFailures, notification.Detail)
		}
		summary.notificationFailuresTruncated = failureNotificationsTruncated
		shouldNotify :=
			(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeRatioChange) && len(emailChanges) > 0) ||
				(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeBalanceWarning) && len(balanceWarnings) > 0) ||
				(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeChannelDisabled) && len(disabledChannels) > 0) ||
				(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeGroupMembershipRemoved) && len(removedGroupMemberships) > 0) ||
				(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeUpstreamSyncFailed) && len(summary.notificationFailures) > 0) ||
				(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeTaskFailed) && (summary.GroupUpdateFailed || taskErr != nil))
		if !shouldNotify || !settings.EmailNotificationEnabled || settings.NotificationEmail == "" {
			return
		}
		if err := sendChannelRatioMonitorNotificationEmailForTypes(settings.NotificationEmail, settings.EmailNotificationTypes, emailChanges, balanceWarnings, disabledChannels, removedGroupMemberships, summary, taskErr, sendEmail); err != nil {
			summary.EmailStatus = "failed"
			errorMessage := err.Error()
			errorRunes := []rune(errorMessage)
			if len(errorRunes) > 255 {
				errorMessage = string(errorRunes[:255])
			}
			summary.EmailError = errorMessage
			logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: notification email failed: %v", err))
			return
		}
		summary.EmailStatus = "sent"
		if channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeUpstreamSyncFailed) && len(failureNotifications) > 0 {
			guards := make([]model.ChannelRatioMonitorFailureAlertGuard, 0, len(failureNotifications))
			for _, notification := range failureNotifications {
				guards = append(guards, notification.Guard)
			}
			if err := model.MarkChannelRatioMonitorFailureAlertsNotified(guards); err != nil {
				if taskErr == nil {
					taskErr = fmt.Errorf("记录上游同步失败通知状态失败: %w", err)
				} else {
					taskErr = fmt.Errorf("%w（记录上游同步失败通知状态失败：%v）", taskErr, err)
				}
				logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: failure alert state update failed: %v", err))
			}
		}
		if !channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeBalanceWarning) || len(balanceWarnings) == 0 {
			return
		}
		alertGuards := make([]model.ChannelRatioMonitorBalanceAlertGuard, 0, len(balanceWarnings))
		for _, warning := range balanceWarnings {
			alertGuards = append(alertGuards, model.ChannelRatioMonitorBalanceAlertGuard{
				ChannelId:        warning.ChannelId,
				UpstreamRevision: warning.UpstreamRevision,
				WarningThreshold: warning.Threshold,
			})
		}
		if err := model.MarkChannelRatioMonitorBalanceAlertsNotified(alertGuards); err != nil {
			if taskErr == nil {
				taskErr = fmt.Errorf("记录余额预警通知状态失败: %w", err)
			} else {
				taskErr = fmt.Errorf("%w（记录余额预警通知状态失败：%v）", taskErr, err)
			}
			logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: balance alert state update failed: %v", err))
		}
	}()

	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		return summary, err
	}

	configured := make([]model.ChannelRatioMonitor, 0, len(monitors))
	for _, monitor := range monitors {
		if monitor.UpstreamType == service.NewAPIUpstreamType || monitor.UpstreamType == service.Sub2APIUpstreamType || monitor.UpstreamType == service.CustomUpstreamType {
			configured = append(configured, monitor)
		}
	}
	summary = channelRatioMonitorTaskResult{Total: len(configured)}
	policyInputs := make(map[int]channelMonitorPolicyInput, len(configured))
	balanceRecoveryInputs := make(map[int]channelMonitorPolicyInput, len(configured))
	for index, monitor := range configured {
		select {
		case <-ctx.Done():
			return summary, ctx.Err()
		default:
		}
		if monitor.UpstreamRatioSyncDisabled && monitor.UpstreamBalanceSyncDisabled {
			summary.Skipped++
			reportProgress(index+1, summary.Total)
			continue
		}
		ratioAutoFetchEnabled := !monitor.UpstreamRatioSyncDisabled &&
			monitor.ConsecutiveFailures < settings.AutoUpdateConsecutiveFailureLimit
		balanceAutoFetchEnabled := !monitor.UpstreamBalanceSyncDisabled &&
			monitor.BalanceConsecutiveFailures < settings.AutoUpdateConsecutiveFailureLimit
		pendingChannelName := ""
		pendingChannelRemark := ""
		pendingRatioFailure := !ratioAutoFetchEnabled && channelRatioMonitorFailureAlertReady(
			monitor, model.ChannelRatioFailureAlertRatio, settings.AutoUpdateConsecutiveFailureLimit,
		)
		pendingBalanceFailure := !balanceAutoFetchEnabled && channelRatioMonitorFailureAlertReady(
			monitor, model.ChannelRatioFailureAlertBalance, settings.AutoUpdateConsecutiveFailureLimit,
		)
		if pendingRatioFailure || pendingBalanceFailure {
			if pendingChannel, lookupErr := model.GetChannelById(monitor.ChannelId, true); lookupErr == nil {
				pendingChannelName = pendingChannel.Name
				if pendingChannel.Remark != nil {
					pendingChannelRemark = strings.TrimSpace(*pendingChannel.Remark)
				}
			}
		}
		if !ratioAutoFetchEnabled {
			_, truncated := appendReadyChannelRatioMonitorFailureNotification(
				&failureNotifications, monitor, pendingChannelName, pendingChannelRemark, model.ChannelRatioFailureAlertRatio,
				settings.AutoUpdateConsecutiveFailureLimit, nil,
			)
			failureNotificationsTruncated = failureNotificationsTruncated || truncated
		}
		if !balanceAutoFetchEnabled {
			_, truncated := appendReadyChannelRatioMonitorFailureNotification(
				&failureNotifications, monitor, pendingChannelName, pendingChannelRemark, model.ChannelRatioFailureAlertBalance,
				settings.AutoUpdateConsecutiveFailureLimit, nil,
			)
			failureNotificationsTruncated = failureNotificationsTruncated || truncated
		}
		if !ratioAutoFetchEnabled && !balanceAutoFetchEnabled {
			summary.Skipped++
			reportProgress(index+1, summary.Total)
			continue
		}
		fetchRatio := ratioAutoFetchEnabled

		channel, err := model.GetChannelById(monitor.ChannelId, true)
		if err != nil {
			summary.recordFailure(monitor.ChannelId, "", "", err)
			applied, statusErr := model.RecordChannelRatioMonitorFetchFailureIfRevision(
				monitor.ChannelId,
				monitor.UpstreamRevision,
				err.Error(),
			)
			if statusErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d failure status update failed: %v", monitor.ChannelId, statusErr))
			} else if applied {
				if latestMonitor, stateErr := model.GetChannelRatioMonitor(monitor.ChannelId); stateErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d failure alert state lookup failed: %v", monitor.ChannelId, stateErr))
				} else {
					_, truncated := appendReadyChannelRatioMonitorFailureNotification(
						&failureNotifications, latestMonitor, "", "", model.ChannelRatioFailureAlertRatio,
						settings.AutoUpdateConsecutiveFailureLimit, err,
					)
					failureNotificationsTruncated = failureNotificationsTruncated || truncated
				}
			}
			logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d lookup failed: %v", monitor.ChannelId, err))
			reportProgress(index+1, summary.Total)
			continue
		}
		channelRemark := ""
		if channel.Remark != nil {
			channelRemark = strings.TrimSpace(*channel.Remark)
		}

		var outcome channelMonitorFetchOutcome
		var recordedBalance *float64
		var balanceEvaluation *channelMonitorBalanceEvaluation
		var effectiveBalanceForRecovery *float64
		balanceBelowAutoDisableThreshold := false
		ratioUpdated := false
		syncSkipped := false
		retriesUsed := 0
		for attempt := 0; attempt <= settings.AutoUpdateRetryCount; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return summary, ctx.Err()
				default:
				}

				refreshedMonitor, refreshErr := model.GetChannelRatioMonitor(monitor.ChannelId)
				if refreshErr != nil {
					err = fmt.Errorf("重试前重新读取上游配置失败: %w", refreshErr)
					break
				}
				monitor = refreshedMonitor
				if fetchRatio {
					if monitor.UpstreamRatioSyncDisabled {
						syncSkipped = true
						err = nil
						break
					}
					if monitor.ConsecutiveFailures >= settings.AutoUpdateConsecutiveFailureLimit {
						break
					}
				} else {
					if monitor.UpstreamBalanceSyncDisabled {
						syncSkipped = true
						err = nil
						break
					}
					if monitor.BalanceConsecutiveFailures >= settings.AutoUpdateConsecutiveFailureLimit {
						break
					}
				}
				retriesUsed++
				summary.Retried++
			}

			ratioUpdated = false
			if fetchRatio {
				fetchMonitor := monitor
				if fetchMonitor.BalanceConsecutiveFailures >= settings.AutoUpdateConsecutiveFailureLimit {
					fetchMonitor.UpstreamBalanceSyncDisabled = true
				}
				outcome, err = fetchAndRecordChannelMonitorUpstreamRatio(ctx, fetchMonitor, channel.GetKeys(), channel.GetSetting().Proxy, requestTimeout, true, 0, "系统自动更新")
				ratioUpdated = err == nil
				if outcome.BalanceRecorded && outcome.Result.Balance.Amount != nil {
					balance := *outcome.Result.Balance.Amount
					recordedBalance = &balance
					balanceEvaluation = outcome.BalanceEvaluation
				}
			} else {
				var balanceResult service.ChannelMonitorUpstreamBalanceResult
				var fetchedEvaluation *channelMonitorBalanceEvaluation
				balanceResult, fetchedEvaluation, err = fetchAndRecordChannelMonitorUpstreamBalance(ctx, monitor, channel.GetKeys(), channel.GetSetting().Proxy, requestTimeout)
				if balanceResult.Amount != nil {
					balance := *balanceResult.Amount
					recordedBalance = &balance
					balanceEvaluation = fetchedEvaluation
				}
			}
			if err == nil ||
				attempt == settings.AutoUpdateRetryCount ||
				errors.Is(err, service.ErrChannelMonitorUpstreamAuthentication) {
				break
			}
			logger.LogWarn(ctx, fmt.Sprintf(
				"channel ratio monitor: channel_id=%d attempt=%d failed: %v",
				monitor.ChannelId,
				attempt+1,
				err,
			))
		}
		if syncSkipped {
			summary.Skipped++
			reportProgress(index+1, summary.Total)
			continue
		}
		if errors.Is(err, model.ErrChannelRatioMonitorConfigChanged) {
			summary.Skipped++
			reportProgress(index+1, summary.Total)
			continue
		}
		upstreamSyncFailed := err != nil
		if recordedBalance != nil {
			balance := *recordedBalance
			effectiveBalance := balance
			estimatedConsumption := 0.0
			if balanceEvaluation != nil {
				effectiveBalance = balanceEvaluation.EffectiveBalance
				estimatedConsumption = balanceEvaluation.EstimatedConsumption
			}
			effectiveBalanceForRecovery = &effectiveBalance
			balanceBelowAutoDisableThreshold = monitor.BalanceAutoDisableThreshold != nil &&
				effectiveBalance < *monitor.BalanceAutoDisableThreshold
			summary.BalanceUpdated++
			balanceAutoDisabled, disableErr := autoDisableChannelMonitorAtEffectiveBalance(
				monitor,
				channel,
				balance,
				effectiveBalance,
				estimatedConsumption,
			)
			if disableErr != nil {
				if err == nil {
					err = disableErr
				} else {
					err = fmt.Errorf("%w（余额自动禁用失败：%v）", err, disableErr)
				}
			}
			if balanceAutoDisabled {
				summary.ChannelsDisabled++
				channelStatusChanged = true
			}
			if monitor.BalanceWarningThreshold != nil &&
				balance < *monitor.BalanceWarningThreshold &&
				!monitor.BalanceAlertNotified {
				summary.BalanceWarnings++
				balanceWarnings = append(balanceWarnings, channelRatioMonitorBalanceWarning{
					ChannelId:        monitor.ChannelId,
					UpstreamRevision: monitor.UpstreamRevision,
					ChannelName:      channel.Name,
					ChannelRemark:    channelRemark,
					UpstreamType:     monitor.UpstreamType,
					Balance:          balance,
					Threshold:        *monitor.BalanceWarningThreshold,
				})
			}
		}
		if err != nil {
			failureErr := err
			if retriesUsed > 0 {
				failureErr = fmt.Errorf("重试 %d 次后仍失败: %w", retriesUsed, err)
			}
			summary.recordFailure(monitor.ChannelId, channel.Name, channelRemark, failureErr)
			if upstreamSyncFailed {
				latestMonitor, stateErr := model.GetChannelRatioMonitor(monitor.ChannelId)
				if stateErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d failure alert state lookup failed: %v", monitor.ChannelId, stateErr))
				} else {
					failureType := model.ChannelRatioFailureAlertBalance
					if fetchRatio {
						failureType = model.ChannelRatioFailureAlertRatio
					}
					_, truncated := appendReadyChannelRatioMonitorFailureNotification(
						&failureNotifications, latestMonitor, channel.Name, channelRemark,
						failureType, settings.AutoUpdateConsecutiveFailureLimit, failureErr,
					)
					failureNotificationsTruncated = failureNotificationsTruncated || truncated
				}
			}
			if settings.AutoDisableOnUpdateFailure && channel.Status == common.ChannelStatusEnabled {
				disabled, revisionCurrent, _, disableErr := model.UpdateChannelMonitorStatusIfSnapshotRevision(
					channel.Id,
					monitor.UpstreamRevision,
					model.CaptureChannelMonitorStatus(channel),
					common.ChannelStatusAutoDisabled,
					channelMonitorUpdateFailureDisableReason,
				)
				if disableErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d automatic disable failed: %v", channel.Id, disableErr))
				}
				if !revisionCurrent {
					disabled = false
				}
				if disabled {
					summary.ChannelsDisabled++
					channelStatusChanged = true
					disabledChannels = append(disabledChannels, channelRatioMonitorDisabledChannel{
						ChannelId:     channel.Id,
						ChannelName:   channel.Name,
						ChannelRemark: channelRemark,
						Reason:        "上游倍率或余额更新失败",
					})
				}
			}
			logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d update failed: %v", monitor.ChannelId, failureErr))
		} else {
			summary.Updated++
			if retriesUsed > 0 {
				summary.RecoveredAfterRetry++
			}
			syncRecovered := (monitor.UpstreamRatioSyncDisabled || ratioUpdated) &&
				(monitor.UpstreamBalanceSyncDisabled || recordedBalance != nil)
			if syncRecovered && channelMonitorUpdateFailureRecovered(monitor, channel, effectiveBalanceForRecovery) {
				recoveryChannel, recoveryErr := model.GetChannelById(channel.Id, true)
				if recoveryErr != nil {
					logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d recovery status lookup failed: %v", channel.Id, recoveryErr))
				} else if channelMonitorUpdateFailureRecovered(monitor, recoveryChannel, effectiveBalanceForRecovery) {
					enabled, revisionCurrent, _, enableErr := model.UpdateChannelMonitorStatusIfSnapshotRevision(
						channel.Id,
						monitor.UpstreamRevision,
						model.CaptureChannelMonitorStatus(recoveryChannel),
						common.ChannelStatusEnabled,
						"",
					)
					if enableErr != nil {
						logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d automatic recovery failed: %v", channel.Id, enableErr))
					}
					if !revisionCurrent {
						enabled = false
					}
					if enabled {
						channel.Status = common.ChannelStatusEnabled
						summary.ChannelsEnabled++
						channelStatusChanged = true
					}
				}
			}
			if ratioUpdated {
				economicInputsChanged = true
				policyInputs[monitor.ChannelId] = channelMonitorPolicyInput{
					UpstreamRevision:                 monitor.UpstreamRevision,
					CostRatio:                        outcome.Result.CostRatio,
					BalanceBelowAutoDisableThreshold: balanceBelowAutoDisableThreshold,
					SingleChannelAction:              monitor.SingleChannelAction,
					MultipleChannelsAction:           monitor.MultipleChannelsAction,
				}
				if outcome.Changed {
					summary.Changed++
					emailChanges = append(emailChanges, channelRatioMonitorEmailChange{
						ChannelId:        monitor.ChannelId,
						ChannelName:      channel.Name,
						ChannelRemark:    channelRemark,
						UpstreamType:     monitor.UpstreamType,
						UpstreamGroup:    monitor.UpstreamGroup,
						OldRatio:         monitor.Ratio,
						NewRatio:         outcome.Result.Ratio,
						ConversionFactor: outcome.Result.ConversionFactor,
						OldCostRatio:     monitor.Ratio * outcome.Result.ConversionFactor,
						NewCostRatio:     outcome.Result.CostRatio,
					})
				}
			}
			if settings.AutoEnableOnBalanceRecovery && recordedBalance != nil {
				costRatio := 0.0
				costRatioAvailable := false
				if ratioUpdated {
					costRatio = outcome.Result.CostRatio
					costRatioAvailable = validateChannelMonitorRatio(&costRatio)
				} else if monitor.UpdatedTime > 0 &&
					(monitor.UpstreamRatioSyncDisabled || monitor.ConsecutiveFailures < settings.AutoUpdateConsecutiveFailureLimit) {
					storedCostRatio, _, conversionErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
					if conversionErr != nil {
						logger.LogWarn(ctx, fmt.Sprintf(
							"channel ratio monitor: channel_id=%d balance recovery cost ratio calculation failed: %v",
							monitor.ChannelId,
							conversionErr,
						))
					} else {
						costRatio = storedCostRatio
						costRatioAvailable = true
					}
				}
				if costRatioAvailable {
					balanceRecoveryInputs[monitor.ChannelId] = channelMonitorPolicyInput{
						UpstreamRevision:                 monitor.UpstreamRevision,
						CostRatio:                        costRatio,
						BalanceBelowAutoDisableThreshold: balanceBelowAutoDisableThreshold,
					}
				}
			}
		}
		reportProgress(index+1, summary.Total)
	}
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		return summary, err
	}
	groupCoefficients := getChannelMonitorGroupCoefficients()
	groupRatios := ratio_setting.GetGroupRatioCopy()
	if settings.AutoEnableOnBalanceRecovery {
		enabledChannelIds, recoveryErr := autoEnableChannelsAfterBalanceRecovery(
			ctx,
			channels,
			balanceRecoveryInputs,
			groupRatios,
			groupCoefficients,
		)
		if recoveryErr != nil {
			return summary, recoveryErr
		}
		if len(enabledChannelIds) > 0 {
			summary.ChannelsEnabled += len(enabledChannelIds)
			channelStatusChanged = true
		}
	}
	if settings.AutoEnableOnCostRatioRecovery {
		enabledChannelIds, recoveryErr := autoEnableChannelsAfterCostRatioRecovery(
			ctx,
			channels,
			policyInputs,
			groupRatios,
			groupCoefficients,
		)
		if recoveryErr != nil {
			return summary, recoveryErr
		}
		if len(enabledChannelIds) > 0 {
			summary.ChannelsEnabled += len(enabledChannelIds)
			channelStatusChanged = true
		}
	}
	plan := planChannelMonitorPolicyActions(
		channels,
		policyInputs,
		groupRatios,
		groupCoefficients,
	)
	summary.GroupsSkipped = plan.SkippedGroupCount
	groupsUpdated, removedMemberships, disabledChannelIds, groupUpdateFailed, err := applyChannelMonitorPolicyPlan(ctx, plan)
	summary.GroupsUpdated = groupsUpdated
	summary.GroupMembershipsRemoved = len(removedMemberships)
	summary.ChannelsDisabled += len(disabledChannelIds)
	summary.GroupUpdateFailed = groupUpdateFailed
	if err != nil {
		return summary, err
	}
	if groupsUpdated > 0 || len(removedMemberships) > 0 {
		economicInputsChanged = true
	}
	if len(removedMemberships) > 0 || len(disabledChannelIds) > 0 {
		channelNames := make(map[int]string, len(channels))
		channelRemarks := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelNames[channel.Id] = channel.Name
			if channel.Remark != nil {
				channelRemarks[channel.Id] = strings.TrimSpace(*channel.Remark)
			}
		}
		for _, removal := range removedMemberships {
			removedGroupMemberships = append(removedGroupMemberships, channelRatioMonitorRemovedGroupMembership{
				ChannelId:     removal.ChannelId,
				ChannelName:   channelNames[removal.ChannelId],
				ChannelRemark: channelRemarks[removal.ChannelId],
				Group:         removal.Group,
			})
		}
		for _, channelId := range disabledChannelIds {
			disabledChannels = append(disabledChannels, channelRatioMonitorDisabledChannel{
				ChannelId:     channelId,
				ChannelName:   channelNames[channelId],
				ChannelRemark: channelRemarks[channelId],
				Reason:        "成本倍率高于分组倍率",
			})
		}
	}
	return summary, nil
}

func channelRatioMonitorEmailRemark(remark string) string {
	remark = strings.TrimSpace(remark)
	if remark == "" {
		return "-"
	}
	return html.EscapeString(remark)
}

func sendChannelRatioMonitorNotificationEmail(receiver string, changes []channelRatioMonitorEmailChange, balanceWarnings []channelRatioMonitorBalanceWarning, disabledChannels []channelRatioMonitorDisabledChannel, removedGroupMemberships []channelRatioMonitorRemovedGroupMembership, summary channelRatioMonitorTaskResult, taskErr error, sendEmail func(subject string, receiver string, content string) error) error {
	return sendChannelRatioMonitorNotificationEmailForTypes(
		receiver, defaultChannelMonitorEmailNotificationTypes(), changes, balanceWarnings,
		disabledChannels, removedGroupMemberships, summary, taskErr, sendEmail,
	)
}

func sendChannelRatioMonitorNotificationEmailForTypes(receiver string, notificationTypes []string, changes []channelRatioMonitorEmailChange, balanceWarnings []channelRatioMonitorBalanceWarning, disabledChannels []channelRatioMonitorDisabledChannel, removedGroupMemberships []channelRatioMonitorRemovedGroupMembership, summary channelRatioMonitorTaskResult, taskErr error, sendEmail func(subject string, receiver string, content string) error) error {
	if sendEmail == nil {
		return fmt.Errorf("邮件发送器未初始化")
	}
	subject, content := buildChannelRatioMonitorNotificationEmail(
		notificationTypes, changes, balanceWarnings, disabledChannels,
		removedGroupMemberships, summary, taskErr,
	)
	return sendEmail(subject, receiver, content)
}

func channelRatioMonitorNotificationFailureDetails(summary channelRatioMonitorTaskResult) ([]channelRatioMonitorTaskFailure, bool) {
	if summary.notificationFailures != nil {
		return summary.notificationFailures, summary.notificationFailuresTruncated
	}
	return summary.Failures, summary.FailureDetailsTruncated
}

func buildChannelRatioMonitorNotificationEmail(notificationTypes []string, changes []channelRatioMonitorEmailChange, balanceWarnings []channelRatioMonitorBalanceWarning, disabledChannels []channelRatioMonitorDisabledChannel, removedGroupMemberships []channelRatioMonitorRemovedGroupMembership, summary channelRatioMonitorTaskResult, taskErr error) (string, string) {
	failureDetails, failureDetailsTruncated := channelRatioMonitorNotificationFailureDetails(summary)
	includeChanges := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeRatioChange) && len(changes) > 0
	includeBalanceWarnings := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeBalanceWarning) && len(balanceWarnings) > 0
	includeDisabledChannels := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeChannelDisabled) && len(disabledChannels) > 0
	includeRemovedGroupMemberships := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeGroupMembershipRemoved) && len(removedGroupMemberships) > 0
	includeUpstreamSyncFailures := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeUpstreamSyncFailed) && len(failureDetails) > 0
	includeTaskFailure := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeTaskFailed) && (summary.GroupUpdateFailed || taskErr != nil)

	var content strings.Builder
	content.WriteString(`<!doctype html><html><head><meta charset="UTF-8"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light"></head><body style="margin:0;background:#ffffff;color:#111827;font-family:Arial,'Microsoft YaHei',sans-serif;font-size:14px;line-height:1.5"><div style="padding:16px">`)
	content.WriteString("<p>渠道监控定时更新检测到以下变化或异常：</p>")
	if includeChanges {
		content.WriteString("<h3>渠道倍率变更</h3>")
		content.WriteString("<table style=\"border-collapse:collapse\"><thead><tr>")
		for _, heading := range []string{"渠道", "备注", "上游类型", "上游分组", "原上游倍率", "新上游倍率", "换算系数", "原成本倍率", "新成本倍率"} {
			fmt.Fprintf(&content, "<th style=\"border:1px solid #ddd;padding:6px 10px;text-align:left\">%s</th>", heading)
		}
		content.WriteString("</tr></thead><tbody>")
		for _, change := range changes {
			upstreamType := channelMonitorUpstreamTypeLabel(change.UpstreamType)
			fmt.Fprintf(
				&content,
				"<tr><td style=\"border:1px solid #ddd;padding:6px 10px\">%s（ID: %d）</td><td style=\"border:1px solid #ddd;padding:6px 10px;white-space:pre-wrap\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td></tr>",
				html.EscapeString(change.ChannelName),
				change.ChannelId,
				channelRatioMonitorEmailRemark(change.ChannelRemark),
				html.EscapeString(upstreamType),
				html.EscapeString(change.UpstreamGroup),
				strconv.FormatFloat(change.OldRatio, 'f', -1, 64),
				strconv.FormatFloat(change.NewRatio, 'f', -1, 64),
				strconv.FormatFloat(change.ConversionFactor, 'f', -1, 64),
				strconv.FormatFloat(change.OldCostRatio, 'f', -1, 64),
				strconv.FormatFloat(change.NewCostRatio, 'f', -1, 64),
			)
		}
		content.WriteString("</tbody></table>")
	}
	if includeBalanceWarnings {
		content.WriteString("<h3>上游余额预警</h3>")
		content.WriteString("<table style=\"border-collapse:collapse\"><thead><tr>")
		for _, heading := range []string{"渠道", "备注", "上游类型", "当前余额", "预警值"} {
			fmt.Fprintf(&content, "<th style=\"border:1px solid #ddd;padding:6px 10px;text-align:left\">%s</th>", heading)
		}
		content.WriteString("</tr></thead><tbody>")
		for _, warning := range balanceWarnings {
			upstreamType := channelMonitorUpstreamTypeLabel(warning.UpstreamType)
			fmt.Fprintf(
				&content,
				"<tr><td style=\"border:1px solid #ddd;padding:6px 10px\">%s（ID: %d）</td><td style=\"border:1px solid #ddd;padding:6px 10px;white-space:pre-wrap\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td></tr>",
				html.EscapeString(warning.ChannelName),
				warning.ChannelId,
				channelRatioMonitorEmailRemark(warning.ChannelRemark),
				html.EscapeString(upstreamType),
				strconv.FormatFloat(warning.Balance, 'f', -1, 64),
				strconv.FormatFloat(warning.Threshold, 'f', -1, 64),
			)
		}
		content.WriteString("</tbody></table>")
	}
	if includeDisabledChannels {
		content.WriteString("<h3>渠道自动禁用</h3>")
		content.WriteString("<p>本次更新已自动禁用以下渠道：</p>")
		content.WriteString("<table style=\"border-collapse:collapse\"><thead><tr>")
		for _, heading := range []string{"渠道", "备注", "禁用原因"} {
			fmt.Fprintf(&content, "<th style=\"border:1px solid #ddd;padding:6px 10px;text-align:left\">%s</th>", heading)
		}
		content.WriteString("</tr></thead><tbody>")
		for _, disabledChannel := range disabledChannels {
			channelName := fmt.Sprintf("渠道 ID %d", disabledChannel.ChannelId)
			if disabledChannel.ChannelName != "" {
				channelName = fmt.Sprintf("%s（ID: %d）", disabledChannel.ChannelName, disabledChannel.ChannelId)
			}
			fmt.Fprintf(
				&content,
				"<tr><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px;white-space:pre-wrap\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td></tr>",
				html.EscapeString(channelName),
				channelRatioMonitorEmailRemark(disabledChannel.ChannelRemark),
				html.EscapeString(disabledChannel.Reason),
			)
		}
		content.WriteString("</tbody></table>")
	}
	if includeRemovedGroupMemberships {
		content.WriteString("<h3>渠道移出分组</h3>")
		content.WriteString("<p>本次更新已解除以下渠道与分组的关联：</p>")
		content.WriteString("<table style=\"border-collapse:collapse\"><thead><tr>")
		for _, heading := range []string{"渠道", "备注", "移出分组"} {
			fmt.Fprintf(&content, "<th style=\"border:1px solid #ddd;padding:6px 10px;text-align:left\">%s</th>", heading)
		}
		content.WriteString("</tr></thead><tbody>")
		for _, removal := range removedGroupMemberships {
			channelName := fmt.Sprintf("渠道 ID %d", removal.ChannelId)
			if removal.ChannelName != "" {
				channelName = fmt.Sprintf("%s（ID: %d）", removal.ChannelName, removal.ChannelId)
			}
			fmt.Fprintf(
				&content,
				"<tr><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px;white-space:pre-wrap\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td></tr>",
				html.EscapeString(channelName),
				channelRatioMonitorEmailRemark(removal.ChannelRemark),
				html.EscapeString(removal.Group),
			)
		}
		content.WriteString("</tbody></table>")
	}

	if includeUpstreamSyncFailures {
		content.WriteString("<h3>上游同步失败</h3>")
		fmt.Fprintf(&content, "<p>共 %d 个渠道在重试后仍未更新成功。</p>", len(failureDetails))
		if len(failureDetails) > 0 {
			content.WriteString("<table style=\"border-collapse:collapse\"><thead><tr>")
			for _, heading := range []string{"渠道", "备注", "失败原因"} {
				fmt.Fprintf(&content, "<th style=\"border:1px solid #ddd;padding:6px 10px;text-align:left\">%s</th>", heading)
			}
			content.WriteString("</tr></thead><tbody>")
			for _, failure := range failureDetails {
				channelName := fmt.Sprintf("渠道 ID %d", failure.ChannelId)
				if failure.ChannelName != "" {
					channelName = fmt.Sprintf("%s（ID: %d）", failure.ChannelName, failure.ChannelId)
				}
				fmt.Fprintf(
					&content,
					"<tr><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px;white-space:pre-wrap\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td></tr>",
					html.EscapeString(channelName),
					channelRatioMonitorEmailRemark(failure.ChannelRemark),
					html.EscapeString(failure.Error),
				)
			}
			content.WriteString("</tbody></table>")
		}
		if failureDetailsTruncated {
			fmt.Fprintf(&content, "<p>失败渠道较多，邮件仅展示前 %d 条明细。</p>", len(failureDetails))
		}
	}

	if includeTaskFailure && summary.GroupUpdateFailed {
		content.WriteString("<h3>分组倍率更新失败</h3>")
		content.WriteString("<p>自动写入分组倍率失败，请检查定时更新记录和服务日志。</p>")
		if taskErr != nil {
			fmt.Fprintf(&content, "<p>失败原因：%s</p>", html.EscapeString(taskErr.Error()))
		}
	} else if includeTaskFailure && taskErr != nil {
		content.WriteString("<h3>定时更新任务失败</h3>")
		fmt.Fprintf(&content, "<p>失败原因：%s</p>", html.EscapeString(taskErr.Error()))
	}

	failureCount := 0
	if includeUpstreamSyncFailures {
		failureCount = len(failureDetails)
	}
	if includeTaskFailure && summary.GroupUpdateFailed {
		failureCount++
	} else if includeTaskFailure && taskErr != nil {
		failureCount++
	}
	changeCount := 0
	if includeChanges {
		changeCount = len(changes)
	}
	balanceWarningCount := 0
	if includeBalanceWarnings {
		balanceWarningCount = len(balanceWarnings)
	}
	disabledChannelCount := 0
	if includeDisabledChannels {
		disabledChannelCount = len(disabledChannels)
	}
	removedGroupMembershipCount := 0
	if includeRemovedGroupMemberships {
		removedGroupMembershipCount = len(removedGroupMemberships)
	}
	subject := fmt.Sprintf("渠道监控：%d 个渠道的上游倍率发生变化", changeCount)
	if balanceWarningCount > 0 || disabledChannelCount > 0 || removedGroupMembershipCount > 0 {
		parts := make([]string, 0, 5)
		if changeCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个倍率变更", changeCount))
		}
		if balanceWarningCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个余额预警", balanceWarningCount))
		}
		if disabledChannelCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个渠道自动禁用", disabledChannelCount))
		}
		if removedGroupMembershipCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 个渠道移出分组", removedGroupMembershipCount))
		}
		if failureCount > 0 {
			parts = append(parts, fmt.Sprintf("%d 项更新失败", failureCount))
		}
		subject = "渠道监控：" + strings.Join(parts, "，")
	} else if changeCount > 0 && failureCount > 0 {
		subject = fmt.Sprintf("渠道监控：%d 个倍率变更，%d 项更新失败", changeCount, failureCount)
	} else if failureCount > 0 {
		subject = fmt.Sprintf("渠道监控：%d 项更新失败", failureCount)
	}
	content.WriteString("</div></body></html>")
	return subject, content.String()
}
