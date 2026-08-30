package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	channelModelDetectionWorkerLeaseDuration = time.Duration(model.ChannelModelDetectionWorkerLeaseSeconds) * time.Second
	channelModelDetectionWorkerErrorLimit    = 512
)

type channelModelDetectionWorkerLeaseContextKey struct{}

var (
	ErrChannelModelDetectionWorkerBusy              = errors.New("模型检测 Worker 已有活动会话")
	ErrChannelModelDetectionWorkerNoWork            = errors.New("模型检测 Worker 没有待处理任务")
	ErrChannelModelDetectionExternalSessionConflict = errors.New("官方检测器会话归属冲突")
	ErrChannelModelDetectionSubmissionUnknown       = errors.New("官方检测器启动结果无法确认")
	ErrChannelModelDetectionRecoveryRejected        = errors.New("官方检测器拒绝恢复原会话")
)

// ChannelModelDetectionDetector is the narrow official HTTP boundary needed
// by the worker. It keeps tests deterministic and prevents recovery logic from
// reading detector storage or implementation details.
type ChannelModelDetectionDetector interface {
	Bootstrap(context.Context) (ChannelModelDetectorBootstrapResponse, error)
	Estimate(context.Context, ChannelModelDetectorPresetConfig) (ChannelModelDetectorEstimateResponse, error)
	Start(context.Context, ChannelModelDetectorStartRequest) (ChannelModelDetectorStartResponse, error)
	Status(context.Context) (ChannelModelDetectorStatusResponse, error)
	Report(context.Context) (ChannelModelDetectorReportResponse, error)
	Stop(context.Context) (ChannelModelDetectorStopResponse, error)
}

// ChannelModelDetectionCredentialIssuer mints a new in-memory task credential
// for every initial start or interrupted-session resume. The returned secret is
// never included in the worker result or persisted execution fields.
type ChannelModelDetectionCredentialIssuer func(context.Context, model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) (credential, baseURL string, err error)

// ChannelModelDetectionWorkerResult is a single, bounded worker pass. Polling
// is performed by later passes so ownership can be re-established after a
// process restart without holding an in-memory goroutine as the source of truth.
type ChannelModelDetectionWorkerResult struct {
	ClaimedExecutionID int64
	RunID              string
	Status             string
	OfficialSessionID  string
	Started            bool
	Resumed            bool
	Completed          bool
	Waiting            bool
}

type ChannelModelDetectionWorker struct {
	DB              *gorm.DB
	DetectorFactory func(string) (ChannelModelDetectionDetector, error)
	IssueCredential ChannelModelDetectionCredentialIssuer
	Now             func() time.Time

	mu         sync.Mutex
	leaseToken string
	leaseUntil time.Time
}

func NewChannelModelDetectionWorker(db *gorm.DB, factory func(string) (ChannelModelDetectionDetector, error), issuer ChannelModelDetectionCredentialIssuer) *ChannelModelDetectionWorker {
	return &ChannelModelDetectionWorker{DB: db, DetectorFactory: factory, IssueCredential: issuer, Now: time.Now}
}

// RenewLease extends this process-local claim. RunOnce also holds a database
// lease so separate application instances cannot operate the detector together.
func (worker *ChannelModelDetectionWorker) RenewLease(token string, now time.Time) bool {
	if worker == nil || strings.TrimSpace(token) == "" {
		return false
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.leaseToken != token || !worker.leaseUntil.After(now) {
		return false
	}
	worker.leaseUntil = now.Add(channelModelDetectionWorkerLeaseDuration)
	return true
}

func (worker *ChannelModelDetectionWorker) TryAcquireLease(now time.Time) (string, bool) {
	if worker == nil {
		return "", false
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.leaseToken != "" && worker.leaseUntil.After(now) {
		return "", false
	}
	worker.leaseToken = common.GetUUID()
	worker.leaseUntil = now.Add(channelModelDetectionWorkerLeaseDuration)
	return worker.leaseToken, true
}

func (worker *ChannelModelDetectionWorker) releaseLease(token string) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.leaseToken == token {
		worker.leaseToken = ""
		worker.leaseUntil = time.Time{}
	}
}

func (worker *ChannelModelDetectionWorker) now() time.Time {
	now := time.Now().UTC()
	if worker != nil && worker.Now != nil {
		now = worker.Now().UTC()
	}
	return now
}

func channelModelDetectionWorkerLeaseToken(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(channelModelDetectionWorkerLeaseContextKey{}).(string)
	return strings.TrimSpace(token)
}

func (worker *ChannelModelDetectionWorker) withDBLeaseTransaction(ctx context.Context, db *gorm.DB, operation func(*gorm.DB) error) error {
	leaseToken := channelModelDetectionWorkerLeaseToken(ctx)
	if leaseToken == "" {
		return ErrChannelModelDetectionWorkerBusy
	}
	now := worker.now()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		global := model.ChannelModelDetectionGlobalConfig{Id: model.ChannelModelDetectionConfigID}
		renewed, err := global.RenewWorkerLease(tx, now.Unix(), leaseToken)
		if err != nil {
			return err
		}
		if !renewed {
			return ErrChannelModelDetectionWorkerBusy
		}
		if operation == nil {
			return nil
		}
		return operation(tx)
	})
}

func (worker *ChannelModelDetectionWorker) renewDBLease(ctx context.Context, db *gorm.DB) error {
	return worker.withDBLeaseTransaction(ctx, db, nil)
}

