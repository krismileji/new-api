package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	ChannelModelDetectionConfigID int64 = 1

	ChannelModelDetectionPresetLow    = "low"
	ChannelModelDetectionPresetMedium = "medium"
	ChannelModelDetectionPresetHigh   = "high"

	ChannelModelDetectionTriggerScheduled = "scheduled"
	ChannelModelDetectionTriggerManual    = "manual"

	ChannelModelDetectionPresetSourceScheduledDefault = "scheduled_default"
	ChannelModelDetectionPresetSourceManualSelected   = "manual_selected"

	ChannelModelDetectionBatchStatusQueued    = "queued"
	ChannelModelDetectionBatchStatusRunning   = "running"
	ChannelModelDetectionBatchStatusCompleted = "completed"
	ChannelModelDetectionBatchStatusPartial   = "partial"
	ChannelModelDetectionBatchStatusFailed    = "failed"
	ChannelModelDetectionBatchStatusCanceled  = "canceled"

	ChannelModelDetectionRunStatusQueued                  = "queued"
	ChannelModelDetectionRunStatusWaitingDetector         = "waiting_detector"
	ChannelModelDetectionRunStatusSubmitting              = "submitting"
	ChannelModelDetectionRunStatusRunning                 = "running"
	ChannelModelDetectionRunStatusSubmissionUnknown       = "submission_unknown"
	ChannelModelDetectionRunStatusCompleted               = "completed"
	ChannelModelDetectionRunStatusPartial                 = "partial"
	ChannelModelDetectionRunStatusFailed                  = "failed"
	ChannelModelDetectionRunStatusExternalSessionConflict = "external_session_conflict"
	ChannelModelDetectionRunStatusCanceling               = "canceling"
	ChannelModelDetectionRunStatusCanceled                = "canceled"

	ChannelModelDetectionExecutionStatusPending    = "pending"
	ChannelModelDetectionExecutionStatusSubmitting = "submitting"
	ChannelModelDetectionExecutionStatusRunning    = "running"
	ChannelModelDetectionExecutionStatusCompleted  = "completed"
	ChannelModelDetectionExecutionStatusFailed     = "failed"
	ChannelModelDetectionExecutionStatusCanceled   = "canceled"
	ChannelModelDetectionExecutionStatusSkipped    = "skipped"

	ChannelModelDetectionDispatchPrepared   = "prepared"
	ChannelModelDetectionDispatchDispatched = "dispatched"
	ChannelModelDetectionDispatchNotStarted = "not_started"

	ChannelModelDetectionSettlementPending       = "pending"
	ChannelModelDetectionSettlementSettled       = "settled"
	ChannelModelDetectionSettlementUnresolved    = "unresolved"
	ChannelModelDetectionSettlementNotApplicable = "not_applicable"

	ChannelModelDetectionUsageUpstreamAuthoritative = "upstream_authoritative"
	ChannelModelDetectionUsageLocalEstimate         = "local_estimate"
	ChannelModelDetectionUsageUnavailable           = "unavailable"

	ChannelModelDetectionCostScopeChannelUpstreamAPI = "channel_upstream_api"

	ChannelModelDetectionClaimedModelSol   = "gpt-5.6-sol"
	ChannelModelDetectionClaimedModelTerra = "gpt-5.6-terra"
	ChannelModelDetectionClaimedModelLuna  = "gpt-5.6-luna"

	ChannelModelDetectionDefaultIntervalHours       = 24
	ChannelModelDetectionDefaultScheduleTime        = "02:30"
	ChannelModelDetectionDefaultTimezone            = "Asia/Shanghai"
	ChannelModelDetectionDefaultRetentionDays       = 30
	ChannelModelDetectionMinRetentionDays           = 7
	ChannelModelDetectionMaxRetentionDays           = 180
	ChannelModelDetectionMaxReportBytes             = 1 << 20
	ChannelModelDetectionLeaseSeconds               = 600
	ChannelModelDetectionWorkerLeaseSeconds         = 120
	ChannelModelDetectionNanoPerCNY           int64 = 1_000_000_000
)

var (
	ErrChannelModelDetectionInvalidPreset            = errors.New("模型检测档位无效")
	ErrChannelModelDetectionInvalidTrigger           = errors.New("模型检测触发方式无效")
	ErrChannelModelDetectionInvalidClaimedModel      = errors.New("模型检测申报型号无效")
	ErrChannelModelDetectionInvalidSchedule          = errors.New("模型检测定时配置无效")
	ErrChannelModelDetectionScheduledHighUnconfirmed = errors.New("定时高档模型检测需要确认本次统一设置的成本风险")
	ErrChannelModelDetectionInvalidCost              = errors.New("模型检测成本字段无效")
)

func channelModelDetectionNow() int64 { return time.Now().UTC().Unix() }

func IsChannelModelDetectionPreset(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ChannelModelDetectionPresetLow, ChannelModelDetectionPresetMedium, ChannelModelDetectionPresetHigh:
		return true
	default:
		return false
	}
}

func IsChannelModelDetectionClaimedModel(value string) bool {
	switch strings.TrimSpace(value) {
	case ChannelModelDetectionClaimedModelSol, ChannelModelDetectionClaimedModelTerra, ChannelModelDetectionClaimedModelLuna:
		return true
	default:
		return false
	}
}

func IsChannelModelDetectionTrigger(value string) bool {
	return value == ChannelModelDetectionTriggerScheduled || value == ChannelModelDetectionTriggerManual
}

func IsChannelModelDetectionDispatchState(value string) bool {
	return value == ChannelModelDetectionDispatchPrepared || value == ChannelModelDetectionDispatchDispatched || value == ChannelModelDetectionDispatchNotStarted
}

func IsChannelModelDetectionSettlementStatus(value string) bool {
	return value == ChannelModelDetectionSettlementPending || value == ChannelModelDetectionSettlementSettled || value == ChannelModelDetectionSettlementUnresolved || value == ChannelModelDetectionSettlementNotApplicable
}

func IsChannelModelDetectionUsageSource(value string) bool {
	return value == ChannelModelDetectionUsageUpstreamAuthoritative || value == ChannelModelDetectionUsageLocalEstimate || value == ChannelModelDetectionUsageUnavailable
}

func validateChannelModelDetectionNonNegative(value int64) error {
	if value < 0 {
		return ErrChannelModelDetectionInvalidCost
	}
	return nil
}

