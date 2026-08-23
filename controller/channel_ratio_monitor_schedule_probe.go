package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

const channelMonitorSmartScheduleProbeTaskType = "channel_smart_schedule_probe"

type channelSmartScheduleProbeTestOptions struct {
	Group          string
	ScheduledProbe bool
}

type channelSmartScheduleProbeTestContextKey struct{}

func withChannelSmartScheduleProbeTestContext(ctx context.Context, group string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, channelSmartScheduleProbeTestContextKey{}, channelSmartScheduleProbeTestOptions{
		Group:          group,
		ScheduledProbe: true,
	})
	return withChannelMonitorSchedulingEligibility(ctx, true)
}

func applyChannelSmartScheduleProbeTestContext(source context.Context, target *gin.Context) {
	if source == nil || target == nil {
		return
	}
	options, ok := source.Value(channelSmartScheduleProbeTestContextKey{}).(channelSmartScheduleProbeTestOptions)
	if !ok || options.Group == "" {
		return
	}
	common.SetContextKey(target, constant.ContextKeyUsingGroup, options.Group)
}

func isChannelSmartScheduleProbeTest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	options, ok := ctx.Value(channelSmartScheduleProbeTestContextKey{}).(channelSmartScheduleProbeTestOptions)
	return ok && options.Group != "" && options.ScheduledProbe
}

type channelSmartScheduleProbeTaskHandler struct{}

type channelSmartScheduleProbeFailure struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Group       string `json:"group"`
	Model       string `json:"model"`
	Error       string `json:"error"`
}

type channelSmartScheduleProbeTaskResult struct {
	Total     int                                `json:"total"`
	Probed    int                                `json:"probed"`
	Succeeded int                                `json:"succeeded"`
	Failed    int                                `json:"failed"`
	Skipped   int                                `json:"skipped"`
	Failures  []channelSmartScheduleProbeFailure `json:"failures,omitempty"`
}

func channelSmartScheduleSupportsTextProbe(channel *model.Channel, modelName string) bool {
	if channel == nil {
		return false
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}

	switch channel.Type {
	case constant.ChannelTypeMidjourney,
		constant.ChannelTypeMidjourneyPlus,
		constant.ChannelTypeSunoAPI,
		constant.ChannelTypeJina,
		constant.ChannelTypeMokaAI,
		constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeVidu,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeSora,
		constant.ChannelTypeReplicate:
		return false
	}

	normalizedModel := strings.ToLower(modelName)
	if common.IsImageGenerationModel(normalizedModel) ||
		model_setting.IsSyncImageModel(modelName) ||
		model_setting.IsGeminiModelSupportImagine(modelName) {
		return false
	}
	for _, marker := range []string{
		"embedding", "embed", "rerank", "moderation",
		"audio", "realtime", "speech", "transcription", "whisper", "tts",
		"image", "imagen", "flux", "seedream", "stable-diffusion", "sdxl",
		"video", "sora", "veo-", "kling", "suno", "music",
	} {
		if strings.Contains(normalizedModel, marker) {
			return false
		}
	}
	if strings.HasPrefix(normalizedModel, "m3e") || strings.Contains(normalizedModel, "bge-") {
		return false
	}

	if channel.Type == constant.ChannelTypeAdvancedCustom {
		advancedCustom := channel.GetOtherSettings().AdvancedCustom
		return advancedCustom != nil && advancedCustom.SupportsPathForModel("/v1/responses", modelName)
	}
	apiType, _ := common.ChannelType2APIType(channel.Type)
	if apiType == constant.APITypeOpenAI {
		return true
	}
	switch channel.Type {
	case constant.ChannelTypeAli,
		constant.ChannelTypeGemini,
		constant.ChannelCloudflare,
		constant.ChannelTypePerplexity,
		constant.ChannelTypeVolcEngine,
		constant.ChannelTypeXai,
		constant.ChannelTypeCodex,
		constant.ChannelTypeSub2API:
		return true
	default:
		return false
	}
}