func (worker *ChannelModelDetectionWorker) RunOnce(ctx context.Context) (ChannelModelDetectionWorkerResult, error) {
	if worker == nil {
		return ChannelModelDetectionWorkerResult{}, ErrChannelModelDetectionWorkerNoWork
	}
	db := worker.DB
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return ChannelModelDetectionWorkerResult{}, errors.New("模型检测 Worker 依赖未配置")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if worker.DetectorFactory == nil {
		return ChannelModelDetectionWorkerResult{}, errors.New("模型检测器客户端工厂未配置")
	}
	now := worker.now()
	leaseToken, acquired := worker.TryAcquireLease(now)
	if !acquired {
		return ChannelModelDetectionWorkerResult{}, ErrChannelModelDetectionWorkerBusy
	}
	defer worker.releaseLease(leaseToken)

	global := model.ChannelModelDetectionGlobalConfig{Id: model.ChannelModelDetectionConfigID}
	claimed, err := global.TryAcquireWorkerLease(db.WithContext(ctx), now.Unix(), leaseToken)
	if err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	if !claimed {
		return ChannelModelDetectionWorkerResult{}, ErrChannelModelDetectionWorkerBusy
	}
	defer func() {
		_, _ = global.ReleaseWorkerLease(db.WithContext(context.Background()), leaseToken)
	}()
	ctx = context.WithValue(ctx, channelModelDetectionWorkerLeaseContextKey{}, leaseToken)

	run, execution, err := worker.claimChannelModelDetectionExecution(ctx, db)
	if err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	result := ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: execution.Status, OfficialSessionID: execution.OfficialSessionId}

	if err := db.WithContext(ctx).Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil {
		return result, err
	}
	detectorURL := execution.DetectorURLSnapshot
	if strings.TrimSpace(detectorURL) == "" {
		detectorURL = global.DetectorURL
	}
	detectorURL, err = NormalizeChannelModelDetectorURL(detectorURL)
	if err != nil {
		return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
	}
	detector, err := worker.DetectorFactory(detectorURL)
	if err != nil {
		return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
	}
	needsImmediateSnapshot := execution.OfficialSessionId != "" ||
		execution.Status == model.ChannelModelDetectionExecutionStatusSubmitting ||
		run.Status == model.ChannelModelDetectionRunStatusSubmissionUnknown
	if execution.DetectorURLSnapshot == "" && needsImmediateSnapshot {
		if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
			updated := tx.Model(&model.ChannelModelDetectionExecution{}).
				Where("id = ? AND detector_url_snapshot = ?", execution.Id, "").
				Update("detector_url_snapshot", detectorURL)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrChannelModelDetectionWorkerBusy
			}
			return nil
		}); err != nil {
			return result, err
		}
		execution.DetectorURLSnapshot = detectorURL
	} else if execution.DetectorURLSnapshot == "" {
		execution.DetectorURLSnapshot = detectorURL
	}
	if execution.OfficialSessionId != "" {
		if err := worker.renewDBLease(ctx, db); err != nil {
			return result, err
		}
		status, err := detector.Status(ctx)
		if err != nil {
			return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
		}
		return worker.reconcileOwnedChannelModelDetectionSession(ctx, db, detector, run, execution, status, now)
	}
	if execution.Status == model.ChannelModelDetectionExecutionStatusSubmitting || run.Status == model.ChannelModelDetectionRunStatusSubmissionUnknown {
		return worker.reconcileChannelModelDetectionSubmission(ctx, db, detector, run, execution, now)
	}
	return worker.startChannelModelDetectionExecution(ctx, db, detector, run, execution, now)
}

func (worker *ChannelModelDetectionWorker) claimChannelModelDetectionExecution(ctx context.Context, db *gorm.DB) (model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution, error) {
	var run model.ChannelModelDetectionRun
	var execution model.ChannelModelDetectionExecution
	err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		activeExecutionStatuses := []string{model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning}
		if err := tx.Where("status IN ?", activeExecutionStatuses).Order("updated_at ASC, id ASC").First(&execution).Error; err == nil {
			return tx.Where("run_id = ?", execution.RunId).First(&run).Error
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		activeRunStatuses := []string{
			model.ChannelModelDetectionRunStatusQueued,
			model.ChannelModelDetectionRunStatusWaitingDetector,
			model.ChannelModelDetectionRunStatusRunning,
			model.ChannelModelDetectionRunStatusSubmissionUnknown,
		}
		if err := tx.Where("status IN ?", activeRunStatuses).
			Order("queued_at ASC, channel_id ASC, id ASC").First(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrChannelModelDetectionWorkerNoWork
			}
			return err
		}
		executionStatuses := []string{model.ChannelModelDetectionExecutionStatusPending}
		if run.Status == model.ChannelModelDetectionRunStatusSubmissionUnknown {
			executionStatuses = append(executionStatuses, model.ChannelModelDetectionExecutionStatusSubmitting)
		}
		if err := tx.Where("run_id = ? AND status IN ?", run.RunId, executionStatuses).
			Order("id ASC").First(&execution).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) && run.Status == model.ChannelModelDetectionRunStatusWaitingDetector {
				return ErrChannelModelDetectionWorkerNoWork
			}
			return err
		}
		return nil
	})
	return run, execution, err
}

