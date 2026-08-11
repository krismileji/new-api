package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	channelStatusProbeScanInterval      = time.Second
	channelStatusProbeLeaseRenewEvery   = 2 * time.Minute
	channelStatusProbeDefaultConcurrent = 5
	channelStatusProbeMaxConcurrent     = 20
	channelStatusProbeSampleRetryEvery  = 30 * time.Second
	channelStatusProbeSampleRetryMaxAge = 24 * time.Hour
	channelStatusProbeSampleRetryBatch  = 20
)

type channelStatusProbeTestContextKey struct{}

type channelStatusProbeOutcome struct {
	Result       string
	StartedAt    int64
	FinishedAt   int64
	DurationMs   *float64
	ProbeResult  testResult
	TestExecuted bool
	ErrorCode    string
	ErrorMessage string
}

var channelStatusProbeWorkerOnce sync.Once

func withChannelStatusProbeTestContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, channelStatusProbeTestContextKey{}, true)
}

func isChannelStatusProbeTest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(channelStatusProbeTestContextKey{}).(bool)
	return value
}

func startChannelStatusProbeWorker() {
	channelStatusProbeWorkerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		concurrency := common.GetEnvOrDefault("CHANNEL_STATUS_PROBE_CONCURRENCY", channelStatusProbeDefaultConcurrent)
		if concurrency < 1 || concurrency > channelStatusProbeMaxConcurrent {
			concurrency = channelStatusProbeDefaultConcurrent
		}
		semaphore := make(chan struct{}, concurrency)
		gopool.Go(func() {
			ticker := time.NewTicker(channelStatusProbeScanInterval)
			defer ticker.Stop()
			lastSampleRetry := time.Time{}
			for {
				if err := runChannelStatusProbeScanOnce(context.Background(), semaphore); err != nil {
					common.SysError("扫描渠道状态探测任务失败: " + err.Error())
				}
				if time.Since(lastSampleRetry) >= channelStatusProbeSampleRetryEvery {
					if err := retryPendingChannelStatusProbeSamples(context.Background()); err != nil {
						common.SysError("重试渠道状态探测样本失败: " + err.Error())
					}
					lastSampleRetry = time.Now()
				}
				<-ticker.C
			}
		})
	})
}

func runChannelStatusProbeScanOnce(ctx context.Context, semaphore chan struct{}) error {
	available := cap(semaphore) - len(semaphore)
	if available <= 0 {
		return nil
	}
	claims, err := model.ClaimDueChannelStatusProbes(common.GetTimestamp(), available)
	if err != nil {
		return err
	}
	for _, current := range claims {
		claim := current
		semaphore <- struct{}{}
		gopool.Go(func() {
			defer func() { <-semaphore }()
			if err := runChannelStatusProbeClaim(ctx, claim); err != nil {
				common.SysError(fmt.Sprintf(
					"执行渠道状态探测失败: channel_id=%d run_id=%s err=%s",
					claim.Config.ChannelId, claim.RunId, err.Error(),
				))
			}
		})
	}
	return nil
}

