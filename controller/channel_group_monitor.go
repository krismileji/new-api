package controller

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

const (
	channelGroupMonitorHealthUnconfigured = "unconfigured"
	channelGroupMonitorHealthPaused       = "paused"
	channelGroupMonitorHealthPending      = "pending"
	channelGroupMonitorHealthHealthy      = "healthy"
	channelGroupMonitorHealthUnavailable  = "unavailable"
	channelGroupMonitorHealthUnhealthy    = "unhealthy"
	channelGroupMonitorHealthRateLimited  = "rate_limited"
	channelGroupMonitorHealthStale        = "stale"
)

type channelGroupMonitorConfigResponse struct {
	Enabled           bool                             `json:"enabled"`
	ShowCacheRate     bool                             `json:"show_cache_rate"`
	CacheMinContextK  int                              `json:"cache_min_context_k"`
	Groups            []model.ChannelGroupMonitorGroup `json:"groups"`
	Categories        []string                         `json:"categories"`
	IntervalSeconds   int                              `json:"interval_seconds"`
	DisplayValue      int                              `json:"display_value"`
	DisplayUnit       string                           `json:"display_unit"`
	NextRunAt         int64                            `json:"next_run_at"`
	ManualRequestId   string                           `json:"manual_request_id"`
	ManualRequestedAt int64                            `json:"manual_requested_at"`
	Revision          int64                            `json:"revision"`
	RunningTrigger    string                           `json:"running_trigger"`
	RunningRunId      string                           `json:"running_run_id"`
	RunningStartedAt  int64                            `json:"running_started_at"`
	UpdatedAt         int64                            `json:"updated_at"`
}

type channelGroupMonitorConfigRequest struct {
	Enabled          *bool                             `json:"enabled"`
	ShowCacheRate    *bool                             `json:"show_cache_rate"`
	CacheMinContextK *int                              `json:"cache_min_context_k"`
	Groups           *[]model.ChannelGroupMonitorGroup `json:"groups"`
	Categories       *[]string                         `json:"categories"`
	IntervalSeconds  *int                              `json:"interval_seconds"`
	DisplayValue     *int                              `json:"display_value"`
	DisplayUnit      *string                           `json:"display_unit"`
	Revision         *int64                            `json:"revision"`
}

type channelGroupMonitorItemResponse struct {
	Passive            *channelGroupPassiveResponse        `json:"passive,omitempty"`
	PassiveMembers     bool                                `json:"passive_members,omitempty"`
	Group              string                              `json:"group"`
	Category           string                              `json:"category,omitempty"`
	Initial            string                              `json:"initial"`
	Status             string                              `json:"status"`
	LatestFirstTokenMs *float64                            `json:"latest_first_token_ms"`
	SuccessRate        *float64                            `json:"success_rate"`
	CacheRate          *float64                            `json:"cache_rate,omitempty"`
	CacheRateMax       *float64                            `json:"cache_rate_max,omitempty"`
	CacheRateAverage   *float64                            `json:"cache_rate_average,omitempty"`
	SuccessCount       int                                 `json:"success_count"`
	CompletedCount     int                                 `json:"completed_count"`
	LastFinishedAt     int64                               `json:"last_finished_at"`
	ProbeModel         string                              `json:"probe_model,omitempty"`
	ConfigValid        bool                                `json:"config_valid,omitempty"`
	LatestResult       string                              `json:"latest_result,omitempty"`
	LastSuccessAt      int64                               `json:"last_success_at,omitempty"`
	LastFailureAt      int64                               `json:"last_failure_at,omitempty"`
	ConsecutiveSuccess int                                 `json:"consecutive_success,omitempty"`
	ConsecutiveFailure int                                 `json:"consecutive_failure,omitempty"`
	RecentWindow       []channelGroupMonitorBucketResponse `json:"recent_window"`
}

type channelGroupMonitorBucketResponse struct {
	StartedAt               int64    `json:"started_at"`
	Success                 int      `json:"success"`
	UpstreamFailure         int      `json:"upstream_failure"`
	RateLimited             int      `json:"rate_limited"`
	LocalFailure            int      `json:"local_failure"`
	Unavailable             int      `json:"unavailable"`
	Skipped                 int      `json:"skipped"`
	Timeout                 int      `json:"timeout"`
	FirstTokenTotalMs       float64  `json:"first_token_total_ms,omitempty"`
	FirstTokenSampleCount   int64    `json:"first_token_sample_count,omitempty"`
	TPSTotal                float64  `json:"tps_total,omitempty"`
	TPSSampleCount          int64    `json:"tps_sample_count,omitempty"`
	ResponseTimeTotalMs     float64  `json:"response_time_total_ms,omitempty"`
	ResponseTimeSampleCount int64    `json:"response_time_sample_count,omitempty"`
	Result                  string   `json:"result"`
	LatestResult            string   `json:"latest_result,omitempty"`
	LatestFirstTokenMs      *float64 `json:"latest_first_token_ms,omitempty"`
	LatestTPS               *float64 `json:"latest_tps,omitempty"`
	LatestResponseTimeMs    *float64 `json:"latest_response_time_ms,omitempty"`
}