func init() {
	service.RegisterSystemTaskHandler(channelSmartScheduleProbeTaskHandler{})
}

func (channelSmartScheduleProbeTaskHandler) Type() string {
	return channelMonitorSmartScheduleProbeTaskType
}

func (channelSmartScheduleProbeTaskHandler) Enabled() bool {
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return false
	}
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		if policy.SampleMode == channelMonitorSmartScheduleSampleProbe ||
			(policy.StabilityEnabled && policy.DegradedProbeEnabled) {
			return true
		}
	}
	return false
}

func (channelSmartScheduleProbeTaskHandler) Interval() time.Duration {
	minimumMinutes := 0
	settings := getChannelMonitorSettings()
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		if policy.SampleMode != channelMonitorSmartScheduleSampleProbe &&
			!(policy.StabilityEnabled && policy.DegradedProbeEnabled) {
			continue
		}
		if minimumMinutes == 0 || policy.ProbeIntervalMinutes < minimumMinutes {
			minimumMinutes = policy.ProbeIntervalMinutes
		}
	}
	if minimumMinutes <= 0 {
		minimumMinutes = defaultChannelMonitorSmartScheduleProbeInterval
	}
	return time.Duration(minimumMinutes) * time.Minute
}

func (channelSmartScheduleProbeTaskHandler) NewPayload() any { return nil }

func (channelSmartScheduleProbeTaskHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := runChannelSmartScheduleProbeOnce(
		ctx,
		service.NewSystemTaskProgressReporter(task, runnerID),
	)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, summary, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

func runChannelSmartScheduleProbeOnce(
	ctx context.Context,
	reportProgress func(processed, total int),
) (channelSmartScheduleProbeTaskResult, error) {
	result := channelSmartScheduleProbeTaskResult{}
	if reportProgress == nil {
		reportProgress = func(int, int) {}
	}
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return result, fmt.Errorf("智能调度已禁用")
	}
	policyByGroup := make(map[string]channelSmartSchedulePolicy)
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policy := configured.policy()
		if policy.SampleMode == channelMonitorSmartScheduleSampleProbe ||
			(policy.StabilityEnabled && policy.DegradedProbeEnabled) {
			policyByGroup[configured.Group] = policy
		}
	}
	if len(policyByGroup) == 0 {
		return result, nil
	}
	if err := model.InitializeChannelSmartScheduleRouteStates(); err != nil {
		return result, err
	}
	routes, err := model.GetChannelSmartScheduleRoutes()
	if err != nil {
		return result, err
	}
	type probeSubjectKey struct {
		logicalChannelId int64
		logicalRevision  int64
		model            string
	}
	type logicalSnapshotKey struct {
		logicalChannelId int64
		logicalRevision  int64
	}
	identityByChannel := make(map[int]model.LogicalChannelIdentity)
	snapshotByLogicalKey := make(map[logicalSnapshotKey]model.LogicalChannelSelectionSnapshot)
	for _, route := range routes {
		if _, exists := identityByChannel[route.ChannelId]; exists {
			continue
		}
		identity, identityErr := model.ResolveChannelLogicalIdentity(route.ChannelId)
		if identityErr != nil {
			return result, identityErr
		}
		identityByChannel[route.ChannelId] = identity
		if identity.Revision <= 0 || identity.LogicalChannelID == int64(identity.ChannelID) {
			continue
		}
		snapshotKey := logicalSnapshotKey{
			logicalChannelId: identity.LogicalChannelID,
			logicalRevision:  identity.Revision,
		}
		if _, exists := snapshotByLogicalKey[snapshotKey]; exists {
			continue
		}
		snapshot, snapshotErr := model.GetLogicalChannelSelectionSnapshot(identity)
		if snapshotErr != nil {
			return result, snapshotErr
		}
		snapshotByLogicalKey[snapshotKey] = snapshot
	}
	requestModelByKey := make(map[probeSubjectKey]string)
	for _, route := range routes {
		routeModel := strings.TrimSpace(route.Model)
		if routeModel == "" || strings.Contains(routeModel, "*") {
			continue
		}
		identity := identityByChannel[route.ChannelId]
		key := probeSubjectKey{
			logicalChannelId: identity.LogicalChannelID,
			logicalRevision:  identity.Revision,
			model:            ratio_setting.FormatMatchingModelName(routeModel),
		}
		if current := requestModelByKey[key]; current == "" || routeModel < current {
			requestModelByKey[key] = routeModel
		}
	}
	selectedRoutes := make([]model.ChannelSmartScheduleRoute, 0, len(routes))
	for _, route := range routes {
		policy, configured := policyByGroup[route.Group]
		if !configured || (len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model)) {
			continue
		}
		selectedRoutes = append(selectedRoutes, route)
	}
	result.Total = len(selectedRoutes)
	if len(selectedRoutes) == 0 {
		reportProgress(0, 0)
		return result, nil
	}
	routesForSchedulingSnapshot := selectedRoutes
	if len(selectedRoutes) == 1 {
		identity := identityByChannel[selectedRoutes[0].ChannelId]
		if identity.Revision > 0 && identity.LogicalChannelID != int64(identity.ChannelID) {
			routesForSchedulingSnapshot = append(
				append([]model.ChannelSmartScheduleRoute(nil), selectedRoutes...),
				selectedRoutes[0],
			)
		}
	}
	schedulingRoutes, err := model.CoalesceChannelSmartScheduleSchedulingRoutes(routesForSchedulingSnapshot)
	if err != nil {
		return result, err
	}
	type probeRouteKey struct {
		subject probeSubjectKey
		group   string
		model   string
	}
	lastSampleTimeBySubject := make(map[probeSubjectKey]int64)
	schedulingRouteByKey := make(map[probeRouteKey]model.ChannelSmartScheduleRoute)
	for _, route := range schedulingRoutes {
		identity := identityByChannel[route.ChannelId]
		if route.LogicalChannelId > 0 {
			identity.LogicalChannelID = route.LogicalChannelId
			identity.Revision = route.LogicalRevision
		}
		key := probeSubjectKey{
			logicalChannelId: identity.LogicalChannelID,
			logicalRevision:  identity.Revision,
			model:            ratio_setting.FormatMatchingModelName(strings.TrimSpace(route.Model)),
		}
		lastSampleTimeBySubject[key] = max(lastSampleTimeBySubject[key], route.SharedSamples.LastTime)
		schedulingRouteByKey[probeRouteKey{
			subject: key, group: route.Group,
			model: ratio_setting.FormatMatchingModelName(strings.TrimSpace(route.Model)),
		}] = route
	}

	now := common.GetTimestamp()
	retentionMinutes := max(
		settings.SmartSchedulePerformanceWindowMinutes,
		settings.SmartScheduleGroupPolicies.maxStabilityWindowMinutes(),
	)
	retentionStart := now - int64(retentionMinutes*60)
	type dueProbe struct {
		routes          []model.ChannelSmartScheduleRoute
		requestModel    string
		identity        model.LogicalChannelIdentity
		logicalSnapshot *model.LogicalChannelSelectionSnapshot
	}
	probesByModel := make(map[probeSubjectKey]dueProbe)
	probeOrder := make([]probeSubjectKey, 0)
	for _, route := range selectedRoutes {
		routeModel := strings.TrimSpace(route.Model)
		normalizedModel := ratio_setting.FormatMatchingModelName(routeModel)
		identity := identityByChannel[route.ChannelId]
		key := probeSubjectKey{
			logicalChannelId: identity.LogicalChannelID,
			logicalRevision:  identity.Revision,
			model:            normalizedModel,
		}
		probe, exists := probesByModel[key]
		if !exists {
			probeOrder = append(probeOrder, key)
			probe.identity = identity
			if identity.Revision > 0 && identity.LogicalChannelID != int64(identity.ChannelID) {
				snapshotKey := logicalSnapshotKey{
					logicalChannelId: identity.LogicalChannelID,
					logicalRevision:  identity.Revision,
				}
				snapshot := snapshotByLogicalKey[snapshotKey]
				probe.logicalSnapshot = &snapshot
			}
		}
		probe.routes = append(probe.routes, route)
		probe.requestModel = requestModelByKey[key]
		probesByModel[key] = probe
	}
	result.Total = len(probeOrder)
	due := make([]dueProbe, 0, len(probeOrder))
	for _, key := range probeOrder {
		probe := probesByModel[key]
		if probe.requestModel == "" {
			result.Skipped++
			continue
		}
		if probe.logicalSnapshot == nil && service.ChannelRateLimitCooldownUntilMatching(
			probe.identity.ChannelID, probe.requestModel,
		) > 0 {
			result.Skipped++
			continue
		}
		eligibleRoutes := make([]model.ChannelSmartScheduleRoute, 0, len(probe.routes))
		minimumIntervalMinutes := 0
		for _, route := range probe.routes {
			state := route.State
			if probe.logicalSnapshot != nil {
				schedulingRoute, exists := schedulingRouteByKey[probeRouteKey{
					subject: key, group: route.Group,
					model: ratio_setting.FormatMatchingModelName(strings.TrimSpace(route.Model)),
				}]
				if !exists {
					continue
				}
				state = schedulingRoute.State
			}
			if route.ChannelStatus != common.ChannelStatusEnabled || !route.Enabled || !state.Participates() ||
				!channelSmartScheduleProbeRouteEnabled(state.StabilityState, policyByGroup[route.Group]) {
				continue
			}
			eligibleRoutes = append(eligibleRoutes, route)
			intervalMinutes := policyByGroup[route.Group].ProbeIntervalMinutes
			if minimumIntervalMinutes == 0 || intervalMinutes < minimumIntervalMinutes {
				minimumIntervalMinutes = intervalMinutes
			}
		}
		if len(eligibleRoutes) == 0 {
			result.Skipped++
			continue
		}
		lastSampleTime := lastSampleTimeBySubject[key]
		if lastSampleTime > 0 && now-lastSampleTime < int64(minimumIntervalMinutes*60) {
			result.Skipped++
			continue
		}
		probe.routes = eligibleRoutes
		due = append(due, probe)
	}
	if len(due) == 0 {
		reportProgress(result.Total, result.Total)
		return result, nil
	}

	channelIds := make([]int, 0, len(due))
	seenChannelIds := make(map[int]struct{}, len(due))
	for _, item := range due {
		for _, route := range item.routes {
			channelId := route.ChannelId
			if _, exists := seenChannelIds[channelId]; exists {
				continue
			}
			seenChannelIds[channelId] = struct{}{}
			channelIds = append(channelIds, channelId)
		}
	}
	channels, err := model.GetChannelsByIds(channelIds)
	if err != nil {
		return result, err
	}
	channelById := make(map[int]*model.Channel, len(channels))
	for _, channel := range channels {
		channelById[channel.Id] = channel
	}
	probeable := make([]dueProbe, 0, len(due))
	for _, item := range due {
		supported := false
		for _, route := range item.routes {
			channel := channelById[route.ChannelId]
			if channel == nil {
				return result, fmt.Errorf("智能调度探测渠道 %d 不存在", route.ChannelId)
			}
			if channelSmartScheduleSupportsTextProbe(channel, item.requestModel) {
				supported = true
				break
			}
		}
		if !supported {
			result.Skipped++
			continue
		}
		probeable = append(probeable, item)
	}
	if len(probeable) == 0 {
		reportProgress(result.Total, result.Total)
		return result, nil
	}
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return result, err
	}
	processed := result.Skipped
	for _, item := range probeable {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		var route model.ChannelSmartScheduleRoute
		var channel *model.Channel
		var probeCtx context.Context
		var lease *service.ChannelConcurrencyLease
		type eligibleProbeMember struct {
			route   model.ChannelSmartScheduleRoute
			channel *model.Channel
		}
		eligibleByChannel := make(map[int]eligibleProbeMember)
		candidateRoutes := make([]model.ChannelSmartScheduleRoute, 0, len(item.routes))
		for _, candidateRoute := range item.routes {
			if candidateRoute.Model == item.requestModel {
				candidateRoutes = append(candidateRoutes, candidateRoute)
			}
		}
		for _, candidateRoute := range item.routes {
			if candidateRoute.Model != item.requestModel {
				candidateRoutes = append(candidateRoutes, candidateRoute)
			}
		}
		for _, candidateRoute := range candidateRoutes {
			if _, exists := eligibleByChannel[candidateRoute.ChannelId]; exists {
				continue
			}
			var currentRoute model.ChannelSmartScheduleRoute
			var currentChannel *model.Channel
			var eligible bool
			var eligibilityErr error
			if item.logicalSnapshot == nil {
				currentRoute, currentChannel, _, eligible, eligibilityErr = channelSmartScheduleProbeEligibility(
					candidateRoute, item.requestModel,
				)
			} else {
				var found bool
				currentRoute, currentChannel, found, eligibilityErr = model.LookupChannelSmartScheduleProbeRoute(
					candidateRoute.ChannelId, candidateRoute.Group, candidateRoute.Model,
				)
				eligible = found && currentRoute.ChannelStatus == common.ChannelStatusEnabled && currentRoute.Enabled &&
					service.ChannelRateLimitCooldownUntilMatching(currentRoute.ChannelId, item.requestModel) <= 0
			}
			if eligibilityErr != nil {
				return result, eligibilityErr
			}
			if eligible && channelSmartScheduleSupportsTextProbe(currentChannel, currentRoute.Model) {
				eligibleByChannel[currentRoute.ChannelId] = eligibleProbeMember{
					route: currentRoute, channel: currentChannel,
				}
				if item.logicalSnapshot == nil {
					route = currentRoute
					channel = currentChannel
					break
				}
			}
		}
		for item.logicalSnapshot != nil && len(eligibleByChannel) > 0 {
			availability := make([]model.LogicalChannelMemberAvailability, 0, len(eligibleByChannel))
			for _, member := range item.logicalSnapshot.Members {
				if _, exists := eligibleByChannel[member.ChannelID]; !exists {
					continue
				}
				availability = append(availability, model.LogicalChannelMemberAvailability{
					ChannelID: member.ChannelID, Weight: member.Weight, Available: true,
				})
			}
			selectedChannelId, selectErr := model.SelectLogicalChannelMember(
				*item.logicalSnapshot, availability, nil,
			)
			if selectErr != nil {
				if errors.Is(selectErr, model.ErrLogicalChannelSelectionNoAvailableMembers) {
					break
				}
				return result, selectErr
			}
			selected := eligibleByChannel[selectedChannelId]
			selectedProbeCtx := withChannelSmartScheduleProbeTestContext(ctx, selected.route.Group)
			selectedLease, acquired, _, acquireErr := service.AcquireChannelConcurrency(
				selectedProbeCtx, selected.channel.Id,
			)
			if acquireErr != nil {
				return result, fmt.Errorf("获取智能调度探测渠道 %d 并发配额失败: %w", selected.channel.Id, acquireErr)
			}
			if !acquired {
				delete(eligibleByChannel, selectedChannelId)
				continue
			}
			route = selected.route
			channel = selected.channel
			probeCtx = selectedProbeCtx
			lease = selectedLease
			break
		}
		if channel == nil {
			result.Skipped++
			processed++
			reportProgress(processed, result.Total)
			continue
		}
		if lease == nil {
			probeCtx = withChannelSmartScheduleProbeTestContext(ctx, route.Group)
			var acquired bool
			var acquireErr error
			lease, acquired, _, acquireErr = service.AcquireChannelConcurrency(probeCtx, channel.Id)
			if acquireErr != nil {
				return result, fmt.Errorf("获取智能调度探测渠道 %d 并发配额失败: %w", channel.Id, acquireErr)
			}
			if !acquired {
				result.Skipped++
				processed++
				reportProgress(processed, result.Total)
				continue
			}
		}
		probeStartedAt := time.Now()
		probeResult := testChannel(
			probeCtx, channel, testUserID, item.requestModel,
			string(constant.EndpointTypeOpenAIResponse), true,
		)
		lease.Release()
		probeDurationMs := float64(time.Since(probeStartedAt)) / float64(time.Millisecond)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !probeResult.requestDispatched {
			result.Skipped++
			processed++
			reportProgress(processed, result.Total)
			continue
		}
		result.Probed++
		probeTime := common.GetTimestamp()
		succeeded := probeResult.localErr == nil && probeResult.newAPIError == nil
		rateLimited := isChannelSmartScheduleUpstreamRateLimit(probeResult)
		message := ""
		if !succeeded {
			if probeResult.localErr != nil {
				message = probeResult.localErr.Error()
			} else {
				message = probeResult.newAPIError.Error()
			}
			messageRunes := []rune(message)
			if len(messageRunes) > 255 {
				message = string(messageRunes[:255])
			}
		}
		if rateLimited {
			protectChannelSmartScheduleScheduledProbeFailure(
				route.ChannelId, item.requestModel, probeResult.newAPIError,
			)
		} else {
			recoveryRequest, recoveryErr := channelSmartScheduleProbeRecoveryRequest(
				route.ChannelId,
				item.requestModel,
				probeTime,
				true,
				message,
			)
			if recoveryErr != nil {
				return result, recoveryErr
			}
			sample := model.ChannelSmartScheduleModelSampleResult{
				ChannelId: route.ChannelId, Model: item.requestModel,
				Source:      model.ChannelSmartScheduleSampleSourceScheduledProbe,
				WindowStart: retentionStart, Time: probeTime, Success: succeeded, Error: message,
				DurationMs:    &probeDurationMs,
				FirstTokenMs:  probeResult.firstResponseMilliseconds,
				TPS:           probeResult.tokensPerSecond,
				ProbeRecovery: recoveryRequest,
			}
			writeApplied := true
			var saveErr error
			if item.logicalSnapshot != nil {
				if !model.IsLogicalChannelGroupingEnabled() {
					writeApplied = false
				} else {
					frozenIdentity := item.identity
					frozenIdentity.ChannelID = route.ChannelId
					_, saveErr = model.SaveLogicalChannelSmartScheduleModelSample(
						frozenIdentity, route.Group, sample,
					)
				}
			} else {
				_, saveErr = model.SaveChannelSmartScheduleModelSample(sample)
			}
			if saveErr != nil {
				if item.logicalSnapshot == nil ||
					(!errors.Is(saveErr, model.ErrChannelLogicalGroupRevisionConflict) &&
						!errors.Is(saveErr, model.ErrLogicalChannelSelectionGroupDisabled)) {
					return result, saveErr
				}
				writeApplied = false
			}
			if writeApplied {
				applyChannelSmartScheduleProbeRecoveryResult(route.ChannelId, item.requestModel, recoveryRequest)
			}
			if writeApplied && !succeeded && probeResult.newAPIError != nil {
				protectChannelSmartScheduleScheduledProbeFailureAfterRecoveryResult(
					route.ChannelId,
					item.requestModel,
					probeResult.newAPIError,
				)
			}
		}
		if succeeded {
			result.Succeeded++
		} else {
			result.Failed++
			route.Model = item.requestModel
			recordChannelSmartScheduleProbeError(
				probeResult.context,
				testUserID,
				route,
				probeResult.newAPIError,
				message,
				probeDurationMs,
			)
			common.SysError(fmt.Sprintf(
				"智能调度共享探测失败: channel_id=%d name=%s model=%s request_group=%s err=%s",
				route.ChannelId, route.ChannelName, route.Model, route.Group, message,
			))
			if len(result.Failures) < maxChannelSmartScheduleTaskFailureDetails {
				result.Failures = append(result.Failures, channelSmartScheduleProbeFailure{
					ChannelId: route.ChannelId, ChannelName: route.ChannelName,
					Group: route.Group, Model: route.Model, Error: message,
				})
			}
		}
		processed++
		reportProgress(processed, result.Total)
		if common.RequestInterval > 0 {
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(common.RequestInterval):
			}
		}
	}
	reportProgress(result.Total, result.Total)
	return result, nil
}

