package model

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ChannelStatusProbeResultSuccess         = "success"
	ChannelStatusProbeResultUpstreamFailure = "upstream_failure"
	ChannelStatusProbeResultRateLimited     = "rate_limited"
	ChannelStatusProbeResultLocalFailure    = "local_failure"
	ChannelStatusProbeResultSkipped         = "skipped"
	ChannelStatusProbeResultCanceled        = "canceled"

	ChannelStatusProbeTriggerScheduled = "scheduled"
	ChannelStatusProbeTriggerManual    = "manual"

	ChannelStatusProbeSamplePending  = "pending"
	ChannelStatusProbeSampleRecorded = "recorded"
	ChannelStatusProbeSampleSkipped  = "skipped"
	ChannelStatusProbeSampleFailed   = "failed"

	ChannelMonitorStatusProbeLogKey = "channel_monitor_status_probe"

	ChannelStatusProbeDefaultIntervalSeconds = 300
	ChannelStatusProbeMinIntervalSeconds     = 30
	ChannelStatusProbeMaxIntervalSeconds     = 86400
	ChannelStatusProbeDisplayUnitMinute      = "minute"
	ChannelStatusProbeDisplayUnitHour        = "hour"
	ChannelStatusProbeDisplayUnitDay         = "day"
	ChannelStatusProbeDefaultDisplayValue    = 60
	ChannelStatusProbeDefaultDisplayUnit     = ChannelStatusProbeDisplayUnitMinute
	ChannelStatusProbeMaxDisplayMinutes      = 60
	ChannelStatusProbeMaxDisplayHours        = 24
	ChannelStatusProbeMaxDisplayDays         = 30
	ChannelStatusProbeMaxModels              = 20
	ChannelStatusProbeLeaseSeconds           = 600
)

var (
	ErrChannelStatusProbeConfigChanged = errors.New("渠道状态探测配置已被其他请求修改，请刷新后重试")
	ErrChannelStatusProbeNotConfigured = errors.New("请先保存至少一个探测模型")
	ErrChannelStatusProbeManualPending = errors.New("该渠道已有待执行或正在执行的立即检测")
)

type ChannelStatusProbeConfig struct {
	Id                int64  `json:"id"`
	ChannelId         int    `json:"channel_id" gorm:"not null;uniqueIndex"`
	Enabled           bool   `json:"enabled"`
	ModelsJSON        string `json:"-" gorm:"type:text;not null"`
	IntervalSeconds   int    `json:"interval_seconds"`
	DisplayValue      int    `json:"display_value"`
	DisplayUnit       string `json:"display_unit" gorm:"type:varchar(16)"`
	RecordSample      bool   `json:"record_sample"`
	NextRunAt         int64  `json:"next_run_at" gorm:"bigint;index"`
	ManualRequestId   string `json:"manual_request_id" gorm:"type:varchar(64)"`
	ManualRequestedAt int64  `json:"manual_requested_at" gorm:"bigint;index"`
	Revision          int64  `json:"revision" gorm:"bigint"`
	LeaseToken        string `json:"-" gorm:"type:varchar(64)"`
	LeaseUntil        int64  `json:"lease_until" gorm:"bigint;index"`
	RunningTrigger    string `json:"running_trigger" gorm:"type:varchar(16)"`
	RunningRunId      string `json:"running_run_id" gorm:"type:varchar(64)"`
	RunningStartedAt  int64  `json:"running_started_at" gorm:"bigint"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint"`
}

func (config ChannelStatusProbeConfig) Models() ([]string, error) {
	models := []string{}
	if strings.TrimSpace(config.ModelsJSON) == "" {
		return models, nil
	}
	if err := common.UnmarshalJsonStr(config.ModelsJSON, &models); err != nil {
		return nil, fmt.Errorf("解析渠道状态探测模型失败: %w", err)
	}
	return models, nil
}