func (worker *ChannelModelDetectionWorker) startChannelModelDetectionExecution(ctx context.Context, db *gorm.DB, detector ChannelModelDetectionDetector, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	result := ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: execution.Status}
	if worker.IssueCredential == nil {
		return result, errors.New("模型检测短期凭证签发器未配置")
	}
	if err := worker.renewDBLease(ctx, db); err != nil {
		return result, err
	}
	bootstrap, err := detector.Bootstrap(ctx)
	if err != nil {
		return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
	}
	preset, ok := bootstrap.Preset(run.Preset)
	if !ok {
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "preset_unavailable", "官方检测器缺少本轮冻结档位", now)
	}
	if err := worker.renewDBLease(ctx, db); err != nil {
		return result, err
	}
	estimate, err := detector.Estimate(ctx, preset)
	if err != nil {
		return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
	}
	configHash := strings.TrimSpace(estimate.ConfigHash)
	if configHash == "" {
		configHash = detectorConfigHash(preset)
	}
	if configHash == "" {
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "config_hash_missing", "官方检测器未返回配置哈希", now)
	}
	plannedRequests := valueOrZero(estimate.TotalRequests)
	if estimate.TotalRequests == nil || plannedRequests <= 0 || plannedRequests > math.MaxInt32 {
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "request_budget_invalid", "官方检测器返回的请求预算无效", now)
	}
	if err := execution.SetOfficialConfig(preset); err != nil {
		return result, err
	}
	execution.PlannedLogicalRequests = plannedRequests
	credential, baseURL, err := worker.IssueCredential(ctx, run, execution)
	if err != nil {
		return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
	}
	baseURL, err = NormalizeChannelModelDetectorURL(baseURL)
	if err != nil {
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "relay_url_invalid", "内部固定渠道地址无效", now)
	}
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		updated := tx.Model(&model.ChannelModelDetectionExecution{}).
			Where("id = ? AND status = ? AND official_session_id = ?", execution.Id, model.ChannelModelDetectionExecutionStatusPending, "").
			Updates(map[string]any{
				"status": model.ChannelModelDetectionExecutionStatusSubmitting, "official_config_json": execution.OfficialConfigJSON,
				"config_hash": configHash, "detector_url_snapshot": execution.DetectorURLSnapshot,
				"planned_logical_requests": plannedRequests, "started_at": now.Unix(), "updated_at": now.Unix(),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelModelDetectionWorkerBusy
		}
		return tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ? AND status IN ?", run.RunId, []string{
			model.ChannelModelDetectionRunStatusQueued, model.ChannelModelDetectionRunStatusWaitingDetector, model.ChannelModelDetectionRunStatusRunning,
		}).Updates(map[string]any{
			"status": model.ChannelModelDetectionRunStatusSubmitting, "planned_logical_requests": plannedRequests,
			"started_at": firstPositive(run.StartedAt, now.Unix()), "updated_at": now.Unix(),
		}).Error
	}); err != nil {
		return result, err
	}

	if err := worker.renewDBLease(ctx, db); err != nil {
		credential = ""
		return result, err
	}
	started, err := detector.Start(ctx, ChannelModelDetectorStartRequest{
		BaseURL: baseURL, APIKey: credential, Model: execution.ClaimedModel,
		ClaimedModel: execution.ClaimedModel, RequestModel: execution.RequestModel, Config: preset,
	})
	credential = ""
	if err != nil {
		if errors.Is(err, ErrChannelModelDetectorSubmissionUnknown) {
			return worker.markChannelModelDetectionSubmissionUnknown(ctx, db, run, execution, "submission_unknown", "启动结果无法唯一确认，已停止自动重提", now)
		}
		if errors.Is(err, ErrChannelModelDetectorBusy) {
			var detectorErr *ChannelModelDetectorError
			if errors.As(err, &detectorErr) && detectorErr.ReconciledStatus != nil && detectorErr.ReconciledStatus.SessionID != "" {
				return worker.markChannelModelDetectionConflict(ctx, db, run, execution, detectorErr.ReconciledStatus.SessionID, now)
			}
		}
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "start_failed", err.Error(), now)
	}
	if started.SessionID == "" || started.ConfigHash != configHash {
		return worker.markChannelModelDetectionSubmissionUnknown(ctx, db, run, execution, "submission_unknown", "启动响应缺少匹配的会话身份", now)
	}
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		updated := tx.Model(&model.ChannelModelDetectionExecution{}).
			Where("id = ? AND status = ? AND official_session_id = ?", execution.Id, model.ChannelModelDetectionExecutionStatusSubmitting, "").
			Updates(map[string]any{"status": model.ChannelModelDetectionExecutionStatusRunning, "official_session_id": started.SessionID, "official": boolValue(started.Official), "updated_at": now.Unix()})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelModelDetectionWorkerBusy
		}
		return tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).
			Updates(map[string]any{"status": model.ChannelModelDetectionRunStatusRunning, "updated_at": now.Unix()}).Error
	}); err != nil {
		return result, err
	}
	result.Started = true
	result.Status = model.ChannelModelDetectionExecutionStatusRunning
	result.OfficialSessionID = started.SessionID
	return result, nil
}