func recordChannelSmartScheduleProbeError(
	c *gin.Context,
	userID int,
	route model.ChannelSmartScheduleRoute,
	apiError *types.NewAPIError,
	message string,
	durationMs float64,
) {
	if c == nil {
		return
	}
	other := map[string]interface{}{
		model.ChannelMonitorSmartScheduleProbeLogKey: true,
		"request_path":                        "/v1/responses",
		"channel_id":                          route.ChannelId,
		"channel_name":                        route.ChannelName,
		"channel_monitor_attempt_duration_ms": int64(math.Max(0, math.Round(durationMs))),
	}
	content := message
	if apiError != nil {
		other["error_type"] = apiError.GetErrorType()
		other["error_code"] = apiError.GetErrorCode()
		other["status_code"] = apiError.StatusCode
		content = apiError.MaskSensitiveErrorWithStatusCode()
	} else {
		other["error_type"] = "probe_request_error"
		other["error_code"] = "probe_request_failed"
	}
	if strings.TrimSpace(content) == "" {
		content = "智能调度探测请求失败"
	}
	useTimeSeconds := int(math.Ceil(math.Max(0, durationMs) / 1000))
	model.RecordErrorLog(
		c,
		userID,
		route.ChannelId,
		route.Model,
		"智能调度探测",
		content,
		0,
		useTimeSeconds,
		true,
		route.Group,
		other,
		false,
	)
}

