package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type channelMonitorSettingsAPIResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    channelMonitorSettings `json:"data"`
}

type channelMonitorGroupSyncAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Group            string  `json:"group"`
		UpstreamRatio    float64 `json:"upstream_ratio"`
		CostRatio        float64 `json:"cost_ratio"`
		ConversionFactor float64 `json:"conversion_factor"`
		Coefficient      float64 `json:"coefficient"`
		Ratio            float64 `json:"ratio"`
	} `json:"data"`
}

type channelMonitorUpstreamConfigAPIResponse struct {
	Success bool                         `json:"success"`
	Data    channelMonitorUpstreamConfig `json:"data"`
}

type channelMonitorUpstreamGroupsAPIResponse struct {
	Success bool                                       `json:"success"`
	Data    service.ChannelMonitorUpstreamGroupsResult `json:"data"`
}

type channelMonitorUpstreamGroupApplyAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Result      service.NewAPIGroupRatioResult `json:"result"`
		KeysUpdated int                            `json:"keys_updated"`
		Changed     bool                           `json:"changed"`
	} `json:"data"`
}

type channelMonitorUpstreamBalanceAPIResponse struct {
	Success bool                                        `json:"success"`
	Data    service.ChannelMonitorUpstreamBalanceResult `json:"data"`
}

type channelMonitorOverviewAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Channels          []channelMonitorItem `json:"channels"`
		ChannelOrder      []int                `json:"channel_order"`
		GroupRatios       map[string]float64   `json:"group_ratios"`
		GroupCoefficients map[string]float64   `json:"group_coefficients"`
	} `json:"data"`
}

type channelMonitorOrderAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ChannelOrder []int `json:"channel_order"`
	} `json:"data"`
}

type channelMonitorConcurrencyAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ConcurrencyLimit int `json:"concurrency_limit"`
	} `json:"data"`
}

type channelMonitorTaskRunAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Created bool                     `json:"created"`
		Task    model.SystemTaskResponse `json:"task"`
	} `json:"data"`
}

func useChannelMonitorOptionMap(t *testing.T, values map[string]string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = make(map[string]string, len(values))
	for key, value := range values {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})
}

func usePersistedChannelMonitorOptions(t *testing.T, db *gorm.DB, values map[string]string) {
	t.Helper()
	useChannelMonitorOptionMap(t, values)
	for key, value := range values {
		require.NoError(t, db.Save(&model.Option{Key: key, Value: value}).Error)
	}
}

func TestChannelMonitorSettingsReportsInvalidSmartSchedulePolicy(t *testing.T) {
	settings := channelMonitorSettingsFromOptions(map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: `{`,
	})

	assert.False(t, settings.SmartScheduleEnabled)
	assert.Contains(t, settings.SmartScheduleConfigError, "JSON 无效")
	assert.Empty(t, settings.SmartScheduleGroupPolicies)
}

func setupChannelMonitorControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	originalIsMasterNode := common.IsMasterNode

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled = false
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	common.RedisEnabled = true
	common.RDB = redisClient
	common.IsMasterNode = true
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	service.ResetChannelDailyCostSnapshotCache()
	require.NoError(t, db.AutoMigrate(
		&model.Option{},
		&model.User{},
		&model.Log{},
		&model.Channel{},
		&model.Ability{},
		&model.ChannelRatioMonitor{},
		&model.ChannelSmartScheduleRouteState{},
		&model.ChannelSmartScheduleGroupPause{},
		&model.ChannelSmartScheduleModelSampleState{},
		&model.ChannelRatioHistory{},
		&model.ChannelDailyCost{},
		&model.ChannelDailyAPIKeyCost{},
		&model.ChannelMonitorMinuteRouteMetric{},
		&model.ChannelMonitorMinuteAPIKeyMetric{},
		&model.ChannelMonitorAggregationState{},
		&model.ChannelMonitorRedisEffectState{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	))
	redisRuntime, err := service.StartChannelMonitorRedisRuntime()
	require.NoError(t, err)
	require.NoError(t, service.ReloadChannelConcurrencyLimits(context.Background()))

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		require.NoError(t, redisRuntime.Stop(stopCtx))
		stopCancel()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		common.IsMasterNode = originalIsMasterNode
		service.ResetChannelDailyCostSnapshotCache()
		require.NoError(t, redisClient.Close())
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func startChannelMonitorEventWriterForTest(t *testing.T) {
	t.Helper()
	writer, err := service.StartChannelMonitorEventWriter()
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, writer.Stop(ctx))
	})
}

func aggregateChannelMonitorTestLogs(startTimestamp int64, endTimestamp int64) error {
	endTimestamp -= endTimestamp % 60
	_, err := model.AggregateChannelMonitorMinuteRange(context.Background(), startTimestamp, endTimestamp)
	return err
}

func emitChannelMonitorControllerRealtimeEvents(t *testing.T, events ...model.ChannelMonitorEvent) {
	t.Helper()
	for _, event := range events {
		require.NoError(t, projectChannelSmartScheduleTestEvent(event))
	}
}

var channelSmartScheduleTestEventSequence atomic.Uint64

func projectChannelSmartScheduleTestEvent(event model.ChannelMonitorEvent) error {
	event.EventSequence = channelSmartScheduleTestEventSequence.Add(1)
	routeProjection, err := service.NewChannelMonitorRedisRouteHealthProjectionForClient(common.RDB)
	if err != nil {
		return err
	}
	if err := routeProjection.HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event}); err != nil {
		return err
	}
	return service.NewChannelMonitorRedisSharedProjectionWithClient(common.RDB).
		HandleChannelMonitorEvents(context.Background(), []model.ChannelMonitorEvent{event})
}

func projectChannelSmartScheduleMetricEventForTest(
	channelId int,
	group string,
	modelName string,
	timestamp int64,
	success bool,
	firstTokenMs *float64,
	tPS *float64,
	attemptDurationMs *int64,
	retry bool,
) error {
	outcome := model.ChannelMonitorEventOutcomeSuccess
	if !success {
		outcome = model.ChannelMonitorEventOutcomeFailure
	}
	event := model.NewChannelMonitorEvent(
		channelId, model.ChannelMonitorEventSourceBusiness, outcome, timestamp,
	)
	event.GroupName = group
	event.ModelName = modelName
	event.RequestDispatched = true
	event.IsRetryAttempt = retry
	event.IsFinalAttempt = !retry
	event.SchedulingEligible = true
	if !success {
		statusCode := http.StatusInternalServerError
		event.StatusCode = &statusCode
	}
	if firstTokenMs != nil {
		value := *firstTokenMs
		event.FirstTokenMs = &value
	}
	if tPS != nil {
		value := *tPS
		event.TPS = &value
	}
	if attemptDurationMs != nil {
		value := *attemptDurationMs
		event.AttemptDurationMs = &value
	}
	return projectChannelSmartScheduleTestEvent(event)
}

func saveChannelSmartScheduleModelSampleForTest(
	result model.ChannelSmartScheduleModelSampleResult,
) (model.ChannelSmartScheduleModelSampleState, error) {
	state, err := model.SaveChannelSmartScheduleModelSample(result)
	if err != nil {
		return state, err
	}
	source := model.ChannelMonitorEventSourceSmartProbe
	switch result.Source {
	case model.ChannelSmartScheduleSampleSourceManualTest:
		source = model.ChannelMonitorEventSourceManualTest
	case model.ChannelSmartScheduleSampleSourceStatusProbe:
		source = model.ChannelMonitorEventSourceStatusProbe
	}
	outcome := model.ChannelMonitorEventOutcomeSuccess
	if !result.Success {
		outcome = model.ChannelMonitorEventOutcomeFailure
	}
	event := model.NewChannelMonitorEvent(result.ChannelId, source, outcome, result.Time)
	event.ModelName = result.Model
	event.RequestId = result.SampleId
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	event.SchedulingEligible = true
	if result.DurationMs != nil {
		durationMs := int64(*result.DurationMs)
		event.AttemptDurationMs = &durationMs
	}
	if result.FirstTokenMs != nil {
		value := *result.FirstTokenMs
		event.FirstTokenMs = &value
	}
	if result.TPS != nil {
		value := *result.TPS
		event.TPS = &value
	}
	if err := projectChannelSmartScheduleTestEvent(event); err != nil {
		return state, err
	}
	return state, nil
}

func disableChannelMonitorSSRFProtection(t *testing.T) {
	t.Helper()
	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	service.InitHttpClient()
	t.Cleanup(func() {
		*fetchSetting = originalFetchSetting
		service.InitHttpClient()
	})
}

func newChannelMonitorControllerContext(t *testing.T, method string, target string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 1)
	ctx.Set("username", "root")
	return ctx, recorder
}

func TestChannelMonitorSettingsDefaultAndTaskInterval(t *testing.T) {
	tests := []struct {
		name               string
		values             map[string]string
		wantInterval       int
		wantRetryCount     int
		wantRetryDelay     int
		wantRequestTimeout int
		wantFailureLimit   int
		wantAutoDisable    bool
		wantEmailEnabled   bool
		wantProbeEnabled   bool
		wantRelayTimeout   int
		wantEnabled        bool
		wantTaskInterval   time.Duration
	}{
		{
			name:               "missing values are disabled",
			values:             map[string]string{},
			wantRetryCount:     defaultChannelMonitorAutoUpdateRetryCount,
			wantRetryDelay:     defaultChannelMonitorAutoUpdateRetryDelaySeconds,
			wantRequestTimeout: defaultChannelMonitorUpstreamRequestTimeoutSeconds,
			wantFailureLimit:   defaultChannelMonitorAutoUpdateConsecutiveFailureLimit,
			wantTaskInterval:   time.Minute,
		},
		{
			name: "valid values",
			values: map[string]string{
				channelMonitorAutoUpdateIntervalOption:                "30",
				channelMonitorAutoUpdateRetryCountOption:              "4",
				channelMonitorAutoUpdateRetryDelaySecondsOption:       "12",
				channelMonitorUpstreamRequestTimeoutOption:            "45",
				channelMonitorAutoUpdateConsecutiveFailureLimitOption: "5",
				channelMonitorAutoDisableOnUpdateFailureOption:        "true",
				channelMonitorEmailNotificationOption:                 "true",
				channelMonitorNotificationEmailOption:                 "alerts@example.com",
				channelMonitorProbeResponseOption:                     "true",
				common.RelayResponseHeaderTimeoutOptionKey:            "60",
			},
			wantInterval:       30,
			wantRetryCount:     4,
			wantRetryDelay:     12,
			wantRequestTimeout: 45,
			wantFailureLimit:   5,
			wantAutoDisable:    true,
			wantEmailEnabled:   true,
			wantProbeEnabled:   true,
			wantRelayTimeout:   60,
			wantEnabled:        true,
			wantTaskInterval:   30 * time.Minute,
		},
		{
			name: "invalid values use safe defaults",
			values: map[string]string{
				channelMonitorAutoUpdateIntervalOption:                "525601",
				channelMonitorAutoUpdateRetryCountOption:              "11",
				channelMonitorAutoUpdateRetryDelaySecondsOption:       "601",
				channelMonitorUpstreamRequestTimeoutOption:            "601",
				channelMonitorAutoUpdateConsecutiveFailureLimitOption: "101",
				channelMonitorAutoDisableOnUpdateFailureOption:        "invalid",
				channelMonitorEmailNotificationOption:                 "invalid",
				channelMonitorNotificationEmailOption:                 "invalid",
				channelMonitorProbeResponseOption:                     "invalid",
				common.RelayResponseHeaderTimeoutOptionKey:            "601",
			},
			wantRetryCount:     defaultChannelMonitorAutoUpdateRetryCount,
			wantRetryDelay:     defaultChannelMonitorAutoUpdateRetryDelaySeconds,
			wantRequestTimeout: defaultChannelMonitorUpstreamRequestTimeoutSeconds,
			wantFailureLimit:   defaultChannelMonitorAutoUpdateConsecutiveFailureLimit,
			wantTaskInterval:   time.Minute,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useChannelMonitorOptionMap(t, test.values)
			settings := getChannelMonitorSettings()
			assert.Equal(t, test.wantInterval, settings.AutoUpdateIntervalMinutes)
			assert.Equal(t, test.wantRetryCount, settings.AutoUpdateRetryCount)
			assert.Equal(t, test.wantRetryDelay, settings.AutoUpdateRetryDelaySeconds)
			assert.Equal(t, test.wantRequestTimeout, settings.UpstreamRequestTimeoutSeconds)
			assert.Equal(t, test.wantFailureLimit, settings.AutoUpdateConsecutiveFailureLimit)
			assert.Equal(t, test.wantAutoDisable, settings.AutoDisableOnUpdateFailure)
			assert.Equal(t, test.wantEmailEnabled, settings.EmailNotificationEnabled)
			assert.Equal(t, defaultChannelMonitorEmailNotificationTypes(), settings.EmailNotificationTypes)
			assert.Equal(t, test.wantProbeEnabled, settings.ProbeResponseEnabled)
			assert.Equal(t, test.wantRelayTimeout, settings.RelayHeaderTimeoutSeconds)
			if test.name == "valid values" {
				assert.Equal(t, "alerts@example.com", settings.NotificationEmail)
			} else {
				assert.Empty(t, settings.NotificationEmail)
			}

			handler := channelRatioMonitorTaskHandler{}
			assert.Equal(t, test.wantEnabled, handler.Enabled())
			assert.Equal(t, test.wantTaskInterval, handler.Interval())
			assert.Equal(t, model.SystemTaskTypeChannelRatioMonitor, handler.Type())
		})
	}
}

func TestChannelMonitorProbeResponseSettingsUseDefaultsAndStoredValues(t *testing.T) {
	t.Run("missing options keep the existing response contract", func(t *testing.T) {
		useChannelMonitorOptionMap(t, map[string]string{})
		settings := getChannelMonitorSettings()

		assert.Equal(t, channelprobe.DefaultMatchInput, settings.ProbeResponseMatchInput)
		assert.Empty(t, settings.ProbeResponseAllowedIPs)
		assert.Equal(t, channelprobe.DefaultResponseText, settings.ProbeResponseText)
		assert.Equal(t, channelprobe.DefaultMinDelayMs, settings.ProbeResponseMinDelayMs)
		assert.Equal(t, channelprobe.DefaultMaxDelayMs, settings.ProbeResponseMaxDelayMs)
		assert.Equal(t, channelprobe.DefaultInputTokens, settings.ProbeResponseInputTokens)
		assert.Equal(t, channelprobe.DefaultCacheWriteTokens, settings.ProbeResponseCacheWriteTokens)
		assert.Equal(t, channelprobe.DefaultCachedTokens, settings.ProbeResponseCachedTokens)
		assert.Equal(t, channelprobe.DefaultOutputTokens, settings.ProbeResponseOutputTokens)
	})

	t.Run("stored options are returned", func(t *testing.T) {
		useChannelMonitorOptionMap(t, map[string]string{
			channelMonitorProbeResponseOption:                 "true",
			channelMonitorProbeResponseAllowedIPsOption:       "203.0.113.10, 2001:db8::10",
			channelMonitorProbeResponseMatchInputOption:       "health check",
			channelMonitorProbeResponseTextOption:             "healthy",
			channelMonitorProbeResponseMinDelayMsOption:       "125",
			channelMonitorProbeResponseMaxDelayMsOption:       "875",
			channelMonitorProbeResponseInputTokensOption:      "7",
			channelMonitorProbeResponseCacheWriteTokensOption: "1",
			channelMonitorProbeResponseCachedTokensOption:     "2",
			channelMonitorProbeResponseOutputTokensOption:     "11",
		})
		settings := getChannelMonitorSettings()

		assert.True(t, settings.ProbeResponseEnabled)
		assert.Equal(t, "203.0.113.10\n2001:db8::10", settings.ProbeResponseAllowedIPs)
		assert.Equal(t, "health check", settings.ProbeResponseMatchInput)
		assert.Equal(t, "healthy", settings.ProbeResponseText)
		assert.Equal(t, 125, settings.ProbeResponseMinDelayMs)
		assert.Equal(t, 875, settings.ProbeResponseMaxDelayMs)
		assert.Equal(t, 7, settings.ProbeResponseInputTokens)
		assert.Equal(t, 1, settings.ProbeResponseCacheWriteTokens)
		assert.Equal(t, 2, settings.ProbeResponseCachedTokens)
		assert.Equal(t, 11, settings.ProbeResponseOutputTokens)
	})
}

func TestUpdateChannelMonitorProbeResponseSettingsValidatesAndPersists(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	invalidRequests := []map[string]any{
		{"probe_response_match_input": " "},
		{"probe_response_allowed_ips": "not-an-ip"},
		{"probe_response_match_input": strings.Repeat("x", channelprobe.MaxMatchInputLength+1)},
		{"probe_response_text": " "},
		{"probe_response_text": strings.Repeat("x", channelprobe.MaxResponseTextLength+1)},
		{"probe_response_min_delay_ms": -1},
		{"probe_response_min_delay_ms": channelprobe.DefaultMaxDelayMs + 1},
		{"probe_response_max_delay_ms": channelprobe.DefaultMinDelayMs - 1},
		{"probe_response_max_delay_ms": channelprobe.MaxDelayMs + 1},
		{"probe_response_input_tokens": -1},
		{"probe_response_output_tokens": channelprobe.MaxTokenCount + 1},
	}
	for _, request := range invalidRequests {
		ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", request)
		UpdateChannelMonitorSettings(ctx)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	}

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"probe_response_enabled":            true,
		"probe_response_allowed_ips":        " 203.0.113.10, 2001:db8::10 ",
		"probe_response_match_input":        " health check ",
		"probe_response_text":               " healthy ",
		"probe_response_min_delay_ms":       125,
		"probe_response_max_delay_ms":       875,
		"probe_response_input_tokens":       7,
		"probe_response_cache_write_tokens": 1,
		"probe_response_cached_tokens":      2,
		"probe_response_output_tokens":      11,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.True(t, response.Data.ProbeResponseEnabled)
	assert.Equal(t, "203.0.113.10\n2001:db8::10", response.Data.ProbeResponseAllowedIPs)
	assert.Equal(t, "health check", response.Data.ProbeResponseMatchInput)
	assert.Equal(t, "healthy", response.Data.ProbeResponseText)
	assert.Equal(t, 125, response.Data.ProbeResponseMinDelayMs)
	assert.Equal(t, 875, response.Data.ProbeResponseMaxDelayMs)
	assert.Equal(t, 7, response.Data.ProbeResponseInputTokens)
	assert.Equal(t, 1, response.Data.ProbeResponseCacheWriteTokens)
	assert.Equal(t, 2, response.Data.ProbeResponseCachedTokens)
	assert.Equal(t, 11, response.Data.ProbeResponseOutputTokens)

	wantOptions := map[string]string{
		channelMonitorProbeResponseOption:                 "true",
		channelMonitorProbeResponseAllowedIPsOption:       "203.0.113.10\n2001:db8::10",
		channelMonitorProbeResponseMatchInputOption:       "health check",
		channelMonitorProbeResponseTextOption:             "healthy",
		channelMonitorProbeResponseMinDelayMsOption:       "125",
		channelMonitorProbeResponseMaxDelayMsOption:       "875",
		channelMonitorProbeResponseInputTokensOption:      "7",
		channelMonitorProbeResponseCacheWriteTokensOption: "1",
		channelMonitorProbeResponseCachedTokensOption:     "2",
		channelMonitorProbeResponseOutputTokensOption:     "11",
	}
	for key, want := range wantOptions {
		var option model.Option
		require.NoError(t, db.Where("key = ?", key).First(&option).Error)
		assert.Equal(t, want, option.Value)
	}
}

func TestUpdateChannelMonitorErrorMessageMappingValidatesAndPersists(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"error_message_mapping": `{"429":429}`,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	mapping := `{"429":"请求过于频繁，请稍后再试","insufficient_quota":"额度不足，请联系管理员"}`
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"error_message_mapping": mapping,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, mapping, response.Data.ErrorMessageMapping)
	assert.Equal(t, mapping, service.GetConfiguredErrorMessageMapping())

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorErrorMessageMappingOption).First(&option).Error)
	assert.Equal(t, mapping, option.Value)
}

func TestUpdateChannelMonitorErrorMessageWhitelistValidatesAndPersists(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	tooMany := make([]string, service.MaxErrorMessageWhitelistCodes+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("code-%d", index)
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"error_message_whitelist": strings.Join(tooMany, "\n"),
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	whitelist := " provider_specific_error\n503 "
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"error_message_whitelist": whitelist,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, strings.TrimSpace(whitelist), response.Data.ErrorMessageWhitelist)
	assert.Equal(t, strings.TrimSpace(whitelist), service.GetConfiguredErrorMessageWhitelist())

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorErrorMessageWhitelistOption).First(&option).Error)
	assert.Equal(t, strings.TrimSpace(whitelist), option.Value)
}

func TestUpdateChannelMonitorErrorMessageKeywordsValidatesAndPersists(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	tooMany := strings.TrimSuffix(strings.Repeat("keyword\n", service.MaxErrorMessageKeywords+1), "\n")
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"error_message_keywords": tooMany,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)

	keywords := " secret upstream detail \nprovider"
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"error_message_keywords": keywords,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, strings.TrimSpace(keywords), response.Data.ErrorMessageKeywords)
	assert.Equal(t, strings.TrimSpace(keywords), service.GetConfiguredErrorMessageKeywords())

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorErrorMessageKeywordsOption).First(&option).Error)
	assert.Equal(t, strings.TrimSpace(keywords), option.Value)
}

func TestChannelMonitorEmailNotificationTypesDistinguishesMissingFromExplicitEmpty(t *testing.T) {
	for _, raw := range []string{"", "null", "{\"invalid\":true}"} {
		t.Run(raw, func(t *testing.T) {
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorEmailNotificationTypesOption: raw,
			})
			assert.Equal(t, defaultChannelMonitorEmailNotificationTypes(), getChannelMonitorSettings().EmailNotificationTypes)
		})
	}

	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorEmailNotificationTypesOption: "[]",
	})
	assert.Empty(t, getChannelMonitorSettings().EmailNotificationTypes)

	notificationTypes, err := normalizeChannelMonitorEmailNotificationTypes([]string{channelMonitorEmailTypeMonitoringHealth})
	require.NoError(t, err)
	assert.Equal(t, []string{channelMonitorEmailTypeMonitoringHealth}, notificationTypes)
}

