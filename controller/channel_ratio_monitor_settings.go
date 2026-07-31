package controller

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	channelMonitorAutoUpdateIntervalOption                 = "ChannelMonitorAutoUpdateIntervalMinutes"
	channelMonitorAutoUpdateRetryCountOption               = "ChannelMonitorAutoUpdateRetryCount"
	channelMonitorUpstreamRequestTimeoutOption             = "ChannelMonitorUpstreamRequestTimeoutSeconds"
	channelMonitorAutoUpdateConsecutiveFailureLimitOption  = "ChannelMonitorAutoUpdateConsecutiveFailureLimit"
	channelMonitorAutoDisableOnUpdateFailureOption         = "ChannelMonitorAutoDisableOnUpdateFailure"
	channelMonitorAutoEnableOnCostRatioRecoveryOption      = "ChannelMonitorAutoEnableOnCostRatioRecovery"
	channelMonitorAutoEnableOnBalanceRecoveryOption        = "ChannelMonitorAutoEnableOnBalanceRecovery"
	channelMonitorCostRetentionDaysOption                  = "ChannelMonitorCostRetentionDays"
	channelMonitorEmailNotificationOption                  = "ChannelMonitorEmailNotificationEnabled"
	channelMonitorNotificationEmailOption                  = "ChannelMonitorNotificationEmail"
	channelMonitorEmailNotificationTypesOption             = "ChannelMonitorEmailNotificationTypes"
	channelMonitorProbeResponseOption                      = channelprobe.OptionKey
	channelMonitorGroupCoefficientsOption                  = "ChannelMonitorGroupCoefficients"
	channelMonitorChannelOrderOption                       = "ChannelMonitorChannelOrder"
	channelMonitorSmartScheduleEnabledOption               = "ChannelMonitorSmartScheduleEnabled"
	channelMonitorSmartScheduleIntervalOption              = "ChannelMonitorSmartScheduleIntervalMinutes"
	channelMonitorSmartScheduleRangeOption                 = "ChannelMonitorSmartSchedulePerformanceMinutes"
	channelMonitorSmartScheduleControlRevisionOption       = model.ChannelSmartScheduleControlRevisionOption
	channelMonitorPolicyActionNone                         = "none"
	channelMonitorPolicyActionUpdateGroupRatio             = "update_group_ratio"
	channelMonitorPolicyActionDisableChannel               = "disable_channel"
	channelMonitorPolicyActionRemoveFromGroup              = "remove_from_group"
	channelMonitorSmartScheduleStrategyRatio               = "ratio"
	channelMonitorSmartScheduleStrategyFirstToken          = "first_token"
	channelMonitorSmartScheduleStrategyTPS                 = "tps"
	channelMonitorSmartScheduleStrategySmart               = "smart"
	channelMonitorSmartScheduleApplyWeight                 = "weight"
	channelMonitorSmartScheduleApplyPriorityWeight         = "priority_weight"
	channelMonitorSmartScheduleSampleOff                   = "off"
	channelMonitorSmartScheduleSampleTraffic               = "traffic"
	channelMonitorSmartScheduleSampleProbe                 = "probe"
	maxChannelMonitorAutoUpdateIntervalMinutes             = 525600
	maxChannelMonitorAutoUpdateRetryCount                  = 10
	minChannelMonitorUpstreamRequestTimeoutSeconds         = 1
	maxChannelMonitorUpstreamRequestTimeoutSeconds         = 600
	minChannelMonitorAutoUpdateConsecutiveFailureLimit     = 1
	maxChannelMonitorAutoUpdateConsecutiveFailureLimit     = 100
	minChannelMonitorCostRetentionDays                     = 1
	maxChannelMonitorCostRetentionDays                     = 3650
	maxChannelMonitorNotificationEmailLength               = 254
	maxChannelMonitorChannelOrderCount                     = 100000
	maxChannelMonitorSmartScheduleModelLength              = 255
	maxChannelMonitorSmartScheduleModelCount               = 100
	maxChannelMonitorSmartScheduleGroupCount               = 100
	maxChannelMonitorSmartScheduleGroupLength              = 64
	maxChannelMonitorSmartScheduleMinSamples               = 100000
	maxChannelMonitorSmartScheduleSuccessRate              = 100
	maxChannelMonitorSmartScheduleExplorationPercent       = 20
	defaultChannelMonitorAutoUpdateRetryCount              = 2
	defaultChannelMonitorUpstreamRequestTimeoutSeconds     = 30
	defaultChannelMonitorAutoUpdateConsecutiveFailureLimit = 2
	defaultChannelMonitorCostRetentionDays                 = 120
	defaultChannelMonitorGroupCoefficient                  = 1
	defaultChannelMonitorSmartScheduleInterval             = 10
	defaultChannelMonitorSmartScheduleRange                = 60
	channelMonitorEmailTypeRatioChange                     = "ratio_change"
	channelMonitorEmailTypeBalanceWarning                  = "balance_warning"
	channelMonitorEmailTypeChannelDisabled                 = "channel_disabled"
	channelMonitorEmailTypeGroupMembershipRemoved          = "group_membership_removed"
	channelMonitorEmailTypeUpstreamSyncFailed              = "upstream_sync_failed"
	channelMonitorEmailTypeTaskFailed                      = "task_failed"
)

