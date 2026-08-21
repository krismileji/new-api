package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
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
			GroupName: "default", ProbeModel: "gpt-4.1",
		}},
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: model.ChannelStatusProbeDisplayUnitMinute,
	}, 1_000)
	require.NoError(t, err)
	firstToken := 215.0
	for _, execution := range []model.ChannelGroupMonitorExecution{
		{
			RunId: "group-success", GroupName: "default", ProbeModel: "gpt-4.1",
			Result: model.ChannelGroupMonitorResultSuccess, FirstTokenMs: &firstToken,
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
	assert.Equal(t, channelGroupMonitorHealthRateLimited, items[0].Status)
	assert.Equal(t, model.ChannelGroupMonitorResultRateLimited, items[0].LatestResult)
	assert.EqualValues(t, 1, items[0].SuccessCount)
	assert.EqualValues(t, 2, items[0].CompletedCount)
	require.NotNil(t, items[0].SuccessRate)
	assert.InDelta(t, 50, *items[0].SuccessRate, 0.001)
	assert.Nil(t, items[0].LatestFirstTokenMs)
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
	assert.InDelta(t, 100, item["success_rate"], 0.001)
	for _, sensitiveField := range []string{
		"probe_model", "config_valid", "latest_result", "last_success_at", "last_failure_at",
		"consecutive_success", "consecutive_failure", "channel_id", "settled_cost_nano_cny", "error_message",
	} {
		assert.NotContains(t, item, sensitiveField)
	}
}