func (worker *ChannelModelDetectionWorker) reconcileChannelModelDetectionSubmission(ctx context.Context, db *gorm.DB, detector ChannelModelDetectionDetector, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	if err := worker.renewDBLease(ctx, db); err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	status, err := detector.Status(ctx)
	if err != nil {
		return worker.markChannelModelDetectionSubmissionUnknown(ctx, db, run, execution, "submission_unknown", "启动结果无法唯一确认，已停止自动重提", now)
	}
	if status.Status == "running" || status.Status == "stopping" || status.Status == "complete" || status.Status == "completed" || status.Status == "interrupted" {
		configMap, configErr := execution.OfficialConfig()
		if configErr == nil && len(configMap) > 0 {
			configData, marshalErr := common.Marshal(configMap)
			var preset ChannelModelDetectorPresetConfig
			if marshalErr == nil && common.Unmarshal(configData, &preset) == nil &&
				status.SessionID != "" && status.ConfigHash == execution.ConfigHash && detectorConfigHash(preset) == execution.ConfigHash &&
				(status.ClaimedModel == "" || status.ClaimedModel == execution.ClaimedModel) {
				adopted := false
				if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
					updated := tx.Model(&model.ChannelModelDetectionExecution{}).
						Where("id = ? AND status = ? AND official_session_id = ?", execution.Id, model.ChannelModelDetectionExecutionStatusSubmitting, "").
						Updates(map[string]any{"status": model.ChannelModelDetectionExecutionStatusRunning, "official_session_id": status.SessionID, "official": boolValue(status.Official), "updated_at": now.Unix()})
					if updated.Error != nil {
						return updated.Error
					}
					if updated.RowsAffected != 1 {
						return ErrChannelModelDetectionWorkerBusy
					}
					adopted = true
					return tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).
						Updates(map[string]any{"status": model.ChannelModelDetectionRunStatusRunning, "error_code": "", "error_message": "", "updated_at": now.Unix()}).Error
				}); err != nil {
					return ChannelModelDetectionWorkerResult{}, err
				}
				if adopted {
					execution.Status = model.ChannelModelDetectionExecutionStatusRunning
					execution.OfficialSessionId = status.SessionID
					return worker.reconcileOwnedChannelModelDetectionSession(ctx, db, detector, run, execution, status, now)
				}
			}
		}
	}
	return worker.markChannelModelDetectionSubmissionUnknown(ctx, db, run, execution, "submission_unknown", "启动结果无法唯一确认，已停止自动重提", now)
}

func (worker *ChannelModelDetectionWorker) reconcileOwnedChannelModelDetectionSession(ctx context.Context, db *gorm.DB, detector ChannelModelDetectionDetector, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, status ChannelModelDetectorStatusResponse, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	result := ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: execution.Status, OfficialSessionID: execution.OfficialSessionId}
	if status.SessionID != execution.OfficialSessionId {
		return worker.markChannelModelDetectionConflict(ctx, db, run, execution, status.SessionID, now)
	}
	if status.ConfigHash != "" && execution.ConfigHash != "" && status.ConfigHash != execution.ConfigHash ||
		status.ClaimedModel != "" && status.ClaimedModel != execution.ClaimedModel ||
		status.RequestModel != "" && status.RequestModel != execution.RequestModel {
		return worker.markChannelModelDetectionConflict(ctx, db, run, execution, status.SessionID, now)
	}
	switch status.Status {
	case "running", "stopping":
		updates := channelModelDetectionProgressUpdates(status.Progress, now.Unix())
		if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
			updated := tx.Model(&model.ChannelModelDetectionExecution{}).
				Where("id = ? AND official_session_id = ?", execution.Id, execution.OfficialSessionId).Updates(updates)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrChannelModelDetectionWorkerBusy
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.Status = model.ChannelModelDetectionExecutionStatusRunning
		return result, nil
	case "complete", "completed":
		if status.ReportAvailable != nil && !*status.ReportAvailable {
			return worker.waitForChannelModelDetector(ctx, db, run, execution, errors.New("官方报告尚未可用"), now)
		}
		if err := worker.renewDBLease(ctx, db); err != nil {
			return result, err
		}
		report, err := detector.Report(ctx)
		if err != nil {
			return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
		}
		if report.SessionID != execution.OfficialSessionId || report.ConfigHash != "" && execution.ConfigHash != "" && report.ConfigHash != execution.ConfigHash {
			return worker.markChannelModelDetectionConflict(ctx, db, run, execution, report.SessionID, now)
		}
		if err := execution.SetReport(report); err != nil {
			return result, err
		}
		reportCompatibilityErr := channelModelDetectorReportCompatibilityError(report, execution.ClaimedModel, execution.RequestModel)
		if err := worker.completeChannelModelDetectionExecution(ctx, db, run, execution, status, report, reportCompatibilityErr, now); err != nil {
			return result, err
		}
		result.Status = model.ChannelModelDetectionExecutionStatusCompleted
		if reportCompatibilityErr != nil {
			result.Status = model.ChannelModelDetectionExecutionStatusFailed
		}
		result.Completed = true
		return result, nil
	case "interrupted":
		return worker.resumeChannelModelDetectionExecution(ctx, db, detector, run, execution, status, now)
	case "error", "failed":
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "official_session_failed", status.Error, now)
	case "idle", "":
		return worker.markChannelModelDetectionConflict(ctx, db, run, execution, status.SessionID, now)
	default:
		return worker.waitForChannelModelDetector(ctx, db, run, execution, fmt.Errorf("未知官方状态: %s", status.Status), now)
	}
}