var channelMonitorEmailNotificationTypes = []string{
	channelMonitorEmailTypeRatioChange,
	channelMonitorEmailTypeBalanceWarning,
	channelMonitorEmailTypeChannelDisabled,
	channelMonitorEmailTypeGroupMembershipRemoved,
	channelMonitorEmailTypeUpstreamSyncFailed,
	channelMonitorEmailTypeTaskFailed,
}

type channelMonitorSettings struct {
	AutoUpdateIntervalMinutes          int                        `json:"auto_update_interval_minutes"`
	AutoUpdateRetryCount               int                        `json:"auto_update_retry_count"`
	UpstreamRequestTimeoutSeconds      int                        `json:"upstream_request_timeout_seconds"`
	AutoUpdateConsecutiveFailureLimit  int                        `json:"auto_update_consecutive_failure_limit"`
	AutoDisableOnUpdateFailure         bool                       `json:"auto_disable_on_update_failure"`
	AutoEnableOnCostRatioRecovery      bool                       `json:"auto_enable_on_cost_ratio_recovery"`
	AutoEnableOnBalanceRecovery        bool                       `json:"auto_enable_on_balance_recovery"`
	CostRetentionDays                  int                        `json:"cost_retention_days"`
	EmailNotificationEnabled           bool                       `json:"email_notification_enabled"`
	NotificationEmail                  string                     `json:"notification_email"`
	EmailNotificationTypes             []string                   `json:"email_notification_types"`
	ProbeResponseEnabled               bool                       `json:"probe_response_enabled"`
	RelayHeaderTimeoutSeconds          int                        `json:"relay_response_header_timeout_seconds"`
	SmartScheduleEnabled               bool                       `json:"smart_schedule_enabled"`
	SmartScheduleGroupPolicies         smartScheduleGroupPolicies `json:"smart_schedule_group_policies"`
	SmartScheduleIntervalMinutes       int                        `json:"smart_schedule_interval_minutes"`
	SmartSchedulePerformanceMinutes    int                        `json:"smart_schedule_performance_minutes"`
	SmartScheduleControlRevision       string                     `json:"-"`
	SmartScheduleForceResetTaskCreated *bool                      `json:"smart_schedule_force_reset_task_created,omitempty"`
	SmartScheduleForceResetTaskId      string                     `json:"smart_schedule_force_reset_task_id,omitempty"`
	SmartScheduleForceResetTaskError   string                     `json:"smart_schedule_force_reset_task_error,omitempty"`
}

type channelMonitorSettingsUpdateRequest struct {
	AutoUpdateIntervalMinutes         *int                        `json:"auto_update_interval_minutes"`
	AutoUpdateRetryCount              *int                        `json:"auto_update_retry_count"`
	UpstreamRequestTimeoutSeconds     *int                        `json:"upstream_request_timeout_seconds"`
	AutoUpdateConsecutiveFailureLimit *int                        `json:"auto_update_consecutive_failure_limit"`
	AutoDisableOnUpdateFailure        *bool                       `json:"auto_disable_on_update_failure"`
	AutoEnableOnCostRatioRecovery     *bool                       `json:"auto_enable_on_cost_ratio_recovery"`
	AutoEnableOnBalanceRecovery       *bool                       `json:"auto_enable_on_balance_recovery"`
	CostRetentionDays                 *int                        `json:"cost_retention_days"`
	EmailNotificationEnabled          *bool                       `json:"email_notification_enabled"`
	NotificationEmail                 *string                     `json:"notification_email"`
	EmailNotificationTypes            *[]string                   `json:"email_notification_types"`
	ProbeResponseEnabled              *bool                       `json:"probe_response_enabled"`
	RelayHeaderTimeoutSeconds         *int                        `json:"relay_response_header_timeout_seconds"`
	SmartScheduleEnabled              *bool                       `json:"smart_schedule_enabled"`
	SmartScheduleGroupPolicies        *smartScheduleGroupPolicies `json:"smart_schedule_group_policies"`
	SmartScheduleIntervalMinutes      *int                        `json:"smart_schedule_interval_minutes"`
	SmartSchedulePerformanceMinutes   *int                        `json:"smart_schedule_performance_minutes"`
	SmartScheduleForceReset           *bool                       `json:"smart_schedule_force_reset"`
}

type channelMonitorOrderUpdateRequest struct {
	ChannelIds *[]int `json:"channel_ids"`
}

