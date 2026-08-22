package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
)

const (
	channelGroupMonitorDefaultScanIntervalMillis = 1000
	channelGroupMonitorMinScanIntervalMillis     = 200
	channelGroupMonitorMaxScanIntervalMillis     = 30000
	channelGroupMonitorLeaseRenewEvery           = 2 * time.Minute
)

type channelGroupMonitorTestContextKey struct{}

type channelGroupMonitorAttemptLogContextKey struct{}
type channelGroupMonitorAttemptResultContextKey struct{}

type channelGroupMonitorAttemptLogInfo struct {
	RunId               string
	Attempt             int
	RetryIndex          int
	AttemptedChannelIds []int
}

var (
	channelGroupMonitorWorkerOnce sync.Once
	channelGroupMonitorWake       = make(chan struct{}, 1)
)

func withChannelGroupMonitorTestContext(ctx context.Context, groupName string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, channelGroupMonitorTestContextKey{}, strings.TrimSpace(groupName))
}

func isChannelGroupMonitorTest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	groupName, _ := ctx.Value(channelGroupMonitorTestContextKey{}).(string)
	return groupName != ""
}

func withChannelGroupMonitorAttemptLogInfo(
	ctx context.Context,
	info channelGroupMonitorAttemptLogInfo,
) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	info.AttemptedChannelIds = append([]int(nil), info.AttemptedChannelIds...)
	return context.WithValue(ctx, channelGroupMonitorAttemptLogContextKey{}, info)
}

func channelGroupMonitorAttemptLogInfoFromContext(
	ctx context.Context,
) (channelGroupMonitorAttemptLogInfo, bool) {
	if ctx == nil {
		return channelGroupMonitorAttemptLogInfo{}, false
	}
	info, ok := ctx.Value(channelGroupMonitorAttemptLogContextKey{}).(channelGroupMonitorAttemptLogInfo)
	if !ok {
		return channelGroupMonitorAttemptLogInfo{}, false
	}
	info.AttemptedChannelIds = append([]int(nil), info.AttemptedChannelIds...)
	return info, true
}

func appendChannelGroupMonitorAttemptLogInfo(
	other map[string]interface{},
	info channelGroupMonitorAttemptLogInfo,
	result string,
) {
	if other == nil {
		return
	}
	other[model.ChannelMonitorGroupProbeLogKey] = true
	adminInfo, _ := other["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	if info.RunId != "" {
		other["channel_monitor_probe_run_id"] = info.RunId
	}
	if info.Attempt > 0 {
		other["channel_monitor_probe_attempt"] = info.Attempt
	}
	if info.RetryIndex > 0 {
		other["channel_monitor_probe_retry_index"] = info.RetryIndex
	}
	if len(info.AttemptedChannelIds) > 0 {
		other["channel_monitor_probe_attempted_channels"] = info.AttemptedChannelIds
	}
	if result != "" {
		other["channel_monitor_probe_result"] = result
	}
}

func withChannelGroupMonitorAttemptResult(ctx context.Context, result string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, channelGroupMonitorAttemptResultContextKey{}, result)
}

func channelGroupMonitorAttemptResultFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	result, _ := ctx.Value(channelGroupMonitorAttemptResultContextKey{}).(string)
	return result
}

func applyChannelGroupMonitorAttemptLogContext(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	info, ok := channelGroupMonitorAttemptLogInfoFromContext(c.Request.Context())
	if !ok || len(info.AttemptedChannelIds) == 0 {
		return
	}
	useChannel := make([]string, 0, len(info.AttemptedChannelIds))
	for _, channelID := range info.AttemptedChannelIds {
		useChannel = append(useChannel, fmt.Sprintf("%d", channelID))
	}
	c.Set("use_channel", useChannel)
}

func appendChannelGroupMonitorAttemptLogInfoFromContext(c *gin.Context, other map[string]interface{}, result string) {
	if c == nil || c.Request == nil {
		return
	}
	info, ok := channelGroupMonitorAttemptLogInfoFromContext(c.Request.Context())
	if ok {
		appendChannelGroupMonitorAttemptLogInfo(other, info, result)
	}
}

func applyChannelGroupMonitorTestContext(source context.Context, target *gin.Context) {
	if source == nil || target == nil {
		return
	}
	groupName, _ := source.Value(channelGroupMonitorTestContextKey{}).(string)
	if groupName == "" {
		return
	}
	common.SetContextKey(target, constant.ContextKeyUsingGroup, groupName)
}

