package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"gorm.io/gorm"
)

const (
	channelModelDetectionRuntimeInterval = 15 * time.Second
	channelModelDetectionPollInterval    = 3 * time.Second
	channelModelDetectionHealthInterval  = 30 * time.Second
)

type channelModelDetectionRuntime struct {
	tokens       *service.ChannelModelDetectorTokenStore
	worker       *service.ChannelModelDetectionWorker
	relayHandler *ChannelModelDetectorRelayHandler
}

type channelModelDetectionRuntimeResult struct {
	Schedule        service.ChannelModelDetectionScheduleResult `json:"schedule"`
	WorkerPasses    int                                         `json:"worker_passes"`
	LastRunID       string                                      `json:"last_run_id"`
	LastStatus      string                                      `json:"last_status"`
	DetectorState   string                                      `json:"detector_state"`
	DetectorError   string                                      `json:"detector_error,omitempty"`
	WaitingDetector bool                                        `json:"waiting_detector"`
}

var channelModelDetectionRuntimeState struct {
	sync.Once
	runtime *channelModelDetectionRuntime
	err     error
}

func getChannelModelDetectionRuntime() (*channelModelDetectionRuntime, error) {
	channelModelDetectionRuntimeState.Do(func() {
		tokens, err := service.GetChannelModelDetectorTokenStore()
		if err != nil {
			channelModelDetectionRuntimeState.err = err
			return
		}
		runtime := &channelModelDetectionRuntime{tokens: tokens}
		relay, err := service.NewChannelModelDetectorRelay(tokens, NewChannelModelDetectorFixedExecutor(nil))
		if err != nil {
			channelModelDetectionRuntimeState.err = err
			return
		}
		runtime.relayHandler = NewChannelModelDetectorRelayHandler(relay)
		runtime.worker = service.NewChannelModelDetectionWorker(nil, func(detectorURL string) (service.ChannelModelDetectionDetector, error) {
			if _, err := service.ValidateChannelModelDetectorTarget(context.Background(), detectorURL); err != nil {
				return nil, err
			}
			return service.NewChannelModelDetectorClient(detectorURL)
		}, runtime.issueCredential)
		service.SetChannelModelDetectionRunCancelerFactory(func(*gorm.DB) (service.ChannelModelDetectionRunCanceler, error) {
			return channelModelDetectionRuntimeCanceler{runtime: runtime}, nil
		})
		service.SetChannelModelDetectionConfigChangeHook(runtime.handleConfigChange)
		channelModelDetectionRuntimeState.runtime = runtime
	})
	return channelModelDetectionRuntimeState.runtime, channelModelDetectionRuntimeState.err
}

// GetChannelModelDetectorRelayHandler returns the single process-local Relay.
// Its bearer store is never exposed through a management endpoint.
func GetChannelModelDetectorRelayHandler() (*ChannelModelDetectorRelayHandler, error) {
	runtime, err := getChannelModelDetectionRuntime()
	if err != nil {
		return nil, err
	}
	return runtime.relayHandler, nil
}

func registerChannelModelDetectionRuntimeTask() {
	service.RegisterSystemTaskHandler(channelModelDetectionTaskHandler{})
	service.WarmChannelModelDetectionOverviewSnapshot()
}

type channelModelDetectionTaskHandler struct{}

func (channelModelDetectionTaskHandler) Type() string {
	return model.SystemTaskTypeChannelModelDetection
}

func (channelModelDetectionTaskHandler) Enabled() bool {
	db := model.DB
	if db == nil {
		return false
	}
	var config model.ChannelModelDetectionGlobalConfig
	if err := db.Select("detector_url", "pending_detector_url").Where("id = ?", model.ChannelModelDetectionConfigID).First(&config).Error; err == nil {
		if strings.TrimSpace(config.DetectorURL) != "" || strings.TrimSpace(config.PendingDetectorURL) != "" {
			return true
		}
	}
	var active int64
	if err := db.Model(&model.ChannelModelDetectionRun{}).
		Where("status IN ?", []string{
			model.ChannelModelDetectionRunStatusQueued,
			model.ChannelModelDetectionRunStatusWaitingDetector,
			model.ChannelModelDetectionRunStatusSubmitting,
			model.ChannelModelDetectionRunStatusRunning,
			model.ChannelModelDetectionRunStatusSubmissionUnknown,
			model.ChannelModelDetectionRunStatusCanceling,
		}).Count(&active).Error; err != nil {
		return false
	}
	return active > 0
}

func (channelModelDetectionTaskHandler) Interval() time.Duration {
	return channelModelDetectionRuntimeInterval
}

func (channelModelDetectionTaskHandler) NewPayload() any { return nil }

