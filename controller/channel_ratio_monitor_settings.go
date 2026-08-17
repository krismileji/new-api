package controller

import (
	"context"
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
	channelMonitorAutoUpdateIntervalOption                     = "ChannelMonitorAutoUpdateIntervalMinutes"
	channelMonitorAutoUpdateRetryCountOption                   = "ChannelMonitorAutoUpdateRetryCount"
	channelMonitorUpstreamRequestTimeoutOption                 = "ChannelMonitorUpstreamRequestTimeoutSeconds"
	channelMonitorAutoUpdateConsecutiveFailureLimitOption      = "ChannelMonitorAutoUpdateConsecutiveFailureLimit"
	channelMonitorAutoDisableOnUpdateFailureOption             = "ChannelMonitorAutoDisableOnUpdateFailure"
	channelMonitorAutoEnableOnCostRatioRecoveryOption          = "ChannelMonitorAutoEnableOnCostRatioRecovery"
	channelMonitorAutoEnableOnBalanceRecoveryOption            = "ChannelMonitorAutoEnableOnBalanceRecovery"
	channelMonitorCostRetentionDaysOption                      = "ChannelMonitorCostRetentionDays"
	channelMonitorRouteMetricRetentionDaysOption               = "ChannelMonitorRouteMetricRetentionDays"
	channelMonitorAPIKeyMetricRetentionDaysOption              = "ChannelMonitorApiKeyMetricRetentionDays"
	channelMonitorExecutionDetailRetentionDaysOption           = "ChannelMonitorExecutionDetailRetentionDays"
	channelMonitorTaskRetentionDaysOption                      = "ChannelMonitorTaskRetentionDays"
	channelMonitorRatioHistoryRetentionDaysOption              = "ChannelMonitorRatioHistoryRetentionDays"
	channelMonitorStatusProbeHistoryRetentionDaysOption        = "ChannelMonitorStatusProbeHistoryRetentionDays"
	channelMonitorModelDetectionRetentionDaysOption            = "ChannelMonitorModelDetectionRetentionDays"
	channelMonitorCleanupEnabledOption                         = "ChannelMonitorCleanupEnabled"
	channelMonitorCleanupBatchSizeOption                       = "ChannelMonitorCleanupBatchSize"
	channelMonitorCleanupBudgetSecondsOption                   = "ChannelMonitorCleanupBudgetSeconds"
	channelMonitorCleanupContinuationSecondsOption             = "ChannelMonitorCleanupContinuationSeconds"
	channelMonitorCleanupIntervalMinutesOption                 = "ChannelMonitorCleanupIntervalMinutes"
	channelMonitorEmailNotificationOption                      = "ChannelMonitorEmailNotificationEnabled"
	channelMonitorNotificationEmailOption                      = "ChannelMonitorNotificationEmail"
	channelMonitorEmailNotificationTypesOption                 = "ChannelMonitorEmailNotificationTypes"
	channelMonitorErrorMessageMappingOption                    = service.ErrorMessageMappingOptionKey
	channelMonitorProbeResponseOption                          = channelprobe.OptionKey
	channelMonitorProbeResponseMatchInputOption                = channelprobe.MatchInputOptionKey
	channelMonitorProbeResponseTextOption                      = channelprobe.ResponseTextOptionKey
	channelMonitorProbeResponseMinDelayMsOption                = channelprobe.MinDelayMsOptionKey
	channelMonitorProbeResponseMaxDelayMsOption                = channelprobe.MaxDelayMsOptionKey
	channelMonitorProbeResponseInputTokensOption               = channelprobe.InputTokensOptionKey
	channelMonitorProbeResponseCacheWriteTokensOption          = channelprobe.CacheWriteTokensOptionKey
	channelMonitorProbeResponseCachedTokensOption              = channelprobe.CachedTokensOptionKey
	channelMonitorProbeResponseOutputTokensOption              = channelprobe.OutputTokensOptionKey
	channelMonitorGroupCoefficientsOption                      = model.ChannelMonitorGroupCoefficientsOption
	channelMonitorChannelOrderOption                           = "ChannelMonitorChannelOrder"
	channelMonitorSmartScheduleEnabledOption                   = "ChannelMonitorSmartScheduleEnabled"
	channelMonitorSmartSchedulePerformanceWindowOption         = model.ChannelMonitorSmartSchedulePerformanceWindowOption
	channelMonitorSmartScheduleStabilityWindowOption           = model.ChannelMonitorSmartScheduleStabilityWindowOption
	channelMonitorSmartScheduleRealtimeRetentionOption         = model.ChannelMonitorSmartScheduleRealtimeRetentionOption
	channelMonitorSmartScheduleRealtimeSampleLimitOption       = model.ChannelMonitorSmartScheduleRealtimeSampleLimitOption
	channelMonitorSmartScheduleRateLimitCooldownOption         = "ChannelMonitorSmartScheduleRateLimitCooldownSeconds"
	channelMonitorSmartScheduleControlRevisionOption           = model.ChannelSmartScheduleControlRevisionOption
	channelMonitorPolicyActionNone                             = "none"
	channelMonitorPolicyActionUpdateGroupRatio                 = "update_group_ratio"
	channelMonitorPolicyActionDisableChannel                   = "disable_channel"
	channelMonitorPolicyActionRemoveFromGroup                  = "remove_from_group"
	channelMonitorSmartScheduleStrategyRatio                   = "ratio"
	channelMonitorSmartScheduleStrategyFirstToken              = "first_token"
	channelMonitorSmartScheduleStrategyTPS                     = "tps"
	channelMonitorSmartScheduleStrategySmart                   = "smart"
	channelMonitorSmartScheduleApplyWeight                     = "weight"
	channelMonitorSmartScheduleApplyPriorityWeight             = "priority_weight"
	channelMonitorSmartScheduleSampleOff                       = "off"
	channelMonitorSmartScheduleSampleTraffic                   = "traffic"
	channelMonitorSmartScheduleSampleProbe                     = "probe"
	channelMonitorSmartScheduleSamplingOrderPriorityWeight     = "priority_weight"
	channelMonitorSmartScheduleSamplingOrderRatio              = "ratio"
	maxChannelMonitorAutoUpdateIntervalMinutes                 = 525600
	maxChannelMonitorAutoUpdateRetryCount                      = 10
	minChannelMonitorUpstreamRequestTimeoutSeconds             = 1
	maxChannelMonitorUpstreamRequestTimeoutSeconds             = 600
	minChannelMonitorAutoUpdateConsecutiveFailureLimit         = 1
	maxChannelMonitorAutoUpdateConsecutiveFailureLimit         = 100
	minChannelMonitorCostRetentionDays                         = 1
	maxChannelMonitorCostRetentionDays                         = 3650
	maxChannelMonitorStatusProbeHistoryRetentionDays           = 90
	minChannelMonitorCleanupBatchSize                          = 1
	maxChannelMonitorCleanupBatchSize                          = 10000
	minChannelMonitorCleanupBudgetSeconds                      = 1
	maxChannelMonitorCleanupBudgetSeconds                      = 300
	minChannelMonitorCleanupContinuationSeconds                = 15
	maxChannelMonitorCleanupContinuationSeconds                = 3600
	minChannelMonitorCleanupIntervalMinutes                    = 60
	maxChannelMonitorCleanupIntervalMinutes                    = 10080
	maxChannelMonitorNotificationEmailLength                   = 254
	maxChannelMonitorChannelOrderCount                         = 100000
	maxChannelMonitorSmartScheduleModelLength                  = 255
	maxChannelMonitorSmartScheduleModelCount                   = 100
	maxChannelMonitorSmartScheduleGroupCount                   = 100
	maxChannelMonitorSmartScheduleGroupLength                  = 64
	maxChannelMonitorSmartScheduleMinSamples                   = 100000
	maxChannelMonitorSmartScheduleSuccessRate                  = 100
	maxChannelMonitorSmartScheduleExplorationPercent           = 20
	maxChannelMonitorSmartScheduleWindowMinutes                = model.ChannelMonitorSmartScheduleMaxWindowMinutes
	minChannelMonitorSmartScheduleRealtimeRetentionMinutes     = model.ChannelMonitorSmartScheduleMinRealtimeRetentionMinutes
	maxChannelMonitorSmartScheduleRealtimeRetentionMinutes     = model.ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes
	minChannelMonitorSmartScheduleRealtimeSampleLimit          = model.ChannelMonitorSmartScheduleMinRealtimeSampleLimit
	maxChannelMonitorSmartScheduleRealtimeSampleLimit          = model.ChannelMonitorSmartScheduleMaxRealtimeSampleLimit
	maxChannelMonitorSmartScheduleRateLimitCooldownSeconds     = 300
	defaultChannelMonitorAutoUpdateRetryCount                  = 2
	defaultChannelMonitorUpstreamRequestTimeoutSeconds         = 30
	defaultChannelMonitorAutoUpdateConsecutiveFailureLimit     = 2
	defaultChannelMonitorCostRetentionDays                     = 30
	defaultChannelMonitorRouteMetricRetentionDays              = 30
	defaultChannelMonitorAPIKeyMetricRetentionDays             = 7
	defaultChannelMonitorExecutionDetailRetentionDays          = 3
	defaultChannelMonitorTaskRetentionDays                     = 90
	defaultChannelMonitorRatioHistoryRetentionDays             = 365
	defaultChannelMonitorStatusProbeHistoryRetentionDays       = 7
	defaultChannelMonitorCleanupEnabled                        = true
	defaultChannelMonitorCleanupBatchSize                      = 1000
	defaultChannelMonitorCleanupBudgetSeconds                  = 10
	defaultChannelMonitorCleanupContinuationSeconds            = 60
	defaultChannelMonitorCleanupIntervalMinutes                = 24 * 60
	defaultChannelMonitorGroupCoefficient                      = 1
	defaultChannelMonitorSmartScheduleProbeInterval            = 10
	defaultChannelMonitorSmartSchedulePerformanceWindowMinutes = model.ChannelMonitorSmartScheduleDefaultPerformanceWindowMinutes
	defaultChannelMonitorSmartScheduleStabilityWindowMinutes   = model.ChannelMonitorSmartScheduleDefaultStabilityWindowMinutes
	defaultChannelMonitorSmartScheduleRealtimeRetentionMinutes = model.ChannelMonitorSmartScheduleDefaultRealtimeRetentionMinutes
	defaultChannelMonitorSmartScheduleRealtimeSampleLimit      = model.ChannelMonitorSmartScheduleDefaultRealtimeSampleLimit
	defaultChannelMonitorSmartScheduleRateLimitCooldownSeconds = 30
	channelMonitorEmailTypeRatioChange                         = "ratio_change"
	channelMonitorEmailTypeBalanceWarning                      = "balance_warning"
	channelMonitorEmailTypeChannelDisabled                     = "channel_disabled"
	channelMonitorEmailTypeGroupMembershipRemoved              = "group_membership_removed"
	channelMonitorEmailTypeUpstreamSyncFailed                  = "upstream_sync_failed"
	channelMonitorEmailTypeTaskFailed                          = "task_failed"
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
	AutoUpdateIntervalMinutes             int                        `json:"auto_update_interval_minutes"`
	AutoUpdateRetryCount                  int                        `json:"auto_update_retry_count"`
	UpstreamRequestTimeoutSeconds         int                        `json:"upstream_request_timeout_seconds"`
	AutoUpdateConsecutiveFailureLimit     int                        `json:"auto_update_consecutive_failure_limit"`
	AutoDisableOnUpdateFailure            bool                       `json:"auto_disable_on_update_failure"`
	AutoEnableOnCostRatioRecovery         bool                       `json:"auto_enable_on_cost_ratio_recovery"`
	AutoEnableOnBalanceRecovery           bool                       `json:"auto_enable_on_balance_recovery"`
	CostRetentionDays                     int                        `json:"cost_retention_days"`
	RouteMetricRetentionDays              int                        `json:"route_metric_retention_days"`
	APIKeyMetricRetentionDays             int                        `json:"api_key_metric_retention_days"`
	ExecutionDetailRetentionDays          int                        `json:"execution_detail_retention_days"`
	TaskRetentionDays                     int                        `json:"task_retention_days"`
	RatioHistoryRetentionDays             int                        `json:"ratio_history_retention_days"`
	StatusProbeHistoryRetentionDays       int                        `json:"status_probe_history_retention_days"`
	ModelDetectionRetentionDays           int                        `json:"model_detection_retention_days"`
	CleanupEnabled                        bool                       `json:"cleanup_enabled"`
	CleanupBatchSize                      int                        `json:"cleanup_batch_size"`
	CleanupBudgetSeconds                  int                        `json:"cleanup_budget_seconds"`
	CleanupContinuationSeconds            int                        `json:"cleanup_continuation_seconds"`
	CleanupIntervalMinutes                int                        `json:"cleanup_interval_minutes"`
	EmailNotificationEnabled              bool                       `json:"email_notification_enabled"`
	NotificationEmail                     string                     `json:"notification_email"`
	EmailNotificationTypes                []string                   `json:"email_notification_types"`
	ErrorMessageMapping                   string                     `json:"error_message_mapping"`
	ProbeResponseEnabled                  bool                       `json:"probe_response_enabled"`
	ProbeResponseMatchInput               string                     `json:"probe_response_match_input"`
	ProbeResponseText                     string                     `json:"probe_response_text"`
	ProbeResponseMinDelayMs               int                        `json:"probe_response_min_delay_ms"`
	ProbeResponseMaxDelayMs               int                        `json:"probe_response_max_delay_ms"`
	ProbeResponseInputTokens              int                        `json:"probe_response_input_tokens"`
	ProbeResponseCacheWriteTokens         int                        `json:"probe_response_cache_write_tokens"`
	ProbeResponseCachedTokens             int                        `json:"probe_response_cached_tokens"`
	ProbeResponseOutputTokens             int                        `json:"probe_response_output_tokens"`
	RelayHeaderTimeoutSeconds             int                        `json:"relay_response_header_timeout_seconds"`
	SmartScheduleEnabled                  bool                       `json:"smart_schedule_enabled"`
	SmartScheduleConfigError              string                     `json:"smart_schedule_config_error"`
	SmartScheduleGroupPolicies            smartScheduleGroupPolicies `json:"smart_schedule_group_policies"`
	SmartSchedulePerformanceWindowMinutes int                        `json:"smart_schedule_performance_window_minutes"`
	SmartScheduleStabilityWindowMinutes   int                        `json:"smart_schedule_stability_window_minutes"`
	SmartScheduleRealtimeRetentionMinutes int                        `json:"smart_schedule_realtime_retention_minutes"`
	SmartScheduleRealtimeSampleLimit      int                        `json:"smart_schedule_realtime_sample_limit"`
	SmartScheduleRateLimitCooldownSeconds int                        `json:"smart_schedule_rate_limit_cooldown_seconds"`
	SmartScheduleControlRevision          string                     `json:"smart_schedule_control_revision"`
	SmartScheduleForceResetTaskCreated    *bool                      `json:"smart_schedule_force_reset_task_created,omitempty"`
	SmartScheduleForceResetTaskId         string                     `json:"smart_schedule_force_reset_task_id,omitempty"`
	SmartScheduleForceResetTaskError      string                     `json:"smart_schedule_force_reset_task_error,omitempty"`
}

