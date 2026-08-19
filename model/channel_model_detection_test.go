package model

import (
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&ChannelModelDetectionGlobalConfig{},
		&ChannelModelDetectionConfig{},
		&ChannelModelDetectionTarget{},
		&ChannelModelDetectionBatch{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionExecution{},
		&ChannelModelDetectionCostEvent{},
	))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestChannelModelDetectionMigrationCreatesPortableTablesAndIndexes(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	models := []any{
		&ChannelModelDetectionGlobalConfig{},
		&ChannelModelDetectionConfig{},
		&ChannelModelDetectionTarget{},
		&ChannelModelDetectionBatch{},
		&ChannelModelDetectionRun{},
		&ChannelModelDetectionExecution{},
		&ChannelModelDetectionCostEvent{},
	}
	for _, model := range models {
		assert.True(t, db.Migrator().HasTable(model), reflect.TypeOf(model).String())
	}

	indexes := []struct {
		model any
		name  string
	}{
		{&ChannelModelDetectionConfig{}, "idx_channel_model_detection_configs_channel_id"},
		{&ChannelModelDetectionTarget{}, "idx_channel_model_detection_target_identity"},
		{&ChannelModelDetectionBatch{}, "idx_channel_model_detection_batches_batch_id"},
		{&ChannelModelDetectionBatch{}, "idx_channel_model_detection_batch_schedule"},
		{&ChannelModelDetectionRun{}, "idx_channel_model_detection_runs_run_id"},
		{&ChannelModelDetectionRun{}, "idx_channel_model_detection_run_channel_created"},
		{&ChannelModelDetectionExecution{}, "idx_channel_model_detection_execution_target"},
		{&ChannelModelDetectionCostEvent{}, "idx_channel_model_detection_cost_events_cost_event_id"},
		{&ChannelModelDetectionCostEvent{}, "idx_channel_model_detection_cost_attempt"},
	}
	for _, index := range indexes {
		assert.True(t, db.Migrator().HasIndex(index.model, index.name), index.name)
	}

	for _, table := range []string{
		"channel_model_detection_global_configs",
		"channel_model_detection_configs",
		"channel_model_detection_targets",
		"channel_model_detection_batches",
		"channel_model_detection_runs",
		"channel_model_detection_executions",
		"channel_model_detection_cost_events",
	} {
		var ddl string
		require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&ddl).Error)
		assert.NotEmpty(t, ddl, table)
		assert.NotContains(t, strings.ToLower(ddl), " json", table)
	}

	for _, column := range []struct {
		model any
		name  string
	}{
		{&ChannelModelDetectionExecution{}, "official_config_json"},
		{&ChannelModelDetectionExecution{}, "report_json"},
	} {
		typeName, err := db.Migrator().ColumnTypes(column.model)
		require.NoError(t, err)
		found := false
		for _, columnType := range typeName {
			if columnType.Name() != column.name {
				continue
			}
			found = true
			assert.Equal(t, "text", strings.ToLower(columnType.DatabaseTypeName()))
		}
		assert.True(t, found, column.name)
	}
	assert.True(t, db.Migrator().HasColumn(&ChannelModelDetectionGlobalConfig{}, "worker_lease_token"))
	assert.True(t, db.Migrator().HasColumn(&ChannelModelDetectionGlobalConfig{}, "scheduled_high_confirmed_revision"))
	assert.True(t, db.Migrator().HasColumn(&ChannelModelDetectionGlobalConfig{}, "relay_url"))
	assert.True(t, db.Migrator().HasColumn(&ChannelModelDetectionExecution{}, "detector_url_snapshot"))
}

