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

func setupChannelGroupMonitorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-group-monitor.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&ChannelGroupMonitorConfig{},
		&ChannelGroupMonitorState{},
		&ChannelGroupMonitorExecution{},
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

func TestSaveChannelGroupMonitorConfigRejectsStaleRevisionAndPreservesOrder(t *testing.T) {
	setupChannelGroupMonitorTestDB(t)
	created, err := SaveChannelGroupMonitorConfig(ChannelGroupMonitorConfigInput{
		Enabled: true,
		Groups: []ChannelGroupMonitorGroup{
			{GroupName: "vip", ProbeModel: "model-vip"},
			{GroupName: "default", ProbeModel: "model-default"},
		},
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: ChannelStatusProbeDisplayUnitMinute, Revision: 0,
	}, 1_000)
	require.NoError(t, err)
	assert.EqualValues(t, 1, created.Revision)
	assert.EqualValues(t, 1_200, created.NextRunAt)

	_, err = SaveChannelGroupMonitorConfig(ChannelGroupMonitorConfigInput{
		Enabled: true,
		Groups: []ChannelGroupMonitorGroup{
			{GroupName: "default", ProbeModel: "model-default"},
		},
		IntervalSeconds: 60, DisplayValue: 60,
		DisplayUnit: ChannelStatusProbeDisplayUnitMinute, Revision: 0,
	}, 1_010)
	assert.ErrorIs(t, err, ErrChannelGroupMonitorConfigChanged)

	stored, err := GetChannelGroupMonitorConfig()
	require.NoError(t, err)
	groups, err := stored.Groups()
	require.NoError(t, err)
	assert.Equal(t, []ChannelGroupMonitorGroup{
		{GroupName: "vip", ProbeModel: "model-vip"},
		{GroupName: "default", ProbeModel: "model-default"},
	}, groups)
}

func TestGetChannelGroupMonitorExecutionWindowSinceIncludesStartedAt(t *testing.T) {
	db := setupChannelGroupMonitorTestDB(t)
	require.NoError(t, db.Create(&ChannelGroupMonitorExecution{
		RunId: "timeout-run", GroupName: "default", Result: ChannelGroupMonitorResultTimeout,
		StartedAt: 1_000, FinishedAt: 1_060, CreatedAt: 1_060,
	}).Error)

	executions, err := GetChannelGroupMonitorExecutionWindowSince(960)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	assert.EqualValues(t, 1_000, executions[0].StartedAt)
}

func TestSaveChannelGroupMonitorExecutionKeepsLatestNonSkippedResult(t *testing.T) {
	setupChannelGroupMonitorTestDB(t)
	firstToken := 184.0
	responseTime := 1_240.0
	tps := 42.5
	created, err := SaveChannelGroupMonitorExecution(&ChannelGroupMonitorExecution{
		RunId: "run-success", GroupName: "vip", ProbeModel: "model-vip",
		Result: ChannelGroupMonitorResultSuccess, ResponseTimeMs: &responseTime,
		FirstTokenMs: &firstToken, TPS: &tps,
		FinishedAt: 1_000, CreatedAt: 1_000,
	})
	require.NoError(t, err)
	assert.True(t, created)
	states, err := GetChannelGroupMonitorStates()
	require.NoError(t, err)
	require.Len(t, states, 1)
	require.NotNil(t, states[0].ResponseTimeMs)
	require.NotNil(t, states[0].FirstTokenMs)
	require.NotNil(t, states[0].TPS)
	assert.InDelta(t, responseTime, *states[0].ResponseTimeMs, 0.001)
	assert.InDelta(t, firstToken, *states[0].FirstTokenMs, 0.001)
	assert.InDelta(t, tps, *states[0].TPS, 0.001)

	created, err = SaveChannelGroupMonitorExecution(&ChannelGroupMonitorExecution{
		RunId: "run-rate-limited", GroupName: "vip", ProbeModel: "model-vip",
		Result: ChannelGroupMonitorResultRateLimited, FirstTokenMs: &firstToken,
		FinishedAt: 1_010, CreatedAt: 1_010,
	})
	require.NoError(t, err)
	assert.True(t, created)

	created, err = SaveChannelGroupMonitorExecution(&ChannelGroupMonitorExecution{
		RunId: "run-skipped", GroupName: "vip", ProbeModel: "model-vip",
		Result: ChannelGroupMonitorResultSkipped, FinishedAt: 1_020, CreatedAt: 1_020,
	})
	require.NoError(t, err)
	assert.True(t, created)

	states, err = GetChannelGroupMonitorStates()
	require.NoError(t, err)
	require.Len(t, states, 1)
	assert.Equal(t, ChannelGroupMonitorResultRateLimited, states[0].Result)
	assert.EqualValues(t, 1_010, states[0].FinishedAt)
	assert.EqualValues(t, 1, states[0].ConsecutiveFailure)
	assert.Zero(t, states[0].ConsecutiveSuccess)
	assert.Nil(t, states[0].FirstTokenMs)
	assert.Nil(t, states[0].ResponseTimeMs)
	assert.Nil(t, states[0].TPS)

	executions, err := GetChannelGroupMonitorExecutionsSince(0)
	require.NoError(t, err)
	require.Len(t, executions, 2)
	assert.Equal(t, []string{
		ChannelGroupMonitorResultRateLimited,
		ChannelGroupMonitorResultSuccess,
	}, []string{executions[0].Result, executions[1].Result})
}

