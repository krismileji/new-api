package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelModelDetectionDetectorStub struct {
	mu                    sync.Mutex
	bootstrap             ChannelModelDetectorBootstrapResponse
	estimate              ChannelModelDetectorEstimateResponse
	statuses              []ChannelModelDetectorStatusResponse
	report                ChannelModelDetectorReportResponse
	start                 ChannelModelDetectorStartResponse
	startErr              error
	statusErr             error
	stopRequiresBootstrap bool
	startHook             func(ChannelModelDetectorStartRequest)
	bootstrapCalls        int
	startCalls            int
	stopCalls             int
	startRequests         []ChannelModelDetectorStartRequest
}

func (stub *channelModelDetectionDetectorStub) Bootstrap(context.Context) (ChannelModelDetectorBootstrapResponse, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.bootstrapCalls++
	return stub.bootstrap, nil
}

func (stub *channelModelDetectionDetectorStub) Estimate(context.Context, ChannelModelDetectorPresetConfig) (ChannelModelDetectorEstimateResponse, error) {
	return stub.estimate, nil
}

func (stub *channelModelDetectionDetectorStub) Start(_ context.Context, request ChannelModelDetectorStartRequest) (ChannelModelDetectorStartResponse, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.startCalls++
	stub.startRequests = append(stub.startRequests, request)
	if stub.startHook != nil {
		stub.startHook(request)
	}
	return stub.start, stub.startErr
}

func (stub *channelModelDetectionDetectorStub) Status(context.Context) (ChannelModelDetectorStatusResponse, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.statusErr != nil {
		return ChannelModelDetectorStatusResponse{}, stub.statusErr
	}
	if len(stub.statuses) == 0 {
		return ChannelModelDetectorStatusResponse{Status: "idle"}, nil
	}
	status := stub.statuses[0]
	if len(stub.statuses) > 1 {
		stub.statuses = stub.statuses[1:]
	}
	return status, nil
}

func (stub *channelModelDetectionDetectorStub) Report(context.Context) (ChannelModelDetectorReportResponse, error) {
	return stub.report, nil
}

func (stub *channelModelDetectionDetectorStub) Stop(context.Context) (ChannelModelDetectorStopResponse, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.stopCalls++
	if stub.stopRequiresBootstrap && stub.bootstrapCalls == 0 {
		return ChannelModelDetectorStopResponse{}, errors.New("请先获取检测器 bootstrap 会话")
	}
	return ChannelModelDetectorStopResponse{}, nil
}

func presetForChannelModelDetectionWorkerTest(t *testing.T, hash string) ChannelModelDetectorPresetConfig {
	t.Helper()
	raw := detectorContractPreset(t, hash)
	return raw
}

func seedChannelModelDetectionWorkerRun(t *testing.T, db *gorm.DB, now time.Time, sessionID string, status string) (model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) {
	t.Helper()
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://127.0.0.1:18080", ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		ScheduleTime: "02:30", Timezone: "Asia/Shanghai", IntervalHours: 24,
	}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: 501}
	require.NoError(t, db.Create(&config).Error)
	target := model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: config.ChannelId, RequestModel: "channel-alias",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true,
	}
	require.NoError(t, db.Create(&target).Error)
	run := model.ChannelModelDetectionRun{
		ChannelId: config.ChannelId, ConfigRevision: config.Revision, GlobalConfigRevision: 1,
		Trigger: model.ChannelModelDetectionTriggerManual, Preset: model.ChannelModelDetectionPresetMedium,
		Status: model.ChannelModelDetectionRunStatusQueued, TargetCount: 1,
		QueuedAt: now.Unix(), CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	created, err := model.CreateChannelModelDetectionRun(db, &run)
	require.NoError(t, err)
	require.True(t, created)
	execution := model.ChannelModelDetectionExecution{
		RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: target.ChannelId,
		RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: run.Preset,
		Status: status, OfficialSessionId: sessionID,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	}
	require.NoError(t, db.Create(&execution).Error)
	return run, execution
}