func TestChannelSmartScheduleHandlerIsEventDriven(t *testing.T) {
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
		channelMonitorSmartScheduleGroupPoliciesOption: channelSmartScheduleTestGroupPoliciesJSON(t,
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyTPS, true,
				channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
			),
		),
	})

	settings := getChannelMonitorSettings()
	assert.True(t, settings.SmartScheduleEnabled)
	assert.Equal(t, defaultChannelMonitorSmartSchedulePerformanceWindowMinutes, settings.SmartSchedulePerformanceWindowMinutes)
	assert.Equal(t, defaultChannelMonitorSmartScheduleRealtimeRetentionMinutes, settings.SmartScheduleRealtimeRetentionMinutes)
	assert.Equal(t, defaultChannelMonitorSmartScheduleRealtimeSampleLimit, settings.SmartScheduleRealtimeSampleLimit)
	assert.Equal(t, defaultChannelMonitorSmartScheduleRateLimitCooldownSeconds, settings.SmartScheduleRateLimitCooldownSeconds)
	require.Len(t, settings.SmartScheduleGroupPolicies, 1)
	assert.Equal(t, "vip", settings.SmartScheduleGroupPolicies[0].Group)
	assert.Equal(t, 5, *settings.SmartScheduleGroupPolicies[0].StabilityWindowMinutes)

	handler := channelSmartScheduleTaskHandler{}
	assert.Equal(t, channelMonitorSmartScheduleTaskType, handler.Type())
	_, scheduled := any(handler).(service.ScheduledSystemTaskHandler)
	assert.False(t, scheduled)
}

func TestChannelSmartScheduleSettingsRetentionCoversLargestPolicyStabilityWindow(t *testing.T) {
	shortWindow := 5
	longWindow := 90
	shortPolicy := channelSmartScheduleTestGroupPolicy(
		"standard", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	shortPolicy.StabilityWindowMinutes = &shortWindow
	longPolicy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	longPolicy.StabilityWindowMinutes = &longWindow
	serializedPolicies, err := common.Marshal([]channelSmartScheduleGroupPolicy{
		shortPolicy, longPolicy,
	})
	require.NoError(t, err)

	settings := channelMonitorSettingsFromOptions(map[string]string{
		channelMonitorSmartScheduleEnabledOption:             "true",
		channelMonitorSmartScheduleGroupPoliciesOption:       string(serializedPolicies),
		channelMonitorSmartSchedulePerformanceWindowOption:   "60",
		channelMonitorSmartScheduleRealtimeRetentionOption:   "60",
		channelMonitorSmartScheduleRealtimeSampleLimitOption: "20000",
		channelMonitorSmartScheduleRateLimitCooldownOption:   "30",
	})

	assert.True(t, settings.SmartScheduleEnabled)
	assert.Equal(t, 90, settings.SmartScheduleRealtimeRetentionMinutes)
	require.Len(t, settings.SmartScheduleGroupPolicies, 2)
	assert.Equal(t, 5, *settings.SmartScheduleGroupPolicies[0].StabilityWindowMinutes)
	assert.Equal(t, 90, *settings.SmartScheduleGroupPolicies[1].StabilityWindowMinutes)
}

func TestChannelSmartScheduleSettingsRejectIncompleteStoredPolicies(t *testing.T) {
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:       "true",
		channelMonitorSmartScheduleGroupPoliciesOption: `[{"group":"vip","strategy":"ratio"}]`,
	})

	settings := getChannelMonitorSettings()
	assert.False(t, settings.SmartScheduleEnabled)
	assert.Empty(t, settings.SmartScheduleGroupPolicies)
}

func TestChannelSmartScheduleSettingsIgnoreRemovedPrimaryMinimum(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	serializedPolicy, err := common.Marshal(policy)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, common.Unmarshal(serializedPolicy, &fields))
	fields["adaptive_sampling_primary_min_percent"] = 70
	serializedLegacyPolicy, err := common.Marshal(fields)
	require.NoError(t, err)

	var decoded channelSmartScheduleGroupPolicy
	require.NoError(t, common.Unmarshal(serializedLegacyPolicy, &decoded))
	normalized, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{decoded})
	require.NoError(t, err)
	require.Len(t, normalized, 1)
	serializedNormalized, err := common.Marshal(normalized)
	require.NoError(t, err)
	assert.NotContains(t, string(serializedNormalized), "adaptive_sampling_primary_min_percent")
}

func TestSavingFirstGroupPolicyDoesNotImplicitlyEnableScheduling(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
	})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_control_revision": "",
		"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
			),
		},
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	for _, removedKey := range []string{
		"smart_schedule_groups",
		"smart_schedule_strategy",
		"smart_schedule_stability_enabled",
		"smart_schedule_scoring",
		"smart_schedule_apply_mode",
		"smart_schedule_model",
		"smart_schedule_models",
		"smart_schedule_min_samples",
		"smart_schedule_min_success_rate",
		"smart_schedule_cooldown_minutes",
	} {
		assert.NotContains(t, recorder.Body.String(), `"`+removedKey+`"`)
	}

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Data.SmartScheduleEnabled)
	var enabledOption model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleEnabledOption).First(&enabledOption).Error)
	assert.Equal(t, "false", enabledOption.Value)
}

func TestChannelSmartScheduleConfigurationDefaultsNewFields(t *testing.T) {
	var scoring channelSmartScheduleScoring
	require.NoError(t, common.UnmarshalJsonStr(`{
		"stability_percent":50,
		"smart":{"cost_ratio_percent":40,"first_token_percent":40,"tps_percent":20},
		"ratio":{"cost_ratio_percent":70,"first_token_percent":20,"tps_percent":10}
	}`, &scoring))
	assert.Equal(t, 90.0, scoring.PrimaryTrafficPercent)
	assert.Equal(t, 10.0, scoring.PrimarySwitchThresholdPercent)
	require.NoError(t, validateChannelSmartScheduleScoring(scoring))
	for _, value := range []float64{51, 99} {
		candidate := scoring
		candidate.PrimaryTrafficPercent = value
		assert.NoError(t, validateChannelSmartScheduleScoring(candidate))
	}
	for _, value := range []float64{50, 100} {
		candidate := scoring
		candidate.PrimaryTrafficPercent = value
		assert.ErrorContains(t, validateChannelSmartScheduleScoring(candidate), "51% 到 99%")
	}

	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	policy.BurstFailureWindowMinutes = nil
	policy.BurstFailureWindowRequests = nil
	policy.BurstFailureWindowSeconds = nil
	policy.BurstFailureThresholdPercent = nil
	policy.ConsecutiveFailureThreshold = nil
	policy.BurstFailureThreshold = nil
	policy.RecoverySuccessThreshold = nil
	policy.AdaptiveSamplingWindowMinutes = nil
	policy.AdaptiveSamplingWindowRequests = nil
	policy.AdaptiveSamplingWindowSeconds = nil
	normalized, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.NoError(t, err)
	require.Len(t, normalized, 1)
	require.NotNil(t, normalized[0].BurstFailureWindowMinutes)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureWindowMinutes, *normalized[0].BurstFailureWindowMinutes)
	require.NotNil(t, normalized[0].BurstFailureWindowRequests)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureWindowRequests, *normalized[0].BurstFailureWindowRequests)
	require.NotNil(t, normalized[0].BurstFailureThresholdPercent)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureThresholdPercent, *normalized[0].BurstFailureThresholdPercent)
	require.NotNil(t, normalized[0].BurstFailureWindowSeconds)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureWindowSeconds, *normalized[0].BurstFailureWindowSeconds)
	require.NotNil(t, normalized[0].ConsecutiveFailureThreshold)
	assert.Equal(t, defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold, *normalized[0].ConsecutiveFailureThreshold)
	require.NotNil(t, normalized[0].BurstFailureThreshold)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureThreshold, *normalized[0].BurstFailureThreshold)
	require.NotNil(t, normalized[0].AdaptiveSamplingWindowMinutes)
	assert.Equal(t, defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowMinutes, *normalized[0].AdaptiveSamplingWindowMinutes)
	require.NotNil(t, normalized[0].AdaptiveSamplingWindowRequests)
	assert.Equal(t, defaultChannelMonitorSmartScheduleAdaptiveSamplingWindowRequests, *normalized[0].AdaptiveSamplingWindowRequests)
	require.NotNil(t, normalized[0].RecoverySuccessThreshold)
	assert.Equal(t, defaultChannelMonitorSmartScheduleRecoverySuccessThreshold, *normalized[0].RecoverySuccessThreshold)
}

func TestNormalizeChannelSmartScheduleGroupPolicyRequiresCurrentJitterThreshold(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	policy.JitterSlowThresholdSeconds = nil

	_, err := normalizeChannelSmartScheduleGroupPolicies([]channelSmartScheduleGroupPolicy{policy})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "必须完整配置")
}

func TestUpdateChannelMonitorSettingsRejectsDeletedSmartSchedulePolicyFields(t *testing.T) {
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, true,
		channelMonitorSmartScheduleApplyPriorityWeight, nil, 5, 80, 30,
	)
	serializedPolicy, err := common.Marshal(policy)
	require.NoError(t, err)
	deletedFields := []string{
		"priority_sampling_enabled",
		"priority_sampling_interval_minutes",
		"priority_sampling_base_percent",
		"priority_sampling_decay_percent",
		"priority_sampling_min_percent",
		"adaptive_sampling_enter_rounds",
		"adaptive_sampling_recover_rounds",
		"adaptive_sampling_switch_confirm_rounds",
		"adaptive_sampling_exploration_lease_minutes",
		"adaptive_sampling_enter_request_percent",
	}

	for _, deletedField := range deletedFields {
		t.Run(deletedField, func(t *testing.T) {
			var policyFields map[string]any
			require.NoError(t, common.Unmarshal(serializedPolicy, &policyFields))
			policyFields[deletedField] = 1

			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
				"smart_schedule_group_policies": []map[string]any{policyFields},
			})
			UpdateChannelMonitorSettings(ctx)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "无效的参数")
			serializedLegacyPolicy, marshalErr := common.Marshal([]map[string]any{policyFields})
			require.NoError(t, marshalErr)
			assert.Empty(t, parseChannelSmartScheduleGroupPolicies(string(serializedLegacyPolicy)))
		})
	}
}