// pricingGroupMonitorItemResponse is the public subset of monitor state.
// Administrative configuration and diagnostic fields remain on the admin API only.
type pricingGroupMonitorItemResponse struct {
	Passive            *channelGroupPassiveResponse        `json:"passive,omitempty"`
	PassiveMembers     bool                                `json:"passive_members,omitempty"`
	Group              string                              `json:"group"`
	Description        string                              `json:"description,omitempty"`
	Category           string                              `json:"category,omitempty"`
	Initial            string                              `json:"initial"`
	Status             string                              `json:"status"`
	ProbeModel         string                              `json:"probe_model,omitempty"`
	LatestFirstTokenMs *float64                            `json:"latest_first_token_ms"`
	SuccessRate        *float64                            `json:"success_rate"`
	CacheRate          *float64                            `json:"cache_rate,omitempty"`
	CacheRateMax       *float64                            `json:"cache_rate_max,omitempty"`
	CacheRateAverage   *float64                            `json:"cache_rate_average,omitempty"`
	GroupRatio         float64                             `json:"group_ratio"`
	LastFinishedAt     int64                               `json:"last_finished_at"`
	RecentWindow       []channelGroupMonitorBucketResponse `json:"recent_window"`
}

type channelGroupMonitorOverviewResponse struct {
	ServerNow              int64                             `json:"server_now"`
	Settings               channelGroupMonitorConfigResponse `json:"settings"`
	CandidateModelsByGroup map[string][]string               `json:"candidate_models_by_group"`
	Items                  []channelGroupMonitorItemResponse `json:"items"`
}

func channelGroupMonitorConfigToResponse(config model.ChannelGroupMonitorConfig) (channelGroupMonitorConfigResponse, error) {
	cacheMinContextK, err := config.CacheMinContextK()
	if err != nil {
		return channelGroupMonitorConfigResponse{}, err
	}
	showCacheRate, err := config.ShowCacheRate()
	if err != nil {
		return channelGroupMonitorConfigResponse{}, err
	}
	groups, err := config.Groups()
	if err != nil {
		return channelGroupMonitorConfigResponse{}, err
	}
	categories, err := config.Categories()
	if err != nil {
		return channelGroupMonitorConfigResponse{}, err
	}
	displayValue, displayUnit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
	return channelGroupMonitorConfigResponse{
		CacheMinContextK: cacheMinContextK,
		ShowCacheRate:    showCacheRate,
		Enabled:          config.Enabled, Groups: groups, Categories: categories, IntervalSeconds: config.IntervalSeconds,
		DisplayValue: displayValue, DisplayUnit: displayUnit, NextRunAt: config.NextRunAt,
		ManualRequestId: config.ManualRequestId, ManualRequestedAt: config.ManualRequestedAt,
		Revision: config.Revision, RunningTrigger: config.RunningTrigger, RunningRunId: config.RunningRunId,
		RunningStartedAt: config.RunningStartedAt, UpdatedAt: config.UpdatedAt,
	}, nil
}

func getChannelGroupMonitorCandidateModels(ctx context.Context, enabledOnly bool) (map[string][]string, error) {
	channels, err := model.GetChannelGroupMonitorCandidateChannels(ctx, enabledOnly)
	if err != nil {
		return nil, err
	}
	channelsByID := make(map[int]*model.Channel, len(channels))
	channelIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelsByID[channel.Id] = channel
		channelIDs = append(channelIDs, channel.Id)
	}
	abilities, err := model.GetChannelGroupMonitorCandidateAbilities(ctx, channelIDs, enabledOnly)
	if err != nil {
		return nil, err
	}
	candidateSets := make(map[string]map[string]struct{})
	for _, ability := range abilities {
		channel := channelsByID[ability.ChannelId]
		if channel == nil {
			continue
		}
		groupName := strings.TrimSpace(ability.Group)
		modelName := strings.TrimSpace(ability.Model)
		if groupName == "" || modelName == "" || strings.Contains(modelName, "*") || !channelGroupMonitorSupportsTextProbe(channel, modelName) {
			continue
		}
		if candidateSets[groupName] == nil {
			candidateSets[groupName] = make(map[string]struct{})
		}
		candidateSets[groupName][modelName] = struct{}{}
	}
	candidates := make(map[string][]string, len(candidateSets))
	for groupName, models := range candidateSets {
		values := make([]string, 0, len(models))
		for modelName := range models {
			values = append(values, modelName)
		}
		sort.Strings(values)
		candidates[groupName] = values
	}
	return candidates, nil
}