func TestChannelModelDetectionWorkerStartsOneSessionAndFreezesOfficialConfig(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 6, 0, 0, 0, time.UTC)
	_, execution := seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusPending)
	preset := presetForChannelModelDetectionWorkerTest(t, "worker-hash")
	totalRequests := int64(14)
	stub := &channelModelDetectionDetectorStub{
		bootstrap: ChannelModelDetectorBootstrapResponse{SinglePresets: map[string]ChannelModelDetectorPresetConfig{
			model.ChannelModelDetectionPresetMedium: preset,
		}},
		estimate: ChannelModelDetectorEstimateResponse{TotalRequests: &totalRequests, ConfigHash: "worker-hash"},
		statuses: []ChannelModelDetectorStatusResponse{{Status: "idle", SessionID: "old-session"}},
		start:    ChannelModelDetectorStartResponse{Started: true, SessionID: "official-session", ConfigHash: "worker-hash"},
	}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, func(context.Context, model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) (string, string, error) {
		return "task-secret", "https://relay.example/internal/model-detector/v1", nil
	})
	worker.Now = func() time.Time { return now }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Started)
	assert.Equal(t, "official-session", result.OfficialSessionID)
	assert.Equal(t, 1, stub.startCalls)
	require.Len(t, stub.startRequests, 1)
	assert.Empty(t, stub.startRequests[0].PreviousSessionID)
	assert.Equal(t, "task-secret", stub.startRequests[0].APIKey)
	assert.Equal(t, execution.ClaimedModel, stub.startRequests[0].ClaimedModel)
	assert.Equal(t, execution.RequestModel, stub.startRequests[0].RequestModel)

	var stored model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&stored, execution.Id).Error)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusRunning, stored.Status)
	assert.Equal(t, "worker-hash", stored.ConfigHash)
	assert.NotEmpty(t, stored.OfficialConfigJSON)
	assert.NotContains(t, stored.OfficialConfigJSON, "task-secret")
	assert.Equal(t, int64(14), stored.PlannedLogicalRequests)
}

func TestChannelModelDetectionWorkerSubmissionUnknownStopsAutomaticRedispatch(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 6, 30, 0, 0, time.UTC)
	_, _ = seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusPending)
	preset := presetForChannelModelDetectionWorkerTest(t, "unknown-hash")
	totalRequests := int64(14)
	stub := &channelModelDetectionDetectorStub{
		bootstrap: ChannelModelDetectorBootstrapResponse{SinglePresets: map[string]ChannelModelDetectorPresetConfig{model.ChannelModelDetectionPresetMedium: preset}},
		estimate:  ChannelModelDetectorEstimateResponse{TotalRequests: &totalRequests, ConfigHash: "unknown-hash"},
		statuses:  []ChannelModelDetectorStatusResponse{{Status: "idle"}},
		startErr:  ErrChannelModelDetectorSubmissionUnknown,
	}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, func(context.Context, model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) (string, string, error) {
		return "task-secret", "https://relay.example/internal/model-detector/v1", nil
	})
	worker.Now = func() time.Time { return now }

	_, err := worker.RunOnce(context.Background())
	assert.ErrorIs(t, err, ErrChannelModelDetectionSubmissionUnknown)
	assert.Equal(t, 1, stub.startCalls)

	_, err = worker.RunOnce(context.Background())
	assert.ErrorIs(t, err, ErrChannelModelDetectionSubmissionUnknown)
	assert.Equal(t, 1, stub.startCalls)

	var run model.ChannelModelDetectionRun
	require.NoError(t, db.First(&run).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusSubmissionUnknown, run.Status)
}

