package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelGroupMonitorProbeContextUsesConfiguredGroup(t *testing.T) {
	source := withChannelGroupMonitorTestContext(context.Background(), "vip")
	target, _ := gin.CreateTestContext(httptest.NewRecorder())
	applyChannelGroupMonitorTestContext(source, target)

	assert.True(t, isChannelGroupMonitorTest(source))
	assert.Equal(t, "vip", common.GetContextKeyString(target, constant.ContextKeyUsingGroup))
}

func TestChannelGroupMonitorAttemptLogInfoCapturesRetryChain(t *testing.T) {
	other := make(map[string]interface{})
	appendChannelGroupMonitorAttemptLogInfo(other, channelGroupMonitorAttemptLogInfo{
		RunId: "group-run-1", Attempt: 2, RetryIndex: 1,
		AttemptedChannelIds: []int{11, 12},
	}, model.ChannelStatusProbeResultUpstreamFailure)

	assert.Equal(t, true, other[model.ChannelMonitorGroupProbeLogKey])
	assert.Equal(t, "group-run-1", other["channel_monitor_probe_run_id"])
	assert.Equal(t, 2, other["channel_monitor_probe_attempt"])
	assert.Equal(t, 1, other["channel_monitor_probe_retry_index"])
	assert.Equal(t, []int{11, 12}, other["channel_monitor_probe_attempted_channels"])
	assert.Equal(t, model.ChannelStatusProbeResultUpstreamFailure, other["channel_monitor_probe_result"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.NotContains(t, adminInfo, "use_channel")
}

func TestApplyChannelGroupMonitorFinalOutcomePreservesExplicitLocalFailure(t *testing.T) {
	execution := model.ChannelGroupMonitorExecution{
		Result:       model.ChannelGroupMonitorResultLocalFailure,
		ErrorCode:    "route_selection_failed",
		ErrorMessage: "路由选择失败",
	}
	outcome := &channelStatusProbeOutcome{
		Result:       model.ChannelStatusProbeResultUpstreamFailure,
		ErrorCode:    "upstream_failure",
		ErrorMessage: "上游失败",
	}

	applyChannelGroupMonitorFinalOutcome(&execution, outcome)

	assert.Equal(t, model.ChannelGroupMonitorResultLocalFailure, execution.Result)
	assert.Equal(t, "route_selection_failed", execution.ErrorCode)
	assert.Equal(t, "路由选择失败", execution.ErrorMessage)
}

func TestApplyChannelGroupMonitorFinalOutcomeAppliesUpstreamOutcomeWhenUnset(t *testing.T) {
	execution := model.ChannelGroupMonitorExecution{}
	outcome := &channelStatusProbeOutcome{
		Result:       model.ChannelStatusProbeResultRateLimited,
		ErrorCode:    "rate_limited",
		ErrorMessage: "上游限流",
	}

	applyChannelGroupMonitorFinalOutcome(&execution, outcome)

	assert.Equal(t, model.ChannelGroupMonitorResultRateLimited, execution.Result)
	assert.Equal(t, "rate_limited", execution.ErrorCode)
	assert.Equal(t, "上游限流", execution.ErrorMessage)
}

func TestGroupMonitorExecutionFromOutcomePersistsPerformanceMetrics(t *testing.T) {
	responseTime := 1_480.0
	firstToken := 220.0
	tps := 38.5
	execution := model.ChannelGroupMonitorExecution{}
	groupMonitorExecutionFromOutcome(&execution, channelStatusProbeOutcome{
		Result:       model.ChannelStatusProbeResultSuccess,
		StartedAt:    100,
		FinishedAt:   102,
		DurationMs:   &responseTime,
		TestExecuted: true,
		ProbeResult: testResult{
			requestDispatched:         true,
			firstResponseMilliseconds: &firstToken,
			tokensPerSecond:           &tps,
		},
	}, 901)

	require.NotNil(t, execution.ResponseTimeMs)
	require.NotNil(t, execution.FirstTokenMs)
	require.NotNil(t, execution.TPS)
	assert.InDelta(t, responseTime, *execution.ResponseTimeMs, 0.001)
	assert.InDelta(t, firstToken, *execution.FirstTokenMs, 0.001)
	assert.InDelta(t, tps, *execution.TPS, 0.001)
}

func TestNormalizeChannelGroupMonitorGroupsValidatesCustomDisplayInitial(t *testing.T) {
	candidates := map[string][]string{"default": {"gpt-4.1"}}

	normalized, err := normalizeChannelGroupMonitorGroups([]model.ChannelGroupMonitorGroup{{
		GroupName: " default ", ProbeModel: " gpt-4.1 ", DisplayInitial: " 组 ",
	}}, candidates)
	require.NoError(t, err)
	require.Len(t, normalized, 1)
	assert.Equal(t, "组", normalized[0].DisplayInitial)

	_, err = normalizeChannelGroupMonitorGroups([]model.ChannelGroupMonitorGroup{{
		GroupName: "default", ProbeModel: "gpt-4.1", DisplayInitial: "AB",
	}}, candidates)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "展示字只能配置一个字符"))
}