func TestUpdateChannelMonitorSettingsValidatesAndPersists(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	originalRelayTimeout := common.GetRelayResponseHeaderTimeoutSeconds()
	t.Cleanup(func() {
		common.SetRelayResponseHeaderTimeoutSeconds(originalRelayTimeout)
	})

	invalidStrategy := channelSmartScheduleTestGroupPolicy(
		"vip", "invalid", false, channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidApplyMode := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false, "invalid", []string{}, 5, 80, 30,
	)
	invalidSmartScoring := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategySmart, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidSmartScoring.Scoring.Smart.TPSPercent = 40
	invalidStabilityScoring := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidStabilityScoring.Scoring.StabilityPercent = 101
	invalidPrimaryTraffic := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidPrimaryTraffic.Scoring.PrimaryTrafficPercent = 50
	invalidRatioScoring := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidRatioScoring.Scoring.Ratio = channelSmartScheduleMetricPercentages{
		FirstTokenPercent: 50,
		TPSPercent:        50,
	}
	invalidPrimarySwitchThreshold := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidPrimarySwitchThreshold.Scoring.PrimarySwitchThresholdPercent = 101
	tooManyModels := make([]string, maxChannelMonitorSmartScheduleModelCount+1)
	invalidModels := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, tooManyModels, 5, 80, 30,
	)
	invalidModelOrder := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidModelOrder.ModelOrder = tooManyModels
	invalidMinSamples := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 0, 80, 30,
	)
	invalidSuccessRate := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 101, 30,
	)
	invalidJitterTolerance := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidJitterToleranceValue := 51.0
	invalidJitterTolerance.JitterTolerancePercent = &invalidJitterToleranceValue
	invalidJitterSlowThreshold := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidJitterSlowThresholdValue := 60.001
	invalidJitterSlowThreshold.JitterSlowThresholdSeconds = &invalidJitterSlowThresholdValue
	invalidBurstFailureWindow := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidBurstFailureWindowValue := 0
	invalidBurstFailureWindow.BurstFailureWindowSeconds = &invalidBurstFailureWindowValue
	invalidBurstFailureWindow.BurstFailureWindowMinutes = &invalidBurstFailureWindowValue
	invalidBurstFailureWindow.BurstFailureWindowRequests = &invalidBurstFailureWindowValue
	invalidConsecutiveFailureThreshold := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidConsecutiveFailureThresholdValue := 0
	invalidConsecutiveFailureThreshold.ConsecutiveFailureThreshold = &invalidConsecutiveFailureThresholdValue
	invalidBurstFailureThreshold := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidBurstFailureThresholdValue := maxChannelMonitorSmartScheduleRuntimeFailureThreshold + 1
	invalidBurstFailureThresholdPercentValue := 101.0
	invalidBurstFailureThreshold.BurstFailureThreshold = &invalidBurstFailureThresholdValue
	invalidBurstFailureThreshold.BurstFailureThresholdPercent = &invalidBurstFailureThresholdPercentValue
	invalidRecoverySuccessThreshold := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, true,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	invalidRecoverySuccessThresholdValue := 0
	invalidRecoverySuccessThreshold.RecoverySuccessThreshold = &invalidRecoverySuccessThresholdValue
	invalidCooldown := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 0,
	)
	invalidExplorationTraffic := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyPriorityWeight, []string{}, 5, 80, 30,
	)
	invalidExplorationPercent := 21.0
	invalidExplorationTraffic.ExplorationTrafficPercent = &invalidExplorationPercent
	invalidExplorationPromptLimit := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	nonKExplorationPromptLimit := 16_384
	invalidExplorationPromptLimit.ExplorationMaxPromptTokens = &nonKExplorationPromptLimit
	invalidStabilityReleasePromptLimit := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	nonKStabilityReleasePromptLimit := 1_500
	invalidStabilityReleasePromptLimit.StabilityReleaseMaxPromptTokens = &nonKStabilityReleasePromptLimit
	invalidSampleMode := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	unsupportedSampleMode := "invalid"
	invalidSampleMode.SampleMode = &unsupportedSampleMode
	invalidSamplingOrder := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	unsupportedSamplingOrder := "invalid"
	invalidSamplingOrder.SamplingOrder = &unsupportedSamplingOrder
	invalidFirstTokenWarningRequestPercent := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	zeroFirstTokenWarningRequestPercent := 0.0
	invalidFirstTokenWarningRequestPercent.AdaptiveSamplingFirstTokenWarningRequestPercent =
		&zeroFirstTokenWarningRequestPercent
	missingSamplingOrder := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	missingSamplingOrder.SamplingOrder = nil
	invalidProbeInterval := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	zeroProbeInterval := 0
	invalidProbeInterval.ProbeIntervalMinutes = &zeroProbeInterval
	invalidTrafficApplyMode := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	trafficSampleMode := channelMonitorSmartScheduleSampleTraffic
	invalidTrafficApplyMode.SampleMode = &trafficSampleMode
	tooManyPolicies := make([]channelSmartScheduleGroupPolicy, maxChannelMonitorSmartScheduleGroupCount+1)
	for index := range tooManyPolicies {
		tooManyPolicies[index] = channelSmartScheduleTestGroupPolicy(
			fmt.Sprintf("group-%d", index), channelMonitorSmartScheduleStrategyRatio, false,
			channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
		)
	}
	invalidRequests := []map[string]any{
		{},
		{"smart_schedule_groups": []string{"vip"}},
		{"smart_schedule_strategy": channelMonitorSmartScheduleStrategyRatio},
		{"auto_update_interval_minutes": -1},
		{"auto_update_interval_minutes": maxChannelMonitorAutoUpdateIntervalMinutes + 1},
		{"auto_update_retry_count": -1},
		{"auto_update_retry_count": maxChannelMonitorAutoUpdateRetryCount + 1},
		{"auto_update_retry_delay_seconds": -1},
		{"auto_update_retry_delay_seconds": maxChannelMonitorAutoUpdateRetryDelaySeconds + 1},
		{"channel_concurrency_wait_seconds": minChannelMonitorChannelConcurrencyWaitSeconds - 1},
		{"channel_concurrency_wait_seconds": maxChannelMonitorChannelConcurrencyWaitSeconds + 1},
		{"upstream_request_timeout_seconds": minChannelMonitorUpstreamRequestTimeoutSeconds - 1},
		{"upstream_request_timeout_seconds": maxChannelMonitorUpstreamRequestTimeoutSeconds + 1},
		{"auto_update_consecutive_failure_limit": 0},
		{"auto_update_consecutive_failure_limit": maxChannelMonitorAutoUpdateConsecutiveFailureLimit + 1},
		{"cost_retention_days": minChannelMonitorCostRetentionDays - 1},
		{"cost_retention_days": maxChannelMonitorCostRetentionDays + 1},
		{"email_notification_enabled": true},
		{"email_notification_enabled": true, "notification_email": "alerts@example.com", "email_notification_types": []string{}},
		{"email_notification_types": []string{"unknown"}},
		{"notification_email": "invalid"},
		{"notification_email": strings.Repeat("a", maxChannelMonitorNotificationEmailLength) + "@example.com"},
		{"relay_response_header_timeout_seconds": -1},
		{"relay_response_header_timeout_seconds": common.MaxRelayResponseHeaderTimeoutSeconds + 1},
		{"smart_schedule_enabled": true},
		{"smart_schedule_group_policies": []map[string]any{{"group": ""}}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{channelSmartScheduleTestGroupPolicy(
			strings.Repeat("g", maxChannelMonitorSmartScheduleGroupLength+1), channelMonitorSmartScheduleStrategyRatio,
			false, channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
		)}},
		{"smart_schedule_group_policies": tooManyPolicies},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidStrategy}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidApplyMode}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidSmartScoring}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidStabilityScoring}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidPrimaryTraffic}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidRatioScoring}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidPrimarySwitchThreshold}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidModels}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidModelOrder}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidMinSamples}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidSuccessRate}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidJitterTolerance}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidJitterSlowThreshold}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidBurstFailureWindow}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidConsecutiveFailureThreshold}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidBurstFailureThreshold}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidRecoverySuccessThreshold}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidCooldown}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidExplorationTraffic}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidExplorationPromptLimit}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidStabilityReleasePromptLimit}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidSampleMode}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidSamplingOrder}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidFirstTokenWarningRequestPercent}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{missingSamplingOrder}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidProbeInterval}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{invalidTrafficApplyMode}},
		{"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{
			channelSmartScheduleTestGroupPolicy("vip", channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30),
			channelSmartScheduleTestGroupPolicy(" vip ", channelMonitorSmartScheduleStrategyRatio, false, channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30),
		}},
		{"smart_schedule_performance_window_minutes": 0},
		{"smart_schedule_performance_window_minutes": maxChannelMonitorSmartScheduleWindowMinutes + 1},
		{"smart_schedule_realtime_retention_minutes": minChannelMonitorSmartScheduleRealtimeRetentionMinutes - 1},
		{"smart_schedule_realtime_retention_minutes": maxChannelMonitorSmartScheduleRealtimeRetentionMinutes + 1},
		{"smart_schedule_realtime_sample_limit": minChannelMonitorSmartScheduleRealtimeSampleLimit - 1},
		{"smart_schedule_realtime_sample_limit": maxChannelMonitorSmartScheduleRealtimeSampleLimit + 1},
		{"smart_schedule_rate_limit_cooldown_seconds": -1},
		{"smart_schedule_rate_limit_cooldown_seconds": maxChannelMonitorSmartScheduleRateLimitCooldownSeconds + 1},
	}
	for _, request := range invalidRequests {
		ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", request)
		UpdateChannelMonitorSettings(ctx)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	}

	validScoring := channelSmartScheduleScoring{
		StabilityPercent:              60,
		PrimaryTrafficPercent:         85,
		PrimarySwitchThresholdPercent: 4,
		Smart: channelSmartScheduleMetricPercentages{
			CostRatioPercent: 50, FirstTokenPercent: 25, TPSPercent: 25,
		},
		Ratio: channelSmartScheduleMetricPercentages{
			CostRatioPercent: 80, FirstTokenPercent: 10, TPSPercent: 10,
		},
	}
	request := map[string]any{
		"auto_update_interval_minutes":          15,
		"auto_update_retry_count":               3,
		"auto_update_retry_delay_seconds":       12,
		"channel_concurrency_wait_seconds":      17,
		"upstream_request_timeout_seconds":      45,
		"auto_update_consecutive_failure_limit": 5,
		"auto_disable_on_update_failure":        true,
		"cost_retention_days":                   365,
		"email_notification_enabled":            true,
		"notification_email":                    "alerts@example.com",
		"email_notification_types":              []string{channelMonitorEmailTypeBalanceWarning, channelMonitorEmailTypeTaskFailed},
		"probe_response_enabled":                true,
		"smart_schedule_enabled":                true,
		"smart_schedule_group_policies": []map[string]any{
			{
				"group": " vip ", "strategy": channelMonitorSmartScheduleStrategyRatio,
				"stability_enabled": false, "stability_window_minutes": 15, "scoring": validScoring,
				"apply_mode":  channelMonitorSmartScheduleApplyWeight,
				"models":      []string{" gpt-4o-mini ", "gpt-4o-mini"},
				"min_samples": 8, "recovery_stability_score": 95,
				"fast_failure_penalty_percent": 40, "fast_failure_seconds": 1, "slow_failure_seconds": 10,
				"jitter_enabled": true, "jitter_tolerance_percent": 5,
				"jitter_slow_threshold_seconds": 12,
				"cooldown_minutes":              45,
				"sample_mode":                   channelMonitorSmartScheduleSampleOff,
				"sampling_order":                " ratio ",
				"exploration_traffic_percent":   3, "probe_interval_minutes": 10,
				"adaptive_sampling_enabled": false, "adaptive_sampling_base_percent": 3,
				"adaptive_sampling_max_percent":           30,
				"adaptive_sampling_error_warning_percent": 5, "adaptive_sampling_error_critical_percent": 15,
				"adaptive_sampling_first_token_warning_seconds": 5, "adaptive_sampling_first_token_critical_seconds": 10,
				"adaptive_sampling_window_seconds": 600, "adaptive_sampling_first_token_warning_request_percent": 10,
				"adaptive_sampling_recover_request_percent":        95,
				"adaptive_sampling_switch_confirm_request_percent": 95,
				"adaptive_sampling_min_comparable_channels":        2,
			},
			{
				"group": "default", "strategy": channelMonitorSmartScheduleStrategySmart,
				"stability_enabled": true, "stability_window_minutes": 120, "scoring": validScoring,
				"apply_mode":  channelMonitorSmartScheduleApplyPriorityWeight,
				"models":      []string{"claude-3-5-sonnet", "gpt-4o-mini"},
				"model_order": []string{" gpt-4o-mini ", "claude-3-5-sonnet", "gpt-4o-mini"},
				"min_samples": 8, "recovery_stability_score": 85,
				"fast_failure_penalty_percent": 40, "fast_failure_seconds": 1, "slow_failure_seconds": 10,
				"fast_failure_same_channel_retry_count":    2,
				"fast_failure_same_channel_retry_delay_ms": 750,
				"burst_failure_window_seconds":             45, "consecutive_failure_threshold": 4,
				"burst_failure_threshold": 6, "recovery_success_threshold": 3,
				"jitter_enabled": true, "jitter_tolerance_percent": 5,
				"jitter_slow_threshold_seconds": 10,
				"cooldown_minutes":              45,
				"sample_mode":                   channelMonitorSmartScheduleSampleTraffic,
				"sampling_order":                channelMonitorSmartScheduleSamplingOrderPriorityWeight,
				"exploration_traffic_percent":   5, "probe_interval_minutes": 5,
				"adaptive_sampling_enabled": true, "adaptive_sampling_base_percent": 3,
				"adaptive_sampling_max_percent":           30,
				"adaptive_sampling_error_warning_percent": 5, "adaptive_sampling_error_critical_percent": 15,
				"adaptive_sampling_first_token_warning_seconds": 5, "adaptive_sampling_first_token_critical_seconds": 10,
				"adaptive_sampling_window_seconds": 600, "adaptive_sampling_first_token_warning_request_percent": 10,
				"adaptive_sampling_recover_request_percent":        95,
				"adaptive_sampling_switch_confirm_request_percent": 95,
				"adaptive_sampling_min_comparable_channels":        2,
			},
		},
		"smart_schedule_performance_window_minutes":  360,
		"smart_schedule_realtime_retention_minutes":  720,
		"smart_schedule_realtime_sample_limit":       50000,
		"smart_schedule_rate_limit_cooldown_seconds": 300,
		"smart_schedule_control_revision":            "",
	}
	request["relay_response_header_timeout_seconds"] = 60
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", request)
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotEmpty(t, response.Data.SmartScheduleControlRevision)
	assert.Equal(t, 15, response.Data.AutoUpdateIntervalMinutes)
	assert.Equal(t, 3, response.Data.AutoUpdateRetryCount)
	assert.Equal(t, 12, response.Data.AutoUpdateRetryDelaySeconds)
	assert.Equal(t, 17, response.Data.ChannelConcurrencyWaitSeconds)
	assert.Equal(t, 45, response.Data.UpstreamRequestTimeoutSeconds)
	assert.Equal(t, 5, response.Data.AutoUpdateConsecutiveFailureLimit)
	assert.True(t, response.Data.AutoDisableOnUpdateFailure)
	assert.Equal(t, 365, response.Data.CostRetentionDays)
	assert.True(t, response.Data.EmailNotificationEnabled)
	assert.Equal(t, "alerts@example.com", response.Data.NotificationEmail)
	assert.Equal(t, []string{channelMonitorEmailTypeBalanceWarning, channelMonitorEmailTypeTaskFailed}, response.Data.EmailNotificationTypes)
	assert.True(t, response.Data.ProbeResponseEnabled)
	assert.Equal(t, 60, response.Data.RelayHeaderTimeoutSeconds)
	assert.Equal(t, 60, common.GetRelayResponseHeaderTimeoutSeconds())
	assert.True(t, response.Data.SmartScheduleEnabled)
	require.Len(t, response.Data.SmartScheduleGroupPolicies, 2)
	defaultGroupPolicy := response.Data.SmartScheduleGroupPolicies[0]
	assert.Equal(t, "default", defaultGroupPolicy.Group)
	require.NotNil(t, defaultGroupPolicy.Strategy)
	assert.Equal(t, channelMonitorSmartScheduleStrategySmart, *defaultGroupPolicy.Strategy)
	require.NotNil(t, defaultGroupPolicy.SampleMode)
	assert.Equal(t, channelMonitorSmartScheduleSampleTraffic, *defaultGroupPolicy.SampleMode)
	require.NotNil(t, defaultGroupPolicy.SamplingOrder)
	assert.Equal(t, channelMonitorSmartScheduleSamplingOrderPriorityWeight, *defaultGroupPolicy.SamplingOrder)
	require.NotNil(t, defaultGroupPolicy.ExplorationTrafficPercent)
	assert.Equal(t, 5.0, *defaultGroupPolicy.ExplorationTrafficPercent)
	require.NotNil(t, defaultGroupPolicy.ProbeIntervalMinutes)
	assert.Equal(t, 5, *defaultGroupPolicy.ProbeIntervalMinutes)
	require.NotNil(t, defaultGroupPolicy.StabilityEnabled)
	assert.True(t, *defaultGroupPolicy.StabilityEnabled)
	require.NotNil(t, defaultGroupPolicy.StabilityWindowMinutes)
	assert.Equal(t, 120, *defaultGroupPolicy.StabilityWindowMinutes)
	require.NotNil(t, defaultGroupPolicy.Scoring)
	assert.Equal(t, validScoring, *defaultGroupPolicy.Scoring)
	require.NotNil(t, defaultGroupPolicy.ApplyMode)
	assert.Equal(t, channelMonitorSmartScheduleApplyPriorityWeight, *defaultGroupPolicy.ApplyMode)
	require.NotNil(t, defaultGroupPolicy.Models)
	assert.Equal(t, []string{"claude-3-5-sonnet", "gpt-4o-mini"}, *defaultGroupPolicy.Models)
	assert.Equal(t, []string{"gpt-4o-mini", "claude-3-5-sonnet"}, defaultGroupPolicy.ModelOrder)
	require.NotNil(t, defaultGroupPolicy.MinSamples)
	assert.Equal(t, 8, *defaultGroupPolicy.MinSamples)
	require.NotNil(t, defaultGroupPolicy.RecoveryStabilityScore)
	assert.Equal(t, 85.0, *defaultGroupPolicy.RecoveryStabilityScore)
	require.NotNil(t, defaultGroupPolicy.FastFailurePenaltyPercent)
	assert.Equal(t, 40.0, *defaultGroupPolicy.FastFailurePenaltyPercent)
	require.NotNil(t, defaultGroupPolicy.FastFailureSeconds)
	assert.Equal(t, 1.0, *defaultGroupPolicy.FastFailureSeconds)
	require.NotNil(t, defaultGroupPolicy.FastFailureSameChannelRetryCount)
	assert.Equal(t, 2, *defaultGroupPolicy.FastFailureSameChannelRetryCount)
	require.NotNil(t, defaultGroupPolicy.FastFailureRetryDelayMs)
	assert.Equal(t, 750, *defaultGroupPolicy.FastFailureRetryDelayMs)
	require.NotNil(t, defaultGroupPolicy.SlowFailureSeconds)
	assert.Equal(t, 10.0, *defaultGroupPolicy.SlowFailureSeconds)
	require.NotNil(t, defaultGroupPolicy.BurstFailureWindowSeconds)
	assert.Equal(t, 45, *defaultGroupPolicy.BurstFailureWindowSeconds)
	require.NotNil(t, defaultGroupPolicy.ConsecutiveFailureThreshold)
	assert.Equal(t, 4, *defaultGroupPolicy.ConsecutiveFailureThreshold)
	require.NotNil(t, defaultGroupPolicy.BurstFailureThreshold)
	assert.Equal(t, 6, *defaultGroupPolicy.BurstFailureThreshold)
	require.NotNil(t, defaultGroupPolicy.RecoverySuccessThreshold)
	assert.Equal(t, 3, *defaultGroupPolicy.RecoverySuccessThreshold)
	require.NotNil(t, defaultGroupPolicy.JitterEnabled)
	assert.True(t, *defaultGroupPolicy.JitterEnabled)
	require.NotNil(t, defaultGroupPolicy.JitterTolerancePercent)
	assert.Equal(t, 5.0, *defaultGroupPolicy.JitterTolerancePercent)
	require.NotNil(t, defaultGroupPolicy.JitterSlowThresholdSeconds)
	assert.Equal(t, 10.0, *defaultGroupPolicy.JitterSlowThresholdSeconds)
	require.NotNil(t, defaultGroupPolicy.CooldownMinutes)
	assert.Equal(t, 45, *defaultGroupPolicy.CooldownMinutes)
	require.NotNil(t, defaultGroupPolicy.ExplorationMaxPromptTokens)
	assert.Equal(t, 50_000, *defaultGroupPolicy.ExplorationMaxPromptTokens)
	require.NotNil(t, defaultGroupPolicy.StabilityReleaseMaxPromptTokens)
	assert.Equal(t, 50_000, *defaultGroupPolicy.StabilityReleaseMaxPromptTokens)
	require.NotNil(t, defaultGroupPolicy.AdaptiveSamplingFirstTokenWarningRequestPercent)
	assert.Equal(t, 10.0, *defaultGroupPolicy.AdaptiveSamplingFirstTokenWarningRequestPercent)

	groupPolicy := response.Data.SmartScheduleGroupPolicies[1]
	assert.Equal(t, "vip", groupPolicy.Group)
	require.NotNil(t, groupPolicy.Strategy)
	assert.Equal(t, channelMonitorSmartScheduleStrategyRatio, *groupPolicy.Strategy)
	require.NotNil(t, groupPolicy.SamplingOrder)
	assert.Equal(t, channelMonitorSmartScheduleSamplingOrderRatio, *groupPolicy.SamplingOrder)
	require.NotNil(t, groupPolicy.StabilityEnabled)
	assert.False(t, *groupPolicy.StabilityEnabled)
	require.NotNil(t, groupPolicy.StabilityWindowMinutes)
	assert.Equal(t, 15, *groupPolicy.StabilityWindowMinutes)
	require.NotNil(t, groupPolicy.Scoring)
	assert.Equal(t, validScoring, *groupPolicy.Scoring)
	require.NotNil(t, groupPolicy.ApplyMode)
	assert.Equal(t, channelMonitorSmartScheduleApplyWeight, *groupPolicy.ApplyMode)
	require.NotNil(t, groupPolicy.Models)
	assert.Equal(t, []string{"gpt-4o-mini"}, *groupPolicy.Models)
	require.NotNil(t, groupPolicy.MinSamples)
	assert.Equal(t, 8, *groupPolicy.MinSamples)
	require.NotNil(t, groupPolicy.RecoveryStabilityScore)
	assert.Equal(t, 95.0, *groupPolicy.RecoveryStabilityScore)
	require.NotNil(t, groupPolicy.FastFailureSameChannelRetryCount)
	assert.Equal(t, defaultChannelMonitorSmartScheduleFastFailureSameChannelRetryCount, *groupPolicy.FastFailureSameChannelRetryCount)
	require.NotNil(t, groupPolicy.FastFailureRetryDelayMs)
	assert.Equal(t, defaultChannelMonitorSmartScheduleFastRetryDelayMs, *groupPolicy.FastFailureRetryDelayMs)
	require.NotNil(t, groupPolicy.BurstFailureWindowMinutes)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureWindowMinutes, *groupPolicy.BurstFailureWindowMinutes)
	require.NotNil(t, groupPolicy.BurstFailureWindowRequests)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureWindowRequests, *groupPolicy.BurstFailureWindowRequests)
	require.NotNil(t, groupPolicy.BurstFailureThresholdPercent)
	assert.Equal(t, defaultChannelMonitorSmartScheduleBurstFailureThresholdPercent, *groupPolicy.BurstFailureThresholdPercent)
	assert.Nil(t, groupPolicy.BurstFailureWindowSeconds)
	require.NotNil(t, groupPolicy.ConsecutiveFailureThreshold)
	assert.Equal(t, defaultChannelMonitorSmartScheduleConsecutiveFailureThreshold, *groupPolicy.ConsecutiveFailureThreshold)
	assert.Nil(t, groupPolicy.BurstFailureThreshold)
	require.NotNil(t, groupPolicy.RecoverySuccessThreshold)
	assert.Equal(t, defaultChannelMonitorSmartScheduleRecoverySuccessThreshold, *groupPolicy.RecoverySuccessThreshold)
	require.NotNil(t, groupPolicy.JitterSlowThresholdSeconds)
	assert.Equal(t, 12.0, *groupPolicy.JitterSlowThresholdSeconds)
	require.NotNil(t, groupPolicy.CooldownMinutes)
	assert.Equal(t, 45, *groupPolicy.CooldownMinutes)
	require.NotNil(t, groupPolicy.ExplorationMaxPromptTokens)
	assert.Equal(t, 50_000, *groupPolicy.ExplorationMaxPromptTokens)
	require.NotNil(t, groupPolicy.StabilityReleaseMaxPromptTokens)
	assert.Equal(t, 50_000, *groupPolicy.StabilityReleaseMaxPromptTokens)
	require.NotNil(t, groupPolicy.AdaptiveSamplingFirstTokenWarningRequestPercent)
	assert.Equal(t, 10.0, *groupPolicy.AdaptiveSamplingFirstTokenWarningRequestPercent)
	require.NotNil(t, groupPolicy.AdaptiveSamplingWindowSeconds)
	assert.Equal(t, 600, *groupPolicy.AdaptiveSamplingWindowSeconds)
	assert.Equal(t, 360, response.Data.SmartSchedulePerformanceWindowMinutes)
	assert.Equal(t, 720, response.Data.SmartScheduleRealtimeRetentionMinutes)
	assert.Equal(t, 50000, response.Data.SmartScheduleRealtimeSampleLimit)
	assert.Equal(t, 300, response.Data.SmartScheduleRateLimitCooldownSeconds)

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorAutoUpdateIntervalOption).First(&option).Error)
	assert.Equal(t, "15", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorAutoUpdateRetryCountOption).First(&option).Error)
	assert.Equal(t, "3", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorAutoUpdateRetryDelaySecondsOption).First(&option).Error)
	assert.Equal(t, "12", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorUpstreamRequestTimeoutOption).First(&option).Error)
	assert.Equal(t, "45", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorAutoUpdateConsecutiveFailureLimitOption).First(&option).Error)
	assert.Equal(t, "5", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorAutoDisableOnUpdateFailureOption).First(&option).Error)
	assert.Equal(t, "true", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorCostRetentionDaysOption).First(&option).Error)
	assert.Equal(t, "365", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorEmailNotificationOption).First(&option).Error)
	assert.Equal(t, "true", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorNotificationEmailOption).First(&option).Error)
	assert.Equal(t, "alerts@example.com", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorProbeResponseOption).First(&option).Error)
	assert.Equal(t, "true", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", common.RelayResponseHeaderTimeoutOptionKey).First(&option).Error)
	assert.Equal(t, "60", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleEnabledOption).First(&option).Error)
	assert.Equal(t, "true", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleGroupPoliciesOption).First(&option).Error)
	var storedGroupPolicies smartScheduleGroupPolicies
	require.NoError(t, common.UnmarshalJsonStr(option.Value, &storedGroupPolicies))
	assert.Equal(t, response.Data.SmartScheduleGroupPolicies, storedGroupPolicies)
	assert.Contains(t, option.Value, `"jitter_slow_threshold_seconds":10`)
	assert.Contains(t, option.Value, `"jitter_slow_threshold_seconds":12`)
	assert.Contains(t, option.Value, `"exploration_max_prompt_tokens":50000`)
	assert.Contains(t, option.Value, `"stability_release_max_prompt_tokens":0`)
	assert.Contains(t, option.Value, `"adaptive_sampling_first_token_warning_request_percent":10`)
	assert.NotContains(t, option.Value, "jitter_absolute_tolerance_seconds")
	assert.NotContains(t, option.Value, "jitter_baseline_minutes")
	assert.NotContains(t, option.Value, "degrade_stability_score")
	for _, deletedField := range []string{
		"priority_sampling_enabled",
		"priority_sampling_interval_minutes",
		"priority_sampling_base_percent",
		"priority_sampling_decay_percent",
		"priority_sampling_min_percent",
		"adaptive_sampling_enter_rounds",
		"adaptive_sampling_recover_rounds",
		"adaptive_sampling_switch_confirm_rounds",
		"adaptive_sampling_exploration_lease_minutes",
		"adaptive_sampling_enter_request_percent",
	} {
		assert.NotContains(t, recorder.Body.String(), deletedField)
		assert.NotContains(t, option.Value, deletedField)
	}
	assert.NotContains(t, recorder.Body.String(), "smart_schedule_interval_minutes")
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartSchedulePerformanceWindowOption).First(&option).Error)
	assert.Equal(t, "360", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleRealtimeRetentionOption).First(&option).Error)
	assert.Equal(t, "720", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleRealtimeSampleLimitOption).First(&option).Error)
	assert.Equal(t, "50000", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleRateLimitCooldownOption).First(&option).Error)
	assert.Equal(t, "300", option.Value)
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"email_notification_enabled": false,
		"notification_email":         "",
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, 15, response.Data.AutoUpdateIntervalMinutes)
	assert.Equal(t, 3, response.Data.AutoUpdateRetryCount)
	assert.Equal(t, 12, response.Data.AutoUpdateRetryDelaySeconds)
	assert.Equal(t, 45, response.Data.UpstreamRequestTimeoutSeconds)
	assert.Equal(t, 5, response.Data.AutoUpdateConsecutiveFailureLimit)
	assert.True(t, response.Data.AutoDisableOnUpdateFailure)
	assert.False(t, response.Data.EmailNotificationEnabled)
	assert.Empty(t, response.Data.NotificationEmail)
	assert.True(t, response.Data.SmartScheduleEnabled)
	require.Len(t, response.Data.SmartScheduleGroupPolicies, 2)
	assert.Equal(t, 300, response.Data.SmartScheduleRateLimitCooldownSeconds)
}

func TestUpdateChannelMonitorSettingsAllowsDisabling429CooldownAndClearsActiveEntries(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	service.StartChannelRateLimitCooldown(1901, "model-a", 30)
	assert.NotZero(t, service.ChannelRateLimitCooldownUntil(1901, "model-a"))

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_rate_limit_cooldown_seconds": 0,
		"smart_schedule_control_revision":            "",
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1901, "model-a"))

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Zero(t, response.Data.SmartScheduleRateLimitCooldownSeconds)
	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleRateLimitCooldownOption).First(&option).Error)
	assert.Equal(t, "0", option.Value)
}

func TestUpdateChannelMonitorSettingsClears429CooldownOnAnySchedulingRevision(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	service.ClearChannelRateLimitCooldowns()
	t.Cleanup(service.ClearChannelRateLimitCooldowns)
	service.StartChannelRateLimitCooldown(1902, "model-a", 30)
	assert.NotZero(t, service.ChannelRateLimitCooldownUntil(1902, "model-a"))

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_performance_window_minutes": 120,
		"smart_schedule_control_revision":           "",
	})
	UpdateChannelMonitorSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Zero(t, service.ChannelRateLimitCooldownUntil(1902, "model-a"))
}

func TestUpdateChannelMonitorSettingsRejectsStaleSmartScheduleRevision(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleControlRevisionOption:   "revision-current",
		channelMonitorSmartSchedulePerformanceWindowOption: "60",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorSmartScheduleControlRevisionOption, Value: "revision-current"},
		{Key: channelMonitorSmartSchedulePerformanceWindowOption, Value: "60"},
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_performance_window_minutes": 120,
		"smart_schedule_control_revision":           "revision-current",
	})
	UpdateChannelMonitorSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var firstResponse channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &firstResponse))
	require.NotEmpty(t, firstResponse.Data.SmartScheduleControlRevision)
	assert.NotEqual(t, "revision-current", firstResponse.Data.SmartScheduleControlRevision)

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorSmartSchedulePerformanceWindowOption).First(&option).Error)
	assert.Equal(t, "120", option.Value)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_performance_window_minutes": 240,
		"smart_schedule_control_revision":           "revision-current",
	})
	UpdateChannelMonitorSettings(ctx)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "渠道监控设置已被其他请求修改")
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartSchedulePerformanceWindowOption).First(&option).Error)
	assert.Equal(t, "120", option.Value)
	option = model.Option{}
	require.NoError(t, db.Where("key = ?", channelMonitorSmartScheduleControlRevisionOption).First(&option).Error)
	assert.Equal(t, firstResponse.Data.SmartScheduleControlRevision, option.Value)
}

func TestUpdateChannelMonitorSettingsRequiresSmartScheduleRevision(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_performance_window_minutes": 120,
	})
	UpdateChannelMonitorSettings(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "缺少配置修订号")
}

