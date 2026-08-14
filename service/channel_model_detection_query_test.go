package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionQueryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-query.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.ChannelModelDetectionGlobalConfig{},
		&model.ChannelModelDetectionConfig{},
		&model.ChannelModelDetectionTarget{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
		&model.ChannelDailyCost{},
	))
	t.Cleanup(func() {
		sqlDB, closeErr := db.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func seedChannelModelDetectionQueryChannel(t *testing.T, db *gorm.DB, channelID int, outcome string, createdAt int64) (model.ChannelModelDetectionRun, model.ChannelModelDetectionExecution) {
	t.Helper()
	remark := fmt.Sprintf("渠道 %d", channelID)
	require.NoError(t, db.Create(&model.Channel{
		Id: channelID, Name: fmt.Sprintf("channel-%d", channelID), Key: fmt.Sprintf("secret-key-%d", channelID),
		Group: "default,vip", Models: "gpt-5.6-sol,gpt-5.6-terra", Remark: &remark, Status: common.ChannelStatusEnabled,
	}).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: channelID, ScheduleEnabled: true, Revision: 1, CreatedAt: createdAt, UpdatedAt: createdAt}
	require.NoError(t, db.Create(&config).Error)
	target := model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: channelID, TargetKey: fmt.Sprintf("target-%d", channelID),
		RequestModel: "gpt-5.6-sol", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
		Enabled: true, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	require.NoError(t, db.Create(&target).Error)
	run := model.ChannelModelDetectionRun{
		RunId: fmt.Sprintf("run-%d", channelID), ChannelId: channelID, ConfigRevision: 1, GlobalConfigRevision: 1,
		Trigger: model.ChannelModelDetectionTriggerManual, Preset: model.ChannelModelDetectionPresetMedium,
		PresetSource: model.ChannelModelDetectionPresetSourceManualSelected, Status: model.ChannelModelDetectionRunStatusCompleted,
		TargetCount: 1, CompletedTargetCount: 1, PlannedLogicalRequests: 3, CompletedLogicalRequests: 3,
		QueuedAt: createdAt, StartedAt: createdAt + 1, FinishedAt: createdAt + 2, UpdatedAt: createdAt + 2, CreatedAt: createdAt,
	}
	require.NoError(t, db.Create(&run).Error)
	execution := model.ChannelModelDetectionExecution{
		RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: channelID,
		RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: run.Preset,
		Status: model.ChannelModelDetectionExecutionStatusCompleted, OutcomeCode: outcome,
		TitleCN: "检测完成", PlannedLogicalRequests: 3, CompletedLogicalRequests: 3,
		StartedAt: createdAt + 1, FinishedAt: createdAt + 2, CreatedAt: createdAt, UpdatedAt: createdAt + 2,
	}
	require.NoError(t, db.Create(&execution).Error)
	return run, execution
}

func TestChannelModelDetectionOverviewUsesFixedQueriesAndDoesNotExposeSecrets(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://10.20.30.40:18080/private?token=secret", ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		ScheduleEnabled: true, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)
	_, firstExecution := seedChannelModelDetectionQueryChannel(t, db, 101, "juice_pass_fingerprint_strong", 100)
	seedChannelModelDetectionQueryChannel(t, db, 102, "juice_insufficient_fingerprint_unclear", 200)
	seedChannelModelDetectionQueryChannel(t, db, 103, "future_detector_outcome", 300)

	settledQuota := int64(4)
	settledCost := int64(25_680_000)
	require.NoError(t, db.Create(&model.ChannelModelDetectionCostEvent{
		CostEventId: "cost-overview", RunId: "run-101", TargetId: firstExecution.TargetId, ExecutionId: firstExecution.Id, ChannelId: 101,
		RequestModel: firstExecution.RequestModel, ClaimedModel: firstExecution.ClaimedModel, Preset: firstExecution.Preset,
		DetectorRequestId: "detector-request", AttemptNo: 1, DispatchState: model.ChannelModelDetectionDispatchDispatched,
		SettlementStatus: model.ChannelModelDetectionSettlementSettled, UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative,
		UsageAvailable: true, SettledQuota: &settledQuota, CostBasisQuota: &settledQuota, SettledCostNanoCNY: &settledCost,
		CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI, CreatedAt: 101, UpdatedAt: 102,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelDailyCost{
		ChannelId: 101, DayStart: model.ChannelDailyCostDayStart(1_000),
		CostNanoCNY: settledCost, ModelDetectionCostNanoCNY: settledCost,
		SettledCount: 1, CreatedAt: 102, UpdatedAt: 102,
	}).Error)

	var queryCount atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:count_model_detection_overview", func(*gorm.DB) {
		queryCount.Add(1)
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove("test:count_model_detection_overview"))
	})

	response, err := GetChannelModelDetectionOverview(context.Background(), db, 1_000)
	require.NoError(t, err)
	initialQueryCount := queryCount.Load()
	assert.Positive(t, initialQueryCount)
	assert.LessOrEqual(t, initialQueryCount, int64(8))
	assert.Len(t, response.Channels, 3)
	assert.Equal(t, "http://10.***.***.***:***", response.Settings.DetectorURLMasked)
	assert.NotContains(t, response.Settings.DetectorURLMasked, "secret")
	assert.NotContains(t, response.Settings.DetectorURLMasked, "18080")
	assert.Equal(t, channelModelDetectionHealthHealthy, response.Channels[0].HealthStatus)
	assert.Equal(t, channelModelDetectionHealthAttention, response.Channels[1].HealthStatus)
	assert.Equal(t, channelModelDetectionHealthAttention, response.Channels[2].HealthStatus)
	require.NotNil(t, response.Channels[0].LatestRunCost)
	require.NotNil(t, response.Channels[0].LatestRunCost.SettledCostCNY)
	assert.Equal(t, "0.025680000", *response.Channels[0].LatestRunCost.SettledCostCNY)
	assert.InDelta(t, 0.02568, response.Channels[0].TodayModelDetectionCostCNY, 1e-12)

	encoded, err := common.Marshal(response)
	require.NoError(t, err)
	body := string(encoded)
	assert.NotContains(t, body, "secret-key")
	assert.NotContains(t, body, "token=secret")

	for channelID := 104; channelID <= 110; channelID++ {
		seedChannelModelDetectionQueryChannel(t, db, channelID, "juice_pass_fingerprint_strong", int64(channelID*10))
	}
	queryCount.Store(0)
	expanded, err := GetChannelModelDetectionOverview(context.Background(), db, 2_000)
	require.NoError(t, err)
	assert.Len(t, expanded.Channels, 10)
	assert.Equal(t, initialQueryCount, queryCount.Load())
}