func ChannelStatusProbeDisplayLimit(unit string) int {
	switch unit {
	case ChannelStatusProbeDisplayUnitMinute:
		return ChannelStatusProbeMaxDisplayMinutes
	case ChannelStatusProbeDisplayUnitHour:
		return ChannelStatusProbeMaxDisplayHours
	case ChannelStatusProbeDisplayUnitDay:
		return ChannelStatusProbeMaxDisplayDays
	default:
		return 0
	}
}

func IsChannelStatusProbeDisplayAllowed(value int, unit string) bool {
	limit := ChannelStatusProbeDisplayLimit(unit)
	return value >= 1 && value <= limit
}

func NormalizeChannelStatusProbeDisplay(value int, unit string) (int, string) {
	if IsChannelStatusProbeDisplayAllowed(value, unit) {
		return value, unit
	}
	return ChannelStatusProbeDefaultDisplayValue, ChannelStatusProbeDefaultDisplayUnit
}

func ChannelStatusProbeDisplayBucketSeconds(unit string) int64 {
	switch unit {
	case ChannelStatusProbeDisplayUnitHour:
		return 60 * 60
	case ChannelStatusProbeDisplayUnitDay:
		return 24 * 60 * 60
	default:
		return 60
	}
}

func ChannelStatusProbeDisplayBucketStart(timestamp int64, unit string) int64 {
	bucketSeconds := ChannelStatusProbeDisplayBucketSeconds(unit)
	if unit == ChannelStatusProbeDisplayUnitDay {
		return (timestamp+channelMonitorCostTimezoneOffsetSeconds)/bucketSeconds*bucketSeconds -
			channelMonitorCostTimezoneOffsetSeconds
	}
	return timestamp - timestamp%bucketSeconds
}

type ChannelStatusProbeBucket struct {
	StartedAt             int64    `json:"started_at"`
	Success               int      `json:"success"`
	UpstreamFailure       int      `json:"upstream_failure"`
	RateLimited           int      `json:"rate_limited"`
	LocalFailure          int      `json:"local_failure"`
	Skipped               int      `json:"skipped"`
	Canceled              int      `json:"canceled"`
	Models                []string `json:"models,omitempty"`
	FirstTokenTotalMs     float64  `json:"first_token_total_ms,omitempty"`
	FirstTokenSampleCount int64    `json:"first_token_sample_count,omitempty"`
	TPSTotal              float64  `json:"tps_total,omitempty"`
	TPSSampleCount        int64    `json:"tps_sample_count,omitempty"`
}

func (bucket *ChannelStatusProbeBucket) Add(
	result string,
	modelName string,
	firstTokenMs *float64,
	tokensPerSecond *float64,
) {
	switch result {
	case ChannelStatusProbeResultSuccess:
		bucket.Success++
		if firstTokenMs != nil {
			bucket.FirstTokenTotalMs += *firstTokenMs
			bucket.FirstTokenSampleCount++
		}
		if tokensPerSecond != nil {
			bucket.TPSTotal += *tokensPerSecond
			bucket.TPSSampleCount++
		}
	case ChannelStatusProbeResultUpstreamFailure:
		bucket.UpstreamFailure++
	case ChannelStatusProbeResultRateLimited:
		bucket.RateLimited++
	case ChannelStatusProbeResultLocalFailure:
		bucket.LocalFailure++
	case ChannelStatusProbeResultSkipped:
		bucket.Skipped++
	case ChannelStatusProbeResultCanceled:
		bucket.Canceled++
	}
	for _, existing := range bucket.Models {
		if existing == modelName {
			return
		}
	}
	if modelName != "" {
		bucket.Models = append(bucket.Models, modelName)
		sort.Strings(bucket.Models)
	}
}