func getChannelMonitorSettings() channelMonitorSettings {
	common.OptionMapRWMutex.RLock()
	rawInterval := common.OptionMap[channelMonitorAutoUpdateIntervalOption]
	rawRetryCount := common.OptionMap[channelMonitorAutoUpdateRetryCountOption]
	rawUpstreamRequestTimeout := common.OptionMap[channelMonitorUpstreamRequestTimeoutOption]
	rawConsecutiveFailureLimit := common.OptionMap[channelMonitorAutoUpdateConsecutiveFailureLimitOption]
	rawAutoDisableOnUpdateFailure := common.OptionMap[channelMonitorAutoDisableOnUpdateFailureOption]
	rawAutoEnableOnCostRatioRecovery := common.OptionMap[channelMonitorAutoEnableOnCostRatioRecoveryOption]
	rawAutoEnableOnBalanceRecovery := common.OptionMap[channelMonitorAutoEnableOnBalanceRecoveryOption]
	rawCostRetentionDays := common.OptionMap[channelMonitorCostRetentionDaysOption]
	rawEmailNotificationEnabled := common.OptionMap[channelMonitorEmailNotificationOption]
	rawNotificationEmail := common.OptionMap[channelMonitorNotificationEmailOption]
	rawEmailNotificationTypes := common.OptionMap[channelMonitorEmailNotificationTypesOption]
	rawProbeResponseEnabled := common.OptionMap[channelMonitorProbeResponseOption]
	rawRelayResponseHeaderTimeout := common.OptionMap[common.RelayResponseHeaderTimeoutOptionKey]
	rawSmartScheduleEnabled := common.OptionMap[channelMonitorSmartScheduleEnabledOption]
	rawSmartScheduleGroupPolicies := common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption]
	rawSmartScheduleInterval := common.OptionMap[channelMonitorSmartScheduleIntervalOption]
	rawSmartScheduleRange := common.OptionMap[channelMonitorSmartScheduleRangeOption]
	rawSmartScheduleControlRevision := common.OptionMap[channelMonitorSmartScheduleControlRevisionOption]
	common.OptionMapRWMutex.RUnlock()

	interval, err := strconv.Atoi(rawInterval)
	if err != nil || interval < 0 || interval > maxChannelMonitorAutoUpdateIntervalMinutes {
		interval = 0
	}
	retryCount, err := strconv.Atoi(rawRetryCount)
	if err != nil || retryCount < 0 || retryCount > maxChannelMonitorAutoUpdateRetryCount {
		retryCount = defaultChannelMonitorAutoUpdateRetryCount
	}
	upstreamRequestTimeoutSeconds, err := strconv.Atoi(rawUpstreamRequestTimeout)
	if err != nil || upstreamRequestTimeoutSeconds < minChannelMonitorUpstreamRequestTimeoutSeconds ||
		upstreamRequestTimeoutSeconds > maxChannelMonitorUpstreamRequestTimeoutSeconds {
		upstreamRequestTimeoutSeconds = defaultChannelMonitorUpstreamRequestTimeoutSeconds
	}
	consecutiveFailureLimit, err := strconv.Atoi(rawConsecutiveFailureLimit)
	if err != nil || consecutiveFailureLimit < minChannelMonitorAutoUpdateConsecutiveFailureLimit ||
		consecutiveFailureLimit > maxChannelMonitorAutoUpdateConsecutiveFailureLimit {
		consecutiveFailureLimit = defaultChannelMonitorAutoUpdateConsecutiveFailureLimit
	}
	autoDisableOnUpdateFailure, err := strconv.ParseBool(rawAutoDisableOnUpdateFailure)
	if err != nil {
		autoDisableOnUpdateFailure = false
	}
	autoEnableOnCostRatioRecovery, err := strconv.ParseBool(rawAutoEnableOnCostRatioRecovery)
	if err != nil {
		autoEnableOnCostRatioRecovery = false
	}
	autoEnableOnBalanceRecovery, err := strconv.ParseBool(rawAutoEnableOnBalanceRecovery)
	if err != nil {
		autoEnableOnBalanceRecovery = false
	}
	costRetentionDays, err := strconv.Atoi(rawCostRetentionDays)
	if err != nil || costRetentionDays < minChannelMonitorCostRetentionDays || costRetentionDays > maxChannelMonitorCostRetentionDays {
		costRetentionDays = defaultChannelMonitorCostRetentionDays
	}
	notificationEmail, err := normalizeChannelMonitorNotificationEmail(rawNotificationEmail)
	if err != nil {
		notificationEmail = ""
	}
	emailNotificationEnabled, err := strconv.ParseBool(rawEmailNotificationEnabled)
	if err != nil {
		emailNotificationEnabled = false
	}
	emailNotificationTypes := parseChannelMonitorEmailNotificationTypes(rawEmailNotificationTypes)
	probeResponseEnabled, err := strconv.ParseBool(rawProbeResponseEnabled)
	if err != nil {
		probeResponseEnabled = false
	}
	relayResponseHeaderTimeoutSeconds, err := strconv.Atoi(rawRelayResponseHeaderTimeout)
	if err != nil || relayResponseHeaderTimeoutSeconds < 0 ||
		relayResponseHeaderTimeoutSeconds > common.MaxRelayResponseHeaderTimeoutSeconds {
		relayResponseHeaderTimeoutSeconds = common.DefaultRelayResponseHeaderTimeoutSeconds
	}
	smartScheduleEnabled, err := strconv.ParseBool(rawSmartScheduleEnabled)
	if err != nil {
		smartScheduleEnabled = false
	}
	smartScheduleInterval, err := strconv.Atoi(rawSmartScheduleInterval)
	if err != nil || smartScheduleInterval <= 0 || smartScheduleInterval > maxChannelMonitorAutoUpdateIntervalMinutes {
		smartScheduleInterval = defaultChannelMonitorSmartScheduleInterval
	}
	smartScheduleRange, err := strconv.Atoi(rawSmartScheduleRange)
	if err != nil || !isChannelMonitorPerformanceRangeSupported(smartScheduleRange) {
		smartScheduleRange = defaultChannelMonitorSmartScheduleRange
	}
	smartScheduleGroupPolicies := parseChannelSmartScheduleGroupPolicies(rawSmartScheduleGroupPolicies)
	if len(smartScheduleGroupPolicies) == 0 {
		smartScheduleEnabled = false
	}
	settings := channelMonitorSettings{
		AutoUpdateIntervalMinutes:         interval,
		AutoUpdateRetryCount:              retryCount,
		UpstreamRequestTimeoutSeconds:     upstreamRequestTimeoutSeconds,
		AutoUpdateConsecutiveFailureLimit: consecutiveFailureLimit,
		AutoDisableOnUpdateFailure:        autoDisableOnUpdateFailure,
		AutoEnableOnCostRatioRecovery:     autoEnableOnCostRatioRecovery,
		AutoEnableOnBalanceRecovery:       autoEnableOnBalanceRecovery,
		CostRetentionDays:                 costRetentionDays,
		EmailNotificationEnabled:          emailNotificationEnabled,
		NotificationEmail:                 notificationEmail,
		EmailNotificationTypes:            emailNotificationTypes,
		ProbeResponseEnabled:              probeResponseEnabled,
		RelayHeaderTimeoutSeconds:         relayResponseHeaderTimeoutSeconds,
		SmartScheduleEnabled:              smartScheduleEnabled,
		SmartScheduleGroupPolicies:        smartScheduleGroupPolicies,
		SmartScheduleIntervalMinutes:      smartScheduleInterval,
		SmartSchedulePerformanceMinutes:   smartScheduleRange,
		SmartScheduleControlRevision:      rawSmartScheduleControlRevision,
	}
	return settings
}