func channelGroupMonitorScanIntervalDuration() time.Duration {
	intervalMillis := common.GetEnvOrDefault(
		"CHANNEL_GROUP_MONITOR_SCAN_INTERVAL_MS",
		channelGroupMonitorDefaultScanIntervalMillis,
	)
	if intervalMillis < channelGroupMonitorMinScanIntervalMillis || intervalMillis > channelGroupMonitorMaxScanIntervalMillis {
		intervalMillis = channelGroupMonitorDefaultScanIntervalMillis
	}
	return time.Duration(intervalMillis) * time.Millisecond
}

func wakeChannelGroupMonitorWorker() {
	select {
	case channelGroupMonitorWake <- struct{}{}:
	default:
	}
}

func startChannelGroupMonitorWorker() {
	channelGroupMonitorWorkerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(channelGroupMonitorScanIntervalDuration())
			defer ticker.Stop()
			for {
				if err := runChannelGroupMonitorScanOnce(context.Background()); err != nil {
					common.SysError("扫描分组监控任务失败: " + err.Error())
				}
				select {
				case <-ticker.C:
				case <-channelGroupMonitorWake:
				}
			}
		})
	})
}

func runChannelGroupMonitorScanOnce(ctx context.Context) error {
	claim, err := model.ClaimDueChannelGroupMonitor(common.GetTimestamp())
	if err != nil || claim == nil {
		return err
	}
	gopool.Go(func() {
		if err := runChannelGroupMonitorClaim(ctx, *claim); err != nil {
			common.SysError(fmt.Sprintf("执行分组监控失败: run_id=%s err=%s", claim.RunId, err.Error()))
		}
	})
	return nil
}

func runChannelGroupMonitorClaim(parent context.Context, claim model.ChannelGroupMonitorClaim) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	gopool.Go(func() {
		ticker := time.NewTicker(channelGroupMonitorLeaseRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				renewed, err := model.RenewChannelGroupMonitorLease(claim, common.GetTimestamp())
				if err != nil {
					common.SysError(fmt.Sprintf("续租分组监控失败: run_id=%s err=%s", claim.RunId, err.Error()))
					cancel()
					return
				}
				if !renewed {
					cancel()
					return
				}
			}
		}
	})
	defer func() {
		close(done)
		cancel()
		if err := model.CompleteChannelGroupMonitorClaim(claim, common.GetTimestamp()); err != nil {
			common.SysError(fmt.Sprintf("完成分组监控租约失败: run_id=%s err=%s", claim.RunId, err.Error()))
		}
	}()

	testUserId, testUserErr := resolveChannelTestUserID(nil)
	validCandidates, err := getChannelGroupMonitorCandidateModels(true)
	if err != nil {
		return err
	}
	var groups sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	for _, group := range claim.Groups {
		group := group
		groups.Add(1)
		go func() {
			defer groups.Done()
			if err := ctx.Err(); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			current, err := model.IsChannelGroupMonitorLeaseCurrent(claim, common.GetTimestamp())
			if err == nil && !current {
				err = errors.New("分组监控配置已变化，本轮任务已取消")
			}
			if err == nil {
				err = runChannelGroupMonitorGroup(ctx, claim, group, validCandidates, testUserId, testUserErr)
			}
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}()
	}
	groups.Wait()
	return firstErr
}