func TestUpdateChannelMonitorSettingsRefreshesStaleInstanceBeforeCurrentRevisionSave(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	storedPolicies := channelSmartScheduleTestGroupPoliciesJSON(t, policy)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleControlRevisionOption: "revision-stale",
		channelMonitorSmartScheduleEnabledOption:         "false",
		channelMonitorSmartScheduleGroupPoliciesOption:   "[]",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorSmartScheduleControlRevisionOption, Value: "revision-current"},
		{Key: channelMonitorSmartScheduleEnabledOption, Value: "true"},
		{Key: channelMonitorSmartScheduleGroupPoliciesOption, Value: storedPolicies},
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_performance_window_minutes": 120,
		"smart_schedule_control_revision":           "revision-current",
	})
	UpdateChannelMonitorSettings(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Data.SmartScheduleEnabled)
	require.Len(t, response.Data.SmartScheduleGroupPolicies, 1)
	assert.Equal(t, "vip", response.Data.SmartScheduleGroupPolicies[0].Group)
	assert.Equal(t, 120, response.Data.SmartSchedulePerformanceWindowMinutes)
	assert.NotEqual(t, "revision-current", response.Data.SmartScheduleControlRevision)
}

func TestForceResetSmartScheduleDoesNotQueueTaskWhenCooldownSyncFails(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	policy := channelSmartScheduleTestGroupPolicy(
		"vip", channelMonitorSmartScheduleStrategyRatio, false,
		channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
	)
	serializedPolicies := channelSmartScheduleTestGroupPoliciesJSON(t, policy)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:         "true",
		channelMonitorSmartScheduleGroupPoliciesOption:   serializedPolicies,
		channelMonitorSmartScheduleControlRevisionOption: "revision-current",
	})
	require.NoError(t, db.Create(&[]model.Option{
		{Key: channelMonitorSmartScheduleEnabledOption, Value: "true"},
		{Key: channelMonitorSmartScheduleGroupPoliciesOption, Value: serializedPolicies},
		{Key: channelMonitorSmartScheduleControlRevisionOption, Value: "revision-current"},
	}).Error)

	originalRedisEnabled := common.RedisEnabled
	originalRedisClient := common.RDB
	failedRedisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	require.NoError(t, failedRedisClient.Close())
	common.RedisEnabled = true
	common.RDB = failedRedisClient
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRedisClient
		service.ClearChannelRateLimitCooldowns()
	})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_force_reset":      true,
		"smart_schedule_control_revision": "revision-current",
	})
	UpdateChannelMonitorSettings(ctx)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "429 冷却状态同步失败")
	task, err := model.GetActiveSystemTask(channelMonitorSmartScheduleTaskType)
	require.NoError(t, err)
	assert.Nil(t, task)
}

func TestUpdateChannelMonitorSettingsWindowChangeStopsTemporaryTraffic(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption:           "true",
		channelMonitorSmartSchedulePerformanceWindowOption: "60",
	})
	require.NoError(t, db.Create(&model.Option{
		Key: channelMonitorSmartSchedulePerformanceWindowOption, Value: "60",
	}).Error)
	priority := int64(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1902, Name: "temporary traffic", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1902, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 5,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1902, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1, BasePriority: 80, BaseWeight: 40,
		TemporaryTrafficKind: model.ChannelSmartScheduleTemporaryTrafficExploration,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_performance_window_minutes": 120,
		"smart_schedule_control_revision":           "",
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var state model.ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1902).First(&state).Error)
	assert.Empty(t, state.TemporaryTrafficKind)
	var ability model.Ability
	require.NoError(t, db.Where("channel_id = ?", 1902).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(80), *ability.Priority)
	assert.Equal(t, uint(40), ability.Weight)
}

func TestForceResetSmartScheduleQueuesOneTimeTask(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	require.NoError(t, db.Create([]model.Channel{
		{Id: 51, Name: "first", Status: common.ChannelStatusEnabled, Group: "vip"},
		{Id: 52, Name: "second", Status: common.ChannelStatusEnabled, Group: "vip"},
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_enabled":          true,
		"smart_schedule_force_reset":      true,
		"smart_schedule_control_revision": "",
		"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
			),
		},
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorSettingsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotNil(t, response.Data.SmartScheduleForceResetTaskCreated)
	assert.True(t, *response.Data.SmartScheduleForceResetTaskCreated)
	assert.NotEmpty(t, response.Data.SmartScheduleForceResetTaskId)
	assert.Empty(t, response.Data.SmartScheduleForceResetTaskError)

	task, err := model.GetActiveSystemTask(channelMonitorSmartScheduleTaskType)
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.Equal(t, response.Data.SmartScheduleForceResetTaskId, task.TaskID)
	payload := channelSmartScheduleTaskPayload{}
	require.NoError(t, task.DecodePayload(&payload))
	assert.True(t, payload.ForceReset)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_force_reset":      true,
		"smart_schedule_control_revision": response.Data.SmartScheduleControlRevision,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotNil(t, response.Data.SmartScheduleForceResetTaskCreated)
	assert.False(t, *response.Data.SmartScheduleForceResetTaskCreated)
	assert.Equal(t, task.TaskID, response.Data.SmartScheduleForceResetTaskId)
}

func TestUpdateChannelMonitorSettingsQueuesOneTimeTaskForEventDrivenConfigChange(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"smart_schedule_enabled":          true,
		"smart_schedule_control_revision": "",
		"smart_schedule_group_policies": []channelSmartScheduleGroupPolicy{
			channelSmartScheduleTestGroupPolicy(
				"vip", channelMonitorSmartScheduleStrategyRatio, false,
				channelMonitorSmartScheduleApplyWeight, []string{}, 5, 80, 30,
			),
		},
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	task, err := model.GetActiveSystemTask(channelMonitorSmartScheduleTaskType)
	require.NoError(t, err)
	require.NotNil(t, task)
	payload := channelSmartScheduleTaskPayload{}
	require.NoError(t, task.DecodePayload(&payload))
	assert.False(t, payload.ForceReset)
}

func TestRunChannelSmartScheduleRejectsManualRunWhileDisabled(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "false",
	})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/schedule/run", nil)
	RunChannelMonitorSmartSchedule(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "智能调度已禁用")
}

func TestRunChannelSmartScheduleRejectsManualRunWithoutGroupPolicy(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorSmartScheduleEnabledOption: "true",
	})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/schedule/run", nil)
	RunChannelMonitorSmartSchedule(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "请先配置至少一个完整的分组策略")
	_, err := runChannelSmartScheduleOnce(context.Background(), nil, false)
	require.ErrorContains(t, err, "请先配置至少一个完整的分组策略")
}

func TestRunChannelMonitorRatioUpdateReusesActiveTask(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/ratio/run", nil)
	RunChannelMonitorRatioUpdate(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var firstResponse channelMonitorTaskRunAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &firstResponse))
	require.True(t, firstResponse.Success)
	assert.True(t, firstResponse.Data.Created)
	assert.Equal(t, model.SystemTaskTypeChannelRatioMonitor, firstResponse.Data.Task.Type)
	assert.Equal(t, model.SystemTaskStatusPending, firstResponse.Data.Task.Status)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/ratio/run", nil)
	RunChannelMonitorRatioUpdate(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var secondResponse channelMonitorTaskRunAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &secondResponse))
	require.True(t, secondResponse.Success)
	assert.False(t, secondResponse.Data.Created)
	assert.Equal(t, firstResponse.Data.Task.TaskID, secondResponse.Data.Task.TaskID)

	var taskCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).
		Where("type = ?", model.SystemTaskTypeChannelRatioMonitor).
		Count(&taskCount).Error)
	assert.EqualValues(t, 1, taskCount)
}

func TestChannelMonitorOverviewIncludesLastFetchFailure(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	testModel := "gpt-4.1-mini"
	channelRemark := "临时渠道，晚高峰可能波动"
	upstreamBalance := 18.75
	require.NoError(t, db.Create(&model.Channel{
		Id:        9,
		Name:      "failed upstream",
		Key:       "secret",
		Remark:    &channelRemark,
		Status:    common.ChannelStatusEnabled,
		Models:    "gpt-4.1-mini,gpt-4.1",
		TestModel: &testModel,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:           9,
		LastFetchStatus:     model.ChannelRatioFetchStatusFailed,
		LastFetchError:      "upstream timeout",
		LastFetchTime:       123456,
		ConsecutiveFailures: 3,
		UpstreamBalance:     &upstreamBalance,
		LastBalanceTime:     123400,
		LastBalanceError:    "balance refresh timeout",
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorOverviewAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Channels, 1)
	assert.Equal(t, []int{9}, response.Data.ChannelOrder)
	item := response.Data.Channels[0]
	assert.Equal(t, channelRemark, item.ChannelRemark)
	assert.Equal(t, "gpt-4.1-mini,gpt-4.1", item.Models)
	assert.Equal(t, &testModel, item.TestModel)
	assert.Equal(t, model.ChannelRatioFetchStatusFailed, item.LastFetchStatus)
	assert.Equal(t, "upstream timeout", item.LastFetchError)
	assert.EqualValues(t, 123456, item.LastFetchTime)
	assert.Equal(t, 3, item.ConsecutiveFailures)
	require.NotNil(t, item.UpstreamBalance)
	assert.InDelta(t, upstreamBalance, *item.UpstreamBalance, 1e-9)
	assert.EqualValues(t, 123400, item.LastBalanceTime)
	assert.Equal(t, "balance refresh timeout", item.LastBalanceError)
}

func TestChannelMonitorOverviewIncludesAutoDisableReason(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	channel := model.Channel{
		Id:     10,
		Name:   "auto disabled",
		Key:    "secret",
		Status: common.ChannelStatusAutoDisabled,
	}
	channel.SetOtherInfo(map[string]interface{}{
		"status_reason": "渠道监控：上游倍率或余额更新失败",
	})
	require.NoError(t, db.Create(&channel).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorOverviewAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Channels, 1)
	assert.Equal(t, "渠道监控：上游倍率或余额更新失败", response.Data.Channels[0].StatusReason)
}

func TestChannelMonitorOverviewReadsCurrentMonitorConfiguration(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	require.NoError(t, db.Create(&model.Channel{
		Id: 901, Name: "current monitor config", Key: "secret", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 901, Ratio: 1.1, UpdatedTime: 1, ConcurrencyLimit: 2, ConcurrencyRevision: 1,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var firstResponse channelMonitorOverviewAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &firstResponse))
	require.Len(t, firstResponse.Data.Channels, 1)
	require.NotNil(t, firstResponse.Data.Channels[0].Ratio)
	assert.Equal(t, 1.1, *firstResponse.Data.Channels[0].Ratio)
	assert.Equal(t, 2, firstResponse.Data.Channels[0].ConcurrencyLimit)

	require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 901).Updates(map[string]any{
		"ratio":                1.4,
		"updated_time":         2,
		"concurrency_limit":    4,
		"concurrency_revision": 2,
	}).Error)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var secondResponse channelMonitorOverviewAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &secondResponse))
	require.Len(t, secondResponse.Data.Channels, 1)
	require.NotNil(t, secondResponse.Data.Channels[0].Ratio)
	assert.Equal(t, 1.4, *secondResponse.Data.Channels[0].Ratio)
	assert.Equal(t, 4, secondResponse.Data.Channels[0].ConcurrencyLimit)
}

func TestGetChannelMonitorHistoryBoundsPaginationAndUsesStableOrder(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 902, Name: "history channel", Key: "secret", Status: common.ChannelStatusEnabled,
	}).Error)
	history := make([]model.ChannelRatioHistory, 0, 25)
	for id := 1; id <= 25; id++ {
		history = append(history, model.ChannelRatioHistory{
			Id: id, ChannelId: 902, CreatedTime: 1_000, Remark: fmt.Sprintf("history-%d", id),
		})
	}
	require.NoError(t, db.Create(&history).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/channel/902/history?p=-2&page_size=-1", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "902"}}
	GetChannelMonitorHistory(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Page     int                         `json:"page"`
			PageSize int                         `json:"page_size"`
			Total    int                         `json:"total"`
			Items    []model.ChannelRatioHistory `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 1, response.Data.Page)
	assert.Equal(t, common.ItemsPerPage, response.Data.PageSize)
	assert.Equal(t, 25, response.Data.Total)
	require.Len(t, response.Data.Items, common.ItemsPerPage)
	assert.Equal(t, 25, response.Data.Items[0].Id)
	assert.Equal(t, 16, response.Data.Items[len(response.Data.Items)-1].Id)

	ctx, recorder = newChannelMonitorControllerContext(
		t, http.MethodGet,
		fmt.Sprintf("/api/channel_monitor/channel/902/history?p=%d&page_size=100", channelMonitorMaxPage+1), nil,
	)
	ctx.Params = gin.Params{{Key: "id", Value: "902"}}
	GetChannelMonitorHistory(ctx)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestUpdateChannelMonitorConcurrencyLimitValidatesPersistsAndReportsUsage(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	require.NoError(t, db.Create(&model.Channel{
		Id:     16,
		Name:   "limited channel",
		Key:    "secret",
		Status: common.ChannelStatusEnabled,
	}).Error)

	invalidRequests := []map[string]any{
		{},
		{"concurrency_limit": -1},
		{"concurrency_limit": service.MaxChannelConcurrencyLimit + 1},
		{"concurrency_limit": 1.5},
	}
	for _, request := range invalidRequests {
		ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/16/concurrency", request)
		ctx.Params = gin.Params{{Key: "id", Value: "16"}}
		UpdateChannelMonitorConcurrencyLimit(ctx)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	}

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/16/concurrency", map[string]any{
		"concurrency_limit": 2,
	})
	ctx.Params = gin.Params{{Key: "id", Value: "16"}}
	UpdateChannelMonitorConcurrencyLimit(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var updateResponse channelMonitorConcurrencyAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &updateResponse))
	require.True(t, updateResponse.Success)
	assert.Equal(t, 2, updateResponse.Data.ConcurrencyLimit)

	monitor, err := model.GetChannelRatioMonitor(16)
	require.NoError(t, err)
	assert.Equal(t, 2, monitor.ConcurrencyLimit)
	lease, acquired, status, err := service.AcquireChannelConcurrency(t.Context(), 16)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, service.ChannelConcurrencyStatus{Active: 1, Limit: 2}, status)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var overviewResponse channelMonitorOverviewAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &overviewResponse))
	require.Len(t, overviewResponse.Data.Channels, 1)
	assert.Equal(t, 2, overviewResponse.Data.Channels[0].ConcurrencyLimit)
	assert.Equal(t, 1, overviewResponse.Data.Channels[0].ConcurrencyActive)
	lease.Release()

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/16/concurrency", map[string]any{
		"concurrency_limit": 0,
	})
	ctx.Params = gin.Params{{Key: "id", Value: "16"}}
	UpdateChannelMonitorConcurrencyLimit(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(16)
	require.NoError(t, err)
	assert.Zero(t, monitor.ConcurrencyLimit)

	unlimitedLease, acquired, status, err := service.AcquireChannelConcurrency(t.Context(), 16)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, service.ChannelConcurrencyStatus{Active: 1, Limit: 0}, status)
	unlimitedLease.Release()
}

func TestUpdateChannelMonitorConcurrencyAndRPMLimits(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	require.NoError(t, db.Create(&model.Channel{
		Id: 17, Name: "rpm channel", Key: "secret", Status: common.ChannelStatusEnabled,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/17/concurrency", map[string]any{
		"rpm_limit": 2,
	})
	ctx.Params = gin.Params{{Key: "id", Value: "17"}}
	UpdateChannelMonitorConcurrencyLimit(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			ConcurrencyLimit int `json:"concurrency_limit"`
			RPMLimit         int `json:"rpm_limit"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Zero(t, response.Data.ConcurrencyLimit)
	assert.Equal(t, 2, response.Data.RPMLimit)

	monitor, err := model.GetChannelRatioMonitor(17)
	require.NoError(t, err)
	assert.Zero(t, monitor.ConcurrencyLimit)
	assert.Equal(t, 2, monitor.RPMLimit)

	lease, acquired, status, err := service.AcquireChannelConcurrency(t.Context(), 17)
	require.NoError(t, err)
	require.True(t, acquired)
	assert.Equal(t, service.ChannelConcurrencyStatus{Active: 1, Limit: 0, CurrentRPM: 1, RPMLimit: 2}, status)
	lease.Release()

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/17/concurrency", map[string]any{
		"concurrency_limit": 3,
	})
	ctx.Params = gin.Params{{Key: "id", Value: "17"}}
	UpdateChannelMonitorConcurrencyLimit(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(17)
	require.NoError(t, err)
	assert.Equal(t, 3, monitor.ConcurrencyLimit)
	assert.Equal(t, 2, monitor.RPMLimit)
}

func TestUpdateChannelMonitorChannelOrderPersistsNormalizedOrder(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	highPriority := int64(30)
	middlePriority := int64(20)
	lowPriority := int64(10)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1, Name: "one", Key: "secret-1", Priority: &highPriority},
		{Id: 2, Name: "two", Key: "secret-2", Priority: &middlePriority},
		{Id: 3, Name: "three", Key: "secret-3", Priority: &lowPriority},
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/order", map[string]any{
		"channel_ids": []int{3, 1},
	})
	UpdateChannelMonitorChannelOrder(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorOrderAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, []int{3, 1, 2}, response.Data.ChannelOrder)

	common.OptionMapRWMutex.RLock()
	storedChannelOrder := common.OptionMap[channelMonitorChannelOrderOption]
	common.OptionMapRWMutex.RUnlock()
	var channelOrder []int
	require.NoError(t, common.UnmarshalJsonStr(storedChannelOrder, &channelOrder))
	assert.Equal(t, []int{3, 1, 2}, channelOrder)

	invalidRequests := []map[string]any{
		{"channel_ids": []int{1, 1}},
		{"channel_ids": []int{999}},
		{},
	}
	for _, request := range invalidRequests {
		ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/order", request)
		UpdateChannelMonitorChannelOrder(ctx)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	}
}

func TestSaveChannelMonitorUpstreamConfigPersistsChannelPolicies(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      10,
		Name:    "stable",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":                     service.NewAPIUpstreamType,
		"base_url":                 "https://upstream.example",
		"group":                    "vip",
		"auth_type":                service.NewAPIUpstreamAuthPublic,
		"single_channel_action":    channelMonitorPolicyActionUpdateGroupRatio,
		"multiple_channels_action": channelMonitorPolicyActionDisableChannel,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/10/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "10"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorUpstreamConfigAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, channelMonitorPolicyActionUpdateGroupRatio, response.Data.SingleChannelAction)
	assert.Equal(t, channelMonitorPolicyActionDisableChannel, response.Data.MultipleChannelsAction)

	monitor, err := model.GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, channelMonitorPolicyActionUpdateGroupRatio, monitor.SingleChannelAction)
	assert.Equal(t, channelMonitorPolicyActionDisableChannel, monitor.MultipleChannelsAction)

	delete(request, "single_channel_action")
	delete(request, "multiple_channels_action")
	request["group"] = "standard"
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/10/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "10"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, "standard", monitor.UpstreamGroup)
	assert.Equal(t, channelMonitorPolicyActionUpdateGroupRatio, monitor.SingleChannelAction)
	assert.Equal(t, channelMonitorPolicyActionDisableChannel, monitor.MultipleChannelsAction)

	request["single_channel_action"] = "invalid"
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/10/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "10"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	request["single_channel_action"] = channelMonitorPolicyActionRemoveFromGroup
	request["multiple_channels_action"] = channelMonitorPolicyActionRemoveFromGroup
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/10/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "10"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	request["single_channel_action"] = channelMonitorPolicyActionNone
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/10/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "10"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, channelMonitorPolicyActionRemoveFromGroup, monitor.MultipleChannelsAction)
}

func TestSaveChannelMonitorUpstreamConfigManagesBalanceThresholds(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      11,
		Name:    "balance alert",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":                           service.NewAPIUpstreamType,
		"base_url":                       baseURL,
		"group":                          "vip",
		"auth_type":                      service.NewAPIUpstreamAuthPublic,
		"balance_warning_threshold":      20.5,
		"balance_auto_disable_threshold": 10.25,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/11/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "11"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response channelMonitorUpstreamConfigAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotNil(t, response.Data.BalanceWarningThreshold)
	assert.Equal(t, 20.5, *response.Data.BalanceWarningThreshold)
	require.NotNil(t, response.Data.BalanceAutoDisableThreshold)
	assert.Equal(t, 10.25, *response.Data.BalanceAutoDisableThreshold)

	delete(request, "balance_warning_threshold")
	delete(request, "balance_auto_disable_threshold")
	request["group"] = "standard"
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/11/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "11"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err := model.GetChannelRatioMonitor(11)
	require.NoError(t, err)
	require.NotNil(t, monitor.BalanceWarningThreshold)
	assert.Equal(t, 20.5, *monitor.BalanceWarningThreshold)
	require.NotNil(t, monitor.BalanceAutoDisableThreshold)
	assert.Equal(t, 10.25, *monitor.BalanceAutoDisableThreshold)

	request["balance_warning_threshold"] = nil
	request["balance_auto_disable_threshold"] = nil
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/11/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "11"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(11)
	require.NoError(t, err)
	assert.Nil(t, monitor.BalanceWarningThreshold)
	assert.Nil(t, monitor.BalanceAutoDisableThreshold)

	for _, field := range []string{"balance_warning_threshold", "balance_auto_disable_threshold"} {
		for _, invalidThreshold := range []any{-0.01, maxChannelMonitorBalanceThreshold + 1, "not-a-number"} {
			request[field] = invalidThreshold
			ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/11/upstream", request)
			ctx.Params = gin.Params{{Key: "id", Value: "11"}}
			SaveChannelMonitorUpstreamConfig(ctx)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		}
		request[field] = nil
	}
}