func channelSmartScheduleProbeEligibility(route model.ChannelSmartScheduleRoute, requestModel string) (
	model.ChannelSmartScheduleRoute,
	*model.Channel,
	channelSmartSchedulePolicy,
	bool,
	error,
) {
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled {
		return model.ChannelSmartScheduleRoute{}, nil, channelSmartSchedulePolicy{}, false, nil
	}
	var policy channelSmartSchedulePolicy
	policyConfigured := false
	for _, configured := range settings.SmartScheduleGroupPolicies {
		if configured.Group != route.Group {
			continue
		}
		policy = configured.policy()
		policyConfigured = (policy.SampleMode == channelMonitorSmartScheduleSampleProbe ||
			(policy.StabilityEnabled && policy.DegradedProbeEnabled)) &&
			(len(policy.Models) == 0 || slices.Contains(policy.Models, route.Model))
		break
	}
	if !policyConfigured {
		return model.ChannelSmartScheduleRoute{}, nil, channelSmartSchedulePolicy{}, false, nil
	}

	current, channel, found, err := model.LookupChannelSmartScheduleProbeRoute(
		route.ChannelId,
		route.Group,
		route.Model,
	)
	if err != nil {
		return model.ChannelSmartScheduleRoute{}, nil, channelSmartSchedulePolicy{}, false, err
	}
	if !found || current.ChannelStatus != common.ChannelStatusEnabled || !current.Enabled || !current.State.Participates() ||
		!channelSmartScheduleProbeRouteEnabled(current.State.StabilityState, policy) {
		return model.ChannelSmartScheduleRoute{}, nil, channelSmartSchedulePolicy{}, false, nil
	}
	if service.ChannelRateLimitCooldownUntilMatching(current.ChannelId, requestModel) > 0 {
		return model.ChannelSmartScheduleRoute{}, nil, channelSmartSchedulePolicy{}, false, nil
	}
	return current, channel, policy, true, nil
}