func (channelModelDetectionTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runtime, err := getChannelModelDetectionRuntime()
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	result := channelModelDetectionRuntimeResult{}
	result.Schedule, err = service.RunChannelModelDetectionScheduleOnce(ctx, nil, time.Now().UTC())
	if err != nil && !errors.Is(err, service.ErrChannelModelDetectionScheduleNotConfigured) {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}

workerLoop:
	for {
		workerResult, runErr := runtime.worker.RunOnce(ctx)
		if errors.Is(runErr, service.ErrChannelModelDetectionWorkerNoWork) {
			break
		}
		if workerResult.RunID != "" {
			service.NotifyChannelModelDetectionOverviewChanged()
		}
		result.WorkerPasses++
		result.LastRunID = workerResult.RunID
		result.LastStatus = workerResult.Status
		runtime.revokeTerminalCredential(workerResult)

		switch {
		case errors.Is(runErr, service.ErrChannelModelDetectionWorkerBusy):
			result.DetectorError = "模型检测 Worker 正由其他操作占用"
			break workerLoop
		case errors.Is(runErr, service.ErrChannelModelDetectionSubmissionUnknown):
			result.DetectorError = "官方检测器启动结果无法确认"
			break workerLoop
		case errors.Is(runErr, service.ErrChannelModelDetectionExternalSessionConflict):
			continue
		case runErr != nil:
			result.DetectorError = sanitizeChannelModelDetectionRuntimeError(runErr.Error())
			result.WaitingDetector = workerResult.Waiting
			break workerLoop
		case workerResult.Waiting:
			result.WaitingDetector = true
			break workerLoop
		case workerResult.Completed:
			continue
		}
		timer := time.NewTimer(channelModelDetectionPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, ctx.Err())
			return
		case <-timer.C:
		}
	}

	snapshot := service.ChannelModelDetectionServiceSnapshot("")
	var global model.ChannelModelDetectionGlobalConfig
	if model.DB != nil && model.DB.Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error == nil {
		snapshot = service.ChannelModelDetectionServiceSnapshot(global.DetectorURL)
		if strings.TrimSpace(global.DetectorURL) != "" && time.Now().Unix()-snapshot.LastCheckedAt >= int64(channelModelDetectionHealthInterval.Seconds()) {
			snapshot, _ = service.TestChannelModelDetectionService(ctx, nil, time.Now().UTC())
		}
	}
	result.DetectorState = snapshot.State
	if result.DetectorError == "" {
		result.DetectorError = snapshot.LastError
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}