func TestChannelModelDetectionRecoveryAdoptsMatchingUnknownSubmissionWithoutRestart(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 6, 45, 0, 0, time.UTC)
	run, execution := seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusSubmitting)
	preset := presetForChannelModelDetectionWorkerTest(t, "adopt-hash")
	require.NoError(t, execution.SetOfficialConfig(preset))
	execution.ConfigHash = "adopt-hash"
	require.NoError(t, db.Model(&execution).Updates(map[string]any{"official_config_json": execution.OfficialConfigJSON, "config_hash": execution.ConfigHash}).Error)
	require.NoError(t, db.Model(&run).Updates(map[string]any{"status": model.ChannelModelDetectionRunStatusSubmissionUnknown}).Error)
	stub := &channelModelDetectionDetectorStub{statuses: []ChannelModelDetectorStatusResponse{{
		Status: "running", SessionID: "adopted-session", ConfigHash: "adopt-hash", ClaimedModel: execution.ClaimedModel,
	}}}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, nil)
	worker.Now = func() time.Time { return now.Add(time.Minute) }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "adopted-session", result.OfficialSessionID)
	assert.Zero(t, stub.startCalls)
	var stored model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&stored, execution.Id).Error)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusRunning, stored.Status)
}

func TestChannelModelDetectionRecoveryRejectsExternalSessionWithoutTakingItOver(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 7, 0, 0, 0, time.UTC)
	_, execution := seedChannelModelDetectionWorkerRun(t, db, now, "owned-session", model.ChannelModelDetectionExecutionStatusRunning)
	stub := &channelModelDetectionDetectorStub{statuses: []ChannelModelDetectorStatusResponse{{Status: "running", SessionID: "external-session"}}}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, nil)
	worker.Now = func() time.Time { return now }

	_, err := worker.RunOnce(context.Background())
	assert.ErrorIs(t, err, ErrChannelModelDetectionExternalSessionConflict)
	assert.Zero(t, stub.startCalls)

	var stored model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&stored, execution.Id).Error)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusFailed, stored.Status)
	assert.Equal(t, "external_session_conflict", stored.ErrorCode)
}

func TestChannelModelDetectionRecoveryResumesOnlyMatchingInterruptedSession(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 7, 30, 0, 0, time.UTC)
	_, execution := seedChannelModelDetectionWorkerRun(t, db, now, "owned-session", model.ChannelModelDetectionExecutionStatusRunning)
	preset := presetForChannelModelDetectionWorkerTest(t, "resume-hash")
	execution.ConfigHash = "resume-hash"
	require.NoError(t, execution.SetOfficialConfig(preset))
	require.NoError(t, db.Model(&execution).Updates(map[string]any{"config_hash": execution.ConfigHash, "official_config_json": execution.OfficialConfigJSON}).Error)
	stub := &channelModelDetectionDetectorStub{
		statuses: []ChannelModelDetectorStatusResponse{{
			Status: "interrupted", SessionID: "owned-session", ConfigHash: "resume-hash", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
		}},
		start: ChannelModelDetectorStartResponse{Started: true, SessionID: "owned-session", ConfigHash: "resume-hash"},
	}
	issued := 0
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, func(context.Context, model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) (string, string, error) {
		issued++
		return "new-task-secret", "https://relay.example/internal/model-detector/v1", nil
	})
	worker.Now = func() time.Time { return now }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Resumed)
	assert.Equal(t, 1, issued)
	require.Len(t, stub.startRequests, 1)
	assert.Equal(t, "owned-session", stub.startRequests[0].ResumeSessionID)
	assert.Equal(t, "owned-session", stub.startRequests[0].PreviousSessionID)
}