func TestBuildChannelGroupMonitorItemsUsesLatestResultAndDisplayWindow(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelGroupMonitorConfig{},
		&model.ChannelGroupMonitorState{},
		&model.ChannelGroupMonitorExecution{},
	))
	require.NoError(t, db.Create(&model.Channel{
		Id: 901, Name: "分组监控渠道", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-4.1",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "gpt-4.1", ChannelId: 901, Enabled: true,
	}).Error)

	config, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled: true,
		Groups: []model.ChannelGroupMonitorGroup{{
			GroupName: "default", ProbeModel: "gpt-4.1", DisplayInitial: "D",
		}},
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
	}, 1_000)
	require.NoError(t, err)
	firstToken := 215.0
	responseTime := 1_680.0
	tps := 44.0
	for _, execution := range []model.ChannelGroupMonitorExecution{
		{
			RunId: "group-success", GroupName: "default", ProbeModel: "gpt-4.1",
			Result: model.ChannelGroupMonitorResultSuccess, ResponseTimeMs: &responseTime,
			FirstTokenMs: &firstToken, TPS: &tps,
			FinishedAt: 980, CreatedAt: 980,
		},
		{
			RunId: "group-rate-limited", GroupName: "default", ProbeModel: "gpt-4.1",
			Result: model.ChannelGroupMonitorResultRateLimited, FinishedAt: 990, CreatedAt: 990,
		},
		{
			RunId: "group-skipped", GroupName: "default", ProbeModel: "gpt-4.1",
			Result: model.ChannelGroupMonitorResultSkipped, FinishedAt: 995, CreatedAt: 995,
		},
	} {
		created, saveErr := model.SaveChannelGroupMonitorExecution(&execution)
		require.NoError(t, saveErr)
		assert.True(t, created)
	}

	items, err := buildChannelGroupMonitorItems(config, false, map[string]string{"default": "默认分组"}, 1_000)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "D", items[0].Initial)
	assert.Equal(t, channelGroupMonitorHealthRateLimited, items[0].Status)
	assert.Equal(t, model.ChannelGroupMonitorResultRateLimited, items[0].LatestResult)
	assert.EqualValues(t, 1, items[0].SuccessCount)
	assert.EqualValues(t, 2, items[0].CompletedCount)
	require.NotNil(t, items[0].SuccessRate)
	assert.InDelta(t, 50, *items[0].SuccessRate, 0.001)
	assert.Nil(t, items[0].LatestFirstTokenMs)
	require.Len(t, items[0].RecentWindow, 60)
	var populatedBucket *channelGroupMonitorBucketResponse
	for index := range items[0].RecentWindow {
		if items[0].RecentWindow[index].Success > 0 || items[0].RecentWindow[index].RateLimited > 0 {
			populatedBucket = &items[0].RecentWindow[index]
			break
		}
	}
	require.NotNil(t, populatedBucket)
	assert.Equal(t, 1, populatedBucket.Success)
	assert.Equal(t, 1, populatedBucket.RateLimited)
	assert.Equal(t, model.ChannelGroupMonitorResultRateLimited, populatedBucket.Result)
	assert.InDelta(t, responseTime, populatedBucket.ResponseTimeTotalMs, 0.001)
	assert.EqualValues(t, 1, populatedBucket.ResponseTimeSampleCount)
	assert.InDelta(t, tps, populatedBucket.TPSTotal, 0.001)
	assert.EqualValues(t, 1, populatedBucket.TPSSampleCount)
	assert.InDelta(t, firstToken, populatedBucket.FirstTokenTotalMs, 0.001)
	assert.EqualValues(t, 1, populatedBucket.FirstTokenSampleCount)
	// The window keeps aggregate counters for the summary, but hover details
	// must identify the latest execution in the time cell.  The skipped probe
	// finished after the successful and rate-limited probes above.
	assert.Equal(t, model.ChannelGroupMonitorResultSkipped, populatedBucket.LatestResult)
	assert.Nil(t, populatedBucket.LatestFirstTokenMs)
	assert.Nil(t, populatedBucket.LatestTPS)
	assert.Nil(t, populatedBucket.LatestResponseTimeMs)
}

