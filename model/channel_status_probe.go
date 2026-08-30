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
	ChannelStatusProbeErrorTimeout   = "probe_timeout"
	ChannelStatusProbeTimeoutMessage = "探测在下一监测周期开始前仍未返回，已判定为超时失败"

	ChannelMonitorStatusProbeLogKey = "channel_monitor_status_probe"

	ChannelStatusProbeDefaultIntervalSeconds = 60
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
	Id        int64 `json:"id"`
	ChannelId int   `json:"channel_id" gorm:"not null;uniqueIndex;index:idx_channel_status_probe_scheduled_due,priority:4;index:idx_channel_status_probe_manual_due,priority:4"`
	// LogicalChannelId is the shared probe identity. It is populated by the
	// status-probe worker when a channel belongs to a logical group; zero keeps
	// legacy rows compatible and means the physical channel is the identity.
	LogicalChannelId  int64  `json:"-" gorm:"bigint;index"`
	LogicalRevision   int64  `json:"-" gorm:"bigint"`
	Enabled           bool   `json:"enabled" gorm:"index:idx_channel_status_probe_scheduled_due,priority:1"`
	ModelsJSON        string `json:"-" gorm:"type:text;not null"`
	IntervalSeconds   int    `json:"interval_seconds"`
	DisplayValue      int    `json:"display_value"`
	DisplayUnit       string `json:"display_unit" gorm:"type:varchar(16)"`
	RecordSample      bool   `json:"record_sample"`
	NextRunAt         int64  `json:"next_run_at" gorm:"bigint;index;index:idx_channel_status_probe_scheduled_due,priority:2"`
	ManualRequestId   string `json:"manual_request_id" gorm:"type:varchar(64);index:idx_channel_status_probe_manual_due,priority:1"`
	ManualRequestedAt int64  `json:"manual_requested_at" gorm:"bigint;index;index:idx_channel_status_probe_manual_due,priority:2,sort:desc"`
	Revision          int64  `json:"revision" gorm:"bigint"`
	LeaseToken        string `json:"-" gorm:"type:varchar(64)"`
	LeaseUntil        int64  `json:"lease_until" gorm:"bigint;index;index:idx_channel_status_probe_scheduled_due,priority:3;index:idx_channel_status_probe_manual_due,priority:3"`
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

// ChannelStatusProbeExecutionBucketTimestamp returns the time cell to which
// an execution belongs. A timeout is recorded at the next scheduler boundary,
// so it must remain visible in the minute in which the probe started.
func ChannelStatusProbeExecutionBucketTimestamp(execution ChannelStatusProbeExecution) int64 {
	if execution.Result == ChannelStatusProbeResultUpstreamFailure &&
		execution.ErrorCode == ChannelStatusProbeErrorTimeout && execution.StartedAt > 0 {
		return execution.StartedAt
	}
	if execution.FinishedAt > 0 {
		return execution.FinishedAt
	}
	if execution.StartedAt > 0 {
		return execution.StartedAt
	}
	return execution.CreatedAt
}

type ChannelStatusProbeBucket struct {
	StartedAt               int64    `json:"started_at"`
	Success                 int      `json:"success"`
	UpstreamFailure         int      `json:"upstream_failure"`
	RateLimited             int      `json:"rate_limited"`
	LocalFailure            int      `json:"local_failure"`
	Skipped                 int      `json:"skipped"`
	Canceled                int      `json:"canceled"`
	Models                  []string `json:"models,omitempty"`
	FirstTokenTotalMs       float64  `json:"first_token_total_ms,omitempty"`
	FirstTokenSampleCount   int64    `json:"first_token_sample_count,omitempty"`
	TPSTotal                float64  `json:"tps_total,omitempty"`
	TPSSampleCount          int64    `json:"tps_sample_count,omitempty"`
	ResponseTimeTotalMs     float64  `json:"response_time_total_ms,omitempty"`
	ResponseTimeSampleCount int64    `json:"response_time_sample_count,omitempty"`
	LatestExecutionId       int64    `json:"latest_execution_id,omitempty"`
	LatestFinishedAt        int64    `json:"latest_finished_at,omitempty"`
	LatestResult            string   `json:"latest_result,omitempty"`
	LatestModelName         string   `json:"latest_model_name,omitempty"`
	LatestFirstTokenMs      *float64 `json:"latest_first_token_ms,omitempty"`
	LatestTPS               *float64 `json:"latest_tps,omitempty"`
	LatestResponseTimeMs    *float64 `json:"latest_response_time_ms,omitempty"`
}