func TestChannelModelDetectionRecoveryReadsOnlyMatchingReportAndCompletesRun(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 8, 0, 0, 0, time.UTC)
	run, execution := seedChannelModelDetectionWorkerRun(t, db, now, "owned-session", model.ChannelModelDetectionExecutionStatusRunning)
	require.NoError(t, db.Model(&execution).Updates(map[string]any{
		"planned_logical_requests":   49,
		"completed_logical_requests": 49,
		"http_attempts":              52,
		"retry_count":                3,
	}).Error)
	execution.ConfigHash = "report-hash"
	require.NoError(t, db.Model(&execution).Update("config_hash", execution.ConfigHash).Error)
	report := ChannelModelDetectorReportResponse{
		SessionID: "owned-session", ConfigHash: "report-hash", OutcomeCode: "juice_pass_fingerprint_strong",
		OverallVerdict: "通过", TitleCN: "Juice通过；指纹强烈指向 Sol", SubtitleCN: "检测器原始说明",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, RequestModel: execution.RequestModel,
		JuiceVerdictState: "pass", FingerprintVerdictState: "strong_match",
		FingerprintModel: model.ChannelModelDetectionClaimedModelLuna,
	}
	stub := &channelModelDetectionDetectorStub{
		statuses: []ChannelModelDetectorStatusResponse{{Status: "complete", SessionID: "owned-session", ConfigHash: "report-hash"}},
		report:   report,
	}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, nil)
	worker.Now = func() time.Time { return now.Add(time.Minute) }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Completed)
	var storedRun model.ChannelModelDetectionRun
	require.NoError(t, db.Where("run_id = ?", run.RunId).First(&storedRun).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCompleted, storedRun.Status)
	assert.Equal(t, int64(49), storedRun.PlannedLogicalRequests)
	assert.Equal(t, int64(49), storedRun.CompletedLogicalRequests)
	assert.Equal(t, int64(52), storedRun.HTTPAttempts)
	assert.Equal(t, int64(3), storedRun.RetryCount)
	assert.Positive(t, storedRun.FinishedAt)
	var storedExecution model.ChannelModelDetectionExecution
	require.NoError(t, db.Where("id = ?", execution.Id).First(&storedExecution).Error)
	assert.Equal(t, "pass", storedExecution.JuiceVerdictState)
	assert.Equal(t, "strong_match", storedExecution.FingerprintVerdictState)
	assert.Equal(t, model.ChannelModelDetectionClaimedModelLuna, storedExecution.FingerprintModel)
	assert.Equal(t, "Juice通过；指纹强烈指向 Sol", storedExecution.TitleCN)
	assert.Equal(t, "检测器原始说明", storedExecution.SubtitleCN)
	assert.Zero(t, storedExecution.SchemaVersion)
	assert.Empty(t, storedExecution.ErrorCode)
	var config model.ChannelModelDetectionConfig
	require.NoError(t, db.Where("channel_id = ?", run.ChannelId).First(&config).Error)
	assert.Empty(t, config.RunningRunId)
}

func TestChannelModelDetectionRecoveryAcceptsNewerReportSchemaAndCompletesRun(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	run, execution := seedChannelModelDetectionWorkerRun(t, db, now, "future-session", model.ChannelModelDetectionExecutionStatusRunning)
	execution.ConfigHash = "future-report-hash"
	require.NoError(t, db.Model(&execution).Update("config_hash", execution.ConfigHash).Error)
	schemaVersion := int64(5)
	report := ChannelModelDetectorReportResponse{
		SessionID: "future-session", ConfigHash: execution.ConfigHash, SchemaVersion: &schemaVersion,
		OutcomeCode: "juice_pass_fingerprint_strong", OverallVerdict: "未来版本结论",
		ClaimedModel: model.ChannelModelDetectionClaimedModelSol, RequestModel: execution.RequestModel,
	}
	stub := &channelModelDetectionDetectorStub{
		statuses: []ChannelModelDetectorStatusResponse{{Status: "complete", SessionID: "future-session", ConfigHash: execution.ConfigHash}},
		report:   report,
	}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, nil)
	worker.Now = func() time.Time { return now.Add(time.Minute) }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Completed)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusCompleted, result.Status)

	var storedExecution model.ChannelModelDetectionExecution
	require.NoError(t, db.Where("id = ?", execution.Id).First(&storedExecution).Error)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusCompleted, storedExecution.Status)
	assert.Empty(t, storedExecution.ErrorCode)
	assert.Equal(t, "juice_pass_fingerprint_strong", storedExecution.OutcomeCode)
	assert.NotEmpty(t, storedExecution.ReportJSON)
	assert.Equal(t, 5, storedExecution.SchemaVersion)

	var storedRun model.ChannelModelDetectionRun
	require.NoError(t, db.Where("run_id = ?", run.RunId).First(&storedRun).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCompleted, storedRun.Status)
}