func TestChannelModelDetectionHistoryValidatesFiltersAndKeepsUnknownCostsNull(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	run, execution := seedChannelModelDetectionQueryChannel(t, db, 201, "future_detector_outcome", 500)
	require.NoError(t, db.Create(&model.ChannelModelDetectionCostEvent{
		CostEventId: "cost-history-unknown", RunId: run.RunId, TargetId: execution.TargetId, ExecutionId: execution.Id, ChannelId: 201,
		RequestModel: execution.RequestModel, ClaimedModel: execution.ClaimedModel, Preset: execution.Preset,
		DetectorRequestId: "detector-request", AttemptNo: 1, DispatchState: model.ChannelModelDetectionDispatchDispatched,
		SettlementStatus: model.ChannelModelDetectionSettlementUnresolved, UsageSource: model.ChannelModelDetectionUsageUnavailable,
		EstimatedQuota: 0, EstimatedCostNanoCNY: nil, CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI,
		CreatedAt: 501, UpdatedAt: 502,
	}).Error)

	tests := []ChannelModelDetectionRunHistoryQuery{
		{Page: 0, PageSize: 20},
		{Page: 1, PageSize: 0},
		{Page: 1, PageSize: 101},
		{Page: 1, PageSize: 20, Trigger: "automatic"},
		{Page: 1, PageSize: 20, Status: "done"},
		{Page: 1, PageSize: 20, Outcome: "future_detector_outcome"},
	}
	for _, input := range tests {
		_, err := ListChannelModelDetectionRuns(context.Background(), db, 201, input)
		assert.ErrorIs(t, err, ErrChannelModelDetectionInvalidHistoryQuery)
	}

	response, err := ListChannelModelDetectionRuns(context.Background(), db, 201, ChannelModelDetectionRunHistoryQuery{
		Page: 1, PageSize: 20, Trigger: model.ChannelModelDetectionTriggerManual,
		Status: model.ChannelModelDetectionRunStatusCompleted, Model: "gpt-5.6-sol",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), response.Total)
	require.Len(t, response.Items, 1)
	assert.Nil(t, response.Items[0].Cost.EstimatedQuota)
	assert.Nil(t, response.Items[0].Cost.EstimatedCostNanoCNY)
	assert.Nil(t, response.Items[0].Cost.EstimatedCostCNY)
	assert.Nil(t, response.Items[0].Cost.UnresolvedCostNanoCNY)
	assert.Nil(t, response.Items[0].Cost.UnresolvedCostCNY)
	assert.Equal(t, int64(1), response.Items[0].Cost.UnresolvedCostUnknownCount)
	assert.Equal(t, ChannelModelDetectionCostStatusUnresolved, response.Items[0].Cost.Status)

	knownFilter, err := ListChannelModelDetectionRuns(context.Background(), db, 201, ChannelModelDetectionRunHistoryQuery{
		Page: 1, PageSize: 20, Outcome: "juice_pass_fingerprint_strong",
	})
	require.NoError(t, err)
	assert.Zero(t, knownFilter.Total)
}

