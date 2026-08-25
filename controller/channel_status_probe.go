package controller

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	channelStatusProbeHealthUnconfigured = "unconfigured"
	channelStatusProbeHealthPaused       = "paused"
	channelStatusProbeHealthPending      = "pending"
	channelStatusProbeHealthHealthy      = "healthy"
	channelStatusProbeHealthPartial      = "partial"
	channelStatusProbeHealthUnhealthy    = "unhealthy"
	channelStatusProbeHealthRateLimited  = "rate_limited"
	channelStatusProbeHealthStale        = "stale"
)

type channelStatusProbeConfigResponse struct {
	Id                int64    `json:"id"`
	ChannelId         int      `json:"channel_id"`
	Enabled           bool     `json:"enabled"`
	Models            []string `json:"models"`
	IntervalSeconds   int      `json:"interval_seconds"`
	DisplayValue      int      `json:"display_value"`
	DisplayUnit       string   `json:"display_unit"`
	RecordSample      bool     `json:"record_sample"`
	NextRunAt         int64    `json:"next_run_at"`
	ManualRequestId   string   `json:"manual_request_id"`
	ManualRequestedAt int64    `json:"manual_requested_at"`
	Revision          int64    `json:"revision"`
	RunningTrigger    string   `json:"running_trigger"`
	RunningRunId      string   `json:"running_run_id"`
	RunningStartedAt  int64    `json:"running_started_at"`
	CreatedAt         int64    `json:"created_at"`
	UpdatedAt         int64    `json:"updated_at"`
}