func TestChannelModelDetectionRecoveryRejectsReportWithDifferentRequestModel(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 17, 8, 30, 0, 0, time.UTC)
	run, execution := seedChannelModelDetectionWorkerRun(t, db, now, "legacy-session", model.ChannelModelDetectionExecutionStatusRunning)
	execution.ConfigHash = "legacy-report-hash"
	require.NoError(t, db.Model(&execution).Update("config_hash", execution.ConfigHash).Error)
	schemaVersion := int64(3)
	report := ChannelModelDetectorReportResponse{
		SessionID: "legacy-session", ConfigHash: execution.ConfigHash, SchemaVersion: &schemaVersion,
		OutcomeCode: "juice_pass_fingerprint_strong", OverallVerdict: "通过",
		ClaimedModel: execution.ClaimedModel, RequestModel: execution.ClaimedModel,
	}
	stub := &channelModelDetectionDetectorStub{
		statuses: []ChannelModelDetectorStatusResponse{{Status: "complete", SessionID: "legacy-session", ConfigHash: execution.ConfigHash}},
		report:   report,
	}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, nil)
	worker.Now = func() time.Time { return now.Add(time.Minute) }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Completed)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusFailed, result.Status)

	var storedExecution model.ChannelModelDetectionExecution
	require.NoError(t, db.Where("id = ?", execution.Id).First(&storedExecution).Error)
	assert.Equal(t, "report_contract_incompatible", storedExecution.ErrorCode)
	assert.Contains(t, storedExecution.ErrorMessage, "request_model")
	assert.Contains(t, storedExecution.ErrorMessage, "可能不支持独立请求模型")
	assert.Empty(t, storedExecution.OutcomeCode)

	var storedRun model.ChannelModelDetectionRun
	require.NoError(t, db.Where("run_id = ?", run.RunId).First(&storedRun).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusFailed, storedRun.Status)
}

func TestChannelModelDetectionLeaseIsSingleSessionAndRenewable(t *testing.T) {
	worker := NewChannelModelDetectionWorker(nil, nil, nil)
	now := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)
	token, ok := worker.TryAcquireLease(now)
	require.True(t, ok)
	assert.NotEmpty(t, token)
	_, ok = worker.TryAcquireLease(now.Add(time.Second))
	assert.False(t, ok)
	assert.False(t, worker.RenewLease("wrong-token", now.Add(time.Second)))
	assert.True(t, worker.RenewLease(token, now.Add(time.Second)))
	_, ok = worker.TryAcquireLease(now.Add(channelModelDetectionWorkerLeaseDuration))
	assert.False(t, ok)
	_, ok = worker.TryAcquireLease(now.Add(2*channelModelDetectionWorkerLeaseDuration + 4*time.Second))
	assert.True(t, ok)
}

func TestChannelModelDetectionWorkerDBLeasePreventsConcurrentStart(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 9, 15, 0, 0, time.UTC)
	_, _ = seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusPending)
	preset := presetForChannelModelDetectionWorkerTest(t, "db-lease-hash")
	totalRequests := int64(14)
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	stub := &channelModelDetectionDetectorStub{
		bootstrap: ChannelModelDetectorBootstrapResponse{SinglePresets: map[string]ChannelModelDetectorPresetConfig{
			model.ChannelModelDetectionPresetMedium: preset,
		}},
		estimate: ChannelModelDetectorEstimateResponse{TotalRequests: &totalRequests, ConfigHash: "db-lease-hash"},
		start:    ChannelModelDetectorStartResponse{Started: true, SessionID: "db-lease-session", ConfigHash: "db-lease-hash"},
		startHook: func(ChannelModelDetectorStartRequest) {
			close(startEntered)
			<-releaseStart
		},
	}
	newWorker := func() *ChannelModelDetectionWorker {
		worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, func(context.Context, model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) (string, string, error) {
			return "task-secret", "https://relay.example/internal/model-detector/v1", nil
		})
		worker.Now = func() time.Time { return now }
		return worker
	}
	firstResult := make(chan error, 1)
	go func() {
		_, err := newWorker().RunOnce(context.Background())
		firstResult <- err
	}()
	<-startEntered

	_, secondErr := newWorker().RunOnce(context.Background())
	assert.ErrorIs(t, secondErr, ErrChannelModelDetectionWorkerBusy)
	close(releaseStart)
	require.NoError(t, <-firstResult)
	assert.Equal(t, 1, stub.startCalls)
}

