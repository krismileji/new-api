package controller

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

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
	Groups            []model.ChannelGroupMonitorGroup `json:"groups"`
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
	Enabled         *bool                             `json:"enabled"`
	Groups          *[]model.ChannelGroupMonitorGroup `json:"groups"`
	IntervalSeconds *int                              `json:"interval_seconds"`
	DisplayValue    *int                              `json:"display_value"`
	DisplayUnit     *string                           `json:"display_unit"`
	Revision        *int64                            `json:"revision"`
}

type channelGroupMonitorItemResponse struct {
	Group              string                              `json:"group"`
	Initial            string                              `json:"initial"`
	Status             string                              `json:"status"`
	LatestFirstTokenMs *float64                            `json:"latest_first_token_ms"`
	SuccessRate        *float64                            `json:"success_rate"`
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
	StartedAt               int64   `json:"started_at"`
	Success                 int     `json:"success"`
	UpstreamFailure         int     `json:"upstream_failure"`
	RateLimited             int     `json:"rate_limited"`
	LocalFailure            int     `json:"local_failure"`
	Unavailable             int     `json:"unavailable"`
	Skipped                 int     `json:"skipped"`
	FirstTokenTotalMs       float64 `json:"first_token_total_ms,omitempty"`
	FirstTokenSampleCount   int64   `json:"first_token_sample_count,omitempty"`
	TPSTotal                float64 `json:"tps_total,omitempty"`
	TPSSampleCount          int64   `json:"tps_sample_count,omitempty"`
	ResponseTimeTotalMs     float64 `json:"response_time_total_ms,omitempty"`
	ResponseTimeSampleCount int64   `json:"response_time_sample_count,omitempty"`
	Result                  string  `json:"result"`
}

// pricingGroupMonitorItemResponse is the public subset of monitor state.
// Administrative configuration and diagnostic fields remain on the admin API only.
type pricingGroupMonitorItemResponse struct {
	Group              string                              `json:"group"`
	Initial            string                              `json:"initial"`
	Status             string                              `json:"status"`
	ProbeModel         string                              `json:"probe_model,omitempty"`
	LatestFirstTokenMs *float64                            `json:"latest_first_token_ms"`
	SuccessRate        *float64                            `json:"success_rate"`
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
	groups, err := config.Groups()
	if err != nil {
		return channelGroupMonitorConfigResponse{}, err
	}
	displayValue, displayUnit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
	return channelGroupMonitorConfigResponse{
		Enabled: config.Enabled, Groups: groups, IntervalSeconds: config.IntervalSeconds,
		DisplayValue: displayValue, DisplayUnit: displayUnit, NextRunAt: config.NextRunAt,
		ManualRequestId: config.ManualRequestId, ManualRequestedAt: config.ManualRequestedAt,
		Revision: config.Revision, RunningTrigger: config.RunningTrigger, RunningRunId: config.RunningRunId,
		RunningStartedAt: config.RunningStartedAt, UpdatedAt: config.UpdatedAt,
	}, nil
}