func runChannelGroupMonitorGroup(
	ctx context.Context,
	claim model.ChannelGroupMonitorClaim,
	group model.ChannelGroupMonitorGroup,
	validCandidates map[string][]string,
	testUserId int,
	testUserErr error,
) error {
	now := common.GetTimestamp()
	execution := model.ChannelGroupMonitorExecution{
		RunId: claim.RunId, GroupName: group.GroupName, ConfigRevision: claim.Config.Revision,
		Trigger: claim.Trigger, ProbeModel: group.ProbeModel, StartedAt: now, FinishedAt: now, CreatedAt: now,
	}
	if !groupMonitorModelIsCandidate(validCandidates, group.GroupName, group.ProbeModel) {
		execution.Result = model.ChannelGroupMonitorResultSkipped
		execution.ErrorCode = "probe_model_invalid"
		execution.ErrorMessage = "探测模型已失效，本轮未发送请求"
		_, saveErr := model.SaveChannelGroupMonitorExecution(&execution)
		return saveErr
	}
	if testUserErr != nil {
		execution.Result = model.ChannelGroupMonitorResultLocalFailure
		execution.ErrorCode = "test_user_unavailable"
		execution.ErrorMessage = truncateChannelGroupMonitorText(common.MaskSensitiveInfo(testUserErr.Error()), 512)
		_, saveErr := model.SaveChannelGroupMonitorExecution(&execution)
		return saveErr
	}

	probeRoutingContext := newChannelGroupMonitorRoutingContext(ctx, testUserId, group.GroupName)
	probeRequestID := common.NewRequestId()
	probeStartedAt := time.Now()
	probeRoutingContext.Set(common.RequestIdKey, probeRequestID)
	common.SetContextKey(probeRoutingContext, constant.ContextKeyRequestStartTime, probeStartedAt)
	probeRoutingContext.Set(channelTestContextKey, true)
	retryParam := &service.RetryParam{
		Ctx: probeRoutingContext, TokenGroup: group.GroupName, ModelName: group.ProbeModel,
		RequestPath: "/v1/responses", Retry: common.GetPointer(0),
		SelectionOptions: service.ChannelSelectionOptionsForRequest(probeRoutingContext, 0),
	}
	retryRouting := newRelayRetryRouting()
	fastFailureRetryBudget := &relayFastFailureRetryBudget{}
	probeCtx := withChannelGroupMonitorTestContext(ctx, group.GroupName)
	probeCtx = context.WithValue(probeCtx, common.RequestIdKey, probeRequestID)
	probeCtx = context.WithValue(probeCtx, constant.ContextKeyRequestStartTime, probeStartedAt)
	var pendingChannel *model.Channel
	var finalOutcome *channelStatusProbeOutcome
	attemptNumber := 0
	attemptedChannelIds := make([]int, 0, common.RetryTimes+1)
	finalRetryLogPending := false
	finalRetryAttemptDuration := time.Duration(0)
	var finalRetryChannelError *types.ChannelError
	var finalRetryLogContext *gin.Context
	var finalRetryError *types.NewAPIError
	for retryParam.GetRetry() <= common.RetryTimes {
		channel := pendingChannel
		pendingChannel = nil
		if channel == nil {
			selected, _, selectionErr := retryRouting.selectChannel(retryParam)
			if selectionErr != nil {
				execution.Result = model.ChannelGroupMonitorResultLocalFailure
				execution.ErrorCode = "route_selection_failed"
				execution.ErrorMessage = truncateChannelGroupMonitorText(common.MaskSensitiveInfo(selectionErr.Error()), 512)
				// A routing failure is the final logical result. Do not let a
				// previous upstream attempt overwrite it below.
				finalOutcome = nil
				break
			}
			channel = selected
		}
		if channel == nil {
			// Normal relay requests turn a vanished same-channel fast retry into
			// an ordinary retry, then select another route. Preserve that
			// behavior for group probes instead of silently keeping the prior
			// physical attempt as the final result.
			if retryRouting.takeSameChannelRetryUnavailable() {
				previousError := channelGroupMonitorOutcomeErrorValue(finalOutcome)
				if previousError != nil && shouldRetry(probeRoutingContext, previousError, common.RetryTimes-retryParam.GetRetry()) {
					retryParam.IsRetry = true
					retryParam.IncreaseRetry()
					fastFailureRetryBudget.resetChannelVisit()
					continue
				}
			}
			if finalOutcome == nil {
				execution.Result = model.ChannelGroupMonitorResultUnavailable
				execution.ErrorCode = "no_available_route"
				execution.ErrorMessage = "当前没有可分配的探测路由"
			}
			break
		}

		attemptNumber++
		attemptedChannelIds = append(attemptedChannelIds, channel.Id)
		attemptCtx := withChannelGroupMonitorAttemptLogInfo(probeCtx, channelGroupMonitorAttemptLogInfo{
			RunId:               claim.RunId,
			Attempt:             attemptNumber,
			RetryIndex:          retryParam.GetRetry(),
			AttemptedChannelIds: attemptedChannelIds,
		})
		outcome := executeChannelStatusProbeModelWithEndpoint(
			attemptCtx, channel, testUserId, group.ProbeModel, string(constant.EndpointTypeOpenAIResponse),
		)
		execution.ChannelId = channel.Id
		finalOutcome = &outcome
		if outcome.Result == model.ChannelStatusProbeResultSuccess {
			finalRetryLogPending = false
			finalRetryChannelError = nil
			finalRetryLogContext = nil
			finalRetryError = nil
			break
		}

		// A saturated channel is a same-round routing condition. It must not
		// consume an ordinary retry, but another channel should be attempted when
		// one is available, just as the normal relay path does.
		if outcome.Result == model.ChannelStatusProbeResultSkipped && outcome.ErrorCode == "channel_busy" {
			retryRouting.exclude(channel.Id)
			selected, _, selectionErr := retryRouting.selectChannelCurrentRound(retryParam)
			if selectionErr != nil {
				execution.Result = model.ChannelGroupMonitorResultLocalFailure
				execution.ErrorCode = "route_selection_failed"
				execution.ErrorMessage = truncateChannelGroupMonitorText(common.MaskSensitiveInfo(selectionErr.Error()), 512)
				finalOutcome = nil
				break
			}
			if selected == nil {
				execution.Result = model.ChannelGroupMonitorResultUnavailable
				execution.ErrorCode = "no_available_route"
				execution.ErrorMessage = "当前没有可分配的探测路由"
				finalOutcome = nil
				break
			}
			pendingChannel = selected
			continue
		}

		probeError := channelGroupMonitorOutcomeError(outcome)
		// Normal relay routing skips a channel with no usable key before dispatch
		// and tries another candidate in the same round. This does not consume the
		// configured upstream retry budget because no request reached upstream.
		if !outcome.ProbeResult.requestDispatched &&
			probeError != nil &&
			types.IsChannelError(probeError) &&
			probeError.GetErrorCode() == types.ErrorCodeChannelNoAvailableKey &&
			!types.IsSkipRetryError(probeError) {
			retryRouting.exclude(channel.Id)
			selected, _, selectionErr := retryRouting.selectChannelCurrentRound(retryParam)
			if selectionErr != nil {
				execution.Result = model.ChannelGroupMonitorResultLocalFailure
				execution.ErrorCode = "route_selection_failed"
				execution.ErrorMessage = truncateChannelGroupMonitorText(common.MaskSensitiveInfo(selectionErr.Error()), 512)
				finalOutcome = nil
				break
			}
			if selected == nil {
				break
			}
			pendingChannel = selected
			continue
		}
		ordinaryRetryable := shouldRetry(probeRoutingContext, probeError, common.RetryTimes-retryParam.GetRetry())
		attemptDuration := time.Duration(0)
		if outcome.ProbeResult.attemptDuration != nil {
			attemptDuration = *outcome.ProbeResult.attemptDuration
		}
		responseStarted := outcome.ProbeResult.firstResponseMilliseconds != nil
		retryDecision, retryDelay := fastFailureRetryBudget.decide(
			group.GroupName, group.ProbeModel, channel.Id, attemptDuration,
			!responseStarted && isFastFailureSameChannelRetryable(probeRoutingContext, probeError),
			!responseStarted && ordinaryRetryable,
		)
		logContext := outcome.ProbeResult.context
		if logContext == nil {
			logContext = probeRoutingContext
		}
		if probeError != nil && logContext != nil {
			if logContext.Request != nil {
				logContext.Request = logContext.Request.WithContext(
					withChannelGroupMonitorAttemptResult(logContext.Request.Context(), outcome.Result),
				)
			}
			attemptDurationForLog := outcome.ProbeResult.attemptDuration
			channelError := newRelayChannelError(logContext, channel)
			processChannelErrorWithTiming(
				logContext,
				*channelError,
				probeError,
				retryDecision != relayRetryNone,
				attemptNumber > 1,
				attemptDurationForLog,
				false,
			)
			finalRetryLogPending = retryDecision != relayRetryNone
			if finalRetryLogPending {
				if attemptDurationForLog != nil {
					finalRetryAttemptDuration = *attemptDurationForLog
				} else {
					finalRetryAttemptDuration = 0
				}
				finalRetryChannelError = channelError
				finalRetryLogContext = logContext
				finalRetryError = probeError
			} else {
				finalRetryChannelError = nil
				finalRetryLogContext = nil
				finalRetryError = nil
			}
		}
		if retryDecision == relayRetryNone {
			break
		}
		retryParam.IsRetry = true
		if retryDecision == relayRetryFastFailureSameChannel {
			retryRouting.retrySameChannel(channel, group.GroupName)
			if !waitForRelayFastFailureRetry(probeRoutingContext.Request.Context(), retryDelay) {
				break
			}
		} else {
			retryParam.IncreaseRetry()
			retryRouting.exclude(channel.Id)
		}
	}
	if finalRetryLogPending && finalRetryChannelError != nil && finalRetryLogContext != nil && finalRetryError != nil {
		processChannelErrorWithTiming(
			finalRetryLogContext, *finalRetryChannelError, finalRetryError, false, false,
			&finalRetryAttemptDuration, true,
		)
	}
	applyChannelGroupMonitorFinalOutcome(&execution, finalOutcome)
	_, saveErr := model.SaveChannelGroupMonitorExecution(&execution)
	return saveErr
}