// channelGroupMonitorSupportsTextProbe filters models that cannot be probed
// by the text request fixture and checks the channel's selected probe path.
func channelGroupMonitorSupportsTextProbe(channel *model.Channel, modelName string) bool {
	if channel == nil {
		return false
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}
	switch channel.Type {
	case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus,
		constant.ChannelTypeSunoAPI, constant.ChannelTypeJina, constant.ChannelTypeMokaAI,
		constant.ChannelTypeKling, constant.ChannelTypeJimeng, constant.ChannelTypeVidu,
		constant.ChannelTypeDoubaoVideo, constant.ChannelTypeSora, constant.ChannelTypeReplicate:
		return false
	}
	normalized := strings.ToLower(modelName)
	for _, marker := range []string{
		"embedding", "embed", "rerank", "moderation", "audio", "realtime", "speech",
		"transcription", "whisper", "tts", "image", "imagen", "flux", "seedream",
		"stable-diffusion", "sdxl", "video", "sora", "veo-", "kling", "suno", "music",
	} {
		if strings.Contains(normalized, marker) {
			return false
		}
	}
	if strings.HasPrefix(normalized, "m3e") || strings.Contains(normalized, "bge-") {
		return false
	}
	// For Anthropic channels, check if they support the Messages API path
	// instead of the Responses API path
	if channel.Type == constant.ChannelTypeAnthropic {
		return middleware.ChannelSupportsRequestPath(channel, "/v1/messages", modelName)
	}
	return middleware.ChannelSupportsRequestPath(channel, "/v1/responses", modelName)
}

func groupMonitorModelIsCandidate(candidates map[string][]string, groupName string, probeModel string) bool {
	for _, candidate := range candidates[groupName] {
		if candidate == probeModel {
			return true
		}
	}
	return false
}

func includeSavedChannelGroupMonitorModels(
	candidates map[string][]string,
	config model.ChannelGroupMonitorConfig,
) (map[string][]string, error) {
	groups, err := config.Groups()
	if err != nil {
		return nil, err
	}
	merged := make(map[string][]string, len(candidates))
	for groupName, models := range candidates {
		merged[groupName] = append([]string(nil), models...)
	}
	for _, group := range groups {
		groupName := strings.TrimSpace(group.GroupName)
		probeModel := strings.TrimSpace(group.ProbeModel)
		if groupName == "" || probeModel == "" || groupMonitorModelIsCandidate(merged, groupName, probeModel) {
			continue
		}
		merged[groupName] = append(merged[groupName], probeModel)
		sort.Strings(merged[groupName])
	}
	return merged, nil
}

func normalizeChannelGroupMonitorGroups(rawGroups []model.ChannelGroupMonitorGroup, candidates map[string][]string) ([]model.ChannelGroupMonitorGroup, error) {
	if len(rawGroups) > model.ChannelGroupMonitorMaxGroups {
		return nil, errors.New("监控分组不能超过 100 个")
	}
	groups := make([]model.ChannelGroupMonitorGroup, 0, len(rawGroups))
	seen := make(map[string]struct{}, len(rawGroups))
	for _, rawGroup := range rawGroups {
		groupName := strings.TrimSpace(rawGroup.GroupName)
		probeModel := strings.TrimSpace(rawGroup.ProbeModel)
		displayInitial := strings.TrimSpace(rawGroup.DisplayInitial)
		category := strings.TrimSpace(rawGroup.Category)
		if groupName == "" || utf8.RuneCountInString(groupName) > 64 {
			return nil, errors.New("分组名称不能为空且长度不能超过 64 个字符")
		}
		if probeModel == "" || utf8.RuneCountInString(probeModel) > 255 || strings.Contains(probeModel, "*") {
			return nil, errors.New("探测模型必须是长度不超过 255 的具体文本模型")
		}
		if utf8.RuneCountInString(displayInitial) > 1 {
			return nil, errors.New("分组展示字只能配置一个字符")
		}
		if utf8.RuneCountInString(category) > 64 {
			return nil, errors.New("分类名称不能超过 64 个字符")
		}
		if _, exists := seen[groupName]; exists {
			return nil, errors.New("同一个监控分组只能配置一次")
		}
		if !groupMonitorModelIsCandidate(candidates, groupName, probeModel) {
			return nil, errors.New("探测模型 " + probeModel + " 不属于分组 " + groupName + " 的可用文本模型")
		}
		seen[groupName] = struct{}{}
		groups = append(groups, model.ChannelGroupMonitorGroup{
			GroupName: groupName, ProbeModel: probeModel, DisplayInitial: displayInitial,
			Category: category, Enabled: rawGroup.Enabled,
		})
	}
	return groups, nil
}