func TestRequestChannelGroupMonitorManualRunClearsExpiredLease(t *testing.T) {
	db := setupChannelGroupMonitorTestDB(t)
	created, err := SaveChannelGroupMonitorConfig(ChannelGroupMonitorConfigInput{
		Enabled:         false,
		Groups:          []ChannelGroupMonitorGroup{{GroupName: "default", ProbeModel: "model-default"}},
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: ChannelStatusProbeDisplayUnitMinute,
	}, 1_000)
	require.NoError(t, err)
	require.NoError(t, db.Model(&ChannelGroupMonitorConfig{}).Where("id = ?", created.Id).Updates(map[string]any{
		"enabled": false, "lease_token": "expired-token", "lease_until": int64(900),
		"running_trigger": ChannelGroupMonitorTriggerScheduled, "running_run_id": "expired-run",
		"running_started_at": int64(800),
	}).Error)

	requestID, err := RequestChannelGroupMonitorManualRun(1_000)
	require.NoError(t, err)
	assert.NotEmpty(t, requestID)
	var stored ChannelGroupMonitorConfig
	require.NoError(t, db.First(&stored, created.Id).Error)
	assert.Equal(t, requestID, stored.ManualRequestId)
	assert.Empty(t, stored.RunningRunId)
	assert.Empty(t, stored.LeaseToken)
	assert.Zero(t, stored.LeaseUntil)
}

func TestRequestChannelGroupMonitorManualRunReplacesExpiredManualLease(t *testing.T) {
	db := setupChannelGroupMonitorTestDB(t)
	created, err := SaveChannelGroupMonitorConfig(ChannelGroupMonitorConfigInput{
		Enabled:         false,
		Groups:          []ChannelGroupMonitorGroup{{GroupName: "default", ProbeModel: "model-default"}},
		IntervalSeconds: 300, DisplayValue: 60,
		DisplayUnit: ChannelStatusProbeDisplayUnitMinute,
	}, 1_000)
	require.NoError(t, err)
	oldRequestID := "expired-manual-request"
	require.NoError(t, db.Model(&ChannelGroupMonitorConfig{}).Where("id = ?", created.Id).Updates(map[string]any{
		"manual_request_id": oldRequestID, "manual_requested_at": int64(800),
		"lease_token": "expired-token", "lease_until": int64(900),
		"running_trigger": ChannelGroupMonitorTriggerManual, "running_run_id": oldRequestID,
		"running_started_at": int64(800),
	}).Error)

	requestID, err := RequestChannelGroupMonitorManualRun(1_000)
	require.NoError(t, err)
	assert.NotEqual(t, oldRequestID, requestID)
	var stored ChannelGroupMonitorConfig
	require.NoError(t, db.First(&stored, created.Id).Error)
	assert.Equal(t, requestID, stored.ManualRequestId)
	assert.Empty(t, stored.RunningRunId)
	assert.Empty(t, stored.LeaseToken)
	assert.Zero(t, stored.LeaseUntil)
}