func (bucket *ChannelStatusProbeBucket) Add(
	result string,
	modelName string,
	firstTokenMs *float64,
	tokensPerSecond *float64,
	responseTimeMs *float64,
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
	if responseTimeMs != nil {
		bucket.ResponseTimeTotalMs += *responseTimeMs
		bucket.ResponseTimeSampleCount++
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
	LogicalChannelId      int64    `json:"-" gorm:"bigint;index"`
	LogicalRevision       int64    `json:"-" gorm:"bigint"`
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
	SettledCostNanoCNY    *int64   `json:"settled_cost_nano_cny"`
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
	Id        int64  `json:"id" gorm:"index:idx_channel_status_probe_channel_result_finished,priority:4,sort:desc;index:idx_channel_status_probe_channel_trigger_finished,priority:4,sort:desc;index:idx_channel_status_probe_sample_retry,priority:4"`
	RunId     string `json:"run_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_channel_status_probe_run_model"`
	ChannelId int    `json:"channel_id" gorm:"not null;index:idx_channel_status_probe_channel_finished,priority:1;index:idx_channel_status_probe_channel_model_finished,priority:1;index:idx_channel_status_probe_channel_result_finished,priority:1;index:idx_channel_status_probe_channel_trigger_finished,priority:1"`
	// LogicalChannelId identifies the shared execution boundary while ChannelId
	// remains the legacy/API owner of the execution row. ActualChannelId records
	// the physical member whose key sent the request. Both fields are internal
	// so existing status-probe API response shapes remain unchanged.
	LogicalChannelId   int64    `json:"-" gorm:"bigint;index"`
	LogicalRevision    int64    `json:"-" gorm:"bigint"`
	ActualChannelId    int      `json:"-" gorm:"index"`
	ModelName          string   `json:"model_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_status_probe_run_model;index:idx_channel_status_probe_channel_model_finished,priority:2"`
	ConfigRevision     int64    `json:"config_revision" gorm:"bigint"`
	Trigger            string   `json:"trigger" gorm:"type:varchar(16);index;index:idx_channel_status_probe_channel_trigger_finished,priority:2"`
	Result             string   `json:"result" gorm:"type:varchar(32);index;index:idx_channel_status_probe_channel_result_finished,priority:2"`
	StartedAt          int64    `json:"started_at" gorm:"bigint"`
	FinishedAt         int64    `json:"finished_at" gorm:"bigint;index;index:idx_channel_status_probe_channel_finished,priority:2,sort:desc;index:idx_channel_status_probe_channel_model_finished,priority:3,sort:desc;index:idx_channel_status_probe_channel_result_finished,priority:3,sort:desc;index:idx_channel_status_probe_channel_trigger_finished,priority:3,sort:desc"`
	ResponseTimeMs     *float64 `json:"response_time_ms"`
	FirstTokenMs       *float64 `json:"first_token_ms"`
	TPS                *float64 `json:"tps"`
	SettledCostNanoCNY *int64   `json:"settled_cost_nano_cny"`
	Endpoint           string   `json:"endpoint" gorm:"type:varchar(255)"`
	Stream             bool     `json:"stream"`
	RequestId          string   `json:"request_id" gorm:"type:varchar(64)"`
	RequestDispatched  bool     `json:"request_dispatched"`
	UsageAvailable     bool     `json:"usage_available"`
	InputTokens        int      `json:"input_tokens"`
	OutputTokens       int      `json:"output_tokens"`
	TotalTokens        int      `json:"total_tokens"`
	CachedTokens       int      `json:"cached_tokens"`
	CacheWriteTokens   int      `json:"cache_write_tokens"`
	ReasoningTokens    int      `json:"reasoning_tokens"`
	ErrorCode          string   `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage       string   `json:"error_message" gorm:"type:varchar(512)"`
	SampleRequested    bool     `json:"sample_requested" gorm:"index:idx_channel_status_probe_sample_retry,priority:1"`
	SampleStatus       string   `json:"sample_status" gorm:"type:varchar(16);index:idx_channel_status_probe_sample_retry,priority:2"`
	SampleMessage      string   `json:"sample_message" gorm:"type:varchar(255)"`
	CreatedAt          int64    `json:"created_at" gorm:"bigint;index:idx_channel_status_probe_sample_retry,priority:3"`
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
	LogicalConfig   bool
	Identity        LogicalChannelIdentity
	Snapshot        LogicalChannelGroupSnapshot
	Models          []string
	Trigger         string
	RunId           string
	ManualRequestId string
	LeaseToken      string
	DeadlineAt      int64
}

type channelStatusProbeScope struct {
	Identity  LogicalChannelIdentity
	Snapshot  LogicalChannelGroupSnapshot
	OwnerID   int
	MemberIDs []int
}

type channelStatusProbeScopeKey struct {
	LogicalChannelID int64
	Revision         int64
}

type channelStatusProbeScopedConfig struct {
	Config        ChannelStatusProbeConfig
	Scope         channelStatusProbeScope
	LogicalConfig bool
}

func lockChannelStatusProbeLogicalScopeTx(tx *gorm.DB, scope channelStatusProbeScope, requiredMemberIDs ...int) (bool, error) {
	if scope.Identity.Revision <= 0 {
		return true, nil
	}
	group, err := LockLogicalChannelGroupForMembership(tx, scope.Identity.LogicalChannelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !IsLogicalChannelGroupActive(group.Status) || group.Revision != scope.Identity.Revision {
		return false, nil
	}
	members := make(map[int]struct{}, len(requiredMemberIDs))
	for _, channelID := range requiredMemberIDs {
		if channelID > 0 {
			members[channelID] = struct{}{}
		}
	}
	if len(members) == 0 {
		return true, nil
	}
	memberIDs := make([]int, 0, len(members))
	for channelID := range members {
		memberIDs = append(memberIDs, channelID)
	}
	var count int64
	if err := tx.Model(&ChannelLogicalGroupMember{}).
		Where("logical_group_id = ? AND channel_id IN ?", scope.Identity.LogicalChannelID, memberIDs).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count == int64(len(memberIDs)), nil
}

func resolveChannelStatusProbeScope(channelID int) (channelStatusProbeScope, error) {
	return resolveChannelStatusProbeScopeWithDB(DB, channelID)
}

func resolveChannelStatusProbeScopeWithDB(db *gorm.DB, channelID int) (channelStatusProbeScope, error) {
	identity, err := resolveChannelLogicalIdentity(db, channelID)
	if err != nil {
		// Legacy model tests and upgrade checks may exercise the probe tables
		// before the channels table exists. Preserve the physical behavior only
		// for that schema state; production relation failures remain visible.
		if db != nil && db.Migrator().HasTable(&Channel{}) {
			return channelStatusProbeScope{}, err
		}
		identity = LogicalChannelIdentity{ChannelID: channelID, LogicalChannelID: int64(channelID)}
	}
	snapshot, err := getLogicalChannelSelectionSnapshot(db, identity)
	if err != nil {
		return channelStatusProbeScope{}, err
	}
	members := append([]LogicalChannelMemberSnapshot(nil), snapshot.Members...)
	sort.Slice(members, func(i, j int) bool { return members[i].ChannelID < members[j].ChannelID })
	if len(members) == 0 {
		return channelStatusProbeScope{}, ErrLogicalChannelRuntimeGroupNotFound
	}
	snapshot.Members = members
	memberIDs := make([]int, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.ChannelID)
	}
	return channelStatusProbeScope{
		Identity: identity, Snapshot: snapshot, OwnerID: memberIDs[0], MemberIDs: memberIDs,
	}, nil
}

func resolvePersistedChannelStatusProbeScope(channelID int, logicalChannelID int64, logicalRevision int64) (channelStatusProbeScope, error) {
	return resolvePersistedChannelStatusProbeScopeWithDB(DB, channelID, logicalChannelID, logicalRevision)
}

func resolvePersistedChannelStatusProbeScopeWithDB(db *gorm.DB, channelID int, logicalChannelID int64, logicalRevision int64) (channelStatusProbeScope, error) {
	scope, err := resolveChannelStatusProbeScopeWithDB(db, channelID)
	if err != nil || !IsLogicalChannelGroupingEnabled() || logicalChannelID <= 0 || logicalRevision <= 0 ||
		(scope.Identity.Revision > 0 && scope.Identity.LogicalChannelID == logicalChannelID) {
		return scope, err
	}
	snapshot, snapshotErr := getLogicalChannelGroupSnapshot(db, logicalChannelID)
	if snapshotErr != nil || !IsLogicalChannelGroupActive(snapshot.Status) {
		return scope, nil
	}
	for _, member := range snapshot.Members {
		candidate, candidateErr := resolveChannelStatusProbeScopeWithDB(db, member.ChannelID)
		if candidateErr == nil && candidate.Identity.LogicalChannelID == logicalChannelID && candidate.Identity.Revision > 0 {
			return candidate, nil
		}
	}
	return scope, nil
}

func resolveChannelStatusProbeConfigScope(config ChannelStatusProbeConfig) (channelStatusProbeScope, error) {
	return resolveChannelStatusProbeConfigScopeWithDB(DB, config)
}

func resolveChannelStatusProbeConfigScopeWithDB(db *gorm.DB, config ChannelStatusProbeConfig) (channelStatusProbeScope, error) {
	return resolvePersistedChannelStatusProbeScopeWithDB(db, config.ChannelId, config.LogicalChannelId, config.LogicalRevision)
}

func channelStatusProbeCanonicalConfigs(configs []ChannelStatusProbeConfig) ([]channelStatusProbeScopedConfig, error) {
	return channelStatusProbeCanonicalConfigsWithDB(DB, configs, nil)
}

func channelStatusProbeCanonicalConfigsWithDB(db *gorm.DB, configs []ChannelStatusProbeConfig, overviewRelations *ChannelStatusProbeOverviewRelations) ([]channelStatusProbeScopedConfig, error) {
	selected := make(map[channelStatusProbeScopeKey]channelStatusProbeScopedConfig, len(configs))
	for _, config := range configs {
		var scope channelStatusProbeScope
		var err error
		if overviewRelations == nil {
			scope, err = resolveChannelStatusProbeConfigScopeWithDB(db, config)
		} else {
			scope, err = overviewRelations.resolvePersistedScope(config.ChannelId, config.LogicalChannelId, config.LogicalRevision)
		}
		if err != nil {
			return nil, err
		}
		key := channelStatusProbeScopeKey{LogicalChannelID: scope.Identity.LogicalChannelID, Revision: scope.Identity.Revision}
		current, exists := selected[key]
		if !exists || (config.ChannelId == scope.OwnerID && current.Config.ChannelId != scope.OwnerID) ||
			(config.ChannelId != scope.OwnerID && current.Config.ChannelId != scope.OwnerID && config.ChannelId < current.Config.ChannelId) {
			selected[key] = channelStatusProbeScopedConfig{Config: config, Scope: scope}
		}
	}
	var logicalConfigs []ChannelStatusProbeLogicalConfig
	if db.Migrator().HasTable(&ChannelStatusProbeLogicalConfig{}) {
		if err := db.Order("logical_channel_id ASC").Find(&logicalConfigs).Error; err != nil {
			return nil, err
		}
	}
	for _, logicalConfig := range logicalConfigs {
		var scope channelStatusProbeScope
		if overviewRelations == nil {
			snapshot, err := getLogicalChannelGroupSnapshot(db, logicalConfig.LogicalChannelId)
			if err != nil || !IsLogicalChannelGroupingEnabled() || !IsLogicalChannelGroupActive(snapshot.Status) || len(snapshot.Members) == 0 {
				continue
			}
			scope, err = resolveChannelStatusProbeScopeWithDB(db, snapshot.Members[0].ChannelID)
			if err != nil || scope.Identity.Revision <= 0 || scope.Identity.LogicalChannelID != logicalConfig.LogicalChannelId {
				continue
			}
		} else {
			var exists bool
			scope, exists = overviewRelations.resolveLogicalScope(logicalConfig.LogicalChannelId)
			if !exists {
				continue
			}
		}
		logicalConfig.LogicalRevision = scope.Identity.Revision
		logicalConfig.OwnerChannelId = scope.OwnerID
		key := channelStatusProbeScopeKey{LogicalChannelID: scope.Identity.LogicalChannelID, Revision: scope.Identity.Revision}
		selected[key] = channelStatusProbeScopedConfig{
			Config: logicalConfig.Project(scope.OwnerID), Scope: scope, LogicalConfig: true,
		}
	}
	result := make([]channelStatusProbeScopedConfig, 0, len(selected))
	for _, item := range selected {
		item.Config.ChannelId = item.Scope.OwnerID
		item.Config.LogicalChannelId = item.Scope.Identity.LogicalChannelID
		item.Config.LogicalRevision = item.Scope.Identity.Revision
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Config.ChannelId < result[j].Config.ChannelId })
	return result, nil
}

func channelStatusProbePhysicalConfigForScope(tx *gorm.DB, scope channelStatusProbeScope, lock bool) (ChannelStatusProbeConfig, error) {
	query := tx.Where("channel_id IN ?", scope.MemberIDs)
	query = query.Order("channel_id ASC")
	if lock {
		query = lockForUpdate(query)
	}
	var configs []ChannelStatusProbeConfig
	if err := query.Find(&configs).Error; err != nil {
		return ChannelStatusProbeConfig{}, err
	}
	if len(configs) == 0 {
		return ChannelStatusProbeConfig{}, gorm.ErrRecordNotFound
	}
	selected := configs[0]
	for _, config := range configs {
		if config.ChannelId == scope.OwnerID {
			selected = config
			break
		}
	}
	return selected, nil
}

func channelStatusProbeLogicalConfigForScope(tx *gorm.DB, scope channelStatusProbeScope, lock bool) (ChannelStatusProbeLogicalConfig, error) {
	query := tx.Where("logical_channel_id = ?", scope.Identity.LogicalChannelID)
	if lock {
		query = lockForUpdate(query)
	}
	var config ChannelStatusProbeLogicalConfig
	if err := query.First(&config).Error; err != nil {
		return ChannelStatusProbeLogicalConfig{}, err
	}
	return config, nil
}

func materializeChannelStatusProbeLogicalConfig(tx *gorm.DB, scope channelStatusProbeScope, now int64) (ChannelStatusProbeLogicalConfig, error) {
	currentScope, err := lockChannelStatusProbeLogicalScopeTx(tx, scope, scope.OwnerID)
	if err != nil {
		return ChannelStatusProbeLogicalConfig{}, err
	}
	if !currentScope {
		return ChannelStatusProbeLogicalConfig{}, ErrChannelLogicalGroupRevisionConflict
	}
	current, err := channelStatusProbeLogicalConfigForScope(tx, scope, true)
	if err == nil {
		if current.LogicalRevision != scope.Identity.Revision || current.OwnerChannelId != scope.OwnerID {
			if err := tx.Model(&ChannelStatusProbeLogicalConfig{}).Where("id = ?", current.Id).
				Updates(map[string]any{"logical_revision": scope.Identity.Revision, "owner_channel_id": scope.OwnerID}).Error; err != nil {
				return ChannelStatusProbeLogicalConfig{}, err
			}
			current.LogicalRevision = scope.Identity.Revision
			current.OwnerChannelId = scope.OwnerID
		}
		return current, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelStatusProbeLogicalConfig{}, err
	}
	physical, err := channelStatusProbePhysicalConfigForScope(tx, scope, true)
	if err != nil {
		return ChannelStatusProbeLogicalConfig{}, err
	}
	current = ChannelStatusProbeLogicalConfig{
		LogicalChannelId: scope.Identity.LogicalChannelID, LogicalRevision: scope.Identity.Revision, OwnerChannelId: scope.OwnerID,
		Enabled: physical.Enabled, ModelsJSON: physical.ModelsJSON, IntervalSeconds: physical.IntervalSeconds,
		DisplayValue: physical.DisplayValue, DisplayUnit: physical.DisplayUnit, RecordSample: physical.RecordSample,
		NextRunAt: physical.NextRunAt, Revision: max(physical.Revision, 1), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "logical_channel_id"}}, DoNothing: true}).
		Create(&current).Error; err != nil {
		return ChannelStatusProbeLogicalConfig{}, err
	}
	return channelStatusProbeLogicalConfigForScope(tx, scope, true)
}

func channelStatusProbeConfigForScope(tx *gorm.DB, scope channelStatusProbeScope, lock bool) (ChannelStatusProbeConfig, bool, error) {
	if scope.Identity.Revision <= 0 {
		config, err := channelStatusProbePhysicalConfigForScope(tx, scope, lock)
		return config, false, err
	}
	logicalConfig, err := channelStatusProbeLogicalConfigForScope(tx, scope, lock)
	if err == nil {
		logicalConfig.LogicalRevision = scope.Identity.Revision
		logicalConfig.OwnerChannelId = scope.OwnerID
		return logicalConfig.Project(scope.OwnerID), true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelStatusProbeConfig{}, false, err
	}
	config, err := channelStatusProbePhysicalConfigForScope(tx, scope, lock)
	if err != nil {
		return ChannelStatusProbeConfig{}, false, err
	}
	config.ChannelId = scope.OwnerID
	config.LogicalChannelId = scope.Identity.LogicalChannelID
	config.LogicalRevision = scope.Identity.Revision
	return config, false, nil
}

func nextChannelStatusProbeRunAt(after int64, intervalSeconds int) int64 {
	interval := int64(intervalSeconds)
	if after < 0 || interval <= 0 {
		return 0
	}
	return after - after%interval + interval
}

func SaveChannelStatusProbeConfig(channelId int, input ChannelStatusProbeConfigInput, now int64) (ChannelStatusProbeConfig, error) {
	scope, err := resolveChannelStatusProbeScope(channelId)
	if err != nil {
		return ChannelStatusProbeConfig{}, err
	}
	modelsJSON, err := common.Marshal(input.Models)
	if err != nil {
		return ChannelStatusProbeConfig{}, err
	}
	displayValue, displayUnit := NormalizeChannelStatusProbeDisplay(input.DisplayValue, input.DisplayUnit)
	var saved ChannelStatusProbeConfig
	err = DB.Transaction(func(tx *gorm.DB) error {
		if scope.Identity.Revision > 0 {
			currentScope, lockErr := lockChannelStatusProbeLogicalScopeTx(tx, scope, scope.OwnerID)
			if lockErr != nil {
				return lockErr
			}
			if !currentScope {
				return ErrChannelLogicalGroupRevisionConflict
			}
			current, findErr := channelStatusProbeLogicalConfigForScope(tx, scope, true)
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				physical, physicalErr := channelStatusProbePhysicalConfigForScope(tx, scope, true)
				currentRevision := int64(0)
				if physicalErr == nil {
					currentRevision = physical.Revision
				} else if !errors.Is(physicalErr, gorm.ErrRecordNotFound) {
					return physicalErr
				}
				if input.Revision != currentRevision {
					return ErrChannelStatusProbeConfigChanged
				}
				nextRunAt := int64(0)
				if input.Enabled {
					nextRunAt = nextChannelStatusProbeRunAt(now, input.IntervalSeconds)
				}
				current = ChannelStatusProbeLogicalConfig{
					LogicalChannelId: scope.Identity.LogicalChannelID, LogicalRevision: scope.Identity.Revision, OwnerChannelId: scope.OwnerID,
					Enabled: input.Enabled, ModelsJSON: string(modelsJSON), IntervalSeconds: input.IntervalSeconds,
					DisplayValue: displayValue, DisplayUnit: displayUnit, RecordSample: input.RecordSample,
					NextRunAt: nextRunAt, Revision: currentRevision + 1, CreatedAt: now, UpdatedAt: now,
				}
				if err := tx.Create(&current).Error; err != nil {
					return err
				}
			} else if findErr != nil {
				return findErr
			} else {
				if current.Revision != input.Revision {
					return ErrChannelStatusProbeConfigChanged
				}
				nextRunAt := current.NextRunAt
				configurationChanged := current.ModelsJSON != string(modelsJSON) || current.IntervalSeconds != input.IntervalSeconds
				if !input.Enabled {
					nextRunAt = 0
				} else if !current.Enabled || configurationChanged || nextRunAt <= 0 || input.IntervalSeconds <= 0 || nextRunAt%int64(input.IntervalSeconds) != 0 {
					nextRunAt = nextChannelStatusProbeRunAt(now, input.IntervalSeconds)
				}
				updates := map[string]any{
					"logical_revision": scope.Identity.Revision, "owner_channel_id": scope.OwnerID,
					"enabled": input.Enabled, "models_json": string(modelsJSON), "interval_seconds": input.IntervalSeconds,
					"display_value": displayValue, "display_unit": displayUnit, "record_sample": input.RecordSample,
					"next_run_at": nextRunAt, "revision": current.Revision + 1, "updated_at": now,
				}
				updated := tx.Model(&ChannelStatusProbeLogicalConfig{}).Where("id = ? AND revision = ?", current.Id, current.Revision).Updates(updates)
				if updated.Error != nil {
					return updated.Error
				}
				if updated.RowsAffected != 1 {
					return ErrChannelStatusProbeConfigChanged
				}
				if err := tx.Where("id = ?", current.Id).First(&current).Error; err != nil {
					return err
				}
			}
			stateQuery := tx.Where("logical_channel_id = ?", scope.Identity.LogicalChannelID)
			if len(input.Models) > 0 {
				stateQuery = stateQuery.Where("model_name NOT IN ?", input.Models)
			}
			if err := stateQuery.Delete(&ChannelStatusProbeLogicalState{}).Error; err != nil {
				return err
			}
			saved = current.Project(channelId)
			return nil
		}

		current, findErr := channelStatusProbePhysicalConfigForScope(tx, scope, true)
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			if input.Revision != 0 {
				return ErrChannelStatusProbeConfigChanged
			}
			nextRunAt := int64(0)
			if input.Enabled {
				nextRunAt = nextChannelStatusProbeRunAt(now, input.IntervalSeconds)
			}
			saved = ChannelStatusProbeConfig{
				ChannelId: channelId,
				Enabled:   input.Enabled, ModelsJSON: string(modelsJSON),
				IntervalSeconds: input.IntervalSeconds,
				DisplayValue:    displayValue,
				DisplayUnit:     displayUnit,
				RecordSample:    input.RecordSample,
				NextRunAt:       nextRunAt, Revision: 1, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&saved).Error; err != nil {
				return err
			}
		} else if findErr != nil {
			return findErr
		} else {
			if current.Revision != input.Revision {
				return ErrChannelStatusProbeConfigChanged
			}
			nextRunAt := current.NextRunAt
			configurationChanged := current.ModelsJSON != string(modelsJSON) || current.IntervalSeconds != input.IntervalSeconds
			if !input.Enabled {
				nextRunAt = 0
			} else if !current.Enabled || configurationChanged || nextRunAt <= 0 || input.IntervalSeconds <= 0 || nextRunAt%int64(input.IntervalSeconds) != 0 {
				nextRunAt = nextChannelStatusProbeRunAt(now, input.IntervalSeconds)
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
			if err := tx.Where("id = ?", current.Id).First(&saved).Error; err != nil {
				return err
			}
		}
		stateQuery := tx.Where("channel_id = ?", channelId)
		if len(input.Models) > 0 {
			stateQuery = stateQuery.Where("model_name NOT IN ?", input.Models)
		}
		return stateQuery.Delete(&ChannelStatusProbeState{}).Error
	})
	if err == nil && scope.Identity.Revision <= 0 {
		saved.ChannelId = channelId
	}
	return saved, err
}

func GetChannelStatusProbeConfig(channelId int) (ChannelStatusProbeConfig, error) {
	scope, err := resolveChannelStatusProbeScope(channelId)
	if err != nil {
		return ChannelStatusProbeConfig{}, err
	}
	config, _, err := channelStatusProbeConfigForScope(DB, scope, false)
	config.ChannelId = channelId
	config.LogicalChannelId = scope.Identity.LogicalChannelID
	config.LogicalRevision = scope.Identity.Revision
	return config, err
}

func GetChannelStatusProbeConfigs() ([]ChannelStatusProbeConfig, error) {
	return getChannelStatusProbeConfigs(DB, nil)
}

func GetChannelStatusProbeConfigsForOverview(ctx context.Context, db *gorm.DB, relations *ChannelStatusProbeOverviewRelations) ([]ChannelStatusProbeConfig, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	return getChannelStatusProbeConfigs(queryDB, relations)
}

func getChannelStatusProbeConfigs(db *gorm.DB, overviewRelations *ChannelStatusProbeOverviewRelations) ([]ChannelStatusProbeConfig, error) {
	var configs []ChannelStatusProbeConfig
	if err := db.Order("channel_id ASC").Find(&configs).Error; err != nil {
		return nil, err
	}
	canonical, err := channelStatusProbeCanonicalConfigsWithDB(db, configs, overviewRelations)
	if err != nil {
		return nil, err
	}
	projected := make([]ChannelStatusProbeConfig, 0, len(configs))
	for _, item := range canonical {
		for _, channelID := range item.Scope.MemberIDs {
			config := item.Config
			config.ChannelId = channelID
			projected = append(projected, config)
		}
	}
	sort.Slice(projected, func(i, j int) bool { return projected[i].ChannelId < projected[j].ChannelId })
	return projected, nil
}

func GetChannelStatusProbeStates() ([]ChannelStatusProbeState, error) {
	return getChannelStatusProbeStates(DB, nil, nil, "", nil)
}

func GetChannelStatusProbeStatesForOverview(
	ctx context.Context,
	db *gorm.DB,
	channelIDs []int,
	logicalChannelIDs []int64,
	selectedModel string,
	relations *ChannelStatusProbeOverviewRelations,
) ([]ChannelStatusProbeState, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	return getChannelStatusProbeStates(queryDB, channelIDs, logicalChannelIDs, selectedModel, relations)
}

func getChannelStatusProbeStates(
	db *gorm.DB,
	channelIDs []int,
	logicalChannelIDs []int64,
	selectedModel string,
	overviewRelations *ChannelStatusProbeOverviewRelations,
) ([]ChannelStatusProbeState, error) {
	if channelIDs != nil && len(channelIDs) == 0 {
		return []ChannelStatusProbeState{}, nil
	}
	var states []ChannelStatusProbeState
	stateQuery := db.Order("channel_id ASC, model_name ASC")
	if channelIDs != nil {
		stateQuery = stateQuery.Where("channel_id IN ?", channelIDs)
	}
	if selectedModel != "" {
		stateQuery = stateQuery.Where("model_name = ?", selectedModel)
	}
	if err := stateQuery.Find(&states).Error; err != nil {
		return nil, err
	}
	type scopedState struct {
		state ChannelStatusProbeState
		scope channelStatusProbeScope
	}
	type stateKey struct {
		scope channelStatusProbeScopeKey
		model string
	}
	selected := make(map[stateKey]scopedState, len(states))
	for _, state := range states {
		var scope channelStatusProbeScope
		var err error
		if overviewRelations == nil {
			scope, err = resolvePersistedChannelStatusProbeScopeWithDB(db, state.ChannelId, state.LogicalChannelId, state.LogicalRevision)
		} else {
			scope, err = overviewRelations.resolvePersistedScope(state.ChannelId, state.LogicalChannelId, state.LogicalRevision)
		}
		if err != nil {
			return nil, err
		}
		key := stateKey{
			scope: channelStatusProbeScopeKey{LogicalChannelID: scope.Identity.LogicalChannelID, Revision: scope.Identity.Revision},
			model: state.ModelName,
		}
		current, exists := selected[key]
		if !exists || state.ChannelId == scope.OwnerID ||
			(current.state.ChannelId != scope.OwnerID && state.FinishedAt > current.state.FinishedAt) {
			selected[key] = scopedState{state: state, scope: scope}
		}
	}
	if db.Migrator().HasTable(&ChannelStatusProbeLogicalState{}) {
		var logicalStates []ChannelStatusProbeLogicalState
		logicalStateQuery := db.Order("logical_channel_id ASC, model_name ASC")
		if logicalChannelIDs != nil {
			if len(logicalChannelIDs) == 0 {
				logicalStateQuery = nil
			} else {
				logicalStateQuery = logicalStateQuery.Where("logical_channel_id IN ?", logicalChannelIDs)
			}
		}
		if logicalStateQuery != nil && selectedModel != "" {
			logicalStateQuery = logicalStateQuery.Where("model_name = ?", selectedModel)
		}
		if logicalStateQuery != nil {
			if err := logicalStateQuery.Find(&logicalStates).Error; err != nil {
				return nil, err
			}
		}
		for _, row := range logicalStates {
			var scope channelStatusProbeScope
			if overviewRelations == nil {
				snapshot, err := getLogicalChannelGroupSnapshot(db, row.LogicalChannelId)
				if err != nil || !IsLogicalChannelGroupingEnabled() || !IsLogicalChannelGroupActive(snapshot.Status) || len(snapshot.Members) == 0 {
					continue
				}
				scope, err = resolveChannelStatusProbeScopeWithDB(db, snapshot.Members[0].ChannelID)
				if err != nil || scope.Identity.Revision <= 0 || scope.Identity.LogicalChannelID != row.LogicalChannelId ||
					row.LogicalRevision != scope.Identity.Revision {
					continue
				}
			} else {
				var exists bool
				scope, exists = overviewRelations.resolveLogicalScope(row.LogicalChannelId)
				if !exists || row.LogicalRevision != scope.Identity.Revision {
					continue
				}
			}
			state, err := row.State(scope.OwnerID)
			if err != nil {
				return nil, err
			}
			key := stateKey{
				scope: channelStatusProbeScopeKey{LogicalChannelID: scope.Identity.LogicalChannelID, Revision: scope.Identity.Revision},
				model: row.ModelName,
			}
			selected[key] = scopedState{state: state, scope: scope}
		}
	}
	allowedChannels := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		allowedChannels[channelID] = struct{}{}
	}
	projected := make([]ChannelStatusProbeState, 0, len(states))
	for _, item := range selected {
		for _, channelID := range item.scope.MemberIDs {
			if channelIDs != nil {
				if _, allowed := allowedChannels[channelID]; !allowed {
					continue
				}
			}
			state := item.state
			state.ChannelId = channelID
			state.LogicalChannelId = item.scope.Identity.LogicalChannelID
			state.LogicalRevision = item.scope.Identity.Revision
			projected = append(projected, state)
		}
	}
	sort.Slice(projected, func(i, j int) bool {
		if projected[i].ChannelId != projected[j].ChannelId {
			return projected[i].ChannelId < projected[j].ChannelId
		}
		return projected[i].ModelName < projected[j].ModelName
	})
	return projected, nil
}

func RequestChannelStatusProbeManualRun(channelId int, now int64) (string, error) {
	scope, err := resolveChannelStatusProbeScope(channelId)
	if err != nil {
		return "", err
	}
	requestId := common.GetUUID()
	err = DB.Transaction(func(tx *gorm.DB) error {
		if scope.Identity.Revision > 0 {
			config, err := materializeChannelStatusProbeLogicalConfig(tx, scope, now)
			if err != nil {
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
			updated := tx.Model(&ChannelStatusProbeLogicalConfig{}).
				Where("id = ? AND manual_request_id = ?", config.Id, "").
				Updates(map[string]any{"manual_request_id": requestId, "manual_requested_at": now, "updated_at": now})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return ErrChannelStatusProbeManualPending
			}
			return nil
		}

		config, err := channelStatusProbePhysicalConfigForScope(tx, scope, true)
		if err != nil {
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
	var persisted []ChannelStatusProbeConfig
	if err := DB.Find(&persisted).Error; err != nil {
		return nil, err
	}
	candidates, err := channelStatusProbeCanonicalConfigs(persisted)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		iManual := strings.TrimSpace(candidates[i].Config.ManualRequestId) != ""
		jManual := strings.TrimSpace(candidates[j].Config.ManualRequestId) != ""
		if iManual != jManual {
			return iManual
		}
		if iManual && candidates[i].Config.ManualRequestedAt != candidates[j].Config.ManualRequestedAt {
			return candidates[i].Config.ManualRequestedAt > candidates[j].Config.ManualRequestedAt
		}
		if candidates[i].Config.NextRunAt != candidates[j].Config.NextRunAt {
			return candidates[i].Config.NextRunAt < candidates[j].Config.NextRunAt
		}
		return candidates[i].Config.ChannelId < candidates[j].Config.ChannelId
	})
	claimCapacity := len(candidates)
	if limit > 0 && claimCapacity > limit {
		claimCapacity = limit
	}
	claims := make([]ChannelStatusProbeClaim, 0, claimCapacity)
	for _, item := range candidates {
		if limit > 0 && len(claims) >= limit {
			break
		}
		if item.Scope.Identity.Revision > 0 && !item.LogicalConfig {
			var logicalConfig ChannelStatusProbeLogicalConfig
			err := DB.Transaction(func(tx *gorm.DB) error {
				var materializeErr error
				logicalConfig, materializeErr = materializeChannelStatusProbeLogicalConfig(tx, item.Scope, now)
				return materializeErr
			})
			if err != nil {
				return nil, err
			}
			item.Config = logicalConfig.Project(item.Scope.OwnerID)
			item.LogicalConfig = true
		}
		candidate := item.Config
		manualRequestId := strings.TrimSpace(candidate.ManualRequestId)
		if candidate.LeaseUntil > now || (manualRequestId == "" && (!candidate.Enabled || candidate.NextRunAt <= 0 || candidate.NextRunAt > now)) {
			continue
		}
		models, decodeErr := candidate.Models()
		if decodeErr != nil {
			return nil, decodeErr
		}
		trigger := ChannelStatusProbeTriggerScheduled
		runId := common.GetUUID()
		claimQuery := DB.Model(&ChannelStatusProbeConfig{}).
			Where("id = ? AND revision = ? AND lease_until <= ?", candidate.Id, candidate.Revision, now)
		if item.LogicalConfig {
			claimQuery = DB.Model(&ChannelStatusProbeLogicalConfig{}).
				Where("id = ? AND revision = ? AND lease_until <= ?", candidate.Id, candidate.Revision, now)
		}
		if manualRequestId != "" {
			trigger = ChannelStatusProbeTriggerManual
			runId = manualRequestId
			claimQuery = claimQuery.Where("manual_request_id = ?", manualRequestId)
		} else {
			claimQuery = claimQuery.Where("enabled = ? AND next_run_at > 0 AND next_run_at <= ?", true, now)
		}
		leaseToken := common.GetUUID()
		claimUpdates := map[string]any{
			"lease_token": leaseToken, "lease_until": now + ChannelStatusProbeLeaseSeconds,
			"running_trigger": trigger, "running_run_id": runId, "running_started_at": now,
			"updated_at": now,
		}
		deadlineAt := nextChannelStatusProbeRunAt(now, candidate.IntervalSeconds)
		if trigger == ChannelStatusProbeTriggerScheduled {
			deadlineAt = nextChannelStatusProbeRunAt(candidate.NextRunAt, candidate.IntervalSeconds)
			claimUpdates["next_run_at"] = deadlineAt
		} else if candidate.NextRunAt > now {
			deadlineAt = candidate.NextRunAt
		}
		claimed := claimQuery.Updates(claimUpdates)
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
			Config: candidate, LogicalConfig: item.LogicalConfig, Identity: item.Scope.Identity, Snapshot: item.Scope.Snapshot,
			Models: models, Trigger: trigger, RunId: runId,
			ManualRequestId: manualRequestId, LeaseToken: leaseToken, DeadlineAt: deadlineAt,
		})
	}
	return claims, nil
}

// TimeoutOverdueChannelStatusProbes fences runs that are still active when
// their next monitoring period begins. Completed model results remain intact;
// only missing results under the previous run ID are recorded as timeouts.
func TimeoutOverdueChannelStatusProbes(now int64, limit int) (int, error) {
	var persisted []ChannelStatusProbeConfig
	if err := DB.Find(&persisted).Error; err != nil {
		return 0, err
	}
	candidates, err := channelStatusProbeCanonicalConfigs(persisted)
	if err != nil {
		return 0, err
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Config.NextRunAt != candidates[j].Config.NextRunAt {
			return candidates[i].Config.NextRunAt < candidates[j].Config.NextRunAt
		}
		return candidates[i].Config.ChannelId < candidates[j].Config.ChannelId
	})
	timedOut := 0
	for _, item := range candidates {
		if limit > 0 && timedOut >= limit {
			break
		}
		if item.Scope.Identity.Revision > 0 && !item.LogicalConfig {
			var logicalConfig ChannelStatusProbeLogicalConfig
			err := DB.Transaction(func(tx *gorm.DB) error {
				var materializeErr error
				logicalConfig, materializeErr = materializeChannelStatusProbeLogicalConfig(tx, item.Scope, now)
				return materializeErr
			})
			if err != nil {
				return timedOut, err
			}
			item.Config = logicalConfig.Project(item.Scope.OwnerID)
			item.LogicalConfig = true
		}
		candidate := item.Config
		if strings.TrimSpace(candidate.RunningRunId) == "" || !candidate.Enabled || candidate.NextRunAt <= 0 || candidate.NextRunAt > now {
			continue
		}
		err = DB.Transaction(func(tx *gorm.DB) error {
			var current ChannelStatusProbeConfig
			currentQuery := lockForUpdate(tx).
				Where("id = ? AND revision = ? AND running_run_id = ? AND next_run_at = ?",
					candidate.Id, candidate.Revision, candidate.RunningRunId, candidate.NextRunAt)
			if item.LogicalConfig {
				var logicalCurrent ChannelStatusProbeLogicalConfig
				if err := currentQuery.First(&logicalCurrent).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					return err
				}
				current = logicalCurrent.Project(item.Scope.OwnerID)
			} else if err := currentQuery.First(&current).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if current.NextRunAt <= 0 || current.NextRunAt > now || strings.TrimSpace(current.RunningRunId) == "" {
				return nil
			}
			current.ChannelId = item.Scope.OwnerID
			current.LogicalChannelId = item.Scope.Identity.LogicalChannelID
			current.LogicalRevision = item.Scope.Identity.Revision
			models, err := current.Models()
			if err != nil {
				return err
			}
			updates := map[string]any{
				"lease_token": "", "lease_until": int64(0), "running_trigger": "",
				"running_run_id": "", "running_started_at": int64(0), "updated_at": now,
			}
			if current.RunningTrigger == ChannelStatusProbeTriggerManual {
				updates["manual_request_id"] = ""
				updates["manual_requested_at"] = int64(0)
			}
			updateModel := tx.Model(&ChannelStatusProbeConfig{})
			if item.LogicalConfig {
				updateModel = tx.Model(&ChannelStatusProbeLogicalConfig{})
			}
			updated := updateModel.
				Where("id = ? AND revision = ? AND lease_token = ? AND running_run_id = ? AND next_run_at = ?",
					current.Id, current.Revision, current.LeaseToken, current.RunningRunId, current.NextRunAt).
				Updates(updates)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return nil
			}
			startedAt := current.RunningStartedAt
			if startedAt <= 0 {
				startedAt = current.NextRunAt
			}
			for _, modelName := range models {
				execution := &ChannelStatusProbeExecution{
					RunId: current.RunningRunId, ChannelId: current.ChannelId, LogicalChannelId: current.LogicalChannelId,
					LogicalRevision: current.LogicalRevision,
					ModelName:       modelName, ConfigRevision: current.Revision, Trigger: current.RunningTrigger,
					Result: ChannelStatusProbeResultUpstreamFailure, StartedAt: startedAt, FinishedAt: current.NextRunAt,
					ErrorCode: ChannelStatusProbeErrorTimeout, ErrorMessage: ChannelStatusProbeTimeoutMessage,
					SampleStatus: ChannelStatusProbeSampleSkipped, SampleMessage: "探测超时，未计入智能调度样本",
					CreatedAt: now,
				}
				if _, err := saveChannelStatusProbeExecutionTx(tx, execution); err != nil {
					return err
				}
			}
			timedOut++
			return nil
		})
		if err != nil {
			return timedOut, err
		}
	}
	return timedOut, nil
}

func RenewChannelStatusProbeLease(claim ChannelStatusProbeClaim, now int64) (bool, error) {
	updateModel := DB.Model(&ChannelStatusProbeConfig{})
	if claim.LogicalConfig {
		updateModel = DB.Model(&ChannelStatusProbeLogicalConfig{})
	}
	updated := updateModel.
		Where("id = ? AND revision = ? AND lease_token = ?", claim.Config.Id, claim.Config.Revision, claim.LeaseToken).
		Updates(map[string]any{"lease_until": now + ChannelStatusProbeLeaseSeconds, "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}

func IsChannelStatusProbeLeaseCurrent(claim ChannelStatusProbeClaim, now int64) (bool, error) {
	var count int64
	query := DB.Model(&ChannelStatusProbeConfig{})
	if claim.LogicalConfig {
		query = DB.Model(&ChannelStatusProbeLogicalConfig{})
	}
	err := query.
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
		nextRunAt := int64(0)
		if claim.LogicalConfig {
			var current ChannelStatusProbeLogicalConfig
			if err := DB.Select("next_run_at").Where("id = ?", claim.Config.Id).First(&current).Error; err == nil {
				nextRunAt = current.NextRunAt
			}
		} else {
			var current ChannelStatusProbeConfig
			if err := DB.Select("next_run_at").Where("id = ?", claim.Config.Id).First(&current).Error; err == nil {
				nextRunAt = current.NextRunAt
			}
		}
		if nextRunAt <= 0 {
			nextRunAt = nextChannelStatusProbeRunAt(finishedAt, claim.Config.IntervalSeconds)
		}
		updates["next_run_at"] = nextRunAt
	} else {
		updates["manual_request_id"] = ""
		updates["manual_requested_at"] = int64(0)
	}
	updateModel := DB.Model(&ChannelStatusProbeConfig{})
	if claim.LogicalConfig {
		updateModel = DB.Model(&ChannelStatusProbeLogicalConfig{})
	}
	updated := updateModel.
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
	cleanupModel := DB.Model(&ChannelStatusProbeConfig{})
	if claim.LogicalConfig {
		cleanupModel = DB.Model(&ChannelStatusProbeLogicalConfig{})
	}
	return cleanupModel.
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
	bucketStart := ChannelStatusProbeDisplayBucketStart(ChannelStatusProbeExecutionBucketTimestamp(*execution), unit)
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
			execution.ResponseTimeMs,
		)
		current := &retained[bucketIndex]
		if current.LatestResult == "" || execution.FinishedAt > current.LatestFinishedAt ||
			(execution.FinishedAt == current.LatestFinishedAt && execution.Id > current.LatestExecutionId) {
			current.LatestExecutionId = execution.Id
			current.LatestFinishedAt = execution.FinishedAt
			current.LatestResult = execution.Result
			current.LatestModelName = execution.ModelName
			current.LatestFirstTokenMs = execution.FirstTokenMs
			current.LatestTPS = execution.TPS
			current.LatestResponseTimeMs = execution.ResponseTimeMs
		}
	}
	sort.Slice(retained, func(i, j int) bool { return retained[i].StartedAt < retained[j].StartedAt })
	return retained
}

func saveChannelStatusProbeExecutionTx(tx *gorm.DB, execution *ChannelStatusProbeExecution) (bool, error) {
	if execution == nil || execution.ChannelId <= 0 || strings.TrimSpace(execution.RunId) == "" || strings.TrimSpace(execution.ModelName) == "" {
		return false, errors.New("渠道状态探测执行记录无效")
	}
	created := false
	if execution.CreatedAt <= 0 {
		execution.CreatedAt = execution.FinishedAt
	}
	inserted := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "run_id"}, {Name: "model_name"}},
		DoNothing: true,
	}).Create(execution)
	if inserted.Error != nil {
		return false, inserted.Error
	}
	if inserted.RowsAffected == 0 {
		var existing ChannelStatusProbeExecution
		if err := tx.Where("run_id = ? AND model_name = ?", execution.RunId, execution.ModelName).
			First(&existing).Error; err != nil {
			return false, err
		}
		*execution = existing
		return false, nil
	}
	created = true

	logicalExecution := execution.LogicalChannelId > 0 && execution.LogicalRevision > 0
	configuredModelsJSON := ""
	var configErr error
	if logicalExecution {
		executionScope := channelStatusProbeScope{
			Identity: LogicalChannelIdentity{
				ChannelID: execution.ChannelId, LogicalChannelID: execution.LogicalChannelId, Revision: execution.LogicalRevision,
			},
		}
		currentScope, revisionErr := lockChannelStatusProbeLogicalScopeTx(
			tx, executionScope, execution.ChannelId, execution.ActualChannelId,
		)
		if revisionErr != nil {
			return false, revisionErr
		}
		if !currentScope {
			return created, nil
		}
		var currentConfig ChannelStatusProbeLogicalConfig
		configErr = lockForUpdate(tx).
			Select("id", "models_json").
			Where("logical_channel_id = ?", execution.LogicalChannelId).
			First(&currentConfig).Error
		configuredModelsJSON = currentConfig.ModelsJSON
	} else {
		var currentConfig ChannelStatusProbeConfig
		configErr = lockForUpdate(tx).
			Select("id", "models_json").
			Where("channel_id = ?", execution.ChannelId).
			First(&currentConfig).Error
		configuredModelsJSON = currentConfig.ModelsJSON
	}
	if configErr == nil {
		configuredModels := []string{}
		if strings.TrimSpace(configuredModelsJSON) != "" {
			if err := common.UnmarshalJsonStr(configuredModelsJSON, &configuredModels); err != nil {
				return false, fmt.Errorf("解析渠道状态探测模型失败: %w", err)
			}
		}
		modelConfigured := false
		for _, modelName := range configuredModels {
			if modelName == execution.ModelName {
				modelConfigured = true
				break
			}
		}
		if !modelConfigured {
			return created, nil
		}
	} else if !errors.Is(configErr, gorm.ErrRecordNotFound) {
		return false, configErr
	}

	var state ChannelStatusProbeState
	var logicalStateRow ChannelStatusProbeLogicalState
	var stateErr error
	if logicalExecution {
		stateErr = lockForUpdate(tx).
			Where("logical_channel_id = ? AND model_name = ?", execution.LogicalChannelId, execution.ModelName).
			First(&logicalStateRow).Error
		if stateErr == nil {
			if logicalStateRow.LogicalRevision == execution.LogicalRevision {
				state, stateErr = logicalStateRow.State(execution.ChannelId)
			} else {
				stateErr = gorm.ErrRecordNotFound
			}
		}
	} else {
		stateErr = lockForUpdate(tx).
			Where("channel_id = ? AND model_name = ?", execution.ChannelId, execution.ModelName).
			First(&state).Error
	}
	if errors.Is(stateErr, gorm.ErrRecordNotFound) {
		state = ChannelStatusProbeState{ChannelId: execution.ChannelId, ModelName: execution.ModelName, CreatedAt: execution.FinishedAt}
	} else if stateErr != nil {
		return false, stateErr
	}
	if logicalExecution {
		state.LogicalChannelId = execution.LogicalChannelId
		state.LogicalRevision = execution.LogicalRevision
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
			return false, err
		}
		buckets = accumulateChannelStatusProbeBuckets(buckets, execution, series.unit, series.limit)
		encodedBuckets, err := common.Marshal(buckets)
		if err != nil {
			return false, err
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
		state.SettledCostNanoCNY = execution.SettledCostNanoCNY
		state.ErrorCode = execution.ErrorCode
		state.ErrorMessage = execution.ErrorMessage
		state.SampleStatus = execution.SampleStatus
		state.SampleMessage = execution.SampleMessage
		state.Trigger = execution.Trigger
		state.Endpoint = execution.Endpoint
		state.Stream = execution.Stream
	}
	healthOutcome := execution.RequestDispatched && (execution.Result == ChannelStatusProbeResultSuccess ||
		execution.Result == ChannelStatusProbeResultUpstreamFailure || execution.Result == ChannelStatusProbeResultRateLimited)
	if execution.Result == ChannelStatusProbeResultUpstreamFailure && execution.ErrorCode == ChannelStatusProbeErrorTimeout {
		healthOutcome = true
	}
	if healthOutcome &&
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
	if logicalExecution {
		row, err := newChannelStatusProbeLogicalStateRow(state)
		if err != nil {
			return false, err
		}
		row.Id = logicalStateRow.Id
		if row.Id == 0 {
			return created, tx.Create(&row).Error
		}
		return created, tx.Save(&row).Error
	}
	if state.Id == 0 {
		return created, tx.Create(&state).Error
	}
	return created, tx.Save(&state).Error
}

func SaveChannelStatusProbeExecution(execution *ChannelStatusProbeExecution) (bool, error) {
	created := false
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		created, err = saveChannelStatusProbeExecutionTx(tx, execution)
		return err
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

func GetChannelStatusProbeExecutionsSince(startedAt int64) ([]ChannelStatusProbeExecution, error) {
	return getChannelStatusProbeExecutionsSince(DB, startedAt, nil, "")
}

func GetChannelStatusProbeExecutionsSinceForOverview(
	ctx context.Context,
	db *gorm.DB,
	startedAt int64,
	channelIDs []int,
	selectedModel string,
) ([]ChannelStatusProbeExecution, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	return getChannelStatusProbeExecutionsSince(queryDB, startedAt, channelIDs, selectedModel)
}

func getChannelStatusProbeExecutionsSince(
	db *gorm.DB,
	startedAt int64,
	channelIDs []int,
	selectedModel string,
) ([]ChannelStatusProbeExecution, error) {
	if channelIDs != nil && len(channelIDs) == 0 {
		return []ChannelStatusProbeExecution{}, nil
	}
	if !db.Migrator().HasTable(&ChannelStatusProbeExecution{}) {
		return []ChannelStatusProbeExecution{}, nil
	}
	var executions []ChannelStatusProbeExecution
	query := db.Where("finished_at >= ?", startedAt)
	if channelIDs != nil {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	if selectedModel != "" {
		query = query.Where("model_name = ?", selectedModel)
	}
	err := query.Order("channel_id ASC, model_name ASC, finished_at ASC, id ASC").Find(&executions).Error
	return executions, err
}

func UpdateChannelStatusProbeExecutionSample(executionId int64, status string, message string, now int64) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&ChannelStatusProbeExecution{}).
			Where("id = ?", executionId).
			Updates(map[string]any{"sample_status": status, "sample_message": message}).Error; err != nil {
			return err
		}
		var execution ChannelStatusProbeExecution
		if err := tx.Select("id", "channel_id", "logical_channel_id", "logical_revision", "model_name").
			Where("id = ?", executionId).First(&execution).Error; err != nil {
			return err
		}
		if execution.LogicalChannelId > 0 && execution.LogicalRevision > 0 {
			var row ChannelStatusProbeLogicalState
			if err := lockForUpdate(tx).Where("logical_channel_id = ? AND model_name = ?", execution.LogicalChannelId, execution.ModelName).
				First(&row).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if row.ExecutionId != executionId {
				return nil
			}
			state, err := row.State(execution.ChannelId)
			if err != nil {
				return err
			}
			state.SampleStatus = status
			state.SampleMessage = message
			state.UpdatedAt = now
			updated, err := newChannelStatusProbeLogicalStateRow(state)
			if err != nil {
				return err
			}
			updated.Id = row.Id
			return tx.Save(&updated).Error
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
	scope, err := resolveChannelStatusProbeScope(channelId)
	if err != nil {
		return nil, 0, err
	}
	query := DB.Model(&ChannelStatusProbeExecution{})
	if scope.Identity.Revision > 0 {
		query = query.Where(
			"channel_id = ? OR actual_channel_id = ? OR logical_channel_id = ?",
			channelId, channelId, scope.Identity.LogicalChannelID,
		)
	} else {
		query = query.Where(
			"channel_id = ? OR actual_channel_id = ? OR (logical_channel_id = ? AND logical_revision = 0)",
			channelId, channelId, scope.Identity.LogicalChannelID,
		)
	}
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
	err = query.Order("finished_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&executions).Error
	for index := range executions {
		executions[index].ChannelId = channelId
	}
	return executions, total, err
}

func DeleteChannelStatusProbeExecutionsBefore(
	ctx context.Context,
	cutoff int64,
	batchSize int,
	budget ChannelMonitorCleanupBudget,
) (int64, error) {
	if cutoff <= 0 || batchSize <= 0 {
		return 0, errors.New("渠道状态探测历史清理参数无效")
	}
	if !DB.Migrator().HasTable(&ChannelStatusProbeExecution{}) {
		return 0, nil
	}
	var deletedRows int64
	db := DB.WithContext(ctx)
	for {
		if budget.Exhausted() {
			return deletedRows, nil
		}
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