func TestChannelModelDetectionUniqueContractsRejectDuplicates(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)

	require.NoError(t, db.Create(&ChannelModelDetectionConfig{ChannelId: 10}).Error)
	require.Error(t, db.Create(&ChannelModelDetectionConfig{ChannelId: 10}).Error)

	config := ChannelModelDetectionConfig{ChannelId: 20}
	require.NoError(t, db.Create(&config).Error)
	target := ChannelModelDetectionTarget{
		ConfigId: config.Id, ChannelId: config.ChannelId, TargetKey: "target-1",
		RequestModel: "channel-alias", ClaimedModel: ChannelModelDetectionClaimedModelSol,
	}
	require.NoError(t, db.Create(&target).Error)
	duplicateTarget := target
	duplicateTarget.Id = 0
	duplicateTarget.TargetKey = "target-2"
	require.Error(t, db.Create(&duplicateTarget).Error)

	require.NoError(t, db.Create(&ChannelModelDetectionBatch{
		BatchId: "batch-1", GlobalConfigRevision: 1, Preset: ChannelModelDetectionPresetLow,
		ScheduledFor: 1_000,
	}).Error)
	require.Error(t, db.Create(&ChannelModelDetectionBatch{
		BatchId: "batch-2", GlobalConfigRevision: 1, Preset: ChannelModelDetectionPresetLow,
		ScheduledFor: 1_000,
	}).Error)

	run := ChannelModelDetectionRun{
		RunId: "run-1", ChannelId: config.ChannelId, ConfigRevision: 1, GlobalConfigRevision: 1,
		Trigger: ChannelModelDetectionTriggerManual, Preset: ChannelModelDetectionPresetMedium,
	}
	require.NoError(t, db.Create(&run).Error)
	require.Error(t, db.Create(&ChannelModelDetectionRun{
		RunId: "run-1", ChannelId: 21, ConfigRevision: 1, GlobalConfigRevision: 1,
		Trigger: ChannelModelDetectionTriggerManual, Preset: ChannelModelDetectionPresetMedium,
	}).Error)

	execution := ChannelModelDetectionExecution{
		RunId: run.RunId, TargetKey: target.TargetKey, TargetId: target.Id, ChannelId: config.ChannelId,
		RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel, Preset: run.Preset,
	}
	require.NoError(t, db.Create(&execution).Error)
	event := ChannelModelDetectionCostEvent{
		CostEventId: "cost-1", RunId: run.RunId, TargetId: target.Id, ExecutionId: execution.Id,
		ChannelId: config.ChannelId, RequestModel: target.RequestModel, ClaimedModel: target.ClaimedModel,
		Preset: run.Preset, DetectorRequestId: "detector-request-1", AttemptNo: 1,
	}
	require.NoError(t, db.Create(&event).Error)
	duplicateEvent := event
	duplicateEvent.Id = 0
	require.Error(t, db.Create(&duplicateEvent).Error)
}

func TestChannelModelDetectionModelsDoNotPersistDetectorCredentials(t *testing.T) {
	modelTypes := []reflect.Type{
		reflect.TypeOf(ChannelModelDetectionGlobalConfig{}),
		reflect.TypeOf(ChannelModelDetectionConfig{}),
		reflect.TypeOf(ChannelModelDetectionTarget{}),
		reflect.TypeOf(ChannelModelDetectionBatch{}),
		reflect.TypeOf(ChannelModelDetectionRun{}),
		reflect.TypeOf(ChannelModelDetectionExecution{}),
		reflect.TypeOf(ChannelModelDetectionCostEvent{}),
	}
	for _, modelType := range modelTypes {
		for index := 0; index < modelType.NumField(); index++ {
			fieldName := strings.ToLower(modelType.Field(index).Name)
			assert.NotContains(t, fieldName, "sessiontoken", modelType.Name())
			assert.NotContains(t, fieldName, "apikey", modelType.Name())
			assert.NotContains(t, fieldName, "rawkey", modelType.Name())
			assert.NotContains(t, fieldName, "taskcredential", modelType.Name())
		}
	}
	executionJSON, err := common.Marshal(ChannelModelDetectionExecution{
		DetectorURLSnapshot: "http://detector.internal:18080/private",
	})
	require.NoError(t, err)
	assert.NotContains(t, string(executionJSON), "detector.internal")
	globalJSON, err := common.Marshal(ChannelModelDetectionGlobalConfig{
		ScheduledHighConfirmedRevision: 7,
		WorkerLeaseToken:               "worker-secret-token",
	})
	require.NoError(t, err)
	assert.NotContains(t, string(globalJSON), "worker-secret-token")
	assert.NotContains(t, string(globalJSON), "scheduled_high_confirmed_revision")
}