func (worker *ChannelModelDetectionWorker) resumeChannelModelDetectionExecution(ctx context.Context, db *gorm.DB, detector ChannelModelDetectionDetector, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, status ChannelModelDetectorStatusResponse, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	result := ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: execution.Status, OfficialSessionID: execution.OfficialSessionId}
	if worker.IssueCredential == nil {
		return result, errors.New("模型检测短期凭证签发器未配置")
	}
	configMap, err := execution.OfficialConfig()
	if err != nil || len(configMap) == 0 {
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "resume_config_missing", "无法恢复原官方配置快照", now)
	}
	configData, err := common.Marshal(configMap)
	if err != nil {
		return result, err
	}
	var preset ChannelModelDetectorPresetConfig
	if err := common.Unmarshal(configData, &preset); err != nil {
		return result, err
	}
	if detectorConfigHash(preset) != execution.ConfigHash || status.ConfigHash != "" && status.ConfigHash != execution.ConfigHash {
		return worker.markChannelModelDetectionConflict(ctx, db, run, execution, status.SessionID, now)
	}
	credential, baseURL, err := worker.IssueCredential(ctx, run, execution)
	if err != nil {
		return worker.waitForChannelModelDetector(ctx, db, run, execution, err, now)
	}
	baseURL, err = NormalizeChannelModelDetectorURL(baseURL)
	if err != nil {
		credential = ""
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "relay_url_invalid", "内部固定渠道地址无效", now)
	}
	if err := worker.renewDBLease(ctx, db); err != nil {
		credential = ""
		return result, err
	}
	resumed, err := detector.Start(ctx, ChannelModelDetectorStartRequest{
		BaseURL: baseURL, APIKey: credential, Model: execution.ClaimedModel,
		ClaimedModel: execution.ClaimedModel, RequestModel: execution.RequestModel, Config: preset,
		ResumeSessionID: execution.OfficialSessionId, PreviousSessionID: status.SessionID,
	})
	credential = ""
	if err != nil {
		if errors.Is(err, ErrChannelModelDetectorSubmissionUnknown) {
			return worker.markChannelModelDetectionSubmissionUnknown(ctx, db, run, execution, "resume_submission_unknown", "恢复结果无法唯一确认，已停止自动重提", now)
		}
		return worker.failChannelModelDetectionExecution(ctx, db, run, execution, "resume_rejected", err.Error(), now)
	}
	if resumed.SessionID != execution.OfficialSessionId {
		return worker.markChannelModelDetectionConflict(ctx, db, run, execution, resumed.SessionID, now)
	}
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		updated := tx.Model(&model.ChannelModelDetectionExecution{}).
			Where("id = ? AND official_session_id = ?", execution.Id, execution.OfficialSessionId).
			Updates(map[string]any{"status": model.ChannelModelDetectionExecutionStatusRunning, "updated_at": now.Unix()})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelModelDetectionWorkerBusy
		}
		return nil
	}); err != nil {
		return result, err
	}
	result.Resumed = true
	result.Status = model.ChannelModelDetectionExecutionStatusRunning
	return result, nil
}

func (worker *ChannelModelDetectionWorker) completeChannelModelDetectionExecution(ctx context.Context, db *gorm.DB, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, status ChannelModelDetectorStatusResponse, report ChannelModelDetectorReportResponse, reportCompatibilityErr error, now time.Time) error {
	reportHash := sha256.Sum256([]byte(execution.ReportJSON))
	updates := channelModelDetectionProgressUpdates(status.Progress, now.Unix())
	updates["status"] = model.ChannelModelDetectionExecutionStatusCompleted
	updates["outcome_code"] = report.OutcomeCode
	titleCN := strings.TrimSpace(report.TitleCN)
	if titleCN == "" {
		titleCN = report.OverallVerdict
	}
	updates["title_cn"] = titleCN
	updates["subtitle_cn"] = report.SubtitleCN
	updates["juice_verdict_state"] = report.JuiceVerdictState
	updates["fingerprint_verdict_state"] = report.FingerprintVerdictState
	updates["fingerprint_model"] = report.FingerprintModel
	updates["official"] = boolValue(report.Official)
	updates["schema_version"] = valueOrZero(report.SchemaVersion)
	updates["scoring_version"] = report.ScoringVersion
	updates["baseline_id"] = report.BaselineID
	updates["baseline_sha256"] = report.BaselineSHA256
	updates["build_hash"] = report.BuildHash
	updates["report_json"] = execution.ReportJSON
	updates["report_sha256"] = fmt.Sprintf("%x", reportHash[:])
	updates["finished_at"] = now.Unix()
	if reportCompatibilityErr != nil {
		updates["status"] = model.ChannelModelDetectionExecutionStatusFailed
		updates["outcome_code"] = ""
		updates["title_cn"] = "检测报告版本不兼容"
		updates["subtitle_cn"] = reportCompatibilityErr.Error()
		updates["juice_verdict_state"] = ""
		updates["fingerprint_verdict_state"] = ""
		updates["fingerprint_model"] = ""
		updates["error_code"] = "report_contract_incompatible"
		updates["error_message"] = reportCompatibilityErr.Error()
	}
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		updated := tx.Model(&model.ChannelModelDetectionExecution{}).
			Where("id = ? AND official_session_id = ? AND status = ?", execution.Id, execution.OfficialSessionId, model.ChannelModelDetectionExecutionStatusRunning).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelModelDetectionWorkerBusy
		}
		return finalizeChannelModelDetectionRun(tx, run, now)
	}); err != nil {
		return err
	}
	return promotePendingChannelModelDetectorURL(ctx, db, now.Unix())
}