func channelGroupMonitorOutcomeErrorValue(outcome *channelStatusProbeOutcome) *types.NewAPIError {
	if outcome == nil {
		return nil
	}
	return channelGroupMonitorOutcomeError(*outcome)
}

func newChannelGroupMonitorRoutingContext(parent context.Context, testUserId int, groupName string) *gin.Context {
	if parent == nil {
		parent = context.Background()
	}
	routingContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	routingContext.Request = httptest.NewRequestWithContext(parent, http.MethodPost, "/v1/responses", nil)
	routingContext.Set("id", testUserId)
	if userGroup, err := model.GetUserGroup(testUserId, false); err == nil {
		common.SetContextKey(routingContext, constant.ContextKeyUserGroup, userGroup)
	}
	common.SetContextKey(routingContext, constant.ContextKeyUsingGroup, groupName)
	return routingContext
}

func channelGroupMonitorOutcomeError(outcome channelStatusProbeOutcome) *types.NewAPIError {
	if outcome.ProbeResult.newAPIError != nil {
		return outcome.ProbeResult.newAPIError
	}
	if outcome.ProbeResult.localErr == nil {
		return nil
	}
	return types.NewError(outcome.ProbeResult.localErr, types.ErrorCodeGetChannelFailed)
}

func applyChannelGroupMonitorFinalOutcome(execution *model.ChannelGroupMonitorExecution, outcome *channelStatusProbeOutcome) {
	if execution == nil || outcome == nil || execution.Result != "" {
		return
	}
	groupMonitorExecutionFromOutcome(execution, *outcome, execution.ChannelId)
}