func TestChannelModelDetectionGlobalConfigDefaultsAndSingleRow(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	config := ChannelModelDetectionGlobalConfig{
		DetectorURL: "http://127.0.0.1:3000",
		RelayURL:    "http://127.0.0.1:3000/internal/model-detector/v1",
	}
	require.NoError(t, db.Create(&config).Error)
	assert.EqualValues(t, ChannelModelDetectionConfigID, config.Id)
	assert.Equal(t, ChannelModelDetectionPresetMedium, config.ScheduledPreset)
	assert.Equal(t, ChannelModelDetectionDefaultIntervalMinutes, config.IntervalMinutes)
	assert.Equal(t, ChannelModelDetectionDefaultIntervalHours, config.IntervalHours)
	assert.Equal(t, ChannelModelDetectionDefaultScheduleTime, config.ScheduleTime)
	assert.Equal(t, ChannelModelDetectionDefaultTimezone, config.Timezone)
	assert.EqualValues(t, 1, config.Revision)
	assert.True(t, config.DetectorURLConfigured())
	assert.True(t, config.RelayURLConfigured())
	require.Error(t, db.Create(&ChannelModelDetectionGlobalConfig{DetectorURL: "http://127.0.0.1:3001"}).Error)

	invalid := ChannelModelDetectionGlobalConfig{
		ScheduledPreset: ChannelModelDetectionPresetLow, IntervalMinutes: ChannelModelDetectionMaxIntervalMinutes + 1,
	}
	assert.ErrorIs(t, invalid.Validate(), ErrChannelModelDetectionInvalidSchedule)
}

func TestChannelModelDetectionGlobalLeaseUsesRevisionAndTokenGuards(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	config := ChannelModelDetectionGlobalConfig{}
	require.NoError(t, db.Create(&config).Error)

	claimed, err := config.TryAcquireLease(db, config.Revision, 1_000, "lease-a")
	require.NoError(t, err)
	assert.True(t, claimed)
	assert.EqualValues(t, 1_000+ChannelModelDetectionLeaseSeconds, config.LeaseUntil)

	other := ChannelModelDetectionGlobalConfig{Id: config.Id}
	claimed, err = other.TryAcquireLease(db, config.Revision, 1_001, "lease-b")
	require.NoError(t, err)
	assert.False(t, claimed)

	renewed, err := config.RenewLease(db, config.Revision+1, 1_010, "lease-a")
	require.NoError(t, err)
	assert.False(t, renewed)
	renewed, err = config.RenewLease(db, config.Revision, 1_010, "lease-a")
	require.NoError(t, err)
	assert.True(t, renewed)

	released, err := config.ReleaseLease(db, config.Revision, "lease-b", 1_020)
	require.NoError(t, err)
	assert.False(t, released)
	released, err = config.ReleaseLease(db, config.Revision, "lease-a", 1_020)
	require.NoError(t, err)
	assert.True(t, released)
}

func TestChannelModelDetectionWorkerDBLeaseFencesExpiredOwner(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	config := ChannelModelDetectionGlobalConfig{}
	require.NoError(t, db.Create(&config).Error)

	claimed, err := config.TryAcquireWorkerLease(db, 1_000, "worker-a")
	require.NoError(t, err)
	assert.True(t, claimed)
	assert.EqualValues(t, 1_000+ChannelModelDetectionWorkerLeaseSeconds, config.WorkerLeaseUntil)

	other := ChannelModelDetectionGlobalConfig{Id: config.Id}
	claimed, err = other.TryAcquireWorkerLease(db, 1_001, "worker-b")
	require.NoError(t, err)
	assert.False(t, claimed)

	claimed, err = other.TryAcquireWorkerLease(db, 1_000+ChannelModelDetectionWorkerLeaseSeconds, "worker-b")
	require.NoError(t, err)
	assert.True(t, claimed)

	renewed, err := config.RenewWorkerLease(db, 1_000+ChannelModelDetectionWorkerLeaseSeconds+1, "worker-a")
	require.NoError(t, err)
	assert.False(t, renewed)
	released, err := config.ReleaseWorkerLease(db, "worker-a")
	require.NoError(t, err)
	assert.False(t, released)

	renewed, err = other.RenewWorkerLease(db, 1_000+ChannelModelDetectionWorkerLeaseSeconds+1, "worker-b")
	require.NoError(t, err)
	assert.True(t, renewed)
	released, err = other.ReleaseWorkerLease(db, "worker-b")
	require.NoError(t, err)
	assert.True(t, released)
}