func finalizeChannelModelDetectionRun(tx *gorm.DB, run model.ChannelModelDetectionRun, now time.Time) error {
	var pending, completed, failed, canceled, skipped int64
	if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("run_id = ? AND status IN ?", run.RunId, []string{
		model.ChannelModelDetectionExecutionStatusPending, model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning,
	}).Count(&pending).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("run_id = ? AND status = ?", run.RunId, model.ChannelModelDetectionExecutionStatusCompleted).Count(&completed).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("run_id = ? AND status = ?", run.RunId, model.ChannelModelDetectionExecutionStatusFailed).Count(&failed).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("run_id = ? AND status = ?", run.RunId, model.ChannelModelDetectionExecutionStatusCanceled).Count(&canceled).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("run_id = ? AND status = ?", run.RunId, model.ChannelModelDetectionExecutionStatusSkipped).Count(&skipped).Error; err != nil {
		return err
	}
	progress, err := aggregateChannelModelDetectionRunProgress(tx, run.RunId)
	if err != nil {
		return err
	}
	progressUpdates := map[string]any{
		"planned_logical_requests":   progress.PlannedLogicalRequests,
		"completed_logical_requests": progress.CompletedLogicalRequests,
		"http_attempts":              progress.HTTPAttempts,
		"retry_count":                progress.RetryCount,
	}
	if pending > 0 {
		progressUpdates["status"] = model.ChannelModelDetectionRunStatusRunning
		progressUpdates["completed_target_count"] = completed
		progressUpdates["updated_at"] = now.Unix()
		return tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).
			Updates(progressUpdates).Error
	}
	status := model.ChannelModelDetectionRunStatusCompleted
	if completed > 0 && (failed > 0 || canceled > 0 || skipped > 0) {
		status = model.ChannelModelDetectionRunStatusPartial
	} else if completed == 0 && failed > 0 {
		status = model.ChannelModelDetectionRunStatusFailed
	} else if completed == 0 && canceled > 0 {
		status = model.ChannelModelDetectionRunStatusCanceled
	}
	progressUpdates["status"] = status
	progressUpdates["completed_target_count"] = completed
	progressUpdates["finished_at"] = now.Unix()
	progressUpdates["updated_at"] = now.Unix()
	if err := tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).Updates(progressUpdates).Error; err != nil {
		return err
	}
	if _, err := model.ReleaseChannelModelDetectionRun(tx, run.ChannelId, run.RunId, now.Unix()); err != nil {
		return err
	}
	if run.BatchId != nil {
		return rebuildChannelModelDetectionBatchState(tx, *run.BatchId, now)
	}
	return nil
}

type channelModelDetectionRunProgressAggregate struct {
	PlannedLogicalRequests   int64
	CompletedLogicalRequests int64
	HTTPAttempts             int64
	RetryCount               int64
}

func aggregateChannelModelDetectionRunProgress(tx *gorm.DB, runID string) (channelModelDetectionRunProgressAggregate, error) {
	var executions []model.ChannelModelDetectionExecution
	if err := tx.Model(&model.ChannelModelDetectionExecution{}).
		Select("planned_logical_requests, completed_logical_requests, http_attempts, retry_count").
		Where("run_id = ?", runID).Find(&executions).Error; err != nil {
		return channelModelDetectionRunProgressAggregate{}, err
	}
	var aggregate channelModelDetectionRunProgressAggregate
	for _, execution := range executions {
		aggregate.PlannedLogicalRequests += execution.PlannedLogicalRequests
		aggregate.CompletedLogicalRequests += execution.CompletedLogicalRequests
		aggregate.HTTPAttempts += execution.HTTPAttempts
		aggregate.RetryCount += execution.RetryCount
	}
	return aggregate, nil
}

func rebuildChannelModelDetectionBatchState(tx *gorm.DB, batchID string, now time.Time) error {
	var total, active, completed, failed, canceled int64
	query := tx.Model(&model.ChannelModelDetectionRun{}).Where("batch_id = ?", batchID)
	if err := query.Count(&total).Error; err != nil {
		return err
	}
	if err := query.Where("status IN ?", []string{
		model.ChannelModelDetectionRunStatusQueued, model.ChannelModelDetectionRunStatusWaitingDetector,
		model.ChannelModelDetectionRunStatusSubmitting, model.ChannelModelDetectionRunStatusRunning,
		model.ChannelModelDetectionRunStatusSubmissionUnknown, model.ChannelModelDetectionRunStatusCanceling,
	}).Count(&active).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ChannelModelDetectionRun{}).Where("batch_id = ? AND status = ?", batchID, model.ChannelModelDetectionRunStatusCompleted).Count(&completed).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ChannelModelDetectionRun{}).Where("batch_id = ? AND status IN ?", batchID, []string{model.ChannelModelDetectionRunStatusFailed, model.ChannelModelDetectionRunStatusExternalSessionConflict}).Count(&failed).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ChannelModelDetectionRun{}).Where("batch_id = ? AND status = ?", batchID, model.ChannelModelDetectionRunStatusCanceled).Count(&canceled).Error; err != nil {
		return err
	}
	status := model.ChannelModelDetectionBatchStatusRunning
	finishedAt := int64(0)
	if active == 0 {
		finishedAt = now.Unix()
		switch {
		case total > 0 && completed == total:
			status = model.ChannelModelDetectionBatchStatusCompleted
		case total > 0 && canceled == total:
			status = model.ChannelModelDetectionBatchStatusCanceled
		case total > 0 && failed == total:
			status = model.ChannelModelDetectionBatchStatusFailed
		default:
			status = model.ChannelModelDetectionBatchStatusPartial
		}
	}
	return tx.Model(&model.ChannelModelDetectionBatch{}).Where("batch_id = ?", batchID).Updates(map[string]any{
		"status": status, "completed_run_count": completed, "failed_run_count": failed,
		"canceled_run_count": canceled, "finished_at": finishedAt, "updated_at": now.Unix(),
	}).Error
}

