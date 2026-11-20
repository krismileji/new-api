package controller

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type channelRatioMonitorTaskHandler struct{}

var channelRatioMonitorNotificationMu sync.Mutex

const maxChannelRatioMonitorTaskFailureDetails = 100
const maxChannelRatioMonitorTaskOutcomeDetails = 100

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
	ChangedChannels               []channelRatioMonitorTaskChange  `json:"changed_channels,omitempty"`
	ChangedDetailsTruncated       bool                             `json:"changed_details_truncated,omitempty"`
	BalanceUpdates                []channelRatioMonitorTaskBalance `json:"balance_updates,omitempty"`
	BalanceDetailsTruncated       bool                             `json:"balance_details_truncated,omitempty"`
	SkippedChannels               []channelRatioMonitorTaskSkipped `json:"skipped_channels,omitempty"`
	SkippedDetailsTruncated       bool                             `json:"skipped_details_truncated,omitempty"`
	Failures                      []channelRatioMonitorTaskFailure `json:"failures,omitempty"`
	FailureDetailsTruncated       bool                             `json:"failure_details_truncated,omitempty"`
	TaskFailureRounds             int                              `json:"task_failure_rounds,omitempty"`
	TaskFailureNotified           bool                             `json:"task_failure_notified,omitempty"`
	TaskFailureMailID             int64                            `json:"task_failure_mail_id,omitempty"`
	EmailStatus                   string                           `json:"email_status,omitempty"`
	EmailError                    string                           `json:"email_error,omitempty"`
	notificationTiming            channelRatioMonitorTaskTiming
	notificationFailures          []channelRatioMonitorTaskFailure
	notificationFailuresTruncated bool
	deliverNotification           func(*channelRatioMonitorTaskResult, func(string, string, string) error)
}

type channelRatioMonitorTaskTiming struct {
	startedAt time.Time
	deadline  time.Time
	elapsed   time.Duration
}

type channelRatioMonitorTaskChange struct {
	ChannelId     int     `json:"channel_id"`
	ChannelName   string  `json:"channel_name"`
	ChannelRemark string  `json:"channel_remark,omitempty"`
	OldRatio      float64 `json:"old_ratio"`
	NewRatio      float64 `json:"new_ratio"`
	OldCostRatio  float64 `json:"old_cost_ratio"`
	NewCostRatio  float64 `json:"new_cost_ratio"`
}

type channelRatioMonitorTaskBalance struct {
	ChannelId        int      `json:"channel_id"`
	ChannelName      string   `json:"channel_name"`
	ChannelRemark    string   `json:"channel_remark,omitempty"`
	PreviousBalance  *float64 `json:"previous_balance,omitempty"`
	Balance          float64  `json:"balance"`
	Warning          bool     `json:"warning,omitempty"`
	WarningThreshold *float64 `json:"warning_threshold,omitempty"`
}