func TestChannelModelDetectionWorkerDBLeaseFencesStaleCompletion(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 9, 30, 0, 0, time.UTC)
	_, execution := seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusPending)
	preset := presetForChannelModelDetectionWorkerTest(t, "stale-owner-hash")
	totalRequests := int64(14)
	stub := &channelModelDetectionDetectorStub{
		bootstrap: ChannelModelDetectorBootstrapResponse{SinglePresets: map[string]ChannelModelDetectorPresetConfig{
			model.ChannelModelDetectionPresetMedium: preset,
		}},
		estimate: ChannelModelDetectorEstimateResponse{TotalRequests: &totalRequests, ConfigHash: "stale-owner-hash"},
		start:    ChannelModelDetectorStartResponse{Started: true, SessionID: "stale-owner-session", ConfigHash: "stale-owner-hash"},
		startHook: func(ChannelModelDetectorStartRequest) {
			require.NoError(t, db.Model(&model.ChannelModelDetectionGlobalConfig{}).
				Where("id = ?", model.ChannelModelDetectionConfigID).
				Updates(map[string]any{
					"worker_lease_token": "new-owner",
					"worker_lease_until": now.Add(channelModelDetectionWorkerLeaseDuration).Unix(),
				}).Error)
		},
	}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, func(context.Context, model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) (string, string, error) {
		return "task-secret", "https://relay.example/internal/model-detector/v1", nil
	})
	worker.Now = func() time.Time { return now }

	_, err := worker.RunOnce(context.Background())
	assert.ErrorIs(t, err, ErrChannelModelDetectionWorkerBusy)
	var stored model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&stored, execution.Id).Error)
	assert.Equal(t, model.ChannelModelDetectionExecutionStatusSubmitting, stored.Status)
	assert.Empty(t, stored.OfficialSessionId)
	assert.NotEmpty(t, stored.DetectorURLSnapshot)
	var global model.ChannelModelDetectionGlobalConfig
	require.NoError(t, db.First(&global, model.ChannelModelDetectionConfigID).Error)
	assert.Equal(t, "new-owner", global.WorkerLeaseToken)
}