func TestTimeoutOverdueChannelGroupMonitorPreservesCompletedGroups(t *testing.T) {
	db := setupChannelGroupMonitorTestDB(t)
	created, err := SaveChannelGroupMonitorConfig(ChannelGroupMonitorConfigInput{
		Enabled: true,
		Groups: []ChannelGroupMonitorGroup{
			{GroupName: "vip", ProbeModel: "model-vip"},
			{GroupName: "default", ProbeModel: "model-default"},
		},
		IntervalSeconds: 60, DisplayValue: 60,
		DisplayUnit: ChannelStatusProbeDisplayUnitMinute,
	}, 1_000)
	require.NoError(t, err)

	claims, err := ClaimDueChannelGroupMonitor(created.NextRunAt)
	require.NoError(t, err)
	require.NotNil(t, claims)
	claim := *claims
	stored, err := GetChannelGroupMonitorConfig()
	require.NoError(t, err)
	assert.Equal(t, created.NextRunAt+60, stored.NextRunAt)

	completed := ChannelGroupMonitorExecution{
		RunId: claim.RunId, GroupName: "vip", ConfigRevision: claim.Config.Revision,
		Trigger: ChannelGroupMonitorTriggerScheduled, ProbeModel: "model-vip",
		Result: ChannelGroupMonitorResultSuccess, StartedAt: created.NextRunAt,
		FinishedAt: stored.NextRunAt - 1, CreatedAt: stored.NextRunAt - 1,
	}
	createdExecution, err := SaveChannelGroupMonitorExecution(&completed)
	require.NoError(t, err)
	assert.True(t, createdExecution)

	timedOut, err := TimeoutOverdueChannelGroupMonitor(stored.NextRunAt, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, timedOut)

	var timeout ChannelGroupMonitorExecution
	require.NoError(t, db.Where("run_id = ? AND group_name = ?", claim.RunId, "default").First(&timeout).Error)
	assert.Equal(t, ChannelGroupMonitorResultTimeout, timeout.Result)
	assert.Equal(t, ChannelGroupMonitorErrorTimeout, timeout.ErrorCode)
	assert.Equal(t, ChannelGroupMonitorTimeoutMessage, timeout.ErrorMessage)
	assert.Equal(t, stored.NextRunAt, timeout.FinishedAt)

	var preserved ChannelGroupMonitorExecution
	require.NoError(t, db.Where("run_id = ? AND group_name = ?", claim.RunId, "vip").First(&preserved).Error)
	assert.Equal(t, ChannelGroupMonitorResultSuccess, preserved.Result)

	current, err := GetChannelGroupMonitorConfig()
	require.NoError(t, err)
	assert.Empty(t, current.RunningRunId)
	assert.Empty(t, current.LeaseToken)

	late := ChannelGroupMonitorExecution{
		RunId: claim.RunId, GroupName: "default", ConfigRevision: claim.Config.Revision,
		Trigger: ChannelGroupMonitorTriggerScheduled, ProbeModel: "model-default",
		Result: ChannelGroupMonitorResultSuccess, StartedAt: created.NextRunAt,
		FinishedAt: stored.NextRunAt + 1, CreatedAt: stored.NextRunAt + 1,
	}
	createdExecution, err = SaveChannelGroupMonitorExecution(&late)
	require.NoError(t, err)
	assert.False(t, createdExecution)
	assert.Equal(t, ChannelGroupMonitorResultTimeout, late.Result)

	nextClaims, err := ClaimDueChannelGroupMonitor(stored.NextRunAt)
	require.NoError(t, err)
	require.NotNil(t, nextClaims)
	assert.NotEqual(t, claim.RunId, nextClaims.RunId)
	assert.Equal(t, stored.NextRunAt+60, nextClaims.DeadlineAt)
}