func channelGroupMonitorDisplaySeconds(value int, unit string) int64 {
	return int64(value) * model.ChannelStatusProbeDisplayBucketSeconds(unit)
}

func channelGroupMonitorBucketResult(bucket channelGroupMonitorBucketResponse) string {
	switch {
	case bucket.UpstreamFailure > 0:
		return model.ChannelGroupMonitorResultUpstreamFailure
	case bucket.Timeout > 0:
		return model.ChannelGroupMonitorResultTimeout
	case bucket.Unavailable > 0:
		return model.ChannelGroupMonitorResultUnavailable
	case bucket.RateLimited > 0:
		return model.ChannelGroupMonitorResultRateLimited
	case bucket.LocalFailure > 0:
		return model.ChannelGroupMonitorResultLocalFailure
	case bucket.Skipped > 0:
		return model.ChannelGroupMonitorResultSkipped
	case bucket.Success > 0:
		return model.ChannelGroupMonitorResultSuccess
	default:
		return ""
	}
}

func mergeChannelGroupMonitorRecentWindow(
	executions []model.ChannelGroupMonitorExecution,
	now int64,
	displayValue int,
	displayUnit string,
) map[string][]channelGroupMonitorBucketResponse {
	displayValue, displayUnit = model.NormalizeChannelStatusProbeDisplay(displayValue, displayUnit)
	bucketSeconds := model.ChannelStatusProbeDisplayBucketSeconds(displayUnit)
	currentBucket := model.ChannelStatusProbeDisplayBucketStart(now, displayUnit)
	minimumBucket := currentBucket - int64(displayValue-1)*bucketSeconds
	bucketsByGroup := make(map[string]map[int64]channelGroupMonitorBucketResponse)
	latestByGroup := make(map[string]map[int64]model.ChannelGroupMonitorExecution)
	for _, execution := range executions {
		if execution.FinishedAt <= 0 || execution.FinishedAt > now {
			continue
		}
		bucketTimestamp := execution.FinishedAt
		if execution.Result == model.ChannelGroupMonitorResultTimeout && execution.StartedAt > 0 {
			bucketTimestamp = execution.StartedAt
		}
		if bucketTimestamp < minimumBucket || bucketTimestamp > now {
			continue
		}
		startedAt := model.ChannelStatusProbeDisplayBucketStart(bucketTimestamp, displayUnit)
		if startedAt < minimumBucket || startedAt > currentBucket {
			continue
		}
		groupBuckets := bucketsByGroup[execution.GroupName]
		if groupBuckets == nil {
			groupBuckets = make(map[int64]channelGroupMonitorBucketResponse)
			bucketsByGroup[execution.GroupName] = groupBuckets
		}
		bucket := groupBuckets[startedAt]
		bucket.StartedAt = startedAt
		switch execution.Result {
		case model.ChannelGroupMonitorResultSuccess:
			bucket.Success++
			if execution.FirstTokenMs != nil {
				bucket.FirstTokenTotalMs += *execution.FirstTokenMs
				bucket.FirstTokenSampleCount++
			}
			if execution.TPS != nil {
				bucket.TPSTotal += *execution.TPS
				bucket.TPSSampleCount++
			}
		case model.ChannelGroupMonitorResultUpstreamFailure:
			bucket.UpstreamFailure++
		case model.ChannelGroupMonitorResultTimeout:
			bucket.Timeout++
		case model.ChannelGroupMonitorResultRateLimited:
			bucket.RateLimited++
		case model.ChannelGroupMonitorResultLocalFailure:
			bucket.LocalFailure++
		case model.ChannelGroupMonitorResultUnavailable:
			bucket.Unavailable++
		case model.ChannelGroupMonitorResultSkipped:
			bucket.Skipped++
		}
		if execution.ResponseTimeMs != nil {
			bucket.ResponseTimeTotalMs += *execution.ResponseTimeMs
			bucket.ResponseTimeSampleCount++
		}
		latestBuckets := latestByGroup[execution.GroupName]
		if latestBuckets == nil {
			latestBuckets = make(map[int64]model.ChannelGroupMonitorExecution)
			latestByGroup[execution.GroupName] = latestBuckets
		}
		if previous, exists := latestBuckets[startedAt]; !exists ||
			execution.FinishedAt > previous.FinishedAt ||
			(execution.FinishedAt == previous.FinishedAt && execution.Id > previous.Id) {
			latestBuckets[startedAt] = execution
		}
		bucket.Result = channelGroupMonitorBucketResult(bucket)
		groupBuckets[startedAt] = bucket
	}
	result := make(map[string][]channelGroupMonitorBucketResponse, len(bucketsByGroup))
	for groupName, groupBuckets := range bucketsByGroup {
		buckets := make([]channelGroupMonitorBucketResponse, 0, displayValue)
		for startedAt := minimumBucket; startedAt <= currentBucket; startedAt += bucketSeconds {
			bucket := groupBuckets[startedAt]
			bucket.StartedAt = startedAt
			if latest, exists := latestByGroup[groupName][startedAt]; exists {
				bucket.LatestResult = latest.Result
				bucket.LatestFirstTokenMs = latest.FirstTokenMs
				bucket.LatestTPS = latest.TPS
				bucket.LatestResponseTimeMs = latest.ResponseTimeMs
			}
			buckets = append(buckets, bucket)
		}
		result[groupName] = buckets
	}
	return result
}