type ChannelStatusProbeState struct {
	Id                    int64    `json:"id"`
	ChannelId             int      `json:"channel_id" gorm:"not null;uniqueIndex:idx_channel_status_probe_state_model"`
	ModelName             string   `json:"model_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_status_probe_state_model"`
	ExecutionId           int64    `json:"execution_id" gorm:"bigint"`
	RunId                 string   `json:"run_id" gorm:"type:varchar(64)"`
	StartedAt             int64    `json:"started_at" gorm:"bigint"`
	FinishedAt            int64    `json:"finished_at" gorm:"bigint;index"`
	Result                string   `json:"result" gorm:"type:varchar(32)"`
	Success               bool     `json:"success"`
	RequestDispatched     bool     `json:"request_dispatched"`
	ResponseTimeMs        *float64 `json:"response_time_ms"`
	FirstTokenMs          *float64 `json:"first_token_ms"`
	TPS                   *float64 `json:"tps"`
	ErrorCode             string   `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage          string   `json:"error_message" gorm:"type:varchar(512)"`
	ConsecutiveSuccesses  int      `json:"consecutive_successes"`
	ConsecutiveFailures   int      `json:"consecutive_failures"`
	LastHealthResult      string   `json:"last_health_result" gorm:"type:varchar(32)"`
	LastHealthExecutionId int64    `json:"last_health_execution_id" gorm:"bigint"`
	LastHealthFinishedAt  int64    `json:"last_health_finished_at" gorm:"bigint;index"`
	SampleStatus          string   `json:"sample_status" gorm:"type:varchar(16)"`
	SampleMessage         string   `json:"sample_message" gorm:"type:varchar(255)"`
	Trigger               string   `json:"trigger" gorm:"type:varchar(16)"`
	Endpoint              string   `json:"endpoint" gorm:"type:varchar(255)"`
	Stream                bool     `json:"stream"`
	MinuteBucketsJSON     string   `json:"-" gorm:"type:text"`
	HourBucketsJSON       string   `json:"-" gorm:"type:text"`
	DayBucketsJSON        string   `json:"-" gorm:"type:text"`
	CreatedAt             int64    `json:"created_at" gorm:"bigint"`
	UpdatedAt             int64    `json:"updated_at" gorm:"bigint"`
}

func (state ChannelStatusProbeState) Buckets(unit string) ([]ChannelStatusProbeBucket, error) {
	raw := state.MinuteBucketsJSON
	label := "分钟"
	switch unit {
	case ChannelStatusProbeDisplayUnitHour:
		raw = state.HourBucketsJSON
		label = "小时"
	case ChannelStatusProbeDisplayUnitDay:
		raw = state.DayBucketsJSON
		label = "天"
	case ChannelStatusProbeDisplayUnitMinute:
	default:
		return nil, errors.New("渠道状态探测展示单位无效")
	}
	buckets := []ChannelStatusProbeBucket{}
	if strings.TrimSpace(raw) == "" {
		return buckets, nil
	}
	if err := common.UnmarshalJsonStr(raw, &buckets); err != nil {
		return nil, fmt.Errorf("解析渠道状态探测%s结果失败: %w", label, err)
	}
	return buckets, nil
}

type ChannelStatusProbeExecution struct {
	Id                int64    `json:"id"`
	RunId             string   `json:"run_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_status_probe_run_model"`
	ChannelId         int      `json:"channel_id" gorm:"not null;index:idx_channel_status_probe_channel_finished,priority:1;index:idx_channel_status_probe_channel_model_finished,priority:1"`
	ModelName         string   `json:"model_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_status_probe_run_model;index:idx_channel_status_probe_channel_model_finished,priority:2"`
	ConfigRevision    int64    `json:"config_revision" gorm:"bigint"`
	Trigger           string   `json:"trigger" gorm:"type:varchar(16);index"`
	Result            string   `json:"result" gorm:"type:varchar(32);index"`
	StartedAt         int64    `json:"started_at" gorm:"bigint"`
	FinishedAt        int64    `json:"finished_at" gorm:"bigint;index;index:idx_channel_status_probe_channel_finished,priority:2,sort:desc;index:idx_channel_status_probe_channel_model_finished,priority:3,sort:desc"`
	ResponseTimeMs    *float64 `json:"response_time_ms"`
	FirstTokenMs      *float64 `json:"first_token_ms"`
	TPS               *float64 `json:"tps"`
	Endpoint          string   `json:"endpoint" gorm:"type:varchar(255)"`
	Stream            bool     `json:"stream"`
	RequestId         string   `json:"request_id" gorm:"type:varchar(64)"`
	RequestDispatched bool     `json:"request_dispatched"`
	UsageAvailable    bool     `json:"usage_available"`
	InputTokens       int      `json:"input_tokens"`
	OutputTokens      int      `json:"output_tokens"`
	TotalTokens       int      `json:"total_tokens"`
	CachedTokens      int      `json:"cached_tokens"`
	CacheWriteTokens  int      `json:"cache_write_tokens"`
	ReasoningTokens   int      `json:"reasoning_tokens"`
	ErrorCode         string   `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage      string   `json:"error_message" gorm:"type:varchar(512)"`
	SampleRequested   bool     `json:"sample_requested"`
	SampleStatus      string   `json:"sample_status" gorm:"type:varchar(16)"`
	SampleMessage     string   `json:"sample_message" gorm:"type:varchar(255)"`
	CreatedAt         int64    `json:"created_at" gorm:"bigint"`
}