type channelMonitorSettingsUpdateRequest struct {
	AutoUpdateIntervalMinutes             *int                        `json:"auto_update_interval_minutes"`
	AutoUpdateRetryCount                  *int                        `json:"auto_update_retry_count"`
	UpstreamRequestTimeoutSeconds         *int                        `json:"upstream_request_timeout_seconds"`
	AutoUpdateConsecutiveFailureLimit     *int                        `json:"auto_update_consecutive_failure_limit"`
	AutoDisableOnUpdateFailure            *bool                       `json:"auto_disable_on_update_failure"`
	AutoEnableOnCostRatioRecovery         *bool                       `json:"auto_enable_on_cost_ratio_recovery"`
	AutoEnableOnBalanceRecovery           *bool                       `json:"auto_enable_on_balance_recovery"`
	CostRetentionDays                     *int                        `json:"cost_retention_days"`
	RouteMetricRetentionDays              *int                        `json:"route_metric_retention_days"`
	APIKeyMetricRetentionDays             *int                        `json:"api_key_metric_retention_days"`
	ExecutionDetailRetentionDays          *int                        `json:"execution_detail_retention_days"`
	TaskRetentionDays                     *int                        `json:"task_retention_days"`
	RatioHistoryRetentionDays             *int                        `json:"ratio_history_retention_days"`
	StatusProbeHistoryRetentionDays       *int                        `json:"status_probe_history_retention_days"`
	ModelDetectionRetentionDays           *int                        `json:"model_detection_retention_days"`
	CleanupEnabled                        *bool                       `json:"cleanup_enabled"`
	CleanupBatchSize                      *int                        `json:"cleanup_batch_size"`
	CleanupBudgetSeconds                  *int                        `json:"cleanup_budget_seconds"`
	CleanupContinuationSeconds            *int                        `json:"cleanup_continuation_seconds"`
	CleanupIntervalMinutes                *int                        `json:"cleanup_interval_minutes"`
	EmailNotificationEnabled              *bool                       `json:"email_notification_enabled"`
	NotificationEmail                     *string                     `json:"notification_email"`
	EmailNotificationTypes                *[]string                   `json:"email_notification_types"`
	ErrorMessageMapping                   *string                     `json:"error_message_mapping"`
	ProbeResponseEnabled                  *bool                       `json:"probe_response_enabled"`
	ProbeResponseMatchInput               *string                     `json:"probe_response_match_input"`
	ProbeResponseText                     *string                     `json:"probe_response_text"`
	ProbeResponseMinDelayMs               *int                        `json:"probe_response_min_delay_ms"`
	ProbeResponseMaxDelayMs               *int                        `json:"probe_response_max_delay_ms"`
	ProbeResponseInputTokens              *int                        `json:"probe_response_input_tokens"`
	ProbeResponseCacheWriteTokens         *int                        `json:"probe_response_cache_write_tokens"`
	ProbeResponseCachedTokens             *int                        `json:"probe_response_cached_tokens"`
	ProbeResponseOutputTokens             *int                        `json:"probe_response_output_tokens"`
	RelayHeaderTimeoutSeconds             *int                        `json:"relay_response_header_timeout_seconds"`
	SmartScheduleEnabled                  *bool                       `json:"smart_schedule_enabled"`
	SmartScheduleGroupPolicies            *smartScheduleGroupPolicies `json:"smart_schedule_group_policies"`
	SmartSchedulePerformanceWindowMinutes *int                        `json:"smart_schedule_performance_window_minutes"`
	SmartScheduleStabilityWindowMinutes   *int                        `json:"smart_schedule_stability_window_minutes"`
	SmartScheduleRealtimeRetentionMinutes *int                        `json:"smart_schedule_realtime_retention_minutes"`
	SmartScheduleRealtimeSampleLimit      *int                        `json:"smart_schedule_realtime_sample_limit"`
	SmartScheduleRateLimitCooldownSeconds *int                        `json:"smart_schedule_rate_limit_cooldown_seconds"`
	SmartScheduleControlRevision          *string                     `json:"smart_schedule_control_revision"`
	SmartScheduleForceReset               *bool                       `json:"smart_schedule_force_reset"`
}