func (settings channelMonitorSettings) upstreamRequestTimeout() time.Duration {
	return time.Duration(settings.UpstreamRequestTimeoutSeconds) * time.Second
}

func defaultChannelMonitorEmailNotificationTypes() []string {
	return append([]string(nil), channelMonitorEmailNotificationTypes...)
}

func normalizeChannelMonitorEmailNotificationTypes(values []string) ([]string, error) {
	if len(values) > len(channelMonitorEmailNotificationTypes) {
		return nil, fmt.Errorf("邮件通知类型不能超过 %d 个", len(channelMonitorEmailNotificationTypes))
	}
	selected := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		valid := false
		for _, notificationType := range channelMonitorEmailNotificationTypes {
			if value == notificationType {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("不支持的邮件通知类型：%s", value)
		}
		selected[value] = struct{}{}
	}
	normalized := make([]string, 0, len(selected))
	for _, notificationType := range channelMonitorEmailNotificationTypes {
		if _, exists := selected[notificationType]; exists {
			normalized = append(normalized, notificationType)
		}
	}
	return normalized, nil
}

func parseChannelMonitorEmailNotificationTypes(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return defaultChannelMonitorEmailNotificationTypes()
	}
	var values []string
	if err := common.UnmarshalJsonStr(raw, &values); err != nil {
		return defaultChannelMonitorEmailNotificationTypes()
	}
	normalized, err := normalizeChannelMonitorEmailNotificationTypes(values)
	if err != nil {
		return defaultChannelMonitorEmailNotificationTypes()
	}
	return normalized
}

