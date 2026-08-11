package controller

import (
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
	StartedAt       int64    `json:"started_at"`
	Success         int      `json:"success"`
	UpstreamFailure int      `json:"upstream_failure"`
	RateLimited     int      `json:"rate_limited"`
	LocalFailure    int      `json:"local_failure"`
	Skipped         int      `json:"skipped"`
	Canceled        int      `json:"canceled"`
	Models          []string `json:"models,omitempty"`
	Result          string   `json:"result"`
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
	AvgFirstTokenMs    *float64                          `json:"avg_first_token_ms"`
	AvgTPS             *float64                          `json:"avg_tps"`
	ModelStatuses      []channelStatusProbeModelResponse `json:"model_statuses"`
	ConfiguredModelNum int                               `json:"configured_model_count"`
}

type channelStatusProbeOverviewResponse struct {
	ServerNow          int64                               `json:"server_now"`
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

func channelStatusProbeHealth(
	config *channelStatusProbeConfigResponse,
	channelStatus int,
	states map[string]model.ChannelStatusProbeState,
	now int64,
) string {
	if config == nil || len(config.Models) == 0 {
		return channelStatusProbeHealthUnconfigured
	}
	if !config.Enabled || channelStatus == common.ChannelStatusManuallyDisabled {
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
	Buckets               []channelStatusProbeBucketResponse
	FirstTokenTotalMs     float64
	FirstTokenSampleCount int64
	TPSTotal              float64
	TPSSampleCount        int64
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
			for _, modelName := range bucket.Models {
				current.Add("", modelName, nil, nil)
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
		})
		summary.FirstTokenTotalMs += bucket.FirstTokenTotalMs
		summary.FirstTokenSampleCount += bucket.FirstTokenSampleCount
		summary.TPSTotal += bucket.TPSTotal
		summary.TPSSampleCount += bucket.TPSSampleCount
	}
	return summary, nil
}

func GetChannelStatusProbeOverview(c *gin.Context) {
	now := common.GetTimestamp()
	selectedModel := strings.TrimSpace(c.Query("model"))
	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	configs, err := model.GetChannelStatusProbeConfigs()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	states, err := model.GetChannelStatusProbeStates()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	monitors, err := model.GetChannelRatioMonitors()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	configByChannel := make(map[int]model.ChannelStatusProbeConfig, len(configs))
	for _, config := range configs {
		configByChannel[config.ChannelId] = config
	}
	statesByChannel := make(map[int][]model.ChannelStatusProbeState)
	for _, state := range states {
		statesByChannel[state.ChannelId] = append(statesByChannel[state.ChannelId], state)
	}
	monitorByChannel := make(map[int]model.ChannelRatioMonitor, len(monitors))
	for _, monitor := range monitors {
		monitorByChannel[monitor.ChannelId] = monitor
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
				common.ApiError(c, convertErr)
				return
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
			windowSummary, mergeErr := mergeChannelStatusProbeRecentWindow(
				statesForModel,
				now,
				displayValue,
				displayUnit,
			)
			if mergeErr != nil {
				common.ApiError(c, mergeErr)
				return
			}
			modelConfig := *configResponse
			modelConfig.Models = []string{modelName}
			modelHealth := channelStatusProbeHealth(&modelConfig, channel.Status, stateByModel, now)
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
		health := channelStatusProbeHealth(healthConfig, channel.Status, stateByModel, now)
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
			ModelStatuses: modelStatuses, ConfiguredModelNum: configuredModelCount,
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
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": channelStatusProbeOverviewResponse{
			ServerNow: now, ScanIntervalSecond: 1, Summary: summary, Groups: groups, Models: models,
			ModelsByGroup: modelsByGroup, Channels: items,
		},
	})
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
	models, err := normalizeChannelStatusProbeModels(channel, *request.Models)
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
	response, err := channelStatusProbeConfigToResponse(saved)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.status_probe_config_update", map[string]any{
		"channel_id": channelId, "enabled": *request.Enabled, "models": models,
		"interval_seconds": *request.IntervalSeconds, "display_value": *request.DisplayValue,
		"display_unit":  *request.DisplayUnit,
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
	recordManageAudit(c, "channel.status_probe_run", map[string]any{"channel_id": channelId, "manual_request_id": requestId})
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