func runChannelStatusProbeClaim(parent context.Context, claim model.ChannelStatusProbeClaim) error {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	gopool.Go(func() {
		ticker := time.NewTicker(channelStatusProbeLeaseRenewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				renewed, err := model.RenewChannelStatusProbeLease(claim, common.GetTimestamp())
				if err != nil {
					common.SysError(fmt.Sprintf("续租渠道状态探测失败: channel_id=%d err=%s", claim.Config.ChannelId, err.Error()))
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
		if err := model.CompleteChannelStatusProbeClaim(claim, common.GetTimestamp()); err != nil {
			common.SysError(fmt.Sprintf("完成渠道状态探测租约失败: channel_id=%d err=%s", claim.Config.ChannelId, err.Error()))
		}
	}()

	channel, err := model.GetChannelById(claim.Config.ChannelId, true)
	if err != nil {
		return err
	}
	if claim.Trigger == model.ChannelStatusProbeTriggerScheduled && channel.Status == common.ChannelStatusManuallyDisabled {
		return nil
	}
	testUserId, testUserErr := resolveChannelTestUserID(nil)
	for index, modelName := range claim.Models {
		if index > 0 && common.RequestInterval > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(common.RequestInterval):
			}
		}
		if ctx.Err() != nil {
			outcome := channelStatusProbeCanceledOutcome("探测租约已失效或服务正在停止")
			if err := persistChannelStatusProbeOutcome(channel, claim, modelName, outcome); err != nil {
				return err
			}
			continue
		}
		current, currentErr := model.IsChannelStatusProbeLeaseCurrent(claim, common.GetTimestamp())
		if currentErr != nil {
			return currentErr
		}
		if !current {
			cancel()
			outcome := channelStatusProbeCanceledOutcome("探测配置已变化，本轮剩余模型已取消")
			if err := persistChannelStatusProbeOutcome(channel, claim, modelName, outcome); err != nil {
				return err
			}
			continue
		}
		var outcome channelStatusProbeOutcome
		if testUserErr != nil {
			now := common.GetTimestamp()
			outcome = channelStatusProbeOutcome{
				Result: model.ChannelStatusProbeResultLocalFailure, StartedAt: now, FinishedAt: now,
				ErrorCode: "test_user_unavailable", ErrorMessage: common.MaskSensitiveInfo(testUserErr.Error()),
			}
		} else {
			outcome = executeChannelStatusProbeModel(ctx, channel, testUserId, modelName)
		}
		if err := persistChannelStatusProbeOutcome(channel, claim, modelName, outcome); err != nil {
			return err
		}
	}
	return nil
}

func channelStatusProbeCanceledOutcome(message string) channelStatusProbeOutcome {
	now := common.GetTimestamp()
	return channelStatusProbeOutcome{
		Result: model.ChannelStatusProbeResultCanceled, StartedAt: now, FinishedAt: now,
		ErrorCode: "probe_canceled", ErrorMessage: message,
	}
}

func executeChannelStatusProbeModel(
	ctx context.Context,
	channel *model.Channel,
	testUserId int,
	modelName string,
) channelStatusProbeOutcome {
	started := time.Now()
	startedAt := started.Unix()
	if _, err := normalizeChannelStatusProbeModels(channel, []string{modelName}); err != nil {
		return channelStatusProbeOutcome{
			Result: model.ChannelStatusProbeResultLocalFailure, StartedAt: startedAt, FinishedAt: common.GetTimestamp(),
			ErrorCode: "model_not_supported", ErrorMessage: common.MaskSensitiveInfo(err.Error()),
		}
	}
	if service.ChannelRateLimitCooldownUntilMatching(channel.Id, modelName) > 0 {
		return channelStatusProbeOutcome{
			Result: model.ChannelStatusProbeResultSkipped, StartedAt: startedAt, FinishedAt: common.GetTimestamp(),
			ErrorCode: "rate_limit_cooldown", ErrorMessage: "渠道模型仍处于 429 冷却期，本次未发送请求",
		}
	}
	probeCtx := withChannelStatusProbeTestContext(ctx)
	lease, acquired, _, err := service.AcquireChannelConcurrency(probeCtx, channel.Id)
	if err != nil {
		return channelStatusProbeOutcome{
			Result: model.ChannelStatusProbeResultLocalFailure, StartedAt: startedAt, FinishedAt: common.GetTimestamp(),
			ErrorCode: "concurrency_lease_failed", ErrorMessage: common.MaskSensitiveInfo(err.Error()),
		}
	}
	if !acquired {
		return channelStatusProbeOutcome{
			Result: model.ChannelStatusProbeResultSkipped, StartedAt: startedAt, FinishedAt: common.GetTimestamp(),
			ErrorCode: "channel_busy", ErrorMessage: "渠道并发已满，本次未发送请求",
		}
	}
	probeResult := testChannel(probeCtx, channel, testUserId, modelName, "", true)
	lease.Release()
	finished := time.Now()
	durationMs := float64(finished.Sub(started)) / float64(time.Millisecond)
	result := model.ChannelStatusProbeResultSuccess
	if isChannelSmartScheduleUpstreamRateLimit(probeResult) {
		result = model.ChannelStatusProbeResultRateLimited
	} else if probeResult.localErr != nil || probeResult.newAPIError != nil {
		if probeResult.requestDispatched {
			result = model.ChannelStatusProbeResultUpstreamFailure
		} else {
			result = model.ChannelStatusProbeResultLocalFailure
		}
	}
	errorCode := ""
	errorMessage := ""
	if probeResult.newAPIError != nil {
		errorCode = fmt.Sprint(probeResult.newAPIError.GetErrorCode())
		errorMessage = probeResult.newAPIError.MaskSensitiveErrorWithStatusCode()
	} else if probeResult.localErr != nil {
		errorCode = "probe_request_failed"
		errorMessage = common.MaskSensitiveInfo(probeResult.localErr.Error())
	}
	return channelStatusProbeOutcome{
		Result: result, StartedAt: startedAt, FinishedAt: finished.Unix(), DurationMs: &durationMs,
		ProbeResult: probeResult, TestExecuted: true, ErrorCode: errorCode, ErrorMessage: errorMessage,
	}
}

func persistChannelStatusProbeOutcome(
	channel *model.Channel,
	claim model.ChannelStatusProbeClaim,
	modelName string,
	outcome channelStatusProbeOutcome,
) error {
	errorRunes := []rune(strings.TrimSpace(outcome.ErrorMessage))
	if len(errorRunes) > 512 {
		errorRunes = errorRunes[:512]
	}
	errorCodeRunes := []rune(strings.TrimSpace(outcome.ErrorCode))
	if len(errorCodeRunes) > 128 {
		errorCodeRunes = errorCodeRunes[:128]
	}
	execution := model.ChannelStatusProbeExecution{
		RunId: claim.RunId, ChannelId: claim.Config.ChannelId, ModelName: modelName,
		ConfigRevision: claim.Config.Revision, Trigger: claim.Trigger, Result: outcome.Result,
		StartedAt: outcome.StartedAt, FinishedAt: outcome.FinishedAt, ResponseTimeMs: outcome.DurationMs,
		ErrorCode: string(errorCodeRunes), ErrorMessage: string(errorRunes),
		SampleRequested: claim.Config.RecordSample, CreatedAt: outcome.FinishedAt,
	}
	if claim.Config.RecordSample {
		execution.SampleStatus = model.ChannelStatusProbeSamplePending
		execution.SampleMessage = "等待写入智能调度样本"
	} else {
		execution.SampleStatus = model.ChannelStatusProbeSampleSkipped
		execution.SampleMessage = "未开启智能调度样本记录"
	}
	if outcome.TestExecuted {
		execution.RequestDispatched = outcome.ProbeResult.requestDispatched
		execution.FirstTokenMs = outcome.ProbeResult.firstResponseMilliseconds
		execution.TPS = outcome.ProbeResult.tokensPerSecond
		execution.UsageAvailable = outcome.ProbeResult.usageMetrics.available
		execution.InputTokens = outcome.ProbeResult.usageMetrics.inputTokens
		execution.OutputTokens = outcome.ProbeResult.usageMetrics.outputTokens
		execution.TotalTokens = outcome.ProbeResult.usageMetrics.totalTokens
		execution.CachedTokens = outcome.ProbeResult.usageMetrics.cachedTokens
		execution.CacheWriteTokens = outcome.ProbeResult.usageMetrics.cacheWriteTokens
		execution.ReasoningTokens = outcome.ProbeResult.usageMetrics.reasoningTokens
		if outcome.ProbeResult.context != nil {
			execution.RequestId = strings.TrimSpace(outcome.ProbeResult.context.GetString(common.RequestIdKey))
			execution.Stream = common.GetContextKeyBool(outcome.ProbeResult.context, constant.ContextKeyIsStream)
			if outcome.ProbeResult.context.Request != nil && outcome.ProbeResult.context.Request.URL != nil {
				execution.Endpoint = outcome.ProbeResult.context.Request.URL.Path
			}
		}
	}
	created, err := model.SaveChannelStatusProbeExecution(&execution)
	if err != nil || !created || !claim.Config.RecordSample {
		return err
	}
	if !outcome.TestExecuted {
		return model.UpdateChannelStatusProbeExecutionSample(
			execution.Id, model.ChannelStatusProbeSampleSkipped, "探测请求未发出，未计入样本", common.GetTimestamp(),
		)
	}
	durationMs := 0.0
	if outcome.DurationMs != nil {
		durationMs = *outcome.DurationMs
	}
	recorded, message := recordChannelStatusProbeSmartScheduleResult(
		channel,
		outcome.ProbeResult,
		durationMs,
		claim.RunId+":"+modelName,
		outcome.FinishedAt,
	)
	sampleStatus := channelStatusProbeSampleDecision(recorded, message)
	if sampleStatus == model.ChannelStatusProbeSamplePending {
		message += "，将在后台重试"
	}
	return model.UpdateChannelStatusProbeExecutionSample(execution.Id, sampleStatus, message, common.GetTimestamp())
}

func channelStatusProbeSampleDecision(recorded bool, message string) string {
	if recorded {
		return model.ChannelStatusProbeSampleRecorded
	}
	if strings.Contains(message, "保存失败") || strings.Contains(message, "读取失败") {
		return model.ChannelStatusProbeSamplePending
	}
	return model.ChannelStatusProbeSampleSkipped
}

func retryPendingChannelStatusProbeSamples(ctx context.Context) error {
	executions, err := model.ListPendingChannelStatusProbeExecutions(channelStatusProbeSampleRetryBatch)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	for _, execution := range executions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if execution.CreatedAt <= now-int64(channelStatusProbeSampleRetryMaxAge/time.Second) {
			if err := model.UpdateChannelStatusProbeExecutionSample(
				execution.Id,
				model.ChannelStatusProbeSampleFailed,
				"样本后台重试超过 24 小时，已停止重试",
				now,
			); err != nil {
				return err
			}
			continue
		}
		if !execution.RequestDispatched || execution.Result == model.ChannelStatusProbeResultLocalFailure ||
			execution.Result == model.ChannelStatusProbeResultSkipped ||
			execution.Result == model.ChannelStatusProbeResultCanceled {
			if err := model.UpdateChannelStatusProbeExecutionSample(
				execution.Id,
				model.ChannelStatusProbeSampleSkipped,
				"探测请求未形成有效上游样本",
				now,
			); err != nil {
				return err
			}
			continue
		}
		if execution.Result == model.ChannelStatusProbeResultRateLimited {
			if err := model.UpdateChannelStatusProbeExecutionSample(
				execution.Id,
				model.ChannelStatusProbeSampleSkipped,
				"上游返回 429，不计入稳定性样本",
				now,
			); err != nil {
				return err
			}
			continue
		}
		channel, err := model.GetChannelById(execution.ChannelId, true)
		if err != nil {
			return fmt.Errorf("读取待重试样本渠道失败: channel_id=%d: %w", execution.ChannelId, err)
		}
		result := testResult{
			requestDispatched:         execution.RequestDispatched,
			originalModelName:         execution.ModelName,
			firstResponseMilliseconds: execution.FirstTokenMs,
			tokensPerSecond:           execution.TPS,
		}
		if execution.Result == model.ChannelStatusProbeResultUpstreamFailure {
			message := strings.TrimSpace(execution.ErrorMessage)
			if message == "" {
				message = "上游探测失败"
			}
			result.newAPIError = relaytypes.NewError(errors.New(message), relaytypes.ErrorCode(execution.ErrorCode))
		}
		durationMs := 0.0
		if execution.ResponseTimeMs != nil {
			durationMs = *execution.ResponseTimeMs
		}
		recorded, message := recordChannelStatusProbeSmartScheduleResult(
			channel,
			result,
			durationMs,
			execution.RunId+":"+execution.ModelName,
			execution.FinishedAt,
		)
		status := channelStatusProbeSampleDecision(recorded, message)
		if status == model.ChannelStatusProbeSamplePending {
			message += "，将在后台重试"
		}
		if err := model.UpdateChannelStatusProbeExecutionSample(execution.Id, status, message, now); err != nil {
			return err
		}
	}
	return nil
}
