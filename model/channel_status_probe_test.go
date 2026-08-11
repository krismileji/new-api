package model

import (
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusProbeModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-status-probe.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&ChannelStatusProbeConfig{},
		&ChannelStatusProbeState{},
		&ChannelStatusProbeExecution{},
	))
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, originalLogDatabaseType)
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestSaveChannelStatusProbeConfigRejectsStaleRevision(t *testing.T) {
	setupChannelStatusProbeModelTestDB(t)
	created, err := SaveChannelStatusProbeConfig(12, ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"gpt-4.1"}, IntervalSeconds: 60, Revision: 0,
	}, 1_000)
	require.NoError(t, err)
	assert.EqualValues(t, 1, created.Revision)
	assert.EqualValues(t, 1_061, created.NextRunAt)
	assert.Equal(t, ChannelStatusProbeDefaultDisplayValue, created.DisplayValue)
	assert.Equal(t, ChannelStatusProbeDefaultDisplayUnit, created.DisplayUnit)

	_, err = SaveChannelStatusProbeConfig(12, ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"gpt-4.1-mini"}, IntervalSeconds: 300,
		DisplayValue: 15, DisplayUnit: ChannelStatusProbeDisplayUnitMinute, Revision: 0,
	}, 1_010)
	assert.ErrorIs(t, err, ErrChannelStatusProbeConfigChanged)

	stored, err := GetChannelStatusProbeConfig(12)
	require.NoError(t, err)
	models, err := stored.Models()
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-4.1"}, models)
	assert.Equal(t, 60, stored.IntervalSeconds)
}

func TestManualChannelStatusProbeClaimKeepsScheduledRunTime(t *testing.T) {
	setupChannelStatusProbeModelTestDB(t)
	created, err := SaveChannelStatusProbeConfig(8, ChannelStatusProbeConfigInput{
		Enabled: true, Models: []string{"model-a", "model-b"}, IntervalSeconds: 300, Revision: 0,
	}, 2_000)
	require.NoError(t, err)

	manualRequestId, err := RequestChannelStatusProbeManualRun(8, 2_010)
	require.NoError(t, err)
	_, err = RequestChannelStatusProbeManualRun(8, 2_011)
	assert.ErrorIs(t, err, ErrChannelStatusProbeManualPending)

	claims, err := ClaimDueChannelStatusProbes(2_012, 1)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	assert.Equal(t, ChannelStatusProbeTriggerManual, claims[0].Trigger)
	assert.Equal(t, manualRequestId, claims[0].RunId)
	assert.Equal(t, []string{"model-a", "model-b"}, claims[0].Models)
	require.NoError(t, CompleteChannelStatusProbeClaim(claims[0], 2_020))

	stored, err := GetChannelStatusProbeConfig(8)
	require.NoError(t, err)
	assert.Equal(t, created.NextRunAt, stored.NextRunAt)
	assert.Empty(t, stored.ManualRequestId)
	assert.Empty(t, stored.RunningRunId)
}