func TestChannelGroupMonitorTimeoutUsesYellowHealthAndWindowResult(t *testing.T) {
	config := model.ChannelGroupMonitorConfig{Enabled: true, IntervalSeconds: 60}
	state := model.ChannelGroupMonitorState{
		Result: model.ChannelGroupMonitorResultTimeout, FinishedAt: 1_000,
	}
	assert.Equal(t, channelGroupMonitorHealthStale, channelGroupMonitorHealth(config, &state, 1_001))

	window := mergeChannelGroupMonitorRecentWindow([]model.ChannelGroupMonitorExecution{{
		GroupName: "default", Result: model.ChannelGroupMonitorResultTimeout, FinishedAt: 1_000,
	}}, 1_000, 1, model.ChannelStatusProbeDisplayUnitMinute)
	buckets := window["default"]
	require.Len(t, buckets, 1)
	assert.Equal(t, 1, buckets[0].Timeout)
	assert.Equal(t, model.ChannelGroupMonitorResultTimeout, buckets[0].Result)
}

func TestMergeChannelGroupMonitorRecentWindowBucketsTimeoutByStartTime(t *testing.T) {
	window := mergeChannelGroupMonitorRecentWindow([]model.ChannelGroupMonitorExecution{{
		GroupName: "default", Result: model.ChannelGroupMonitorResultTimeout,
		StartedAt: 1_000, FinishedAt: 1_060,
	}}, 1_060, 2, model.ChannelStatusProbeDisplayUnitMinute)

	buckets := window["default"]
	require.Len(t, buckets, 2)
	assert.EqualValues(t, 1, buckets[0].Timeout)
	assert.Zero(t, buckets[1].Timeout)
	assert.Equal(t, model.ChannelGroupMonitorResultTimeout, buckets[0].LatestResult)
}

func TestMergeChannelGroupMonitorRecentWindowUsesLatestExecutionForHoverMetrics(t *testing.T) {
	firstToken := 1_040.0
	tps := 10.0
	responseTime := 31_040.0
	window := mergeChannelGroupMonitorRecentWindow([]model.ChannelGroupMonitorExecution{
		{
			Id: 1, GroupName: "default", Result: model.ChannelGroupMonitorResultSuccess,
			FinishedAt: 980, FirstTokenMs: &firstToken, TPS: &tps, ResponseTimeMs: &responseTime,
		},
		{
			Id: 2, GroupName: "default", Result: model.ChannelGroupMonitorResultTimeout,
			FinishedAt: 1_000,
		},
	}, 1_000, 1, model.ChannelStatusProbeDisplayUnitMinute)

	buckets := window["default"]
	require.Len(t, buckets, 1)
	assert.Equal(t, model.ChannelGroupMonitorResultTimeout, buckets[0].Result)
	assert.Equal(t, model.ChannelGroupMonitorResultTimeout, buckets[0].LatestResult)
	assert.Nil(t, buckets[0].LatestFirstTokenMs)
	assert.Nil(t, buckets[0].LatestTPS)
	assert.Nil(t, buckets[0].LatestResponseTimeMs)
}

func TestChannelGroupMonitorCandidatesRequireEnabledAbility(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 905, Name: "候选模型验证渠道", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Group: "default", Models: "gpt-4.1",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "gpt-4.1", ChannelId: 905, Enabled: false,
	}).Error)

	candidates, err := getChannelGroupMonitorCandidateModels(true)
	require.NoError(t, err)
	assert.NotContains(t, candidates, "default")

	require.NoError(t, db.Model(&model.Ability{}).
		Where(&model.Ability{Group: "default", Model: "gpt-4.1", ChannelId: 905}).
		Update("enabled", true).Error)
	candidates, err = getChannelGroupMonitorCandidateModels(true)
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-4.1"}, candidates["default"])
}