func groupMonitorExecutionFromOutcome(execution *model.ChannelGroupMonitorExecution, outcome channelStatusProbeOutcome, channelID int) {
	if execution == nil {
		return
	}
	if channelID > 0 {
		execution.ChannelId = channelID
	}
	if outcome.ProbeResult.context != nil && outcome.ProbeResult.context.GetInt("channel_id") > 0 {
		execution.ChannelId = outcome.ProbeResult.context.GetInt("channel_id")
	}
	execution.StartedAt = outcome.StartedAt
	execution.FinishedAt = outcome.FinishedAt
	execution.SettledCostNanoCNY = outcome.SettledCostNanoCNY
	execution.ErrorCode = truncateChannelGroupMonitorText(outcome.ErrorCode, 128)
	execution.ErrorMessage = truncateChannelGroupMonitorText(outcome.ErrorMessage, 512)
	if outcome.TestExecuted {
		execution.RequestDispatched = outcome.ProbeResult.requestDispatched
		execution.ResponseTimeMs = outcome.DurationMs
		execution.FirstTokenMs = outcome.ProbeResult.firstResponseMilliseconds
		execution.TPS = outcome.ProbeResult.tokensPerSecond
		if outcome.ProbeResult.context != nil {
			execution.ChannelId = outcome.ProbeResult.context.GetInt("channel_id")
			execution.RequestId = strings.TrimSpace(outcome.ProbeResult.context.GetString(common.RequestIdKey))
		}
	}
	switch outcome.Result {
	case model.ChannelStatusProbeResultSuccess:
		execution.Result = model.ChannelGroupMonitorResultSuccess
	case model.ChannelStatusProbeResultRateLimited:
		execution.Result = model.ChannelGroupMonitorResultRateLimited
	case model.ChannelStatusProbeResultSkipped, model.ChannelStatusProbeResultCanceled:
		execution.Result = model.ChannelGroupMonitorResultSkipped
	case model.ChannelStatusProbeResultUpstreamFailure:
		execution.Result = model.ChannelGroupMonitorResultUpstreamFailure
	default:
		execution.Result = model.ChannelGroupMonitorResultLocalFailure
	}
}

func truncateChannelGroupMonitorText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