func channelMonitorEmailNotificationTypeEnabled(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalizeChannelMonitorSmartScheduleModels(models []string, fieldName string) ([]string, error) {
	if len(models) > maxChannelMonitorSmartScheduleModelCount {
		return nil, fmt.Errorf("分组调度%s不能超过 %d 个", fieldName, maxChannelMonitorSmartScheduleModelCount)
	}
	normalizedModels := make([]string, 0, len(models))
	seenModels := make(map[string]struct{}, len(models))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		if utf8.RuneCountInString(modelName) > maxChannelMonitorSmartScheduleModelLength {
			return nil, fmt.Errorf("分组调度%s中的模型名称不能超过 %d 个字符", fieldName, maxChannelMonitorSmartScheduleModelLength)
		}
		if _, exists := seenModels[modelName]; exists {
			continue
		}
		seenModels[modelName] = struct{}{}
		normalizedModels = append(normalizedModels, modelName)
	}
	return normalizedModels, nil
}

func isChannelMonitorSmartScheduleStrategySupported(strategy string) bool {
	switch strategy {
	case channelMonitorSmartScheduleStrategyRatio,
		channelMonitorSmartScheduleStrategyFirstToken,
		channelMonitorSmartScheduleStrategyTPS,
		channelMonitorSmartScheduleStrategySmart:
		return true
	default:
		return false
	}
}

func isChannelMonitorSmartScheduleApplyModeSupported(mode string) bool {
	switch mode {
	case channelMonitorSmartScheduleApplyWeight,
		channelMonitorSmartScheduleApplyPriorityWeight:
		return true
	default:
		return false
	}
}

func isChannelMonitorPerformanceRangeSupported(minutes int) bool {
	switch minutes {
	case 15, 60, 360, 1440:
		return true
	default:
		return false
	}
}

func normalizeChannelMonitorNotificationEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if utf8.RuneCountInString(value) > maxChannelMonitorNotificationEmailLength {
		return "", fmt.Errorf("通知邮箱不能超过 %d 个字符", maxChannelMonitorNotificationEmailLength)
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return "", errors.New("请输入有效的通知邮箱")
	}
	return address.Address, nil
}

func normalizeChannelMonitorPolicyAction(action string) string {
	switch action {
	case channelMonitorPolicyActionUpdateGroupRatio,
		channelMonitorPolicyActionDisableChannel,
		channelMonitorPolicyActionRemoveFromGroup:
		return action
	default:
		return channelMonitorPolicyActionNone
	}
}

func getChannelMonitorGroupCoefficients() map[string]float64 {
	common.OptionMapRWMutex.RLock()
	rawCoefficients := common.OptionMap[channelMonitorGroupCoefficientsOption]
	common.OptionMapRWMutex.RUnlock()

	coefficients := make(map[string]float64)
	if rawCoefficients == "" || common.UnmarshalJsonStr(rawCoefficients, &coefficients) != nil {
		return map[string]float64{}
	}
	if coefficients == nil {
		return map[string]float64{}
	}
	for group, coefficient := range coefficients {
		if group == "" || math.IsNaN(coefficient) || math.IsInf(coefficient, 0) || coefficient < 0 || coefficient > maxChannelMonitorRatio {
			delete(coefficients, group)
		}
	}
	return coefficients
}

func getChannelMonitorGroupCoefficient(coefficients map[string]float64, group string) float64 {
	coefficient, exists := coefficients[group]
	if !exists || !validateChannelMonitorRatio(&coefficient) {
		return defaultChannelMonitorGroupCoefficient
	}
	return coefficient
}

func normalizeChannelMonitorChannelOrder(channels []*model.Channel, channelIds []int) []int {
	availableChannelIds := make(map[int]struct{}, len(channels))
	for _, channel := range channels {
		availableChannelIds[channel.Id] = struct{}{}
	}

	orderedChannelIds := make([]int, 0, len(channels))
	seenChannelIds := make(map[int]struct{}, len(channels))
	for _, channelId := range channelIds {
		if _, exists := availableChannelIds[channelId]; !exists {
			continue
		}
		if _, exists := seenChannelIds[channelId]; exists {
			continue
		}
		orderedChannelIds = append(orderedChannelIds, channelId)
		seenChannelIds[channelId] = struct{}{}
	}
	for _, channel := range channels {
		if _, exists := seenChannelIds[channel.Id]; exists {
			continue
		}
		orderedChannelIds = append(orderedChannelIds, channel.Id)
	}
	return orderedChannelIds
}

func getChannelMonitorChannelOrder(channels []*model.Channel) []int {
	common.OptionMapRWMutex.RLock()
	rawChannelOrder := common.OptionMap[channelMonitorChannelOrderOption]
	common.OptionMapRWMutex.RUnlock()

	var channelIds []int
	if rawChannelOrder != "" && common.UnmarshalJsonStr(rawChannelOrder, &channelIds) != nil {
		channelIds = nil
	}
	return normalizeChannelMonitorChannelOrder(channels, channelIds)
}