func TestChannelModelDetectionDetectorURLSnapshotSurvivesGlobalChange(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 9, 45, 0, 0, time.UTC)
	_, execution := seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusPending)
	preset := presetForChannelModelDetectionWorkerTest(t, "url-snapshot-hash")
	totalRequests := int64(14)
	stub := &channelModelDetectionDetectorStub{
		bootstrap: ChannelModelDetectorBootstrapResponse{SinglePresets: map[string]ChannelModelDetectorPresetConfig{
			model.ChannelModelDetectionPresetMedium: preset,
		}},
		estimate: ChannelModelDetectorEstimateResponse{TotalRequests: &totalRequests, ConfigHash: "url-snapshot-hash"},
		start:    ChannelModelDetectorStartResponse{Started: true, SessionID: "url-snapshot-session", ConfigHash: "url-snapshot-hash"},
	}
	var factoryMu sync.Mutex
	factoryURLs := make([]string, 0, 2)
	factory := func(baseURL string) (ChannelModelDetectionDetector, error) {
		factoryMu.Lock()
		factoryURLs = append(factoryURLs, baseURL)
		factoryMu.Unlock()
		return stub, nil
	}
	issuer := func(context.Context, model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) (string, string, error) {
		return "task-secret", "https://relay.example/internal/model-detector/v1", nil
	}
	worker := NewChannelModelDetectionWorker(db, factory, issuer)
	worker.Now = func() time.Time { return now }
	_, err := worker.RunOnce(context.Background())
	require.NoError(t, err)

	require.NoError(t, db.Model(&model.ChannelModelDetectionGlobalConfig{}).
		Where("id = ?", model.ChannelModelDetectionConfigID).
		Update("detector_url", "http://127.0.0.1:18081").Error)
	stub.mu.Lock()
	stub.statuses = []ChannelModelDetectorStatusResponse{{
		Status: "running", SessionID: "url-snapshot-session", ConfigHash: "url-snapshot-hash",
	}}
	stub.mu.Unlock()
	worker = NewChannelModelDetectionWorker(db, factory, issuer)
	worker.Now = func() time.Time { return now.Add(time.Minute) }
	_, err = worker.RunOnce(context.Background())
	require.NoError(t, err)

	var stored model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&stored, execution.Id).Error)
	assert.Equal(t, "http://127.0.0.1:18080", stored.DetectorURLSnapshot)
	factoryMu.Lock()
	assert.Equal(t, []string{"http://127.0.0.1:18080", "http://127.0.0.1:18080"}, factoryURLs)
	factoryMu.Unlock()
}

func TestChannelModelDetectionWorkerKeepsUnavailableDetectorAsInfrastructureState(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 10, 0, 0, 0, time.UTC)
	_, _ = seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusPending)
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) {
		return nil, errors.New("dial failed")
	}, nil)
	worker.Now = func() time.Time { return now }

	result, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Waiting)
	assert.Equal(t, model.ChannelModelDetectionRunStatusWaitingDetector, result.Status)
}

func TestChannelModelDetectionWorkerCancelQueuedRunDoesNotCallDetector(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 10, 30, 0, 0, time.UTC)
	run, _ := seedChannelModelDetectionWorkerRun(t, db, now, "", model.ChannelModelDetectionExecutionStatusPending)
	stub := &channelModelDetectionDetectorStub{}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, nil)
	worker.Now = func() time.Time { return now }

	require.NoError(t, worker.CancelRun(context.Background(), run.RunId))
	require.NoError(t, worker.CancelRun(context.Background(), run.RunId))
	assert.Zero(t, stub.startCalls)
	var stored model.ChannelModelDetectionRun
	require.NoError(t, db.Where("run_id = ?", run.RunId).First(&stored).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCanceled, stored.Status)
}

func TestChannelModelDetectionWorkerCancelRunningRunBootstrapsBeforeStop(t *testing.T) {
	db := setupChannelModelDetectionSchedulerTestDB(t)
	now := time.Date(2026, time.August, 13, 10, 45, 0, 0, time.UTC)
	run, _ := seedChannelModelDetectionWorkerRun(t, db, now, "owned-session", model.ChannelModelDetectionExecutionStatusRunning)
	stub := &channelModelDetectionDetectorStub{
		statuses:              []ChannelModelDetectorStatusResponse{{Status: "running", SessionID: "owned-session"}},
		stopRequiresBootstrap: true,
	}
	worker := NewChannelModelDetectionWorker(db, func(string) (ChannelModelDetectionDetector, error) { return stub, nil }, nil)
	worker.Now = func() time.Time { return now }

	require.NoError(t, worker.CancelRun(context.Background(), run.RunId))
	assert.Equal(t, 1, stub.bootstrapCalls)
	assert.Equal(t, 1, stub.stopCalls)

	var stored model.ChannelModelDetectionRun
	require.NoError(t, db.Where("run_id = ?", run.RunId).First(&stored).Error)
	assert.Equal(t, model.ChannelModelDetectionRunStatusCanceled, stored.Status)
}