type ChannelStatusProbeConfigInput struct {
	Enabled         bool
	Models          []string
	IntervalSeconds int
	DisplayValue    int
	DisplayUnit     string
	RecordSample    bool
	Revision        int64
}

type ChannelStatusProbeClaim struct {
	Config          ChannelStatusProbeConfig
	Models          []string
	Trigger         string
	RunId           string
	ManualRequestId string
	LeaseToken      string
}

func SaveChannelStatusProbeConfig(channelId int, input ChannelStatusProbeConfigInput, now int64) (ChannelStatusProbeConfig, error) {
	modelsJSON, err := common.Marshal(input.Models)
	if err != nil {
		return ChannelStatusProbeConfig{}, err
	}
	displayValue, displayUnit := NormalizeChannelStatusProbeDisplay(input.DisplayValue, input.DisplayUnit)
	var saved ChannelStatusProbeConfig
	err = DB.Transaction(func(tx *gorm.DB) error {
		var current ChannelStatusProbeConfig
		findErr := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&current).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if input.Revision != 0 {
				return ErrChannelStatusProbeConfigChanged
			}
			nextRunAt := int64(0)
			if input.Enabled {
				nextRunAt = now + int64(input.IntervalSeconds) + int64(channelId%11)
			}
			saved = ChannelStatusProbeConfig{
				ChannelId: channelId, Enabled: input.Enabled, ModelsJSON: string(modelsJSON),
				IntervalSeconds: input.IntervalSeconds,
				DisplayValue:    displayValue,
				DisplayUnit:     displayUnit,
				RecordSample:    input.RecordSample,
				NextRunAt:       nextRunAt, Revision: 1, CreatedAt: now, UpdatedAt: now,
			}
			return tx.Create(&saved).Error
		}
		if findErr != nil {
			return findErr
		}
		if current.Revision != input.Revision {
			return ErrChannelStatusProbeConfigChanged
		}
		nextRunAt := current.NextRunAt
		configurationChanged := current.ModelsJSON != string(modelsJSON) || current.IntervalSeconds != input.IntervalSeconds
		if !input.Enabled {
			nextRunAt = 0
		} else if !current.Enabled || configurationChanged || nextRunAt <= 0 {
			nextRunAt = now + int64(input.IntervalSeconds) + int64(channelId%11)
		}
		updates := map[string]any{
			"enabled": input.Enabled, "models_json": string(modelsJSON),
			"interval_seconds": input.IntervalSeconds,
			"display_value":    displayValue,
			"display_unit":     displayUnit,
			"record_sample":    input.RecordSample,
			"next_run_at":      nextRunAt, "revision": current.Revision + 1, "updated_at": now,
		}
		updated := tx.Model(&ChannelStatusProbeConfig{}).
			Where("id = ? AND revision = ?", current.Id, current.Revision).
			Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelStatusProbeConfigChanged
		}
		return tx.Where("id = ?", current.Id).First(&saved).Error
	})
	return saved, err
}