func (worker *ChannelModelDetectionWorker) markChannelModelDetectionConflict(ctx context.Context, db *gorm.DB, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, observedSessionID string, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	message := "检测器当前会话不属于本地执行"
	if observedSessionID == "" {
		message = "本地官方会话已不存在"
	}
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).Updates(map[string]any{
			"status": model.ChannelModelDetectionExecutionStatusFailed, "error_code": "external_session_conflict", "error_message": message,
			"finished_at": now.Unix(), "updated_at": now.Unix(),
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).Updates(map[string]any{
			"status": model.ChannelModelDetectionRunStatusExternalSessionConflict, "error_code": "external_session_conflict",
			"error_message": message, "finished_at": now.Unix(), "updated_at": now.Unix(),
		}).Error; err != nil {
			return err
		}
		_, err := model.ReleaseChannelModelDetectionRun(tx, run.ChannelId, run.RunId, now.Unix())
		return err
	}); err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	if err := promotePendingChannelModelDetectorURL(ctx, db, now.Unix()); err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	return ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: model.ChannelModelDetectionRunStatusExternalSessionConflict, OfficialSessionID: execution.OfficialSessionId}, ErrChannelModelDetectionExternalSessionConflict
}

func (worker *ChannelModelDetectionWorker) markChannelModelDetectionSubmissionUnknown(ctx context.Context, db *gorm.DB, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, code, message string, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	message = sanitizeChannelModelDetectionWorkerError(message)
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).Updates(map[string]any{
			"status": model.ChannelModelDetectionExecutionStatusSubmitting, "error_code": code, "error_message": message, "updated_at": now.Unix(),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).Updates(map[string]any{
			"status": model.ChannelModelDetectionRunStatusSubmissionUnknown, "error_code": code, "error_message": message, "updated_at": now.Unix(),
		}).Error
	}); err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	return ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: model.ChannelModelDetectionRunStatusSubmissionUnknown}, ErrChannelModelDetectionSubmissionUnknown
}

func (worker *ChannelModelDetectionWorker) waitForChannelModelDetector(ctx context.Context, db *gorm.DB, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, cause error, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	message := sanitizeChannelModelDetectionWorkerError(cause.Error())
	if execution.OfficialSessionId != "" || execution.Status == model.ChannelModelDetectionExecutionStatusRunning {
		return ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: execution.Status, OfficialSessionID: execution.OfficialSessionId, Waiting: true}, cause
	}
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).
			Updates(map[string]any{"status": model.ChannelModelDetectionExecutionStatusPending, "error_code": "detector_unavailable", "error_message": message, "updated_at": now.Unix()}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).
			Updates(map[string]any{"status": model.ChannelModelDetectionRunStatusWaitingDetector, "error_code": "detector_unavailable", "error_message": message, "updated_at": now.Unix()}).Error
	}); err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	return ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: model.ChannelModelDetectionRunStatusWaitingDetector, Waiting: true}, nil
}

func (worker *ChannelModelDetectionWorker) failChannelModelDetectionExecution(ctx context.Context, db *gorm.DB, run model.ChannelModelDetectionRun, execution model.ChannelModelDetectionExecution, code, message string, now time.Time) (ChannelModelDetectionWorkerResult, error) {
	message = sanitizeChannelModelDetectionWorkerError(message)
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).Updates(map[string]any{
			"status": model.ChannelModelDetectionExecutionStatusFailed, "error_code": code, "error_message": message, "finished_at": now.Unix(), "updated_at": now.Unix(),
		}).Error; err != nil {
			return err
		}
		return finalizeChannelModelDetectionRun(tx, run, now)
	}); err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	if err := promotePendingChannelModelDetectorURL(ctx, db, now.Unix()); err != nil {
		return ChannelModelDetectionWorkerResult{}, err
	}
	return ChannelModelDetectionWorkerResult{ClaimedExecutionID: execution.Id, RunID: run.RunId, Status: model.ChannelModelDetectionExecutionStatusFailed}, nil
}