func TestChannelModelDetectionScheduledHighConfirmationIsRevisionScoped(t *testing.T) {
	config := ChannelModelDetectionGlobalConfig{
		ScheduledPreset: ChannelModelDetectionPresetHigh,
		ScheduleEnabled: true,
		IntervalHours:   ChannelModelDetectionDefaultIntervalHours,
		ScheduleTime:    ChannelModelDetectionDefaultScheduleTime,
		Timezone:        ChannelModelDetectionDefaultTimezone,
		Revision:        4,
	}
	assert.ErrorIs(t, config.ApplyScheduledHighCostConfirmation(false), ErrChannelModelDetectionScheduledHighUnconfirmed)
	require.NoError(t, config.ApplyScheduledHighCostConfirmation(true))
	assert.EqualValues(t, config.Revision, config.ScheduledHighConfirmedRevision)
	assert.NoError(t, config.Validate())

	config.Revision++
	assert.ErrorIs(t, config.Validate(), ErrChannelModelDetectionScheduledHighUnconfirmed)
	config.ScheduledPreset = ChannelModelDetectionPresetMedium
	require.NoError(t, config.ApplyScheduledHighCostConfirmation(false))
	assert.Zero(t, config.ScheduledHighConfirmedRevision)
}

func TestChannelModelDetectionRunFreezesManualPresetSource(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	run := ChannelModelDetectionRun{
		ChannelId: 44, ConfigRevision: 3, GlobalConfigRevision: 7,
		Trigger: ChannelModelDetectionTriggerManual, Preset: ChannelModelDetectionPresetHigh,
	}
	require.NoError(t, db.Create(&run).Error)
	assert.NotEmpty(t, run.RunId)
	assert.Equal(t, ChannelModelDetectionPresetSourceManualSelected, run.PresetSource)
	assert.Equal(t, ChannelModelDetectionRunStatusQueued, run.Status)
	assert.Positive(t, run.QueuedAt)
	assert.Equal(t, run.QueuedAt, run.CreatedAt)

	invalid := run
	invalid.PresetSource = ChannelModelDetectionPresetSourceScheduledDefault
	assert.ErrorIs(t, invalid.Validate(), ErrChannelModelDetectionInvalidTrigger)
}

func TestChannelModelDetectionCreateRunGuardsSingleActiveChannelRun(t *testing.T) {
	db := setupChannelModelDetectionTestDB(t)
	config := ChannelModelDetectionConfig{ChannelId: 45}
	require.NoError(t, db.Create(&config).Error)

	first := ChannelModelDetectionRun{
		ChannelId: config.ChannelId, ConfigRevision: config.Revision, GlobalConfigRevision: 1,
		Trigger: ChannelModelDetectionTriggerScheduled, Preset: ChannelModelDetectionPresetLow,
	}
	created, err := CreateChannelModelDetectionRun(db, &first)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEmpty(t, first.RunId)

	second := ChannelModelDetectionRun{
		ChannelId: config.ChannelId, ConfigRevision: config.Revision, GlobalConfigRevision: 1,
		Trigger: ChannelModelDetectionTriggerManual, Preset: ChannelModelDetectionPresetMedium,
	}
	created, err = CreateChannelModelDetectionRun(db, &second)
	require.NoError(t, err)
	assert.False(t, created)

	var runCount int64
	require.NoError(t, db.Model(&ChannelModelDetectionRun{}).Where("channel_id = ?", config.ChannelId).Count(&runCount).Error)
	assert.EqualValues(t, 1, runCount)

	released, err := ReleaseChannelModelDetectionRun(db, config.ChannelId, "wrong-run", 100)
	require.NoError(t, err)
	assert.False(t, released)
	released, err = ReleaseChannelModelDetectionRun(db, config.ChannelId, first.RunId, 101)
	require.NoError(t, err)
	assert.True(t, released)

	created, err = CreateChannelModelDetectionRun(db, &second)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, ChannelModelDetectionPresetSourceManualSelected, second.PresetSource)
}

func TestChannelModelDetectionExecutionJSONUsesWrapperAndPreservesUnknownFields(t *testing.T) {
	execution := ChannelModelDetectionExecution{}
	require.NoError(t, execution.SetOfficialConfig(map[string]any{
		"workers":               2,
		"future_detector_field": map[string]any{"enabled": true},
	}))
	config, err := execution.OfficialConfig()
	require.NoError(t, err)
	assert.EqualValues(t, 2, config["workers"])
	assert.NotNil(t, config["future_detector_field"])

	require.NoError(t, execution.SetReport(map[string]any{"schema_version": 3, "future_report_field": "kept"}))
	report, err := execution.Report()
	require.NoError(t, err)
	assert.Equal(t, "kept", report["future_report_field"])

	assert.Error(t, execution.SetReport(map[string]any{"too_large": strings.Repeat("x", ChannelModelDetectionMaxReportBytes)}))
}