func validateChannelModelDetectionNullableNonNegative(value *int64) error {
	if value != nil && *value < 0 {
		return ErrChannelModelDetectionInvalidCost
	}
	return nil
}

func validateChannelModelDetectionSchedule(scheduleTime, timezone string) error {
	if _, err := time.Parse("15:04", strings.TrimSpace(scheduleTime)); err != nil {
		return ErrChannelModelDetectionInvalidSchedule
	}
	if strings.TrimSpace(timezone) == "" {
		return ErrChannelModelDetectionInvalidSchedule
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return ErrChannelModelDetectionInvalidSchedule
	}
	return nil
}

func marshalChannelModelDetectionJSON(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	data, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeChannelModelDetectionJSON(data string, value any) error {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	return common.UnmarshalJsonStr(data, value)
}

// ChannelModelDetectionGlobalConfig stores the single global scheduling row.
// Detector/session credentials intentionally have no field in this model.
type ChannelModelDetectionGlobalConfig struct {
	Id                             int64  `json:"id" gorm:"primaryKey"`
	DetectorURL                    string `json:"-" gorm:"type:varchar(1024)"`
	ScheduledPreset                string `json:"scheduled_preset" gorm:"type:varchar(16);not null"`
	ScheduleEnabled                bool   `json:"schedule_enabled" gorm:"not null"`
	IntervalHours                  int    `json:"interval_hours" gorm:"not null"`
	ScheduleTime                   string `json:"schedule_time" gorm:"type:varchar(5);not null"`
	Timezone                       string `json:"timezone" gorm:"type:varchar(64);not null"`
	ScheduleAnchorAt               int64  `json:"schedule_anchor_at" gorm:"bigint"`
	NextBatchAt                    int64  `json:"next_batch_at" gorm:"bigint;index"`
	PendingDetectorURL             string `json:"-" gorm:"type:varchar(1024)"`
	ScheduledHighConfirmedRevision int64  `json:"-" gorm:"bigint"`
	Revision                       int64  `json:"revision" gorm:"bigint;not null"`
	LeaseToken                     string `json:"-" gorm:"type:varchar(64);index"`
	LeaseUntil                     int64  `json:"-" gorm:"bigint;index"`
	WorkerLeaseToken               string `json:"-" gorm:"type:varchar(64)"`
	WorkerLeaseUntil               int64  `json:"-" gorm:"bigint"`
	CreatedAt                      int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt                      int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (c *ChannelModelDetectionGlobalConfig) BeforeCreate(_ *gorm.DB) error {
	if c.Id == 0 {
		c.Id = ChannelModelDetectionConfigID
	}
	if c.ScheduledPreset == "" {
		c.ScheduledPreset = ChannelModelDetectionPresetMedium
	}
	if c.IntervalHours == 0 {
		c.IntervalHours = ChannelModelDetectionDefaultIntervalHours
	}
	if c.ScheduleTime == "" {
		c.ScheduleTime = ChannelModelDetectionDefaultScheduleTime
	}
	if c.Timezone == "" {
		c.Timezone = ChannelModelDetectionDefaultTimezone
	}
	if c.Revision == 0 {
		c.Revision = 1
	}
	now := channelModelDetectionNow()
	if c.CreatedAt == 0 {
		c.CreatedAt = now
	}
	if c.UpdatedAt == 0 {
		c.UpdatedAt = c.CreatedAt
	}
	return c.Validate()
}

func (c ChannelModelDetectionGlobalConfig) Validate() error {
	if c.Id != 0 && c.Id != ChannelModelDetectionConfigID {
		return ErrChannelModelDetectionInvalidSchedule
	}
	if !IsChannelModelDetectionPreset(c.ScheduledPreset) {
		return ErrChannelModelDetectionInvalidPreset
	}
	if c.IntervalHours != 6 && c.IntervalHours != 12 && c.IntervalHours != 24 && c.IntervalHours != 48 && c.IntervalHours != 72 && c.IntervalHours != 168 {
		return ErrChannelModelDetectionInvalidSchedule
	}
	highScheduleEnabled := c.ScheduleEnabled && c.ScheduledPreset == ChannelModelDetectionPresetHigh
	if highScheduleEnabled && c.ScheduledHighConfirmedRevision != c.Revision {
		return ErrChannelModelDetectionScheduledHighUnconfirmed
	}
	if !highScheduleEnabled && c.ScheduledHighConfirmedRevision != 0 {
		return ErrChannelModelDetectionInvalidSchedule
	}
	return validateChannelModelDetectionSchedule(c.ScheduleTime, c.Timezone)
}

// ApplyScheduledHighCostConfirmation consumes the command-only confirmation
// for the revision being saved. The persisted revision marker is internal and
// becomes invalid as soon as a later settings command increments Revision.
func (c *ChannelModelDetectionGlobalConfig) ApplyScheduledHighCostConfirmation(confirm bool) error {
	if c == nil {
		return ErrChannelModelDetectionInvalidSchedule
	}
	if c.ScheduleEnabled && c.ScheduledPreset == ChannelModelDetectionPresetHigh {
		if !confirm || c.Revision <= 0 {
			return ErrChannelModelDetectionScheduledHighUnconfirmed
		}
		c.ScheduledHighConfirmedRevision = c.Revision
	} else {
		c.ScheduledHighConfirmedRevision = 0
	}
	return c.Validate()
}

func (c ChannelModelDetectionGlobalConfig) DetectorURLConfigured() bool {
	return strings.TrimSpace(c.DetectorURL) != ""
}

func (c *ChannelModelDetectionGlobalConfig) TryAcquireLease(tx *gorm.DB, expectedRevision, now int64, leaseToken string) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || c == nil || c.Id <= 0 || expectedRevision <= 0 || now <= 0 || strings.TrimSpace(leaseToken) == "" {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	claimed := tx.Model(&ChannelModelDetectionGlobalConfig{}).
		Where("id = ? AND revision = ? AND lease_until <= ?", c.Id, expectedRevision, now).
		Updates(map[string]any{
			"lease_token": leaseToken,
			"lease_until": now + ChannelModelDetectionLeaseSeconds,
			"updated_at":  now,
		})
	if claimed.Error != nil || claimed.RowsAffected != 1 {
		return false, claimed.Error
	}
	c.LeaseToken = leaseToken
	c.LeaseUntil = now + ChannelModelDetectionLeaseSeconds
	c.UpdatedAt = now
	return true, nil
}

func (c *ChannelModelDetectionGlobalConfig) RenewLease(tx *gorm.DB, expectedRevision, now int64, leaseToken string) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || c == nil || c.Id <= 0 || expectedRevision <= 0 || now <= 0 || strings.TrimSpace(leaseToken) == "" {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	renewed := tx.Model(&ChannelModelDetectionGlobalConfig{}).
		Where("id = ? AND revision = ? AND lease_token = ? AND lease_until > ?", c.Id, expectedRevision, leaseToken, now).
		Updates(map[string]any{
			"lease_until": now + ChannelModelDetectionLeaseSeconds,
			"updated_at":  now,
		})
	if renewed.Error != nil || renewed.RowsAffected != 1 {
		return false, renewed.Error
	}
	c.LeaseUntil = now + ChannelModelDetectionLeaseSeconds
	c.UpdatedAt = now
	return true, nil
}

func (c *ChannelModelDetectionGlobalConfig) ReleaseLease(tx *gorm.DB, expectedRevision int64, leaseToken string, now int64) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || c == nil || c.Id <= 0 || expectedRevision <= 0 || strings.TrimSpace(leaseToken) == "" {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	if now <= 0 {
		now = channelModelDetectionNow()
	}
	released := tx.Model(&ChannelModelDetectionGlobalConfig{}).
		Where("id = ? AND revision = ? AND lease_token = ?", c.Id, expectedRevision, leaseToken).
		Updates(map[string]any{
			"lease_token": "",
			"lease_until": int64(0),
			"updated_at":  now,
		})
	if released.Error != nil || released.RowsAffected != 1 {
		return false, released.Error
	}
	c.LeaseToken = ""
	c.LeaseUntil = 0
	c.UpdatedAt = now
	return true, nil
}

func (c *ChannelModelDetectionGlobalConfig) TryAcquireWorkerLease(tx *gorm.DB, now int64, leaseToken string) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || c == nil || c.Id <= 0 || now <= 0 || strings.TrimSpace(leaseToken) == "" {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	claimed := tx.Model(&ChannelModelDetectionGlobalConfig{}).
		Where("id = ? AND worker_lease_until <= ?", c.Id, now).
		Updates(map[string]any{
			"worker_lease_token": leaseToken,
			"worker_lease_until": now + ChannelModelDetectionWorkerLeaseSeconds,
		})
	if claimed.Error != nil || claimed.RowsAffected != 1 {
		return false, claimed.Error
	}
	c.WorkerLeaseToken = leaseToken
	c.WorkerLeaseUntil = now + ChannelModelDetectionWorkerLeaseSeconds
	return true, nil
}

func (c *ChannelModelDetectionGlobalConfig) RenewWorkerLease(tx *gorm.DB, now int64, leaseToken string) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || c == nil || c.Id <= 0 || now <= 0 || strings.TrimSpace(leaseToken) == "" {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	renewed := tx.Model(&ChannelModelDetectionGlobalConfig{}).
		Where("id = ? AND worker_lease_token = ? AND worker_lease_until > ?", c.Id, leaseToken, now).
		Update("worker_lease_until", gorm.Expr(
			"CASE WHEN worker_lease_until >= ? THEN worker_lease_until + 1 ELSE ? END",
			now+ChannelModelDetectionWorkerLeaseSeconds,
			now+ChannelModelDetectionWorkerLeaseSeconds,
		))
	if renewed.Error != nil || renewed.RowsAffected != 1 {
		return false, renewed.Error
	}
	c.WorkerLeaseToken = leaseToken
	c.WorkerLeaseUntil = now + ChannelModelDetectionWorkerLeaseSeconds
	return true, nil
}

func (c *ChannelModelDetectionGlobalConfig) ReleaseWorkerLease(tx *gorm.DB, leaseToken string) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || c == nil || c.Id <= 0 || strings.TrimSpace(leaseToken) == "" {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	released := tx.Model(&ChannelModelDetectionGlobalConfig{}).
		Where("id = ? AND worker_lease_token = ?", c.Id, leaseToken).
		Updates(map[string]any{"worker_lease_token": "", "worker_lease_until": int64(0)})
	if released.Error != nil || released.RowsAffected != 1 {
		return false, released.Error
	}
	c.WorkerLeaseToken = ""
	c.WorkerLeaseUntil = 0
	return true, nil
}

type ChannelModelDetectionConfig struct {
	Id                int64  `json:"id" gorm:"primaryKey"`
	ChannelId         int    `json:"channel_id" gorm:"not null;uniqueIndex;index:idx_channel_model_detection_config_due,priority:1"`
	ScheduleEnabled   bool   `json:"schedule_enabled" gorm:"not null;index:idx_channel_model_detection_config_due,priority:2"`
	ManualRequestId   string `json:"manual_request_id" gorm:"type:varchar(64);index:idx_channel_model_detection_config_manual_due,priority:1"`
	ManualRequestedAt int64  `json:"manual_requested_at" gorm:"bigint;index:idx_channel_model_detection_config_manual_due,priority:2,sort:desc"`
	Revision          int64  `json:"revision" gorm:"bigint;not null"`
	RunningRunId      string `json:"running_run_id" gorm:"type:varchar(64);index"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (c *ChannelModelDetectionConfig) BeforeCreate(_ *gorm.DB) error {
	if c.ChannelId <= 0 {
		return errors.New("渠道 ID 必须为正数")
	}
	if c.Revision == 0 {
		c.Revision = 1
	}
	now := channelModelDetectionNow()
	if c.CreatedAt == 0 {
		c.CreatedAt = now
	}
	if c.UpdatedAt == 0 {
		c.UpdatedAt = c.CreatedAt
	}
	return nil
}

type ChannelModelDetectionTarget struct {
	Id           int64  `json:"id" gorm:"primaryKey"`
	ConfigId     int64  `json:"config_id" gorm:"not null;index"`
	ChannelId    int    `json:"channel_id" gorm:"not null;index;uniqueIndex:idx_channel_model_detection_target_identity"`
	TargetKey    string `json:"target_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	RequestModel string `json:"request_model" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_model_detection_target_identity"`
	ClaimedModel string `json:"claimed_model" gorm:"type:varchar(32);not null;uniqueIndex:idx_channel_model_detection_target_identity"`
	Position     int    `json:"position" gorm:"not null"`
	Enabled      bool   `json:"enabled" gorm:"not null;index"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (target *ChannelModelDetectionTarget) BeforeCreate(_ *gorm.DB) error {
	if target.TargetKey == "" {
		target.TargetKey = common.GetUUID()
	}
	now := channelModelDetectionNow()
	if target.CreatedAt == 0 {
		target.CreatedAt = now
	}
	if target.UpdatedAt == 0 {
		target.UpdatedAt = target.CreatedAt
	}
	return target.Validate()
}

func (target ChannelModelDetectionTarget) Validate() error {
	if target.ChannelId <= 0 || target.ConfigId <= 0 || strings.TrimSpace(target.RequestModel) == "" {
		return errors.New("模型检测目标配置无效")
	}
	if !IsChannelModelDetectionClaimedModel(target.ClaimedModel) {
		return ErrChannelModelDetectionInvalidClaimedModel
	}
	if target.Position < 0 {
		return errors.New("模型检测目标顺序无效")
	}
	return nil
}

type ChannelModelDetectionBatch struct {
	Id                         int64  `json:"id" gorm:"primaryKey"`
	BatchId                    string `json:"batch_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	GlobalConfigRevision       int64  `json:"global_config_revision" gorm:"bigint;not null"`
	Preset                     string `json:"preset" gorm:"type:varchar(16);not null"`
	ScheduledFor               int64  `json:"scheduled_for" gorm:"bigint;not null;uniqueIndex:idx_channel_model_detection_batch_schedule"`
	Status                     string `json:"status" gorm:"type:varchar(32);not null;index"`
	ChannelCount               int    `json:"channel_count" gorm:"not null"`
	RunCount                   int    `json:"run_count" gorm:"not null"`
	CompletedRunCount          int    `json:"completed_run_count" gorm:"not null"`
	FailedRunCount             int    `json:"failed_run_count" gorm:"not null"`
	CanceledRunCount           int    `json:"canceled_run_count" gorm:"not null"`
	EstimatedQuota             int64  `json:"estimated_quota" gorm:"bigint;not null"`
	EstimatedCostNanoCNY       *int64 `json:"estimated_cost_nano_cny" gorm:"bigint"`
	CostEstimateUnknownCount   int64  `json:"cost_estimate_unknown_count" gorm:"bigint;not null"`
	SettledQuota               int64  `json:"settled_quota" gorm:"bigint;not null"`
	CostBasisQuota             int64  `json:"cost_basis_quota" gorm:"bigint;not null"`
	SettledCostNanoCNY         *int64 `json:"settled_cost_nano_cny" gorm:"bigint"`
	UnresolvedCostNanoCNY      *int64 `json:"unresolved_cost_nano_cny" gorm:"bigint"`
	UnresolvedCostUnknownCount int64  `json:"unresolved_cost_unknown_count" gorm:"bigint;not null"`
	SettledRequestCount        int64  `json:"settled_request_count" gorm:"bigint;not null"`
	UnresolvedRequestCount     int64  `json:"unresolved_request_count" gorm:"bigint;not null"`
	CreatedAt                  int64  `json:"created_at" gorm:"bigint;not null;index"`
	FinishedAt                 int64  `json:"finished_at" gorm:"bigint;index"`
	UpdatedAt                  int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (batch *ChannelModelDetectionBatch) BeforeCreate(_ *gorm.DB) error {
	if batch.BatchId == "" {
		batch.BatchId = common.GetUUID()
	}
	if batch.Status == "" {
		batch.Status = ChannelModelDetectionBatchStatusQueued
	}
	now := channelModelDetectionNow()
	if batch.CreatedAt == 0 {
		batch.CreatedAt = now
	}
	if batch.UpdatedAt == 0 {
		batch.UpdatedAt = batch.CreatedAt
	}
	return batch.Validate()
}

func (batch ChannelModelDetectionBatch) Validate() error {
	if strings.TrimSpace(batch.BatchId) == "" || batch.ScheduledFor < 0 || !IsChannelModelDetectionPreset(batch.Preset) {
		return ErrChannelModelDetectionInvalidSchedule
	}
	for _, value := range []int64{batch.EstimatedQuota, batch.CostEstimateUnknownCount, batch.SettledQuota, batch.CostBasisQuota, batch.UnresolvedCostUnknownCount, batch.SettledRequestCount, batch.UnresolvedRequestCount} {
		if err := validateChannelModelDetectionNonNegative(value); err != nil {
			return err
		}
	}
	for _, value := range []*int64{batch.EstimatedCostNanoCNY, batch.SettledCostNanoCNY, batch.UnresolvedCostNanoCNY} {
		if err := validateChannelModelDetectionNullableNonNegative(value); err != nil {
			return err
		}
	}
	return nil
}

type ChannelModelDetectionRun struct {
	Id                         int64   `json:"id" gorm:"primaryKey"`
	RunId                      string  `json:"run_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	BatchId                    *string `json:"batch_id,omitempty" gorm:"type:varchar(64);index"`
	ChannelId                  int     `json:"channel_id" gorm:"not null;index:idx_channel_model_detection_run_channel_created,priority:1"`
	ConfigRevision             int64   `json:"config_revision" gorm:"bigint;not null"`
	GlobalConfigRevision       int64   `json:"global_config_revision" gorm:"bigint;not null"`
	Trigger                    string  `json:"trigger" gorm:"type:varchar(16);not null"`
	Preset                     string  `json:"preset" gorm:"type:varchar(16);not null"`
	PresetSource               string  `json:"preset_source" gorm:"type:varchar(32);not null"`
	Status                     string  `json:"status" gorm:"type:varchar(48);not null;index:idx_channel_model_detection_run_status_updated,priority:1"`
	TargetCount                int     `json:"target_count" gorm:"not null"`
	CompletedTargetCount       int     `json:"completed_target_count" gorm:"not null"`
	PlannedLogicalRequests     int64   `json:"planned_logical_requests" gorm:"bigint;not null"`
	CompletedLogicalRequests   int64   `json:"completed_logical_requests" gorm:"bigint;not null"`
	HTTPAttempts               int64   `json:"http_attempts" gorm:"column:http_attempts;bigint;not null"`
	RetryCount                 int64   `json:"retry_count" gorm:"bigint;not null"`
	ErrorCount                 int64   `json:"error_count" gorm:"bigint;not null"`
	InFlightCount              int64   `json:"in_flight_count" gorm:"bigint;not null"`
	PricingContextUserId       int     `json:"pricing_context_user_id" gorm:"index"`
	BudgetQuotaLimit           int64   `json:"budget_quota_limit" gorm:"bigint;not null"`
	BudgetCostNanoCNY          *int64  `json:"budget_cost_nano_cny" gorm:"bigint"`
	EstimatedQuota             int64   `json:"estimated_quota" gorm:"bigint;not null"`
	EstimatedCostNanoCNY       *int64  `json:"estimated_cost_nano_cny" gorm:"bigint"`
	CostEstimateUnknownCount   int64   `json:"cost_estimate_unknown_count" gorm:"bigint;not null"`
	SettledQuota               int64   `json:"settled_quota" gorm:"bigint;not null"`
	CostBasisQuota             int64   `json:"cost_basis_quota" gorm:"bigint;not null"`
	SettledCostNanoCNY         *int64  `json:"settled_cost_nano_cny" gorm:"bigint"`
	UnresolvedCostNanoCNY      *int64  `json:"unresolved_cost_nano_cny" gorm:"bigint"`
	UnresolvedCostUnknownCount int64   `json:"unresolved_cost_unknown_count" gorm:"bigint;not null"`
	SettledRequestCount        int64   `json:"settled_request_count" gorm:"bigint;not null"`
	UnresolvedRequestCount     int64   `json:"unresolved_request_count" gorm:"bigint;not null"`
	QueuedAt                   int64   `json:"queued_at" gorm:"bigint;index"`
	StartedAt                  int64   `json:"started_at" gorm:"bigint;index"`
	FinishedAt                 int64   `json:"finished_at" gorm:"bigint;index"`
	UpdatedAt                  int64   `json:"updated_at" gorm:"bigint;not null;index:idx_channel_model_detection_run_status_updated,priority:2;index:idx_channel_model_detection_run_channel_created,priority:2,sort:desc"`
	CancelRequestedAt          int64   `json:"cancel_requested_at" gorm:"bigint"`
	ErrorCode                  string  `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage               string  `json:"error_message" gorm:"type:varchar(512)"`
	CreatedByUserId            int     `json:"created_by_user_id" gorm:"index"`
	CreatedByUsername          string  `json:"created_by_username" gorm:"type:varchar(128)"`
	CreatedAt                  int64   `json:"created_at" gorm:"bigint;not null;index:idx_channel_model_detection_run_channel_created,priority:2,sort:desc"`
}

func IsChannelModelDetectionActiveRunStatus(status string) bool {
	switch status {
	case ChannelModelDetectionRunStatusQueued,
		ChannelModelDetectionRunStatusWaitingDetector,
		ChannelModelDetectionRunStatusSubmitting,
		ChannelModelDetectionRunStatusRunning,
		ChannelModelDetectionRunStatusSubmissionUnknown,
		ChannelModelDetectionRunStatusCanceling:
		return true
	default:
		return false
	}
}

// CreateChannelModelDetectionRun makes the channel config's running_run_id the
// cross-database single-active-run guard. The compare-and-swap works on
// SQLite, MySQL and PostgreSQL without a partial-index dependency.
func CreateChannelModelDetectionRun(tx *gorm.DB, run *ChannelModelDetectionRun) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || run == nil || !IsChannelModelDetectionActiveRunStatus(run.Status) && run.Status != "" {
		return false, errors.New("模型检测轮次创建参数无效")
	}
	if run.RunId == "" {
		run.RunId = common.GetUUID()
	}
	var created bool
	err := tx.Transaction(func(tx *gorm.DB) error {
		claimed := tx.Model(&ChannelModelDetectionConfig{}).
			Where("channel_id = ? AND running_run_id = ?", run.ChannelId, "").
			Updates(map[string]any{"running_run_id": run.RunId, "updated_at": channelModelDetectionNow()})
		if claimed.Error != nil {
			return claimed.Error
		}
		if claimed.RowsAffected != 1 {
			return nil
		}
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

func ReleaseChannelModelDetectionRun(tx *gorm.DB, channelId int, runId string, now int64) (bool, error) {
	if tx == nil {
		tx = DB
	}
	if tx == nil || channelId <= 0 || strings.TrimSpace(runId) == "" {
		return false, errors.New("模型检测轮次释放参数无效")
	}
	if now <= 0 {
		now = channelModelDetectionNow()
	}
	updated := tx.Model(&ChannelModelDetectionConfig{}).
		Where("channel_id = ? AND running_run_id = ?", channelId, runId).
		Updates(map[string]any{"running_run_id": "", "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

func (run *ChannelModelDetectionRun) BeforeCreate(_ *gorm.DB) error {
	if run.RunId == "" {
		run.RunId = common.GetUUID()
	}
	if run.Status == "" {
		run.Status = ChannelModelDetectionRunStatusQueued
	}
	if run.PresetSource == "" {
		if run.Trigger == ChannelModelDetectionTriggerManual {
			run.PresetSource = ChannelModelDetectionPresetSourceManualSelected
		} else {
			run.PresetSource = ChannelModelDetectionPresetSourceScheduledDefault
		}
	}
	now := channelModelDetectionNow()
	if run.QueuedAt == 0 {
		run.QueuedAt = now
	}
	if run.CreatedAt == 0 {
		run.CreatedAt = run.QueuedAt
	}
	if run.UpdatedAt == 0 {
		run.UpdatedAt = run.QueuedAt
	}
	return run.Validate()
}

func (run ChannelModelDetectionRun) Validate() error {
	if strings.TrimSpace(run.RunId) == "" || run.ChannelId <= 0 {
		return errors.New("模型检测轮次无效")
	}
	if !IsChannelModelDetectionTrigger(run.Trigger) {
		return ErrChannelModelDetectionInvalidTrigger
	}
	if !IsChannelModelDetectionPreset(run.Preset) {
		return ErrChannelModelDetectionInvalidPreset
	}
	if run.Trigger == ChannelModelDetectionTriggerManual && run.PresetSource != ChannelModelDetectionPresetSourceManualSelected {
		return ErrChannelModelDetectionInvalidTrigger
	}
	if run.Trigger == ChannelModelDetectionTriggerScheduled && run.PresetSource != ChannelModelDetectionPresetSourceScheduledDefault {
		return ErrChannelModelDetectionInvalidTrigger
	}
	for _, value := range []int64{run.PlannedLogicalRequests, run.CompletedLogicalRequests, run.HTTPAttempts, run.RetryCount, run.ErrorCount, run.InFlightCount, run.BudgetQuotaLimit, run.EstimatedQuota, run.CostEstimateUnknownCount, run.SettledQuota, run.CostBasisQuota, run.UnresolvedCostUnknownCount, run.SettledRequestCount, run.UnresolvedRequestCount} {
		if err := validateChannelModelDetectionNonNegative(value); err != nil {
			return err
		}
	}
	for _, value := range []*int64{run.BudgetCostNanoCNY, run.EstimatedCostNanoCNY, run.SettledCostNanoCNY, run.UnresolvedCostNanoCNY} {
		if err := validateChannelModelDetectionNullableNonNegative(value); err != nil {
			return err
		}
	}
	return nil
}

type ChannelModelDetectionExecution struct {
	Id                         int64  `json:"id" gorm:"primaryKey"`
	RunId                      string `json:"run_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_model_detection_execution_target"`
	TargetKey                  string `json:"target_key" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_model_detection_execution_target"`
	TargetId                   int64  `json:"target_id" gorm:"not null;index"`
	ChannelId                  int    `json:"channel_id" gorm:"not null;index"`
	RequestModel               string `json:"request_model" gorm:"type:varchar(255);not null"`
	ClaimedModel               string `json:"claimed_model" gorm:"type:varchar(32);not null"`
	Preset                     string `json:"preset" gorm:"type:varchar(16);not null"`
	Status                     string `json:"status" gorm:"type:varchar(32);not null;index"`
	OutcomeCode                string `json:"outcome_code" gorm:"type:varchar(96);index"`
	TitleCN                    string `json:"title_cn" gorm:"column:title_cn;type:varchar(255)"`
	SubtitleCN                 string `json:"subtitle_cn" gorm:"column:subtitle_cn;type:varchar(512)"`
	JuiceVerdictState          string `json:"juice_verdict_state" gorm:"type:varchar(32)"`
	FingerprintVerdictState    string `json:"fingerprint_verdict_state" gorm:"type:varchar(32)"`
	FingerprintModel           string `json:"fingerprint_model" gorm:"type:varchar(255)"`
	OfficialSessionId          string `json:"official_session_id" gorm:"type:varchar(128);index"`
	Official                   bool   `json:"official" gorm:"not null"`
	ConfigHash                 string `json:"config_hash" gorm:"type:varchar(128)"`
	OfficialConfigJSON         string `json:"-" gorm:"column:official_config_json;type:text"`
	DetectorURLSnapshot        string `json:"-" gorm:"column:detector_url_snapshot;type:varchar(1024)"`
	SchemaVersion              int    `json:"schema_version" gorm:"not null"`
	ScoringVersion             string `json:"scoring_version" gorm:"type:varchar(128)"`
	BaselineId                 string `json:"baseline_id" gorm:"type:varchar(128)"`
	BaselineSHA256             string `json:"baseline_sha256" gorm:"type:varchar(128)"`
	BuildHash                  string `json:"build_hash" gorm:"type:varchar(128)"`
	PlannedLogicalRequests     int64  `json:"planned_logical_requests" gorm:"bigint;not null"`
	CompletedLogicalRequests   int64  `json:"completed_logical_requests" gorm:"bigint;not null"`
	HTTPAttempts               int64  `json:"http_attempts" gorm:"column:http_attempts;bigint;not null"`
	RetryCount                 int64  `json:"retry_count" gorm:"bigint;not null"`
	FinalErrorCode             string `json:"final_error_code" gorm:"type:varchar(128)"`
	Canceled                   bool   `json:"canceled" gorm:"not null"`
	UsageAvailable             bool   `json:"usage_available" gorm:"not null"`
	InputTokens                int64  `json:"input_tokens" gorm:"bigint;not null"`
	OutputTokens               int64  `json:"output_tokens" gorm:"bigint;not null"`
	TotalTokens                int64  `json:"total_tokens" gorm:"bigint;not null"`
	EstimatedQuota             int64  `json:"estimated_quota" gorm:"bigint;not null"`
	EstimatedCostNanoCNY       *int64 `json:"estimated_cost_nano_cny" gorm:"bigint"`
	CostEstimateUnknownCount   int64  `json:"cost_estimate_unknown_count" gorm:"bigint;not null"`
	SettledQuota               int64  `json:"settled_quota" gorm:"bigint;not null"`
	CostBasisQuota             int64  `json:"cost_basis_quota" gorm:"bigint;not null"`
	SettledCostNanoCNY         *int64 `json:"settled_cost_nano_cny" gorm:"bigint"`
	UnresolvedCostNanoCNY      *int64 `json:"unresolved_cost_nano_cny" gorm:"bigint"`
	UnresolvedCostUnknownCount int64  `json:"unresolved_cost_unknown_count" gorm:"bigint;not null"`
	SettledRequestCount        int64  `json:"settled_request_count" gorm:"bigint;not null"`
	UnresolvedRequestCount     int64  `json:"unresolved_request_count" gorm:"bigint;not null"`
	ReportJSON                 string `json:"-" gorm:"column:report_json;type:text"`
	ReportSHA256               string `json:"report_sha256" gorm:"type:varchar(128)"`
	StartedAt                  int64  `json:"started_at" gorm:"bigint;index"`
	FinishedAt                 int64  `json:"finished_at" gorm:"bigint;index"`
	ErrorCode                  string `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage               string `json:"error_message" gorm:"type:varchar(512)"`
	CreatedAt                  int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt                  int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (execution *ChannelModelDetectionExecution) BeforeCreate(_ *gorm.DB) error {
	if execution.Status == "" {
		execution.Status = ChannelModelDetectionExecutionStatusPending
	}
	now := channelModelDetectionNow()
	if execution.CreatedAt == 0 {
		execution.CreatedAt = now
	}
	if execution.UpdatedAt == 0 {
		execution.UpdatedAt = execution.CreatedAt
	}
	return execution.Validate()
}

func (execution ChannelModelDetectionExecution) Validate() error {
	if strings.TrimSpace(execution.RunId) == "" || strings.TrimSpace(execution.TargetKey) == "" || execution.TargetId <= 0 || execution.ChannelId <= 0 {
		return errors.New("模型检测目标执行无效")
	}
	if !IsChannelModelDetectionPreset(execution.Preset) {
		return ErrChannelModelDetectionInvalidPreset
	}
	for _, value := range []int64{execution.PlannedLogicalRequests, execution.CompletedLogicalRequests, execution.HTTPAttempts, execution.RetryCount, execution.InputTokens, execution.OutputTokens, execution.TotalTokens, execution.EstimatedQuota, execution.CostEstimateUnknownCount, execution.SettledQuota, execution.CostBasisQuota, execution.UnresolvedCostUnknownCount, execution.SettledRequestCount, execution.UnresolvedRequestCount} {
		if err := validateChannelModelDetectionNonNegative(value); err != nil {
			return err
		}
	}
	for _, value := range []*int64{execution.EstimatedCostNanoCNY, execution.SettledCostNanoCNY, execution.UnresolvedCostNanoCNY} {
		if err := validateChannelModelDetectionNullableNonNegative(value); err != nil {
			return err
		}
	}
	if len(execution.ReportJSON) > ChannelModelDetectionMaxReportBytes {
		return fmt.Errorf("模型检测报告超过 %d 字节", ChannelModelDetectionMaxReportBytes)
	}
	return nil
}

func (execution ChannelModelDetectionExecution) OfficialConfig() (map[string]any, error) {
	value := map[string]any{}
	if err := decodeChannelModelDetectionJSON(execution.OfficialConfigJSON, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (execution *ChannelModelDetectionExecution) SetOfficialConfig(value any) error {
	encoded, err := marshalChannelModelDetectionJSON(value)
	if err != nil {
		return err
	}
	execution.OfficialConfigJSON = encoded
	return nil
}

func (execution ChannelModelDetectionExecution) Report() (map[string]any, error) {
	value := map[string]any{}
	if err := decodeChannelModelDetectionJSON(execution.ReportJSON, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (execution *ChannelModelDetectionExecution) SetReport(value any) error {
	encoded, err := marshalChannelModelDetectionJSON(value)
	if err != nil {
		return err
	}
	if len(encoded) > ChannelModelDetectionMaxReportBytes {
		return fmt.Errorf("模型检测报告超过 %d 字节", ChannelModelDetectionMaxReportBytes)
	}
	execution.ReportJSON = encoded
	return nil
}

type ChannelModelDetectionCostEvent struct {
	Id                     int64   `json:"id" gorm:"primaryKey"`
	CostEventId            string  `json:"cost_event_id" gorm:"column:cost_event_id;type:varchar(64);not null;uniqueIndex"`
	RunId                  string  `json:"run_id" gorm:"type:varchar(64);not null;index"`
	TargetId               int64   `json:"target_id" gorm:"not null;index"`
	ExecutionId            int64   `json:"execution_id" gorm:"not null;index"`
	ChannelId              int     `json:"channel_id" gorm:"not null;index"`
	RequestModel           string  `json:"request_model" gorm:"type:varchar(255);not null"`
	ClaimedModel           string  `json:"claimed_model" gorm:"type:varchar(32);not null"`
	Preset                 string  `json:"preset" gorm:"type:varchar(16);not null"`
	DetectorRequestId      string  `json:"detector_request_id" gorm:"type:varchar(128);index;uniqueIndex:idx_channel_model_detection_cost_attempt"`
	AttemptNo              int     `json:"attempt_no" gorm:"not null;uniqueIndex:idx_channel_model_detection_cost_attempt"`
	RequestId              string  `json:"request_id" gorm:"type:varchar(128);index"`
	UpstreamRequestId      string  `json:"upstream_request_id" gorm:"type:varchar(128);index"`
	UpstreamKeyId          string  `json:"upstream_key_id" gorm:"type:varchar(128)"`
	UpstreamKeyFingerprint string  `json:"upstream_key_fingerprint" gorm:"type:varchar(128)"`
	UpstreamKeyDisplay     string  `json:"upstream_key_display" gorm:"type:varchar(128)"`
	DispatchState          string  `json:"dispatch_state" gorm:"type:varchar(16);not null;index:idx_channel_model_detection_cost_dispatch"`
	SettlementStatus       string  `json:"settlement_status" gorm:"type:varchar(24);not null;index:idx_channel_model_detection_cost_settlement"`
	UsageSource            string  `json:"usage_source" gorm:"type:varchar(32);not null"`
	UsageAvailable         bool    `json:"usage_available" gorm:"not null"`
	InputTokens            int64   `json:"input_tokens" gorm:"bigint;not null"`
	OutputTokens           int64   `json:"output_tokens" gorm:"bigint;not null"`
	TotalTokens            int64   `json:"total_tokens" gorm:"bigint;not null"`
	EstimatedQuota         int64   `json:"estimated_quota" gorm:"bigint;not null"`
	SettledQuota           *int64  `json:"settled_quota" gorm:"bigint"`
	CostBasisQuota         *int64  `json:"cost_basis_quota" gorm:"bigint"`
	EstimatedCostNanoCNY   *int64  `json:"estimated_cost_nano_cny" gorm:"bigint"`
	SettledCostNanoCNY     *int64  `json:"settled_cost_nano_cny" gorm:"bigint"`
	CostRatioCNY           *string `json:"cost_ratio_cny" gorm:"type:varchar(64)"`
	QuotaPerUnit           *int64  `json:"quota_per_unit" gorm:"bigint"`
	CostScope              string  `json:"cost_scope" gorm:"type:varchar(64);not null"`
	ErrorCode              string  `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage           string  `json:"error_message" gorm:"type:varchar(512)"`
	CreatedAt              int64   `json:"created_at" gorm:"bigint;not null;index:idx_channel_model_detection_cost_created"`
	SettledAt              int64   `json:"settled_at" gorm:"bigint;index"`
	UpdatedAt              int64   `json:"updated_at" gorm:"bigint;not null"`
}

func (event *ChannelModelDetectionCostEvent) BeforeCreate(_ *gorm.DB) error {
	if event.CostEventId == "" {
		event.CostEventId = common.GetUUID()
	}
	if event.DispatchState == "" {
		event.DispatchState = ChannelModelDetectionDispatchPrepared
	}
	if event.SettlementStatus == "" {
		event.SettlementStatus = ChannelModelDetectionSettlementPending
	}
	if event.UsageSource == "" {
		event.UsageSource = ChannelModelDetectionUsageUnavailable
	}
	if event.CostScope == "" {
		event.CostScope = ChannelModelDetectionCostScopeChannelUpstreamAPI
	}
	now := channelModelDetectionNow()
	if event.CreatedAt == 0 {
		event.CreatedAt = now
	}
	if event.UpdatedAt == 0 {
		event.UpdatedAt = event.CreatedAt
	}
	return event.Validate()
}

func (event ChannelModelDetectionCostEvent) Validate() error {
	if strings.TrimSpace(event.CostEventId) == "" || strings.TrimSpace(event.RunId) == "" || event.TargetId <= 0 || event.ExecutionId <= 0 || event.ChannelId <= 0 || event.AttemptNo <= 0 {
		return ErrChannelModelDetectionInvalidCost
	}
	if !IsChannelModelDetectionPreset(event.Preset) || !IsChannelModelDetectionDispatchState(event.DispatchState) || !IsChannelModelDetectionSettlementStatus(event.SettlementStatus) || !IsChannelModelDetectionUsageSource(event.UsageSource) || event.CostScope != ChannelModelDetectionCostScopeChannelUpstreamAPI {
		return ErrChannelModelDetectionInvalidCost
	}
	if event.DispatchState == ChannelModelDetectionDispatchNotStarted && event.SettlementStatus != ChannelModelDetectionSettlementNotApplicable {
		return ErrChannelModelDetectionInvalidCost
	}
	if event.DispatchState == ChannelModelDetectionDispatchDispatched && event.SettlementStatus == ChannelModelDetectionSettlementNotApplicable {
		return ErrChannelModelDetectionInvalidCost
	}
	if event.DispatchState == ChannelModelDetectionDispatchPrepared && event.SettlementStatus != ChannelModelDetectionSettlementPending {
		return ErrChannelModelDetectionInvalidCost
	}
	for _, value := range []int64{event.InputTokens, event.OutputTokens, event.TotalTokens, event.EstimatedQuota} {
		if err := validateChannelModelDetectionNonNegative(value); err != nil {
			return err
		}
	}
	for _, value := range []*int64{event.SettledQuota, event.CostBasisQuota, event.EstimatedCostNanoCNY, event.SettledCostNanoCNY, event.QuotaPerUnit} {
		if err := validateChannelModelDetectionNullableNonNegative(value); err != nil {
			return err
		}
	}
	if event.QuotaPerUnit != nil && *event.QuotaPerUnit == 0 {
		return ErrChannelModelDetectionInvalidCost
	}
	if event.CostRatioCNY != nil && strings.TrimSpace(*event.CostRatioCNY) == "" {
		return ErrChannelModelDetectionInvalidCost
	}
	if event.SettlementStatus == ChannelModelDetectionSettlementSettled && event.SettledQuota == nil {
		return ErrChannelModelDetectionInvalidCost
	}
	return nil
}

func (event *ChannelModelDetectionCostEvent) MarkDispatched(now int64) error {
	if event.DispatchState != ChannelModelDetectionDispatchPrepared {
		return ErrChannelModelDetectionInvalidCost
	}
	event.DispatchState = ChannelModelDetectionDispatchDispatched
	event.SettlementStatus = ChannelModelDetectionSettlementPending
	if now > 0 {
		event.UpdatedAt = now
	}
	return nil
}

func (event *ChannelModelDetectionCostEvent) MarkNotStarted(now int64) error {
	if event.DispatchState != ChannelModelDetectionDispatchPrepared {
		return ErrChannelModelDetectionInvalidCost
	}
	event.DispatchState = ChannelModelDetectionDispatchNotStarted
	event.SettlementStatus = ChannelModelDetectionSettlementNotApplicable
	if now > 0 {
		event.UpdatedAt = now
	}
	return nil
}

func (event *ChannelModelDetectionCostEvent) MarkSettled(now int64, settledQuota, costBasisQuota int64, costNanoCNY *int64, inputTokens, outputTokens, totalTokens int64, usageSource string) error {
	if event.DispatchState != ChannelModelDetectionDispatchDispatched || event.SettlementStatus == ChannelModelDetectionSettlementNotApplicable {
		return ErrChannelModelDetectionInvalidCost
	}
	if event.SettlementStatus == ChannelModelDetectionSettlementSettled {
		return ErrChannelModelDetectionInvalidCost
	}
	if settledQuota < 0 || costBasisQuota < 0 || inputTokens < 0 || outputTokens < 0 || totalTokens < 0 || !IsChannelModelDetectionUsageSource(usageSource) || usageSource == ChannelModelDetectionUsageUnavailable {
		return ErrChannelModelDetectionInvalidCost
	}
	if err := validateChannelModelDetectionNullableNonNegative(costNanoCNY); err != nil {
		return err
	}
	event.SettlementStatus = ChannelModelDetectionSettlementSettled
	event.UsageSource = usageSource
	event.UsageAvailable = usageSource == ChannelModelDetectionUsageUpstreamAuthoritative
	event.SettledQuota = &settledQuota
	event.CostBasisQuota = &costBasisQuota
	event.SettledCostNanoCNY = costNanoCNY
	event.InputTokens = inputTokens
	event.OutputTokens = outputTokens
	event.TotalTokens = totalTokens
	if now <= 0 {
		now = channelModelDetectionNow()
	}
	event.SettledAt = now
	event.UpdatedAt = now
	return nil
}

func (event *ChannelModelDetectionCostEvent) MarkUnresolved(now int64, unresolvedCostNanoCNY *int64, usageSource string) error {
	if event.DispatchState != ChannelModelDetectionDispatchDispatched || event.SettlementStatus != ChannelModelDetectionSettlementPending {
		return ErrChannelModelDetectionInvalidCost
	}
	if usageSource != ChannelModelDetectionUsageLocalEstimate && usageSource != ChannelModelDetectionUsageUnavailable {
		return ErrChannelModelDetectionInvalidCost
	}
	if err := validateChannelModelDetectionNullableNonNegative(unresolvedCostNanoCNY); err != nil {
		return err
	}
	event.SettlementStatus = ChannelModelDetectionSettlementUnresolved
	event.UsageSource = usageSource
	event.UsageAvailable = false
	if unresolvedCostNanoCNY != nil {
		event.EstimatedCostNanoCNY = unresolvedCostNanoCNY
	}
	if now <= 0 {
		now = channelModelDetectionNow()
	}
	event.UpdatedAt = now
	return nil
}

func (event *ChannelModelDetectionCostEvent) IsCostKnown() bool {
	if event == nil || event.DispatchState != ChannelModelDetectionDispatchDispatched {
		return false
	}
	switch event.SettlementStatus {
	case ChannelModelDetectionSettlementSettled:
		return event.SettledCostNanoCNY != nil
	case ChannelModelDetectionSettlementUnresolved:
		return event.EstimatedCostNanoCNY != nil
	default:
		return false
	}
}

func CheckChannelModelDetectionCostValues(values ...float64) error {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return ErrChannelModelDetectionInvalidCost
		}
	}
	return nil
}