func UpdateChannelMonitorChannelOrder(c *gin.Context) {
	var request channelMonitorOrderUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.ChannelIds == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	if len(*request.ChannelIds) > maxChannelMonitorChannelOrderCount {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道排序数量过多"})
		return
	}

	channels, err := model.GetAllChannelsForMonitor()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	availableChannelIds := make(map[int]struct{}, len(channels))
	for _, channel := range channels {
		availableChannelIds[channel.Id] = struct{}{}
	}
	seenChannelIds := make(map[int]struct{}, len(*request.ChannelIds))
	for _, channelId := range *request.ChannelIds {
		if _, exists := availableChannelIds[channelId]; !exists {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("渠道 %d 不存在，请刷新后重试", channelId),
			})
			return
		}
		if _, exists := seenChannelIds[channelId]; exists {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道排序中存在重复渠道"})
			return
		}
		seenChannelIds[channelId] = struct{}{}
	}

	channelOrder := normalizeChannelMonitorChannelOrder(channels, *request.ChannelIds)
	channelOrderBytes, err := common.Marshal(channelOrder)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOptionsBulk(map[string]string{
		channelMonitorChannelOrderOption: string(channelOrderBytes),
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.monitor_order_update", map[string]interface{}{
		"channel_count": len(channelOrder),
	})
	common.ApiSuccess(c, gin.H{"channel_order": channelOrder})
}

