package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	ChannelModelDetectionHistoryDefaultPageSize = 20
	ChannelModelDetectionHistoryMaxPageSize     = 100

	channelModelDetectionBucketResultSuccess   = "success"
	channelModelDetectionBucketResultAttention = "attention"
	channelModelDetectionBucketResultUnhealthy = "unhealthy"
	channelModelDetectionBucketResultFailed    = "failed"
	channelModelDetectionBucketResultRunning   = "running"
	channelModelDetectionBucketResultInactive  = "inactive"

	channelModelDetectionHealthUnconfigured        = "unconfigured"
	channelModelDetectionHealthPaused              = "paused"
	channelModelDetectionHealthPending             = "pending"
	channelModelDetectionHealthRunning             = "running"
	channelModelDetectionHealthHealthy             = "healthy"
	channelModelDetectionHealthAttention           = "attention"
	channelModelDetectionHealthUnhealthy           = "unhealthy"
	channelModelDetectionHealthDetectorUnavailable = "detector_unavailable"
	channelModelDetectionHealthStale               = "stale"
)

var (
	ErrChannelModelDetectionInvalidHistoryQuery = errors.New("模型检测历史查询参数无效")
	ErrChannelModelDetectionReportTooLarge      = errors.New("模型检测报告超过大小限制")
)

var channelModelDetectionKnownOutcomes = map[string]struct{}{
	"juice_pass_fingerprint_strong":          {},
	"juice_pass_fingerprint_unclear":         {},
	"juice_mismatch_fingerprint_strong":      {},
	"juice_mismatch_fingerprint_unclear":     {},
	"juice_insufficient_fingerprint_strong":  {},
	"juice_insufficient_fingerprint_unclear": {},
	"possible_non_gpt":                       {},
}

var channelModelDetectionRunStatuses = map[string]struct{}{
	model.ChannelModelDetectionRunStatusQueued:                  {},
	model.ChannelModelDetectionRunStatusWaitingDetector:         {},
	model.ChannelModelDetectionRunStatusSubmitting:              {},
	model.ChannelModelDetectionRunStatusRunning:                 {},
	model.ChannelModelDetectionRunStatusSubmissionUnknown:       {},
	model.ChannelModelDetectionRunStatusCompleted:               {},
	model.ChannelModelDetectionRunStatusPartial:                 {},
	model.ChannelModelDetectionRunStatusFailed:                  {},
	model.ChannelModelDetectionRunStatusExternalSessionConflict: {},
	model.ChannelModelDetectionRunStatusCanceling:               {},
	model.ChannelModelDetectionRunStatusCanceled:                {},
}

type ChannelModelDetectionCostResponse struct {
	Currency                   string  `json:"currency"`
	EstimatedQuota             *int64  `json:"estimated_quota"`
	EstimatedCostNanoCNY       *int64  `json:"estimated_cost_nano_cny"`
	EstimatedCostCNY           *string `json:"estimated_cost_cny"`
	CostEstimateUnknownCount   int64   `json:"cost_estimate_unknown_count"`
	SettledQuota               int64   `json:"settled_quota"`
	CostBasisQuota             int64   `json:"cost_basis_quota"`
	SettledCostNanoCNY         *int64  `json:"settled_cost_nano_cny"`
	SettledCostCNY             *string `json:"settled_cost_cny"`
	UnresolvedCostNanoCNY      *int64  `json:"unresolved_cost_nano_cny"`
	UnresolvedCostCNY          *string `json:"unresolved_cost_cny"`
	UnresolvedCostUnknownCount int64   `json:"unresolved_cost_unknown_count"`
	SettledRequestCount        int64   `json:"settled_request_count"`
	UnresolvedRequestCount     int64   `json:"unresolved_request_count"`
	Status                     string  `json:"status"`
	CostScope                  string  `json:"cost_scope"`
}

type ChannelModelDetectionProgressResponse struct {
	Planned          int64 `json:"planned"`
	LogicalCompleted int64 `json:"logical_completed"`
	Successful       int64 `json:"successful"`
	Errors           int64 `json:"errors"`
	Cancelled        int64 `json:"cancelled"`
	HTTPAttempts     int64 `json:"http_attempts"`
	Retries          int64 `json:"retries"`
}

type ChannelModelDetectionExecutionSummary struct {
	RunID                    string                                `json:"run_id"`
	TargetKey                string                                `json:"target_key"`
	Status                   string                                `json:"status"`
	ErrorCode                string                                `json:"error_code"`
	RequestModel             string                                `json:"request_model"`
	ClaimedModel             string                                `json:"claimed_model"`
	OutcomeCode              string                                `json:"outcome_code"`
	TitleCN                  string                                `json:"title_cn"`
	SubtitleCN               string                                `json:"subtitle_cn"`
	JuiceVerdictState        string                                `json:"juice_verdict_state"`
	FingerprintVerdictState  string                                `json:"fingerprint_verdict_state"`
	FingerprintModel         string                                `json:"fingerprint_model"`
	FingerprintClaimMismatch bool                                  `json:"fingerprint_claim_mismatch"`
	Preset                   string                                `json:"preset"`
	PresetSource             string                                `json:"preset_source"`
	Trigger                  string                                `json:"trigger"`
	Progress                 ChannelModelDetectionProgressResponse `json:"progress"`
	Cost                     ChannelModelDetectionCostResponse     `json:"cost"`
	StartedAt                int64                                 `json:"started_at"`
	FinishedAt               int64                                 `json:"finished_at"`
	UpdatedAt                int64                                 `json:"updated_at"`
}

type ChannelModelDetectionResultBucket struct {
	StartedAt      int64  `json:"started_at"`
	Result         string `json:"result"`
	DetectionCount int    `json:"detection_count"`
	Success        int    `json:"success"`
	Attention      int    `json:"attention"`
	Unhealthy      int    `json:"unhealthy"`
	Failed         int    `json:"failed"`
	Running        int    `json:"running"`
	Inactive       int    `json:"inactive"`
}

type ChannelModelDetectionTargetSummary struct {
	TargetKey    string                                 `json:"target_key"`
	RequestModel string                                 `json:"request_model"`
	ClaimedModel string                                 `json:"claimed_model"`
	Enabled      bool                                   `json:"enabled"`
	Position     int                                    `json:"position"`
	Latest       *ChannelModelDetectionExecutionSummary `json:"latest"`
	RecentWindow []ChannelModelDetectionResultBucket    `json:"recent_window"`
}

type ChannelModelDetectionChannelConfigResponse struct {
	ChannelID       int   `json:"channel_id"`
	ScheduleEnabled bool  `json:"schedule_enabled"`
	Revision        int64 `json:"revision"`
	CreatedAt       int64 `json:"created_at"`
	UpdatedAt       int64 `json:"updated_at"`
}

type ChannelModelDetectionActiveRunResponse struct {
	RunID        string                                `json:"run_id"`
	Status       string                                `json:"status"`
	Trigger      string                                `json:"trigger"`
	Preset       string                                `json:"preset"`
	PresetSource string                                `json:"preset_source"`
	Progress     ChannelModelDetectionProgressResponse `json:"progress"`
	QueuedAt     int64                                 `json:"queued_at"`
	StartedAt    int64                                 `json:"started_at"`
	UpdatedAt    int64                                 `json:"updated_at"`
}

type ChannelModelDetectionChannelResponse struct {
	ID                         int                                         `json:"id"`
	Name                       string                                      `json:"name"`
	Type                       int                                         `json:"type"`
	ChannelStatus              int                                         `json:"channel_status"`
	Remark                     string                                      `json:"remark"`
	Groups                     []string                                    `json:"groups"`
	CostRatio                  *float64                                    `json:"cost_ratio"`
	SupportedModels            []string                                    `json:"supported_models"`
	HealthStatus               string                                      `json:"health_status"`
	Config                     *ChannelModelDetectionChannelConfigResponse `json:"config"`
	ActiveRun                  *ChannelModelDetectionActiveRunResponse     `json:"active_run"`
	Targets                    []ChannelModelDetectionTargetSummary        `json:"targets"`
	LatestRunCost              *ChannelModelDetectionCostResponse          `json:"latest_run_cost"`
	TodayModelDetectionCost    *ChannelModelDetectionCostResponse          `json:"today_model_detection_cost"`
	TodayModelDetectionCostCNY float64                                     `json:"today_model_detection_cost_cny"`
}

type ChannelModelDetectionPresetEstimateResponse struct {
	Preset            string `json:"preset"`
	Available         bool   `json:"available"`
	LogicalRequests   *int64 `json:"logical_requests"`
	Fixed32KRequests  *int64 `json:"fixed_32k_requests"`
	ConfigHash        string `json:"config_hash"`
	UnavailableReason string `json:"unavailable_reason"`
}