func TestSaveChannelMonitorUpstreamConfigAppliesCostConversion(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      15,
		Name:    "converted upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":      service.NewAPIUpstreamType,
		"base_url":  baseURL,
		"group":     "vip",
		"auth_type": service.NewAPIUpstreamAuthPublic,
		"cost_conversion": map[string]any{
			"mode":         service.ChannelMonitorCostConversionRecharge,
			"paid_cny":     100,
			"credited_usd": 200,
		},
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/15/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "15"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var configResponse channelMonitorUpstreamConfigAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &configResponse))
	assert.Equal(t, service.ChannelMonitorCostConversionRecharge, configResponse.Data.CostConversion.Mode)
	assert.Equal(t, 100.0, configResponse.Data.CostConversion.PaidCNY)
	assert.Equal(t, 200.0, configResponse.Data.CostConversion.CreditedUSD)

	monitor, err := model.GetChannelRatioMonitor(15)
	require.NoError(t, err)
	storedConversion, err := service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCostConversionRecharge, storedConversion.Mode)

	_, _, _, err = model.UpdateChannelRatioMonitorFromUpstream(15, 0.8, "first fetch", 1, "root")
	require.NoError(t, err)
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var overviewResponse channelMonitorOverviewAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &overviewResponse))
	require.Len(t, overviewResponse.Data.Channels, 1)
	require.NotNil(t, overviewResponse.Data.Channels[0].CostRatio)
	require.NotNil(t, overviewResponse.Data.Channels[0].ConversionFactor)
	assert.InDelta(t, 0.4, *overviewResponse.Data.Channels[0].CostRatio, 1e-9)
	assert.InDelta(t, 0.5, *overviewResponse.Data.Channels[0].ConversionFactor, 1e-9)

	_, _, _, err = model.UpdateChannelRatioMonitorFromUpstream(15, 1.0, "second fetch", 1, "root")
	require.NoError(t, err)
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &overviewResponse))
	require.Len(t, overviewResponse.Data.Channels, 1)
	require.NotNil(t, overviewResponse.Data.Channels[0].PreviousRatio)
	require.NotNil(t, overviewResponse.Data.Channels[0].PreviousCostRatio)
	assert.InDelta(t, 0.8, *overviewResponse.Data.Channels[0].PreviousRatio, 1e-9)
	assert.InDelta(t, 0.4, *overviewResponse.Data.Channels[0].PreviousCostRatio, 1e-9)

	delete(request, "cost_conversion")
	request["group"] = "standard"
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/15/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "15"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(15)
	require.NoError(t, err)
	storedConversion, err = service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCostConversionRecharge, storedConversion.Mode)

	request["cost_conversion"] = map[string]any{"mode": service.ChannelMonitorCostConversionNone}
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/15/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "15"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(15)
	require.NoError(t, err)
	storedConversion, err = service.ParseChannelMonitorCostConversion(monitor.CostConversion)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCostConversionNone, storedConversion.Mode)
	assert.Nil(t, monitor.PreviousRatio)
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &overviewResponse))
	require.Len(t, overviewResponse.Data.Channels, 1)
	assert.Nil(t, overviewResponse.Data.Channels[0].PreviousRatio)
	assert.Nil(t, overviewResponse.Data.Channels[0].PreviousCostRatio)

	request["cost_conversion"] = map[string]any{
		"mode":         service.ChannelMonitorCostConversionRecharge,
		"paid_cny":     100,
		"credited_usd": 0,
	}
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/15/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "15"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestSaveChannelMonitorCustomUpstreamConfigAppliesFixedValues(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":0.3}`})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":0.3}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	baseURL := "https://custom.example/api"
	require.NoError(t, db.Create(&model.Channel{
		Id:      28,
		Name:    "fixed custom upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":                  service.CustomUpstreamType,
		"base_url":              baseURL,
		"group":                 "",
		"auth_type":             service.CustomUpstreamAuthType,
		"single_channel_action": channelMonitorPolicyActionDisableChannel,
		"custom_config": map[string]any{
			"version": 1,
			"ratio": map[string]any{
				"source":      service.ChannelMonitorCustomSourceFixed,
				"fixed_value": 0.75,
			},
			"balance": map[string]any{
				"source":      service.ChannelMonitorCustomSourceFixed,
				"fixed_value": 25.5,
			},
		},
		"cost_conversion": map[string]any{
			"mode":         service.ChannelMonitorCostConversionRecharge,
			"paid_cny":     100,
			"credited_usd": 200,
		},
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/28/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "28"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response channelMonitorUpstreamConfigAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotNil(t, response.Data.CustomConfig)
	assert.Equal(t, service.CustomUpstreamType, response.Data.Type)
	assert.Equal(t, service.ChannelMonitorCustomSourceFixed, response.Data.CustomConfig.Ratio.Source)
	require.NotNil(t, response.Data.CustomConfig.Ratio.FixedValue)
	assert.Equal(t, 0.75, *response.Data.CustomConfig.Ratio.FixedValue)

	monitor, err := model.GetChannelRatioMonitor(28)
	require.NoError(t, err)
	assert.Equal(t, 0.75, monitor.Ratio)
	assert.NotZero(t, monitor.UpdatedTime)
	require.NotNil(t, monitor.UpstreamBalance)
	assert.Equal(t, 25.5, *monitor.UpstreamBalance)
	channel, err := model.GetChannelById(28, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	storedConfig, err := service.ParseChannelMonitorCustomUpstreamConfig(monitor.CustomUpstreamConfig)
	require.NoError(t, err)
	assert.Equal(t, service.ChannelMonitorCustomSourceFixed, storedConfig.Balance.Source)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var overviewResponse channelMonitorOverviewAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &overviewResponse))
	require.Len(t, overviewResponse.Data.Channels, 1)
	require.NotNil(t, overviewResponse.Data.Channels[0].CostRatio)
	assert.InDelta(t, 0.375, *overviewResponse.Data.Channels[0].CostRatio, 1e-9)
}

func TestChannelMonitorCustomUpstreamTestUsesUnsavedHTTPConfig(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/account", r.URL.Path)
		assert.Equal(t, "vip", r.URL.Query().Get("group"))
		assert.Equal(t, "Bearer unsaved-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ratio":"1.25","balance":42},"authorization":"Bearer unsaved-secret"}`))
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      29,
		Name:    "unsaved custom upstream",
		Key:     "unused",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":                 service.CustomUpstreamType,
		"base_url":             server.URL,
		"group":                "",
		"auth_type":            service.CustomUpstreamAuthType,
		"ratio_sync_enabled":   false,
		"balance_sync_enabled": true,
		"custom_config": map[string]any{
			"version": 1,
			"ratio": map[string]any{
				"source": service.ChannelMonitorCustomSourceHTTP,
				"request": map[string]any{
					"method":    http.MethodGet,
					"path":      "/account",
					"body_type": service.ChannelMonitorCustomBodyNone,
					"query": []map[string]any{
						{"key": "group", "value": "vip"},
					},
					"headers": []map[string]any{
						{"key": "Authorization", "value": "Bearer unsaved-secret", "secret": true},
					},
				},
				"result": map[string]any{
					"response_type": service.ChannelMonitorCustomResponseJSON,
					"value_path":    "data.ratio",
					"multiplier":    1,
				},
			},
			"balance": map[string]any{
				"source": service.ChannelMonitorCustomSourceHTTP,
				"result": map[string]any{
					"response_type": service.ChannelMonitorCustomResponseJSON,
					"value_path":    "data.balance",
					"multiplier":    1,
				},
			},
			"balance_reuse_ratio_request": true,
		},
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/29/upstream/test", request)
	ctx.Params = gin.Params{{Key: "id", Value: "29"}}
	TestChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response struct {
		Success bool                           `json:"success"`
		Data    service.NewAPIGroupRatioResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, 1.25, response.Data.Ratio)
	require.NotNil(t, response.Data.Balance.Amount)
	assert.Equal(t, 42.0, *response.Data.Balance.Amount)
	require.NotNil(t, response.Data.Debug)
	assert.NotContains(t, response.Data.Debug.ResponsePreview, "unsaved-secret")
	assert.Contains(t, response.Data.Debug.ResponsePreview, "[REDACTED]")

	_, err := model.GetChannelRatioMonitor(29)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestChannelMonitorCustomUpstreamTestReusesSavedSecret(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer saved-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ratio":0.8}`))
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      30,
		Name:    "saved custom upstream",
		Key:     "unused",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)
	balance := 12.0
	customConfig := service.ChannelMonitorCustomUpstreamConfig{
		Version: 1,
		Ratio: service.ChannelMonitorCustomMetricConfig{
			Source: service.ChannelMonitorCustomSourceHTTP,
			Request: &service.ChannelMonitorCustomRequestConfig{
				Method:   http.MethodGet,
				Path:     "/ratio",
				BodyType: service.ChannelMonitorCustomBodyNone,
				Headers: []service.ChannelMonitorCustomKeyValue{
					{Key: "Authorization", Value: "Bearer saved-secret", Secret: true},
				},
			},
			Result: &service.ChannelMonitorCustomResultConfig{
				ResponseType: service.ChannelMonitorCustomResponseJSON,
				ValuePath:    "ratio",
				Multiplier:   1,
			},
		},
		Balance: service.ChannelMonitorCustomMetricConfig{
			Source:     service.ChannelMonitorCustomSourceFixed,
			FixedValue: &balance,
		},
	}
	saveRequest := map[string]any{
		"type":          service.CustomUpstreamType,
		"base_url":      server.URL,
		"group":         "",
		"auth_type":     service.CustomUpstreamAuthType,
		"custom_config": customConfig,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/30/upstream", saveRequest)
	ctx.Params = gin.Params{{Key: "id", Value: "30"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var saveResponse channelMonitorUpstreamConfigAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &saveResponse))
	require.NotNil(t, saveResponse.Data.CustomConfig)
	require.NotNil(t, saveResponse.Data.CustomConfig.Ratio.Request)
	require.Len(t, saveResponse.Data.CustomConfig.Ratio.Request.Headers, 1)
	savedHeader := saveResponse.Data.CustomConfig.Ratio.Request.Headers[0]
	assert.Empty(t, savedHeader.Value)
	assert.True(t, savedHeader.HasValue)
	assert.NotContains(t, recorder.Body.String(), "saved-secret")

	testRequest := map[string]any{
		"type":          service.CustomUpstreamType,
		"base_url":      server.URL,
		"group":         "",
		"auth_type":     service.CustomUpstreamAuthType,
		"custom_config": saveResponse.Data.CustomConfig,
	}
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/30/upstream/test", testRequest)
	ctx.Params = gin.Params{{Key: "id", Value: "30"}}
	TestChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var testResponse struct {
		Success bool                           `json:"success"`
		Data    service.NewAPIGroupRatioResult `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &testResponse))
	require.True(t, testResponse.Success)
	assert.Equal(t, 0.8, testResponse.Data.Ratio)
}

func TestSaveChannelMonitorCustomFixedBalanceUsesAutoDisableThreshold(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)

	tests := []struct {
		name       string
		channelId  int
		balance    float64
		threshold  float64
		wantStatus int
	}{
		{name: "below threshold", channelId: 31, balance: 2, threshold: 3, wantStatus: common.ChannelStatusAutoDisabled},
		{name: "equal to threshold", channelId: 32, balance: 3, threshold: 3, wantStatus: common.ChannelStatusEnabled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, db.Create(&model.Channel{
				Id: test.channelId, Name: test.name, Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&model.Ability{
				Group: "vip", Model: "model-a", ChannelId: test.channelId, Enabled: true,
			}).Error)
			ratio := 1.0
			request := map[string]any{
				"type":                           service.CustomUpstreamType,
				"base_url":                       "https://custom.example",
				"auth_type":                      service.CustomUpstreamAuthType,
				"balance_auto_disable_threshold": test.threshold,
				"custom_config": service.ChannelMonitorCustomUpstreamConfig{
					Version: 1,
					Ratio: service.ChannelMonitorCustomMetricConfig{
						Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &ratio,
					},
					Balance: service.ChannelMonitorCustomMetricConfig{
						Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &test.balance,
					},
				},
			}
			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, fmt.Sprintf("/api/channel_monitor/channel/%d/upstream", test.channelId), request)
			ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(test.channelId)}}
			SaveChannelMonitorUpstreamConfig(ctx)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

			monitor, err := model.GetChannelRatioMonitor(test.channelId)
			require.NoError(t, err)
			require.NotNil(t, monitor.UpstreamBalance)
			assert.Equal(t, test.balance, *monitor.UpstreamBalance)
			channel, err := model.GetChannelById(test.channelId, true)
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, channel.Status)
			var ability model.Ability
			require.NoError(t, db.First(&ability, "channel_id = ?", test.channelId).Error)
			assert.Equal(t, test.wantStatus == common.ChannelStatusEnabled, ability.Enabled)
		})
	}
}

func TestSaveChannelMonitorUpstreamConfigManagesSyncCapabilities(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      12,
		Name:    "custom upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":                 service.NewAPIUpstreamType,
		"base_url":             baseURL,
		"group":                "vip",
		"auth_type":            service.NewAPIUpstreamAuthPublic,
		"ratio_sync_enabled":   false,
		"balance_sync_enabled": false,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/12/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "12"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorUpstreamConfigAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.False(t, response.Data.RatioSyncEnabled)
	assert.False(t, response.Data.BalanceSyncEnabled)
	monitor, err := model.GetChannelRatioMonitor(12)
	require.NoError(t, err)
	assert.True(t, monitor.UpstreamRatioSyncDisabled)
	assert.True(t, monitor.UpstreamBalanceSyncDisabled)

	delete(request, "ratio_sync_enabled")
	delete(request, "balance_sync_enabled")
	request["group"] = "standard"
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/12/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "12"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(12)
	require.NoError(t, err)
	assert.True(t, monitor.UpstreamRatioSyncDisabled)
	assert.True(t, monitor.UpstreamBalanceSyncDisabled)

	request["ratio_sync_enabled"] = true
	request["balance_sync_enabled"] = true
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/12/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "12"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(12)
	require.NoError(t, err)
	assert.False(t, monitor.UpstreamRatioSyncDisabled)
	assert.False(t, monitor.UpstreamBalanceSyncDisabled)
}

func TestSaveChannelMonitorSub2APIConfigPersistsToken(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      13,
		Name:    "session-bound upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":          service.Sub2APIUpstreamType,
		"base_url":      baseURL,
		"group":         "vip",
		"auth_type":     service.Sub2APIAuthToken,
		"access_token":  "jwt-token",
		"refresh_token": "refresh-token",
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/13/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "13"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	monitor, err := model.GetChannelRatioMonitor(13)
	require.NoError(t, err)
	assert.Equal(t, "jwt-token", monitor.UpstreamAccessToken)
	assert.Equal(t, "refresh-token", monitor.UpstreamRefreshToken)
	assert.NotContains(t, recorder.Body.String(), "jwt-token")
	assert.NotContains(t, recorder.Body.String(), "refresh-token")
}

func TestSaveChannelMonitorSub2APIConfigPersistsRefreshToken(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      16,
		Name:    "refreshable upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":         service.Sub2APIUpstreamType,
		"base_url":     baseURL,
		"group":        "vip",
		"auth_type":    service.Sub2APIAuthRefreshToken,
		"access_token": "refresh-token",
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/16/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "16"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	monitor, err := model.GetChannelRatioMonitor(16)
	require.NoError(t, err)
	assert.Equal(t, service.Sub2APIAuthRefreshToken, monitor.UpstreamAuthType)
	assert.Equal(t, "refresh-token", monitor.UpstreamAccessToken)
	assert.NotContains(t, recorder.Body.String(), "refresh-token")
}

func TestSaveChannelMonitorSub2APIConfigPersistsRotatedRefreshToken(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)
	var refreshRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshRequests.Add(1)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-rotated","expires_in":3600}}`))
		case "/api/v1/groups/available":
			assert.Equal(t, "Bearer access-fresh", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      18,
		Name:    "rotating refresh token upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":                 service.Sub2APIUpstreamType,
		"base_url":             baseURL,
		"group":                "vip",
		"auth_type":            service.Sub2APIAuthRefreshToken,
		"access_token":         "refresh-original",
		"balance_sync_enabled": false,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/18/upstream/test", request)
	ctx.Params = gin.Params{{Key: "id", Value: "18"}}
	TestChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/18/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "18"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.EqualValues(t, 1, refreshRequests.Load())
	monitor, err := model.GetChannelRatioMonitor(18)
	require.NoError(t, err)
	assert.Equal(t, "refresh-rotated", monitor.UpstreamAccessToken)
	assert.NotContains(t, recorder.Body.String(), "refresh-rotated")
}

func TestSaveChannelMonitorSub2APIConfigCanonicalizesExplicitRotatedRefreshToken(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"access-fresh","refresh_token":"refresh-rotated","expires_in":3600}}`))
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      19,
		Name:    "existing rotating refresh token upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:           19,
		UpstreamType:        service.Sub2APIUpstreamType,
		UpstreamBaseURL:     baseURL,
		UpstreamGroup:       "vip",
		UpstreamAuthType:    service.Sub2APIAuthRefreshToken,
		UpstreamAccessToken: "refresh-original",
		UpstreamRevision:    3,
	}).Error)

	request := map[string]any{
		"type":                 service.Sub2APIUpstreamType,
		"base_url":             baseURL,
		"group":                "vip",
		"auth_type":            service.Sub2APIAuthRefreshToken,
		"access_token":         "refresh-original",
		"balance_sync_enabled": false,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/19/upstream/test", request)
	ctx.Params = gin.Params{{Key: "id", Value: "19"}}
	TestChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/19/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "19"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err := model.GetChannelRatioMonitor(19)
	require.NoError(t, err)
	assert.Equal(t, "refresh-rotated", monitor.UpstreamAccessToken)
}

func TestResolveChannelMonitorSub2APIRefreshTokenCredentialBinding(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      17,
		Name:    "refreshable upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:           17,
		UpstreamType:        service.Sub2APIUpstreamType,
		UpstreamBaseURL:     baseURL,
		UpstreamAuthType:    service.Sub2APIAuthRefreshToken,
		UpstreamAccessToken: "saved-refresh-token",
		UpstreamRevision:    4,
	}).Error)
	channel, err := model.GetChannelById(17, false)
	require.NoError(t, err)

	config, err := resolveChannelMonitorUpstreamRequest(channel, channelMonitorUpstreamRequest{
		Type:     service.Sub2APIUpstreamType,
		BaseURL:  baseURL,
		Group:    "vip",
		AuthType: service.Sub2APIAuthRefreshToken,
	}, true)
	require.NoError(t, err)
	assert.Equal(t, "saved-refresh-token", config.AccessToken)
	assert.Equal(t, 17, config.CredentialID)
	assert.Equal(t, int64(4), config.Revision)

	config, err = resolveChannelMonitorUpstreamRequest(channel, channelMonitorUpstreamRequest{
		Type:        service.Sub2APIUpstreamType,
		BaseURL:     baseURL,
		Group:       "vip",
		AuthType:    service.Sub2APIAuthRefreshToken,
		AccessToken: "replacement-refresh-token",
	}, true)
	require.NoError(t, err)
	assert.Equal(t, "replacement-refresh-token", config.AccessToken)
	assert.Zero(t, config.CredentialID)
	assert.Zero(t, config.Revision)
}

func TestSaveChannelMonitorSub2APIConfigPersistsAccountPassword(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      15,
		Name:    "account upstream",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":      service.Sub2APIUpstreamType,
		"base_url":  baseURL,
		"group":     "vip",
		"auth_type": service.Sub2APIAuthAccount,
		"account":   "monitor@example.com",
		"password":  "secret-password",
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/15/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "15"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	monitor, err := model.GetChannelRatioMonitor(15)
	require.NoError(t, err)
	assert.Equal(t, service.Sub2APIAuthAccount, monitor.UpstreamAuthType)
	assert.Equal(t, "monitor@example.com", monitor.UpstreamAccount)
	assert.Equal(t, "secret-password", monitor.UpstreamPassword)
	assert.Empty(t, monitor.UpstreamAccessToken)
	assert.Contains(t, recorder.Body.String(), `"account":"monitor@example.com"`)
	assert.Contains(t, recorder.Body.String(), `"has_password":true`)
	assert.NotContains(t, recorder.Body.String(), "secret-password")

	request["password"] = ""
	request["group"] = "standard"
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/15/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "15"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	monitor, err = model.GetChannelRatioMonitor(15)
	require.NoError(t, err)
	assert.Equal(t, "secret-password", monitor.UpstreamPassword)
}

func TestSaveChannelMonitorSub2APIConfigAllowsChannelKeyOnly(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      14,
		Name:    "api-key-only upstream",
		Key:     "sk-direct",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":      service.Sub2APIUpstreamType,
		"base_url":  baseURL,
		"group":     "vip",
		"auth_type": service.Sub2APIAuthAPIKey,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/14/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "14"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	monitor, err := model.GetChannelRatioMonitor(14)
	require.NoError(t, err)
	assert.Equal(t, service.Sub2APIUpstreamType, monitor.UpstreamType)
	assert.Equal(t, service.Sub2APIAuthAPIKey, monitor.UpstreamAuthType)
	assert.Empty(t, monitor.UpstreamAccessToken)
	assert.Contains(t, recorder.Body.String(), `"has_access_token":false`)
}

func TestListChannelMonitorUpstreamGroupsUsesSavedSub2APIToken(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "Bearer jwt-token", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case "/api/v1/keys":
			assert.Equal(t, "secret", r.URL.Query().Get("search"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"items":[{"id":99,"key":"secret","group_id":7}],"total":1,"page":1,"page_size":1000,"pages":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      20,
		Name:    "sub2api",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:           20,
		UpstreamType:        service.Sub2APIUpstreamType,
		UpstreamBaseURL:     server.URL,
		UpstreamGroup:       "vip",
		UpstreamAuthType:    service.Sub2APIAuthToken,
		UpstreamAccessToken: "jwt-token",
	}).Error)

	request := map[string]any{
		"type":         service.Sub2APIUpstreamType,
		"base_url":     server.URL,
		"group":        "vip",
		"auth_type":    service.Sub2APIAuthToken,
		"access_token": "",
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/20/upstream/groups", request)
	ctx.Params = gin.Params{{Key: "id", Value: "20"}}
	ListChannelMonitorUpstreamGroups(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorUpstreamGroupsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Groups, 1)
	assert.Equal(t, "vip", response.Data.Groups[0].Name)
	assert.Equal(t, 1.25, response.Data.Groups[0].Ratio)
	assert.Equal(t, "vip", response.Data.AppliedGroup)
	assert.Empty(t, response.Data.AppliedGroupError)

	monitor, err := model.GetChannelRatioMonitor(20)
	require.NoError(t, err)
	assert.Equal(t, "jwt-token", monitor.UpstreamAccessToken)
}

func TestListChannelMonitorUpstreamGroupsAcceptsUnsavedSub2APIToken(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "Bearer jwt-token", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api/v1/groups/available":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{}}`))
		case "/api/v1/keys":
			assert.Equal(t, "secret", r.URL.Query().Get("search"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"items":[{"id":99,"key":"secret","group_id":7}],"total":1,"page":1,"page_size":1000,"pages":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      21,
		Name:    "unconfigured sub2api",
		Key:     "secret",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	request := map[string]any{
		"type":                 service.Sub2APIUpstreamType,
		"base_url":             server.URL,
		"group":                "",
		"auth_type":            service.Sub2APIAuthToken,
		"access_token":         "jwt-token",
		"balance_sync_enabled": false,
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/21/upstream/groups", request)
	ctx.Params = gin.Params{{Key: "id", Value: "21"}}
	ListChannelMonitorUpstreamGroups(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorUpstreamGroupsAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Groups, 1)
	assert.Equal(t, "vip", response.Data.Groups[0].Name)
	assert.Equal(t, "vip", response.Data.AppliedGroup)

	_, err := model.GetChannelRatioMonitor(21)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestApplyChannelMonitorUpstreamGroupUpdatesRemoteTokenAndRecordsRatio(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":1}`})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	disableChannelMonitorSSRFProtection(t)

	updatedGroup := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "Bearer dashboard-token", r.Header.Get("Authorization"))
		assert.Equal(t, "42", r.Header.Get("New-Api-User"))
		switch r.URL.Path {
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.4}}}`))
		case "/api/token/search":
			assert.Equal(t, "sk-channel", r.URL.Query().Get("token"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":31,"name":"channel","expired_time":-1,"remain_quota":0,"unlimited_quota":true,"model_limits_enabled":false,"model_limits":"","allow_ips":null,"group":"default","cross_group_retry":false}]}}`))
		case "/api/token/":
			var request struct {
				Group string `json:"group"`
			}
			require.NoError(t, common.DecodeJson(r.Body, &request))
			updatedGroup = request.Group
			_, _ = w.Write([]byte(`{"success":true,"message":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      22,
		Name:    "new-api",
		Key:     "sk-channel",
		Group:   "vip",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:           22,
		Ratio:               1,
		UpdatedTime:         1,
		UpstreamType:        service.NewAPIUpstreamType,
		UpstreamBaseURL:     server.URL,
		UpstreamGroup:       "vip",
		UpstreamAuthType:    service.NewAPIUpstreamAuthUser,
		UpstreamUserId:      42,
		UpstreamAccessToken: "dashboard-token",
		SingleChannelAction: channelMonitorPolicyActionDisableChannel,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/22/upstream/group/apply", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "22"}}
	ApplyChannelMonitorUpstreamGroup(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorUpstreamGroupApplyAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.Equal(t, "vip", updatedGroup)
	assert.Equal(t, 1, response.Data.KeysUpdated)
	assert.True(t, response.Data.Changed)
	assert.InDelta(t, 1.4, response.Data.Result.Ratio, 1e-9)
	assert.NotContains(t, recorder.Body.String(), "dashboard-token")
	assert.NotContains(t, recorder.Body.String(), "sk-channel")

	monitor, err := model.GetChannelRatioMonitor(22)
	require.NoError(t, err)
	assert.InDelta(t, 1.4, monitor.Ratio, 1e-9)
	assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
	assert.Contains(t, monitor.Remark, "切换到分组 vip")
	storedChannel, err := model.GetChannelById(22, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
}

func TestApplyChannelMonitorUpstreamGroupDoesNotOverwriteNewerConfig(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.4}}}`))
		case "/api/token/search":
			_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"id":31,"name":"channel","expired_time":-1,"remain_quota":0,"unlimited_quota":true,"model_limits_enabled":false,"model_limits":"","allow_ips":null,"group":"default","cross_group_retry":false}]}}`))
		case "/api/token/":
			require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).
				Where("channel_id = ?", 24).
				Updates(map[string]any{
					"upstream_group":    "new-config",
					"upstream_revision": int64(2),
				}).Error)
			_, _ = w.Write([]byte(`{"success":true,"message":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 24, Name: "new-api", Key: "sk-channel", Group: "vip", BaseURL: &baseURL,
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 24, Ratio: 1, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
		UpstreamUserId: 42, UpstreamAccessToken: "dashboard-token", UpstreamRevision: 1,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/24/upstream/group/apply", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "24"}}
	ApplyChannelMonitorUpstreamGroup(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "本地上游配置已变更")

	monitor, err := model.GetChannelRatioMonitor(24)
	require.NoError(t, err)
	assert.Equal(t, "new-config", monitor.UpstreamGroup)
	assert.Equal(t, int64(2), monitor.UpstreamRevision)
	assert.Equal(t, 1.0, monitor.Ratio)
}