func getChannelGroupMonitorCandidateModels(enabledOnly bool) (map[string][]string, error) {
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		return nil, err
	}
	channelsByID := make(map[int]*model.Channel, len(channels))
	for _, channel := range channels {
		channelsByID[channel.Id] = channel
	}
	abilities, err := model.GetAllEnableAbilityWithChannels()
	if err != nil {
		return nil, err
	}
	candidateSets := make(map[string]map[string]struct{})
	for _, ability := range abilities {
		channel := channelsByID[ability.ChannelId]
		if channel == nil {
			continue
		}
		if enabledOnly && channel.Status != common.ChannelStatusEnabled {
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

// channelGroupMonitorSupportsTextProbe keeps the model semantics used by a
// normal /v1/responses request while filtering models that cannot be probed by
// the text request fixture. Unlike the smart-schedule sampler, this must not
// restrict the provider API type: Claude and DeepSeek responses adaptors are
// valid group routes too.
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
		if groupName == "" || utf8.RuneCountInString(groupName) > 64 {
			return nil, errors.New("分组名称不能为空且长度不能超过 64 个字符")
		}
		if probeModel == "" || utf8.RuneCountInString(probeModel) > 255 || strings.Contains(probeModel, "*") {
			return nil, errors.New("探测模型必须是长度不超过 255 的具体文本模型")
		}
		if utf8.RuneCountInString(displayInitial) > 1 {
			return nil, errors.New("分组展示字只能配置一个字符")
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
	for _, execution := range executions {
		if execution.FinishedAt < minimumBucket || execution.FinishedAt > now {
			continue
		}
		startedAt := model.ChannelStatusProbeDisplayBucketStart(execution.FinishedAt, displayUnit)
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
		bucket.Result = channelGroupMonitorBucketResult(bucket)
		groupBuckets[startedAt] = bucket
	}
	result := make(map[string][]channelGroupMonitorBucketResponse, len(bucketsByGroup))
	for groupName, groupBuckets := range bucketsByGroup {
		buckets := make([]channelGroupMonitorBucketResponse, 0, displayValue)
		for startedAt := minimumBucket; startedAt <= currentBucket; startedAt += bucketSeconds {
			bucket := groupBuckets[startedAt]
			bucket.StartedAt = startedAt
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
	case model.ChannelGroupMonitorResultUnavailable:
		return channelGroupMonitorHealthUnavailable
	case model.ChannelGroupMonitorResultUpstreamFailure, model.ChannelGroupMonitorResultLocalFailure:
		return channelGroupMonitorHealthUnhealthy
	default:
		return channelGroupMonitorHealthPending
	}
}

func buildChannelGroupMonitorItems(config model.ChannelGroupMonitorConfig, includeInvalid bool, usableGroups map[string]string, now int64) ([]channelGroupMonitorItemResponse, error) {
	groups, err := config.Groups()
	if err != nil {
		return nil, err
	}
	validCandidates, err := getChannelGroupMonitorCandidateModels(true)
	if err != nil {
		return nil, err
	}
	states, err := model.GetChannelGroupMonitorStates()
	if err != nil {
		return nil, err
	}
	stateByGroup := make(map[string]model.ChannelGroupMonitorState, len(states))
	for _, state := range states {
		stateByGroup[state.GroupName] = state
	}
	displayValue, displayUnit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
	summaries, err := model.GetChannelGroupMonitorExecutionSummariesSince(
		now - channelGroupMonitorDisplaySeconds(displayValue, displayUnit),
	)
	if err != nil {
		return nil, err
	}
	type summary struct{ success, completed int }
	summaryByGroup := make(map[string]summary)
	for _, item := range summaries {
		current := summaryByGroup[item.GroupName]
		current.completed += int(item.ResultCount)
		if item.Result == model.ChannelGroupMonitorResultSuccess {
			current.success += int(item.ResultCount)
		}
		summaryByGroup[item.GroupName] = current
	}
	windowStart := model.ChannelStatusProbeDisplayBucketStart(now, displayUnit) -
		int64(displayValue-1)*model.ChannelStatusProbeDisplayBucketSeconds(displayUnit)
	executions, err := model.GetChannelGroupMonitorExecutionWindowSince(windowStart)
	if err != nil {
		return nil, err
	}
	recentWindows := mergeChannelGroupMonitorRecentWindow(executions, now, displayValue, displayUnit)
	for _, group := range groups {
		if _, exists := recentWindows[group.GroupName]; !exists {
			recentWindows[group.GroupName] = emptyChannelGroupMonitorRecentWindow(now, displayValue, displayUnit)
		}
	}
	items := make([]channelGroupMonitorItemResponse, 0, len(groups))
	for _, group := range groups {
		if usableGroups != nil {
			if _, allowed := usableGroups[group.GroupName]; !allowed {
				continue
			}
		}
		configValid := groupMonitorModelIsCandidate(validCandidates, group.GroupName, group.ProbeModel)
		if !configValid && !includeInvalid {
			continue
		}
		item := channelGroupMonitorItemResponse{
			Group: group.GroupName, Initial: channelGroupMonitorInitial(group.GroupName, group.DisplayInitial), ProbeModel: group.ProbeModel,
			ConfigValid: configValid, RecentWindow: recentWindows[group.GroupName],
		}
		if !configValid {
			item.Status = channelGroupMonitorHealthUnconfigured
			items = append(items, item)
			continue
		}
		window := summaryByGroup[group.GroupName]
		item.SuccessCount = window.success
		item.CompletedCount = window.completed
		if window.completed > 0 {
			rate := float64(window.success) * 100 / float64(window.completed)
			item.SuccessRate = &rate
		}
		if state, exists := stateByGroup[group.GroupName]; exists {
			item.Status = channelGroupMonitorHealth(config, &state, now)
			item.LatestFirstTokenMs = state.FirstTokenMs
			item.LastFinishedAt = state.FinishedAt
			item.LatestResult = state.Result
			item.LastSuccessAt = state.LastSuccessAt
			item.LastFailureAt = state.LastFailureAt
			item.ConsecutiveSuccess = state.ConsecutiveSuccess
			item.ConsecutiveFailure = state.ConsecutiveFailure
		} else {
			item.Status = channelGroupMonitorHealth(config, nil, now)
		}
		items = append(items, item)
	}
	return items, nil
}

func GetChannelGroupMonitorSettings(c *gin.Context) {
	config, err := model.GetChannelGroupMonitorConfigOrDefault()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response, err := channelGroupMonitorConfigToResponse(config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	candidates, err := getChannelGroupMonitorCandidateModels(true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"settings": response, "candidate_models_by_group": candidates,
	}})
}

func UpdateChannelGroupMonitorSettings(c *gin.Context) {
	var request channelGroupMonitorConfigRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.Enabled == nil || request.Groups == nil ||
		request.IntervalSeconds == nil || request.DisplayValue == nil || request.DisplayUnit == nil || request.Revision == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "分组监控配置参数不完整"})
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
	candidates, err := getChannelGroupMonitorCandidateModels(true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	groups, err := normalizeChannelGroupMonitorGroups(*request.Groups, candidates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	saved, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled: *request.Enabled, Groups: groups, IntervalSeconds: *request.IntervalSeconds,
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
	response, err := channelGroupMonitorConfigToResponse(saved)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.group_monitor_config_changed", map[string]any{
		"enabled": *request.Enabled, "groups": groups, "group_count": len(groups),
		"interval_seconds": *request.IntervalSeconds, "display_value": *request.DisplayValue,
		"display_unit": *request.DisplayUnit,
	})
	wakeChannelGroupMonitorWorker()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": response})
}