func TestChannelModelDetectionReportAPIPreservesUnknownFieldsAndEnforcesSize(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	run, execution := seedChannelModelDetectionQueryChannel(t, db, 301, "future_detector_outcome", 700)
	report := map[string]any{
		"outcome_code":  "future_detector_outcome",
		"future_report": map[string]any{"proof": float64(1)},
		"api_key":       "report-secret-key",
		"X-API-Key":     "report-header-secret",
		"session_token": "report-session-token",
		"detector_url":  "http://127.0.0.1:18080/private",
	}
	reportData, err := common.Marshal(report)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).
		Updates(map[string]any{
			"report_json": string(reportData), "official_config_json": `{"api_key":"config-secret"}`,
			"detector_url_snapshot": "http://127.0.0.1:18080/private", "official_session_id": "official-session",
		}).Error)

	detail, err := GetChannelModelDetectionRunDetail(context.Background(), db, run.RunId)
	require.NoError(t, err)
	require.Len(t, detail.Executions, 1)
	assert.Equal(t, "future_detector_outcome", detail.Executions[0].OutcomeCode)
	reportMap, ok := detail.Executions[0].Report.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "[已隐藏]", reportMap["api_key"])
	assert.Equal(t, "[已隐藏]", reportMap["X-API-Key"])
	assert.Equal(t, "[已隐藏]", reportMap["session_token"])
	assert.Equal(t, "[已隐藏]", reportMap["detector_url"])
	assert.Equal(t, map[string]any{"proof": float64(1)}, reportMap["future_report"])
	encoded, err := common.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "report-secret-key")
	assert.NotContains(t, string(encoded), "config-secret")
	assert.NotContains(t, string(encoded), "127.0.0.1:18080")

	boundaryReport := `{"payload":"` + strings.Repeat("a", model.ChannelModelDetectionMaxReportBytes-len(`{"payload":""}`)) + `"}`
	require.Len(t, boundaryReport, model.ChannelModelDetectionMaxReportBytes)
	require.NoError(t, db.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).Update("report_json", boundaryReport).Error)
	_, err = GetChannelModelDetectionRunDetail(context.Background(), db, run.RunId)
	require.NoError(t, err)

	overLimit := boundaryReport + " "
	require.NoError(t, db.Model(&model.ChannelModelDetectionExecution{}).Where("id = ?", execution.Id).Update("report_json", overLimit).Error)
	_, err = GetChannelModelDetectionRunDetail(context.Background(), db, run.RunId)
	assert.True(t, errors.Is(err, ErrChannelModelDetectionReportTooLarge))
}
