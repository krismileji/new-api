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
	for index, group := range claim.Groups {
		if index > 0 && common.RequestInterval > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(common.RequestInterval):
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := model.IsChannelGroupMonitorLeaseCurrent(claim, common.GetTimestamp())
		if err != nil {
			return err
		}
		if !current {
			return errors.New("分组监控配置已变化，本轮任务已取消")
		}
		if err := runChannelGroupMonitorGroup(ctx, claim, group, validCandidates, testUserId, testUserErr); err != nil {
			return err
		}
	}
	return nil
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
	retryParam := &service.RetryParam{
		Ctx: probeRoutingContext, TokenGroup: group.GroupName, ModelName: group.ProbeModel,
		RequestPath: "/v1/responses", Retry: common.GetPointer(0),
		SelectionOptions: service.ChannelSelectionOptionsForRequest(probeRoutingContext, 0),
	}
	retryRouting := newRelayRetryRouting()
	fastFailureRetryBudget := &relayFastFailureRetryBudget{}
	probeCtx := withChannelGroupMonitorTestContext(ctx, group.GroupName)
	var pendingChannel *model.Channel
	var finalOutcome *channelStatusProbeOutcome
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

		outcome := executeChannelStatusProbeModelWithEndpoint(
			probeCtx, channel, testUserId, group.ProbeModel, string(constant.EndpointTypeOpenAIResponse),
		)
		execution.ChannelId = channel.Id
		finalOutcome = &outcome
		if outcome.Result == model.ChannelStatusProbeResultSuccess {
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