func UpdateChannelMonitorSettings(c *gin.Context) {
	var request channelMonitorSettingsUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	if request.AutoUpdateIntervalMinutes == nil &&
		request.AutoUpdateRetryCount == nil &&
		request.UpstreamRequestTimeoutSeconds == nil &&
		request.AutoUpdateConsecutiveFailureLimit == nil &&
		request.AutoDisableOnUpdateFailure == nil &&
		request.AutoEnableOnCostRatioRecovery == nil &&
		request.AutoEnableOnBalanceRecovery == nil &&
		request.CostRetentionDays == nil &&
		request.EmailNotificationEnabled == nil &&
		request.NotificationEmail == nil &&
		request.EmailNotificationTypes == nil &&
		request.ProbeResponseEnabled == nil &&
		request.RelayHeaderTimeoutSeconds == nil &&
		request.SmartScheduleEnabled == nil &&
		request.SmartScheduleGroupPolicies == nil &&
		request.SmartScheduleIntervalMinutes == nil &&
		request.SmartSchedulePerformanceMinutes == nil &&
		request.SmartScheduleForceReset == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供要更新的设置"})
		return
	}
	settings := getChannelMonitorSettings()
	values := make(map[string]string, 19)
	if request.AutoUpdateIntervalMinutes != nil && (*request.AutoUpdateIntervalMinutes < 0 ||
		*request.AutoUpdateIntervalMinutes > maxChannelMonitorAutoUpdateIntervalMinutes) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "自动更新间隔必须在 0 到 525600 分钟之间",
		})
		return
	}
	if request.AutoUpdateIntervalMinutes != nil {
		settings.AutoUpdateIntervalMinutes = *request.AutoUpdateIntervalMinutes
		values[channelMonitorAutoUpdateIntervalOption] = strconv.Itoa(settings.AutoUpdateIntervalMinutes)
	}
	if request.AutoUpdateRetryCount != nil && (*request.AutoUpdateRetryCount < 0 ||
		*request.AutoUpdateRetryCount > maxChannelMonitorAutoUpdateRetryCount) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "失败重试次数必须在 0 到 10 次之间",
		})
		return
	}
	if request.AutoUpdateRetryCount != nil {
		settings.AutoUpdateRetryCount = *request.AutoUpdateRetryCount
		values[channelMonitorAutoUpdateRetryCountOption] = strconv.Itoa(settings.AutoUpdateRetryCount)
	}
	if request.UpstreamRequestTimeoutSeconds != nil &&
		(*request.UpstreamRequestTimeoutSeconds < minChannelMonitorUpstreamRequestTimeoutSeconds ||
			*request.UpstreamRequestTimeoutSeconds > maxChannelMonitorUpstreamRequestTimeoutSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "上游请求超时时间必须在 1 到 600 秒之间",
		})
		return
	}
	if request.UpstreamRequestTimeoutSeconds != nil {
		settings.UpstreamRequestTimeoutSeconds = *request.UpstreamRequestTimeoutSeconds
		values[channelMonitorUpstreamRequestTimeoutOption] = strconv.Itoa(settings.UpstreamRequestTimeoutSeconds)
	}
	if request.AutoUpdateConsecutiveFailureLimit != nil &&
		(*request.AutoUpdateConsecutiveFailureLimit < minChannelMonitorAutoUpdateConsecutiveFailureLimit ||
			*request.AutoUpdateConsecutiveFailureLimit > maxChannelMonitorAutoUpdateConsecutiveFailureLimit) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "连续失败停止次数必须在 1 到 100 次之间",
		})
		return
	}
	if request.AutoUpdateConsecutiveFailureLimit != nil {
		settings.AutoUpdateConsecutiveFailureLimit = *request.AutoUpdateConsecutiveFailureLimit
		values[channelMonitorAutoUpdateConsecutiveFailureLimitOption] = strconv.Itoa(settings.AutoUpdateConsecutiveFailureLimit)
	}
	if request.AutoDisableOnUpdateFailure != nil {
		settings.AutoDisableOnUpdateFailure = *request.AutoDisableOnUpdateFailure
		values[channelMonitorAutoDisableOnUpdateFailureOption] = strconv.FormatBool(settings.AutoDisableOnUpdateFailure)
	}
	if request.AutoEnableOnCostRatioRecovery != nil {
		settings.AutoEnableOnCostRatioRecovery = *request.AutoEnableOnCostRatioRecovery
		values[channelMonitorAutoEnableOnCostRatioRecoveryOption] = strconv.FormatBool(settings.AutoEnableOnCostRatioRecovery)
	}
	if request.AutoEnableOnBalanceRecovery != nil {
		settings.AutoEnableOnBalanceRecovery = *request.AutoEnableOnBalanceRecovery
		values[channelMonitorAutoEnableOnBalanceRecoveryOption] = strconv.FormatBool(settings.AutoEnableOnBalanceRecovery)
	}
	if request.CostRetentionDays != nil && (*request.CostRetentionDays < minChannelMonitorCostRetentionDays ||
		*request.CostRetentionDays > maxChannelMonitorCostRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "成本数据保留天数必须在 1 到 3650 天之间",
		})
		return
	}
	if request.CostRetentionDays != nil {
		settings.CostRetentionDays = *request.CostRetentionDays
		values[channelMonitorCostRetentionDaysOption] = strconv.Itoa(settings.CostRetentionDays)
	}
	if request.EmailNotificationEnabled != nil {
		settings.EmailNotificationEnabled = *request.EmailNotificationEnabled
		values[channelMonitorEmailNotificationOption] = strconv.FormatBool(settings.EmailNotificationEnabled)
	}
	if request.NotificationEmail != nil {
		notificationEmail, err := normalizeChannelMonitorNotificationEmail(*request.NotificationEmail)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		settings.NotificationEmail = notificationEmail
		values[channelMonitorNotificationEmailOption] = notificationEmail
	}
	if request.EmailNotificationTypes != nil {
		notificationTypes, err := normalizeChannelMonitorEmailNotificationTypes(*request.EmailNotificationTypes)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		encoded, err := common.Marshal(notificationTypes)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		settings.EmailNotificationTypes = notificationTypes
		values[channelMonitorEmailNotificationTypesOption] = string(encoded)
	}
	if settings.EmailNotificationEnabled && settings.NotificationEmail == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "开启邮件通知时请填写通知邮箱"})
		return
	}
	if settings.EmailNotificationEnabled && len(settings.EmailNotificationTypes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "开启邮件通知时请至少选择一种通知类型"})
		return
	}
	if request.ProbeResponseEnabled != nil {
		settings.ProbeResponseEnabled = *request.ProbeResponseEnabled
		values[channelMonitorProbeResponseOption] = strconv.FormatBool(settings.ProbeResponseEnabled)
	}
	if request.RelayHeaderTimeoutSeconds != nil &&
		(*request.RelayHeaderTimeoutSeconds < 0 ||
			*request.RelayHeaderTimeoutSeconds > common.MaxRelayResponseHeaderTimeoutSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "上游响应等待时间必须在 0 到 600 秒之间",
		})
		return
	}
	if request.RelayHeaderTimeoutSeconds != nil {
		settings.RelayHeaderTimeoutSeconds = *request.RelayHeaderTimeoutSeconds
		values[common.RelayResponseHeaderTimeoutOptionKey] = strconv.Itoa(settings.RelayHeaderTimeoutSeconds)
	}
	if request.SmartScheduleEnabled != nil {
		settings.SmartScheduleEnabled = *request.SmartScheduleEnabled
		values[channelMonitorSmartScheduleEnabledOption] = strconv.FormatBool(settings.SmartScheduleEnabled)
	}
	if request.SmartScheduleIntervalMinutes != nil && (*request.SmartScheduleIntervalMinutes <= 0 ||
		*request.SmartScheduleIntervalMinutes > maxChannelMonitorAutoUpdateIntervalMinutes) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "智能调度间隔必须在 1 到 525600 分钟之间",
		})
		return
	}
	if request.SmartScheduleIntervalMinutes != nil {
		settings.SmartScheduleIntervalMinutes = *request.SmartScheduleIntervalMinutes
		values[channelMonitorSmartScheduleIntervalOption] = strconv.Itoa(settings.SmartScheduleIntervalMinutes)
	}
	if request.SmartSchedulePerformanceMinutes != nil &&
		!isChannelMonitorPerformanceRangeSupported(*request.SmartSchedulePerformanceMinutes) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "智能调度统计范围无效"})
		return
	}
	if request.SmartSchedulePerformanceMinutes != nil {
		settings.SmartSchedulePerformanceMinutes = *request.SmartSchedulePerformanceMinutes
		values[channelMonitorSmartScheduleRangeOption] = strconv.Itoa(settings.SmartSchedulePerformanceMinutes)
	}
	if request.SmartScheduleGroupPolicies != nil {
		groupPolicies, err := normalizeChannelSmartScheduleGroupPolicies(*request.SmartScheduleGroupPolicies)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		serializedGroupPolicies, err := common.Marshal(groupPolicies)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		settings.SmartScheduleGroupPolicies = groupPolicies
		values[channelMonitorSmartScheduleGroupPoliciesOption] = string(serializedGroupPolicies)
		values[channelMonitorSmartScheduleEnabledOption] = strconv.FormatBool(settings.SmartScheduleEnabled)
	}
	forceResetSmartSchedule := request.SmartScheduleForceReset != nil && *request.SmartScheduleForceReset
	if forceResetSmartSchedule && !settings.SmartScheduleEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "启用智能调度后才能强制重置"})
		return
	}
	if (request.SmartScheduleEnabled != nil || request.SmartScheduleGroupPolicies != nil || forceResetSmartSchedule) &&
		settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "启用智能调度前请至少配置一个完整的分组策略"})
		return
	}
	smartScheduleSettingsChanged := request.SmartScheduleEnabled != nil ||
		request.SmartScheduleGroupPolicies != nil ||
		request.SmartScheduleIntervalMinutes != nil ||
		request.SmartSchedulePerformanceMinutes != nil || forceResetSmartSchedule
	if smartScheduleSettingsChanged {
		values[channelMonitorSmartScheduleControlRevisionOption] = common.GetUUID()
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.SmartScheduleEnabled != nil || request.SmartScheduleGroupPolicies != nil {
		routingChanged, err := model.ClearChannelSmartScheduleExplorations()
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if routingChanged {
			model.InitChannelCache()
		}
	}
	forceResetTaskCreated := false
	forceResetTaskId := ""
	forceResetTaskError := ""
	if forceResetSmartSchedule {
		task, created, err := service.EnqueueSystemTask(
			channelMonitorSmartScheduleTaskType,
			channelSmartScheduleTaskPayload{ForceReset: true},
		)
		forceResetTaskCreated = created
		if err != nil {
			forceResetTaskError = err.Error()
		} else {
			forceResetTaskId = task.TaskID
		}
		settings.SmartScheduleForceResetTaskCreated = &forceResetTaskCreated
		settings.SmartScheduleForceResetTaskId = forceResetTaskId
		settings.SmartScheduleForceResetTaskError = forceResetTaskError
	}
	auditDetails := map[string]interface{}{
		"auto_update_interval_minutes":          settings.AutoUpdateIntervalMinutes,
		"auto_update_retry_count":               settings.AutoUpdateRetryCount,
		"upstream_request_timeout_seconds":      settings.UpstreamRequestTimeoutSeconds,
		"auto_update_consecutive_failure_limit": settings.AutoUpdateConsecutiveFailureLimit,
		"auto_disable_on_update_failure":        settings.AutoDisableOnUpdateFailure,
		"auto_enable_on_cost_ratio_recovery":    settings.AutoEnableOnCostRatioRecovery,
		"auto_enable_on_balance_recovery":       settings.AutoEnableOnBalanceRecovery,
		"cost_retention_days":                   settings.CostRetentionDays,
		"email_notification_enabled":            settings.EmailNotificationEnabled,
		"notification_email_configured":         settings.NotificationEmail != "",
		"email_notification_types":              settings.EmailNotificationTypes,
		"probe_response_enabled":                settings.ProbeResponseEnabled,
		"smart_schedule_enabled":                settings.SmartScheduleEnabled,
		"smart_schedule_group_policies":         settings.SmartScheduleGroupPolicies,
		"smart_schedule_interval_minutes":       settings.SmartScheduleIntervalMinutes,
		"smart_schedule_performance_minutes":    settings.SmartSchedulePerformanceMinutes,
		"smart_schedule_force_reset":            forceResetSmartSchedule,
		"smart_schedule_force_reset_created":    forceResetTaskCreated,
		"smart_schedule_force_reset_task_id":    forceResetTaskId,
		"smart_schedule_force_reset_error":      forceResetTaskError,
	}
	auditDetails["relay_response_header_timeout_seconds"] = settings.RelayHeaderTimeoutSeconds
	recordManageAudit(c, "channel.monitor_settings_update", auditDetails)
	common.ApiSuccess(c, settings)
}