func GetChannelStatusProbeConfig(channelId int) (ChannelStatusProbeConfig, error) {
	var config ChannelStatusProbeConfig
	err := DB.Where("channel_id = ?", channelId).First(&config).Error
	return config, err
}

func GetChannelStatusProbeConfigs() ([]ChannelStatusProbeConfig, error) {
	var configs []ChannelStatusProbeConfig
	err := DB.Order("channel_id ASC").Find(&configs).Error
	return configs, err
}

func GetChannelStatusProbeStates() ([]ChannelStatusProbeState, error) {
	var states []ChannelStatusProbeState
	err := DB.Order("channel_id ASC, model_name ASC").Find(&states).Error
	return states, err
}

func RequestChannelStatusProbeManualRun(channelId int, now int64) (string, error) {
	requestId := common.GetUUID()
	err := DB.Transaction(func(tx *gorm.DB) error {
		var config ChannelStatusProbeConfig
		if err := lockForUpdate(tx).Where("channel_id = ?", channelId).First(&config).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrChannelStatusProbeNotConfigured
			}
			return err
		}
		models, err := config.Models()
		if err != nil {
			return err
		}
		if len(models) == 0 {
			return ErrChannelStatusProbeNotConfigured
		}
		if strings.TrimSpace(config.ManualRequestId) != "" {
			return ErrChannelStatusProbeManualPending
		}
		updated := tx.Model(&ChannelStatusProbeConfig{}).
			Where("id = ? AND manual_request_id = ?", config.Id, "").
			Updates(map[string]any{
				"manual_request_id": requestId, "manual_requested_at": now, "updated_at": now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrChannelStatusProbeManualPending
		}
		return nil
	})
	return requestId, err
}

func ClaimDueChannelStatusProbes(now int64, limit int) ([]ChannelStatusProbeClaim, error) {
	if limit <= 0 {
		return []ChannelStatusProbeClaim{}, nil
	}
	var candidates []ChannelStatusProbeConfig
	err := DB.Where("lease_until <= ?", now).
		Where("manual_request_id <> ? OR (enabled = ? AND next_run_at > 0 AND next_run_at <= ?)", "", true, now).
		Order("manual_requested_at DESC, next_run_at ASC, channel_id ASC").
		Limit(limit * 3).
		Find(&candidates).Error
	if err != nil {
		return nil, err
	}
	claims := make([]ChannelStatusProbeClaim, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		if len(claims) >= limit {
			break
		}
		models, decodeErr := candidate.Models()
		if decodeErr != nil {
			return nil, decodeErr
		}
		trigger := ChannelStatusProbeTriggerScheduled
		runId := common.GetUUID()
		manualRequestId := strings.TrimSpace(candidate.ManualRequestId)
		claimQuery := DB.Model(&ChannelStatusProbeConfig{}).
			Where("id = ? AND revision = ? AND lease_until <= ?", candidate.Id, candidate.Revision, now)
		if manualRequestId != "" {
			trigger = ChannelStatusProbeTriggerManual
			runId = manualRequestId
			claimQuery = claimQuery.Where("manual_request_id = ?", manualRequestId)
		} else {
			claimQuery = claimQuery.Where("enabled = ? AND next_run_at > 0 AND next_run_at <= ?", true, now)
		}
		leaseToken := common.GetUUID()
		claimed := claimQuery.Updates(map[string]any{
			"lease_token": leaseToken, "lease_until": now + ChannelStatusProbeLeaseSeconds,
			"running_trigger": trigger, "running_run_id": runId, "running_started_at": now,
			"updated_at": now,
		})
		if claimed.Error != nil {
			return nil, claimed.Error
		}
		if claimed.RowsAffected != 1 {
			continue
		}
		candidate.LeaseToken = leaseToken
		candidate.LeaseUntil = now + ChannelStatusProbeLeaseSeconds
		candidate.RunningTrigger = trigger
		candidate.RunningRunId = runId
		candidate.RunningStartedAt = now
		claims = append(claims, ChannelStatusProbeClaim{
			Config: candidate, Models: models, Trigger: trigger, RunId: runId,
			ManualRequestId: manualRequestId, LeaseToken: leaseToken,
		})
	}
	return claims, nil
}