type channelStatusProbeBucketResponse struct {
	StartedAt               int64    `json:"started_at"`
	Success                 int      `json:"success"`
	UpstreamFailure         int      `json:"upstream_failure"`
	RateLimited             int      `json:"rate_limited"`
	LocalFailure            int      `json:"local_failure"`
	Skipped                 int      `json:"skipped"`
	Canceled                int      `json:"canceled"`
	Models                  []string `json:"models,omitempty"`
	Result                  string   `json:"result"`
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

type channelStatusProbeModelResponse struct {
	ModelName       string                             `json:"model_name"`
	HealthStatus    string                             `json:"health_status"`
	Latest          *model.ChannelStatusProbeState     `json:"latest"`
	RecentWindow    []channelStatusProbeBucketResponse `json:"recent_window"`
	AvgFirstTokenMs *float64                           `json:"avg_first_token_ms"`
	AvgTPS          *float64                           `json:"avg_tps"`
}

type channelStatusProbeChannelResponse struct {
	Id                 int                               `json:"id"`
	Name               string                            `json:"name"`
	Type               int                               `json:"type"`
	ChannelStatus      int                               `json:"channel_status"`
	Remark             string                            `json:"remark"`
	Groups             []string                          `json:"groups"`
	CostRatio          *float64                          `json:"cost_ratio"`
	SupportedModels    []string                          `json:"supported_models"`
	AllowsCustomModel  bool                              `json:"allows_custom_model"`
	Config             *channelStatusProbeConfigResponse `json:"config"`
	HealthStatus       string                            `json:"health_status"`
	Running            bool                              `json:"running"`
	Latest             *model.ChannelStatusProbeState    `json:"latest"`
	TodayProbeCostCNY  float64                           `json:"today_probe_cost_cny"`
	AvgFirstTokenMs    *float64                          `json:"avg_first_token_ms"`
	AvgTPS             *float64                          `json:"avg_tps"`
	ModelStatuses      []channelStatusProbeModelResponse `json:"model_statuses"`
	ConfiguredModelNum int                               `json:"configured_model_count"`
}

type channelStatusProbeOverviewResponse struct {
	ServerNow          int64                               `json:"server_now"`
	SnapshotVersion    int                                 `json:"snapshot_version"`
	SnapshotRevision   uint64                              `json:"snapshot_revision"`
	EventWatermark     uint64                              `json:"event_watermark"`
	GeneratedAt        int64                               `json:"generated_at"`
	SnapshotAgeSeconds int64                               `json:"snapshot_age_seconds"`
	Stale              bool                                `json:"stale"`
	ScanIntervalSecond int                                 `json:"scan_interval_seconds"`
	Summary            map[string]int                      `json:"summary"`
	Groups             []string                            `json:"groups"`
	Models             []string                            `json:"models"`
	ModelsByGroup      map[string][]string                 `json:"models_by_group"`
	Channels           []channelStatusProbeChannelResponse `json:"channels"`
}

type channelStatusProbeConfigRequest struct {
	Enabled         *bool     `json:"enabled"`
	Models          *[]string `json:"models"`
	IntervalSeconds *int      `json:"interval_seconds"`
	DisplayValue    *int      `json:"display_value"`
	DisplayUnit     *string   `json:"display_unit"`
	RecordSample    *bool     `json:"record_sample"`
	Revision        *int64    `json:"revision"`
}

func channelStatusProbeConfigToResponse(config model.ChannelStatusProbeConfig) (channelStatusProbeConfigResponse, error) {
	models, err := config.Models()
	if err != nil {
		return channelStatusProbeConfigResponse{}, err
	}
	displayValue, displayUnit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
	return channelStatusProbeConfigResponse{
		Id: config.Id, ChannelId: config.ChannelId, Enabled: config.Enabled, Models: models,
		IntervalSeconds: config.IntervalSeconds,
		DisplayValue:    displayValue,
		DisplayUnit:     displayUnit,
		RecordSample:    config.RecordSample,
		NextRunAt:       config.NextRunAt, ManualRequestId: config.ManualRequestId,
		ManualRequestedAt: config.ManualRequestedAt, Revision: config.Revision,
		RunningTrigger: config.RunningTrigger, RunningRunId: config.RunningRunId,
		RunningStartedAt: config.RunningStartedAt, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
	}, nil
}

func normalizeChannelStatusProbeModels(channel *model.Channel, rawModels []string) ([]string, error) {
	if channel == nil {
		return nil, errors.New("渠道不存在")
	}
	if len(rawModels) == 0 || len(rawModels) > model.ChannelStatusProbeMaxModels {
		return nil, errors.New("探测模型数量必须在 1 到 20 之间")
	}
	supportedModels := channel.GetModels()
	seen := make(map[string]struct{}, len(rawModels))
	models := make([]string, 0, len(rawModels))
	for _, rawModel := range rawModels {
		modelName := strings.TrimSpace(rawModel)
		if modelName == "" || utf8.RuneCountInString(modelName) > 255 || strings.Contains(modelName, "*") {
			return nil, errors.New("探测模型必须是长度不超过 255 的具体模型名称")
		}
		if _, exists := seen[modelName]; exists {
			continue
		}
		matched := false
		normalizedModel := ratio_setting.FormatMatchingModelName(modelName)
		for _, supported := range supportedModels {
			supported = strings.TrimSpace(supported)
			if supported == modelName || supported == "*" || supported == normalizedModel ||
				(strings.HasSuffix(supported, "*") && strings.HasPrefix(modelName, strings.TrimSuffix(supported, "*"))) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, errors.New("模型 " + modelName + " 不在该渠道支持范围内")
		}
		if !channelSmartScheduleSupportsTextProbe(channel, modelName) {
			return nil, errors.New("模型 " + modelName + " 不支持自动文本探测")
		}
		seen[modelName] = struct{}{}
		models = append(models, modelName)
	}
	if len(models) == 0 {
		return nil, errors.New("至少需要一个探测模型")
	}
	return models, nil
}

func normalizeLogicalChannelStatusProbeModels(memberChannels map[int]*model.Channel, rawModels []string) ([]string, error) {
	if len(rawModels) == 0 || len(rawModels) > model.ChannelStatusProbeMaxModels {
		return nil, errors.New("探测模型数量必须在 1 到 20 之间")
	}
	memberIDs := make([]int, 0, len(memberChannels))
	for channelID := range memberChannels {
		memberIDs = append(memberIDs, channelID)
	}
	sort.Ints(memberIDs)
	seen := make(map[string]struct{}, len(rawModels))
	models := make([]string, 0, len(rawModels))
	for _, rawModel := range rawModels {
		modelName := strings.TrimSpace(rawModel)
		if modelName == "" || utf8.RuneCountInString(modelName) > 255 || strings.Contains(modelName, "*") {
			return nil, errors.New("探测模型必须是长度不超过 255 的具体模型名称")
		}
		if _, exists := seen[modelName]; exists {
			continue
		}
		supported := false
		for _, channelID := range memberIDs {
			if _, err := normalizeChannelStatusProbeModels(memberChannels[channelID], []string{modelName}); err == nil {
				supported = true
				break
			}
		}
		if !supported {
			return nil, errors.New("模型 " + modelName + " 不在逻辑渠道组任一成员支持范围内")
		}
		seen[modelName] = struct{}{}
		models = append(models, modelName)
	}
	return models, nil
}

func channelStatusProbeHealth(
	config *channelStatusProbeConfigResponse,
	states map[string]model.ChannelStatusProbeState,
	now int64,
) string {
	if config == nil || len(config.Models) == 0 {
		return channelStatusProbeHealthUnconfigured
	}
	if !config.Enabled {
		return channelStatusProbeHealthPaused
	}
	staleBefore := now - int64(config.IntervalSeconds*2)
	healthy := 0
	failed := 0
	rateLimited := 0
	stale := 0
	unknown := 0
	localFailure := 0
	for _, modelName := range config.Models {
		state, exists := states[modelName]
		if !exists || state.LastHealthFinishedAt <= 0 {
			if exists && state.Result == model.ChannelStatusProbeResultLocalFailure {
				localFailure++
			} else {
				unknown++
			}
			continue
		}
		if state.LastHealthFinishedAt < staleBefore {
			stale++
			continue
		}
		switch state.LastHealthResult {
		case model.ChannelStatusProbeResultSuccess:
			healthy++
		case model.ChannelStatusProbeResultRateLimited:
			rateLimited++
		case model.ChannelStatusProbeResultUpstreamFailure:
			failed++
		default:
			unknown++
		}
		if state.Result == model.ChannelStatusProbeResultLocalFailure && state.FinishedAt > state.LastHealthFinishedAt {
			localFailure++
		}
	}
	if failed+rateLimited > 0 {
		if healthy+stale+unknown+localFailure > 0 {
			return channelStatusProbeHealthPartial
		}
		if failed > 0 {
			return channelStatusProbeHealthUnhealthy
		}
		return channelStatusProbeHealthRateLimited
	}
	if localFailure > 0 {
		return channelStatusProbeHealthPartial
	}
	if stale > 0 {
		if healthy+unknown > 0 {
			return channelStatusProbeHealthPartial
		}
		return channelStatusProbeHealthStale
	}
	if unknown > 0 {
		if healthy > 0 {
			return channelStatusProbeHealthPartial
		}
		return channelStatusProbeHealthPending
	}
	if healthy > 0 {
		return channelStatusProbeHealthHealthy
	}
	return channelStatusProbeHealthPending
}

func channelStatusProbeBucketResult(bucket model.ChannelStatusProbeBucket) string {
	switch {
	case bucket.UpstreamFailure > 0:
		return model.ChannelStatusProbeResultUpstreamFailure
	case bucket.RateLimited > 0:
		return model.ChannelStatusProbeResultRateLimited
	case bucket.LocalFailure > 0:
		return model.ChannelStatusProbeResultLocalFailure
	case bucket.Canceled > 0:
		return model.ChannelStatusProbeResultCanceled
	case bucket.Skipped > 0:
		return model.ChannelStatusProbeResultSkipped
	case bucket.Success > 0:
		return model.ChannelStatusProbeResultSuccess
	default:
		return ""
	}
}

type channelStatusProbeWindowSummary struct {
	Buckets                 []channelStatusProbeBucketResponse
	FirstTokenTotalMs       float64
	FirstTokenSampleCount   int64
	TPSTotal                float64
	TPSSampleCount          int64
	ResponseTimeTotalMs     float64
	ResponseTimeSampleCount int64
}

func channelStatusProbeBucketResponseFromModelBucket(
	bucket model.ChannelStatusProbeBucket,
) channelStatusProbeBucketResponse {
	return channelStatusProbeBucketResponse{
		StartedAt: bucket.StartedAt, Success: bucket.Success,
		UpstreamFailure: bucket.UpstreamFailure, RateLimited: bucket.RateLimited,
		LocalFailure: bucket.LocalFailure, Skipped: bucket.Skipped, Canceled: bucket.Canceled,
		Models: bucket.Models, Result: channelStatusProbeBucketResult(bucket),
		FirstTokenTotalMs: bucket.FirstTokenTotalMs, FirstTokenSampleCount: bucket.FirstTokenSampleCount,
		TPSTotal: bucket.TPSTotal, TPSSampleCount: bucket.TPSSampleCount,
		ResponseTimeTotalMs: bucket.ResponseTimeTotalMs, ResponseTimeSampleCount: bucket.ResponseTimeSampleCount,
		LatestExecutionId: bucket.LatestExecutionId, LatestFinishedAt: bucket.LatestFinishedAt,
		LatestResult: bucket.LatestResult, LatestModelName: bucket.LatestModelName,
		LatestFirstTokenMs: bucket.LatestFirstTokenMs, LatestTPS: bucket.LatestTPS,
		LatestResponseTimeMs: bucket.LatestResponseTimeMs,
	}
}

func mergeChannelStatusProbeRecentWindow(
	states []model.ChannelStatusProbeState,
	now int64,
	displayValue int,
	displayUnit string,
) (channelStatusProbeWindowSummary, error) {
	displayValue, displayUnit = model.NormalizeChannelStatusProbeDisplay(displayValue, displayUnit)
	bucketSeconds := model.ChannelStatusProbeDisplayBucketSeconds(displayUnit)
	currentBucket := model.ChannelStatusProbeDisplayBucketStart(now, displayUnit)
	minimumBucket := currentBucket - int64(displayValue-1)*bucketSeconds
	merged := make(map[int64]model.ChannelStatusProbeBucket, displayValue)
	for _, state := range states {
		buckets, err := state.Buckets(displayUnit)
		if err != nil {
			return channelStatusProbeWindowSummary{}, err
		}
		for _, bucket := range buckets {
			if bucket.StartedAt < minimumBucket || bucket.StartedAt > currentBucket {
				continue
			}
			current := merged[bucket.StartedAt]
			current.StartedAt = bucket.StartedAt
			current.Success += bucket.Success
			current.UpstreamFailure += bucket.UpstreamFailure
			current.RateLimited += bucket.RateLimited
			current.LocalFailure += bucket.LocalFailure
			current.Skipped += bucket.Skipped
			current.Canceled += bucket.Canceled
			current.FirstTokenTotalMs += bucket.FirstTokenTotalMs
			current.FirstTokenSampleCount += bucket.FirstTokenSampleCount
			current.TPSTotal += bucket.TPSTotal
			current.TPSSampleCount += bucket.TPSSampleCount
			current.ResponseTimeTotalMs += bucket.ResponseTimeTotalMs
			current.ResponseTimeSampleCount += bucket.ResponseTimeSampleCount
			if bucket.LatestResult != "" &&
				(current.LatestResult == "" || bucket.LatestFinishedAt > current.LatestFinishedAt ||
					(bucket.LatestFinishedAt == current.LatestFinishedAt && bucket.LatestExecutionId > current.LatestExecutionId)) {
				current.LatestExecutionId = bucket.LatestExecutionId
				current.LatestFinishedAt = bucket.LatestFinishedAt
				current.LatestResult = bucket.LatestResult
				current.LatestModelName = bucket.LatestModelName
				current.LatestFirstTokenMs = bucket.LatestFirstTokenMs
				current.LatestTPS = bucket.LatestTPS
				current.LatestResponseTimeMs = bucket.LatestResponseTimeMs
			}
			for _, modelName := range bucket.Models {
				current.Add("", modelName, nil, nil, nil)
			}
			merged[bucket.StartedAt] = current
		}
	}
	summary := channelStatusProbeWindowSummary{
		Buckets: make([]channelStatusProbeBucketResponse, 0, displayValue),
	}
	for startedAt := minimumBucket; startedAt <= currentBucket; startedAt += bucketSeconds {
		bucket := merged[startedAt]
		bucket.StartedAt = startedAt
		summary.Buckets = append(summary.Buckets, channelStatusProbeBucketResponse{
			StartedAt: bucket.StartedAt, Success: bucket.Success,
			UpstreamFailure: bucket.UpstreamFailure, RateLimited: bucket.RateLimited,
			LocalFailure: bucket.LocalFailure, Skipped: bucket.Skipped, Canceled: bucket.Canceled,
			Models: bucket.Models, Result: channelStatusProbeBucketResult(bucket),
			FirstTokenTotalMs: bucket.FirstTokenTotalMs, FirstTokenSampleCount: bucket.FirstTokenSampleCount,
			TPSTotal: bucket.TPSTotal, TPSSampleCount: bucket.TPSSampleCount,
			ResponseTimeTotalMs: bucket.ResponseTimeTotalMs, ResponseTimeSampleCount: bucket.ResponseTimeSampleCount,
			LatestExecutionId: bucket.LatestExecutionId, LatestFinishedAt: bucket.LatestFinishedAt,
			LatestResult: bucket.LatestResult, LatestModelName: bucket.LatestModelName,
			LatestFirstTokenMs: bucket.LatestFirstTokenMs, LatestTPS: bucket.LatestTPS,
			LatestResponseTimeMs: bucket.LatestResponseTimeMs,
		})
		summary.FirstTokenTotalMs += bucket.FirstTokenTotalMs
		summary.FirstTokenSampleCount += bucket.FirstTokenSampleCount
		summary.TPSTotal += bucket.TPSTotal
		summary.TPSSampleCount += bucket.TPSSampleCount
		summary.ResponseTimeTotalMs += bucket.ResponseTimeTotalMs
		summary.ResponseTimeSampleCount += bucket.ResponseTimeSampleCount
	}
	return summary, nil
}

func mergeChannelStatusProbeExecutionRecentWindow(
	executions []model.ChannelStatusProbeExecution,
	now int64,
	displayValue int,
	displayUnit string,
) channelStatusProbeWindowSummary {
	displayValue, displayUnit = model.NormalizeChannelStatusProbeDisplay(displayValue, displayUnit)
	bucketSeconds := model.ChannelStatusProbeDisplayBucketSeconds(displayUnit)
	currentBucket := model.ChannelStatusProbeDisplayBucketStart(now, displayUnit)
	minimumBucket := currentBucket - int64(displayValue-1)*bucketSeconds
	bucketsByStart := make(map[int64]model.ChannelStatusProbeBucket, displayValue)
	latestTimestamps := make(map[int64]int64, displayValue)
	for _, execution := range executions {
		if execution.FinishedAt > now {
			continue
		}
		timestamp := model.ChannelStatusProbeExecutionBucketTimestamp(execution)
		if timestamp <= 0 || timestamp < minimumBucket || timestamp > now {
			continue
		}
		latestTimestamp := execution.FinishedAt
		if latestTimestamp <= 0 {
			latestTimestamp = timestamp
		}
		startedAt := model.ChannelStatusProbeDisplayBucketStart(timestamp, displayUnit)
		if startedAt < minimumBucket || startedAt > currentBucket {
			continue
		}
		bucket := bucketsByStart[startedAt]
		bucket.StartedAt = startedAt
		bucket.Add(execution.Result, execution.ModelName, execution.FirstTokenMs, execution.TPS, execution.ResponseTimeMs)
		bucketsByStart[startedAt] = bucket
		if previousTimestamp, exists := latestTimestamps[startedAt]; !exists || latestTimestamp > previousTimestamp ||
			(latestTimestamp == previousTimestamp && execution.Id > bucket.LatestExecutionId) {
			bucket.LatestExecutionId = execution.Id
			bucket.LatestFinishedAt = execution.FinishedAt
			bucket.LatestResult = execution.Result
			bucket.LatestModelName = execution.ModelName
			bucket.LatestFirstTokenMs = execution.FirstTokenMs
			bucket.LatestTPS = execution.TPS
			bucket.LatestResponseTimeMs = execution.ResponseTimeMs
			bucketsByStart[startedAt] = bucket
			latestTimestamps[startedAt] = latestTimestamp
		}
	}
	summary := channelStatusProbeWindowSummary{Buckets: make([]channelStatusProbeBucketResponse, 0, displayValue)}
	for startedAt := minimumBucket; startedAt <= currentBucket; startedAt += bucketSeconds {
		bucket := bucketsByStart[startedAt]
		bucket.StartedAt = startedAt
		responseBucket := channelStatusProbeBucketResponseFromModelBucket(bucket)
		summary.Buckets = append(summary.Buckets, responseBucket)
		summary.FirstTokenTotalMs += bucket.FirstTokenTotalMs
		summary.FirstTokenSampleCount += bucket.FirstTokenSampleCount
		summary.TPSTotal += bucket.TPSTotal
		summary.TPSSampleCount += bucket.TPSSampleCount
		summary.ResponseTimeTotalMs += bucket.ResponseTimeTotalMs
		summary.ResponseTimeSampleCount += bucket.ResponseTimeSampleCount
	}
	return summary
}

func GetChannelStatusProbeOverview(c *gin.Context) {
	selectedModel := strings.TrimSpace(c.Query("model"))
	response, err := getChannelStatusProbeOverviewCached(selectedModel)
	if err != nil {
		if errors.Is(err, errChannelStatusProbeOverviewSnapshotUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}

func buildChannelStatusProbeOverview(
	selectedModel string,
	now int64,
) (channelStatusProbeOverviewResponse, error) {
	channels, err := model.GetChannelsForStatusProbeOverview()
	if err != nil {
		return channelStatusProbeOverviewResponse{}, err
	}
	configs, err := model.GetChannelStatusProbeConfigs()
	if err != nil {
		return channelStatusProbeOverviewResponse{}, err
	}
	states, err := model.GetChannelStatusProbeStates()
	if err != nil {
		return channelStatusProbeOverviewResponse{}, err
	}
	maxDisplaySeconds := int64(model.ChannelStatusProbeDefaultDisplayValue) *
		model.ChannelStatusProbeDisplayBucketSeconds(model.ChannelStatusProbeDefaultDisplayUnit)
	for _, config := range configs {
		displayValue, displayUnit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
		displaySeconds := int64(displayValue) * model.ChannelStatusProbeDisplayBucketSeconds(displayUnit)
		if displaySeconds > maxDisplaySeconds {
			maxDisplaySeconds = displaySeconds
		}
	}
	recentExecutions, err := model.GetChannelStatusProbeExecutionsSince(now - maxDisplaySeconds)
	if err != nil {
		return channelStatusProbeOverviewResponse{}, err
	}
	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		return channelStatusProbeOverviewResponse{}, err
	}
	todayStart := model.ChannelDailyCostDayStart(now)
	todayCosts, err := model.GetChannelDailyCosts(context.Background(), todayStart, todayStart+channelMonitorCostDaySeconds)
	if err != nil {
		return channelStatusProbeOverviewResponse{}, err
	}

	configByChannel := make(map[int]model.ChannelStatusProbeConfig, len(configs))
	for _, config := range configs {
		configByChannel[config.ChannelId] = config
	}
	statesByChannel := make(map[int][]model.ChannelStatusProbeState)
	for _, state := range states {
		statesByChannel[state.ChannelId] = append(statesByChannel[state.ChannelId], state)
	}
	type executionKey struct {
		channelID int
		modelName string
	}
	executionsByChannelModel := make(map[executionKey][]model.ChannelStatusProbeExecution)
	for _, execution := range recentExecutions {
		key := executionKey{channelID: execution.ChannelId, modelName: execution.ModelName}
		executionsByChannelModel[key] = append(executionsByChannelModel[key], execution)
	}
	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(monitors))
	for _, monitor := range monitors {
		monitorByChannel[monitor.ChannelId] = monitor
	}
	todayProbeCostByChannel := make(map[int]int64, len(todayCosts))
	for _, cost := range todayCosts {
		todayProbeCostByChannel[cost.ChannelId] = cost.ProbeCostNanoCNY
	}
	groupSet := make(map[string]struct{})
	modelSet := make(map[string]struct{})
	modelSetByGroup := make(map[string]map[string]struct{})
	items := make([]channelStatusProbeChannelResponse, 0, len(channels))
	summary := map[string]int{
		channelStatusProbeHealthUnconfigured: 0, channelStatusProbeHealthPaused: 0,
		channelStatusProbeHealthPending: 0, channelStatusProbeHealthHealthy: 0,
		channelStatusProbeHealthPartial: 0, channelStatusProbeHealthUnhealthy: 0,
		channelStatusProbeHealthRateLimited: 0, channelStatusProbeHealthStale: 0,
	}
	for _, channel := range channels {
		channelGroups := channel.GetGroups()
		for _, groupName := range channelGroups {
			if groupName != "" {
				groupSet[groupName] = struct{}{}
			}
		}
		var configResponse *channelStatusProbeConfigResponse
		configuredModels := make(map[string]struct{})
		if config, exists := configByChannel[channel.Id]; exists {
			converted, convertErr := channelStatusProbeConfigToResponse(config)
			if convertErr != nil {
				return channelStatusProbeOverviewResponse{}, convertErr
			}
			configResponse = &converted
			for _, modelName := range converted.Models {
				modelSet[modelName] = struct{}{}
				configuredModels[modelName] = struct{}{}
				for _, groupName := range channelGroups {
					if groupName == "" {
						continue
					}
					groupModels, exists := modelSetByGroup[groupName]
					if !exists {
						groupModels = make(map[string]struct{})
						modelSetByGroup[groupName] = groupModels
					}
					groupModels[modelName] = struct{}{}
				}
			}
		}
		if selectedModel != "" {
			configured := false
			if configResponse != nil {
				for _, modelName := range configResponse.Models {
					if modelName == selectedModel {
						configured = true
						break
					}
				}
			}
			if !configured {
				continue
			}
		}

		stateByModel := make(map[string]model.ChannelStatusProbeState)
		for _, state := range statesByChannel[channel.Id] {
			if _, configured := configuredModels[state.ModelName]; !configured {
				continue
			}
			if selectedModel != "" && state.ModelName != selectedModel {
				continue
			}
			stateByModel[state.ModelName] = state
		}
		visibleModels := make([]string, 0, len(configuredModels))
		if configResponse != nil {
			for _, modelName := range configResponse.Models {
				if selectedModel == "" || modelName == selectedModel {
					visibleModels = append(visibleModels, modelName)
				}
			}
		}
		var latest *model.ChannelStatusProbeState
		displayValue := model.ChannelStatusProbeDefaultDisplayValue
		displayUnit := model.ChannelStatusProbeDefaultDisplayUnit
		if configResponse != nil {
			displayValue = configResponse.DisplayValue
			displayUnit = configResponse.DisplayUnit
		}
		modelStatuses := make([]channelStatusProbeModelResponse, 0, len(visibleModels))
		var firstTokenTotal float64
		var firstTokenSamples int64
		var tpsTotal float64
		var tpsSamples int64
		for _, modelName := range visibleModels {
			state, hasState := stateByModel[modelName]
			var latestForModel *model.ChannelStatusProbeState
			statesForModel := make([]model.ChannelStatusProbeState, 0, 1)
			if hasState {
				stateCopy := state
				latestForModel = &stateCopy
				statesForModel = append(statesForModel, state)
				if latest == nil || state.FinishedAt > latest.FinishedAt ||
					(state.FinishedAt == latest.FinishedAt && state.ExecutionId > latest.ExecutionId) {
					latest = &stateCopy
				}
			}
			var windowSummary channelStatusProbeWindowSummary
			modelExecutions := executionsByChannelModel[executionKey{channelID: channel.Id, modelName: modelName}]
			if len(modelExecutions) > 0 {
				windowSummary = mergeChannelStatusProbeExecutionRecentWindow(
					modelExecutions, now, displayValue, displayUnit,
				)
			} else {
				var mergeErr error
				windowSummary, mergeErr = mergeChannelStatusProbeRecentWindow(
					statesForModel, now, displayValue, displayUnit,
				)
				if mergeErr != nil {
					return channelStatusProbeOverviewResponse{}, mergeErr
				}
			}
			modelConfig := *configResponse
			modelConfig.Models = []string{modelName}
			modelHealth := channelStatusProbeHealth(&modelConfig, stateByModel, now)
			var modelAvgFirstTokenMs *float64
			if windowSummary.FirstTokenSampleCount > 0 {
				value := windowSummary.FirstTokenTotalMs / float64(windowSummary.FirstTokenSampleCount)
				modelAvgFirstTokenMs = &value
			}
			var modelAvgTPS *float64
			if windowSummary.TPSSampleCount > 0 {
				value := windowSummary.TPSTotal / float64(windowSummary.TPSSampleCount)
				modelAvgTPS = &value
			}
			modelStatuses = append(modelStatuses, channelStatusProbeModelResponse{
				ModelName: modelName, HealthStatus: modelHealth, Latest: latestForModel,
				RecentWindow: windowSummary.Buckets, AvgFirstTokenMs: modelAvgFirstTokenMs, AvgTPS: modelAvgTPS,
			})
			if windowSummary.FirstTokenSampleCount > 0 {
				firstTokenTotal += windowSummary.FirstTokenTotalMs
				firstTokenSamples += windowSummary.FirstTokenSampleCount
			}
			if windowSummary.TPSSampleCount > 0 {
				tpsTotal += windowSummary.TPSTotal
				tpsSamples += windowSummary.TPSSampleCount
			}
		}
		var avgFirstTokenMs *float64
		if firstTokenSamples > 0 {
			value := firstTokenTotal / float64(firstTokenSamples)
			avgFirstTokenMs = &value
		}
		var avgTPS *float64
		if tpsSamples > 0 {
			value := tpsTotal / float64(tpsSamples)
			avgTPS = &value
		}
		healthConfig := configResponse
		if selectedModel != "" && configResponse != nil {
			selectedConfig := *configResponse
			selectedConfig.Models = []string{selectedModel}
			healthConfig = &selectedConfig
		}
		health := channelStatusProbeHealth(healthConfig, stateByModel, now)
		summary[health]++
		remark := ""
		if channel.Remark != nil {
			remark = strings.TrimSpace(*channel.Remark)
		}
		supportedModels := make([]string, 0)
		allowsCustomModel := false
		seenSupported := make(map[string]struct{})
		for _, supported := range channel.GetModels() {
			supported = strings.TrimSpace(supported)
			if supported == "" {
				continue
			}
			if strings.Contains(supported, "*") {
				allowsCustomModel = true
			}
			if _, exists := seenSupported[supported]; exists {
				continue
			}
			seenSupported[supported] = struct{}{}
			supportedModels = append(supportedModels, supported)
		}
		sort.Strings(supportedModels)
		var costRatio *float64
		if monitor, exists := monitorByChannel[channel.Id]; exists && monitor.UpdatedTime > 0 {
			value, _, ratioErr := channelMonitorCostRatioFromModel(monitor, monitor.Ratio)
			if ratioErr == nil {
				costRatio = &value
			}
			if remark == "" {
				remark = strings.TrimSpace(monitor.Remark)
			}
		}
		configuredModelCount := 0
		running := false
		if configResponse != nil {
			configuredModelCount = len(configResponse.Models)
			running = configResponse.RunningRunId != "" && configByChannel[channel.Id].LeaseUntil > now
		}
		items = append(items, channelStatusProbeChannelResponse{
			Id: channel.Id, Name: channel.Name, Type: channel.Type, ChannelStatus: channel.Status,
			Remark: remark, Groups: channelGroups, CostRatio: costRatio, SupportedModels: supportedModels,
			AllowsCustomModel: allowsCustomModel, Config: configResponse, HealthStatus: health,
			Running: running, Latest: latest, AvgFirstTokenMs: avgFirstTokenMs, AvgTPS: avgTPS,
			TodayProbeCostCNY: channelMonitorCostCNY(todayProbeCostByChannel[channel.Id]),
			ModelStatuses:     modelStatuses, ConfiguredModelNum: configuredModelCount,
		})
	}
	groups := make([]string, 0, len(groupSet))
	for groupName := range groupSet {
		groups = append(groups, groupName)
	}
	sort.Strings(groups)
	models := make([]string, 0, len(modelSet))
	for modelName := range modelSet {
		models = append(models, modelName)
	}
	sort.Strings(models)
	modelsByGroup := make(map[string][]string, len(modelSetByGroup))
	for groupName, groupModelSet := range modelSetByGroup {
		groupModels := make([]string, 0, len(groupModelSet))
		for modelName := range groupModelSet {
			groupModels = append(groupModels, modelName)
		}
		sort.Strings(groupModels)
		modelsByGroup[groupName] = groupModels
	}
	return channelStatusProbeOverviewResponse{
		ServerNow: now, ScanIntervalSecond: channelStatusProbeScanIntervalSeconds(),
		Summary: summary, Groups: groups, Models: models,
		ModelsByGroup: modelsByGroup, Channels: items,
	}, nil
}

func UpdateChannelStatusProbeConfig(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的渠道 ID"})
		return
	}
	var request channelStatusProbeConfigRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.Enabled == nil ||
		request.Models == nil || request.IntervalSeconds == nil || request.DisplayValue == nil ||
		request.DisplayUnit == nil ||
		request.RecordSample == nil || request.Revision == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态探测配置参数不完整"})
		return
	}
	if *request.IntervalSeconds < model.ChannelStatusProbeMinIntervalSeconds ||
		*request.IntervalSeconds > model.ChannelStatusProbeMaxIntervalSeconds {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "探测间隔必须在 30 到 86400 秒之间"})
		return
	}
	if *request.RecordSample && *request.IntervalSeconds < 60 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "计入智能调度样本时，探测间隔不能小于 60 秒"})
		return
	}
	if !model.IsChannelStatusProbeDisplayAllowed(*request.DisplayValue, *request.DisplayUnit) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "状态展示范围必须为 1 到 60 分钟、1 到 24 小时或 1 到 30 天",
		})
		return
	}
	channel, err := model.GetChannelById(channelId, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
			return
		}
		common.ApiError(c, err)
		return
	}
	_, _, memberChannels, err := resolveChannelStatusProbeMembers(channel.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	models, err := normalizeLogicalChannelStatusProbeModels(memberChannels, *request.Models)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	saved, err := model.SaveChannelStatusProbeConfig(channelId, model.ChannelStatusProbeConfigInput{
		Enabled: *request.Enabled, Models: models, IntervalSeconds: *request.IntervalSeconds,
		DisplayValue: *request.DisplayValue, DisplayUnit: *request.DisplayUnit,
		RecordSample: *request.RecordSample, Revision: *request.Revision,
	}, common.GetTimestamp())
	if err != nil {
		if errors.Is(err, model.ErrChannelStatusProbeConfigChanged) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	invalidateChannelStatusProbeOverviewCache()
	wakeChannelStatusProbeWorker()
	response, err := channelStatusProbeConfigToResponse(saved)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	probeStatus := "关闭"
	if *request.Enabled {
		probeStatus = "开启"
	}
	recordManageAudit(c, "channel.status_probe_config_changed", map[string]any{
		"channel_id": channelId, "enabled": *request.Enabled, "models": models,
		"interval_seconds": *request.IntervalSeconds, "display_value": *request.DisplayValue,
		"display_unit": *request.DisplayUnit, "model_count": len(models),
		"status":        probeStatus,
		"record_sample": *request.RecordSample,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}

func RunChannelStatusProbeNow(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的渠道 ID"})
		return
	}
	if _, err := model.GetChannelById(channelId, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
			return
		}
		common.ApiError(c, err)
		return
	}
	requestId, err := model.RequestChannelStatusProbeManualRun(channelId, common.GetTimestamp())
	if err != nil {
		switch {
		case errors.Is(err, model.ErrChannelStatusProbeNotConfigured):
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		case errors.Is(err, model.ErrChannelStatusProbeManualPending):
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		default:
			common.ApiError(c, err)
		}
		return
	}
	invalidateChannelStatusProbeOverviewCache()
	wakeChannelStatusProbeWorker()
	c.JSON(http.StatusAccepted, gin.H{
		"success": true, "message": "", "data": gin.H{"manual_request_id": requestId},
	})
}

func ListChannelStatusProbeExecutions(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的渠道 ID"})
		return
	}
	if _, err := model.GetChannelById(channelId, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
			return
		}
		common.ApiError(c, err)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "每页数量必须在 1 到 100 之间"})
		return
	}
	modelName := strings.TrimSpace(c.Query("model"))
	result := strings.TrimSpace(c.Query("result"))
	trigger := strings.TrimSpace(c.Query("trigger"))
	validResults := map[string]bool{
		"": true, model.ChannelStatusProbeResultSuccess: true,
		model.ChannelStatusProbeResultUpstreamFailure: true, model.ChannelStatusProbeResultRateLimited: true,
		model.ChannelStatusProbeResultLocalFailure: true, model.ChannelStatusProbeResultSkipped: true,
		model.ChannelStatusProbeResultCanceled: true,
	}
	validTriggers := map[string]bool{
		"": true, model.ChannelStatusProbeTriggerScheduled: true, model.ChannelStatusProbeTriggerManual: true,
	}
	if !validResults[result] || !validTriggers[trigger] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "执行记录筛选条件无效"})
		return
	}
	executions, total, err := model.ListChannelStatusProbeExecutions(channelId, page, pageSize, modelName, result, trigger)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true, "message": "", "data": gin.H{
			"page": page, "page_size": pageSize, "total": total, "items": executions,
		},
	})
}