func GetChannelGroupMonitorOverview(c *gin.Context) {
	now := common.GetTimestamp()
	config, err := model.GetChannelGroupMonitorConfigOrDefault()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	settings, err := channelGroupMonitorConfigToResponse(config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items, err := buildChannelGroupMonitorItems(config, true, nil, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	candidates, err := getChannelGroupMonitorCandidateModels(true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": channelGroupMonitorOverviewResponse{
		ServerNow: now, Settings: settings, CandidateModelsByGroup: candidates, Items: items,
	}})
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
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "每页数量必须在 1 到 100 之间"})
		return
	}
	items, total, err := model.ListChannelGroupMonitorExecutions(page, pageSize, c.Query("group"), c.Query("result"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"page": page, "page_size": pageSize, "total": total, "items": items,
	}})
}

func GetPricingGroupMonitor(c *gin.Context) {
	now := common.GetTimestamp()
	config, err := model.GetChannelGroupMonitorConfigOrDefault()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	userGroup := ""
	if userId, exists := c.Get("id"); exists {
		if user, userErr := model.GetUserCache(userId.(int)); userErr == nil {
			userGroup = user.Group
		}
	}
	usableGroups := service.GetRoleUsableGroups(userGroup, c.GetInt("role"))
	items, err := buildChannelGroupMonitorItems(config, false, usableGroups, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	publicItems := make([]pricingGroupMonitorItemResponse, 0, len(items))
	for _, item := range items {
		publicItems = append(publicItems, pricingGroupMonitorItemResponse{
			Group:              item.Group,
			Initial:            item.Initial,
			Status:             item.Status,
			ProbeModel:         item.ProbeModel,
			LatestFirstTokenMs: item.LatestFirstTokenMs,
			SuccessRate:        item.SuccessRate,
			LastFinishedAt:     item.LastFinishedAt,
			RecentWindow:       item.RecentWindow,
		})
	}
	displayValue, displayUnit := model.NormalizeChannelStatusProbeDisplay(config.DisplayValue, config.DisplayUnit)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{
		"enabled": config.Enabled, "server_now": now,
		"data_cutoff_at": now - channelGroupMonitorDisplaySeconds(displayValue, displayUnit),
		"display_value":  displayValue, "display_unit": displayUnit, "items": publicItems,
	}})
}