func TestAutoDisableChannelMonitorForLowBalanceIgnoresStaleThreshold(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{Id: 25, Name: "balance", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	currentThreshold := 0.5
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 25, UpstreamRevision: 2, BalanceAutoDisableThreshold: &currentThreshold,
	}).Error)

	staleThreshold := 10.0
	changed, err := autoDisableChannelMonitorForLowBalance(model.ChannelRatioMonitor{
		ChannelId: 25, UpstreamRevision: 1, BalanceAutoDisableThreshold: &staleThreshold,
	}, &channel, 0.25)
	require.NoError(t, err)
	assert.False(t, changed)

	storedChannel, err := model.GetChannelById(25, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, storedChannel.Status)
}

func TestAutoDisableChannelMonitorForLowBalanceIncludesRecentLocalCostWhenBelowWarning(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{Id: 26, Name: "balance estimate", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	warningThreshold := 10.0
	autoDisableThreshold := 4.0
	costConversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{
		Mode:        service.ChannelMonitorCostConversionRecharge,
		PaidCNY:     2,
		CreditedUSD: 1,
	})
	require.NoError(t, err)
	now := common.GetTimestamp()
	lastBalanceTime := now - 60
	lastBalanceCostNanoCNY := int64(100) * model.ChannelDailyCostNanoPerCNY
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 26, now-120, lastBalanceCostNanoCNY, 1, 0))
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 26, now-1, 4*model.ChannelDailyCostNanoPerCNY, 1, 0))
	monitor := model.ChannelRatioMonitor{
		ChannelId:                   26,
		UpstreamRevision:            1,
		LastBalanceTime:             lastBalanceTime,
		LastBalanceCostNanoCNY:      &lastBalanceCostNanoCNY,
		BalanceWarningThreshold:     &warningThreshold,
		BalanceAutoDisableThreshold: &autoDisableThreshold,
		CostConversion:              costConversion,
	}
	require.NoError(t, db.Create(&monitor).Error)

	changed, err := autoDisableChannelMonitorForLowBalanceWithContext(context.Background(), monitor, &channel, 5)
	require.NoError(t, err)
	assert.True(t, changed)

	storedChannel, err := model.GetChannelById(26, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
	assert.Contains(t, storedChannel.GetOtherInfo()["status_reason"], "本地消费估算 2，估算余额 3")
}

func TestAutoDisableChannelMonitorForLowBalanceDoesNotEstimateAboveWarning(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{Id: 27, Name: "balance warning", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	warningThreshold := 4.0
	autoDisableThreshold := 4.0
	costConversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{
		Mode:        service.ChannelMonitorCostConversionRecharge,
		PaidCNY:     2,
		CreditedUSD: 1,
	})
	require.NoError(t, err)
	now := common.GetTimestamp()
	lastBalanceCostNanoCNY := int64(0)
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 27, now-1, 4*model.ChannelDailyCostNanoPerCNY, 1, 0))
	monitor := model.ChannelRatioMonitor{
		ChannelId:                   27,
		UpstreamRevision:            1,
		LastBalanceTime:             now - 60,
		LastBalanceCostNanoCNY:      &lastBalanceCostNanoCNY,
		BalanceWarningThreshold:     &warningThreshold,
		BalanceAutoDisableThreshold: &autoDisableThreshold,
		CostConversion:              costConversion,
	}
	require.NoError(t, db.Create(&monitor).Error)

	changed, err := autoDisableChannelMonitorForLowBalanceWithContext(context.Background(), monitor, &channel, 5)
	require.NoError(t, err)
	assert.False(t, changed)

	storedChannel, err := model.GetChannelById(27, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, storedChannel.Status)
}

func TestRecordChannelMonitorBalanceUpdateCarriesPendingConsumptionUntilUpstreamReflectsIt(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	warningThreshold := 100.0
	autoDisableThreshold := 4.0
	now := common.GetTimestamp()
	baselineCostNanoCNY := int64(20) * model.ChannelDailyCostNanoPerCNY
	previousBalance := 50.0
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 28, now-120, baselineCostNanoCNY, 1, 0))
	monitor := model.ChannelRatioMonitor{
		ChannelId:                   28,
		UpstreamRevision:            1,
		UpstreamBalance:             &previousBalance,
		LastBalanceTime:             now - 60,
		LastBalanceCostNanoCNY:      &baselineCostNanoCNY,
		BalanceWarningThreshold:     &warningThreshold,
		BalanceAutoDisableThreshold: &autoDisableThreshold,
	}
	require.NoError(t, db.Create(&monitor).Error)

	require.NoError(t, model.AddChannelDailyCost(context.Background(), 28, now-1, 3*model.ChannelDailyCostNanoPerCNY, 1, 0))
	unchangedBalance := 50.0
	evaluation, applied, err := recordChannelMonitorBalanceUpdate(context.Background(), monitor, &unchangedBalance, "")
	require.NoError(t, err)
	require.True(t, applied)
	require.NotNil(t, evaluation)
	assert.InDelta(t, 3, evaluation.EstimatedConsumption, 1e-9)
	assert.InDelta(t, 47, evaluation.EffectiveBalance, 1e-9)

	monitor, err = model.GetChannelRatioMonitor(28)
	require.NoError(t, err)
	assert.InDelta(t, 3, monitor.BalancePendingConsumption, 1e-9)
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 28, now, 2*model.ChannelDailyCostNanoPerCNY, 1, 0))
	evaluation, applied, err = recordChannelMonitorBalanceUpdate(context.Background(), monitor, &unchangedBalance, "")
	require.NoError(t, err)
	require.True(t, applied)
	require.NotNil(t, evaluation)
	assert.InDelta(t, 5, evaluation.EstimatedConsumption, 1e-9)
	assert.InDelta(t, 45, evaluation.EffectiveBalance, 1e-9)

	monitor, err = model.GetChannelRatioMonitor(28)
	require.NoError(t, err)
	reflectedBalance := 45.0
	evaluation, applied, err = recordChannelMonitorBalanceUpdate(context.Background(), monitor, &reflectedBalance, "")
	require.NoError(t, err)
	require.True(t, applied)
	require.NotNil(t, evaluation)
	assert.Zero(t, evaluation.EstimatedConsumption)
	assert.InDelta(t, reflectedBalance, evaluation.EffectiveBalance, 1e-9)

	monitor, err = model.GetChannelRatioMonitor(28)
	require.NoError(t, err)
	assert.Zero(t, monitor.BalancePendingConsumption)
}

func TestFetchChannelMonitorUpstreamBalanceRecordsSnapshotAndAutoDisables(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self":
			assert.Equal(t, "Bearer dashboard-token", r.Header.Get("Authorization"))
			assert.Equal(t, "42", r.Header.Get("New-Api-User"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1750000}}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 23, Name: "balance", Key: "secret", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 23, Enabled: true,
	}).Error)
	autoDisableThreshold := 4.0
	warningThreshold := 10.0
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:                   23,
		UpstreamType:                service.NewAPIUpstreamType,
		UpstreamBaseURL:             server.URL,
		UpstreamAuthType:            service.NewAPIUpstreamAuthUser,
		UpstreamUserId:              42,
		UpstreamAccessToken:         "dashboard-token",
		BalanceWarningThreshold:     &warningThreshold,
		BalanceAutoDisableThreshold: &autoDisableThreshold,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/23/upstream/balance/fetch", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "23"}}
	FetchChannelMonitorUpstreamBalance(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorUpstreamBalanceAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	require.NotNil(t, response.Data.Amount)
	assert.InDelta(t, 3.5, *response.Data.Amount, 1e-9)

	monitor, err := model.GetChannelRatioMonitor(23)
	require.NoError(t, err)
	require.NotNil(t, monitor.UpstreamBalance)
	assert.InDelta(t, 3.5, *monitor.UpstreamBalance, 1e-9)
	assert.NotZero(t, monitor.LastBalanceTime)
	require.NotNil(t, monitor.LastBalanceCostNanoCNY)
	assert.Zero(t, *monitor.LastBalanceCostNanoCNY)
	assert.Zero(t, monitor.BalancePendingConsumption)
	assert.Empty(t, monitor.LastBalanceError)
	channel, err := model.GetChannelById(23, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	assert.Contains(t, channel.GetOtherInfo()["status_reason"], "上游余额 3.5 低于自动禁用阈值 4")
	var ability model.Ability
	require.NoError(t, db.First(&ability, "channel_id = ?", 23).Error)
	assert.False(t, ability.Enabled)
}

func TestFetchChannelMonitorUpstreamRatioSkipsSeparateBalanceRequest(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	disableChannelMonitorSSRFProtection(t)
	var ratioRequests atomic.Int32
	var balanceRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			ratioRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
		case "/api/user/self":
			balanceRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":350}}`))
		case "/api/status":
			balanceRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 25, Name: "ratio and balance", Key: "secret", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 25, Enabled: true,
	}).Error)
	autoDisableThreshold := 4.0
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:                   25,
		Ratio:                       1,
		UpstreamType:                service.NewAPIUpstreamType,
		UpstreamBaseURL:             server.URL,
		UpstreamGroup:               "vip",
		UpstreamAuthType:            service.NewAPIUpstreamAuthUser,
		UpstreamUserId:              42,
		UpstreamAccessToken:         "dashboard-token",
		BalanceAutoDisableThreshold: &autoDisableThreshold,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/25/upstream/fetch", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "25"}}
	FetchChannelMonitorUpstreamRatio(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"balance_auto_disabled":false`)
	assert.Equal(t, int32(1), ratioRequests.Load())
	assert.Zero(t, balanceRequests.Load())

	monitor, err := model.GetChannelRatioMonitor(25)
	require.NoError(t, err)
	assert.Equal(t, 1.25, monitor.Ratio)
	assert.Nil(t, monitor.UpstreamBalance)
	channel, err := model.GetChannelById(25, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	var ability model.Ability
	require.NoError(t, db.First(&ability, "channel_id = ?", 25).Error)
	assert.True(t, ability.Enabled)
}

func TestManualSharedUpstreamRequestRefreshesRatioAndBalance(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler func(*gin.Context)
	}{
		{name: "ratio refresh", path: "/api/channel_monitor/channel/25/upstream/fetch", handler: FetchChannelMonitorUpstreamRatio},
		{name: "balance refresh", path: "/api/channel_monitor/channel/25/upstream/balance/fetch", handler: FetchChannelMonitorUpstreamBalance},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":1}`})
			originalGroupRatios := ratio_setting.GroupRatio2JSONString()
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
			t.Cleanup(func() {
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
			})
			disableChannelMonitorSSRFProtection(t)
			var upstreamRequests atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamRequests.Add(1)
				assert.Equal(t, "/metrics", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"ratio":1.25,"balance":3.5}}`))
			}))
			defer server.Close()

			customConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
				Version: 1,
				Ratio: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceHTTP,
					Request: &service.ChannelMonitorCustomRequestConfig{
						Method:   http.MethodGet,
						Path:     "/metrics",
						BodyType: service.ChannelMonitorCustomBodyNone,
					},
					Result: &service.ChannelMonitorCustomResultConfig{
						ResponseType: service.ChannelMonitorCustomResponseJSON,
						ValuePath:    "data.ratio",
						Multiplier:   1,
					},
				},
				Balance: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceHTTP,
					Result: &service.ChannelMonitorCustomResultConfig{
						ResponseType: service.ChannelMonitorCustomResponseJSON,
						ValuePath:    "data.balance",
						Multiplier:   1,
					},
				},
				BalanceReuseRatioRequest: true,
			})
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Channel{
				Id: 25, Name: "shared metrics", Key: "secret", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&model.Channel{
				Id: 26, Name: "shared peer", Key: "secret", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&model.Ability{
				Group: "vip", Model: "model-a", ChannelId: 25, Enabled: true,
			}).Error)
			autoDisableThreshold := 3.0
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId:                   25,
				Ratio:                       1,
				UpstreamType:                service.CustomUpstreamType,
				UpstreamBaseURL:             server.URL,
				UpstreamAuthType:            service.CustomUpstreamAuthType,
				CustomUpstreamConfig:        customConfig,
				BalanceAutoDisableThreshold: &autoDisableThreshold,
				MultipleChannelsAction:      channelMonitorPolicyActionDisableChannel,
			}).Error)

			ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, test.path, nil)
			ctx.Params = gin.Params{{Key: "id", Value: "25"}}
			test.handler(ctx)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"success":true`)
			assert.Equal(t, int32(1), upstreamRequests.Load())

			monitor, err := model.GetChannelRatioMonitor(25)
			require.NoError(t, err)
			assert.Equal(t, 1.25, monitor.Ratio)
			require.NotNil(t, monitor.UpstreamBalance)
			assert.Equal(t, 3.5, *monitor.UpstreamBalance)
			channel, err := model.GetChannelById(25, true)
			require.NoError(t, err)
			assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
			assert.Equal(t, channelMonitorCostRatioPolicyDisableReason, channel.GetOtherInfo()["status_reason"])
			peer, err := model.GetChannelById(26, true)
			require.NoError(t, err)
			assert.Equal(t, common.ChannelStatusEnabled, peer.Status)
			var ability model.Ability
			require.NoError(t, db.First(&ability, "channel_id = ?", 25).Error)
			assert.False(t, ability.Enabled)
		})
	}
}

func TestManualUpstreamRefreshSkipsDisabledCapabilities(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	var upstreamRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		http.Error(w, "unsupported", http.StatusNotFound)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 24, Name: "custom upstream", Key: "secret", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:                   24,
		UpstreamType:                service.NewAPIUpstreamType,
		UpstreamBaseURL:             server.URL,
		UpstreamGroup:               "vip",
		UpstreamAuthType:            service.NewAPIUpstreamAuthPublic,
		UpstreamRatioSyncDisabled:   true,
		UpstreamBalanceSyncDisabled: true,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/24/upstream/fetch", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "24"}}
	FetchChannelMonitorUpstreamRatio(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "该渠道已关闭上游倍率同步")

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/24/upstream/balance/fetch", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "24"}}
	FetchChannelMonitorUpstreamBalance(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "该渠道已关闭上游余额同步")
	assert.Zero(t, upstreamRequests.Load())
}

func TestResolveChannelMonitorUpstreamRequestDoesNotReuseCredentialsAcrossHosts(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	oldBaseURL := "https://old.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      21,
		Name:    "secure",
		Key:     "secret",
		BaseURL: &oldBaseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:           21,
		UpstreamType:        service.NewAPIUpstreamType,
		UpstreamBaseURL:     oldBaseURL,
		UpstreamAuthType:    service.NewAPIUpstreamAuthUser,
		UpstreamUserId:      7,
		UpstreamAccessToken: "saved-token",
	}).Error)
	channel, err := model.GetChannelById(21, false)
	require.NoError(t, err)

	_, err = resolveChannelMonitorUpstreamRequest(channel, channelMonitorUpstreamRequest{
		Type:     service.NewAPIUpstreamType,
		BaseURL:  "https://new.example",
		Group:    "vip",
		AuthType: service.NewAPIUpstreamAuthUser,
		UserId:   7,
	}, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "访问令牌")
}

func TestResolveChannelMonitorUpstreamRequestIncludesChannelProxy(t *testing.T) {
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorUpstreamRequestTimeoutOption: "45",
	})
	channel := &model.Channel{Id: 21}
	channel.SetSetting(dto.ChannelSettings{Proxy: "socks5://127.0.0.1:1080"})

	config, err := resolveChannelMonitorUpstreamRequest(channel, channelMonitorUpstreamRequest{
		Type:     service.NewAPIUpstreamType,
		BaseURL:  "https://upstream.example",
		Group:    "vip",
		AuthType: service.NewAPIUpstreamAuthPublic,
	}, true)
	require.NoError(t, err)
	assert.Equal(t, "socks5://127.0.0.1:1080", config.Proxy)
	assert.Equal(t, 45*time.Second, config.RequestTimeout)
}