func RenewChannelStatusProbeLease(claim ChannelStatusProbeClaim, now int64) (bool, error) {
	updated := DB.Model(&ChannelStatusProbeConfig{}).
		Where("id = ? AND revision = ? AND lease_token = ?", claim.Config.Id, claim.Config.Revision, claim.LeaseToken).
		Updates(map[string]any{"lease_until": now + ChannelStatusProbeLeaseSeconds, "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

func IsChannelStatusProbeLeaseCurrent(claim ChannelStatusProbeClaim, now int64) (bool, error) {
	var count int64
	err := DB.Model(&ChannelStatusProbeConfig{}).
		Where("id = ? AND revision = ? AND lease_token = ? AND lease_until > ?", claim.Config.Id, claim.Config.Revision, claim.LeaseToken, now).
		Count(&count).Error
	return count == 1, err
}

func CompleteChannelStatusProbeClaim(claim ChannelStatusProbeClaim, finishedAt int64) error {
	updates := map[string]any{
		"lease_token": "", "lease_until": int64(0), "running_trigger": "",
		"running_run_id": "", "running_started_at": int64(0), "updated_at": finishedAt,
	}
	if claim.Trigger == ChannelStatusProbeTriggerScheduled {
		updates["next_run_at"] = finishedAt + int64(claim.Config.IntervalSeconds)
	} else {
		updates["manual_request_id"] = ""
		updates["manual_requested_at"] = int64(0)
	}
	updated := DB.Model(&ChannelStatusProbeConfig{}).
		Where("id = ? AND revision = ? AND lease_token = ?", claim.Config.Id, claim.Config.Revision, claim.LeaseToken).
		Updates(updates)
	if updated.Error != nil || updated.RowsAffected == 1 {
		return updated.Error
	}
	cleanup := map[string]any{
		"lease_token": "", "lease_until": int64(0), "running_trigger": "",
		"running_run_id": "", "running_started_at": int64(0), "updated_at": finishedAt,
	}
	if claim.Trigger == ChannelStatusProbeTriggerManual {
		cleanup["manual_request_id"] = ""
		cleanup["manual_requested_at"] = int64(0)
	}
	return DB.Model(&ChannelStatusProbeConfig{}).
		Where("id = ? AND lease_token = ?", claim.Config.Id, claim.LeaseToken).
		Updates(cleanup).Error
}

func accumulateChannelStatusProbeBuckets(
	buckets []ChannelStatusProbeBucket,
	execution *ChannelStatusProbeExecution,
	unit string,
	limit int,
) []ChannelStatusProbeBucket {
	bucketSeconds := ChannelStatusProbeDisplayBucketSeconds(unit)
	bucketStart := ChannelStatusProbeDisplayBucketStart(execution.FinishedAt, unit)
	latestStart := bucketStart
	for _, bucket := range buckets {
		if bucket.StartedAt > latestStart {
			latestStart = bucket.StartedAt
		}
	}
	minimumStart := latestStart - int64(limit-1)*bucketSeconds
	retained := make([]ChannelStatusProbeBucket, 0, min(limit, len(buckets)+1))
	bucketIndex := -1
	for _, bucket := range buckets {
		if bucket.StartedAt < minimumStart || bucket.StartedAt > latestStart {
			continue
		}
		if bucket.StartedAt == bucketStart {
			bucketIndex = len(retained)
		}
		retained = append(retained, bucket)
	}
	if bucketIndex < 0 && bucketStart >= minimumStart && bucketStart <= latestStart {
		retained = append(retained, ChannelStatusProbeBucket{StartedAt: bucketStart})
		bucketIndex = len(retained) - 1
	}
	if bucketIndex >= 0 {
		retained[bucketIndex].Add(
			execution.Result,
			execution.ModelName,
			execution.FirstTokenMs,
			execution.TPS,
		)
	}
	sort.Slice(retained, func(i, j int) bool { return retained[i].StartedAt < retained[j].StartedAt })
	return retained
}

func SaveChannelStatusProbeExecution(execution *ChannelStatusProbeExecution) (bool, error) {
	if execution == nil || execution.ChannelId <= 0 || strings.TrimSpace(execution.RunId) == "" || strings.TrimSpace(execution.ModelName) == "" {
		return false, errors.New("渠道状态探测执行记录无效")
	}
	created := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		if execution.CreatedAt <= 0 {
			execution.CreatedAt = execution.FinishedAt
		}
		inserted := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "run_id"}, {Name: "model_name"}},
			DoNothing: true,
		}).Create(execution)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			var existing ChannelStatusProbeExecution
			if err := tx.Where("run_id = ? AND model_name = ?", execution.RunId, execution.ModelName).
				First(&existing).Error; err != nil {
				return err
			}
			*execution = existing
			return nil
		}
		created = true

		var state ChannelStatusProbeState
		stateErr := lockForUpdate(tx).
			Where("channel_id = ? AND model_name = ?", execution.ChannelId, execution.ModelName).
			First(&state).Error
		if errors.Is(stateErr, gorm.ErrRecordNotFound) {
			state = ChannelStatusProbeState{
				ChannelId: execution.ChannelId, ModelName: execution.ModelName,
				CreatedAt: execution.FinishedAt,
			}
		} else if stateErr != nil {
			return stateErr
		}

		bucketSeries := []struct {
			unit  string
			limit int
		}{
			{unit: ChannelStatusProbeDisplayUnitMinute, limit: ChannelStatusProbeMaxDisplayMinutes},
			{unit: ChannelStatusProbeDisplayUnitHour, limit: ChannelStatusProbeMaxDisplayHours},
			{unit: ChannelStatusProbeDisplayUnitDay, limit: ChannelStatusProbeMaxDisplayDays},
		}
		for _, series := range bucketSeries {
			buckets, err := state.Buckets(series.unit)
			if err != nil {
				return err
			}
			buckets = accumulateChannelStatusProbeBuckets(buckets, execution, series.unit, series.limit)
			encodedBuckets, err := common.Marshal(buckets)
			if err != nil {
				return err
			}
			switch series.unit {
			case ChannelStatusProbeDisplayUnitMinute:
				state.MinuteBucketsJSON = string(encodedBuckets)
			case ChannelStatusProbeDisplayUnitHour:
				state.HourBucketsJSON = string(encodedBuckets)
			case ChannelStatusProbeDisplayUnitDay:
				state.DayBucketsJSON = string(encodedBuckets)
			}
		}

		if execution.FinishedAt > state.FinishedAt || (execution.FinishedAt == state.FinishedAt && execution.Id > state.ExecutionId) {
			state.ExecutionId = execution.Id
			state.RunId = execution.RunId
			state.StartedAt = execution.StartedAt
			state.FinishedAt = execution.FinishedAt
			state.Result = execution.Result
			state.Success = execution.Result == ChannelStatusProbeResultSuccess
			state.RequestDispatched = execution.RequestDispatched
			state.ResponseTimeMs = execution.ResponseTimeMs
			state.FirstTokenMs = execution.FirstTokenMs
			state.TPS = execution.TPS
			state.ErrorCode = execution.ErrorCode
			state.ErrorMessage = execution.ErrorMessage
			state.SampleStatus = execution.SampleStatus
			state.SampleMessage = execution.SampleMessage
			state.Trigger = execution.Trigger
			state.Endpoint = execution.Endpoint
			state.Stream = execution.Stream
		}
		if execution.RequestDispatched && (execution.Result == ChannelStatusProbeResultSuccess ||
			execution.Result == ChannelStatusProbeResultUpstreamFailure || execution.Result == ChannelStatusProbeResultRateLimited) &&
			(execution.FinishedAt > state.LastHealthFinishedAt ||
				(execution.FinishedAt == state.LastHealthFinishedAt && execution.Id > state.LastHealthExecutionId)) {
			state.LastHealthResult = execution.Result
			state.LastHealthExecutionId = execution.Id
			state.LastHealthFinishedAt = execution.FinishedAt
			if execution.Result == ChannelStatusProbeResultSuccess {
				state.ConsecutiveSuccesses++
				state.ConsecutiveFailures = 0
			} else {
				state.ConsecutiveFailures++
				state.ConsecutiveSuccesses = 0
			}
		}
		state.UpdatedAt = max(state.UpdatedAt, execution.FinishedAt)
		if state.Id == 0 {
			return tx.Create(&state).Error
		}
		return tx.Save(&state).Error
	})
	return created, err
}