type channelMonitorOrderUpdateRequest struct {
	ChannelIds *[]int `json:"channel_ids"`
}

func getChannelMonitorSettings() channelMonitorSettings {
	common.OptionMapRWMutex.RLock()
	options := make(map[string]string, len(common.OptionMap))
	for key, value := range common.OptionMap {
		options[key] = value
	}
	common.OptionMapRWMutex.RUnlock()
	return channelMonitorSettingsFromOptions(options)
}

func loadChannelMonitorSettingsSnapshot(ctx context.Context) (channelMonitorSettings, error) {
	if model.DB == nil {
		return channelMonitorSettings{}, errors.New("渠道监控设置数据库未初始化")
	}
	optionKeys := []string{
		channelMonitorAutoUpdateIntervalOption,
		channelMonitorAutoUpdateRetryCountOption,
		channelMonitorUpstreamRequestTimeoutOption,
		channelMonitorAutoUpdateConsecutiveFailureLimitOption,
		channelMonitorAutoDisableOnUpdateFailureOption,
		channelMonitorAutoEnableOnCostRatioRecoveryOption,
		channelMonitorAutoEnableOnBalanceRecoveryOption,
		channelMonitorCostRetentionDaysOption,
		channelMonitorRouteMetricRetentionDaysOption,
		channelMonitorAPIKeyMetricRetentionDaysOption,
		channelMonitorExecutionDetailRetentionDaysOption,
		channelMonitorTaskRetentionDaysOption,
		channelMonitorRatioHistoryRetentionDaysOption,
		channelMonitorStatusProbeHistoryRetentionDaysOption,
		channelMonitorModelDetectionRetentionDaysOption,
		channelMonitorCleanupEnabledOption,
		channelMonitorCleanupBatchSizeOption,
		channelMonitorCleanupBudgetSecondsOption,
		channelMonitorCleanupContinuationSecondsOption,
		channelMonitorCleanupIntervalMinutesOption,
		channelMonitorEmailNotificationOption,
		channelMonitorNotificationEmailOption,
		channelMonitorEmailNotificationTypesOption,
		channelMonitorErrorMessageMappingOption,
		channelMonitorProbeResponseOption,
		channelMonitorProbeResponseMatchInputOption,
		channelMonitorProbeResponseTextOption,
		channelMonitorProbeResponseMinDelayMsOption,
		channelMonitorProbeResponseMaxDelayMsOption,
		channelMonitorProbeResponseInputTokensOption,
		channelMonitorProbeResponseCacheWriteTokensOption,
		channelMonitorProbeResponseCachedTokensOption,
		channelMonitorProbeResponseOutputTokensOption,
		common.RelayResponseHeaderTimeoutOptionKey,
		channelMonitorSmartScheduleEnabledOption,
		channelMonitorSmartScheduleGroupPoliciesOption,
		channelMonitorSmartSchedulePerformanceWindowOption,
		channelMonitorSmartScheduleStabilityWindowOption,
		channelMonitorSmartScheduleRealtimeRetentionOption,
		channelMonitorSmartScheduleRealtimeSampleLimitOption,
		channelMonitorSmartScheduleRateLimitCooldownOption,
		channelMonitorSmartScheduleControlRevisionOption,
	}
	var storedOptions []model.Option
	if err := model.DB.WithContext(ctx).
		Select("key", "value").
		Where(map[string]any{"key": optionKeys}).
		Find(&storedOptions).Error; err != nil {
		return channelMonitorSettings{}, fmt.Errorf("读取渠道监控设置失败: %w", err)
	}
	options := make(map[string]string, len(storedOptions))
	for _, option := range storedOptions {
		options[option.Key] = option.Value
	}
	return channelMonitorSettingsFromOptions(options), nil
}