type channelRatioMonitorTaskSkipped struct {
	ChannelId     int    `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	ChannelRemark string `json:"channel_remark,omitempty"`
	Reason        string `json:"reason"`
}

type channelRatioMonitorTaskFailure struct {
	ChannelId     int    `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	ChannelRemark string `json:"channel_remark,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Error         string `json:"error"`
}

func (result *channelRatioMonitorTaskResult) recordFailure(channelId int, channelName string, channelRemark string, failureType string, failure error) {
	result.Failed++
	if len(result.Failures) >= maxChannelRatioMonitorTaskFailureDetails {
		result.FailureDetailsTruncated = true
		return
	}

	result.Failures = append(result.Failures, newChannelRatioMonitorTaskFailure(channelId, channelName, channelRemark, failureType, failure))
}

func newChannelRatioMonitorTaskFailure(channelId int, channelName string, channelRemark string, failureType string, failure error) channelRatioMonitorTaskFailure {
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
		Kind:          failureType,
		Error:         errorMessage,
	}
}

func (result *channelRatioMonitorTaskResult) recordChangedChannel(change channelRatioMonitorTaskChange) {
	if len(result.ChangedChannels) >= maxChannelRatioMonitorTaskOutcomeDetails {
		result.ChangedDetailsTruncated = true
		return
	}
	result.ChangedChannels = append(result.ChangedChannels, change)
}

func (result *channelRatioMonitorTaskResult) recordBalanceUpdate(update channelRatioMonitorTaskBalance) {
	if len(result.BalanceUpdates) >= maxChannelRatioMonitorTaskOutcomeDetails {
		result.BalanceDetailsTruncated = true
		return
	}
	result.BalanceUpdates = append(result.BalanceUpdates, update)
}

func (result *channelRatioMonitorTaskResult) recordSkippedChannel(skipped channelRatioMonitorTaskSkipped) {
	if len(result.SkippedChannels) >= maxChannelRatioMonitorTaskOutcomeDetails {
		result.SkippedDetailsTruncated = true
		return
	}
	result.SkippedChannels = append(result.SkippedChannels, skipped)
}

func (result *channelRatioMonitorTaskResult) sortOutcomeDetails() {
	sort.Slice(result.ChangedChannels, func(i, j int) bool {
		return result.ChangedChannels[i].ChannelId < result.ChangedChannels[j].ChannelId
	})
	sort.Slice(result.BalanceUpdates, func(i, j int) bool {
		return result.BalanceUpdates[i].ChannelId < result.BalanceUpdates[j].ChannelId
	})
	sort.Slice(result.SkippedChannels, func(i, j int) bool {
		return result.SkippedChannels[i].ChannelId < result.SkippedChannels[j].ChannelId
	})
	sort.Slice(result.Failures, func(i, j int) bool {
		return result.Failures[i].ChannelId < result.Failures[j].ChannelId
	})
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
		Detail: newChannelRatioMonitorTaskFailure(channelId, channelName, channelRemark, failureType, failure),
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

func channelRatioMonitorFailureLimitReached(failureCount int, failureLimit int) bool {
	return failureLimit > 0 && failureCount >= failureLimit
}

func shouldRetryChannelMonitorSync(err error) bool {
	return err != nil &&
		!errors.Is(err, service.ErrChannelMonitorUpstreamAuthentication) &&
		!errors.Is(err, service.ErrChannelMonitorUpstreamGroupUnavailable) &&
		!errors.Is(err, model.ErrChannelRatioMonitorConfigChanged) &&
		!errors.Is(err, model.ErrUpstreamAccountChanged)
}

func channelRatioMonitorFailureAlertReady(monitor model.ChannelRatioMonitor, failureType string, failureLimit int) bool {
	failureCount, notified, _, active := channelRatioMonitorFailureAlertState(monitor, failureType)
	return active && channelRatioMonitorFailureLimitReached(failureCount, failureLimit) && !notified
}

func channelRatioMonitorStoppedSyncFailure(monitor model.ChannelRatioMonitor, failureLimit int) (string, error) {
	if !monitor.UpstreamRatioSyncDisabled &&
		channelRatioMonitorFailureLimitReached(monitor.ConsecutiveFailures, failureLimit) {
		message := strings.TrimSpace(monitor.LastFetchError)
		if message == "" {
			message = "上游倍率获取失败"
		}
		return model.ChannelRatioFailureAlertRatio, errors.New(message)
	}
	if !monitor.UpstreamBalanceSyncDisabled &&
		channelRatioMonitorFailureLimitReached(monitor.BalanceConsecutiveFailures, failureLimit) {
		message := strings.TrimSpace(monitor.LastBalanceError)
		if message == "" {
			message = "上游余额获取失败"
		}
		return model.ChannelRatioFailureAlertBalance, errors.New(message)
	}
	return "", nil
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
	if !active || !channelRatioMonitorFailureLimitReached(failureCount, failureLimit) || notified {
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
	AccountID        int
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
	if pageInfo.Page < 1 {
		pageInfo.Page = 1
	}
	if pageInfo.Page > channelMonitorMaxPage {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "页码必须在 1 到 1000000 之间"})
		return
	}
	if pageInfo.PageSize < 1 {
		pageInfo.PageSize = common.ItemsPerPage
	}
	tasks, total, err := model.GetSystemTasksByTypePage(
		c.Request.Context(), taskType, pageInfo.GetStartIdx(), pageInfo.GetPageSize(),
	)
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

func GetChannelMonitorRatioTaskDetails(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "执行任务 ID 不能为空",
		})
		return
	}
	task, err := model.GetSystemTaskByTaskID(taskID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil || task.Type != model.SystemTaskTypeChannelRatioMonitor {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "倍率与余额更新记录不存在",
		})
		return
	}
	result := channelRatioMonitorTaskResult{}
	if strings.TrimSpace(task.Result) != "" {
		if err := common.UnmarshalJsonStr(task.Result, &result); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	result.sortOutcomeDetails()
	common.ApiSuccess(c, result)
}

func RunChannelMonitorRatioUpdate(c *gin.Context) {
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeChannelRatioMonitor, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
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

type channelMonitorUpdateWindow struct {
	Start int64 `json:"start"`
}

func (channelRatioMonitorTaskHandler) NewScheduledPayload(start, _ time.Time) any {
	return channelMonitorUpdateWindow{Start: start.Unix()}
}

func channelMonitorUpdateContext(ctx context.Context, task *model.SystemTask) (context.Context, context.CancelFunc, error) {
	var window channelMonitorUpdateWindow
	if err := task.DecodePayload(&window); err != nil {
		return ctx, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(getChannelMonitorSettings().RoundTimeoutMinutes)*time.Minute)
	return context.WithValue(ctx, upstreamAccountBalanceRoundKey{}, &upstreamAccountBalanceRound{start: window.Start}), cancel, nil
}

func (channelRatioMonitorTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runChannelRatioMonitorSystemTask(ctx, task, runnerID, func(subject, receiver, content string) error {
		return common.SendEmailWithTimeout(subject, receiver, content, 20*time.Second)
	})
}

func runChannelRatioMonitorSystemTask(ctx context.Context, task *model.SystemTask, runnerID string, sendEmail func(string, string, string) error) {
	ctx, cancel, err := channelMonitorUpdateContext(ctx, task)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	defer cancel()
	summary, err := collectChannelRatioMonitorRound(ctx, service.NewSystemTaskProgressReporter(task, runnerID), task.ID)
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if err != nil {
		status = model.SystemTaskStatusFailed
		errorMessage = err.Error()
	}
	deliver := summary.deliverNotification
	summary.deliverNotification = nil
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, summary, errorMessage); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("渠道监控保存任务结果失败：%v", err))
		return
	}
	if deliver != nil {
		go func() {
			deliver(&summary, sendEmail)
			if err := model.UpdateFinishedChannelMonitorTaskResult(task.TaskID, runnerID, status, summary); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("渠道监控保存邮件发送结果失败：%v", err))
			}
		}()
	}
}

// The previous finished round is the checkpoint. While its SMTP delivery is
// pending, carry the sending task ID forward so other runners can see its outcome.
func (result *channelRatioMonitorTaskResult) continueTaskFailure(taskID int64) error {
	result.TaskFailureRounds = 1
	var previous model.SystemTask
	err := model.DB.Select("id", "result", "status", "updated_at").
		Where("type = ? AND id < ?", model.SystemTaskTypeChannelRatioMonitor, taskID).
		Order("id desc").Limit(1).Find(&previous).Error
	if err != nil || previous.Status != model.SystemTaskStatusFailed || previous.Result == "" {
		return err
	}
	var last channelRatioMonitorTaskResult
	if err := common.UnmarshalJsonStr(previous.Result, &last); err != nil {
		return err
	}
	result.TaskFailureRounds += last.TaskFailureRounds
	result.TaskFailureNotified = last.TaskFailureNotified
	if last.TaskFailureMailID == 0 || result.TaskFailureNotified {
		return nil
	}
	if last.TaskFailureMailID != previous.ID {
		var sender model.SystemTask
		if err := model.DB.Select("result", "updated_at").Where("id = ?", last.TaskFailureMailID).Limit(1).Find(&sender).Error; err != nil {
			return err
		}
		if sender.Result == "" {
			return nil
		}
		previous = sender
		var delivery channelRatioMonitorTaskResult
		if err := common.UnmarshalJsonStr(sender.Result, &delivery); err != nil {
			return err
		}
		last = delivery
	}
	result.TaskFailureNotified = last.TaskFailureNotified
	// SMTP has a 20s deadline. Allow retry after a crashed delivery worker.
	if last.EmailStatus == "pending" && time.Since(time.Unix(previous.UpdatedAt, 0)) < 2*time.Minute {
		result.TaskFailureMailID = last.TaskFailureMailID
	}
	return nil
}

func collectChannelRatioMonitorRound(ctx context.Context, reportProgress func(processed, total int), taskID int64) (summary channelRatioMonitorTaskResult, taskErr error) {
	startedAt := time.Now()
	if reportProgress == nil {
		reportProgress = func(int, int) {}
	}
	settings := getChannelMonitorSettings()
	requestTimeout := settings.upstreamRequestTimeout()
	failureStopLimit := settings.AutoUpdateConsecutiveFailureLimit
	if settings.AutoDisableOnUpdateFailure {
		// Continue polling so the channel can reach the disable threshold and recover.
		failureStopLimit = 0
	}
	failureAlertLimit := settings.SyncFailureAlertThreshold
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
			service.NotifyChannelModelDetectionOverviewChanged()
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
		if errors.Is(context.Cause(ctx), context.DeadlineExceeded) && !errors.Is(taskErr, context.DeadlineExceeded) {
			if taskErr == nil || errors.Is(taskErr, context.Canceled) {
				taskErr = context.DeadlineExceeded
			} else {
				taskErr = errors.Join(context.DeadlineExceeded, taskErr)
			}
		}
		summary.sortOutcomeDetails()
		emailBalanceWarnings := mergeUpstreamAccountBalanceWarnings(balanceWarnings)
		summary.BalanceWarnings = len(emailBalanceWarnings)
		summary.notificationFailures = make([]channelRatioMonitorTaskFailure, 0, len(failureNotifications))
		for _, notification := range failureNotifications {
			summary.notificationFailures = append(summary.notificationFailures, notification.Detail)
		}
		summary.notificationFailuresTruncated = failureNotificationsTruncated
		types := settings.EmailNotificationTypes
		taskFailure := summary.GroupUpdateFailed || taskErr != nil
		if taskFailure {
			err := summary.continueTaskFailure(taskID)
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("读取渠道监控连续失败记录失败：%v", err))
			}
			if err != nil || summary.TaskFailureRounds < settings.SyncFailureAlertThreshold || summary.TaskFailureNotified || summary.TaskFailureMailID != 0 {
				types = slices.DeleteFunc(slices.Clone(types), func(kind string) bool { return kind == channelMonitorEmailTypeTaskFailed })
			}
		}
		notifyTaskFailure := taskFailure && channelMonitorEmailNotificationTypeEnabled(types, channelMonitorEmailTypeTaskFailed)
		notifyRound :=
			(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeRatioChange) && len(emailChanges) > 0) ||
				(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeChannelDisabled) && len(disabledChannels) > 0) ||
				(channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeGroupMembershipRemoved) && len(removedGroupMemberships) > 0) ||
				notifyTaskFailure
		notifyBalance := channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeBalanceWarning) && len(balanceWarnings) > 0
		notifyFailure := channelMonitorEmailNotificationTypeEnabled(settings.EmailNotificationTypes, channelMonitorEmailTypeUpstreamSyncFailed) && len(failureNotifications) > 0
		if !settings.EmailNotificationEnabled || settings.NotificationEmail == "" || !(notifyRound || notifyBalance || notifyFailure) {
			return
		}
		deadline, _ := ctx.Deadline()
		summary.notificationTiming = channelRatioMonitorTaskTiming{
			startedAt: startedAt,
			deadline:  deadline,
			elapsed:   time.Since(startedAt),
		}
		if notifyTaskFailure {
			summary.TaskFailureMailID = taskID
		}
		summary.EmailStatus = "pending"
		summary.deliverNotification = func(result *channelRatioMonitorTaskResult, sendEmail func(string, string, string) error) {
			// New rounds can finish while SMTP is in progress. Recheck alert
			// acknowledgements before sending the next message.
			channelRatioMonitorNotificationMu.Lock()
			defer channelRatioMonitorNotificationMu.Unlock()
			checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var failureGuards []model.ChannelRatioMonitorFailureAlertGuard
			if notifyFailure {
				result.notificationFailures = make([]channelRatioMonitorTaskFailure, 0, len(failureNotifications))
				for _, notification := range failureNotifications {
					guard := notification.Guard
					current, err := model.GetChannelRatioMonitorWithContext(checkCtx, guard.ChannelId)
					if err == nil && (current.UpstreamRevision != guard.UpstreamRevision ||
						!channelRatioMonitorFailureAlertReady(current, guard.FailureType, guard.FailureCount)) {
						continue
					}
					result.notificationFailures = append(result.notificationFailures, notification.Detail)
					failureGuards = append(failureGuards, guard)
				}
			}
			var balanceGuards []model.ChannelRatioMonitorBalanceAlertGuard
			var currentWarnings []channelRatioMonitorBalanceWarning
			if notifyBalance {
				for _, warning := range balanceWarnings {
					current, err := model.GetChannelRatioMonitorWithContext(checkCtx, warning.ChannelId)
					if err == nil && (current.UpstreamRevision != warning.UpstreamRevision || current.BalanceAlertNotified ||
						current.UpstreamBalance == nil || *current.UpstreamBalance >= warning.Threshold) {
						continue
					}
					currentWarnings = append(currentWarnings, warning)
					balanceGuards = append(balanceGuards, model.ChannelRatioMonitorBalanceAlertGuard{
						ChannelId: warning.ChannelId, UpstreamRevision: warning.UpstreamRevision, WarningThreshold: warning.Threshold,
					})
				}
			}
			cancel()
			if !notifyRound && len(failureGuards) == 0 && len(balanceGuards) == 0 {
				result.EmailStatus = ""
				return
			}
			if err := sendChannelRatioMonitorNotificationEmailForTypes(settings.NotificationEmail, types, emailChanges, mergeUpstreamAccountBalanceWarnings(currentWarnings), disabledChannels, removedGroupMemberships, *result, taskErr, sendEmail); err != nil {
				result.EmailStatus = "failed"
				errorRunes := []rune(err.Error())
				result.EmailError = string(errorRunes[:min(len(errorRunes), 255)])
				logger.LogWarn(context.Background(), fmt.Sprintf("channel ratio monitor: notification email failed: %v", err))
				return
			}
			result.EmailStatus = "sent"
			if notifyTaskFailure {
				result.TaskFailureNotified = true
				result.TaskFailureMailID = 0
			}
			if err := model.MarkChannelRatioMonitorFailureAlertsNotified(failureGuards); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("channel ratio monitor: failure alert state update failed: %v", err))
			}
			if err := model.MarkChannelRatioMonitorBalanceAlertsNotified(balanceGuards); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("channel ratio monitor: balance alert state update failed: %v", err))
			}
		}
	}()

	monitors, err := model.GetChannelRatioMonitorsWithContext(ctx)
	if err != nil {
		return summary, err
	}

	ctx = withUpstreamAccountBalanceRound(ctx)
	round := ctx.Value(upstreamAccountBalanceRoundKey{}).(*upstreamAccountBalanceRound)
	round.bulkUpdate = true
	leaseBudget := time.Duration(settings.AutoUpdateRetryCount+1)*requestTimeout +
		time.Duration(settings.AutoUpdateRetryCount*settings.AutoUpdateRetryDelaySeconds)*time.Second
	round.leaseUntil = time.Now().Add(leaseBudget + 15*time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(round.leaseUntil) {
		round.leaseUntil = deadline.Add(10 * time.Second)
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
	var stateMu sync.Mutex
	var progressMu sync.Mutex
	processed := 0
	reportChannelProgress := func() {
		progressMu.Lock()
		defer progressMu.Unlock()
		processed++
		reportProgress(processed, summary.Total)
	}
	recordTaskError := func(err error) {
		if err == nil {
			return
		}
		stateMu.Lock()
		defer stateMu.Unlock()
		if taskErr == nil {
			taskErr = err
		}
	}
	var monitorWorkers sync.WaitGroup
	for _, monitor := range configured {
		monitorWorkers.Go(func() {
			if taskID > 0 && monitor.LastSyncRoundTaskID >= taskID {
				reportChannelProgress()
				return
			}
			if errors.Is(context.Cause(ctx), context.Canceled) {
				recordTaskError(context.Cause(ctx))
				return
			}
			if monitor.UpstreamRatioSyncDisabled && monitor.UpstreamBalanceSyncDisabled {
				channelName := ""
				channelRemark := ""
				if channel, lookupErr := model.GetChannelById(monitor.ChannelId, true); lookupErr == nil {
					channelName = channel.Name
					if channel.Remark != nil {
						channelRemark = strings.TrimSpace(*channel.Remark)
					}
				}
				stateMu.Lock()
				summary.Skipped++
				summary.recordSkippedChannel(channelRatioMonitorTaskSkipped{
					ChannelId:     monitor.ChannelId,
					ChannelName:   channelName,
					ChannelRemark: channelRemark,
					Reason:        "倍率和余额同步均已禁用",
				})
				stateMu.Unlock()
				reportChannelProgress()
				return
			}
			ratioAutoFetchEnabled := !monitor.UpstreamRatioSyncDisabled &&
				!channelRatioMonitorFailureLimitReached(monitor.ConsecutiveFailures, failureStopLimit)
			balanceSupported := monitor.UpstreamType != service.NewAPIUpstreamType ||
				monitor.UpstreamAuthType == service.NewAPIUpstreamAuthUser || monitor.UpstreamAccountID > 0
			balanceAutoFetchEnabled := balanceSupported && !monitor.UpstreamBalanceSyncDisabled &&
				!channelRatioMonitorFailureLimitReached(monitor.BalanceConsecutiveFailures, failureStopLimit)
			stoppedSyncFailureType, stoppedSyncFailure := channelRatioMonitorStoppedSyncFailure(
				monitor,
				failureStopLimit,
			)
			pendingChannelName := ""
			pendingChannelRemark := ""
			pendingRatioFailure := !ratioAutoFetchEnabled && channelRatioMonitorFailureAlertReady(
				monitor, model.ChannelRatioFailureAlertRatio, failureAlertLimit,
			)
			pendingBalanceFailure := !balanceAutoFetchEnabled && channelRatioMonitorFailureAlertReady(
				monitor, model.ChannelRatioFailureAlertBalance, failureAlertLimit,
			)
			if pendingRatioFailure || pendingBalanceFailure || (!ratioAutoFetchEnabled && !balanceAutoFetchEnabled) {
				if pendingChannel, lookupErr := model.GetChannelById(monitor.ChannelId, true); lookupErr == nil {
					pendingChannelName = pendingChannel.Name
					if pendingChannel.Remark != nil {
						pendingChannelRemark = strings.TrimSpace(*pendingChannel.Remark)
					}
				}
			}
			if !ratioAutoFetchEnabled {
				stateMu.Lock()
				_, truncated := appendReadyChannelRatioMonitorFailureNotification(
					&failureNotifications, monitor, pendingChannelName, pendingChannelRemark, model.ChannelRatioFailureAlertRatio,
					failureAlertLimit, nil,
				)
				failureNotificationsTruncated = failureNotificationsTruncated || truncated
				stateMu.Unlock()
			}
			if !balanceAutoFetchEnabled {
				stateMu.Lock()
				_, truncated := appendReadyChannelRatioMonitorFailureNotification(
					&failureNotifications, monitor, pendingChannelName, pendingChannelRemark, model.ChannelRatioFailureAlertBalance,
					failureAlertLimit, nil,
				)
				failureNotificationsTruncated = failureNotificationsTruncated || truncated
				stateMu.Unlock()
			}
			if !ratioAutoFetchEnabled && !balanceAutoFetchEnabled {
				if stoppedSyncFailure != nil {
					stateMu.Lock()
					summary.recordFailure(monitor.ChannelId, pendingChannelName, pendingChannelRemark, stoppedSyncFailureType, stoppedSyncFailure)
					stateMu.Unlock()
				} else {
					stateMu.Lock()
					summary.Skipped++
					summary.recordSkippedChannel(channelRatioMonitorTaskSkipped{
						ChannelId:     monitor.ChannelId,
						ChannelName:   pendingChannelName,
						ChannelRemark: pendingChannelRemark,
						Reason:        "连续失败达到阈值，等待恢复",
					})
					stateMu.Unlock()
				}
				reportChannelProgress()
				return
			}
			fetchRatio := ratioAutoFetchEnabled

			channel, err := model.GetChannelById(monitor.ChannelId, true)
			if err != nil {
				stateMu.Lock()
				failureType := model.ChannelRatioFailureAlertBalance
				if fetchRatio {
					failureType = model.ChannelRatioFailureAlertRatio
				}
				summary.recordFailure(monitor.ChannelId, "", "", failureType, err)
				stateMu.Unlock()
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
						stateMu.Lock()
						_, truncated := appendReadyChannelRatioMonitorFailureNotification(
							&failureNotifications, latestMonitor, "", "", model.ChannelRatioFailureAlertRatio,
							failureAlertLimit, err,
						)
						failureNotificationsTruncated = failureNotificationsTruncated || truncated
						stateMu.Unlock()
					}
				}
				logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d lookup failed: %v", monitor.ChannelId, err))
				reportChannelProgress()
				return
			}
			channelRemark := ""
			if channel.Remark != nil {
				channelRemark = strings.TrimSpace(*channel.Remark)
			}
			var previousBalance *float64
			if monitor.UpstreamBalance != nil {
				value := *monitor.UpstreamBalance
				previousBalance = &value
			}

			var outcome channelMonitorFetchOutcome
			previousRatio := monitor.Ratio
			var recordedBalance *float64
			var balanceEvaluation *channelMonitorBalanceEvaluation
			var effectiveBalanceForRecovery *float64
			var balanceFetchFailure error
			balanceBelowAutoDisableThreshold := false
			balanceEstimateIncomplete := false
			ratioUpdated := false
			retryRatio := fetchRatio
			refreshOptions := channelMonitorRefreshOptions{IncludeSeparateBalance: true, DeferFailureRecording: true}
			started := false
			var sharedBalance <-chan upstreamAccountBalanceResult
			if fetchRatio && balanceAutoFetchEnabled && monitor.UpstreamAccountID > 0 {
				started = ctx.Err() == nil
				result := make(chan upstreamAccountBalanceResult, 1)
				sharedBalance = result
				go func(monitor model.ChannelRatioMonitor) {
					outcome, err := fetchAndRecordUpstreamAccountBalance(ctx, monitor, requestTimeout)
					result <- upstreamAccountBalanceResult{outcome: outcome, err: err}
				}(monitor)
			}
			retriesUsed := 0
			ratioRetriesUsed := 0
			maxRetries := settings.AutoUpdateRetryCount
			if sharesRequest, _ := channelMonitorSharesRatioBalanceRequest(monitor); sharesRequest {
				maxRetries = 0
			}
		retryLoop:
			for attempt := 0; attempt <= maxRetries; attempt++ {
				if err := context.Cause(ctx); err != nil {
					recordTaskError(err)
					break
				}
				if attempt > 0 {
					if settings.AutoUpdateRetryDelaySeconds > 0 {
						timer := time.NewTimer(time.Duration(settings.AutoUpdateRetryDelaySeconds) * time.Second)
						select {
						case <-ctx.Done():
							timer.Stop()
							recordTaskError(context.Cause(ctx))
							break retryLoop
						case <-timer.C:
						}
					}
					select {
					case <-ctx.Done():
						recordTaskError(context.Cause(ctx))
						break retryLoop
					default:
					}

					refreshedMonitor, refreshErr := model.GetChannelRatioMonitorWithContext(ctx, monitor.ChannelId)
					if refreshErr != nil {
						err = fmt.Errorf("重试前重新读取上游配置失败: %w", refreshErr)
						break
					}
					if refreshedMonitor.UpstreamRevision != monitor.UpstreamRevision {
						err = model.ErrChannelRatioMonitorConfigChanged
						break
					}
					monitor = refreshedMonitor
					if retryRatio && !ratioUpdated {
						ratioRetriesUsed++
					}
					retriesUsed++
					stateMu.Lock()
					summary.Retried++
					stateMu.Unlock()
				}

				started = true
				if retryRatio && !ratioUpdated {
					fetchMonitor := monitor
					if sharedBalance != nil || recordedBalance != nil ||
						(balanceFetchFailure != nil && !shouldRetryChannelMonitorSync(balanceFetchFailure)) ||
						channelRatioMonitorFailureLimitReached(fetchMonitor.BalanceConsecutiveFailures, failureStopLimit) {
						fetchMonitor.UpstreamBalanceSyncDisabled = true
					}
					outcome, err = fetchAndRecordChannelMonitorUpstreamRatio(ctx, fetchMonitor, channel.GetKeys(), channel.GetSetting().Proxy, requestTimeout, refreshOptions, 0, "系统自动更新")
					ratioUpdated = outcome.RatioRecorded
					if outcome.CustomActionSucceeded {
						refreshOptions.SkipCustomActions = true
						recordedBalance, balanceEvaluation = nil, nil
					}
					if !fetchMonitor.UpstreamBalanceSyncDisabled || outcome.CustomActionSucceeded {
						balanceFetchFailure = nil
					}
					if balanceError := strings.TrimSpace(outcome.Result.Balance.Error); balanceError != "" &&
						(fetchMonitor.UpstreamType != service.NewAPIUpstreamType ||
							fetchMonitor.UpstreamAuthType == service.NewAPIUpstreamAuthUser) {
						balanceFetchFailure = outcome.Result.Balance.FetchError
						if balanceFetchFailure == nil {
							balanceFetchFailure = errors.New(balanceError)
						}
					}
					if outcome.BalanceRecorded && outcome.Result.Balance.Amount != nil {
						balance := *outcome.Result.Balance.Amount
						recordedBalance = &balance
						balanceEvaluation = outcome.BalanceEvaluation
					}
				} else {
					balanceOutcome, balanceErr := fetchAndRecordChannelMonitorUpstreamBalance(ctx, monitor, channel.GetKeys(), channel.GetSetting().Proxy, requestTimeout, refreshOptions)
					if balanceOutcome.CustomActionSucceeded {
						refreshOptions.SkipCustomActions = true
						recordedBalance, balanceEvaluation = nil, nil
						balanceFetchFailure = nil
						outcome = balanceOutcome
						ratioUpdated = outcome.RatioRecorded
						fetchRatio = !monitor.UpstreamRatioSyncDisabled
					}
					if fetchRatio && !balanceOutcome.CustomActionSucceeded {
						// Keep a successful ratio while retrying only its failed balance request.
						balanceFetchFailure = balanceErr
						if errors.Is(balanceErr, model.ErrChannelRatioMonitorConfigChanged) {
							err = balanceErr
						}
					} else {
						err = balanceErr
					}
					if balanceOutcome.BalanceRecorded && balanceOutcome.Result.Balance.Amount != nil {
						balance := *balanceOutcome.Result.Balance.Amount
						recordedBalance = &balance
						balanceEvaluation = balanceOutcome.BalanceEvaluation
					}
				}
				if errors.Is(err, model.ErrChannelRatioMonitorConfigChanged) || errors.Is(balanceFetchFailure, model.ErrChannelRatioMonitorConfigChanged) {
					err = model.ErrChannelRatioMonitorConfigChanged
					break
				}
				attemptErr := err
				if fetchRatio {
					retryRatio = !ratioUpdated && shouldRetryChannelMonitorSync(err)
					if !retryRatio {
						attemptErr = balanceFetchFailure
					}
				}
				if attempt == maxRetries || !shouldRetryChannelMonitorSync(attemptErr) || (monitor.UpstreamAccountID > 0 && !retryRatio) {
					break
				}
				logger.LogWarn(ctx, fmt.Sprintf(
					"channel ratio monitor: channel_id=%d attempt=%d failed: %v",
					monitor.ChannelId,
					attempt+1,
					attemptErr,
				))
			}
			if sharedBalance != nil {
				result := <-sharedBalance
				balanceFetchFailure = result.err
				if result.outcome.BalanceRecorded && result.outcome.Result.Balance.Amount != nil {
					recordedBalance = result.outcome.Result.Balance.Amount
					evaluation, _ := evaluateChannelMonitorBalance(ctx, monitor, *recordedBalance)
					balanceEvaluation = &evaluation
				}
			}
			if errors.Is(context.Cause(ctx), context.Canceled) {
				recordTaskError(context.Cause(ctx))
				return
			}
			if errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
				recordTaskError(context.Cause(ctx))
				if !started {
					stateMu.Lock()
					summary.Skipped++
					summary.recordSkippedChannel(channelRatioMonitorTaskSkipped{ChannelId: monitor.ChannelId, ChannelName: channel.Name, ChannelRemark: channelRemark, Reason: "任务超时，本轮未执行"})
					stateMu.Unlock()
					reportChannelProgress()
					return
				}
				if !ratioUpdated && err == nil && fetchRatio {
					err = context.DeadlineExceeded
				}
				if balanceAutoFetchEnabled && recordedBalance == nil {
					if !fetchRatio && err == nil {
						err = context.DeadlineExceeded
					} else if fetchRatio && balanceFetchFailure == nil {
						balanceFetchFailure = context.DeadlineExceeded
					}
				}
			}
			if errors.Is(err, model.ErrChannelRatioMonitorConfigChanged) || errors.Is(err, model.ErrUpstreamAccountChanged) ||
				errors.Is(balanceFetchFailure, model.ErrChannelRatioMonitorConfigChanged) || errors.Is(balanceFetchFailure, model.ErrUpstreamAccountChanged) {
				stateMu.Lock()
				summary.Skipped++
				summary.recordSkippedChannel(channelRatioMonitorTaskSkipped{
					ChannelId:     monitor.ChannelId,
					ChannelName:   channel.Name,
					ChannelRemark: channelRemark,
					Reason:        "同步配置已变更，丢弃本次上游结果",
				})
				stateMu.Unlock()
				reportChannelProgress()
				return
			}
			ratioFailure, balanceFailure := err, balanceFetchFailure
			if !fetchRatio {
				ratioFailure, balanceFailure = nil, err
			}
			roundSucceeded := ratioFailure == nil && balanceFailure == nil && stoppedSyncFailure == nil
			finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			latestMonitor, roundRecorded, recordErr := model.RecordChannelMonitorSyncRound(
				finishCtx, monitor.ChannelId, monitor.UpstreamRevision, taskID, ratioFailure, balanceFailure, roundSucceeded,
			)
			finishCancel()
			if recordErr != nil {
				recordTaskError(fmt.Errorf("记录渠道 %d 本轮同步结果失败：%w", monitor.ChannelId, recordErr))
			}
			if roundRecorded {
				stateMu.Lock()
				for _, failure := range []struct {
					kind string
					err  error
				}{{model.ChannelRatioFailureAlertRatio, ratioFailure}, {model.ChannelRatioFailureAlertBalance, balanceFailure}} {
					if failure.err != nil {
						_, truncated := appendReadyChannelRatioMonitorFailureNotification(
							&failureNotifications, latestMonitor, channel.Name, channelRemark, failure.kind, failureAlertLimit, failure.err,
						)
						failureNotificationsTruncated = failureNotificationsTruncated || truncated
					}
				}
				stateMu.Unlock()
			}
			if recordedBalance != nil {
				balance := *recordedBalance
				effectiveBalance := balance
				estimatedConsumption := 0.0
				if balanceEvaluation != nil {
					effectiveBalance = balanceEvaluation.EffectiveBalance
					estimatedConsumption = balanceEvaluation.EstimatedConsumption
					balanceEstimateIncomplete = !balanceEvaluation.Complete
				}
				effectiveBalanceForRecovery = &effectiveBalance
				balanceBelowAutoDisableThreshold = monitor.BalanceAutoDisableThreshold != nil &&
					effectiveBalance < *monitor.BalanceAutoDisableThreshold
				balanceWarning := monitor.BalanceWarningThreshold != nil &&
					balance < *monitor.BalanceWarningThreshold
				stateMu.Lock()
				summary.BalanceUpdated++
				summary.recordBalanceUpdate(channelRatioMonitorTaskBalance{
					ChannelId:        channel.Id,
					ChannelName:      channel.Name,
					ChannelRemark:    channelRemark,
					PreviousBalance:  previousBalance,
					Balance:          balance,
					Warning:          balanceWarning,
					WarningThreshold: monitor.BalanceWarningThreshold,
				})
				stateMu.Unlock()
				balanceAutoDisabled, disableErr := autoDisableChannelMonitorAtEffectiveBalance(
					monitor,
					channel,
					balance,
					effectiveBalance,
					estimatedConsumption,
					balanceEvaluation,
				)
				if disableErr != nil {
					if err == nil {
						err = disableErr
					} else {
						err = fmt.Errorf("%w（余额自动禁用失败：%v）", err, disableErr)
					}
				}
				if balanceAutoDisabled {
					stateMu.Lock()
					summary.ChannelsDisabled++
					channelStatusChanged = true
					disabledChannels = append(disabledChannels, channelRatioMonitorDisabledChannel{
						ChannelId:     channel.Id,
						ChannelName:   channel.Name,
						ChannelRemark: channelRemark,
						Reason: "上游余额 " + formatChannelMonitorBalanceAmount(balance) + "，估算余额 " +
							formatChannelMonitorBalanceAmount(effectiveBalance) + "，低于自动禁用阈值 " +
							formatChannelMonitorBalanceAmount(*monitor.BalanceAutoDisableThreshold),
					})
					stateMu.Unlock()
				}
				if balanceWarning && !monitor.BalanceAlertNotified {
					stateMu.Lock()
					summary.BalanceWarnings++
					balanceWarnings = append(balanceWarnings, channelRatioMonitorBalanceWarning{
						AccountID:        monitor.UpstreamAccountID,
						ChannelId:        monitor.ChannelId,
						UpstreamRevision: monitor.UpstreamRevision,
						ChannelName:      channel.Name,
						ChannelRemark:    channelRemark,
						UpstreamType:     monitor.UpstreamType,
						Balance:          balance,
						Threshold:        *monitor.BalanceWarningThreshold,
					})
					stateMu.Unlock()
				}
			}
			stoppedSyncOnly := err == nil && stoppedSyncFailure != nil
			updateFailed := err != nil || stoppedSyncOnly
			if updateFailed {
				failureErr := err
				if failureErr == nil {
					failureErr = stoppedSyncFailure
				}
				failureRetries := retriesUsed
				if fetchRatio {
					failureRetries = ratioRetriesUsed
				}
				if failureRetries > 0 {
					failureErr = fmt.Errorf("重试 %d 次后仍失败: %w", failureRetries, failureErr)
				}
				if fetchRatio && balanceFetchFailure != nil {
					failureErr = fmt.Errorf("%w；余额同步失败：%v", failureErr, balanceFetchFailure)
				}
				stateMu.Lock()
				failureType := model.ChannelRatioFailureAlertBalance
				if fetchRatio {
					failureType = model.ChannelRatioFailureAlertRatio
				}
				summary.recordFailure(monitor.ChannelId, channel.Name, channelRemark, failureType, failureErr)
				stateMu.Unlock()
				logger.LogWarn(ctx, fmt.Sprintf("channel ratio monitor: channel_id=%d update failed: %v", monitor.ChannelId, failureErr))
				if stoppedSyncOnly {
					stateMu.Lock()
					summary.Updated++
					if retriesUsed > 0 {
						summary.RecoveredAfterRetry++
					}
					stateMu.Unlock()
				}
			} else {
				stateMu.Lock()
				summary.Updated++
				if fetchRatio && balanceFetchFailure != nil {
					summary.recordFailure(
						monitor.ChannelId,
						channel.Name,
						channelRemark,
						model.ChannelRatioFailureAlertBalance,
						balanceFetchFailure,
					)
				}
				if retriesUsed > 0 && balanceFetchFailure == nil {
					summary.RecoveredAfterRetry++
				}
				stateMu.Unlock()
				syncRecovered := balanceFetchFailure == nil && (monitor.UpstreamRatioSyncDisabled || ratioUpdated) &&
					(!balanceSupported || monitor.UpstreamBalanceSyncDisabled || recordedBalance != nil)
				if syncRecovered && !balanceEstimateIncomplete && channelMonitorUpdateFailureRecovered(monitor, channel, effectiveBalanceForRecovery) {
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
							stateMu.Lock()
							summary.ChannelsEnabled++
							channelStatusChanged = true
							stateMu.Unlock()
						}
					}
				}
				if ratioUpdated {
					stateMu.Lock()
					economicInputsChanged = true
					policyInputs[monitor.ChannelId] = channelMonitorPolicyInput{
						UpstreamRevision:                 monitor.UpstreamRevision,
						CostRatio:                        outcome.Result.CostRatio,
						BalanceBelowAutoDisableThreshold: balanceBelowAutoDisableThreshold,
						BalanceUnavailable: !monitor.UpstreamBalanceSyncDisabled &&
							monitor.BalanceAutoDisableThreshold != nil && (recordedBalance == nil || balanceFetchFailure != nil || balanceEstimateIncomplete),
						SingleChannelAction:    monitor.SingleChannelAction,
						MultipleChannelsAction: monitor.MultipleChannelsAction,
					}
					if outcome.Changed {
						summary.Changed++
						summary.recordChangedChannel(channelRatioMonitorTaskChange{
							ChannelId:     channel.Id,
							ChannelName:   channel.Name,
							ChannelRemark: channelRemark,
							OldRatio:      previousRatio,
							NewRatio:      outcome.Result.Ratio,
							OldCostRatio:  previousRatio * outcome.Result.ConversionFactor,
							NewCostRatio:  outcome.Result.CostRatio,
						})
						emailChanges = append(emailChanges, channelRatioMonitorEmailChange{
							ChannelId:        monitor.ChannelId,
							ChannelName:      channel.Name,
							ChannelRemark:    channelRemark,
							UpstreamType:     monitor.UpstreamType,
							UpstreamGroup:    monitor.UpstreamGroup,
							OldRatio:         previousRatio,
							NewRatio:         outcome.Result.Ratio,
							ConversionFactor: outcome.Result.ConversionFactor,
							OldCostRatio:     previousRatio * outcome.Result.ConversionFactor,
							NewCostRatio:     outcome.Result.CostRatio,
						})
					}
					stateMu.Unlock()
				}
				if settings.AutoEnableOnBalanceRecovery && recordedBalance != nil {
					costRatio := 0.0
					costRatioAvailable := false
					if ratioUpdated {
						costRatio = outcome.Result.CostRatio
						costRatioAvailable = validateChannelMonitorRatio(&costRatio)
					} else if monitor.UpdatedTime > 0 &&
						(monitor.UpstreamRatioSyncDisabled || !channelRatioMonitorFailureLimitReached(monitor.ConsecutiveFailures, failureStopLimit)) {
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
						stateMu.Lock()
						balanceRecoveryInputs[monitor.ChannelId] = channelMonitorPolicyInput{
							UpstreamRevision:                 monitor.UpstreamRevision,
							CostRatio:                        costRatio,
							BalanceBelowAutoDisableThreshold: balanceBelowAutoDisableThreshold,
							BalanceUnavailable:               balanceEstimateIncomplete,
						}
						stateMu.Unlock()
					}
				}
			}
			if settings.AutoDisableOnUpdateFailure && roundRecorded && (ratioFailure != nil || balanceFailure != nil) &&
				latestMonitor.SyncFailureRounds >= settings.AutoDisableFailureRounds &&
				channel.Status == common.ChannelStatusEnabled {
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
				if disabled && revisionCurrent {
					service.DrainChannelWebSockets(channel.Id)
					stateMu.Lock()
					summary.ChannelsDisabled++
					channelStatusChanged = true
					disabledChannels = append(disabledChannels, channelRatioMonitorDisabledChannel{
						ChannelId:     channel.Id,
						ChannelName:   channel.Name,
						ChannelRemark: channelRemark,
						Reason:        fmt.Sprintf("上游倍率或余额连续 %d 轮更新失败或超时", latestMonitor.SyncFailureRounds),
					})
					stateMu.Unlock()
				}
			}
			reportChannelProgress()
		})
	}
	monitorWorkers.Wait()
	var releases sync.WaitGroup
	round.leases.Range(func(_, value any) bool {
		lease := value.(*upstreamAccountRoundLease)
		if lease.release != nil {
			releases.Go(lease.release)
		}
		return true
	})
	releases.Wait()
	if taskErr != nil {
		return summary, taskErr
	}
	if err := ctx.Err(); err != nil {
		return summary, err
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
	return summary, ctx.Err()
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

func writeChannelRatioMonitorFailureTable(content *strings.Builder, failures []channelRatioMonitorTaskFailure) {
	content.WriteString("<table style=\"border-collapse:collapse\"><thead><tr>")
	for _, heading := range []string{"渠道", "备注", "同步项目", "失败原因"} {
		fmt.Fprintf(content, "<th style=\"border:1px solid #ddd;padding:6px 10px;text-align:left\">%s</th>", heading)
	}
	content.WriteString("</tr></thead><tbody>")
	for _, failure := range failures {
		channelName := fmt.Sprintf("渠道 ID %d", failure.ChannelId)
		if failure.ChannelName != "" {
			channelName = fmt.Sprintf("%s（ID: %d）", failure.ChannelName, failure.ChannelId)
		}
		kind := "-"
		switch failure.Kind {
		case model.ChannelRatioFailureAlertRatio:
			kind = "倍率"
		case model.ChannelRatioFailureAlertBalance:
			kind = "余额"
		}
		reason := strings.ReplaceAll(common.MaskSensitiveInfo(failure.Error), context.DeadlineExceeded.Error(), "请求超时")
		fmt.Fprintf(content,
			"<tr><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px;white-space:pre-wrap\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td><td style=\"border:1px solid #ddd;padding:6px 10px\">%s</td></tr>",
			html.EscapeString(channelName), channelRatioMonitorEmailRemark(failure.ChannelRemark), kind, html.EscapeString(reason),
		)
	}
	content.WriteString("</tbody></table>")
}

func buildChannelRatioMonitorNotificationEmail(notificationTypes []string, changes []channelRatioMonitorEmailChange, balanceWarnings []channelRatioMonitorBalanceWarning, disabledChannels []channelRatioMonitorDisabledChannel, removedGroupMemberships []channelRatioMonitorRemovedGroupMembership, summary channelRatioMonitorTaskResult, taskErr error, sections ...channelMonitorNotificationEmailSection) (string, string) {
	failureDetails, failureDetailsTruncated := channelRatioMonitorNotificationFailureDetails(summary)
	includeChanges := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeRatioChange) && len(changes) > 0
	includeBalanceWarnings := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeBalanceWarning) && len(balanceWarnings) > 0
	includeDisabledChannels := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeChannelDisabled) && len(disabledChannels) > 0
	includeRemovedGroupMemberships := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeGroupMembershipRemoved) && len(removedGroupMemberships) > 0
	includeUpstreamSyncFailures := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeUpstreamSyncFailed) && len(failureDetails) > 0
	includeTaskFailure := channelMonitorEmailNotificationTypeEnabled(notificationTypes, channelMonitorEmailTypeTaskFailed) && (summary.GroupUpdateFailed || taskErr != nil)
	taskTimedOut := includeTaskFailure && !summary.GroupUpdateFailed && errors.Is(taskErr, context.DeadlineExceeded)

	var content strings.Builder
	content.WriteString(`<!doctype html><html><head><meta charset="UTF-8"><meta name="color-scheme" content="light only"><meta name="supported-color-schemes" content="light"></head><body style="margin:0;background:#ffffff;color:#111827;font-family:Arial,'Microsoft YaHei',sans-serif;font-size:14px;line-height:1.5"><div style="padding:16px">`)
	content.WriteString("<p>渠道监控检测到以下变化或异常：</p>")
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
		writeChannelRatioMonitorFailureTable(&content, failureDetails)
		if failureDetailsTruncated {
			fmt.Fprintf(&content, "<p>失败渠道较多，邮件仅展示前 %d 条明细。</p>", len(failureDetails))
		}
	}

	if includeTaskFailure {
		switch {
		case summary.GroupUpdateFailed:
			content.WriteString("<h3>分组倍率更新失败</h3>")
			content.WriteString("<p>自动写入分组倍率失败。</p>")
		case taskTimedOut:
			content.WriteString("<h3>定时更新任务超时</h3>")
		default:
			content.WriteString("<h3>定时更新任务失败</h3>")
		}
		if taskErr == context.DeadlineExceeded {
			content.WriteString("<p>任务已达到执行时限，部分更新或收尾步骤尚未完成。</p>")
		} else if taskErr != nil {
			reason := strings.ReplaceAll(common.MaskSensitiveInfo(taskErr.Error()), context.DeadlineExceeded.Error(), "执行超时")
			fmt.Fprintf(&content, "<p>失败原因：%s</p>", html.EscapeString(reason))
		}
		timing := summary.notificationTiming
		if !timing.startedAt.IsZero() {
			const timeFormat = "2006-01-02 15:04:05 UTC-07:00"
			fmt.Fprintf(&content, "<p>执行开始于 %s，耗时 %.1f 秒", timing.startedAt.Format(timeFormat), timing.elapsed.Seconds())
			if !timing.deadline.IsZero() {
				fmt.Fprintf(&content, "，本轮截止时间为 %s", timing.deadline.In(timing.startedAt.Location()).Format(timeFormat))
			}
			content.WriteString("。</p>")
		}
		if summary.Total > 0 {
			fmt.Fprintf(&content, "<p>计划检查 %d 个渠道，已刷新 %d 个渠道的余额，倍率变更 %d 个，记录 %d 项同步失败，跳过 %d 个渠道。</p>",
				summary.Total, summary.BalanceUpdated, summary.Changed, summary.Failed, summary.Skipped)
		}
		content.WriteString("<p>请在「渠道监控 → 运行记录 → 倍率与余额更新记录」中查看本轮详情。</p>")
	}

	failureCount := 0
	if includeUpstreamSyncFailures {
		failureCount = len(failureDetails)
	}
	if includeTaskFailure && !taskTimedOut {
		failureCount++
	}
	var failureParts []string
	if taskTimedOut {
		failureParts = append(failureParts, "定时更新任务超时")
	}
	if failureCount > 0 {
		failureParts = append(failureParts, fmt.Sprintf("%d 项更新失败", failureCount))
	}
	failureSummary := strings.Join(failureParts, "，")
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
		if failureSummary != "" {
			parts = append(parts, failureSummary)
		}
		subject = "渠道监控：" + strings.Join(parts, "，")
	} else if changeCount > 0 && failureSummary != "" {
		subject = fmt.Sprintf("渠道监控：%d 个倍率变更，%s", changeCount, failureSummary)
	} else if failureSummary != "" {
		subject = "渠道监控：" + failureSummary
	}
	if len(sections) > 0 {
		parts := make([]string, 0, len(sections)+1)
		if includeChanges || includeBalanceWarnings || includeDisabledChannels || includeRemovedGroupMemberships || includeUpstreamSyncFailures || includeTaskFailure {
			parts = append(parts, strings.TrimPrefix(subject, "渠道监控："))
		}
		for _, section := range sections {
			content.WriteString(section.HTML)
			parts = append(parts, section.Summary)
		}
		subject = "渠道监控：" + strings.Join(parts, "，")
	}
	content.WriteString("</div></body></html>")
	return subject, content.String()
}
