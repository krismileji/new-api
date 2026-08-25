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
		&model.ChannelRatioMonitor{},
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
		DetectorURL: "http://10.20.30.40:18080/private", ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		ScheduleEnabled: true, IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)
	_, firstExecution := seedChannelModelDetectionQueryChannel(t, db, 101, "juice_pass_fingerprint_strong", 100)
	_, secondExecution := seedChannelModelDetectionQueryChannel(t, db, 102, "juice_insufficient_fingerprint_unclear", 200)
	seedChannelModelDetectionQueryChannel(t, db, 103, "future_detector_outcome", 300)

	settledQuota := int64(4)
	settledCost := int64(25_680_000)
	require.NoError(t, db.Create(&model.ChannelModelDetectionCostEvent{
		CostEventId: "cost-overview", RunId: "run-101", TargetId: firstExecution.TargetId, ExecutionId: firstExecution.Id, ChannelId: 101,
		RequestModel: firstExecution.RequestModel, ClaimedModel: firstExecution.ClaimedModel, Preset: firstExecution.Preset,
		DetectorRequestId: "detector-request", AttemptNo: 1, DispatchState: model.ChannelModelDetectionDispatchDispatched,
		SettlementStatus: model.ChannelModelDetectionSettlementSettled, UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative,
		UsageAvailable: true, SettledQuota: &settledQuota, CostBasisQuota: &settledQuota, SettledCostNanoCNY: &settledCost,
		CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI, CreatedAt: 101, SettledAt: 102, UpdatedAt: 102,
	}).Error)
	unresolvedCost := int64(759_054_000)
	require.NoError(t, db.Create(&model.ChannelModelDetectionCostEvent{
		CostEventId: "cost-overview-unresolved", RunId: "run-102", TargetId: secondExecution.TargetId, ExecutionId: secondExecution.Id, ChannelId: 102,
		RequestModel: secondExecution.RequestModel, ClaimedModel: secondExecution.ClaimedModel, Preset: secondExecution.Preset,
		DetectorRequestId: "detector-request-unresolved", AttemptNo: 1, DispatchState: model.ChannelModelDetectionDispatchDispatched,
		SettlementStatus: model.ChannelModelDetectionSettlementUnresolved, UsageSource: model.ChannelModelDetectionUsageUnavailable,
		EstimatedQuota: 1, EstimatedCostNanoCNY: &unresolvedCost,
		CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI, CreatedAt: 201, UpdatedAt: 202,
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
	assert.LessOrEqual(t, initialQueryCount, int64(9))
	assert.Len(t, response.Channels, 3)
	assert.Equal(t, "http://10.20.30.40:18080/private", response.Settings.DetectorURLMasked)
	assert.Contains(t, response.Settings.DetectorURLMasked, "18080")
	assert.Contains(t, response.Settings.DetectorURLMasked, "/private")
	assert.Equal(t, channelModelDetectionHealthHealthy, response.Channels[0].HealthStatus)
	assert.Equal(t, channelModelDetectionHealthAttention, response.Channels[1].HealthStatus)
	assert.Equal(t, channelModelDetectionHealthAttention, response.Channels[2].HealthStatus)
	require.NotNil(t, response.Channels[0].LatestRunCost)
	require.NotNil(t, response.Channels[0].LatestRunCost.SettledCostCNY)
	assert.Equal(t, "0.025680000", *response.Channels[0].LatestRunCost.SettledCostCNY)
	assert.InDelta(t, 0.02568, response.Channels[0].TodayModelDetectionCostCNY, 1e-12)
	require.NotNil(t, response.Channels[0].TodayModelDetectionCost)
	require.NotNil(t, response.Channels[0].TodayModelDetectionCost.SettledCostCNY)
	assert.Equal(t, "0.025680000", *response.Channels[0].TodayModelDetectionCost.SettledCostCNY)
	require.NotNil(t, response.Channels[1].TodayModelDetectionCost)
	assert.Nil(t, response.Channels[1].TodayModelDetectionCost.UnresolvedCostCNY)
	assert.EqualValues(t, 1, response.Channels[1].TodayModelDetectionCost.UnresolvedCostUnknownCount)
	assert.EqualValues(t, 1, response.Channels[1].TodayModelDetectionCost.UnresolvedRequestCount)

	encoded, err := common.Marshal(response)
	require.NoError(t, err)
	body := string(encoded)
	assert.NotContains(t, body, "secret-key")

	for channelID := 104; channelID <= 110; channelID++ {
		seedChannelModelDetectionQueryChannel(t, db, channelID, "juice_pass_fingerprint_strong", int64(channelID*10))
	}
	queryCount.Store(0)
	expanded, err := GetChannelModelDetectionOverview(context.Background(), db, 2_000)
	require.NoError(t, err)
	assert.Len(t, expanded.Channels, 10)
	assert.Equal(t, initialQueryCount, queryCount.Load())
}

func TestChannelModelDetectionOverviewTreatsLegacyNullLogicalFieldsAsPhysical(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	olderRun, execution := seedChannelModelDetectionQueryChannel(t, db, 104, "success", 100)
	newerRun := olderRun
	newerRun.Id = 0
	newerRun.RunId = "legacy-null-latest"
	newerRun.CreatedAt = 200
	newerRun.QueuedAt = 200
	newerRun.FinishedAt = 201
	newerRun.UpdatedAt = 201
	require.NoError(t, db.Create(&newerRun).Error)
	require.NoError(t, db.Exec("UPDATE channel_model_detection_runs SET logical_channel_id = NULL, logical_revision = NULL WHERE run_id = ?", newerRun.RunId).Error)
	settledQuota, settledCost := int64(2), int64(200)
	require.NoError(t, db.Create(&model.ChannelModelDetectionCostEvent{
		CostEventId: "legacy-null-cost", RunId: newerRun.RunId, TargetId: execution.TargetId, ExecutionId: execution.Id, ChannelId: 104,
		RequestModel: execution.RequestModel, ClaimedModel: execution.ClaimedModel, Preset: execution.Preset,
		DetectorRequestId: "legacy-null-request", AttemptNo: 1, DispatchState: model.ChannelModelDetectionDispatchDispatched,
		SettlementStatus: model.ChannelModelDetectionSettlementSettled, UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative,
		UsageAvailable: true, SettledQuota: &settledQuota, CostBasisQuota: &settledQuota, SettledCostNanoCNY: &settledCost,
		CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI, CreatedAt: 200, SettledAt: 201, UpdatedAt: 201,
	}).Error)

	overview, err := GetChannelModelDetectionOverview(context.Background(), db, 300)
	require.NoError(t, err)
	require.Len(t, overview.Channels, 1)
	require.NotNil(t, overview.Channels[0].LatestRunCost)
	require.NotNil(t, overview.Channels[0].LatestRunCost.SettledCostCNY)
	assert.Equal(t, "0.000000200", *overview.Channels[0].LatestRunCost.SettledCostCNY)
}

func TestChannelModelDetectionOverviewTreatsStrongFingerprintConflictAsUnhealthy(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://127.0.0.1:18080", ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		IntervalHours: 24, ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)
	_, execution := seedChannelModelDetectionQueryChannel(t, db, 111, "juice_pass_fingerprint_strong", 100)
	require.NoError(t, execution.SetReport(map[string]any{
		"claimed_model":              model.ChannelModelDetectionClaimedModelSol,
		"outcome_code":               "juice_pass_fingerprint_strong",
		"fingerprint_verdict_state":  "strong_match",
		"fingerprint_model":          model.ChannelModelDetectionClaimedModelLuna,
		"fingerprint_claim_mismatch": true,
	}))
	require.NoError(t, db.Model(&execution).Update("report_json", execution.ReportJSON).Error)

	response, err := GetChannelModelDetectionOverview(context.Background(), db, 1_000)
	require.NoError(t, err)
	require.Len(t, response.Channels, 1)
	assert.Equal(t, channelModelDetectionHealthUnhealthy, response.Channels[0].HealthStatus)
	require.Len(t, response.Channels[0].Targets, 1)
	require.NotNil(t, response.Channels[0].Targets[0].Latest)
	assert.Equal(t, model.ChannelModelDetectionClaimedModelLuna, response.Channels[0].Targets[0].Latest.FingerprintModel)
	assert.True(t, response.Channels[0].Targets[0].Latest.FingerprintClaimMismatch)
}

func TestChannelModelDetectionExecutionSummaryIncludesScheduleTimeoutCode(t *testing.T) {
	summary := channelModelDetectionExecutionSummary(model.ChannelModelDetectionExecution{
		Status:    model.ChannelModelDetectionExecutionStatusCanceled,
		ErrorCode: model.ChannelModelDetectionErrorScheduleTimeout,
	}, "", "", ChannelModelDetectionCostAggregate{})

	assert.Equal(t, model.ChannelModelDetectionErrorScheduleTimeout, summary.ErrorCode)
}

func TestChannelModelDetectionOverviewBuildsConfiguredDisplayBuckets(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://127.0.0.1:18080", ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		IntervalHours: 24, DisplayValue: 4, DisplayUnit: model.ChannelModelDetectionDisplayUnitMinute,
		ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)
	_, initialExecution := seedChannelModelDetectionQueryChannel(t, db, 115, "juice_pass_fingerprint_strong", 700)

	tests := []struct {
		sequence  int
		startedAt int64
		status    string
		outcome   string
		title     string
		conflict  bool
	}{
		{sequence: 1, startedAt: 790, status: model.ChannelModelDetectionExecutionStatusCompleted, outcome: "juice_pass_fingerprint_strong", title: "Juice 通过；指纹强烈指向 Sol"},
		{sequence: 2, startedAt: 850, status: model.ChannelModelDetectionExecutionStatusCompleted, outcome: "juice_pass_fingerprint_strong", title: "Juice 通过；指纹强烈指向 Luna", conflict: true},
		{sequence: 3, startedAt: 870, status: model.ChannelModelDetectionExecutionStatusFailed, title: "执行失败"},
		{sequence: 4, startedAt: 905, status: model.ChannelModelDetectionExecutionStatusCompleted, outcome: "juice_insufficient_fingerprint_unclear", title: "证据不足；指纹不明确"},
	}
	for _, test := range tests {
		run := model.ChannelModelDetectionRun{
			RunId: fmt.Sprintf("run-115-window-%02d", test.sequence), ChannelId: 115,
			ConfigRevision: 1, GlobalConfigRevision: 1,
			Trigger: model.ChannelModelDetectionTriggerScheduled, Preset: model.ChannelModelDetectionPresetMedium,
			PresetSource: model.ChannelModelDetectionPresetSourceScheduledDefault,
			Status:       model.ChannelModelDetectionRunStatusCompleted,
			TargetCount:  1, CompletedTargetCount: 1, PlannedLogicalRequests: 3, CompletedLogicalRequests: 3,
			QueuedAt: test.startedAt - 1, StartedAt: test.startedAt, FinishedAt: test.startedAt + 2,
			UpdatedAt: test.startedAt + 2, CreatedAt: test.startedAt - 1,
		}
		require.NoError(t, db.Create(&run).Error)
		execution := model.ChannelModelDetectionExecution{
			RunId: run.RunId, TargetKey: initialExecution.TargetKey, TargetId: initialExecution.TargetId, ChannelId: 115,
			RequestModel: initialExecution.RequestModel, ClaimedModel: initialExecution.ClaimedModel,
			Preset: run.Preset, Status: test.status, OutcomeCode: test.outcome, TitleCN: test.title,
			PlannedLogicalRequests: 3, CompletedLogicalRequests: 3,
			StartedAt: test.startedAt, FinishedAt: test.startedAt + 2,
			CreatedAt: test.startedAt - 1, UpdatedAt: test.startedAt + 2,
		}
		if test.conflict {
			require.NoError(t, execution.SetReport(map[string]any{
				"claimed_model":              model.ChannelModelDetectionClaimedModelSol,
				"outcome_code":               "juice_pass_fingerprint_strong",
				"fingerprint_verdict_state":  "strong_match",
				"fingerprint_model":          model.ChannelModelDetectionClaimedModelLuna,
				"fingerprint_claim_mismatch": true,
			}))
		}
		require.NoError(t, db.Create(&execution).Error)
	}

	response, err := GetChannelModelDetectionOverview(context.Background(), db, 1_000)
	require.NoError(t, err)
	require.Len(t, response.Channels, 1)
	require.Len(t, response.Channels[0].Targets, 1)
	target := response.Channels[0].Targets[0]
	require.Len(t, target.RecentWindow, 4)
	assert.Equal(t, []int64{780, 840, 900, 960}, []int64{
		target.RecentWindow[0].StartedAt, target.RecentWindow[1].StartedAt,
		target.RecentWindow[2].StartedAt, target.RecentWindow[3].StartedAt,
	})
	assert.Equal(t, channelModelDetectionBucketResultSuccess, target.RecentWindow[0].Result)
	assert.Equal(t, channelModelDetectionBucketResultUnhealthy, target.RecentWindow[1].Result)
	assert.Equal(t, 2, target.RecentWindow[1].DetectionCount)
	assert.Equal(t, 1, target.RecentWindow[1].Unhealthy)
	assert.Equal(t, 1, target.RecentWindow[1].Failed)
	assert.Equal(t, channelModelDetectionBucketResultFailed, target.RecentWindow[1].LatestResult)
	assert.Equal(t, 1, target.RecentWindow[1].LatestDetectionCount)
	assert.Equal(t, 1, target.RecentWindow[1].LatestFailed)
	assert.Equal(t, channelModelDetectionBucketResultAttention, target.RecentWindow[2].Result)
	assert.Empty(t, target.RecentWindow[3].Result)
	assert.Zero(t, target.RecentWindow[3].DetectionCount)
	require.NotNil(t, target.Latest)
	assert.Equal(t, "run-115-window-04", target.Latest.RunID)
	assert.Equal(t, 4, response.Settings.DisplayValue)
	assert.Equal(t, model.ChannelModelDetectionDisplayUnitMinute, response.Settings.DisplayUnit)
}

func TestChannelModelDetectionOverviewKeepsLatestExecutionOutsideDisplayWindow(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	require.NoError(t, db.Create(&model.ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://127.0.0.1:18080", ScheduledPreset: model.ChannelModelDetectionPresetMedium,
		IntervalHours: 24, DisplayValue: 2, DisplayUnit: model.ChannelModelDetectionDisplayUnitMinute,
		ScheduleTime: "02:30", Timezone: "Asia/Shanghai", Revision: 1,
	}).Error)
	run, _ := seedChannelModelDetectionQueryChannel(t, db, 116, "juice_pass_fingerprint_strong", 700)

	response, err := GetChannelModelDetectionOverview(context.Background(), db, 1_000)
	require.NoError(t, err)
	require.Len(t, response.Channels, 1)
	require.Len(t, response.Channels[0].Targets, 1)
	target := response.Channels[0].Targets[0]
	require.Len(t, target.RecentWindow, 2)
	assert.Equal(t, []int64{900, 960}, []int64{target.RecentWindow[0].StartedAt, target.RecentWindow[1].StartedAt})
	assert.Zero(t, target.RecentWindow[0].DetectionCount)
	assert.Zero(t, target.RecentWindow[1].DetectionCount)
	require.NotNil(t, target.Latest)
	assert.Equal(t, run.RunId, target.Latest.RunID)
	assert.Equal(t, "juice_pass_fingerprint_strong", target.Latest.OutcomeCode)
}