func emptyChannelGroupMonitorRecentWindow(
	now int64,
	displayValue int,
	displayUnit string,
) []channelGroupMonitorBucketResponse {
	displayValue, displayUnit = model.NormalizeChannelStatusProbeDisplay(displayValue, displayUnit)
	bucketSeconds := model.ChannelStatusProbeDisplayBucketSeconds(displayUnit)
	currentBucket := model.ChannelStatusProbeDisplayBucketStart(now, displayUnit)
	minimumBucket := currentBucket - int64(displayValue-1)*bucketSeconds
	buckets := make([]channelGroupMonitorBucketResponse, 0, displayValue)
	for startedAt := minimumBucket; startedAt <= currentBucket; startedAt += bucketSeconds {
		buckets = append(buckets, channelGroupMonitorBucketResponse{StartedAt: startedAt})
	}
	return buckets
}

func channelGroupMonitorInitial(groupName string, displayInitial string) string {
	displayInitial = strings.TrimSpace(displayInitial)
	if displayInitial != "" && utf8.RuneCountInString(displayInitial) == 1 {
		return displayInitial
	}
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return "?"
	}
	r, _ := utf8.DecodeRuneInString(groupName)
	if r == utf8.RuneError {
		return "?"
	}
	return string(unicode.ToUpper(r))
}

func channelGroupMonitorHealth(config model.ChannelGroupMonitorConfig, state *model.ChannelGroupMonitorState, now int64) string {
	if !config.Enabled {
		return channelGroupMonitorHealthPaused
	}
	if state == nil || state.FinishedAt <= 0 {
		return channelGroupMonitorHealthPending
	}
	if state.FinishedAt < now-int64(config.IntervalSeconds*2) {
		return channelGroupMonitorHealthStale
	}
	switch state.Result {
	case model.ChannelGroupMonitorResultSuccess:
		return channelGroupMonitorHealthHealthy
	case model.ChannelGroupMonitorResultRateLimited:
		return channelGroupMonitorHealthRateLimited
	case model.ChannelGroupMonitorResultTimeout:
		return channelGroupMonitorHealthStale
	case model.ChannelGroupMonitorResultUnavailable:
		return channelGroupMonitorHealthUnavailable
	case model.ChannelGroupMonitorResultUpstreamFailure, model.ChannelGroupMonitorResultLocalFailure:
		return channelGroupMonitorHealthUnhealthy
	default:
		return channelGroupMonitorHealthPending
	}
}

type channelGroupMonitorBuildInputs struct {
	Reuse             map[string]channelGroupMonitorItemResponse
	EnabledCandidates map[string][]string
}