func channelMonitorSettingsFromOptions(options map[string]string) channelMonitorSettings {
	rawInterval := options[channelMonitorAutoUpdateIntervalOption]
	rawRetryCount := options[channelMonitorAutoUpdateRetryCountOption]
	rawUpstreamRequestTimeout := options[channelMonitorUpstreamRequestTimeoutOption]
	rawConsecutiveFailureLimit := options[channelMonitorAutoUpdateConsecutiveFailureLimitOption]
	rawAutoDisableOnUpdateFailure := options[channelMonitorAutoDisableOnUpdateFailureOption]
	rawAutoEnableOnCostRatioRecovery := options[channelMonitorAutoEnableOnCostRatioRecoveryOption]
	rawAutoEnableOnBalanceRecovery := options[channelMonitorAutoEnableOnBalanceRecoveryOption]
	rawCostRetentionDays := options[channelMonitorCostRetentionDaysOption]
	rawRouteMetricRetentionDays := options[channelMonitorRouteMetricRetentionDaysOption]
	rawAPIKeyMetricRetentionDays := options[channelMonitorAPIKeyMetricRetentionDaysOption]
	rawExecutionDetailRetentionDays := options[channelMonitorExecutionDetailRetentionDaysOption]
	rawTaskRetentionDays := options[channelMonitorTaskRetentionDaysOption]
	rawRatioHistoryRetentionDays := options[channelMonitorRatioHistoryRetentionDaysOption]
	rawStatusProbeHistoryRetentionDays := options[channelMonitorStatusProbeHistoryRetentionDaysOption]
	rawModelDetectionRetentionDays := options[channelMonitorModelDetectionRetentionDaysOption]
	rawCleanupEnabled := options[channelMonitorCleanupEnabledOption]
	rawCleanupBatchSize := options[channelMonitorCleanupBatchSizeOption]
	rawCleanupBudgetSeconds := options[channelMonitorCleanupBudgetSecondsOption]
	rawCleanupContinuationSeconds := options[channelMonitorCleanupContinuationSecondsOption]
	rawCleanupIntervalMinutes := options[channelMonitorCleanupIntervalMinutesOption]
	rawEmailNotificationEnabled := options[channelMonitorEmailNotificationOption]
	rawNotificationEmail := options[channelMonitorNotificationEmailOption]
	rawEmailNotificationTypes := options[channelMonitorEmailNotificationTypesOption]
	rawErrorMessageMapping := options[channelMonitorErrorMessageMappingOption]
	rawRelayResponseHeaderTimeout := options[common.RelayResponseHeaderTimeoutOptionKey]
	rawSmartScheduleEnabled := options[channelMonitorSmartScheduleEnabledOption]
	rawSmartScheduleGroupPolicies := options[channelMonitorSmartScheduleGroupPoliciesOption]
	rawSmartSchedulePerformanceWindow := options[channelMonitorSmartSchedulePerformanceWindowOption]
	rawSmartScheduleStabilityWindow := options[channelMonitorSmartScheduleStabilityWindowOption]
	rawSmartScheduleRealtimeRetention := options[channelMonitorSmartScheduleRealtimeRetentionOption]
	rawSmartScheduleRealtimeSampleLimit := options[channelMonitorSmartScheduleRealtimeSampleLimitOption]
	rawSmartScheduleRateLimitCooldown := options[channelMonitorSmartScheduleRateLimitCooldownOption]
	rawSmartScheduleControlRevision := options[channelMonitorSmartScheduleControlRevisionOption]

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
	routeMetricRetentionDays, err := strconv.Atoi(rawRouteMetricRetentionDays)
	if err != nil || routeMetricRetentionDays < minChannelMonitorCostRetentionDays || routeMetricRetentionDays > maxChannelMonitorCostRetentionDays {
		routeMetricRetentionDays = defaultChannelMonitorRouteMetricRetentionDays
	}
	apiKeyMetricRetentionDays, err := strconv.Atoi(rawAPIKeyMetricRetentionDays)
	if err != nil || apiKeyMetricRetentionDays < minChannelMonitorCostRetentionDays || apiKeyMetricRetentionDays > maxChannelMonitorCostRetentionDays {
		apiKeyMetricRetentionDays = defaultChannelMonitorAPIKeyMetricRetentionDays
	}
	executionDetailRetentionDays, err := strconv.Atoi(rawExecutionDetailRetentionDays)
	if err != nil || executionDetailRetentionDays < minChannelMonitorCostRetentionDays || executionDetailRetentionDays > maxChannelMonitorCostRetentionDays {
		executionDetailRetentionDays = defaultChannelMonitorExecutionDetailRetentionDays
	}
	taskRetentionDays, err := strconv.Atoi(rawTaskRetentionDays)
	if err != nil || taskRetentionDays < minChannelMonitorCostRetentionDays || taskRetentionDays > maxChannelMonitorCostRetentionDays {
		taskRetentionDays = defaultChannelMonitorTaskRetentionDays
	}
	if taskRetentionDays < executionDetailRetentionDays {
		taskRetentionDays = executionDetailRetentionDays
	}
	ratioHistoryRetentionDays, err := strconv.Atoi(rawRatioHistoryRetentionDays)
	if err != nil || ratioHistoryRetentionDays < minChannelMonitorCostRetentionDays || ratioHistoryRetentionDays > maxChannelMonitorCostRetentionDays {
		ratioHistoryRetentionDays = defaultChannelMonitorRatioHistoryRetentionDays
	}
	statusProbeHistoryRetentionDays, err := strconv.Atoi(rawStatusProbeHistoryRetentionDays)
	if err != nil || statusProbeHistoryRetentionDays < minChannelMonitorCostRetentionDays ||
		statusProbeHistoryRetentionDays > maxChannelMonitorStatusProbeHistoryRetentionDays {
		statusProbeHistoryRetentionDays = defaultChannelMonitorStatusProbeHistoryRetentionDays
	}
	modelDetectionRetentionDays, err := strconv.Atoi(rawModelDetectionRetentionDays)
	if err != nil || modelDetectionRetentionDays < model.ChannelModelDetectionMinRetentionDays ||
		modelDetectionRetentionDays > model.ChannelModelDetectionMaxRetentionDays {
		modelDetectionRetentionDays = model.ChannelModelDetectionDefaultRetentionDays
	}
	cleanupEnabled, err := strconv.ParseBool(rawCleanupEnabled)
	if err != nil {
		cleanupEnabled = defaultChannelMonitorCleanupEnabled
	}
	cleanupBatchSize, err := strconv.Atoi(rawCleanupBatchSize)
	if err != nil || cleanupBatchSize < minChannelMonitorCleanupBatchSize || cleanupBatchSize > maxChannelMonitorCleanupBatchSize {
		cleanupBatchSize = defaultChannelMonitorCleanupBatchSize
	}
	cleanupBudgetSeconds, err := strconv.Atoi(rawCleanupBudgetSeconds)
	if err != nil || cleanupBudgetSeconds < minChannelMonitorCleanupBudgetSeconds || cleanupBudgetSeconds > maxChannelMonitorCleanupBudgetSeconds {
		cleanupBudgetSeconds = defaultChannelMonitorCleanupBudgetSeconds
	}
	cleanupContinuationSeconds, err := strconv.Atoi(rawCleanupContinuationSeconds)
	if err != nil || cleanupContinuationSeconds < minChannelMonitorCleanupContinuationSeconds ||
		cleanupContinuationSeconds > maxChannelMonitorCleanupContinuationSeconds {
		cleanupContinuationSeconds = defaultChannelMonitorCleanupContinuationSeconds
	}
	cleanupIntervalMinutes, err := strconv.Atoi(rawCleanupIntervalMinutes)
	if err != nil || cleanupIntervalMinutes < minChannelMonitorCleanupIntervalMinutes ||
		cleanupIntervalMinutes > maxChannelMonitorCleanupIntervalMinutes {
		cleanupIntervalMinutes = defaultChannelMonitorCleanupIntervalMinutes
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
	probeResponseConfig := channelprobe.ResponseConfigFromOptions(options)
	relayResponseHeaderTimeoutSeconds, err := strconv.Atoi(rawRelayResponseHeaderTimeout)
	if err != nil || relayResponseHeaderTimeoutSeconds < 0 ||
		relayResponseHeaderTimeoutSeconds > common.MaxRelayResponseHeaderTimeoutSeconds {
		relayResponseHeaderTimeoutSeconds = common.DefaultRelayResponseHeaderTimeoutSeconds
	}
	smartScheduleEnabled, err := strconv.ParseBool(rawSmartScheduleEnabled)
	if err != nil {
		smartScheduleEnabled = false
	}
	smartSchedulePerformanceWindow, err := strconv.Atoi(rawSmartSchedulePerformanceWindow)
	if err != nil || !isChannelMonitorSmartScheduleWindowSupported(smartSchedulePerformanceWindow) {
		smartSchedulePerformanceWindow = defaultChannelMonitorSmartSchedulePerformanceWindowMinutes
	}
	smartScheduleStabilityWindow, err := strconv.Atoi(rawSmartScheduleStabilityWindow)
	if err != nil || !isChannelMonitorSmartScheduleWindowSupported(smartScheduleStabilityWindow) {
		smartScheduleStabilityWindow = defaultChannelMonitorSmartScheduleStabilityWindowMinutes
	}
	smartScheduleRateLimitCooldown, err := strconv.Atoi(rawSmartScheduleRateLimitCooldown)
	if err != nil || smartScheduleRateLimitCooldown < 0 || smartScheduleRateLimitCooldown > maxChannelMonitorSmartScheduleRateLimitCooldownSeconds {
		smartScheduleRateLimitCooldown = defaultChannelMonitorSmartScheduleRateLimitCooldownSeconds
	}
	smartSchedulePolicies, smartScheduleConfigErr := parseChannelSmartScheduleGroupPoliciesWithErrorAndLegacyStabilityWindow(
		rawSmartScheduleGroupPolicies, smartScheduleStabilityWindow,
	)
	if smartScheduleConfigErr == nil {
		smartScheduleStabilityWindow = smartScheduleGroupPolicies(smartSchedulePolicies).maxStabilityWindowMinutes(
			smartScheduleStabilityWindow,
		)
	}
	realtimeSettings := model.ChannelMonitorSmartScheduleRealtimeSettingsFromOptions(map[string]string{
		channelMonitorSmartSchedulePerformanceWindowOption:   strconv.Itoa(smartSchedulePerformanceWindow),
		channelMonitorSmartScheduleStabilityWindowOption:     strconv.Itoa(smartScheduleStabilityWindow),
		channelMonitorSmartScheduleRealtimeRetentionOption:   rawSmartScheduleRealtimeRetention,
		channelMonitorSmartScheduleRealtimeSampleLimitOption: rawSmartScheduleRealtimeSampleLimit,
	})
	if smartScheduleConfigErr == nil && smartScheduleEnabled && len(smartSchedulePolicies) == 0 {
		smartScheduleConfigErr = errors.New("智能调度已启用，但分组调度策略为空")
	}
	smartScheduleConfigError := ""
	if smartScheduleConfigErr != nil {
		smartScheduleEnabled = false
		smartScheduleConfigError = smartScheduleConfigErr.Error()
	}
	settings := channelMonitorSettings{
		AutoUpdateIntervalMinutes:             interval,
		AutoUpdateRetryCount:                  retryCount,
		UpstreamRequestTimeoutSeconds:         upstreamRequestTimeoutSeconds,
		AutoUpdateConsecutiveFailureLimit:     consecutiveFailureLimit,
		AutoDisableOnUpdateFailure:            autoDisableOnUpdateFailure,
		AutoEnableOnCostRatioRecovery:         autoEnableOnCostRatioRecovery,
		AutoEnableOnBalanceRecovery:           autoEnableOnBalanceRecovery,
		CostRetentionDays:                     costRetentionDays,
		RouteMetricRetentionDays:              routeMetricRetentionDays,
		APIKeyMetricRetentionDays:             apiKeyMetricRetentionDays,
		ExecutionDetailRetentionDays:          executionDetailRetentionDays,
		TaskRetentionDays:                     taskRetentionDays,
		RatioHistoryRetentionDays:             ratioHistoryRetentionDays,
		StatusProbeHistoryRetentionDays:       statusProbeHistoryRetentionDays,
		ModelDetectionRetentionDays:           modelDetectionRetentionDays,
		CleanupEnabled:                        cleanupEnabled,
		CleanupBatchSize:                      cleanupBatchSize,
		CleanupBudgetSeconds:                  cleanupBudgetSeconds,
		CleanupContinuationSeconds:            cleanupContinuationSeconds,
		CleanupIntervalMinutes:                cleanupIntervalMinutes,
		EmailNotificationEnabled:              emailNotificationEnabled,
		NotificationEmail:                     notificationEmail,
		EmailNotificationTypes:                emailNotificationTypes,
		ErrorMessageMapping:                   rawErrorMessageMapping,
		ProbeResponseEnabled:                  probeResponseConfig.Enabled,
		ProbeResponseMatchInput:               probeResponseConfig.MatchInput,
		ProbeResponseText:                     probeResponseConfig.ResponseText,
		ProbeResponseMinDelayMs:               probeResponseConfig.MinDelayMs,
		ProbeResponseMaxDelayMs:               probeResponseConfig.MaxDelayMs,
		ProbeResponseInputTokens:              probeResponseConfig.InputTokens,
		ProbeResponseCacheWriteTokens:         probeResponseConfig.CacheWriteTokens,
		ProbeResponseCachedTokens:             probeResponseConfig.CachedTokens,
		ProbeResponseOutputTokens:             probeResponseConfig.OutputTokens,
		RelayHeaderTimeoutSeconds:             relayResponseHeaderTimeoutSeconds,
		SmartScheduleEnabled:                  smartScheduleEnabled,
		SmartScheduleConfigError:              smartScheduleConfigError,
		SmartScheduleGroupPolicies:            smartSchedulePolicies,
		SmartSchedulePerformanceWindowMinutes: smartSchedulePerformanceWindow,
		SmartScheduleStabilityWindowMinutes:   smartScheduleStabilityWindow,
		SmartScheduleRealtimeRetentionMinutes: realtimeSettings.RetentionMinutes,
		SmartScheduleRealtimeSampleLimit:      realtimeSettings.SampleLimit,
		SmartScheduleRateLimitCooldownSeconds: smartScheduleRateLimitCooldown,
		SmartScheduleControlRevision:          rawSmartScheduleControlRevision,
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

func isChannelMonitorSmartScheduleWindowSupported(minutes int) bool {
	return minutes > 0 && minutes <= maxChannelMonitorSmartScheduleWindowMinutes
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
		request.RouteMetricRetentionDays == nil &&
		request.APIKeyMetricRetentionDays == nil &&
		request.ExecutionDetailRetentionDays == nil &&
		request.TaskRetentionDays == nil &&
		request.RatioHistoryRetentionDays == nil &&
		request.StatusProbeHistoryRetentionDays == nil &&
		request.ModelDetectionRetentionDays == nil &&
		request.CleanupEnabled == nil &&
		request.CleanupBatchSize == nil &&
		request.CleanupBudgetSeconds == nil &&
		request.CleanupContinuationSeconds == nil &&
		request.CleanupIntervalMinutes == nil &&
		request.EmailNotificationEnabled == nil &&
		request.NotificationEmail == nil &&
		request.EmailNotificationTypes == nil &&
		request.ErrorMessageMapping == nil &&
		request.ProbeResponseEnabled == nil &&
		request.ProbeResponseMatchInput == nil &&
		request.ProbeResponseText == nil &&
		request.ProbeResponseMinDelayMs == nil &&
		request.ProbeResponseMaxDelayMs == nil &&
		request.ProbeResponseInputTokens == nil &&
		request.ProbeResponseCacheWriteTokens == nil &&
		request.ProbeResponseCachedTokens == nil &&
		request.ProbeResponseOutputTokens == nil &&
		request.RelayHeaderTimeoutSeconds == nil &&
		request.SmartScheduleEnabled == nil &&
		request.SmartScheduleGroupPolicies == nil &&
		request.SmartSchedulePerformanceWindowMinutes == nil &&
		request.SmartScheduleStabilityWindowMinutes == nil &&
		request.SmartScheduleRealtimeRetentionMinutes == nil &&
		request.SmartScheduleRealtimeSampleLimit == nil &&
		request.SmartScheduleRateLimitCooldownSeconds == nil &&
		request.SmartScheduleForceReset == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供要更新的设置"})
		return
	}
	forceResetSmartSchedule := request.SmartScheduleForceReset != nil && *request.SmartScheduleForceReset
	smartScheduleSettingsChanged := request.SmartScheduleEnabled != nil ||
		request.SmartScheduleGroupPolicies != nil ||
		request.SmartSchedulePerformanceWindowMinutes != nil ||
		request.SmartScheduleStabilityWindowMinutes != nil ||
		request.SmartScheduleRealtimeRetentionMinutes != nil ||
		request.SmartScheduleRealtimeSampleLimit != nil ||
		request.SmartScheduleRateLimitCooldownSeconds != nil || forceResetSmartSchedule
	if err := validateChannelMonitorRetentionRequest(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	settings, err := loadChannelMonitorSettingsSnapshot(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if smartScheduleSettingsChanged && request.SmartScheduleControlRevision != nil &&
		settings.SmartScheduleControlRevision != *request.SmartScheduleControlRevision {
		if err := model.RefreshChannelSmartScheduleOptions(); err != nil {
			common.ApiError(c, err)
			return
		}
		settings, err = loadChannelMonitorSettingsSnapshot(c.Request.Context())
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	values := make(map[string]string, 32)
	if request.ErrorMessageMapping != nil {
		errorMessageMapping := strings.TrimSpace(*request.ErrorMessageMapping)
		if err := service.ValidateErrorMessageMapping(errorMessageMapping); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		settings.ErrorMessageMapping = errorMessageMapping
		values[channelMonitorErrorMessageMappingOption] = errorMessageMapping
	}
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
	if request.RouteMetricRetentionDays != nil && (*request.RouteMetricRetentionDays < minChannelMonitorCostRetentionDays ||
		*request.RouteMetricRetentionDays > maxChannelMonitorCostRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "路由分钟指标保留天数必须在 1 到 3650 天之间",
		})
		return
	}
	if request.RouteMetricRetentionDays != nil {
		settings.RouteMetricRetentionDays = *request.RouteMetricRetentionDays
		values[channelMonitorRouteMetricRetentionDaysOption] = strconv.Itoa(settings.RouteMetricRetentionDays)
	}
	if request.APIKeyMetricRetentionDays != nil && (*request.APIKeyMetricRetentionDays < minChannelMonitorCostRetentionDays ||
		*request.APIKeyMetricRetentionDays > maxChannelMonitorCostRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "API Key 分钟指标保留天数必须在 1 到 3650 天之间",
		})
		return
	}
	if request.APIKeyMetricRetentionDays != nil {
		settings.APIKeyMetricRetentionDays = *request.APIKeyMetricRetentionDays
		values[channelMonitorAPIKeyMetricRetentionDaysOption] = strconv.Itoa(settings.APIKeyMetricRetentionDays)
	}
	if request.ExecutionDetailRetentionDays != nil && (*request.ExecutionDetailRetentionDays < minChannelMonitorCostRetentionDays ||
		*request.ExecutionDetailRetentionDays > maxChannelMonitorCostRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "智能调度执行明细保留天数必须在 1 到 3650 天之间",
		})
		return
	}
	if request.ExecutionDetailRetentionDays != nil {
		settings.ExecutionDetailRetentionDays = *request.ExecutionDetailRetentionDays
		values[channelMonitorExecutionDetailRetentionDaysOption] = strconv.Itoa(settings.ExecutionDetailRetentionDays)
	}
	if request.TaskRetentionDays != nil && (*request.TaskRetentionDays < minChannelMonitorCostRetentionDays ||
		*request.TaskRetentionDays > maxChannelMonitorCostRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "渠道监控任务保留天数必须在 1 到 3650 天之间",
		})
		return
	}
	if request.TaskRetentionDays != nil {
		settings.TaskRetentionDays = *request.TaskRetentionDays
		values[channelMonitorTaskRetentionDaysOption] = strconv.Itoa(settings.TaskRetentionDays)
	}
	if request.RatioHistoryRetentionDays != nil && (*request.RatioHistoryRetentionDays < minChannelMonitorCostRetentionDays ||
		*request.RatioHistoryRetentionDays > maxChannelMonitorCostRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "倍率变更历史保留天数必须在 1 到 3650 天之间",
		})
		return
	}
	if request.RatioHistoryRetentionDays != nil {
		settings.RatioHistoryRetentionDays = *request.RatioHistoryRetentionDays
		values[channelMonitorRatioHistoryRetentionDaysOption] = strconv.Itoa(settings.RatioHistoryRetentionDays)
	}
	if request.StatusProbeHistoryRetentionDays != nil &&
		(*request.StatusProbeHistoryRetentionDays < minChannelMonitorCostRetentionDays ||
			*request.StatusProbeHistoryRetentionDays > maxChannelMonitorStatusProbeHistoryRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "状态探测执行记录保留天数必须在 1 到 90 天之间",
		})
		return
	}
	if request.StatusProbeHistoryRetentionDays != nil {
		settings.StatusProbeHistoryRetentionDays = *request.StatusProbeHistoryRetentionDays
		values[channelMonitorStatusProbeHistoryRetentionDaysOption] = strconv.Itoa(settings.StatusProbeHistoryRetentionDays)
	}
	if request.ModelDetectionRetentionDays != nil &&
		(*request.ModelDetectionRetentionDays < model.ChannelModelDetectionMinRetentionDays ||
			*request.ModelDetectionRetentionDays > model.ChannelModelDetectionMaxRetentionDays) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "模型检测历史保留天数必须在 7 到 180 天之间",
		})
		return
	}
	if request.ModelDetectionRetentionDays != nil {
		settings.ModelDetectionRetentionDays = *request.ModelDetectionRetentionDays
		values[channelMonitorModelDetectionRetentionDaysOption] = strconv.Itoa(settings.ModelDetectionRetentionDays)
	}
	if request.CleanupEnabled != nil {
		settings.CleanupEnabled = *request.CleanupEnabled
		values[channelMonitorCleanupEnabledOption] = strconv.FormatBool(settings.CleanupEnabled)
	}
	if request.CleanupBatchSize != nil && (*request.CleanupBatchSize < minChannelMonitorCleanupBatchSize ||
		*request.CleanupBatchSize > maxChannelMonitorCleanupBatchSize) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "清理批次大小必须在 1 到 10000 条之间",
		})
		return
	}
	if request.CleanupBatchSize != nil {
		settings.CleanupBatchSize = *request.CleanupBatchSize
		values[channelMonitorCleanupBatchSizeOption] = strconv.Itoa(settings.CleanupBatchSize)
	}
	if request.CleanupBudgetSeconds != nil && (*request.CleanupBudgetSeconds < minChannelMonitorCleanupBudgetSeconds ||
		*request.CleanupBudgetSeconds > maxChannelMonitorCleanupBudgetSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "单轮清理预算必须在 1 到 300 秒之间",
		})
		return
	}
	if request.CleanupBudgetSeconds != nil {
		settings.CleanupBudgetSeconds = *request.CleanupBudgetSeconds
		values[channelMonitorCleanupBudgetSecondsOption] = strconv.Itoa(settings.CleanupBudgetSeconds)
	}
	if request.CleanupContinuationSeconds != nil &&
		(*request.CleanupContinuationSeconds < minChannelMonitorCleanupContinuationSeconds ||
			*request.CleanupContinuationSeconds > maxChannelMonitorCleanupContinuationSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "清理续跑间隔必须在 15 到 3600 秒之间",
		})
		return
	}
	if request.CleanupContinuationSeconds != nil {
		settings.CleanupContinuationSeconds = *request.CleanupContinuationSeconds
		values[channelMonitorCleanupContinuationSecondsOption] = strconv.Itoa(settings.CleanupContinuationSeconds)
	}
	if request.CleanupIntervalMinutes != nil &&
		(*request.CleanupIntervalMinutes < minChannelMonitorCleanupIntervalMinutes ||
			*request.CleanupIntervalMinutes > maxChannelMonitorCleanupIntervalMinutes) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "清理周期必须在 60 到 10080 分钟之间",
		})
		return
	}
	if request.CleanupIntervalMinutes != nil {
		settings.CleanupIntervalMinutes = *request.CleanupIntervalMinutes
		values[channelMonitorCleanupIntervalMinutesOption] = strconv.Itoa(settings.CleanupIntervalMinutes)
	}
	if settings.TaskRetentionDays < settings.ExecutionDetailRetentionDays {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "监控任务保留天数不能小于调度执行明细保留天数",
		})
		return
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
	probeResponseSettingsChanged := request.ProbeResponseEnabled != nil ||
		request.ProbeResponseMatchInput != nil ||
		request.ProbeResponseText != nil ||
		request.ProbeResponseMinDelayMs != nil ||
		request.ProbeResponseMaxDelayMs != nil ||
		request.ProbeResponseInputTokens != nil ||
		request.ProbeResponseCacheWriteTokens != nil ||
		request.ProbeResponseCachedTokens != nil ||
		request.ProbeResponseOutputTokens != nil
	if probeResponseSettingsChanged {
		probeResponseConfig := channelprobe.ResponseConfig{
			Enabled:          settings.ProbeResponseEnabled,
			MatchInput:       settings.ProbeResponseMatchInput,
			ResponseText:     settings.ProbeResponseText,
			MinDelayMs:       settings.ProbeResponseMinDelayMs,
			MaxDelayMs:       settings.ProbeResponseMaxDelayMs,
			InputTokens:      settings.ProbeResponseInputTokens,
			CacheWriteTokens: settings.ProbeResponseCacheWriteTokens,
			CachedTokens:     settings.ProbeResponseCachedTokens,
			OutputTokens:     settings.ProbeResponseOutputTokens,
		}
		if request.ProbeResponseEnabled != nil {
			probeResponseConfig.Enabled = *request.ProbeResponseEnabled
		}
		if request.ProbeResponseMatchInput != nil {
			probeResponseConfig.MatchInput = *request.ProbeResponseMatchInput
		}
		if request.ProbeResponseText != nil {
			probeResponseConfig.ResponseText = *request.ProbeResponseText
		}
		if request.ProbeResponseMinDelayMs != nil {
			probeResponseConfig.MinDelayMs = *request.ProbeResponseMinDelayMs
		}
		if request.ProbeResponseMaxDelayMs != nil {
			probeResponseConfig.MaxDelayMs = *request.ProbeResponseMaxDelayMs
		}
		if request.ProbeResponseInputTokens != nil {
			probeResponseConfig.InputTokens = *request.ProbeResponseInputTokens
		}
		if request.ProbeResponseCacheWriteTokens != nil {
			probeResponseConfig.CacheWriteTokens = *request.ProbeResponseCacheWriteTokens
		}
		if request.ProbeResponseCachedTokens != nil {
			probeResponseConfig.CachedTokens = *request.ProbeResponseCachedTokens
		}
		if request.ProbeResponseOutputTokens != nil {
			probeResponseConfig.OutputTokens = *request.ProbeResponseOutputTokens
		}
		normalizedProbeResponseConfig, err := channelprobe.NormalizeResponseConfig(probeResponseConfig)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		settings.ProbeResponseEnabled = normalizedProbeResponseConfig.Enabled
		settings.ProbeResponseMatchInput = normalizedProbeResponseConfig.MatchInput
		settings.ProbeResponseText = normalizedProbeResponseConfig.ResponseText
		settings.ProbeResponseMinDelayMs = normalizedProbeResponseConfig.MinDelayMs
		settings.ProbeResponseMaxDelayMs = normalizedProbeResponseConfig.MaxDelayMs
		settings.ProbeResponseInputTokens = normalizedProbeResponseConfig.InputTokens
		settings.ProbeResponseCacheWriteTokens = normalizedProbeResponseConfig.CacheWriteTokens
		settings.ProbeResponseCachedTokens = normalizedProbeResponseConfig.CachedTokens
		settings.ProbeResponseOutputTokens = normalizedProbeResponseConfig.OutputTokens
		if request.ProbeResponseEnabled != nil {
			values[channelMonitorProbeResponseOption] = strconv.FormatBool(settings.ProbeResponseEnabled)
		}
		if request.ProbeResponseMatchInput != nil {
			values[channelMonitorProbeResponseMatchInputOption] = settings.ProbeResponseMatchInput
		}
		if request.ProbeResponseText != nil {
			values[channelMonitorProbeResponseTextOption] = settings.ProbeResponseText
		}
		if request.ProbeResponseMinDelayMs != nil {
			values[channelMonitorProbeResponseMinDelayMsOption] = strconv.Itoa(settings.ProbeResponseMinDelayMs)
		}
		if request.ProbeResponseMaxDelayMs != nil {
			values[channelMonitorProbeResponseMaxDelayMsOption] = strconv.Itoa(settings.ProbeResponseMaxDelayMs)
		}
		if request.ProbeResponseInputTokens != nil {
			values[channelMonitorProbeResponseInputTokensOption] = strconv.Itoa(settings.ProbeResponseInputTokens)
		}
		if request.ProbeResponseCacheWriteTokens != nil {
			values[channelMonitorProbeResponseCacheWriteTokensOption] = strconv.Itoa(settings.ProbeResponseCacheWriteTokens)
		}
		if request.ProbeResponseCachedTokens != nil {
			values[channelMonitorProbeResponseCachedTokensOption] = strconv.Itoa(settings.ProbeResponseCachedTokens)
		}
		if request.ProbeResponseOutputTokens != nil {
			values[channelMonitorProbeResponseOutputTokensOption] = strconv.Itoa(settings.ProbeResponseOutputTokens)
		}
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
	if request.SmartSchedulePerformanceWindowMinutes != nil &&
		!isChannelMonitorSmartScheduleWindowSupported(*request.SmartSchedulePerformanceWindowMinutes) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "智能调度性能窗口必须在 1 到 1440 分钟之间"})
		return
	}
	if request.SmartSchedulePerformanceWindowMinutes != nil {
		settings.SmartSchedulePerformanceWindowMinutes = *request.SmartSchedulePerformanceWindowMinutes
		values[channelMonitorSmartSchedulePerformanceWindowOption] = strconv.Itoa(settings.SmartSchedulePerformanceWindowMinutes)
	}
	if request.SmartScheduleStabilityWindowMinutes != nil &&
		!isChannelMonitorSmartScheduleWindowSupported(*request.SmartScheduleStabilityWindowMinutes) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "智能调度稳定性评分窗口必须在 1 到 1440 分钟之间"})
		return
	}
	legacyStabilityWindowMinutes := settings.SmartScheduleStabilityWindowMinutes
	if request.SmartScheduleStabilityWindowMinutes != nil {
		legacyStabilityWindowMinutes = *request.SmartScheduleStabilityWindowMinutes
	}
	if request.SmartScheduleGroupPolicies != nil {
		groupPolicies, err := normalizeChannelSmartScheduleGroupPoliciesWithLegacyStabilityWindow(
			*request.SmartScheduleGroupPolicies, legacyStabilityWindowMinutes,
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		settings.SmartScheduleGroupPolicies = groupPolicies
	} else if request.SmartScheduleStabilityWindowMinutes != nil {
		for index := range settings.SmartScheduleGroupPolicies {
			value := legacyStabilityWindowMinutes
			settings.SmartScheduleGroupPolicies[index].StabilityWindowMinutes = &value
		}
	}
	if request.SmartScheduleGroupPolicies != nil || request.SmartScheduleStabilityWindowMinutes != nil {
		serializedGroupPolicies, err := common.Marshal(settings.SmartScheduleGroupPolicies)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		settings.SmartScheduleStabilityWindowMinutes = settings.SmartScheduleGroupPolicies.maxStabilityWindowMinutes(
			legacyStabilityWindowMinutes,
		)
		values[channelMonitorSmartScheduleGroupPoliciesOption] = string(serializedGroupPolicies)
		values[channelMonitorSmartScheduleStabilityWindowOption] = strconv.Itoa(settings.SmartScheduleStabilityWindowMinutes)
		values[channelMonitorSmartScheduleEnabledOption] = strconv.FormatBool(settings.SmartScheduleEnabled)
	}
	if request.SmartScheduleRealtimeRetentionMinutes != nil &&
		(*request.SmartScheduleRealtimeRetentionMinutes < minChannelMonitorSmartScheduleRealtimeRetentionMinutes ||
			*request.SmartScheduleRealtimeRetentionMinutes > maxChannelMonitorSmartScheduleRealtimeRetentionMinutes) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "智能调度实时样本保留时间必须在 5 到 1440 分钟之间",
		})
		return
	}
	if request.SmartScheduleRealtimeRetentionMinutes != nil {
		settings.SmartScheduleRealtimeRetentionMinutes = *request.SmartScheduleRealtimeRetentionMinutes
		values[channelMonitorSmartScheduleRealtimeRetentionOption] = strconv.Itoa(settings.SmartScheduleRealtimeRetentionMinutes)
	}
	if request.SmartScheduleRealtimeSampleLimit != nil &&
		(*request.SmartScheduleRealtimeSampleLimit < minChannelMonitorSmartScheduleRealtimeSampleLimit ||
			*request.SmartScheduleRealtimeSampleLimit > maxChannelMonitorSmartScheduleRealtimeSampleLimit) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "智能调度单路由实时样本上限必须在 1000 到 200000 条之间",
		})
		return
	}
	if request.SmartScheduleRealtimeSampleLimit != nil {
		settings.SmartScheduleRealtimeSampleLimit = *request.SmartScheduleRealtimeSampleLimit
		values[channelMonitorSmartScheduleRealtimeSampleLimitOption] = strconv.Itoa(settings.SmartScheduleRealtimeSampleLimit)
	}
	if request.SmartScheduleRealtimeRetentionMinutes == nil {
		requiredRetentionMinutes := max(
			settings.SmartSchedulePerformanceWindowMinutes,
			settings.SmartScheduleStabilityWindowMinutes,
		)
		if settings.SmartScheduleRealtimeRetentionMinutes < requiredRetentionMinutes {
			settings.SmartScheduleRealtimeRetentionMinutes = requiredRetentionMinutes
			values[channelMonitorSmartScheduleRealtimeRetentionOption] = strconv.Itoa(requiredRetentionMinutes)
		}
	}
	if settings.SmartScheduleRealtimeRetentionMinutes < settings.SmartSchedulePerformanceWindowMinutes {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "实时样本保留时间不能短于性能窗口",
		})
		return
	}
	if settings.SmartScheduleRealtimeRetentionMinutes < settings.SmartScheduleStabilityWindowMinutes {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "实时样本保留时间不能短于稳定性评分窗口",
		})
		return
	}
	if request.SmartScheduleRateLimitCooldownSeconds != nil &&
		(*request.SmartScheduleRateLimitCooldownSeconds < 0 ||
			*request.SmartScheduleRateLimitCooldownSeconds > maxChannelMonitorSmartScheduleRateLimitCooldownSeconds) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "429 冷却时间必须在 0 到 300 秒之间",
		})
		return
	}
	if request.SmartScheduleRateLimitCooldownSeconds != nil {
		settings.SmartScheduleRateLimitCooldownSeconds = *request.SmartScheduleRateLimitCooldownSeconds
		values[channelMonitorSmartScheduleRateLimitCooldownOption] = strconv.Itoa(settings.SmartScheduleRateLimitCooldownSeconds)
	}
	if forceResetSmartSchedule && !settings.SmartScheduleEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "启用智能调度后才能强制重置"})
		return
	}
	if (request.SmartScheduleEnabled != nil || request.SmartScheduleGroupPolicies != nil || forceResetSmartSchedule) &&
		settings.SmartScheduleEnabled && len(settings.SmartScheduleGroupPolicies) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "启用智能调度前请至少配置一个完整的分组策略"})
		return
	}
	if smartScheduleSettingsChanged {
		if request.SmartScheduleControlRevision == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "智能调度设置缺少配置修订号，请刷新后重试",
			})
			return
		}
		values[channelMonitorSmartScheduleControlRevisionOption] = common.GetUUID()
	}
	var expectedSmartScheduleControlRevision *string
	if smartScheduleSettingsChanged {
		expectedSmartScheduleControlRevision = request.SmartScheduleControlRevision
	}
	routingChanged, err := model.UpdateChannelMonitorSettingsOptions(
		values,
		smartScheduleSettingsChanged,
		expectedSmartScheduleControlRevision,
	)
	if err != nil {
		if errors.Is(err, model.ErrChannelMonitorSettingsChanged) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	if routingChanged {
		model.InitChannelCache()
	}
	settings, err = loadChannelMonitorSettingsSnapshot(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	previousSmartScheduleControlRevision := settings.SmartScheduleControlRevision
	if expectedSmartScheduleControlRevision != nil {
		previousSmartScheduleControlRevision = *expectedSmartScheduleControlRevision
	}
	cooldownRevisionApplied := true
	var cooldownRevisionError error
	if smartScheduleSettingsChanged {
		cooldownRevisionApplied, cooldownRevisionError = service.UpdateChannelRateLimitCooldownControlRevision(
			values[channelMonitorSmartScheduleControlRevisionOption],
			previousSmartScheduleControlRevision,
		)
	}
	forceResetTaskCreated := false
	forceResetTaskId := ""
	forceResetTaskError := ""
	var scheduleTaskError error
	scheduleTaskRequested := smartScheduleSettingsChanged && settings.SmartScheduleEnabled
	if scheduleTaskRequested && cooldownRevisionError != nil && forceResetSmartSchedule {
		forceResetTaskError = "429 冷却状态同步失败，未创建强制重置任务"
	} else if scheduleTaskRequested && !cooldownRevisionApplied && forceResetSmartSchedule {
		forceResetTaskError = "智能调度配置已被更新的修订覆盖，未创建强制重置任务"
	} else if scheduleTaskRequested && cooldownRevisionError == nil && cooldownRevisionApplied {
		payload := newChannelSmartScheduleTaskPayload("settings_update", "smart_schedule_settings_changed")
		payload.ForceReset = forceResetSmartSchedule
		task, created, err := service.EnqueueRequiredSystemTask(
			channelMonitorSmartScheduleTaskType,
			payload,
		)
		scheduleTaskError = err
		if forceResetSmartSchedule {
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
	}
	auditDetails := map[string]interface{}{
		"auto_update_interval_minutes":               settings.AutoUpdateIntervalMinutes,
		"auto_update_retry_count":                    settings.AutoUpdateRetryCount,
		"upstream_request_timeout_seconds":           settings.UpstreamRequestTimeoutSeconds,
		"auto_update_consecutive_failure_limit":      settings.AutoUpdateConsecutiveFailureLimit,
		"auto_disable_on_update_failure":             settings.AutoDisableOnUpdateFailure,
		"auto_enable_on_cost_ratio_recovery":         settings.AutoEnableOnCostRatioRecovery,
		"auto_enable_on_balance_recovery":            settings.AutoEnableOnBalanceRecovery,
		"cost_retention_days":                        settings.CostRetentionDays,
		"route_metric_retention_days":                settings.RouteMetricRetentionDays,
		"api_key_metric_retention_days":              settings.APIKeyMetricRetentionDays,
		"execution_detail_retention_days":            settings.ExecutionDetailRetentionDays,
		"task_retention_days":                        settings.TaskRetentionDays,
		"ratio_history_retention_days":               settings.RatioHistoryRetentionDays,
		"status_probe_history_retention_days":        settings.StatusProbeHistoryRetentionDays,
		"model_detection_retention_days":             settings.ModelDetectionRetentionDays,
		"cleanup_enabled":                            settings.CleanupEnabled,
		"cleanup_batch_size":                         settings.CleanupBatchSize,
		"cleanup_budget_seconds":                     settings.CleanupBudgetSeconds,
		"cleanup_continuation_seconds":               settings.CleanupContinuationSeconds,
		"cleanup_interval_minutes":                   settings.CleanupIntervalMinutes,
		"email_notification_enabled":                 settings.EmailNotificationEnabled,
		"notification_email_configured":              settings.NotificationEmail != "",
		"email_notification_types":                   settings.EmailNotificationTypes,
		"error_message_mapping_configured":           strings.TrimSpace(settings.ErrorMessageMapping) != "",
		"probe_response_enabled":                     settings.ProbeResponseEnabled,
		"probe_response_match_input":                 settings.ProbeResponseMatchInput,
		"probe_response_text":                        settings.ProbeResponseText,
		"probe_response_min_delay_ms":                settings.ProbeResponseMinDelayMs,
		"probe_response_max_delay_ms":                settings.ProbeResponseMaxDelayMs,
		"probe_response_input_tokens":                settings.ProbeResponseInputTokens,
		"probe_response_cache_write_tokens":          settings.ProbeResponseCacheWriteTokens,
		"probe_response_cached_tokens":               settings.ProbeResponseCachedTokens,
		"probe_response_output_tokens":               settings.ProbeResponseOutputTokens,
		"smart_schedule_enabled":                     settings.SmartScheduleEnabled,
		"smart_schedule_group_policies":              settings.SmartScheduleGroupPolicies,
		"smart_schedule_performance_window_minutes":  settings.SmartSchedulePerformanceWindowMinutes,
		"smart_schedule_stability_window_minutes":    settings.SmartScheduleStabilityWindowMinutes,
		"smart_schedule_realtime_retention_minutes":  settings.SmartScheduleRealtimeRetentionMinutes,
		"smart_schedule_realtime_sample_limit":       settings.SmartScheduleRealtimeSampleLimit,
		"smart_schedule_rate_limit_cooldown_seconds": settings.SmartScheduleRateLimitCooldownSeconds,
		"smart_schedule_force_reset":                 forceResetSmartSchedule,
		"smart_schedule_force_reset_created":         forceResetTaskCreated,
		"smart_schedule_force_reset_task_id":         forceResetTaskId,
		"smart_schedule_force_reset_error":           forceResetTaskError,
		"smart_schedule_cooldown_revision_applied":   cooldownRevisionApplied,
	}
	if cooldownRevisionError != nil {
		auditDetails["smart_schedule_cooldown_revision_error"] = cooldownRevisionError.Error()
	}
	if scheduleTaskError != nil {
		auditDetails["smart_schedule_task_error"] = scheduleTaskError.Error()
	}
	auditDetails["relay_response_header_timeout_seconds"] = settings.RelayHeaderTimeoutSeconds
	auditDetails["smart_schedule_status"] = "关闭"
	if settings.SmartScheduleEnabled {
		auditDetails["smart_schedule_status"] = "开启"
	}
	auditDetails["email_notification_status"] = "关闭"
	if settings.EmailNotificationEnabled {
		auditDetails["email_notification_status"] = "开启"
	}
	auditDetails["probe_response_status"] = "关闭"
	if settings.ProbeResponseEnabled {
		auditDetails["probe_response_status"] = "开启"
	}
	recordManageAudit(c, "channel.monitor_settings_changed", auditDetails)
	if cooldownRevisionError != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "设置已保存，但 429 冷却状态同步失败，请重试当前设置",
		})
		return
	}
	if !cooldownRevisionApplied {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "设置已保存，但已被更新的智能调度配置覆盖，请刷新后确认",
		})
		return
	}
	if scheduleTaskError != nil && !forceResetSmartSchedule {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "设置已保存，但智能调度更新任务创建失败，请重试当前设置",
		})
		return
	}
	common.ApiSuccess(c, settings)
}