// CancelRun is idempotent. Queued targets are canceled locally; a currently
// owned official session is stopped only after status confirms the session ID.
func (worker *ChannelModelDetectionWorker) CancelRun(ctx context.Context, runID string) error {
	if worker == nil || strings.TrimSpace(runID) == "" {
		return gorm.ErrRecordNotFound
	}
	db := worker.DB
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return errors.New("模型检测 Worker 数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := worker.now()
	leaseToken, acquired := worker.TryAcquireLease(now)
	if !acquired {
		return ErrChannelModelDetectionWorkerBusy
	}
	defer worker.releaseLease(leaseToken)
	global := model.ChannelModelDetectionGlobalConfig{Id: model.ChannelModelDetectionConfigID}
	claimed, err := global.TryAcquireWorkerLease(db.WithContext(ctx), now.Unix(), leaseToken)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrChannelModelDetectionWorkerBusy
	}
	defer func() {
		_, _ = global.ReleaseWorkerLease(db.WithContext(context.Background()), leaseToken)
	}()
	ctx = context.WithValue(ctx, channelModelDetectionWorkerLeaseContextKey{}, leaseToken)
	var run model.ChannelModelDetectionRun
	if err := db.WithContext(ctx).Where("run_id = ?", strings.TrimSpace(runID)).First(&run).Error; err != nil {
		return err
	}
	if !model.IsChannelModelDetectionActiveRunStatus(run.Status) {
		return nil
	}
	var execution model.ChannelModelDetectionExecution
	err = db.WithContext(ctx).Where("run_id = ? AND status IN ?", run.RunId, []string{
		model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning,
	}).Order("id ASC").First(&execution).Error
	if err == nil && execution.OfficialSessionId != "" {
		detectorURL := execution.DetectorURLSnapshot
		if strings.TrimSpace(detectorURL) == "" {
			if err := db.WithContext(ctx).Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil {
				return err
			}
			detectorURL = global.DetectorURL
		}
		detectorURL, err = NormalizeChannelModelDetectorURL(detectorURL)
		if err != nil {
			return err
		}
		if worker.DetectorFactory == nil {
			return errors.New("模型检测器客户端工厂未配置")
		}
		detector, err := worker.DetectorFactory(detectorURL)
		if err != nil {
			return err
		}
		if err := worker.renewDBLease(ctx, db); err != nil {
			return err
		}
		if _, err := detector.Bootstrap(ctx); err != nil {
			return err
		}
		if err := worker.renewDBLease(ctx, db); err != nil {
			return err
		}
		status, err := detector.Status(ctx)
		if err != nil {
			return err
		}
		if status.SessionID != execution.OfficialSessionId {
			_, conflictErr := worker.markChannelModelDetectionConflict(ctx, db, run, execution, status.SessionID, now)
			return conflictErr
		}
		if err := worker.renewDBLease(ctx, db); err != nil {
			return err
		}
		if _, err := detector.Stop(ctx); err != nil {
			return err
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		// No live official session: queued work can be canceled locally without
		// contacting the detector.
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := worker.withDBLeaseTransaction(ctx, db, func(tx *gorm.DB) error {
		if err := tx.Model(&model.ChannelModelDetectionExecution{}).Where("run_id = ? AND status IN ?", run.RunId, []string{
			model.ChannelModelDetectionExecutionStatusPending, model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning,
		}).Updates(map[string]any{
			"status": model.ChannelModelDetectionExecutionStatusCanceled, "canceled": true, "finished_at": now.Unix(), "updated_at": now.Unix(),
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ChannelModelDetectionRun{}).Where("run_id = ?", run.RunId).Updates(map[string]any{
			"status": model.ChannelModelDetectionRunStatusCanceled, "cancel_requested_at": firstPositive(run.CancelRequestedAt, now.Unix()),
			"finished_at": now.Unix(), "updated_at": now.Unix(),
		}).Error; err != nil {
			return err
		}
		_, err := model.ReleaseChannelModelDetectionRun(tx, run.ChannelId, run.RunId, now.Unix())
		return err
	}); err != nil {
		return err
	}
	return promotePendingChannelModelDetectorURL(ctx, db, now.Unix())
}

func promotePendingChannelModelDetectorURL(ctx context.Context, db *gorm.DB, now int64) error {
	if db == nil {
		return errors.New("模型检测统一设置数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var active int64
	if err := db.WithContext(ctx).Model(&model.ChannelModelDetectionExecution{}).
		Where("status IN ?", []string{model.ChannelModelDetectionExecutionStatusSubmitting, model.ChannelModelDetectionExecutionStatusRunning}).
		Count(&active).Error; err != nil || active > 0 {
		return err
	}
	updated := db.WithContext(ctx).Model(&model.ChannelModelDetectionGlobalConfig{}).
		Where("id = ? AND pending_detector_url <> ?", model.ChannelModelDetectionConfigID, "").
		Updates(map[string]any{
			"detector_url": gorm.Expr("pending_detector_url"), "pending_detector_url": "", "updated_at": now,
		})
	if updated.Error != nil {
		return updated.Error
	}
	return nil
}

func channelModelDetectionProgressUpdates(progress ChannelModelDetectorProgress, now int64) map[string]any {
	updates := map[string]any{"updated_at": now}
	if progress.Planned != nil && *progress.Planned >= 0 {
		updates["planned_logical_requests"] = *progress.Planned
	}
	if progress.LogicalCompleted != nil && *progress.LogicalCompleted >= 0 {
		updates["completed_logical_requests"] = *progress.LogicalCompleted
	}
	if progress.HTTPAttempts != nil && *progress.HTTPAttempts >= 0 {
		updates["http_attempts"] = *progress.HTTPAttempts
	}
	if progress.Retries != nil && *progress.Retries >= 0 {
		updates["retry_count"] = *progress.Retries
	}
	return updates
}

func sanitizeChannelModelDetectionWorkerError(message string) string {
	message = redactDetectorMessage(strings.TrimSpace(message))
	if len(message) > channelModelDetectionWorkerErrorLimit {
		message = message[:channelModelDetectionWorkerErrorLimit]
	}
	return message
}

func valueOrZero[T ~int64](value *T) int64 {
	if value == nil || *value < 0 {
		return 0
	}
	return int64(*value)
}

func firstPositive(current, fallback int64) int64 {
	if current > 0 {
		return current
	}
	return fallback
}

func boolValue(value *bool) bool {
	return value != nil && *value
}