func sanitizeChannelModelDetectionRuntimeError(message string) string {
	message = common.MaskSensitiveInfo(strings.TrimSpace(message))
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

func (runtime *channelModelDetectionRuntime) issueCredential(ctx context.Context, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution) (string, string, error) {
	if runtime == nil || runtime.tokens == nil || model.DB == nil {
		return "", "", errors.New("模型检测短期凭证服务不可用")
	}
	planned := execution.PlannedLogicalRequests
	if planned <= 0 || planned > math.MaxInt32 {
		return "", "", errors.New("模型检测 HTTP 尝试预算无效")
	}
	var recordedAttempts int64
	if err := model.DB.WithContext(ctx).Model(&model.ChannelModelDetectionCostEvent{}).
		Where("execution_id = ?", execution.Id).Count(&recordedAttempts).Error; err != nil {
		return "", "", err
	}
	used := recordedAttempts
	if execution.HTTPAttempts > used {
		used = execution.HTTPAttempts
	}
	remaining := planned - used
	if remaining <= 0 {
		return "", "", service.ErrChannelModelDetectorTokenBudgetExceeded
	}

	if run.PricingContextUserId <= 0 {
		var root model.User
		if err := model.DB.WithContext(ctx).Where("role = ?", common.RoleRootUser).Order("id ASC").First(&root).Error; err != nil {
			return "", "", errors.New("模型检测计价账号不可用")
		}
		run.PricingContextUserId = root.Id
		if err := model.DB.WithContext(ctx).Model(&model.ChannelModelDetectionRun{}).
			Where("run_id = ? AND pricing_context_user_id = ?", run.RunId, 0).
			Update("pricing_context_user_id", root.Id).Error; err != nil {
			return "", "", err
		}
	}

	relayBaseURL, err := service.ResolveChannelModelDetectionRelayBaseURL(ctx, model.DB)
	if err != nil {
		return "", "", err
	}
	ttl := 2 * time.Hour
	switch execution.Preset {
	case model.ChannelModelDetectionPresetMedium:
		ttl = 8 * time.Hour
	case model.ChannelModelDetectionPresetHigh:
		ttl = service.ChannelModelDetectorTokenMaxTTL
	}
	runtime.tokens.RevokeRunTarget(run.RunId, execution.TargetId)
	logicalMembers, err := run.LogicalMemberSnapshot()
	if err != nil {
		return "", "", err
	}
	if run.LogicalRevision > 0 && len(logicalMembers) == 0 {
		return "", "", service.ErrChannelModelDetectorRelayUnavailable
	}
	credential, err := runtime.tokens.Issue(service.ChannelModelDetectorTokenSpec{
		RunID: run.RunId, TargetID: execution.TargetId, ExecutionID: execution.Id,
		ChannelID: execution.ChannelId, RequestModel: execution.RequestModel,
		LogicalChannelID: run.LogicalChannelID, LogicalRevision: run.LogicalRevision,
		LogicalMembers: logicalMembers,
		ClaimedModel:   execution.ClaimedModel, Preset: execution.Preset,
		RelayBaseURL: relayBaseURL, MaxHTTPAttempts: int(remaining),
		ExpiresAt: time.Now().UTC().Add(ttl).Unix(),
	})
	if err != nil {
		return "", "", err
	}
	return credential.BearerToken(), relayBaseURL, nil
}

func (runtime *channelModelDetectionRuntime) revokeTerminalCredential(result service.ChannelModelDetectionWorkerResult) {
	if runtime == nil || runtime.tokens == nil || result.ClaimedExecutionID <= 0 || model.DB == nil {
		return
	}
	var execution model.ChannelModelDetectionExecution
	if err := model.DB.Select("run_id", "target_id", "status").Where("id = ?", result.ClaimedExecutionID).First(&execution).Error; err != nil {
		return
	}
	switch execution.Status {
	case model.ChannelModelDetectionExecutionStatusCompleted,
		model.ChannelModelDetectionExecutionStatusFailed,
		model.ChannelModelDetectionExecutionStatusCanceled,
		model.ChannelModelDetectionExecutionStatusSkipped:
		runtime.tokens.RevokeRunTarget(execution.RunId, execution.TargetId)
	}
}

func (runtime *channelModelDetectionRuntime) handleConfigChange(ctx context.Context, change service.ChannelModelDetectionConfigChange) {
	if runtime == nil || runtime.tokens == nil || runtime.worker == nil || model.DB == nil || change.ChannelID <= 0 {
		return
	}
	runID := ""
	if identity, identityErr := service.ResolveChannelLogicalIdentity(change.ChannelID); identityErr == nil && identity.Revision > 0 {
		var sharedRun model.ChannelModelDetectionRun
		if err := model.DB.WithContext(ctx).Select("run_id").Where("logical_channel_id = ? AND status IN ?", identity.LogicalChannelID, []string{model.ChannelModelDetectionRunStatusQueued, model.ChannelModelDetectionRunStatusWaitingDetector, model.ChannelModelDetectionRunStatusSubmitting, model.ChannelModelDetectionRunStatusRunning, model.ChannelModelDetectionRunStatusSubmissionUnknown, model.ChannelModelDetectionRunStatusCanceling}).Order("id DESC").First(&sharedRun).Error; err == nil {
			runID = sharedRun.RunId
		}
	} else {
		var config model.ChannelModelDetectionConfig
		if err := model.DB.WithContext(ctx).Select("running_run_id").Where("channel_id = ?", change.ChannelID).First(&config).Error; err == nil {
			runID = config.RunningRunId
		}
	}
	if runID == "" {
		return
	}
	var executions []model.ChannelModelDetectionExecution
	if err := model.DB.WithContext(ctx).Select("target_id").Where("run_id = ?", runID).Find(&executions).Error; err != nil {
		return
	}
	for _, execution := range executions {
		runtime.tokens.RevokeRunTarget(runID, execution.TargetId)
	}
	go func(runID string) {
		for attempt := 0; attempt < 3; attempt++ {
			if err := runtime.worker.CancelRun(context.Background(), runID); err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
				return
			} else if !errors.Is(err, service.ErrChannelModelDetectionWorkerBusy) {
				logger.LogWarn(context.Background(), fmt.Sprintf("模型检测配置变更后取消轮次失败: %v", err))
				return
			}
			time.Sleep(time.Second)
		}
		logger.LogWarn(context.Background(), "模型检测配置变更后取消轮次超时")
	}(runID)
}

type channelModelDetectionRuntimeCanceler struct {
	runtime *channelModelDetectionRuntime
}

func (canceler channelModelDetectionRuntimeCanceler) CancelRun(ctx context.Context, runID string) error {
	if canceler.runtime == nil || canceler.runtime.worker == nil {
		return errors.New("模型检测取消服务不可用")
	}
	if canceler.runtime.tokens != nil && model.DB != nil {
		var executions []model.ChannelModelDetectionExecution
		if err := model.DB.WithContext(ctx).Select("target_id").Where("run_id = ?", runID).Find(&executions).Error; err != nil {
			return err
		}
		for _, execution := range executions {
			canceler.runtime.tokens.RevokeRunTarget(runID, execution.TargetId)
		}
	}
	return canceler.runtime.worker.CancelRun(ctx, runID)
}