type ChannelModelDetectionDetectorResponse struct {
	State                 string                                                 `json:"state"`
	DetectorURLConfigured bool                                                   `json:"detector_url_configured"`
	DetectorURLMasked     string                                                 `json:"detector_url_masked"`
	Busy                  bool                                                   `json:"busy"`
	ActiveSessionOwned    bool                                                   `json:"active_session_owned"`
	DeploymentID          *string                                                `json:"deployment_id"`
	LastCheckedAt         int64                                                  `json:"last_checked_at"`
	LastError             string                                                 `json:"last_error"`
	CompatibilityMessage  string                                                 `json:"compatibility_message"`
	Estimates             map[string]ChannelModelDetectionPresetEstimateResponse `json:"estimates"`
}

type ChannelModelDetectionSettingsSummary struct {
	DetectorURLConfigured bool   `json:"detector_url_configured"`
	DetectorURLMasked     string `json:"detector_url_masked"`
	ScheduledPreset       string `json:"scheduled_preset"`
	ScheduleEnabled       bool   `json:"schedule_enabled"`
	IntervalMinutes       int    `json:"interval_minutes"`
	DisplayValue          int    `json:"display_value"`
	DisplayUnit           string `json:"display_unit"`
	IntervalHours         int    `json:"-"`
	ScheduleTime          string `json:"-"`
	Timezone              string `json:"-"`
	NextBatchAt           int64  `json:"next_batch_at"`
	Revision              int64  `json:"revision"`
}

type ChannelModelDetectionOverviewResponse struct {
	ServerNow          int64                                  `json:"server_now"`
	SnapshotVersion    int                                    `json:"snapshot_version"`
	SnapshotRevision   uint64                                 `json:"snapshot_revision"`
	EventWatermark     uint64                                 `json:"event_watermark"`
	GeneratedAt        int64                                  `json:"generated_at"`
	DataCutoffAt       int64                                  `json:"data_cutoff_at"`
	SnapshotAgeSeconds int64                                  `json:"snapshot_age_seconds"`
	Stale              bool                                   `json:"stale"`
	Settings           ChannelModelDetectionSettingsSummary   `json:"settings"`
	Detector           ChannelModelDetectionDetectorResponse  `json:"detector"`
	Summary            map[string]int                         `json:"summary"`
	Groups             []string                               `json:"groups"`
	Models             []string                               `json:"models"`
	ModelsByGroup      map[string][]string                    `json:"models_by_group"`
	Channels           []ChannelModelDetectionChannelResponse `json:"channels"`
}

type ChannelModelDetectionRunHistoryQuery struct {
	Page     int
	PageSize int
	Trigger  string
	Status   string
	Model    string
	Outcome  string
}

type ChannelModelDetectionRunSummary struct {
	RunID                string                                `json:"run_id"`
	ChannelID            int                                   `json:"channel_id"`
	Trigger              string                                `json:"trigger"`
	Preset               string                                `json:"preset"`
	PresetSource         string                                `json:"preset_source"`
	Status               string                                `json:"status"`
	TargetCount          int                                   `json:"target_count"`
	CompletedTargetCount int                                   `json:"completed_target_count"`
	Progress             ChannelModelDetectionProgressResponse `json:"progress"`
	Cost                 ChannelModelDetectionCostResponse     `json:"cost"`
	QueuedAt             int64                                 `json:"queued_at"`
	StartedAt            int64                                 `json:"started_at"`
	FinishedAt           int64                                 `json:"finished_at"`
	UpdatedAt            int64                                 `json:"updated_at"`
	CancelRequestedAt    int64                                 `json:"cancel_requested_at"`
	ErrorCode            string                                `json:"error_code"`
	ErrorMessage         string                                `json:"error_message"`
	CreatedByUserID      int                                   `json:"created_by_user_id"`
	CreatedByUsername    string                                `json:"created_by_username"`
	CreatedAt            int64                                 `json:"created_at"`
}

type ChannelModelDetectionRunHistoryResponse struct {
	Page     int                               `json:"page"`
	PageSize int                               `json:"page_size"`
	Total    int64                             `json:"total"`
	Items    []ChannelModelDetectionRunSummary `json:"items"`
}