func TestChannelModelDetectionOverviewIncludesSupportedModelsBeforeTargetsAreConfigured(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	channels := []model.Channel{
		{Id: 111, Name: "default-channel", Key: "secret-default", Group: "default", Models: "model-a,model-shared", Status: common.ChannelStatusEnabled},
		{Id: 112, Name: "vip-channel", Key: "secret-vip", Group: "vip", Models: "model-b,model-shared", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	config := model.ChannelModelDetectionConfig{ChannelId: 112, ScheduleEnabled: false, Revision: 1}
	require.NoError(t, db.Create(&config).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: 112, TargetKey: "vip-custom-target",
		RequestModel: "custom-model", ClaimedModel: model.ChannelModelDetectionClaimedModelSol, Enabled: true,
	}).Error)

	overview, err := GetChannelModelDetectionOverview(context.Background(), db, 1_700_000_000)
	require.NoError(t, err)
	assert.Equal(t, []string{"custom-model", "model-a", "model-b", "model-shared"}, overview.Models)
	assert.Equal(t, []string{"model-a", "model-shared"}, overview.ModelsByGroup["default"])
	assert.Equal(t, []string{"custom-model", "model-b", "model-shared"}, overview.ModelsByGroup["vip"])
}

func TestChannelModelDetectionOverviewIncludesCurrentCostRatio(t *testing.T) {
	db := setupChannelModelDetectionQueryTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id: 121, Name: "ratio-channel", Key: "secret-ratio", Group: "default", Models: "gpt-5.6-sol", Status: common.ChannelStatusEnabled,
	}).Error)
	costConversion, err := MarshalChannelMonitorCostConversion(ChannelMonitorCostConversion{
		Mode:        ChannelMonitorCostConversionRecharge,
		PaidCNY:     80,
		CreditedUSD: 100,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 121, Ratio: 1.5, CostConversion: costConversion, UpdatedTime: 1_000,
	}).Error)

	overview, err := GetChannelModelDetectionOverview(context.Background(), db, 1_700_000_000)
	require.NoError(t, err)
	require.Len(t, overview.Channels, 1)
	require.NotNil(t, overview.Channels[0].CostRatio)
	assert.InDelta(t, 1.2, *overview.Channels[0].CostRatio, 1e-12)

	encoded, err := common.Marshal(overview.Channels[0])
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"cost_ratio":1.2`)
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