func TestPlanChannelMonitorPolicyActions(t *testing.T) {
	enabledChannel := func(id int, group string) *model.Channel {
		return &model.Channel{Id: id, Group: group, Status: common.ChannelStatusEnabled}
	}

	t.Run("single channel update uses coefficient", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {CostRatio: 1.2, SingleChannelAction: channelMonitorPolicyActionUpdateGroupRatio},
			},
			map[string]float64{"vip": 1},
			map[string]float64{"vip": 1.1},
		)
		require.Contains(t, plan.GroupRatioUpdates, "vip")
		assert.InDelta(t, 1.32, plan.GroupRatioUpdates["vip"], 1e-9)
		assert.Equal(t, model.ChannelMonitorGroupRatioValueSnapshot{
			Ratio: 1, Coefficient: 1.1,
		}, plan.GroupRatioValues["vip"])
		assert.Empty(t, plan.DisableChannelIds)
	})

	t.Run("disabled peers use single channel policy", func(t *testing.T) {
		disabled := &model.Channel{Id: 2, Group: "vip", Status: common.ChannelStatusManuallyDisabled}
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), disabled},
			map[int]channelMonitorPolicyInput{
				1: {CostRatio: 1.25, SingleChannelAction: channelMonitorPolicyActionDisableChannel},
				2: {CostRatio: 9, SingleChannelAction: channelMonitorPolicyActionUpdateGroupRatio},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Equal(t, []int{1}, plan.DisableChannelIds)
	})

	t.Run("multiple channel update uses highest target", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {CostRatio: 1.1, MultipleChannelsAction: channelMonitorPolicyActionUpdateGroupRatio},
				2: {CostRatio: 1.4, MultipleChannelsAction: channelMonitorPolicyActionUpdateGroupRatio},
			},
			map[string]float64{"vip": 1},
			map[string]float64{"vip": 1.2},
		)
		require.Contains(t, plan.GroupRatioUpdates, "vip")
		assert.InDelta(t, 1.68, plan.GroupRatioUpdates["vip"], 1e-9)
	})

	t.Run("multiple channel policies apply per channel", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{
				enabledChannel(1, "vip"),
				enabledChannel(2, "vip"),
				enabledChannel(3, "vip"),
			},
			map[int]channelMonitorPolicyInput{
				1: {CostRatio: 1.1, MultipleChannelsAction: channelMonitorPolicyActionNone},
				2: {CostRatio: 1.3, MultipleChannelsAction: channelMonitorPolicyActionDisableChannel},
				3: {CostRatio: 1.25, MultipleChannelsAction: channelMonitorPolicyActionUpdateGroupRatio},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Equal(t, []int{2}, plan.DisableChannelIds)
		require.Contains(t, plan.GroupRatioUpdates, "vip")
		assert.InDelta(t, 1.25, plan.GroupRatioUpdates["vip"], 1e-9)
	})

	t.Run("multiple disables re-evaluate the remaining single channel", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {
					CostRatio:              1.2,
					MultipleChannelsAction: channelMonitorPolicyActionDisableChannel,
				},
				2: {
					CostRatio:              1.3,
					SingleChannelAction:    channelMonitorPolicyActionUpdateGroupRatio,
					MultipleChannelsAction: channelMonitorPolicyActionDisableChannel,
				},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Equal(t, []int{1}, plan.DisableChannelIds)
		require.Contains(t, plan.GroupRatioUpdates, "vip")
		assert.InDelta(t, 1.3, plan.GroupRatioUpdates["vip"], 1e-9)
	})

	t.Run("single channel disable can continue after multiple channel disable", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {
					CostRatio:              1.2,
					SingleChannelAction:    channelMonitorPolicyActionDisableChannel,
					MultipleChannelsAction: channelMonitorPolicyActionDisableChannel,
				},
				2: {
					CostRatio:              1.3,
					SingleChannelAction:    channelMonitorPolicyActionDisableChannel,
					MultipleChannelsAction: channelMonitorPolicyActionDisableChannel,
				},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Equal(t, []int{1, 2}, plan.DisableChannelIds)
		assert.Empty(t, plan.GroupRatioUpdates)
	})

	t.Run("temporary channel is disabled then stable channel uses single policy", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {
					CostRatio:              1.2,
					SingleChannelAction:    channelMonitorPolicyActionUpdateGroupRatio,
					MultipleChannelsAction: channelMonitorPolicyActionUpdateGroupRatio,
				},
				2: {
					CostRatio:              1.5,
					SingleChannelAction:    channelMonitorPolicyActionDisableChannel,
					MultipleChannelsAction: channelMonitorPolicyActionDisableChannel,
				},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Equal(t, []int{2}, plan.DisableChannelIds)
		require.Contains(t, plan.GroupRatioUpdates, "vip")
		assert.InDelta(t, 1.2, plan.GroupRatioUpdates["vip"], 1e-9)
		assert.Equal(t, map[int]int64{1: 0, 2: 0}, plan.GroupRatioRevisions["vip"])
		assert.Equal(t, map[int]string{1: "vip", 2: "vip"}, plan.GroupRatioMemberships["vip"])
	})

	t.Run("disabling a channel re-evaluates its other groups", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{
				enabledChannel(1, "vip,team"),
				enabledChannel(2, "vip"),
				enabledChannel(3, "team"),
			},
			map[int]channelMonitorPolicyInput{
				1: {
					CostRatio:              1.5,
					MultipleChannelsAction: channelMonitorPolicyActionDisableChannel,
				},
				2: {CostRatio: 1.1},
				3: {
					CostRatio:           2.5,
					SingleChannelAction: channelMonitorPolicyActionUpdateGroupRatio,
				},
			},
			map[string]float64{"vip": 1, "team": 2},
			nil,
		)
		assert.Equal(t, []int{1}, plan.DisableChannelIds)
		require.Contains(t, plan.GroupRatioUpdates, "team")
		assert.InDelta(t, 2.5, plan.GroupRatioUpdates["team"], 1e-9)
	})

	t.Run("removing a channel re-evaluates the remaining single channel", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip,backup"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {
					CostRatio:              1.5,
					MultipleChannelsAction: channelMonitorPolicyActionRemoveFromGroup,
				},
				2: {
					CostRatio:           1.25,
					SingleChannelAction: channelMonitorPolicyActionUpdateGroupRatio,
				},
			},
			map[string]float64{"vip": 1, "backup": 2},
			nil,
		)
		assert.Equal(t, []model.ChannelMonitorGroupMembershipRemoval{{
			ChannelId: 1, Group: "vip", ExpectedGroups: "vip,backup", GuardUpstreamRevision: true,
		}}, plan.GroupMembershipRemovals)
		require.Contains(t, plan.GroupRatioUpdates, "vip")
		assert.InDelta(t, 1.25, plan.GroupRatioUpdates["vip"], 1e-9)
		assert.Equal(t, map[int]int64{1: 0, 2: 0}, plan.GroupRatioRevisions["vip"])
		assert.Equal(t, map[int]string{1: "vip,backup", 2: "vip"}, plan.GroupRatioMemberships["vip"])
		assert.Empty(t, plan.DisableChannelIds)
	})

	t.Run("disable policy takes precedence over membership removal", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip,team"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {
					CostRatio:              1.5,
					SingleChannelAction:    channelMonitorPolicyActionDisableChannel,
					MultipleChannelsAction: channelMonitorPolicyActionRemoveFromGroup,
				},
				2: {CostRatio: 1},
			},
			map[string]float64{"vip": 1, "team": 1},
			nil,
		)
		assert.Equal(t, []int{1}, plan.DisableChannelIds)
		assert.Empty(t, plan.GroupMembershipRemovals)
	})

	t.Run("membership removal keeps the channel's only group", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {CostRatio: 1.5, MultipleChannelsAction: channelMonitorPolicyActionRemoveFromGroup},
				2: {CostRatio: 1},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Empty(t, plan.GroupMembershipRemovals)
		assert.Empty(t, plan.DisableChannelIds)
		assert.Empty(t, plan.GroupRatioUpdates)
	})

	t.Run("incomplete current ratios still apply channel disable policy", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {CostRatio: 1.5, MultipleChannelsAction: channelMonitorPolicyActionDisableChannel},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Equal(t, []int{1}, plan.DisableChannelIds)
		assert.Empty(t, plan.GroupRatioUpdates)
		assert.Equal(t, 1, plan.SkippedGroupCount)
	})

	t.Run("incomplete current ratios still apply group removal policy", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip,backup"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {
					CostRatio:              1.5,
					SingleChannelAction:    channelMonitorPolicyActionDisableChannel,
					MultipleChannelsAction: channelMonitorPolicyActionRemoveFromGroup,
				},
			},
			map[string]float64{"vip": 1, "backup": 2},
			nil,
		)
		assert.Equal(t, []model.ChannelMonitorGroupMembershipRemoval{{
			ChannelId: 1, Group: "vip", ExpectedGroups: "vip,backup", GuardUpstreamRevision: true,
		}}, plan.GroupMembershipRemovals)
		assert.Empty(t, plan.DisableChannelIds)
		assert.Empty(t, plan.GroupRatioUpdates)
		assert.Equal(t, 1, plan.SkippedGroupCount)
	})

	t.Run("incomplete current ratios skip group ratio update", func(t *testing.T) {
		plan := planChannelMonitorPolicyActions(
			[]*model.Channel{enabledChannel(1, "vip"), enabledChannel(2, "vip")},
			map[int]channelMonitorPolicyInput{
				1: {CostRatio: 1.5, MultipleChannelsAction: channelMonitorPolicyActionUpdateGroupRatio},
			},
			map[string]float64{"vip": 1},
			nil,
		)
		assert.Empty(t, plan.DisableChannelIds)
		assert.Empty(t, plan.GroupRatioUpdates)
		assert.Equal(t, 1, plan.SkippedGroupCount)
	})
}

func TestUpdateChannelMonitorRatioAppliesSingleChannelPolicyImmediately(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":1}`})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	require.NoError(t, db.Create(&model.Channel{
		Id: 29, Name: "single policy", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 29, Ratio: 1, UpdatedTime: 1,
		SingleChannelAction: channelMonitorPolicyActionDisableChannel,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/29/ratio", map[string]any{
		"ratio": 1.25,
	})
	ctx.Params = gin.Params{{Key: "id", Value: "29"}}
	UpdateChannelMonitorRatio(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":true`)

	channel, err := model.GetChannelById(29, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}

func TestSaveChannelMonitorUpstreamConfigAppliesPolicyAfterCostConversionChange(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":1}`})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	baseURL := "https://upstream.example"
	require.NoError(t, db.Create(&model.Channel{
		Id: 30, Name: "conversion policy", Group: "vip", BaseURL: &baseURL,
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 30, Ratio: 1, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: baseURL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
	}).Error)

	request := map[string]any{
		"type":                  service.NewAPIUpstreamType,
		"base_url":              baseURL,
		"group":                 "vip",
		"auth_type":             service.NewAPIUpstreamAuthPublic,
		"single_channel_action": channelMonitorPolicyActionDisableChannel,
		"cost_conversion": map[string]any{
			"mode":         service.ChannelMonitorCostConversionRecharge,
			"paid_cny":     200,
			"credited_usd": 100,
		},
	}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/channel/30/upstream", request)
	ctx.Params = gin.Params{{Key: "id", Value: "30"}}
	SaveChannelMonitorUpstreamConfig(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	channel, err := model.GetChannelById(30, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}

func TestFetchChannelMonitorUpstreamRatioAppliesMultipleChannelPolicyImmediately(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":1}`})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":1.25}}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 31, Name: "refresh policy", Key: "secret", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 32, Name: "refresh peer", Key: "secret", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 31, Ratio: 1, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
		MultipleChannelsAction:      channelMonitorPolicyActionDisableChannel,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/31/upstream/fetch", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "31"}}
	FetchChannelMonitorUpstreamRatio(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	channel, err := model.GetChannelById(31, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	peer, err := model.GetChannelById(32, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, peer.Status)
}

func TestApplyChannelMonitorPolicyPlanMarksGroupUpdateFailure(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&model.Option{}))

	groupsUpdated, removedMemberships, disabledChannelIds, groupUpdateFailed, err := applyChannelMonitorPolicyPlan(
		context.Background(),
		channelMonitorPolicyPlan{GroupRatioUpdates: map[string]float64{"monitor-test": 2}},
	)

	require.Error(t, err)
	assert.Zero(t, groupsUpdated)
	assert.Empty(t, removedMemberships)
	assert.Empty(t, disabledChannelIds)
	assert.True(t, groupUpdateFailed)
}

func TestApplyChannelMonitorPolicyPlanSkipsStaleDisableRevision(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(10)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id: 26, Name: "stale policy", Status: common.ChannelStatusEnabled,
		Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 26, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 26, UpstreamRevision: 2,
	}).Error)

	_, _, disabledChannelIds, _, err := applyChannelMonitorPolicyPlan(
		context.Background(),
		channelMonitorPolicyPlan{
			DisableChannelIds:       []int{26},
			DisableChannelRevisions: map[int]int64{26: 1},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, disabledChannelIds)

	storedChannel, err := model.GetChannelById(26, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, storedChannel.Status)
}

func TestApplyChannelMonitorPolicyPlanSkipsStaleGroupRatioRevision(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":1}`})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 27, UpstreamRevision: 2,
	}).Error)

	groupsUpdated, _, _, groupUpdateFailed, err := applyChannelMonitorPolicyPlan(
		context.Background(),
		channelMonitorPolicyPlan{
			GroupRatioUpdates: map[string]float64{"vip": 2},
			GroupRatioRevisions: model.ChannelMonitorGroupRatioRevisionGuard{
				"vip": {27: 1},
			},
		},
	)
	require.NoError(t, err)
	assert.Zero(t, groupsUpdated)
	assert.False(t, groupUpdateFailed)
	assert.Equal(t, float64(1), ratio_setting.GetGroupRatioCopy()["vip"])
}

func TestApplyChannelMonitorPolicyPlanSkipsChangedGroupPolicyValues(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		"GroupRatio": `{"vip":1}`,
		model.ChannelMonitorGroupCoefficientsOption: `{}`,
	})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	_, err := model.MergeChannelMonitorGroupOptions(
		map[string]float64{"vip": 0.8},
		map[string]float64{"vip": 1.2},
		false,
	)
	require.NoError(t, err)

	groupsUpdated, _, _, groupUpdateFailed, err := applyChannelMonitorPolicyPlan(
		context.Background(),
		channelMonitorPolicyPlan{
			GroupRatioUpdates: map[string]float64{"vip": 2},
			GroupRatioValues: model.ChannelMonitorGroupRatioValueGuard{
				"vip": {Ratio: 1, Coefficient: 1},
			},
		},
	)
	require.NoError(t, err)
	assert.Zero(t, groupsUpdated)
	assert.False(t, groupUpdateFailed)
	assert.Equal(t, 0.8, ratio_setting.GetGroupRatioCopy()["vip"])
}

func TestSyncChannelMonitorGroupRatioUsesHighestEnabledChannel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{"GroupRatio": `{"vip":1}`})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	channels := []model.Channel{
		{Id: 1, Name: "first", Key: "first-key", Group: "vip", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "highest", Key: "highest-key", Group: "vip", Status: common.ChannelStatusEnabled},
		{Id: 3, Name: "disabled", Key: "disabled-key", Group: "vip", Status: common.ChannelStatusManuallyDisabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	monitors := []model.ChannelRatioMonitor{
		{ChannelId: 1, Ratio: 1.2, UpdatedTime: 1, CostConversion: `{"mode":"recharge","paid_cny":200,"credited_usd":100}`},
		{ChannelId: 2, Ratio: 1.5, UpdatedTime: 1},
		{ChannelId: 3, Ratio: 9, UpdatedTime: 1},
	}
	require.NoError(t, db.Create(&monitors).Error)

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/group/sync", map[string]any{
		"group": "vip", "coefficient": 1.1,
	})
	SyncChannelMonitorGroupRatio(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response channelMonitorGroupSyncAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, "vip", response.Data.Group)
	assert.InDelta(t, 1.2, response.Data.UpstreamRatio, 1e-9)
	assert.InDelta(t, 2, response.Data.ConversionFactor, 1e-9)
	assert.InDelta(t, 2.4, response.Data.CostRatio, 1e-9)
	assert.InDelta(t, 1.1, response.Data.Coefficient, 1e-9)
	assert.InDelta(t, 2.64, response.Data.Ratio, 1e-9)
	assert.InDelta(t, 2.64, ratio_setting.GetGroupRatio("vip"), 1e-9)
	assert.InDelta(t, 1.1, getChannelMonitorGroupCoefficients()["vip"], 1e-9)
}

func TestRunChannelRatioMonitorTaskRespectsPerChannelSyncCapabilities(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "0",
	})
	disableChannelMonitorSSRFProtection(t)

	var ratioRequests atomic.Int32
	var balanceRequests atomic.Int32
	var statusRequests atomic.Int32
	var unexpectedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			ratioRequests.Add(1)
			assert.Equal(t, "42", r.Header.Get("New-Api-User"))
			assert.Equal(t, "Bearer ratio-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
		case "/api/user/self":
			balanceRequests.Add(1)
			assert.Equal(t, "43", r.Header.Get("New-Api-User"))
			assert.Equal(t, "Bearer balance-token", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
		case "/api/status":
			statusRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		default:
			unexpectedRequests.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ratioRemark := "倍率线路"
	balanceRemark := "余额线路"
	disabledRemark := "停用线路"
	channels := []model.Channel{
		{Id: 1, Name: "ratio only", Remark: &ratioRemark, Key: "ratio-key", Group: "vip", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "balance only", Remark: &balanceRemark, Key: "balance-key", Group: "vip", Status: common.ChannelStatusEnabled},
		{Id: 3, Name: "fully disabled", Remark: &disabledRemark, Key: "disabled-key", Group: "vip", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	monitors := []model.ChannelRatioMonitor{
		{
			ChannelId: 1, Ratio: 1, UpdatedTime: 1,
			UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
			UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 42, UpstreamAccessToken: "ratio-token",
			UpstreamBalanceSyncDisabled: true,
		},
		{
			ChannelId:    2,
			UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
			UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 43, UpstreamAccessToken: "balance-token",
			UpstreamRatioSyncDisabled: true,
		},
		{
			ChannelId:    3,
			UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
			UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 44, UpstreamAccessToken: "disabled-token",
			UpstreamRatioSyncDisabled: true, UpstreamBalanceSyncDisabled: true,
		},
	}
	require.NoError(t, db.Create(&monitors).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, summary.Total)
	assert.Equal(t, 2, summary.Updated)
	assert.Equal(t, 1, summary.Changed)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Equal(t, 1, summary.Skipped)
	assert.Zero(t, summary.Failed)
	require.Len(t, summary.ChangedChannels, 1)
	assert.Equal(t, 1, summary.ChangedChannels[0].ChannelId)
	assert.Equal(t, "ratio only", summary.ChangedChannels[0].ChannelName)
	assert.Equal(t, ratioRemark, summary.ChangedChannels[0].ChannelRemark)
	assert.InDelta(t, 1.25, summary.ChangedChannels[0].NewRatio, 1e-9)
	require.Len(t, summary.BalanceUpdates, 1)
	assert.Equal(t, 2, summary.BalanceUpdates[0].ChannelId)
	assert.Equal(t, "balance only", summary.BalanceUpdates[0].ChannelName)
	assert.Equal(t, balanceRemark, summary.BalanceUpdates[0].ChannelRemark)
	assert.InDelta(t, 5, summary.BalanceUpdates[0].Balance, 1e-9)
	require.Len(t, summary.SkippedChannels, 1)
	assert.Equal(t, 3, summary.SkippedChannels[0].ChannelId)
	assert.Equal(t, "fully disabled", summary.SkippedChannels[0].ChannelName)
	assert.Equal(t, disabledRemark, summary.SkippedChannels[0].ChannelRemark)
	assert.EqualValues(t, 1, ratioRequests.Load())
	assert.EqualValues(t, 1, balanceRequests.Load())
	assert.EqualValues(t, 1, statusRequests.Load())
	assert.Zero(t, unexpectedRequests.Load())

	ratioMonitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.InDelta(t, 1.25, ratioMonitor.Ratio, 1e-9)
	assert.Nil(t, ratioMonitor.UpstreamBalance)
	assert.Empty(t, ratioMonitor.LastBalanceError)

	balanceMonitor, err := model.GetChannelRatioMonitor(2)
	require.NoError(t, err)
	assert.Zero(t, balanceMonitor.UpdatedTime)
	require.NotNil(t, balanceMonitor.UpstreamBalance)
	assert.InDelta(t, 5, *balanceMonitor.UpstreamBalance, 1e-9)
}

func TestRunChannelRatioMonitorTaskProcessesChannelsConcurrently(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "0",
	})
	disableChannelMonitorSSRFProtection(t)

	var activeRequests atomic.Int32
	var maxActiveRequests atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/pricing", r.URL.Path)
		active := activeRequests.Add(1)
		for {
			previous := maxActiveRequests.Load()
			if active <= previous || maxActiveRequests.CompareAndSwap(previous, active) {
				break
			}
		}
		started <- struct{}{}
		<-release
		activeRequests.Add(-1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"success\":true,\"group_ratio\":{\"vip\":1.25}}"))
	}))
	defer server.Close()

	channels := []model.Channel{
		{Id: 1, Name: "first", Key: "first-key", Group: "vip", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "second", Key: "second-key", Group: "vip", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 1, Ratio: 1, UpdatedTime: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL, UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic, UpstreamBalanceSyncDisabled: true},
		{ChannelId: 2, Ratio: 1, UpdatedTime: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL, UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic, UpstreamBalanceSyncDisabled: true},
	}).Error)

	taskDone := make(chan struct{})
	var summary channelRatioMonitorTaskResult
	var taskErr error
	go func() {
		summary, taskErr = runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
		close(taskDone)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			require.FailNow(t, "expected both channel refreshes to start concurrently")
		}
	}
	close(release)
	<-taskDone

	require.NoError(t, taskErr)
	assert.Equal(t, 2, summary.Updated)
	assert.GreaterOrEqual(t, maxActiveRequests.Load(), int32(2))
}

func TestRunChannelRatioMonitorTaskUpdatesCustomFixedSources(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "0",
	})
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "custom fixed", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	ratio := 0.8
	balance := 12.5
	autoDisableThreshold := 13.0
	customConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
		Ratio: service.ChannelMonitorCustomMetricConfig{
			Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &ratio,
		},
		Balance: service.ChannelMonitorCustomMetricConfig{
			Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &balance,
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, Ratio: 0.5, UpdatedTime: 1,
		UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: "https://custom.example",
		UpstreamAuthType: service.CustomUpstreamAuthType, CustomUpstreamConfig: customConfig,
		BalanceAutoDisableThreshold: &autoDisableThreshold,
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Total)
	assert.Equal(t, 1, summary.Updated)
	assert.Equal(t, 1, summary.Changed)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Equal(t, 1, summary.ChannelsDisabled)
	assert.Zero(t, summary.Failed)

	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, ratio, monitor.Ratio)
	require.NotNil(t, monitor.UpstreamBalance)
	assert.Equal(t, balance, *monitor.UpstreamBalance)
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}