func buildChannelGroupMonitorItems(
	ctx context.Context,
	config model.ChannelGroupMonitorConfig,
	validCandidates map[string][]string,
	now int64,
	inputs ...channelGroupMonitorBuildInputs,
) ([]channelGroupMonitorItemResponse, error) {
	groups, err := config.Groups()
	if err != nil {
		return nil, err
	}
	generation, err := service.SyncChannelGroupMonitorGeneration(ctx, config)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]bool, len(groups))
	for _, group := range groups {
		selected[group.GroupName] = true
		if len(inputs) > 0 {
			_, unchanged := inputs[0].Reuse[group.GroupName]
			selected[group.GroupName] = !unchanged
		}
	}
	projection, err := service.ReadChannelGroupMonitorProjection(ctx, generation, now, selected)
	if err != nil {
		return nil, err
	}
	showCacheRate, err := config.ShowCacheRate()
	if err != nil {
		return nil, err
	}
	items := make([]channelGroupMonitorItemResponse, 0, len(groups))
	// Disabled channels and abilities remain valid configuration, but must not
	// leave an old healthy result visible until the next scheduled execution.
	var enabledCandidates map[string][]string
	if len(inputs) > 0 {
		enabledCandidates = inputs[0].EnabledCandidates
	}
	if enabledCandidates == nil {
		enabledCandidates, err = getChannelGroupMonitorCandidateModels(ctx, true)
		if err != nil {
			return nil, err
		}
	}
	for _, group := range groups {
		if len(inputs) > 0 {
			if previous, ok := inputs[0].Reuse[group.GroupName]; ok {
				items = append(items, previous)
				continue
			}
		}
		configValid := groupMonitorModelIsCandidate(validCandidates, group.GroupName, group.ProbeModel)
		item := channelGroupMonitorItemResponse{
			Group: group.GroupName, Initial: channelGroupMonitorInitial(group.GroupName, group.DisplayInitial), ProbeModel: group.ProbeModel,
			Category:    group.Category,
			ConfigValid: configValid, RecentWindow: emptyChannelGroupMonitorRecentWindow(now, config.DisplayValue, config.DisplayUnit),
		}
		data := projection[group.GroupName]
		if showCacheRate {
			item.CacheRate, item.CacheRateMax, item.CacheRateAverage = data.Cache.Weighted, data.Cache.APIKeyMax, data.Cache.APIKeyAverage
		}
		for index, source := range data.Buckets {
			bucket := &item.RecentWindow[index]
			bucket.Success = int(source.Counts["success"])
			bucket.UpstreamFailure = int(source.Counts["upstream_failure"])
			bucket.LocalFailure = int(source.Counts["local_failure"])
			bucket.RateLimited = int(source.Counts["rate_limited"])
			bucket.Unavailable = int(source.Counts["unavailable"])
			bucket.Timeout = int(source.Counts["timeout"])
			bucket.Skipped = int(source.Counts["skipped"])
			bucket.FirstTokenTotalMs, bucket.FirstTokenSampleCount = source.Counts["first_total"], int64(source.Counts["first_count"])
			bucket.TPSTotal, bucket.TPSSampleCount = source.Counts["tps_total"], int64(source.Counts["tps_count"])
			bucket.ResponseTimeTotalMs, bucket.ResponseTimeSampleCount = source.Counts["response_total"], int64(source.Counts["response_count"])
			bucket.Result = channelGroupMonitorBucketResult(*bucket)
			if source.Latest != nil {
				bucket.LatestResult, bucket.LatestFirstTokenMs = source.Latest.Result, source.Latest.FirstTokenMs
				bucket.LatestTPS, bucket.LatestResponseTimeMs = source.Latest.TPS, source.Latest.ResponseTimeMs
			}
			item.SuccessCount += bucket.Success
			item.CompletedCount += bucket.Success + bucket.UpstreamFailure + bucket.LocalFailure + bucket.RateLimited + bucket.Unavailable + bucket.Timeout
		}
		if item.CompletedCount > 0 {
			rate := float64(item.SuccessCount) * 100 / float64(item.CompletedCount)
			item.SuccessRate = &rate
		}
		item.Status = channelGroupMonitorHealth(config, data.State, now)
		if state := data.State; state != nil {
			item.LatestFirstTokenMs, item.LastFinishedAt, item.LatestResult = state.FirstTokenMs, state.FinishedAt, state.Result
			item.LastSuccessAt, item.LastFailureAt = state.LastSuccessAt, state.LastFailureAt
			item.ConsecutiveSuccess, item.ConsecutiveFailure = state.ConsecutiveSuccess, state.ConsecutiveFailure
		}
		if !configValid {
			item.Status = channelGroupMonitorHealthUnconfigured
		} else if config.Enabled && !groupMonitorModelIsCandidate(enabledCandidates, group.GroupName, group.ProbeModel) {
			item.Status = channelGroupMonitorHealthUnavailable
		}
		if !group.IsEnabled() {
			item.Status = channelGroupMonitorHealthPaused
		}
		items = append(items, item)
	}
	applyChannelGroupPassiveOverviewSince(ctx, items, generation.StartedAt)
	return items, nil
}

func respondChannelGroupMonitorQueryError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrChannelGroupMonitorCandidatesTooLarge) ||
		errors.Is(err, model.ErrChannelGroupMonitorWindowTooLarge) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiError(c, err)
}

func GetChannelGroupMonitorSettings(c *gin.Context) {
	var snapshot channelGroupMonitorSnapshot
	if err := service.ReadChannelGroupMonitorSnapshot(c.Request.Context(), &snapshot); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": service.ErrChannelGroupMonitorSnapshotPending.Error()})
		return
	}
	writeChannelMonitorBoundedJSON(c, gin.H{"settings": snapshot.Overview.Settings, "candidate_models_by_group": snapshot.Overview.CandidateModelsByGroup})
}