func TestSaveChannelStatusProbeExecutionAccumulatesMinuteAndIsIdempotent(t *testing.T) {
	db := setupChannelStatusProbeModelTestDB(t)
	firstToken := 240.0
	tps := 42.5
	response := 1_200.0
	first := ChannelStatusProbeExecution{
		RunId: "run-one", ChannelId: 3, ModelName: "gpt-4.1", ConfigRevision: 1,
		Trigger: ChannelStatusProbeTriggerScheduled, Result: ChannelStatusProbeResultSuccess,
		StartedAt: 3_600, FinishedAt: 3_601, ResponseTimeMs: &response,
		FirstTokenMs: &firstToken, TPS: &tps, RequestDispatched: true,
		SampleStatus: ChannelStatusProbeSampleSkipped,
	}
	created, err := SaveChannelStatusProbeExecution(&first)
	require.NoError(t, err)
	assert.True(t, created)

	duplicate := first
	duplicate.Id = 0
	created, err = SaveChannelStatusProbeExecution(&duplicate)
	require.NoError(t, err)
	assert.False(t, created)
	var executionCount int64
	require.NoError(t, db.Model(&ChannelStatusProbeExecution{}).Count(&executionCount).Error)
	assert.EqualValues(t, 1, executionCount)

	second := ChannelStatusProbeExecution{
		RunId: "run-two", ChannelId: 3, ModelName: "gpt-4.1", ConfigRevision: 1,
		Trigger: ChannelStatusProbeTriggerManual, Result: ChannelStatusProbeResultUpstreamFailure,
		StartedAt: 3_620, FinishedAt: 3_625, ResponseTimeMs: &response,
		RequestDispatched: true, ErrorCode: "upstream_error", ErrorMessage: "上游不可用",
		SampleStatus: ChannelStatusProbeSampleSkipped,
	}
	created, err = SaveChannelStatusProbeExecution(&second)
	require.NoError(t, err)
	assert.True(t, created)

	var state ChannelStatusProbeState
	require.NoError(t, db.Where("channel_id = ? AND model_name = ?", 3, "gpt-4.1").First(&state).Error)
	assert.Equal(t, second.Id, state.ExecutionId)
	assert.Equal(t, ChannelStatusProbeResultUpstreamFailure, state.LastHealthResult)
	assert.Equal(t, 1, state.ConsecutiveFailures)
	assert.Nil(t, state.FirstTokenMs)
	buckets, err := state.Buckets(ChannelStatusProbeDisplayUnitMinute)
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	assert.Equal(t, 1, buckets[0].Success)
	assert.Equal(t, 1, buckets[0].UpstreamFailure)
	assert.InDelta(t, firstToken, buckets[0].FirstTokenTotalMs, 0.001)
	assert.EqualValues(t, 1, buckets[0].FirstTokenSampleCount)
	assert.InDelta(t, tps, buckets[0].TPSTotal, 0.001)
	assert.EqualValues(t, 1, buckets[0].TPSSampleCount)

	hourBuckets, err := state.Buckets(ChannelStatusProbeDisplayUnitHour)
	require.NoError(t, err)
	require.Len(t, hourBuckets, 1)
	assert.EqualValues(t, 3_600, hourBuckets[0].StartedAt)
	dayBuckets, err := state.Buckets(ChannelStatusProbeDisplayUnitDay)
	require.NoError(t, err)
	require.Len(t, dayBuckets, 1)
	assert.EqualValues(t, -8*60*60, dayBuckets[0].StartedAt)
}

func TestSaveChannelStatusProbeExecutionDoesNotLetOlderResultReplaceLatest(t *testing.T) {
	setupChannelStatusProbeModelTestDB(t)
	newer := ChannelStatusProbeExecution{
		RunId: "newer", ChannelId: 4, ModelName: "model-a", Trigger: ChannelStatusProbeTriggerScheduled,
		Result: ChannelStatusProbeResultSuccess, StartedAt: 7_000, FinishedAt: 7_010,
		RequestDispatched: true, SampleStatus: ChannelStatusProbeSampleSkipped,
	}
	_, err := SaveChannelStatusProbeExecution(&newer)
	require.NoError(t, err)
	older := ChannelStatusProbeExecution{
		RunId: "older", ChannelId: 4, ModelName: "model-a", Trigger: ChannelStatusProbeTriggerScheduled,
		Result: ChannelStatusProbeResultUpstreamFailure, StartedAt: 6_900, FinishedAt: 6_910,
		RequestDispatched: true, SampleStatus: ChannelStatusProbeSampleSkipped,
	}
	_, err = SaveChannelStatusProbeExecution(&older)
	require.NoError(t, err)

	states, err := GetChannelStatusProbeStates()
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, newer.Id, states[0].ExecutionId)
	assert.Equal(t, ChannelStatusProbeResultSuccess, states[0].Result)
	assert.Equal(t, ChannelStatusProbeResultSuccess, states[0].LastHealthResult)
	assert.Equal(t, newer.Id, states[0].LastHealthExecutionId)
	buckets, err := states[0].Buckets(ChannelStatusProbeDisplayUnitMinute)
	require.NoError(t, err)
	require.Len(t, buckets, 2)
	assert.EqualValues(t, 6_900, buckets[0].StartedAt)
	assert.Equal(t, 1, buckets[0].UpstreamFailure)
	assert.EqualValues(t, 6_960, buckets[1].StartedAt)
	assert.Equal(t, 1, buckets[1].Success)
}