func validateChannelMonitorRetentionRequest(request channelMonitorSettingsUpdateRequest) error {
	validateDays := func(value *int, min int, max int, message string) error {
		if value != nil && (*value < min || *value > max) {
			return errors.New(message)
		}
		return nil
	}
	if err := validateDays(request.CostRetentionDays, minChannelMonitorCostRetentionDays,
		maxChannelMonitorCostRetentionDays, "成本保留天数必须在 1 到 3650 天之间"); err != nil {
		return err
	}
	if err := validateDays(request.RouteMetricRetentionDays, minChannelMonitorCostRetentionDays,
		maxChannelMonitorCostRetentionDays, "路由分钟指标保留天数必须在 1 到 3650 天之间"); err != nil {
		return err
	}
	if err := validateDays(request.APIKeyMetricRetentionDays, minChannelMonitorCostRetentionDays,
		maxChannelMonitorCostRetentionDays, "API Key 分钟指标保留天数必须在 1 到 3650 天之间"); err != nil {
		return err
	}
	if err := validateDays(request.ExecutionDetailRetentionDays, minChannelMonitorCostRetentionDays,
		maxChannelMonitorCostRetentionDays, "执行明细保留天数必须在 1 到 3650 天之间"); err != nil {
		return err
	}
	if err := validateDays(request.TaskRetentionDays, minChannelMonitorCostRetentionDays,
		maxChannelMonitorCostRetentionDays, "监控任务保留天数必须在 1 到 3650 天之间"); err != nil {
		return err
	}
	if err := validateDays(request.RatioHistoryRetentionDays, minChannelMonitorCostRetentionDays,
		maxChannelMonitorCostRetentionDays, "倍率历史保留天数必须在 1 到 3650 天之间"); err != nil {
		return err
	}
	if err := validateDays(request.StatusProbeHistoryRetentionDays, minChannelMonitorCostRetentionDays,
		maxChannelMonitorStatusProbeHistoryRetentionDays, "状态探测历史保留天数必须在 1 到 90 天之间"); err != nil {
		return err
	}
	if err := validateDays(request.ModelDetectionRetentionDays, model.ChannelModelDetectionMinRetentionDays,
		model.ChannelModelDetectionMaxRetentionDays, "模型检测历史保留天数必须在 7 到 180 天之间"); err != nil {
		return err
	}
	if request.TaskRetentionDays != nil && request.ExecutionDetailRetentionDays != nil &&
		*request.TaskRetentionDays < *request.ExecutionDetailRetentionDays {
		return errors.New("监控任务保留天数不能小于调度执行明细保留天数")
	}
	if request.CleanupBatchSize != nil && (*request.CleanupBatchSize < minChannelMonitorCleanupBatchSize ||
		*request.CleanupBatchSize > maxChannelMonitorCleanupBatchSize) {
		return errors.New("清理批次大小必须在 1 到 10000 条之间")
	}
	if request.CleanupBudgetSeconds != nil && (*request.CleanupBudgetSeconds < minChannelMonitorCleanupBudgetSeconds ||
		*request.CleanupBudgetSeconds > maxChannelMonitorCleanupBudgetSeconds) {
		return errors.New("单轮清理预算必须在 1 到 300 秒之间")
	}
	if request.CleanupContinuationSeconds != nil &&
		(*request.CleanupContinuationSeconds < minChannelMonitorCleanupContinuationSeconds ||
			*request.CleanupContinuationSeconds > maxChannelMonitorCleanupContinuationSeconds) {
		return errors.New("清理续跑间隔必须在 15 到 3600 秒之间")
	}
	if request.CleanupIntervalMinutes != nil &&
		(*request.CleanupIntervalMinutes < minChannelMonitorCleanupIntervalMinutes ||
			*request.CleanupIntervalMinutes > maxChannelMonitorCleanupIntervalMinutes) {
		return errors.New("清理周期必须在 60 到 10080 分钟之间")
	}
	return nil
}