func UpdateChannelGroupMonitorSettings(c *gin.Context) {
	var request channelGroupMonitorConfigRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.Enabled == nil || request.Groups == nil ||
		request.IntervalSeconds == nil || request.DisplayValue == nil || request.DisplayUnit == nil || request.Revision == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分组监控配置参数不完整"})
		return
	}
	if request.CacheMinContextK != nil && (*request.CacheMinContextK < 0 || *request.CacheMinContextK > model.ChannelGroupMonitorMaxCacheContextK) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缓存率最小上下文必须为 0 到 1000 K 的整数"})
		return
	}
	if *request.IntervalSeconds < model.ChannelGroupMonitorMinIntervalSeconds || *request.IntervalSeconds > model.ChannelGroupMonitorMaxIntervalSeconds {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "探测间隔必须在 30 到 86400 秒之间"})
		return
	}
	if !model.IsChannelStatusProbeDisplayAllowed(*request.DisplayValue, *request.DisplayUnit) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态展示范围必须为 1 到 60 分钟、1 到 24 小时或 1 到 30 天"})
		return
	}
	if channelGroupMonitorDisplaySeconds(*request.DisplayValue, *request.DisplayUnit) < int64(*request.IntervalSeconds*2) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "状态展示范围至少需要覆盖两个探测周期"})
		return
	}
	candidates, err := getChannelGroupMonitorCandidateModels(c.Request.Context(), false)
	if err != nil {
		respondChannelGroupMonitorQueryError(c, err)
		return
	}
	currentConfig, err := model.GetChannelGroupMonitorConfigOrDefaultWithContext(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	candidates, err = includeSavedChannelGroupMonitorModels(candidates, currentConfig)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups, err := normalizeChannelGroupMonitorGroups(*request.Groups, candidates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	var categories []string
	if request.Categories != nil {
		categories = *request.Categories
	} else {
		// Older clients omit category metadata; retain empty categories as well.
		categories, err = currentConfig.Categories()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		knownCategories := make(map[string]bool, len(categories))
		for _, category := range categories {
			knownCategories[category] = true
		}
		for _, group := range groups {
			category := group.Category
			if category == "" {
				category = "未分类"
			}
			if !knownCategories[category] {
				categories = append(categories, category)
				knownCategories[category] = true
			}
		}
	}
	categories, err = normalizeChannelGroupMonitorCategories(categories, groups)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	showCacheRate, err := currentConfig.ShowCacheRate()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if request.ShowCacheRate != nil {
		showCacheRate = *request.ShowCacheRate
	}
	cacheMinContextK, err := currentConfig.CacheMinContextK()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if request.CacheMinContextK != nil {
		cacheMinContextK = *request.CacheMinContextK
	}
	saved, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		CacheMinContextK: cacheMinContextK,
		ShowCacheRate:    showCacheRate,
		Enabled:          *request.Enabled, Groups: groups, IntervalSeconds: *request.IntervalSeconds,
		Categories:   categories,
		DisplayValue: *request.DisplayValue, DisplayUnit: *request.DisplayUnit, Revision: *request.Revision,
	}, common.GetTimestamp())
	if err != nil {
		if errors.Is(err, model.ErrChannelGroupMonitorConfigChanged) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	if err := service.UpdateChannelGroupMonitorCachePolicy(saved); err != nil {
		common.SysError("更新分组缓存率配置失败: " + err.Error())
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "配置已保存，Redis 统计重置暂未完成，请稍后刷新配置", "revision": saved.Revision})
		return
	}
	response, err := channelGroupMonitorConfigToResponse(saved)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.group_monitor_config_changed", map[string]any{
		"cache_min_context_k": cacheMinContextK,
		"show_cache_rate":     showCacheRate,
		"enabled":             *request.Enabled, "groups": groups, "group_count": len(groups),
		"categories":       categories,
		"interval_seconds": *request.IntervalSeconds, "display_value": *request.DisplayValue,
		"display_unit": *request.DisplayUnit,
	})
	// Saving invalidates the previous lease with the statistics generation.
	// Publish the new initial view now instead of waiting for the periodic check.
	if err := refreshChannelGroupMonitorSnapshot(c.Request.Context(), true); err != nil {
		common.SysError("生成分组监控初始快照失败: " + err.Error())
	}
	wakeChannelGroupMonitorWorker()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}