func TestChannelStatusProbeDisplayRangeValidatesNaturalUnitLimits(t *testing.T) {
	tests := []struct {
		value int
		unit  string
		want  bool
	}{
		{value: 1, unit: ChannelStatusProbeDisplayUnitMinute, want: true},
		{value: 60, unit: ChannelStatusProbeDisplayUnitMinute, want: true},
		{value: 61, unit: ChannelStatusProbeDisplayUnitMinute, want: false},
		{value: 24, unit: ChannelStatusProbeDisplayUnitHour, want: true},
		{value: 25, unit: ChannelStatusProbeDisplayUnitHour, want: false},
		{value: 30, unit: ChannelStatusProbeDisplayUnitDay, want: true},
		{value: 31, unit: ChannelStatusProbeDisplayUnitDay, want: false},
		{value: 1, unit: "week", want: false},
	}
	for _, test := range tests {
		assert.Equal(t, test.want, IsChannelStatusProbeDisplayAllowed(test.value, test.unit))
	}

	value, unit := NormalizeChannelStatusProbeDisplay(31, ChannelStatusProbeDisplayUnitDay)
	assert.Equal(t, ChannelStatusProbeDefaultDisplayValue, value)
	assert.Equal(t, ChannelStatusProbeDefaultDisplayUnit, unit)
}

func TestAccumulateChannelStatusProbeBucketsKeepsLatestThirtyBeijingDays(t *testing.T) {
	const firstFinishedAt = int64(1_725_854_400)
	firstDayStart := ChannelStatusProbeDisplayBucketStart(firstFinishedAt, ChannelStatusProbeDisplayUnitDay)
	buckets := []ChannelStatusProbeBucket{}
	for day := 0; day < 31; day++ {
		execution := ChannelStatusProbeExecution{
			ModelName:  "model-a",
			Result:     ChannelStatusProbeResultSuccess,
			FinishedAt: firstFinishedAt + int64(day)*24*60*60,
		}
		buckets = accumulateChannelStatusProbeBuckets(
			buckets,
			&execution,
			ChannelStatusProbeDisplayUnitDay,
			ChannelStatusProbeMaxDisplayDays,
		)
	}

	require.Len(t, buckets, ChannelStatusProbeMaxDisplayDays)
	assert.Equal(t, firstDayStart+24*60*60, buckets[0].StartedAt)
	assert.Equal(t, firstDayStart+30*24*60*60, buckets[len(buckets)-1].StartedAt)
}

func TestListPendingChannelStatusProbeExecutionsReturnsOldestRequestedSamples(t *testing.T) {
	db := setupChannelStatusProbeModelTestDB(t)
	executions := []ChannelStatusProbeExecution{
		{RunId: "recorded", ChannelId: 1, ModelName: "model-a", FinishedAt: 100, CreatedAt: 100, SampleRequested: true, SampleStatus: ChannelStatusProbeSampleRecorded},
		{RunId: "pending-new", ChannelId: 1, ModelName: "model-b", FinishedAt: 300, CreatedAt: 300, SampleRequested: true, SampleStatus: ChannelStatusProbeSamplePending},
		{RunId: "pending-old", ChannelId: 1, ModelName: "model-c", FinishedAt: 200, CreatedAt: 200, SampleRequested: true, SampleStatus: ChannelStatusProbeSamplePending},
		{RunId: "not-requested", ChannelId: 1, ModelName: "model-d", FinishedAt: 50, CreatedAt: 50, SampleStatus: ChannelStatusProbeSamplePending},
	}
	require.NoError(t, db.Create(&executions).Error)

	pending, err := ListPendingChannelStatusProbeExecutions(1)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "pending-old", pending[0].RunId)
}

func TestDeleteChannelStatusProbeExecutionsBeforeUsesStrictCutoff(t *testing.T) {
	db := setupChannelStatusProbeModelTestDB(t)
	executions := []ChannelStatusProbeExecution{
		{RunId: "expired", ChannelId: 1, ModelName: "model-a", FinishedAt: 99, CreatedAt: 99},
		{RunId: "boundary", ChannelId: 1, ModelName: "model-b", FinishedAt: 100, CreatedAt: 100},
		{RunId: "current", ChannelId: 1, ModelName: "model-c", FinishedAt: 101, CreatedAt: 101},
	}
	require.NoError(t, db.Create(&executions).Error)

	deleted, err := DeleteChannelStatusProbeExecutionsBefore(t.Context(), 100, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	var remaining []ChannelStatusProbeExecution
	require.NoError(t, db.Order("finished_at ASC").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	assert.Equal(t, "boundary", remaining[0].RunId)
	assert.Equal(t, "current", remaining[1].RunId)
}