func ListPendingChannelStatusProbeExecutions(limit int) ([]ChannelStatusProbeExecution, error) {
	if limit <= 0 {
		return []ChannelStatusProbeExecution{}, nil
	}
	var executions []ChannelStatusProbeExecution
	err := DB.Where("sample_requested = ? AND sample_status = ?", true, ChannelStatusProbeSamplePending).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Find(&executions).Error
	return executions, err
}

func UpdateChannelStatusProbeExecutionSample(executionId int64, status string, message string, now int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&ChannelStatusProbeExecution{}).
			Where("id = ?", executionId).
			Updates(map[string]any{"sample_status": status, "sample_message": message}).Error; err != nil {
			return err
		}
		return tx.Model(&ChannelStatusProbeState{}).
			Where("execution_id = ?", executionId).
			Updates(map[string]any{"sample_status": status, "sample_message": message, "updated_at": now}).Error
	})
}

func ListChannelStatusProbeExecutions(
	channelId int,
	page int,
	pageSize int,
	modelName string,
	result string,
	trigger string,
) ([]ChannelStatusProbeExecution, int64, error) {
	query := DB.Model(&ChannelStatusProbeExecution{}).Where("channel_id = ?", channelId)
	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}
	if result != "" {
		query = query.Where("result = ?", result)
	}
	if trigger != "" {
		query = query.Where("trigger = ?", trigger)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var executions []ChannelStatusProbeExecution
	err := query.Order("finished_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&executions).Error
	return executions, total, err
}

func DeleteChannelStatusProbeExecutionsBefore(ctx context.Context, cutoff int64, batchSize int) (int64, error) {
	if cutoff <= 0 || batchSize <= 0 {
		return 0, errors.New("渠道状态探测历史清理参数无效")
	}
	if !DB.Migrator().HasTable(&ChannelStatusProbeExecution{}) {
		return 0, nil
	}
	var deletedRows int64
	db := DB.WithContext(ctx)
	for {
		var ids []int64
		if err := db.Model(&ChannelStatusProbeExecution{}).
			Where("finished_at < ?", cutoff).
			Order("finished_at ASC, id ASC").
			Limit(batchSize).
			Pluck("id", &ids).Error; err != nil {
			return deletedRows, err
		}
		if len(ids) == 0 {
			return deletedRows, nil
		}
		deleted := db.Where("id IN ?", ids).Delete(&ChannelStatusProbeExecution{})
		if deleted.Error != nil {
			return deletedRows, deleted.Error
		}
		deletedRows += deleted.RowsAffected
	}
}
