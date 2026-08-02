package main

import (
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateSeedEnvironmentRequiresDevelopmentAndConfirmation(t *testing.T) {
	tests := []struct {
		name      string
		values    map[string]string
		wantError string
	}{
		{
			name: "valid sqlite target",
			values: map[string]string{
				"APP_ENV":              "development",
				seedConfirmEnvironment: seedConfirmToken,
				"SQLITE_PATH":          ":memory:",
			},
		},
		{
			name: "valid sql dsn target",
			values: map[string]string{
				"APP_ENV":              "development",
				seedConfirmEnvironment: seedConfirmToken,
				"SQL_DSN":              "local",
			},
		},
		{
			name: "production",
			values: map[string]string{
				"APP_ENV":              "production",
				seedConfirmEnvironment: seedConfirmToken,
				"SQLITE_PATH":          ":memory:",
			},
			wantError: "仅允许",
		},
		{
			name: "missing confirmation",
			values: map[string]string{
				"APP_ENV":     "development",
				"SQLITE_PATH": ":memory:",
			},
			wantError: seedConfirmEnvironment,
		},
		{
			name: "missing database target",
			values: map[string]string{
				"APP_ENV":              "development",
				seedConfirmEnvironment: seedConfirmToken,
			},
			wantError: "SQL_DSN 或 SQLITE_PATH",
		},
		{
			name: "ambiguous database target",
			values: map[string]string{
				"APP_ENV":              "development",
				seedConfirmEnvironment: seedConfirmToken,
				"SQL_DSN":              "mysql://development",
				"SQLITE_PATH":          ":memory:",
			},
			wantError: "只能设置一个",
		},
		{
			name: "slave node",
			values: map[string]string{
				"APP_ENV":              "development",
				seedConfirmEnvironment: seedConfirmToken,
				"SQLITE_PATH":          ":memory:",
				"NODE_TYPE":            "slave",
			},
			wantError: "从节点",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSeedEnvironment(func(key string) string { return test.values[key] })
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestSeedAcceptanceDataIsRepeatableAndUsesSharedChannelModelSamples(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.ChannelRatioMonitor{},
		&model.ChannelSmartScheduleRouteState{},
		&model.ChannelSmartScheduleModelSampleState{},
	))

	firstIDs, err := seedAcceptanceData(db, 10_000)
	require.NoError(t, err)
	require.Len(t, firstIDs, 10)
	secondIDs, err := seedAcceptanceData(db, 20_000)
	require.NoError(t, err)
	assert.Equal(t, firstIDs, secondIDs)

	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Where("name LIKE ?", seedChannelNamePrefix+"%").Count(&channelCount).Error)
	assert.Equal(t, int64(10), channelCount)
	var abilityCount int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id IN ?", firstIDs).Count(&abilityCount).Error)
	assert.Equal(t, int64(40), abilityCount)
	var routeCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleRouteState{}).Where("channel_id IN ?", firstIDs).Count(&routeCount).Error)
	assert.Equal(t, int64(40), routeCount)
	var sharedSampleCount int64
	require.NoError(t, db.Model(&model.ChannelSmartScheduleModelSampleState{}).Where("channel_id IN ?", firstIDs).Count(&sharedSampleCount).Error)
	assert.Equal(t, int64(20), sharedSampleCount)
	var rankedAbilities []model.Ability
	require.NoError(t, db.Where(&model.Ability{Group: seedGroups[0], Model: seedModels[0]}).Order("priority ASC").Find(&rankedAbilities).Error)
	require.Len(t, rankedAbilities, 10)
	for index, ability := range rankedAbilities {
		require.NotNil(t, ability.Priority)
		assert.Equal(t, int64(index+1), *ability.Priority)
	}

	var jitterState model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where("channel_id = ? AND model_name = ?", firstIDs[6], seedModels[0]).First(&jitterState).Error)
	metrics := jitterState.MetricsSince(0)
	assert.Equal(t, int64(24), metrics.SampleCount)
	assert.Equal(t, int64(1), metrics.FailureCount)
	require.NotNil(t, metrics.FirstTokenP95Ms)
	assert.Greater(t, *metrics.FirstTokenP95Ms, 10_000.0)
	manualMetrics := jitterState.ManualTestMetricsSince(0)
	assert.Equal(t, int64(12), manualMetrics.SampleCount)
	assert.Equal(t, int64(1), manualMetrics.FailureCount)

	var insufficientState model.ChannelSmartScheduleModelSampleState
	require.NoError(t, db.Where("channel_id = ? AND model_name = ?", firstIDs[9], seedModels[0]).First(&insufficientState).Error)
	assert.Equal(t, int64(2), insufficientState.SampleCount)
}

func TestSeedAcceptanceDataRollsBackWhenASeedNameIsAmbiguous(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.ChannelRatioMonitor{},
		&model.ChannelSmartScheduleRouteState{},
		&model.ChannelSmartScheduleModelSampleState{},
	))

	duplicateName := seedChannelNamePrefix + " 渠道 02 - 稳定低成本"
	require.NoError(t, db.Create(&model.Channel{Name: duplicateName}).Error)
	require.NoError(t, db.Create(&model.Channel{Name: duplicateName}).Error)

	channelIDs, err := seedAcceptanceData(db, 10_000)
	assert.ErrorContains(t, err, "多个同名验收渠道")
	assert.Nil(t, channelIDs)
	var channelCount int64
	require.NoError(t, db.Model(&model.Channel{}).Where("name LIKE ?", seedChannelNamePrefix+"%").Count(&channelCount).Error)
	assert.Equal(t, int64(2), channelCount)
	var abilityCount int64
	require.NoError(t, db.Model(&model.Ability{}).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}