func TestUpdateChannelGroupMonitorSettingsKeepsDisabledConfiguredModel(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelGroupMonitorConfig{},
		&model.ChannelGroupMonitorState{},
		&model.ChannelGroupMonitorExecution{},
	))
	require.NoError(t, db.Create(&model.Channel{
		Id: 906, Name: "已停用的分组监控渠道", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusAutoDisabled, Group: "特价", Models: "gpt-5.6-sol",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "特价", Model: "gpt-5.6-sol", ChannelId: 906, Enabled: true,
	}).Error)
	created, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled:         true,
		Groups:          []model.ChannelGroupMonitorGroup{{GroupName: "特价", ProbeModel: "gpt-5.6-sol"}},
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
	}, 1_000)
	require.NoError(t, err)

	body, err := common.Marshal(map[string]any{
		"enabled": true,
		"groups": []map[string]string{{
			"group_name": "特价", "probe_model": "gpt-5.6-sol",
		}},
		"interval_seconds": 300,
		"display_value":    60,
		"display_unit":     model.ChannelStatusProbeDisplayUnitMinute,
		"revision":         created.Revision,
	})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/channel_monitor/group_monitor/settings", bytes.NewReader(body))
	UpdateChannelGroupMonitorSettings(c)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
}

func TestGetPricingGroupMonitorOnlyReturnsVisiblePublicFields(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelGroupMonitorConfig{},
		&model.ChannelGroupMonitorState{},
		&model.ChannelGroupMonitorExecution{},
	))
	require.NoError(t, db.Create(&[]model.Channel{
		{
			Id: 902, Name: "私有分组监控渠道", Type: constant.ChannelTypeOpenAI,
			Status: common.ChannelStatusEnabled, Group: "private", Models: "gpt-4.1",
		},
		{
			Id: 903, Name: "受限分组监控渠道", Type: constant.ChannelTypeOpenAI,
			Status: common.ChannelStatusEnabled, Group: "restricted", Models: "gpt-4.1",
		},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "private", Model: "gpt-4.1", ChannelId: 902, Enabled: true},
		{Group: "restricted", Model: "gpt-4.1", ChannelId: 903, Enabled: true},
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id: 904, Username: "pricing-group-monitor-user", Password: "password",
		Group: "private", Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
	}).Error)

	now := common.GetTimestamp()
	_, err := model.SaveChannelGroupMonitorConfig(model.ChannelGroupMonitorConfigInput{
		Enabled: true,
		Groups: []model.ChannelGroupMonitorGroup{
			{GroupName: "private", ProbeModel: "gpt-4.1"},
			{GroupName: "restricted", ProbeModel: "gpt-4.1"},
		},
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
	}, now)
	require.NoError(t, err)
	firstToken := 215.0
	for _, execution := range []model.ChannelGroupMonitorExecution{
		{
			RunId: "private-pricing-probe", GroupName: "private", ProbeModel: "gpt-4.1",
			ChannelId: 902, Result: model.ChannelGroupMonitorResultSuccess, FirstTokenMs: &firstToken,
			ErrorMessage: "仅管理员可见的诊断信息", FinishedAt: now, CreatedAt: now,
		},
		{
			RunId: "restricted-pricing-probe", GroupName: "restricted", ProbeModel: "gpt-4.1",
			ChannelId: 903, Result: model.ChannelGroupMonitorResultSuccess,
			FinishedAt: now, CreatedAt: now,
		},
	} {
		created, saveErr := model.SaveChannelGroupMonitorExecution(&execution)
		require.NoError(t, saveErr)
		require.True(t, created)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/pricing/group-monitor", nil)
	context.Set("id", 904)
	context.Set("role", common.RoleCommonUser)
	GetPricingGroupMonitor(context)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data.Items, 1)
	item := payload.Data.Items[0]
	assert.Equal(t, "private", item["group"])
	assert.Equal(t, "gpt-4.1", item["probe_model"])
	assert.InDelta(t, 100, item["success_rate"], 0.001)
	for _, sensitiveField := range []string{
		"config_valid", "latest_result", "last_success_at", "last_failure_at",
		"consecutive_success", "consecutive_failure", "channel_id", "settled_cost_nano_cny", "error_message",
	} {
		assert.NotContains(t, item, sensitiveField)
	}
}