func TestRunChannelRatioMonitorTaskUsesCostRatioForGroupPolicy(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)
	useChannelMonitorOptionMap(t, map[string]string{
		"GroupRatio":                             `{"vip":0.4}`,
		channelMonitorAutoUpdateRetryCountOption: "0",
	})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":0.4}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/user/self/groups", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.2}}}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "converted", Key: "secret", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, Ratio: 1, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
		UpstreamUserId: 42, UpstreamAccessToken: "dashboard-token",
		UpstreamBalanceSyncDisabled: true,
		SingleChannelAction:         channelMonitorPolicyActionUpdateGroupRatio,
		MultipleChannelsAction:      channelMonitorPolicyActionNone,
		CostConversion:              `{"mode":"recharge","paid_cny":100,"credited_usd":200}`,
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Updated)
	assert.Equal(t, 1, summary.Changed)
	assert.Equal(t, 1, summary.GroupsUpdated)
	assert.InDelta(t, 0.6, ratio_setting.GetGroupRatio("vip"), 1e-9)

	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.InDelta(t, 1.2, monitor.Ratio, 1e-9)
}

func TestRunChannelRatioMonitorTaskContinuesAfterFailure(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	disableChannelMonitorSSRFProtection(t)

	var failingRequestCount atomic.Int32
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failingRequestCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failingServer.Close()
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":1.25}}`))
	}))
	defer successServer.Close()

	channels := []model.Channel{
		{Id: 1, Name: "failing disabled", Key: "first-key", Group: "vip", Status: common.ChannelStatusManuallyDisabled},
		{Id: 2, Name: "successful", Key: "second-key", Group: "vip", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	monitors := []model.ChannelRatioMonitor{
		{ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: failingServer.URL, UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic},
		{ChannelId: 2, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: successServer.URL, UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic},
	}
	require.NoError(t, db.Create(&monitors).Error)

	progress := make([][2]int, 0, 2)
	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), func(processed, total int) {
		progress = append(progress, [2]int{processed, total})
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Total)
	assert.Equal(t, 1, summary.Updated)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 3, summary.Retried)
	assert.Zero(t, summary.RecoveredAfterRetry)
	require.Len(t, summary.Failures, 1)
	assert.Equal(t, 1, summary.Failures[0].ChannelId)
	assert.Equal(t, model.ChannelRatioFailureAlertRatio, summary.Failures[0].Kind)
	assert.Equal(t, "failing disabled", summary.Failures[0].ChannelName)
	assert.Contains(t, summary.Failures[0].Error, "重试 3 次后仍失败")
	assert.Contains(t, summary.Failures[0].Error, "502 Bad Gateway")
	assert.False(t, summary.FailureDetailsTruncated)
	assert.Equal(t, [][2]int{{1, 2}, {2, 2}}, progress)
	assert.EqualValues(t, 8, failingRequestCount.Load())

	failedMonitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelRatioFetchStatusFailed, failedMonitor.LastFetchStatus)
	assert.NotEmpty(t, failedMonitor.LastFetchError)
	assert.NotZero(t, failedMonitor.LastFetchTime)
	assert.Equal(t, 4, failedMonitor.ConsecutiveFailures)

	monitor, err := model.GetChannelRatioMonitor(2)
	require.NoError(t, err)
	assert.InDelta(t, 1.25, monitor.Ratio, 1e-9)
	assert.Equal(t, "系统自动更新", monitor.UpdatedByUsername)
	assert.NotZero(t, monitor.UpdatedTime)
	assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
	assert.Empty(t, monitor.LastFetchError)
	assert.Zero(t, monitor.ConsecutiveFailures)
}

func TestRunChannelRatioMonitorTaskDoesNotRetrySub2APIAuthenticationFailure(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "2",
	})
	disableChannelMonitorSSRFProtection(t)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "/api/v1/groups/available", r.URL.Path)
		assert.Equal(t, "Bearer jwt-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"token expired","data":null}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "session bound", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.Sub2APIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.Sub2APIAuthToken,
		UpstreamAccessToken: "jwt-token",
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)
	assert.Zero(t, summary.Retried)
	require.Len(t, summary.Failures, 1)
	assert.Contains(t, summary.Failures[0].Error, "token expired")
	assert.NotContains(t, summary.Failures[0].Error, "重试")
	assert.EqualValues(t, 1, requestCount.Load())

	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, 1, monitor.ConsecutiveFailures)
}

func TestRunChannelRatioMonitorTaskUsesSub2APIAccountPassword(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)

	var loginRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/login":
			loginRequests.Add(1)
			var request struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			require.NoError(t, common.DecodeJson(r.Body, &request))
			assert.Equal(t, "monitor@example.com", request.Email)
			assert.Equal(t, "secret-password", request.Password)
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"auto-jwt","expires_in":3600}}`))
		case "/api/v1/groups/available":
			assert.Equal(t, "Bearer auto-jwt", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":[{"id":7,"name":"vip","rate_multiplier":1.25}]}`))
		case "/api/v1/groups/rates":
			assert.Equal(t, "Bearer auto-jwt", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"7":1.5}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "account upstream", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.Sub2APIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.Sub2APIAuthAccount,
		UpstreamAccount: "monitor@example.com", UpstreamPassword: "secret-password",
		UpstreamBalanceSyncDisabled: true,
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Updated)
	assert.Zero(t, summary.Failed)
	assert.EqualValues(t, 1, loginRequests.Load())

	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.InDelta(t, 1.5, monitor.Ratio, 1e-9)
	assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
}

func TestRunChannelRatioMonitorTaskRecoversAfterRetry(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:       "2",
		channelMonitorAutoDisableOnUpdateFailureOption: "true",
	})
	disableChannelMonitorSSRFProtection(t)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requestCount.Add(1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":1.25}}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "recovers", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Updated)
	assert.Zero(t, summary.Failed)
	assert.Equal(t, 1, summary.Retried)
	assert.Equal(t, 1, summary.RecoveredAfterRetry)
	assert.EqualValues(t, 3, requestCount.Load())

	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.InDelta(t, 1.25, monitor.Ratio, 1e-9)
	assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
	assert.Zero(t, monitor.ConsecutiveFailures)
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestSendChannelRatioMonitorNotificationEmailIncludesChannelRemarks(t *testing.T) {
	var content string
	err := sendChannelRatioMonitorNotificationEmail(
		"alerts@example.com",
		[]channelRatioMonitorEmailChange{{
			ChannelId: 1, ChannelName: "ratio", ChannelRemark: "<倍率备注 & 一>",
			UpstreamType: service.NewAPIUpstreamType, UpstreamGroup: "vip",
			OldRatio: 1, NewRatio: 1.2, ConversionFactor: 1, OldCostRatio: 1, NewCostRatio: 1.2,
		}},
		[]channelRatioMonitorBalanceWarning{{
			ChannelId: 2, ChannelName: "balance", ChannelRemark: "<余额备注 & 二>",
			UpstreamType: service.NewAPIUpstreamType, Balance: 5, Threshold: 10,
		}},
		[]channelRatioMonitorDisabledChannel{{
			ChannelId: 3, ChannelName: "disabled", ChannelRemark: "<禁用备注 & 三>", Reason: "测试禁用",
		}},
		[]channelRatioMonitorRemovedGroupMembership{{
			ChannelId: 4, ChannelName: "removed", ChannelRemark: "<移组备注 & 四>", Group: "vip",
		}},
		channelRatioMonitorTaskResult{
			Failed: 1,
			Failures: []channelRatioMonitorTaskFailure{{
				ChannelId: 5, ChannelName: "failed", ChannelRemark: "<失败备注 & 五>", Error: "测试失败",
			}},
		},
		nil,
		func(_ string, _ string, gotContent string) error {
			content = gotContent
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, 5, strings.Count(content, ">备注</th>"))
	for _, remark := range []string{
		"&lt;倍率备注 &amp; 一&gt;",
		"&lt;余额备注 &amp; 二&gt;",
		"&lt;禁用备注 &amp; 三&gt;",
		"&lt;移组备注 &amp; 四&gt;",
		"&lt;失败备注 &amp; 五&gt;",
	} {
		assert.Contains(t, content, remark)
	}
}

func TestRunChannelRatioMonitorTaskEmailsRatioChanges(t *testing.T) {
	tests := []struct {
		name            string
		emailEnabled    bool
		sendError       error
		wantEmailStatus string
		wantEmailCalls  int
	}{
		{name: "sent", emailEnabled: true, wantEmailStatus: "sent", wantEmailCalls: 1},
		{name: "send failure remains visible", emailEnabled: true, sendError: errors.New("smtp unavailable"), wantEmailStatus: "failed", wantEmailCalls: 1},
		{name: "disabled", emailEnabled: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			emailEnabled := "false"
			if test.emailEnabled {
				emailEnabled = "true"
			}
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorEmailNotificationOption: emailEnabled,
				channelMonitorNotificationEmailOption: "alerts@example.com",
			})
			disableChannelMonitorSSRFProtection(t)
			channelRemark := "<Primary remark & billing>"

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":1.25}}`))
			}))
			defer server.Close()

			require.NoError(t, db.Create(&model.Channel{
				Id:     1,
				Name:   "<Primary & API>",
				Key:    "secret",
				Group:  "vip",
				Remark: &channelRemark,
				Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId:        1,
				Ratio:            1,
				UpdatedTime:      1,
				UpstreamType:     service.NewAPIUpstreamType,
				UpstreamBaseURL:  server.URL,
				UpstreamGroup:    "vip",
				UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
			}).Error)

			var subject string
			var receiver string
			var content string
			emailCalls := 0
			summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, func(gotSubject string, gotReceiver string, gotContent string) error {
				emailCalls++
				subject = gotSubject
				receiver = gotReceiver
				content = gotContent
				return test.sendError
			})
			require.NoError(t, err)
			assert.Equal(t, 1, summary.Changed)
			assert.Equal(t, test.wantEmailStatus, summary.EmailStatus)
			assert.Equal(t, test.wantEmailCalls, emailCalls)
			if test.wantEmailCalls > 0 {
				assert.Contains(t, subject, "1 个渠道")
				assert.Equal(t, "alerts@example.com", receiver)
				assert.Contains(t, content, "&lt;Primary &amp; API&gt;")
				assert.Contains(t, content, "&lt;Primary remark &amp; billing&gt;")
				assert.Contains(t, content, "vip")
				assert.Contains(t, content, ">1<")
				assert.Contains(t, content, ">1.25<")
			}
			if test.sendError == nil || !test.emailEnabled {
				assert.Empty(t, summary.EmailError)
			} else {
				assert.Contains(t, summary.EmailError, test.sendError.Error())
			}

			monitor, err := model.GetChannelRatioMonitor(1)
			require.NoError(t, err)
			assert.InDelta(t, 1.25, monitor.Ratio, 1e-9)
		})
	}
}

func TestRunChannelRatioMonitorTaskEmailsRatioPolicyAutoDisable(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		"GroupRatio":                             `{"vip":1}`,
		channelMonitorAutoUpdateRetryCountOption: "0",
		channelMonitorEmailNotificationOption:    "true",
		channelMonitorNotificationEmailOption:    "alerts@example.com",
	})
	disableChannelMonitorSSRFProtection(t)
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":1.25}}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "<Disabled & API>", Key: "secret", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, Ratio: 1, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
		SingleChannelAction:         channelMonitorPolicyActionDisableChannel,
	}).Error)

	var subject string
	var receiver string
	var content string
	emailCalls := 0
	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, func(gotSubject string, gotReceiver string, gotContent string) error {
		emailCalls++
		subject = gotSubject
		receiver = gotReceiver
		content = gotContent
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, summary.Changed)
	assert.Equal(t, 1, summary.ChannelsDisabled)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)
	assert.Contains(t, subject, "1 个渠道自动禁用")
	assert.Equal(t, "alerts@example.com", receiver)
	assert.Contains(t, content, "渠道自动禁用")
	assert.Contains(t, content, "&lt;Disabled &amp; API&gt;（ID: 1）")
	assert.Contains(t, content, "成本倍率高于分组倍率")

	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}

func TestRunChannelRatioMonitorTaskEmailsRatioPolicyGroupRemoval(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		"GroupRatio":                             `{"vip":1,"backup":2}`,
		channelMonitorAutoUpdateRetryCountOption: "0",
		channelMonitorEmailNotificationOption:    "true",
		channelMonitorNotificationEmailOption:    "alerts@example.com",
	})
	disableChannelMonitorSSRFProtection(t)
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1,"backup":2}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":1.25}}`))
	}))
	defer server.Close()

	channels := []model.Channel{
		{Id: 1, Name: "<Removed & API>", Key: "secret", Group: "vip,backup", Models: "model-a", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "stable", Key: "secret", Group: "vip", Models: "model-b", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	for i := range channels {
		require.NoError(t, channels[i].AddAbilities(nil))
	}
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, Ratio: 1, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
		SingleChannelAction:         channelMonitorPolicyActionDisableChannel,
		MultipleChannelsAction:      channelMonitorPolicyActionRemoveFromGroup,
	}).Error)

	var subject string
	var content string
	emailCalls := 0
	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, func(gotSubject string, _ string, gotContent string) error {
		emailCalls++
		subject = gotSubject
		content = gotContent
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, summary.Changed)
	assert.Equal(t, 1, summary.GroupMembershipsRemoved)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)
	assert.Contains(t, subject, "1 个渠道移出分组")
	assert.Contains(t, content, "渠道移出分组")
	assert.Contains(t, content, "&lt;Removed &amp; API&gt;（ID: 1）")
	assert.Contains(t, content, ">vip<")

	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, "backup", channel.Group)
	var abilities []model.Ability
	require.NoError(t, db.Where("channel_id = ?", 1).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "backup", abilities[0].Group)
}

func TestRunChannelRatioMonitorTaskRefreshesBalanceAndDeduplicatesLowBalanceEmail(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "0",
		channelMonitorEmailNotificationOption:    "true",
		channelMonitorNotificationEmailOption:    "alerts@example.com",
	})
	disableChannelMonitorSSRFProtection(t)

	var upstreamQuota atomic.Int64
	upstreamQuota.Store(500)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			assert.Equal(t, "Bearer dashboard-token", r.Header.Get("Authorization"))
			assert.Equal(t, "42", r.Header.Get("New-Api-User"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
		case "/api/user/self":
			assert.Equal(t, "Bearer dashboard-token", r.Header.Get("Authorization"))
			assert.Equal(t, "42", r.Header.Get("New-Api-User"))
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"quota":%d}}`, upstreamQuota.Load())
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	threshold := 10.0
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "<Balance & API>", Key: "secret", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:               1,
		Ratio:                   1.25,
		UpdatedTime:             1,
		UpstreamType:            service.NewAPIUpstreamType,
		UpstreamBaseURL:         server.URL,
		UpstreamGroup:           "vip",
		UpstreamAuthType:        service.NewAPIUpstreamAuthUser,
		UpstreamUserId:          42,
		UpstreamAccessToken:     "dashboard-token",
		BalanceWarningThreshold: &threshold,
	}).Error)

	emailCalls := 0
	var emailSendError error
	var subject string
	var content string
	sendEmail := func(gotSubject string, receiver string, gotContent string) error {
		emailCalls++
		subject = gotSubject
		content = gotContent
		assert.Equal(t, "alerts@example.com", receiver)
		return emailSendError
	}

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Updated)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Equal(t, 1, summary.BalanceWarnings)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)
	assert.Contains(t, subject, "1 个余额预警")
	assert.Contains(t, content, "上游余额预警")
	assert.Contains(t, content, "&lt;Balance &amp; API&gt;")
	assert.Contains(t, content, ">5<")
	assert.Contains(t, content, ">10<")
	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	require.NotNil(t, monitor.UpstreamBalance)
	assert.Equal(t, 5.0, *monitor.UpstreamBalance)
	assert.True(t, monitor.BalanceAlertNotified)

	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Zero(t, summary.BalanceWarnings)
	assert.Empty(t, summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)

	upstreamQuota.Store(1500)
	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Zero(t, summary.BalanceWarnings)
	assert.Equal(t, 1, emailCalls)
	monitor, err = model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	require.NotNil(t, monitor.UpstreamBalance)
	assert.Equal(t, 15.0, *monitor.UpstreamBalance)
	assert.False(t, monitor.BalanceAlertNotified)

	upstreamQuota.Store(400)
	emailSendError = errors.New("smtp unavailable")
	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.BalanceWarnings)
	assert.Equal(t, "failed", summary.EmailStatus)
	assert.Contains(t, summary.EmailError, "smtp unavailable")
	assert.Equal(t, 2, emailCalls)
	monitor, err = model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.False(t, monitor.BalanceAlertNotified)

	emailSendError = nil
	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Equal(t, 1, summary.BalanceWarnings)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 3, emailCalls)
	monitor, err = model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.True(t, monitor.BalanceAlertNotified)
}

func TestRunChannelRatioMonitorTaskRecordsBalanceFailureWhenRatioSucceeds(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "0",
	})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
		case "/api/user/self":
			http.Error(w, "balance unavailable", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	remark := "倍率与余额"
	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "partial sync", Remark: &remark, Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, Ratio: 1, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
		UpstreamUserId: 42, UpstreamAccessToken: "test-token",
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Updated)
	assert.Equal(t, 1, summary.Changed)
	assert.Zero(t, summary.BalanceUpdated)
	assert.Equal(t, 1, summary.Failed)
	require.Len(t, summary.ChangedChannels, 1)
	require.Len(t, summary.Failures, 1)
	assert.Equal(t, model.ChannelRatioFailureAlertBalance, summary.Failures[0].Kind)
	assert.Equal(t, remark, summary.Failures[0].ChannelRemark)
	assert.Contains(t, summary.Failures[0].Error, "502 Bad Gateway")

	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.InDelta(t, 1.25, monitor.Ratio, 1e-9)
	assert.NotEmpty(t, monitor.LastBalanceError)
}

func TestRunChannelRatioMonitorTaskEmailsUpdateFailures(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "0",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "1",
		channelMonitorEmailNotificationOption:                 "true",
		channelMonitorNotificationEmailOption:                 "alerts@example.com",
	})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "<Failing & API>", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
	}).Error)

	var subject string
	var receiver string
	var content string
	emailCalls := 0
	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, func(gotSubject string, gotReceiver string, gotContent string) error {
		emailCalls++
		subject = gotSubject
		receiver = gotReceiver
		content = gotContent
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)
	assert.Contains(t, subject, "1 项更新失败")
	assert.Equal(t, "alerts@example.com", receiver)
	assert.Contains(t, content, "上游同步失败")
	assert.Contains(t, content, "&lt;Failing &amp; API&gt;")
	assert.Contains(t, content, "502 Bad Gateway")
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
}

func TestRunChannelRatioMonitorTaskEmailsOnlyAtFailureLimitAndRetriesUnacknowledgedAlert(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "0",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "2",
		channelMonitorEmailNotificationOption:                 "true",
		channelMonitorNotificationEmailOption:                 "alerts@example.com",
		channelMonitorEmailNotificationTypesOption:            `["upstream_sync_failed"]`,
	})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "failing", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
	}).Error)

	emailCalls := 0
	emailSendError := errors.New("smtp unavailable")
	sendEmail := func(subject string, receiver string, content string) error {
		emailCalls++
		assert.Contains(t, subject, "1 项更新失败")
		assert.Equal(t, "alerts@example.com", receiver)
		assert.Contains(t, content, "上游同步失败")
		return emailSendError
	}

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)
	assert.Empty(t, summary.EmailStatus)
	assert.Zero(t, emailCalls)
	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, 1, monitor.ConsecutiveFailures)
	assert.False(t, monitor.FetchFailureAlertNotified)

	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, "failed", summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)
	monitor, err = model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, 2, monitor.ConsecutiveFailures)
	assert.False(t, monitor.FetchFailureAlertNotified)

	emailSendError = nil
	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)
	assert.Zero(t, summary.Skipped)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 2, emailCalls)
	monitor, err = model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.True(t, monitor.FetchFailureAlertNotified)

	summary, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, sendEmail)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)
	assert.Zero(t, summary.Skipped)
	assert.Empty(t, summary.EmailStatus)
	assert.Equal(t, 2, emailCalls)
}

func TestRunChannelRatioMonitorTaskAutoDisablesChannelAfterUpdateFailure(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "0",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "1",
		channelMonitorAutoDisableOnUpdateFailureOption:        "true",
		channelMonitorEmailNotificationOption:                 "true",
		channelMonitorNotificationEmailOption:                 "alerts@example.com",
	})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "<Auto Disabled & API>", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
	}).Error)

	var subject string
	var content string
	emailCalls := 0
	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, func(gotSubject string, _ string, gotContent string) error {
		emailCalls++
		subject = gotSubject
		content = gotContent
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.ChannelsDisabled)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)
	assert.Contains(t, subject, "1 个渠道自动禁用")
	assert.Contains(t, subject, "1 项更新失败")
	assert.Contains(t, content, "渠道自动禁用")
	assert.Contains(t, content, "&lt;Auto Disabled &amp; API&gt;（ID: 1）")
	assert.Contains(t, content, "上游倍率或余额更新失败")

	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
}

func TestRunChannelRatioMonitorTaskEmailsGroupUpdateFailure(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		"GroupRatio":                          `{"vip":1}`,
		channelMonitorEmailNotificationOption: "true",
		channelMonitorNotificationEmailOption: "alerts@example.com",
	})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	disableChannelMonitorSSRFProtection(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":1.25}}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "stable", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, Ratio: 1.25, UpdatedTime: 1,
		UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		SingleChannelAction: channelMonitorPolicyActionUpdateGroupRatio,
	}).Error)
	require.NoError(t, db.Migrator().DropTable(&model.Option{}))

	var subject string
	var content string
	emailCalls := 0
	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, func(gotSubject string, _ string, gotContent string) error {
		emailCalls++
		subject = gotSubject
		content = gotContent
		return nil
	})

	require.Error(t, err)
	assert.True(t, summary.GroupUpdateFailed)
	assert.Equal(t, "sent", summary.EmailStatus)
	assert.Equal(t, 1, emailCalls)
	assert.Contains(t, subject, "1 项更新失败")
	assert.Contains(t, content, "分组倍率更新失败")
	assert.Contains(t, content, "失败原因")
}