func GetChannelGroupMonitorOverview(c *gin.Context) {
	var snapshot channelGroupMonitorSnapshot
	if err := service.ReadChannelGroupMonitorSnapshot(c.Request.Context(), &snapshot); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": service.ErrChannelGroupMonitorSnapshotPending.Error()})
		return
	}
	writeChannelMonitorBoundedJSON(c, snapshot.Overview)
}

func RunChannelGroupMonitorNow(c *gin.Context) {
	requestId, err := model.RequestChannelGroupMonitorManualRun(common.GetTimestamp())
	if err != nil {
		if errors.Is(err, model.ErrChannelGroupMonitorManualPending) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		if strings.Contains(err.Error(), "请先保存") {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	wakeChannelGroupMonitorWorker()
	c.JSON(http.StatusAccepted, gin.H{"success": true, "message": "", "data": gin.H{"manual_request_id": requestId}})
}

func ListChannelGroupMonitorExecutions(c *gin.Context) {
	page, ok := parseChannelMonitorPositivePageQuery(c, "page", 1, channelMonitorMaxPage)
	if !ok {
		return
	}
	pageSize, ok := parseChannelMonitorPositivePageQuery(c, "page_size", 20, 100)
	if !ok {
		return
	}
	groupName := strings.TrimSpace(c.Query("group"))
	result := strings.TrimSpace(c.Query("result"))
	if utf8.RuneCountInString(groupName) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分组名称长度不能超过 64 个字符"})
		return
	}
	validResults := map[string]bool{
		"": true, model.ChannelGroupMonitorResultSuccess: true,
		model.ChannelGroupMonitorResultUpstreamFailure: true,
		model.ChannelGroupMonitorResultRateLimited:     true,
		model.ChannelGroupMonitorResultLocalFailure:    true,
		model.ChannelGroupMonitorResultUnavailable:     true,
		model.ChannelGroupMonitorResultSkipped:         true,
		model.ChannelGroupMonitorResultTimeout:         true,
	}
	if !validResults[result] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "执行记录筛选条件无效"})
		return
	}
	items, total, err := model.ListChannelGroupMonitorExecutionsWithContext(
		c.Request.Context(), page, pageSize, groupName, result,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"page": page, "page_size": pageSize, "total": total, "items": items,
	}})
}

func GetPricingGroupMonitor(c *gin.Context) {
	var snapshot channelGroupMonitorSnapshot
	if err := service.ReadChannelGroupMonitorSnapshot(c.Request.Context(), &snapshot); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": service.ErrChannelGroupMonitorSnapshotPending.Error()})
		return
	}
	userGroup := c.GetString("user_group")
	for index := range snapshot.Public.Items {
		item := &snapshot.Public.Items[index]
		if ratio, ok := snapshot.UserRatios[userGroup][item.Group]; ok {
			item.GroupRatio = ratio
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": snapshot.Public})
}

func buildPricingGroupMonitorItems(config model.ChannelGroupMonitorConfig, items []channelGroupMonitorItemResponse) []pricingGroupMonitorItemResponse {
	common.OptionMapRWMutex.RLock()
	descriptionsJSON := common.OptionMap["GroupDescriptions"]
	common.OptionMapRWMutex.RUnlock()
	descriptions := make(map[string]string)
	if descriptionsJSON != "" {
		if err := common.UnmarshalJsonStr(descriptionsJSON, &descriptions); err != nil || descriptions == nil {
			descriptions = make(map[string]string)
		}
	}
	// Selectable groups retain their current descriptions, including explicit blanks.
	maps.Copy(descriptions, setting.GetUserUsableGroupsCopy())
	publicItems := make([]pricingGroupMonitorItemResponse, 0, len(items))
	for _, item := range items {
		status := item.Status
		if !config.Enabled || status == channelGroupMonitorHealthPaused {
			status = channelGroupMonitorHealthPaused
		} else if !item.ConfigValid {
			status = channelGroupMonitorHealthUnavailable
		}
		publicItems = append(publicItems, pricingGroupMonitorItemResponse{
			Passive: item.Passive, PassiveMembers: item.PassiveMembers,
			Group:              item.Group,
			Description:        descriptions[item.Group],
			Category:           item.Category,
			Initial:            item.Initial,
			Status:             status,
			ProbeModel:         item.ProbeModel,
			LatestFirstTokenMs: item.LatestFirstTokenMs,
			SuccessRate:        item.SuccessRate,
			CacheRate:          item.CacheRate,
			CacheRateMax:       item.CacheRateMax,
			CacheRateAverage:   item.CacheRateAverage,
			GroupRatio:         service.GetUserGroupRatio("", item.Group),
			LastFinishedAt:     item.LastFinishedAt,
			RecentWindow:       item.RecentWindow,
		})
	}
	return publicItems
}