type ChannelModelDetectionExecutionDetail struct {
	ChannelModelDetectionExecutionSummary
	OfficialSessionID string `json:"official_session_id"`
	Official          bool   `json:"official"`
	ConfigHash        string `json:"config_hash"`
	SchemaVersion     int    `json:"schema_version"`
	ScoringVersion    string `json:"scoring_version"`
	BaselineID        string `json:"baseline_id"`
	BaselineSHA256    string `json:"baseline_sha256"`
	BuildHash         string `json:"build_hash"`
	UsageAvailable    bool   `json:"usage_available"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	TotalTokens       int64  `json:"total_tokens"`
	ReportSHA256      string `json:"report_sha256"`
	FinalErrorCode    string `json:"final_error_code"`
	ErrorCode         string `json:"error_code"`
	ErrorMessage      string `json:"error_message"`
	Report            any    `json:"report"`
}

type ChannelModelDetectionRunDetailResponse struct {
	Run        ChannelModelDetectionRunSummary        `json:"run"`
	Executions []ChannelModelDetectionExecutionDetail `json:"executions"`
}

type channelModelDetectionExecutionOverviewRow struct {
	model.ChannelModelDetectionExecution
	RunTrigger      string `gorm:"column:run_trigger"`
	RunPresetSource string `gorm:"column:run_preset_source"`
}

type channelModelDetectionLogicalOverviewData struct {
	Configs []model.ChannelModelDetectionLogicalConfig
	Targets []model.ChannelModelDetectionLogicalTarget
}

func GetChannelModelDetectionOverview(ctx context.Context, tx *gorm.DB, now int64) (ChannelModelDetectionOverviewResponse, error) {
	db, err := channelModelDetectionQueryDB(ctx, tx)
	if err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	if now <= 0 {
		now = common.GetTimestamp()
	}

	var channels []model.Channel
	if err := db.Omit("key").Order("id ASC").Find(&channels).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	activeLogicalGroups := make(map[int64]struct{})
	if model.IsLogicalChannelGroupingEnabled() && db.Migrator().HasTable(&model.ChannelLogicalGroup{}) {
		var groupIDs []int64
		if err := db.Model(&model.ChannelLogicalGroup{}).Where("status = ?", model.ChannelLogicalGroupStatusEnabled).Pluck("id", &groupIDs).Error; err != nil {
			return ChannelModelDetectionOverviewResponse{}, err
		}
		for _, groupID := range groupIDs {
			activeLogicalGroups[groupID] = struct{}{}
		}
	}
	for index := range channels {
		if channels[index].LogicalChannelID == nil {
			continue
		}
		if _, active := activeLogicalGroups[*channels[index].LogicalChannelID]; !active {
			channels[index].LogicalChannelID = nil
		}
	}

	global := model.ChannelModelDetectionGlobalConfig{}
	if err := db.Where("id = ?", model.ChannelModelDetectionConfigID).First(&global).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	applyChannelModelDetectionGlobalDefaults(&global)
	displayValue, displayUnit := global.EffectiveDisplay()
	bucketSeconds := model.ChannelModelDetectionDisplayBucketSeconds(displayUnit)
	currentBucket := model.ChannelModelDetectionDisplayBucketStart(now, displayUnit)
	minimumBucket := currentBucket - int64(displayValue-1)*bucketSeconds

	var configs []model.ChannelModelDetectionConfig
	if err := db.Order("channel_id ASC").Find(&configs).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	var targets []model.ChannelModelDetectionTarget
	if err := db.Order("channel_id ASC, position ASC, id ASC").Find(&targets).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	logicalData := channelModelDetectionLogicalOverviewData{}
	if db.Migrator().HasTable(&model.ChannelModelDetectionLogicalConfig{}) {
		if err := db.Order("logical_channel_id ASC").Find(&logicalData.Configs).Error; err != nil {
			return ChannelModelDetectionOverviewResponse{}, err
		}
	}
	if db.Migrator().HasTable(&model.ChannelModelDetectionLogicalTarget{}) {
		if err := db.Order("logical_channel_id ASC, position ASC, id ASC").Find(&logicalData.Targets).Error; err != nil {
			return ChannelModelDetectionOverviewResponse{}, err
		}
	}

	runTable := db.NamingStrategy.TableName("ChannelModelDetectionRun")
	activeStatuses := []string{
		model.ChannelModelDetectionRunStatusQueued,
		model.ChannelModelDetectionRunStatusWaitingDetector,
		model.ChannelModelDetectionRunStatusSubmitting,
		model.ChannelModelDetectionRunStatusRunning,
		model.ChannelModelDetectionRunStatusSubmissionUnknown,
		model.ChannelModelDetectionRunStatusCanceling,
	}
	var runs []model.ChannelModelDetectionRun
	latestRunClause := "NOT EXISTS (SELECT 1 FROM " + runTable + " AS newer_run WHERE ((COALESCE(detection_run.logical_revision, 0) > 0 AND COALESCE(newer_run.logical_revision, 0) > 0 AND newer_run.logical_channel_id = detection_run.logical_channel_id) OR (COALESCE(detection_run.logical_revision, 0) = 0 AND COALESCE(newer_run.logical_revision, 0) = 0 AND newer_run.channel_id = detection_run.channel_id)) AND (newer_run.created_at > detection_run.created_at OR (newer_run.created_at = detection_run.created_at AND newer_run.id > detection_run.id)))"
	if err := db.Table(runTable+" AS detection_run").Select("detection_run.*").
		Where("detection_run.status IN ? OR "+latestRunClause, activeStatuses).
		Order("detection_run.channel_id ASC, detection_run.created_at DESC, detection_run.id DESC").
		Scan(&runs).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}

	executionTable := db.NamingStrategy.TableName("ChannelModelDetectionExecution")
	targetTable := db.NamingStrategy.TableName("ChannelModelDetectionTarget")
	var executionRows []channelModelDetectionExecutionOverviewRow
	latestExecutionClause := "NOT EXISTS (SELECT 1 FROM " + executionTable + " AS newer_execution WHERE newer_execution.target_key = detection_execution.target_key AND (newer_execution.created_at > detection_execution.created_at OR (newer_execution.created_at = detection_execution.created_at AND newer_execution.id > detection_execution.id)))"
	currentTargetClause := "EXISTS (SELECT 1 FROM " + targetTable + " AS current_target WHERE current_target.id = detection_execution.target_id)"
	if db.Migrator().HasTable(&model.ChannelModelDetectionLogicalTarget{}) {
		logicalTargetTable := db.NamingStrategy.TableName("ChannelModelDetectionLogicalTarget")
		currentTargetClause = "((detection_execution.logical_target_id IS NOT NULL AND EXISTS (SELECT 1 FROM " + logicalTargetTable + " AS current_logical_target WHERE current_logical_target.id = detection_execution.logical_target_id)) OR (detection_execution.logical_target_id IS NULL AND EXISTS (SELECT 1 FROM " + targetTable + " AS current_target WHERE current_target.id = detection_execution.target_id)))"
	}
	executionTimestamp := "(CASE WHEN detection_execution.started_at > 0 THEN detection_execution.started_at ELSE detection_execution.created_at END)"
	if err := db.Table(executionTable+" AS detection_execution").
		Select("detection_execution.*, detection_run.trigger AS run_trigger, detection_run.preset_source AS run_preset_source").
		Joins("JOIN "+runTable+" AS detection_run ON detection_run.run_id = detection_execution.run_id").
		Where(currentTargetClause).Where("("+executionTimestamp+" BETWEEN ? AND ?) OR "+latestExecutionClause, minimumBucket, now).
		Order("detection_execution.target_id ASC, detection_execution.created_at ASC, detection_execution.id ASC").Scan(&executionRows).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}

	runIDs := make([]string, 0, len(runs))
	for i := range runs {
		runIDs = append(runIDs, runs[i].RunId)
	}
	latestExecutionByTarget := make(map[int64]model.ChannelModelDetectionExecution, len(targets))
	for i := range executionRows {
		execution := executionRows[i].ChannelModelDetectionExecution
		latest, exists := latestExecutionByTarget[execution.TargetId]
		if !exists || execution.CreatedAt > latest.CreatedAt || execution.CreatedAt == latest.CreatedAt && execution.Id > latest.Id {
			latestExecutionByTarget[execution.TargetId] = execution
		}
	}
	executionIDs := make([]int64, 0, len(latestExecutionByTarget))
	for _, execution := range latestExecutionByTarget {
		executionIDs = append(executionIDs, execution.Id)
	}
	todayStart := model.ChannelDailyCostDayStart(now)
	todayCostClause := "((created_at >= ? AND created_at <= ?) OR (settled_at >= ? AND settled_at <= ?))"
	var costEvents []model.ChannelModelDetectionCostEvent
	costQuery := db.Model(&model.ChannelModelDetectionCostEvent{})
	switch {
	case len(runIDs) > 0 && len(executionIDs) > 0:
		costQuery = costQuery.Where("(run_id IN ? OR execution_id IN ?) OR "+todayCostClause, runIDs, executionIDs, todayStart, now, todayStart, now)
	case len(runIDs) > 0:
		costQuery = costQuery.Where("run_id IN ? OR "+todayCostClause, runIDs, todayStart, now, todayStart, now)
	case len(executionIDs) > 0:
		costQuery = costQuery.Where("execution_id IN ? OR "+todayCostClause, executionIDs, todayStart, now, todayStart, now)
	default:
		costQuery = costQuery.Where(todayCostClause, todayStart, now, todayStart, now)
	}
	if err := costQuery.Order("id ASC").Find(&costEvents).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	var todayCosts []model.ChannelDailyCost
	if err := db.Where("day_start = ?", todayStart).Order("channel_id ASC").Find(&todayCosts).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	var ratioMonitors []model.ChannelRatioMonitor
	if err := db.Select("channel_id", "ratio", "cost_conversion", "updated_time").Order("channel_id ASC").Find(&ratioMonitors).Error; err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}

	response, err := buildChannelModelDetectionOverview(now, global, channels, configs, targets, runs, executionRows, costEvents, todayCosts, ratioMonitors, logicalData)
	if err != nil {
		return ChannelModelDetectionOverviewResponse{}, err
	}
	response.Detector = channelModelDetectionDetectorResponse(ChannelModelDetectionServiceSnapshot(global.DetectorURL))
	return response, nil
}

func channelModelDetectionDetectorResponse(status ChannelModelDetectionServiceResponse) ChannelModelDetectionDetectorResponse {
	estimates := status.Estimates
	if estimates == nil {
		estimates = make(map[string]ChannelModelDetectionPresetEstimateResponse)
	}
	return ChannelModelDetectionDetectorResponse{
		State: status.State, DetectorURLConfigured: status.DetectorURLConfigured,
		DetectorURLMasked: status.DetectorURLMasked, Busy: status.Busy,
		ActiveSessionOwned: status.ActiveSessionOwned, DeploymentID: status.DeploymentID,
		LastCheckedAt: status.LastCheckedAt, LastError: status.LastError,
		CompatibilityMessage: status.CompatibilityMessage, Estimates: estimates,
	}
}

func ListChannelModelDetectionRuns(ctx context.Context, tx *gorm.DB, channelID int, input ChannelModelDetectionRunHistoryQuery) (ChannelModelDetectionRunHistoryResponse, error) {
	db, err := channelModelDetectionQueryDB(ctx, tx)
	if err != nil {
		return ChannelModelDetectionRunHistoryResponse{}, err
	}
	query, err := normalizeChannelModelDetectionRunHistoryQuery(input)
	if err != nil {
		return ChannelModelDetectionRunHistoryResponse{}, err
	}
	if channelID <= 0 {
		return ChannelModelDetectionRunHistoryResponse{}, fmt.Errorf("%w: 渠道 ID 必须为正整数", ErrChannelModelDetectionInvalidHistoryQuery)
	}
	var channel model.Channel
	if err := db.Select("id").Where("id = ?", channelID).First(&channel).Error; err != nil {
		return ChannelModelDetectionRunHistoryResponse{}, err
	}

	runTable := db.NamingStrategy.TableName("ChannelModelDetectionRun")
	executionTable := db.NamingStrategy.TableName("ChannelModelDetectionExecution")
	runsQuery := db.Table(runTable+" AS detection_run").Where("detection_run.channel_id = ?", channelID)
	sharedPhysicalView := false
	if identity, identityErr := channelModelDetectionLogicalIdentity(db, channelID, db == model.DB); identityErr == nil && identity.Revision > 0 {
		runsQuery = db.Table(runTable+" AS detection_run").Where("detection_run.channel_id = ? OR detection_run.logical_channel_id = ?", channelID, identity.LogicalChannelID)
		sharedPhysicalView = true
	}
	if query.Trigger != "" {
		runsQuery = runsQuery.Where("detection_run.trigger = ?", query.Trigger)
	}
	if query.Status != "" {
		runsQuery = runsQuery.Where("detection_run.status = ?", query.Status)
	}
	if query.Model != "" || query.Outcome != "" {
		conditions := []string{"filter_execution.run_id = detection_run.run_id"}
		args := make([]any, 0, 2)
		if query.Model != "" {
			conditions = append(conditions, "filter_execution.request_model = ?")
			args = append(args, query.Model)
		}
		if query.Outcome != "" {
			conditions = append(conditions, "filter_execution.outcome_code = ?")
			args = append(args, query.Outcome)
		}
		runsQuery = runsQuery.Where("EXISTS (SELECT 1 FROM "+executionTable+" AS filter_execution WHERE "+strings.Join(conditions, " AND ")+")", args...)
	}

	var total int64
	if err := runsQuery.Count(&total).Error; err != nil {
		return ChannelModelDetectionRunHistoryResponse{}, err
	}
	var runs []model.ChannelModelDetectionRun
	if err := runsQuery.Select("detection_run.*").
		Order("detection_run.created_at DESC, detection_run.id DESC").
		Limit(query.PageSize).Offset((query.Page - 1) * query.PageSize).
		Scan(&runs).Error; err != nil {
		return ChannelModelDetectionRunHistoryResponse{}, err
	}

	runIDs := make([]string, 0, len(runs))
	for i := range runs {
		runIDs = append(runIDs, runs[i].RunId)
	}
	eventsByRun, _, err := queryChannelModelDetectionCostEvents(db, runIDs, nil)
	if err != nil {
		return ChannelModelDetectionRunHistoryResponse{}, err
	}
	items := make([]ChannelModelDetectionRunSummary, 0, len(runs))
	for i := range runs {
		projectedRun := runs[i]
		physicalEvents := eventsByRun[runs[i].RunId]
		if sharedPhysicalView && runs[i].LogicalRevision > 0 {
			physicalEvents = nil
			for _, event := range eventsByRun[runs[i].RunId] {
				if event.ChannelId == channelID {
					physicalEvents = append(physicalEvents, event)
				}
			}
			projectedRun.ChannelId = channelID
		}
		aggregate, aggregateErr := aggregateChannelModelDetectionCostForPhysicalView(
			projectedRun, physicalEvents, eventsByRun[runs[i].RunId], sharedPhysicalView && runs[i].LogicalRevision > 0,
		)
		if aggregateErr != nil {
			return ChannelModelDetectionRunHistoryResponse{}, aggregateErr
		}
		items = append(items, channelModelDetectionRunSummary(projectedRun, aggregate))
	}
	return ChannelModelDetectionRunHistoryResponse{Page: query.Page, PageSize: query.PageSize, Total: total, Items: items}, nil
}

func GetChannelModelDetectionRunDetail(ctx context.Context, tx *gorm.DB, runID string) (ChannelModelDetectionRunDetailResponse, error) {
	db, err := channelModelDetectionQueryDB(ctx, tx)
	if err != nil {
		return ChannelModelDetectionRunDetailResponse{}, err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" || len(runID) > 64 {
		return ChannelModelDetectionRunDetailResponse{}, fmt.Errorf("%w: 轮次 ID 无效", ErrChannelModelDetectionInvalidHistoryQuery)
	}
	var run model.ChannelModelDetectionRun
	if err := db.Where("run_id = ?", runID).First(&run).Error; err != nil {
		return ChannelModelDetectionRunDetailResponse{}, err
	}
	var executions []model.ChannelModelDetectionExecution
	if err := db.Where("run_id = ?", runID).Order("id ASC").Find(&executions).Error; err != nil {
		return ChannelModelDetectionRunDetailResponse{}, err
	}
	executionIDs := make([]int64, 0, len(executions))
	for i := range executions {
		executionIDs = append(executionIDs, executions[i].Id)
	}
	eventsByRun, eventsByExecution, err := queryChannelModelDetectionCostEvents(db, []string{runID}, executionIDs)
	if err != nil {
		return ChannelModelDetectionRunDetailResponse{}, err
	}
	runAggregate, err := aggregateChannelModelDetectionCostForRun(run, eventsByRun[runID])
	if err != nil {
		return ChannelModelDetectionRunDetailResponse{}, err
	}
	response := ChannelModelDetectionRunDetailResponse{
		Run:        channelModelDetectionRunSummary(run, runAggregate),
		Executions: make([]ChannelModelDetectionExecutionDetail, 0, len(executions)),
	}
	for i := range executions {
		execution := executions[i]
		if len(execution.ReportJSON) > model.ChannelModelDetectionMaxReportBytes {
			return ChannelModelDetectionRunDetailResponse{}, fmt.Errorf("%w: %s", ErrChannelModelDetectionReportTooLarge, execution.TargetKey)
		}
		var report any
		if strings.TrimSpace(string(execution.ReportJSON)) != "" {
			if err := common.UnmarshalJsonStr(string(execution.ReportJSON), &report); err != nil {
				return ChannelModelDetectionRunDetailResponse{}, err
			}
			report = redactChannelModelDetectionReportSecrets(report)
		}
		aggregate, aggregateErr := aggregateChannelModelDetectionCostForExecution(execution, eventsByExecution[execution.Id])
		if aggregateErr != nil {
			return ChannelModelDetectionRunDetailResponse{}, aggregateErr
		}
		summary := channelModelDetectionExecutionSummary(execution, run.Trigger, run.PresetSource, aggregate)
		response.Executions = append(response.Executions, ChannelModelDetectionExecutionDetail{
			ChannelModelDetectionExecutionSummary: summary,
			OfficialSessionID:                     execution.OfficialSessionId,
			Official:                              execution.Official, ConfigHash: execution.ConfigHash,
			SchemaVersion: execution.SchemaVersion, ScoringVersion: execution.ScoringVersion,
			BaselineID: execution.BaselineId, BaselineSHA256: execution.BaselineSHA256, BuildHash: execution.BuildHash,
			UsageAvailable: execution.UsageAvailable,
			InputTokens:    execution.InputTokens, OutputTokens: execution.OutputTokens, TotalTokens: execution.TotalTokens,
			ReportSHA256: execution.ReportSHA256, FinalErrorCode: execution.FinalErrorCode,
			ErrorCode: execution.ErrorCode, ErrorMessage: execution.ErrorMessage, Report: report,
		})
	}
	return response, nil
}

func buildChannelModelDetectionOverview(now int64, global model.ChannelModelDetectionGlobalConfig, channels []model.Channel, configs []model.ChannelModelDetectionConfig, targets []model.ChannelModelDetectionTarget, runs []model.ChannelModelDetectionRun, executionRows []channelModelDetectionExecutionOverviewRow, costEvents []model.ChannelModelDetectionCostEvent, todayCosts []model.ChannelDailyCost, ratioMonitors []model.ChannelRatioMonitor, logicalData ...channelModelDetectionLogicalOverviewData) (ChannelModelDetectionOverviewResponse, error) {
	todayStart := model.ChannelDailyCostDayStart(now)
	configured := global.DetectorURLConfigured()
	maskedURL := maskChannelModelDetectorURL(global.DetectorURL)
	displayValue, displayUnit := global.EffectiveDisplay()
	response := ChannelModelDetectionOverviewResponse{
		ServerNow: now,
		Settings: ChannelModelDetectionSettingsSummary{
			DetectorURLConfigured: configured, DetectorURLMasked: maskedURL,
			ScheduledPreset: global.ScheduledPreset, ScheduleEnabled: global.ScheduleEnabled,
			IntervalMinutes: global.EffectiveIntervalMinutes(), IntervalHours: global.IntervalHours, ScheduleTime: global.ScheduleTime, Timezone: global.Timezone,
			DisplayValue: displayValue, DisplayUnit: displayUnit,
			NextBatchAt: global.NextBatchAt, Revision: global.Revision,
		},
		Detector: ChannelModelDetectionDetectorResponse{
			State: "unknown", DetectorURLConfigured: configured, DetectorURLMasked: maskedURL,
			Estimates: make(map[string]ChannelModelDetectionPresetEstimateResponse),
		},
		Summary: map[string]int{
			channelModelDetectionHealthUnconfigured: 0, channelModelDetectionHealthPaused: 0,
			channelModelDetectionHealthPending: 0, channelModelDetectionHealthRunning: 0,
			channelModelDetectionHealthHealthy: 0, channelModelDetectionHealthAttention: 0,
			channelModelDetectionHealthUnhealthy: 0, channelModelDetectionHealthDetectorUnavailable: 0,
			channelModelDetectionHealthStale: 0,
		},
		Groups: []string{}, Models: []string{}, ModelsByGroup: map[string][]string{},
		Channels: make([]ChannelModelDetectionChannelResponse, 0, len(channels)),
	}
	if !configured {
		response.Detector.State = "unconfigured"
	}

	configByChannel := make(map[int]model.ChannelModelDetectionConfig, len(configs))
	logicalChannelByPhysical := make(map[int]int64, len(channels))
	for i := range channels {
		logicalID := int64(channels[i].Id)
		if model.IsLogicalChannelGroupingEnabled() && channels[i].LogicalChannelID != nil && *channels[i].LogicalChannelID > 0 {
			logicalID = *channels[i].LogicalChannelID
		}
		logicalChannelByPhysical[channels[i].Id] = logicalID
	}
	configByLogical := make(map[int64]model.ChannelModelDetectionConfig)
	for i := range configs {
		configByChannel[configs[i].ChannelId] = configs[i]
		logicalID := logicalChannelByPhysical[configs[i].ChannelId]
		if logicalID > 0 {
			if previous, exists := configByLogical[logicalID]; !exists || configs[i].ChannelId < previous.ChannelId {
				configByLogical[logicalID] = configs[i]
			}
		}
	}
	if len(logicalData) > 0 {
		for _, logicalConfig := range logicalData[0].Configs {
			configByLogical[logicalConfig.LogicalChannelId] = model.ChannelModelDetectionConfig{
				Id: logicalConfig.Id, ScheduleEnabled: logicalConfig.ScheduleEnabled, Revision: logicalConfig.Revision,
				RunningRunId: logicalConfig.RunningRunId, CreatedAt: logicalConfig.CreatedAt, UpdatedAt: logicalConfig.UpdatedAt,
			}
		}
	}
	targetsByChannel := make(map[int][]model.ChannelModelDetectionTarget)
	targetsByLogical := make(map[int64][]model.ChannelModelDetectionTarget)
	for i := range targets {
		targetsByChannel[targets[i].ChannelId] = append(targetsByChannel[targets[i].ChannelId], targets[i])
		logicalID := logicalChannelByPhysical[targets[i].ChannelId]
		if logicalID > 0 {
			// The canonical target set is owned by the lowest member.
			if config, ok := configByLogical[logicalID]; ok && config.ChannelId == targets[i].ChannelId {
				targetsByLogical[logicalID] = append(targetsByLogical[logicalID], targets[i])
			}
		}
	}
	if len(logicalData) > 0 {
		for _, logicalConfig := range logicalData[0].Configs {
			targetsByLogical[logicalConfig.LogicalChannelId] = nil
		}
		for _, logicalTarget := range logicalData[0].Targets {
			targetsByLogical[logicalTarget.LogicalChannelId] = append(targetsByLogical[logicalTarget.LogicalChannelId], model.ChannelModelDetectionTarget{
				Id: logicalTarget.Id, ConfigId: logicalTarget.ConfigId, TargetKey: logicalTarget.TargetKey,
				RequestModel: logicalTarget.RequestModel, ClaimedModel: logicalTarget.ClaimedModel,
				Position: logicalTarget.Position, Enabled: logicalTarget.Enabled,
				CreatedAt: logicalTarget.CreatedAt, UpdatedAt: logicalTarget.UpdatedAt,
			})
		}
	}
	latestRunByChannel := make(map[int]model.ChannelModelDetectionRun)
	activeRunByChannel := make(map[int]model.ChannelModelDetectionRun)
	latestRunByLogical := make(map[int64]model.ChannelModelDetectionRun)
	activeRunByLogical := make(map[int64]model.ChannelModelDetectionRun)
	for i := range runs {
		run := runs[i]
		latest, ok := latestRunByChannel[run.ChannelId]
		if !ok || channelModelDetectionRunIsNewer(run, latest) {
			latestRunByChannel[run.ChannelId] = run
		}
		if model.IsChannelModelDetectionActiveRunStatus(run.Status) {
			active, exists := activeRunByChannel[run.ChannelId]
			if !exists || channelModelDetectionRunIsNewer(run, active) {
				activeRunByChannel[run.ChannelId] = run
			}
		}
		if run.LogicalChannelID > 0 && run.LogicalRevision > 0 {
			if latest, ok := latestRunByLogical[run.LogicalChannelID]; !ok || channelModelDetectionRunIsNewer(run, latest) {
				latestRunByLogical[run.LogicalChannelID] = run
			}
			if model.IsChannelModelDetectionActiveRunStatus(run.Status) {
				if active, ok := activeRunByLogical[run.LogicalChannelID]; !ok || channelModelDetectionRunIsNewer(run, active) {
					activeRunByLogical[run.LogicalChannelID] = run
				}
			}
		}
	}
	executionsByTarget := make(map[string][]channelModelDetectionExecutionOverviewRow, len(targets))
	for i := range executionRows {
		executionsByTarget[executionRows[i].TargetKey] = append(executionsByTarget[executionRows[i].TargetKey], executionRows[i])
	}
	eventsByRun := make(map[string][]model.ChannelModelDetectionCostEvent)
	eventsByExecution := make(map[int64][]model.ChannelModelDetectionCostEvent)
	todayCostEventsByChannel := make(map[int][]model.ChannelModelDetectionCostEvent)
	for i := range costEvents {
		event := costEvents[i]
		eventsByRun[event.RunId] = append(eventsByRun[event.RunId], event)
		eventsByExecution[event.ExecutionId] = append(eventsByExecution[event.ExecutionId], event)
		isToday := event.CreatedAt >= todayStart && event.CreatedAt <= now
		if event.SettlementStatus == model.ChannelModelDetectionSettlementSettled {
			isToday = event.SettledAt >= todayStart && event.SettledAt <= now
		}
		if isToday {
			todayCostEventsByChannel[event.ChannelId] = append(todayCostEventsByChannel[event.ChannelId], event)
		}
	}
	todayModelDetectionCostByChannel := make(map[int]int64, len(todayCosts))
	for i := range todayCosts {
		todayModelDetectionCostByChannel[todayCosts[i].ChannelId] = todayCosts[i].ModelDetectionCostNanoCNY
	}
	costRatioByChannel := make(map[int]float64, len(ratioMonitors))
	for i := range ratioMonitors {
		monitor := ratioMonitors[i]
		if monitor.UpdatedTime <= 0 {
			continue
		}
		conversion, conversionErr := ParseChannelMonitorCostConversion(monitor.CostConversion)
		if conversionErr != nil {
			continue
		}
		costRatio, _, ratioErr := CalculateChannelMonitorCostRatio(monitor.Ratio, conversion)
		if ratioErr == nil {
			costRatioByChannel[monitor.ChannelId] = costRatio
		}
	}

	groupSet := map[string]struct{}{}
	modelSet := map[string]struct{}{}
	modelsByGroupSet := map[string]map[string]struct{}{}
	supportedModelsByLogical := make(map[int64]map[string]struct{})
	for i := range channels {
		logicalID := logicalChannelByPhysical[channels[i].Id]
		if logicalID == int64(channels[i].Id) {
			continue
		}
		if supportedModelsByLogical[logicalID] == nil {
			supportedModelsByLogical[logicalID] = make(map[string]struct{})
		}
		for _, supportedModel := range splitChannelModelDetectionList(channels[i].Models) {
			supportedModelsByLogical[logicalID][supportedModel] = struct{}{}
		}
	}
	for i := range channels {
		channel := channels[i]
		groups := splitChannelModelDetectionList(channel.Group)
		supportedModels := splitChannelModelDetectionList(channel.Models)
		logicalID := logicalChannelByPhysical[channel.Id]
		if logicalID != int64(channel.Id) {
			supportedModels = sortedChannelModelDetectionSet(supportedModelsByLogical[logicalID])
		}
		for _, group := range groups {
			groupSet[group] = struct{}{}
			if modelsByGroupSet[group] == nil {
				modelsByGroupSet[group] = map[string]struct{}{}
			}
		}
		for _, supportedModel := range supportedModels {
			modelSet[supportedModel] = struct{}{}
			for _, group := range groups {
				modelsByGroupSet[group][supportedModel] = struct{}{}
			}
		}
		item := ChannelModelDetectionChannelResponse{
			ID: channel.Id, Name: channel.Name, Type: channel.Type, ChannelStatus: channel.Status,
			Groups: groups, SupportedModels: supportedModels, Targets: []ChannelModelDetectionTargetSummary{},
			TodayModelDetectionCostCNY: float64(todayModelDetectionCostByChannel[channel.Id]) / float64(model.ChannelDailyCostNanoPerCNY),
		}
		if costRatio, exists := costRatioByChannel[channel.Id]; exists {
			item.CostRatio = &costRatio
		}
		if todayEvents := todayCostEventsByChannel[channel.Id]; len(todayEvents) > 0 {
			aggregate, aggregateErr := aggregateChannelModelDetectionCostEventList(todayEvents)
			if aggregateErr != nil {
				return ChannelModelDetectionOverviewResponse{}, aggregateErr
			}
			cost := channelModelDetectionCostResponse(aggregate)
			item.TodayModelDetectionCost = &cost
		}
		if channel.Remark != nil {
			item.Remark = *channel.Remark
		}
		config, hasConfig := configByChannel[channel.Id]
		if logicalID != int64(channel.Id) {
			if sharedConfig, ok := configByLogical[logicalID]; ok {
				config, hasConfig = sharedConfig, true
				config.ChannelId = channel.Id
			}
		}
		if hasConfig {
			item.Config = &ChannelModelDetectionChannelConfigResponse{
				ChannelID: config.ChannelId, ScheduleEnabled: config.ScheduleEnabled, Revision: config.Revision,
				CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
			}
		}
		activeRun, hasActiveRun := activeRunByChannel[channel.Id]
		if logicalID != int64(channel.Id) {
			if sharedRun, ok := activeRunByLogical[logicalID]; ok {
				activeRun, hasActiveRun = sharedRun, true
			}
		}
		if hasActiveRun {
			progress := channelModelDetectionRunProgress(activeRun)
			item.ActiveRun = &ChannelModelDetectionActiveRunResponse{
				RunID: activeRun.RunId, Status: activeRun.Status, Trigger: activeRun.Trigger,
				Preset: activeRun.Preset, PresetSource: activeRun.PresetSource, Progress: progress,
				QueuedAt: activeRun.QueuedAt, StartedAt: activeRun.StartedAt, UpdatedAt: activeRun.UpdatedAt,
			}
			response.Detector.Busy = true
		}
		latestRun, hasLatestRun := latestRunByChannel[channel.Id]
		if logicalID != int64(channel.Id) {
			if sharedRun, ok := latestRunByLogical[logicalID]; ok {
				latestRun, hasLatestRun = sharedRun, true
			}
		}
		if hasLatestRun {
			physicalEvents := make([]model.ChannelModelDetectionCostEvent, 0, len(eventsByRun[latestRun.RunId]))
			for _, event := range eventsByRun[latestRun.RunId] {
				if event.ChannelId == channel.Id {
					physicalEvents = append(physicalEvents, event)
				}
			}
			if len(physicalEvents) > 0 || logicalID == int64(channel.Id) {
				aggregate, aggregateErr := aggregateChannelModelDetectionCostForPhysicalView(
					latestRun, physicalEvents, eventsByRun[latestRun.RunId], logicalID != int64(channel.Id) && latestRun.LogicalRevision > 0,
				)
				if aggregateErr != nil {
					return ChannelModelDetectionOverviewResponse{}, aggregateErr
				}
				cost := channelModelDetectionCostResponse(aggregate)
				item.LatestRunCost = &cost
			}
		}

		channelTargets := targetsByChannel[channel.Id]
		if logicalID != int64(channel.Id) {
			channelTargets = append([]model.ChannelModelDetectionTarget(nil), targetsByLogical[logicalID]...)
			for index := range channelTargets {
				channelTargets[index].ChannelId = channel.Id
			}
		}
		for j := range channelTargets {
			target := channelTargets[j]
			targetItem := ChannelModelDetectionTargetSummary{
				TargetKey: target.TargetKey, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel,
				Enabled: target.Enabled, Position: target.Position, RecentWindow: []ChannelModelDetectionResultBucket{},
			}
			if target.Enabled {
				modelSet[target.RequestModel] = struct{}{}
				for _, group := range groups {
					modelsByGroupSet[group][target.RequestModel] = struct{}{}
				}
			}
			targetExecutions := executionsByTarget[target.TargetKey]
			targetItem.RecentWindow = channelModelDetectionResultBuckets(now, displayValue, displayUnit, targetExecutions)
			if len(targetExecutions) > 0 {
				executionRow := targetExecutions[len(targetExecutions)-1]
				aggregate, aggregateErr := aggregateChannelModelDetectionCostForExecution(executionRow.ChannelModelDetectionExecution, eventsByExecution[executionRow.Id])
				if aggregateErr != nil {
					return ChannelModelDetectionOverviewResponse{}, aggregateErr
				}
				summary := channelModelDetectionExecutionSummary(executionRow.ChannelModelDetectionExecution, executionRow.RunTrigger, executionRow.RunPresetSource, aggregate)
				targetItem.Latest = &summary
			}
			item.Targets = append(item.Targets, targetItem)
		}
		item.HealthStatus = channelModelDetectionChannelHealth(now, global, configured, hasConfig, hasActiveRun, item.Targets)
		response.Summary[item.HealthStatus]++
		response.Channels = append(response.Channels, item)
	}
	response.Groups = sortedChannelModelDetectionSet(groupSet)
	response.Models = sortedChannelModelDetectionSet(modelSet)
	for group, values := range modelsByGroupSet {
		response.ModelsByGroup[group] = sortedChannelModelDetectionSet(values)
	}
	return response, nil
}

func normalizeChannelModelDetectionRunHistoryQuery(input ChannelModelDetectionRunHistoryQuery) (ChannelModelDetectionRunHistoryQuery, error) {
	input.Trigger = strings.ToLower(strings.TrimSpace(input.Trigger))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.Model = strings.TrimSpace(input.Model)
	input.Outcome = strings.TrimSpace(input.Outcome)
	if input.Page < 1 {
		return ChannelModelDetectionRunHistoryQuery{}, fmt.Errorf("%w: 页码必须为正整数", ErrChannelModelDetectionInvalidHistoryQuery)
	}
	if input.PageSize < 1 || input.PageSize > ChannelModelDetectionHistoryMaxPageSize {
		return ChannelModelDetectionRunHistoryQuery{}, fmt.Errorf("%w: 每页数量必须在 1 到 %d 之间", ErrChannelModelDetectionInvalidHistoryQuery, ChannelModelDetectionHistoryMaxPageSize)
	}
	if input.Trigger != "" && !model.IsChannelModelDetectionTrigger(input.Trigger) {
		return ChannelModelDetectionRunHistoryQuery{}, fmt.Errorf("%w: 触发方式无效", ErrChannelModelDetectionInvalidHistoryQuery)
	}
	if input.Status != "" {
		if _, ok := channelModelDetectionRunStatuses[input.Status]; !ok {
			return ChannelModelDetectionRunHistoryQuery{}, fmt.Errorf("%w: 轮次状态无效", ErrChannelModelDetectionInvalidHistoryQuery)
		}
	}
	if len(input.Model) > 255 {
		return ChannelModelDetectionRunHistoryQuery{}, fmt.Errorf("%w: 模型名称过长", ErrChannelModelDetectionInvalidHistoryQuery)
	}
	if input.Outcome != "" {
		if _, ok := channelModelDetectionKnownOutcomes[input.Outcome]; !ok {
			return ChannelModelDetectionRunHistoryQuery{}, fmt.Errorf("%w: 检测结论无效", ErrChannelModelDetectionInvalidHistoryQuery)
		}
	}
	return input, nil
}

func queryChannelModelDetectionCostEvents(db *gorm.DB, runIDs []string, executionIDs []int64) (map[string][]model.ChannelModelDetectionCostEvent, map[int64][]model.ChannelModelDetectionCostEvent, error) {
	var events []model.ChannelModelDetectionCostEvent
	query := db.Model(&model.ChannelModelDetectionCostEvent{})
	switch {
	case len(runIDs) > 0 && len(executionIDs) > 0:
		query = query.Where("run_id IN ? OR execution_id IN ?", runIDs, executionIDs)
	case len(runIDs) > 0:
		query = query.Where("run_id IN ?", runIDs)
	case len(executionIDs) > 0:
		query = query.Where("execution_id IN ?", executionIDs)
	default:
		query = query.Where("1 = 0")
	}
	if err := query.Order("id ASC").Find(&events).Error; err != nil {
		return nil, nil, err
	}
	byRun := make(map[string][]model.ChannelModelDetectionCostEvent)
	byExecution := make(map[int64][]model.ChannelModelDetectionCostEvent)
	for i := range events {
		event := events[i]
		byRun[event.RunId] = append(byRun[event.RunId], event)
		byExecution[event.ExecutionId] = append(byExecution[event.ExecutionId], event)
	}
	return byRun, byExecution, nil
}

func aggregateChannelModelDetectionCostForRun(run model.ChannelModelDetectionRun, events []model.ChannelModelDetectionCostEvent) (ChannelModelDetectionCostAggregate, error) {
	if len(events) > 0 {
		return aggregateChannelModelDetectionCostEventList(events)
	}
	return channelModelDetectionStoredCostAggregate(
		run.EstimatedQuota, run.EstimatedCostNanoCNY, run.CostEstimateUnknownCount,
		run.SettledQuota, run.CostBasisQuota, run.SettledCostNanoCNY,
		run.UnresolvedCostNanoCNY, run.UnresolvedCostUnknownCount,
		run.SettledRequestCount, run.UnresolvedRequestCount,
	)
}

// aggregateChannelModelDetectionCostForPhysicalView prevents a shared run's
// stored logical cost from being projected onto a physical member that did
// not produce any cost event. The logical run remains visible, but billing
// stays attached to the actual member channel.
func aggregateChannelModelDetectionCostForPhysicalView(
	run model.ChannelModelDetectionRun,
	physicalEvents []model.ChannelModelDetectionCostEvent,
	allRunEvents []model.ChannelModelDetectionCostEvent,
	shared bool,
) (ChannelModelDetectionCostAggregate, error) {
	if shared && len(physicalEvents) == 0 && len(allRunEvents) > 0 {
		zero := int64(0)
		return ChannelModelDetectionCostAggregate{
			Status:                ChannelModelDetectionCostStatusNotStarted,
			SettledCostNanoCNY:    &zero,
			UnresolvedCostNanoCNY: &zero,
		}, nil
	}
	return aggregateChannelModelDetectionCostForRun(run, physicalEvents)
}

func aggregateChannelModelDetectionCostForExecution(execution model.ChannelModelDetectionExecution, events []model.ChannelModelDetectionCostEvent) (ChannelModelDetectionCostAggregate, error) {
	if len(events) > 0 {
		return aggregateChannelModelDetectionCostEventList(events)
	}
	aggregate, err := channelModelDetectionStoredCostAggregate(
		execution.EstimatedQuota, execution.EstimatedCostNanoCNY, execution.CostEstimateUnknownCount,
		execution.SettledQuota, execution.CostBasisQuota, execution.SettledCostNanoCNY,
		execution.UnresolvedCostNanoCNY, execution.UnresolvedCostUnknownCount,
		execution.SettledRequestCount, execution.UnresolvedRequestCount,
	)
	if err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	aggregate.InputTokens = execution.InputTokens
	aggregate.OutputTokens = execution.OutputTokens
	aggregate.TotalTokens = execution.TotalTokens
	aggregate.UsageAvailable = execution.UsageAvailable
	return aggregate, nil
}

func channelModelDetectionStoredCostAggregate(estimatedQuota int64, estimatedCost *int64, estimateUnknown int64, settledQuota int64, costBasisQuota int64, settledCost *int64, unresolvedCost *int64, unresolvedUnknown int64, settledRequests int64, unresolvedRequests int64) (ChannelModelDetectionCostAggregate, error) {
	aggregate := ChannelModelDetectionCostAggregate{
		EstimatedQuota: estimatedQuota, EstimatedCostNanoCNY: estimatedCost, CostEstimateUnknownCount: estimateUnknown,
		SettledQuota: settledQuota, CostBasisQuota: costBasisQuota, SettledCostNanoCNY: settledCost,
		UnresolvedCostNanoCNY: unresolvedCost, UnresolvedCostUnknownCount: unresolvedUnknown,
		SettledRequestCount: settledRequests, UnresolvedRequestCount: unresolvedRequests,
		Status: ChannelModelDetectionCostStatusNotStarted,
	}
	switch {
	case settledRequests > 0 && unresolvedRequests > 0:
		aggregate.Status = ChannelModelDetectionCostStatusPartial
	case unresolvedRequests > 0:
		aggregate.Status = ChannelModelDetectionCostStatusUnresolved
	case settledRequests > 0:
		aggregate.Status = ChannelModelDetectionCostStatusSettled
	}
	if aggregate.SettledCostNanoCNY == nil && settledRequests == 0 {
		zero := int64(0)
		aggregate.SettledCostNanoCNY = &zero
	}
	if aggregate.UnresolvedCostNanoCNY == nil && unresolvedRequests == 0 {
		zero := int64(0)
		aggregate.UnresolvedCostNanoCNY = &zero
	}
	if aggregate.EstimatedCostNanoCNY == nil && estimateUnknown == 0 {
		zero := int64(0)
		aggregate.EstimatedCostNanoCNY = &zero
	}
	if err := ValidateChannelModelDetectionCostAggregate(aggregate); err != nil {
		return ChannelModelDetectionCostAggregate{}, err
	}
	return aggregate, nil
}

func channelModelDetectionCostResponse(aggregate ChannelModelDetectionCostAggregate) ChannelModelDetectionCostResponse {
	return ChannelModelDetectionCostResponse{
		Currency:                 "CNY",
		EstimatedQuota:           nil,
		EstimatedCostNanoCNY:     nil,
		EstimatedCostCNY:         nil,
		CostEstimateUnknownCount: 0,
		SettledQuota:             aggregate.SettledQuota, CostBasisQuota: aggregate.CostBasisQuota,
		SettledCostNanoCNY:         aggregate.SettledCostNanoCNY,
		SettledCostCNY:             FormatChannelModelDetectionCostCNY(aggregate.SettledCostNanoCNY),
		UnresolvedCostNanoCNY:      aggregate.UnresolvedCostNanoCNY,
		UnresolvedCostCNY:          FormatChannelModelDetectionCostCNY(aggregate.UnresolvedCostNanoCNY),
		UnresolvedCostUnknownCount: aggregate.UnresolvedCostUnknownCount,
		SettledRequestCount:        aggregate.SettledRequestCount, UnresolvedRequestCount: aggregate.UnresolvedRequestCount,
		Status: aggregate.Status, CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI,
	}
}

func channelModelDetectionRunSummary(run model.ChannelModelDetectionRun, aggregate ChannelModelDetectionCostAggregate) ChannelModelDetectionRunSummary {
	return ChannelModelDetectionRunSummary{
		RunID: run.RunId, ChannelID: run.ChannelId, Trigger: run.Trigger, Preset: run.Preset,
		PresetSource: run.PresetSource, Status: run.Status, TargetCount: run.TargetCount,
		CompletedTargetCount: run.CompletedTargetCount, Progress: channelModelDetectionRunProgress(run),
		Cost: channelModelDetectionCostResponse(aggregate), QueuedAt: run.QueuedAt, StartedAt: run.StartedAt,
		FinishedAt: run.FinishedAt, UpdatedAt: run.UpdatedAt, CancelRequestedAt: run.CancelRequestedAt,
		ErrorCode: run.ErrorCode, ErrorMessage: run.ErrorMessage, CreatedByUserID: run.CreatedByUserId,
		CreatedByUsername: run.CreatedByUsername, CreatedAt: run.CreatedAt,
	}
}

func channelModelDetectionRunProgress(run model.ChannelModelDetectionRun) ChannelModelDetectionProgressResponse {
	successful := run.CompletedLogicalRequests - run.ErrorCount
	if successful < 0 {
		successful = 0
	}
	return ChannelModelDetectionProgressResponse{
		Planned: run.PlannedLogicalRequests, LogicalCompleted: run.CompletedLogicalRequests,
		Successful: successful, Errors: run.ErrorCount, HTTPAttempts: run.HTTPAttempts, Retries: run.RetryCount,
	}
}

func channelModelDetectionExecutionSummary(execution model.ChannelModelDetectionExecution, trigger string, presetSource string, aggregate ChannelModelDetectionCostAggregate) ChannelModelDetectionExecutionSummary {
	evidence := channelModelDetectionExecutionEvidence(execution)
	successful := int64(0)
	errorsCount := int64(0)
	cancelled := int64(0)
	switch execution.Status {
	case model.ChannelModelDetectionExecutionStatusCompleted:
		successful = execution.CompletedLogicalRequests
	case model.ChannelModelDetectionExecutionStatusFailed:
		errorsCount = 1
	case model.ChannelModelDetectionExecutionStatusCanceled:
		cancelled = 1
	}
	return ChannelModelDetectionExecutionSummary{
		RunID: execution.RunId, TargetKey: execution.TargetKey, Status: execution.Status, ErrorCode: execution.ErrorCode,
		RequestModel: execution.RequestModel, ClaimedModel: execution.ClaimedModel,
		OutcomeCode: execution.OutcomeCode, TitleCN: execution.TitleCN, SubtitleCN: execution.SubtitleCN,
		JuiceVerdictState: evidence.JuiceVerdictState, FingerprintVerdictState: evidence.FingerprintVerdictState,
		FingerprintModel: evidence.FingerprintModel, FingerprintClaimMismatch: evidence.FingerprintClaimMismatch,
		Preset: execution.Preset, PresetSource: presetSource, Trigger: trigger,
		Progress: ChannelModelDetectionProgressResponse{
			Planned: execution.PlannedLogicalRequests, LogicalCompleted: execution.CompletedLogicalRequests,
			Successful: successful, Errors: errorsCount, Cancelled: cancelled,
			HTTPAttempts: execution.HTTPAttempts, Retries: execution.RetryCount,
		},
		Cost: channelModelDetectionCostResponse(aggregate), StartedAt: execution.StartedAt,
		FinishedAt: execution.FinishedAt, UpdatedAt: execution.UpdatedAt,
	}
}

func channelModelDetectionResultBuckets(
	now int64,
	displayValue int,
	displayUnit string,
	executions []channelModelDetectionExecutionOverviewRow,
) []ChannelModelDetectionResultBucket {
	displayValue, displayUnit = model.NormalizeChannelModelDetectionDisplay(displayValue, displayUnit)
	bucketSeconds := model.ChannelModelDetectionDisplayBucketSeconds(displayUnit)
	currentBucket := model.ChannelModelDetectionDisplayBucketStart(now, displayUnit)
	minimumBucket := currentBucket - int64(displayValue-1)*bucketSeconds
	buckets := make([]ChannelModelDetectionResultBucket, displayValue)
	indices := make(map[int64]int, displayValue)
	for index := range buckets {
		startedAt := minimumBucket + int64(index)*bucketSeconds
		buckets[index].StartedAt = startedAt
		indices[startedAt] = index
	}
	for _, row := range executions {
		timestamp := row.StartedAt
		if timestamp <= 0 {
			timestamp = row.CreatedAt
		}
		startedAt := model.ChannelModelDetectionDisplayBucketStart(timestamp, displayUnit)
		index, ok := indices[startedAt]
		if !ok {
			continue
		}
		bucket := &buckets[index]
		bucket.DetectionCount++
		classification := channelModelDetectionBucketClassification(row.ChannelModelDetectionExecution)
		switch classification {
		case channelModelDetectionBucketResultSuccess:
			bucket.Success++
		case channelModelDetectionBucketResultAttention:
			bucket.Attention++
		case channelModelDetectionBucketResultUnhealthy:
			bucket.Unhealthy++
		case channelModelDetectionBucketResultFailed:
			bucket.Failed++
		case channelModelDetectionBucketResultRunning:
			bucket.Running++
		case channelModelDetectionBucketResultInactive:
			bucket.Inactive++
		}
		bucket.Result = channelModelDetectionBucketResult(bucket)
	}
	return buckets
}

func channelModelDetectionBucketClassification(execution model.ChannelModelDetectionExecution) string {
	if execution.ErrorCode == model.ChannelModelDetectionErrorScheduleTimeout {
		return channelModelDetectionBucketResultAttention
	}
	switch execution.Status {
	case model.ChannelModelDetectionExecutionStatusPending,
		model.ChannelModelDetectionExecutionStatusSubmitting,
		model.ChannelModelDetectionExecutionStatusRunning:
		return channelModelDetectionBucketResultRunning
	case model.ChannelModelDetectionExecutionStatusFailed:
		return channelModelDetectionBucketResultFailed
	case model.ChannelModelDetectionExecutionStatusCanceled,
		model.ChannelModelDetectionExecutionStatusSkipped:
		return channelModelDetectionBucketResultInactive
	}
	if channelModelDetectionExecutionEvidence(execution).FingerprintClaimMismatch {
		return channelModelDetectionBucketResultUnhealthy
	}
	switch execution.OutcomeCode {
	case "juice_mismatch_fingerprint_strong", "juice_mismatch_fingerprint_unclear", "possible_non_gpt":
		return channelModelDetectionBucketResultUnhealthy
	case "juice_pass_fingerprint_strong", "juice_pass_fingerprint_unclear":
		return channelModelDetectionBucketResultSuccess
	case "juice_insufficient_fingerprint_strong", "juice_insufficient_fingerprint_unclear":
		return channelModelDetectionBucketResultAttention
	default:
		return channelModelDetectionBucketResultAttention
	}
}

func channelModelDetectionBucketResult(bucket *ChannelModelDetectionResultBucket) string {
	switch {
	case bucket.Unhealthy > 0:
		return channelModelDetectionBucketResultUnhealthy
	case bucket.Failed > 0:
		return channelModelDetectionBucketResultFailed
	case bucket.Attention > 0:
		return channelModelDetectionBucketResultAttention
	case bucket.Running > 0:
		return channelModelDetectionBucketResultRunning
	case bucket.Success > 0:
		return channelModelDetectionBucketResultSuccess
	case bucket.Inactive > 0:
		return channelModelDetectionBucketResultInactive
	default:
		return ""
	}
}

type channelModelDetectionExecutionEvidenceSummary struct {
	JuiceVerdictState        string
	FingerprintVerdictState  string
	FingerprintModel         string
	FingerprintClaimMismatch bool
}

func channelModelDetectionExecutionEvidence(execution model.ChannelModelDetectionExecution) channelModelDetectionExecutionEvidenceSummary {
	evidence := channelModelDetectionExecutionEvidenceSummary{
		JuiceVerdictState:       strings.TrimSpace(execution.JuiceVerdictState),
		FingerprintVerdictState: strings.TrimSpace(execution.FingerprintVerdictState),
		FingerprintModel:        strings.TrimSpace(execution.FingerprintModel),
	}
	explicitMismatch := false
	if execution.OutcomeCode == "juice_pass_fingerprint_strong" && strings.TrimSpace(string(execution.ReportJSON)) != "" &&
		(evidence.JuiceVerdictState == "" || evidence.FingerprintVerdictState == "" || evidence.FingerprintModel == "") {
		var report struct {
			JuiceVerdictState        string `json:"juice_verdict_state"`
			FingerprintVerdictState  string `json:"fingerprint_verdict_state"`
			FingerprintModel         string `json:"fingerprint_model"`
			FingerprintClaimMismatch *bool  `json:"fingerprint_claim_mismatch"`
		}
		if common.UnmarshalJsonStr(string(execution.ReportJSON), &report) == nil {
			if evidence.JuiceVerdictState == "" {
				evidence.JuiceVerdictState = strings.TrimSpace(report.JuiceVerdictState)
			}
			if evidence.FingerprintVerdictState == "" {
				evidence.FingerprintVerdictState = strings.TrimSpace(report.FingerprintVerdictState)
			}
			if evidence.FingerprintModel == "" {
				evidence.FingerprintModel = strings.TrimSpace(report.FingerprintModel)
			}
			explicitMismatch = report.FingerprintClaimMismatch != nil && *report.FingerprintClaimMismatch
		}
	}
	evidence.FingerprintClaimMismatch = explicitMismatch ||
		execution.OutcomeCode == "juice_pass_fingerprint_strong" &&
			evidence.FingerprintModel != "" && execution.ClaimedModel != "" &&
			!strings.EqualFold(evidence.FingerprintModel, execution.ClaimedModel)
	return evidence
}

func channelModelDetectionChannelHealth(now int64, global model.ChannelModelDetectionGlobalConfig, detectorConfigured bool, hasConfig bool, hasActiveRun bool, targets []ChannelModelDetectionTargetSummary) string {
	enabledTargets := make([]ChannelModelDetectionTargetSummary, 0, len(targets))
	for i := range targets {
		if targets[i].Enabled {
			enabledTargets = append(enabledTargets, targets[i])
		}
	}
	if !hasConfig || len(enabledTargets) == 0 {
		return channelModelDetectionHealthUnconfigured
	}
	if hasActiveRun {
		return channelModelDetectionHealthRunning
	}
	hasPending := false
	hasAttention := false
	latestAt := int64(0)
	for i := range enabledTargets {
		latest := enabledTargets[i].Latest
		if latest == nil || latest.Status != model.ChannelModelDetectionExecutionStatusCompleted || latest.OutcomeCode == "" {
			hasPending = true
			if latest != nil && (latest.Status == model.ChannelModelDetectionExecutionStatusFailed || latest.Status == model.ChannelModelDetectionExecutionStatusCanceled) {
				hasAttention = true
			}
			continue
		}
		if latest.FinishedAt > latestAt {
			latestAt = latest.FinishedAt
		}
		if latest.UpdatedAt > latestAt {
			latestAt = latest.UpdatedAt
		}
		if latest.FingerprintClaimMismatch {
			return channelModelDetectionHealthUnhealthy
		}
		switch latest.OutcomeCode {
		case "juice_mismatch_fingerprint_strong", "juice_mismatch_fingerprint_unclear", "possible_non_gpt":
			return channelModelDetectionHealthUnhealthy
		case "juice_insufficient_fingerprint_strong", "juice_insufficient_fingerprint_unclear":
			hasAttention = true
		case "juice_pass_fingerprint_strong", "juice_pass_fingerprint_unclear":
		default:
			hasAttention = true
		}
	}
	if hasAttention {
		return channelModelDetectionHealthAttention
	}
	if !detectorConfigured {
		return channelModelDetectionHealthDetectorUnavailable
	}
	staleSeconds := int64(global.EffectiveIntervalMinutes()) * 2 * 60
	if staleSeconds < 48*60*60 {
		staleSeconds = 48 * 60 * 60
	}
	if !hasPending && latestAt > 0 && now-latestAt > staleSeconds {
		return channelModelDetectionHealthStale
	}
	if !hasPending {
		return channelModelDetectionHealthHealthy
	}
	return channelModelDetectionHealthPending
}

func applyChannelModelDetectionGlobalDefaults(config *model.ChannelModelDetectionGlobalConfig) {
	if config.ScheduledPreset == "" {
		config.ScheduledPreset = model.ChannelModelDetectionPresetMedium
	}
	if config.IntervalMinutes <= 0 {
		config.IntervalMinutes = config.EffectiveIntervalMinutes()
	}
	if config.DisplayValue == 0 && strings.TrimSpace(config.DisplayUnit) == "" {
		config.DisplayValue = model.ChannelModelDetectionDefaultDisplayValue
		config.DisplayUnit = model.ChannelModelDetectionDefaultDisplayUnit
	}
}

func channelModelDetectionRunIsNewer(left model.ChannelModelDetectionRun, right model.ChannelModelDetectionRun) bool {
	return left.CreatedAt > right.CreatedAt || left.CreatedAt == right.CreatedAt && left.Id > right.Id
}

func splitChannelModelDetectionList(value string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func sortedChannelModelDetectionSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func maskChannelModelDetectorURL(raw string) string {
	// The detector address is an administrator-managed setting, so the UI
	// should show the same value that is configured instead of a lossy mask.
	return strings.TrimSpace(raw)
}

func redactChannelModelDetectionReportSecrets(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalizedKey := strings.Map(func(r rune) rune {
				if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, strings.ToLower(key))
			if isChannelModelDetectionSensitiveReportKey(normalizedKey) {
				result[key] = "[已隐藏]"
			} else {
				result[key] = redactChannelModelDetectionReportSecrets(item)
			}
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i := range typed {
			result[i] = redactChannelModelDetectionReportSecrets(typed[i])
		}
		return result
	default:
		return value
	}
}

func isChannelModelDetectionSensitiveReportKey(normalizedKey string) bool {
	if normalizedKey == "key" || normalizedKey == "token" || normalizedKey == "authorization" || normalizedKey == "secret" || normalizedKey == "password" {
		return true
	}
	for _, marker := range []string{
		"apikey", "accesstoken", "sessiontoken", "proxytoken", "tasktoken", "bearertoken",
		"authorization", "credential", "password", "secret", "taskbearer", "detectorurl", "detectoraddress",
	} {
		if strings.Contains(normalizedKey, marker) {
			return true
		}
	}
	return false
}

func channelModelDetectionQueryDB(ctx context.Context, tx *gorm.DB) (*gorm.DB, error) {
	if tx == nil {
		tx = model.DB
	}
	if tx == nil {
		return nil, errors.New("模型检测查询数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return tx.WithContext(ctx), nil
}