func channelSmartScheduleProbeRouteEnabled(stabilityState string, policy channelSmartSchedulePolicy) bool {
	if stabilityState == model.ChannelSmartScheduleStabilityDegraded {
		return policy.StabilityEnabled && policy.DegradedProbeEnabled
	}
	if policy.SampleMode != channelMonitorSmartScheduleSampleProbe {
		return false
	}
	return stabilityState == "" || stabilityState == model.ChannelSmartScheduleStabilityProbing
}

func channelSmartScheduleMergeSharedSamplePerformance(
	performance *channelSmartSchedulePerformance,
	state model.ChannelSmartScheduleModelSampleState,
	windowStart int64,
) *channelSmartSchedulePerformance {
	return channelSmartScheduleMergeSampleMetrics(performance, state.MetricsSince(windowStart))
}

func channelSmartScheduleMergeSampleMetrics(
	performance *channelSmartSchedulePerformance,
	metrics model.ChannelSmartScheduleSampleMetrics,
) *channelSmartSchedulePerformance {
	if metrics.SampleCount <= 0 {
		return performance
	}
	if performance == nil {
		performance = &channelSmartSchedulePerformance{}
	}
	performance.AverageFirstTokenMs, performance.FirstTokenSampleCount = channelSmartScheduleMergeSharedSampleAverage(
		performance.AverageFirstTokenMs,
		performance.FirstTokenSampleCount,
		metrics.AverageFirstTokenMs,
		metrics.FirstTokenSampleCount,
	)
	performance.FirstTokenDurationBuckets = append(
		performance.FirstTokenDurationBuckets,
		metrics.FirstTokenDurationBuckets...,
	)
	performance.FirstTokenDurationSampleCount, performance.FirstTokenP50Ms, performance.FirstTokenP95Ms,
		performance.WinsorizedAverageFirstTokenMs = model.SummarizeChannelMonitorDurationBuckets(
		performance.FirstTokenDurationBuckets,
	)
	performance.AverageTPS, performance.TPSSampleCount = channelSmartScheduleMergeSharedSampleAverage(
		performance.AverageTPS,
		performance.TPSSampleCount,
		metrics.AverageTPS,
		metrics.TPSSampleCount,
	)
	performance.StabilitySuccessCount += metrics.SuccessCount
	performance.StabilityFailureCount += metrics.FailureCount
	performance.StabilityRetryFailureCount += metrics.FailureCount
	performance.StabilityRetryFailureDurationTotalMs += metrics.FailureDurationTotalMs
	for _, probeBucket := range metrics.FailureDurationBuckets {
		merged := false
		for index := range performance.StabilityFailureDurationBuckets {
			bucket := &performance.StabilityFailureDurationBuckets[index]
			if bucket.LowerBoundMs == probeBucket.LowerBoundMs && bucket.UpperBoundMs == probeBucket.UpperBoundMs {
				bucket.Count += probeBucket.Count
				merged = true
				break
			}
		}
		if !merged {
			performance.StabilityFailureDurationBuckets = append(
				performance.StabilityFailureDurationBuckets, probeBucket,
			)
		}
	}
	performance.Stability = nil
	performance.StabilitySampleCount = performance.StabilitySuccessCount + performance.StabilityFailureCount
	return performance
}

func channelSmartScheduleMergeSharedSampleAverage(
	current *float64,
	currentCount int,
	probe *float64,
	probeCount int64,
) (*float64, int) {
	if probe == nil || probeCount <= 0 {
		return current, currentCount
	}
	if current == nil || currentCount <= 0 {
		value := *probe
		return &value, int(probeCount)
	}
	totalCount := int64(currentCount) + probeCount
	value := *current + (*probe-*current)*float64(probeCount)/float64(totalCount)
	return &value, int(totalCount)
}