func TestChannelModelDetectionCostEventTracksTransportBoundaryAndUnknownCost(t *testing.T) {
	base := ChannelModelDetectionCostEvent{
		RunId: "run-1", TargetId: 1, ExecutionId: 2, ChannelId: 3,
		RequestModel: "model-alias", ClaimedModel: ChannelModelDetectionClaimedModelTerra,
		Preset: ChannelModelDetectionPresetMedium, DetectorRequestId: "request-1", AttemptNo: 1,
	}
	require.NoError(t, base.BeforeCreate(nil))
	assert.Equal(t, ChannelModelDetectionDispatchPrepared, base.DispatchState)
	assert.Equal(t, ChannelModelDetectionSettlementPending, base.SettlementStatus)
	assert.Equal(t, ChannelModelDetectionUsageUnavailable, base.UsageSource)
	assert.False(t, base.IsCostKnown())

	notStarted := base
	require.NoError(t, notStarted.MarkNotStarted(100))
	assert.Equal(t, ChannelModelDetectionSettlementNotApplicable, notStarted.SettlementStatus)
	assert.False(t, notStarted.IsCostKnown())
	assert.Error(t, notStarted.MarkDispatched(101))

	dispatched := base
	require.NoError(t, dispatched.MarkDispatched(100))
	require.NoError(t, dispatched.MarkUnresolved(101, nil, ChannelModelDetectionUsageUnavailable))
	assert.Equal(t, ChannelModelDetectionSettlementUnresolved, dispatched.SettlementStatus)
	assert.Nil(t, dispatched.EstimatedCostNanoCNY)
	assert.False(t, dispatched.IsCostKnown())

	settledCost := int64(2_500_000_000)
	require.NoError(t, dispatched.MarkSettled(
		102, 1_000, 800, &settledCost, 10, 20, 30,
		ChannelModelDetectionUsageUpstreamAuthoritative,
	))
	assert.Equal(t, ChannelModelDetectionSettlementSettled, dispatched.SettlementStatus)
	assert.True(t, dispatched.UsageAvailable)
	assert.True(t, dispatched.IsCostKnown())
	assert.Equal(t, settledCost, *dispatched.SettledCostNanoCNY)
	assert.Equal(t, int64(1_000), *dispatched.SettledQuota)
	assert.Equal(t, int64(800), *dispatched.CostBasisQuota)
	assert.Error(t, dispatched.MarkUnresolved(103, nil, ChannelModelDetectionUsageUnavailable))
	assert.Error(t, dispatched.MarkSettled(
		104, 1_000, 800, &settledCost, 10, 20, 30,
		ChannelModelDetectionUsageUpstreamAuthoritative,
	))
}

func TestChannelModelDetectionCostRejectsNegativeAndNonFiniteValues(t *testing.T) {
	negative := int64(-1)
	event := ChannelModelDetectionCostEvent{
		CostEventId: "cost", RunId: "run", TargetId: 1, ExecutionId: 2, ChannelId: 3,
		RequestModel: "model", ClaimedModel: ChannelModelDetectionClaimedModelSol,
		Preset: ChannelModelDetectionPresetLow, DetectorRequestId: "request", AttemptNo: 1,
		DispatchState:        ChannelModelDetectionDispatchPrepared,
		SettlementStatus:     ChannelModelDetectionSettlementPending,
		UsageSource:          ChannelModelDetectionUsageUnavailable,
		CostScope:            ChannelModelDetectionCostScopeChannelUpstreamAPI,
		EstimatedCostNanoCNY: &negative,
	}
	assert.ErrorIs(t, event.Validate(), ErrChannelModelDetectionInvalidCost)
	assert.ErrorIs(t, CheckChannelModelDetectionCostValues(math.NaN()), ErrChannelModelDetectionInvalidCost)
	assert.ErrorIs(t, CheckChannelModelDetectionCostValues(math.Inf(1)), ErrChannelModelDetectionInvalidCost)
	assert.ErrorIs(t, CheckChannelModelDetectionCostValues(-0.1), ErrChannelModelDetectionInvalidCost)
	assert.NoError(t, CheckChannelModelDetectionCostValues(0, 1.5))
}
