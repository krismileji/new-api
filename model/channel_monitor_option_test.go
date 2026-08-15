package model

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func setupChannelMonitorOptionTestDB(t *testing.T, ratios string, coefficients string) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{
		"GroupRatio":                          ratios,
		ChannelMonitorGroupCoefficientsOption: coefficients,
	}
	common.OptionMapRWMutex.Unlock()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, originalLogDatabaseType)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(
		&Option{},
		&Channel{},
		&Ability{},
		&ChannelRatioMonitor{},
		&ChannelSmartScheduleRouteState{},
	))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(ratios))
	require.NoError(t, db.Create([]Option{
		{Key: "GroupRatio", Value: ratios},
		{Key: ChannelMonitorGroupCoefficientsOption, Value: coefficients},
	}).Error)

	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalDatabaseType, originalLogDatabaseType)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestMergeChannelMonitorGroupOptionsPreservesConcurrentGroupUpdates(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"existing":1}`, `{"existing":1.1}`)

	const updateCount = 12
	start := make(chan struct{})
	errorsByUpdate := make(chan error, updateCount)
	var updates sync.WaitGroup
	for index := range updateCount {
		updates.Add(1)
		go func() {
			defer updates.Done()
			<-start
			group := fmt.Sprintf("group-%02d", index)
			_, err := MergeChannelMonitorGroupOptions(
				map[string]float64{group: float64(index + 2)},
				map[string]float64{group: float64(index+2) / 10},
				false,
			)
			errorsByUpdate <- err
		}()
	}
	close(start)
	updates.Wait()
	close(errorsByUpdate)
	for err := range errorsByUpdate {
		require.NoError(t, err)
	}

	var ratioOption Option
	require.NoError(t, db.Where("key = ?", "GroupRatio").First(&ratioOption).Error)
	groupRatios := map[string]float64{}
	require.NoError(t, common.UnmarshalJsonStr(ratioOption.Value, &groupRatios))
	assert.Equal(t, float64(1), groupRatios["existing"])
	for index := range updateCount {
		assert.Equal(t, float64(index+2), groupRatios[fmt.Sprintf("group-%02d", index)])
	}
	assert.Equal(t, groupRatios, ratio_setting.GetGroupRatioCopy())

	var coefficientOption Option
	require.NoError(t, db.Where("key = ?", ChannelMonitorGroupCoefficientsOption).First(&coefficientOption).Error)
	coefficients := map[string]float64{}
	require.NoError(t, common.UnmarshalJsonStr(coefficientOption.Value, &coefficients))
	assert.Equal(t, 1.1, coefficients["existing"])
	for index := range updateCount {
		assert.Equal(t, float64(index+2)/10, coefficients[fmt.Sprintf("group-%02d", index)])
	}
	common.OptionMapRWMutex.RLock()
	runtimeCoefficients := common.OptionMap[ChannelMonitorGroupCoefficientsOption]
	common.OptionMapRWMutex.RUnlock()
	assert.JSONEq(t, coefficientOption.Value, runtimeCoefficients)
}

func TestMergeChannelMonitorGroupOptionsDoesNotLowerNewerPolicyRatio(t *testing.T) {
	setupChannelMonitorOptionTestDB(t, `{"vip":3}`, `{}`)

	updated, err := MergeChannelMonitorGroupOptions(
		map[string]float64{"vip": 2, "missing": 0.5},
		nil,
		true,
	)
	require.NoError(t, err)
	assert.Zero(t, updated)
	assert.Equal(t, map[string]float64{"vip": 3}, ratio_setting.GetGroupRatioCopy())
}

func TestMergeChannelMonitorGroupOptionsTreatsNullMapsAsEmpty(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `null`, `null`)

	_, err := MergeChannelMonitorGroupOptions(
		map[string]float64{"vip": 2},
		map[string]float64{"vip": 1.1},
		false,
	)
	require.NoError(t, err)

	var ratioOption, coefficientOption Option
	require.NoError(t, db.Where("key = ?", "GroupRatio").First(&ratioOption).Error)
	require.NoError(t, db.Where("key = ?", ChannelMonitorGroupCoefficientsOption).First(&coefficientOption).Error)
	assert.JSONEq(t, `{"vip":2}`, ratioOption.Value)
	assert.JSONEq(t, `{"vip":1.1}`, coefficientOption.Value)
}

func TestMergeChannelMonitorGroupOptionsIfCurrentSkipsOnlyStaleGroups(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1,"standard":1}`, `{}`)
	require.NoError(t, db.Create(&[]ChannelRatioMonitor{
		{ChannelId: 1, UpstreamRevision: 2},
		{ChannelId: 2, UpstreamRevision: 1},
	}).Error)

	updated, err := MergeChannelMonitorGroupOptionsIfCurrent(
		map[string]float64{"vip": 2, "standard": 3},
		nil,
		true,
		ChannelMonitorGroupRatioRevisionGuard{
			"vip":      {1: 1},
			"standard": {2: 1},
		},
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	assert.Equal(t, map[string]float64{"vip": 1, "standard": 3}, ratio_setting.GetGroupRatioCopy())
}

func TestMergeChannelMonitorGroupOptionsIfCurrentAppliesCurrentGroup(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Create(&ChannelRatioMonitor{ChannelId: 1, UpstreamRevision: 2}).Error)

	updated, err := MergeChannelMonitorGroupOptionsIfCurrent(
		map[string]float64{"vip": 2},
		nil,
		true,
		ChannelMonitorGroupRatioRevisionGuard{"vip": {1: 2}},
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, updated)
	assert.Equal(t, map[string]float64{"vip": 2}, ratio_setting.GetGroupRatioCopy())
}

func TestMergeChannelMonitorGroupOptionsIfCurrentSkipsChangedMembership(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Create(&Channel{Id: 1, Name: "membership", Group: "standard"}).Error)
	require.NoError(t, db.Create(&ChannelRatioMonitor{ChannelId: 1, UpstreamRevision: 2}).Error)

	updated, err := MergeChannelMonitorGroupOptionsIfCurrent(
		map[string]float64{"vip": 2},
		nil,
		true,
		ChannelMonitorGroupRatioRevisionGuard{"vip": {1: 2}},
		ChannelMonitorGroupRatioMembershipGuard{"vip": {1: "vip"}},
		ChannelMonitorGroupRatioStatusGuard{"vip": {1: common.ChannelStatusEnabled}},
		nil,
	)
	require.NoError(t, err)
	assert.Zero(t, updated)
	assert.Equal(t, map[string]float64{"vip": 1}, ratio_setting.GetGroupRatioCopy())
}

func TestMergeChannelMonitorGroupOptionsIfCurrentGuardsPolicyValuesPerGroup(t *testing.T) {
	tests := []struct {
		name              string
		adminRatios       map[string]float64
		adminCoefficients map[string]float64
		wantUpdated       int
		wantRatios        map[string]float64
	}{
		{
			name:        "same group ratio changed",
			adminRatios: map[string]float64{"vip": 0.75},
			wantRatios:  map[string]float64{"vip": 0.75, "standard": 1},
		},
		{
			name:              "same group coefficient changed",
			adminCoefficients: map[string]float64{"vip": 1.25},
			wantRatios:        map[string]float64{"vip": 1, "standard": 1},
		},
		{
			name:              "unrelated group values changed",
			adminRatios:       map[string]float64{"standard": 2},
			adminCoefficients: map[string]float64{"standard": 1.25},
			wantUpdated:       1,
			wantRatios:        map[string]float64{"vip": 3, "standard": 2},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupChannelMonitorOptionTestDB(t, `{"vip":1,"standard":1}`, `{}`)
			_, err := MergeChannelMonitorGroupOptions(
				test.adminRatios,
				test.adminCoefficients,
				false,
			)
			require.NoError(t, err)

			updated, err := MergeChannelMonitorGroupOptionsIfCurrent(
				map[string]float64{"vip": 3},
				nil,
				true,
				nil,
				nil,
				nil,
				ChannelMonitorGroupRatioValueGuard{
					"vip": {Ratio: 1, Coefficient: 1},
				},
			)
			require.NoError(t, err)
			assert.Equal(t, test.wantUpdated, updated)
			assert.Equal(t, test.wantRatios, ratio_setting.GetGroupRatioCopy())
		})
	}
}

func TestMergeChannelMonitorGroupOptionsRollsBackBothOptions(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Model(&Option{}).
		Where("key = ?", ChannelMonitorGroupCoefficientsOption).
		Update("value", "not-json").Error)

	_, err := MergeChannelMonitorGroupOptions(
		map[string]float64{"vip": 2},
		map[string]float64{"vip": 1.2},
		false,
	)
	require.ErrorContains(t, err, "解析 ChannelMonitorGroupCoefficients 失败")

	var ratioOption Option
	require.NoError(t, db.Where("key = ?", "GroupRatio").First(&ratioOption).Error)
	assert.JSONEq(t, `{"vip":1}`, ratioOption.Value)
	assert.Equal(t, map[string]float64{"vip": 1}, ratio_setting.GetGroupRatioCopy())
}

func TestUpdateChannelMonitorSettingsOptionsRollsBackWhenTemporaryTrafficCleanupFails(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Create(&Option{Key: "test-setting", Value: "old"}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap["test-setting"] = "old"
	common.OptionMapRWMutex.Unlock()

	priority := int64(90)
	require.NoError(t, db.Create(&Channel{Id: 1, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1,
		Group:     "vip",
		Model:     "model-a",
		Enabled:   true,
		Priority:  &priority,
		Weight:    50,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId:            1,
		GroupName:            "vip",
		ModelName:            "model-a",
		Revision:             math.MaxInt64,
		BasePriority:         80,
		BaseWeight:           10,
		TemporaryTrafficKind: "exploration",
	}).Error)

	routingChanged, err := UpdateChannelMonitorSettingsOptions(
		map[string]string{"test-setting": "new"},
		true,
		nil,
	)
	require.ErrorContains(t, err, "修订号已达上限")
	assert.False(t, routingChanged)

	var setting Option
	require.NoError(t, db.Where("key = ?", "test-setting").First(&setting).Error)
	assert.Equal(t, "old", setting.Value)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "old", common.OptionMap["test-setting"])
	common.OptionMapRWMutex.RUnlock()

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1).First(&state).Error)
	assert.Equal(t, "exploration", state.TemporaryTrafficKind)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", 1).First(&ability).Error)
	assert.Equal(t, int64(90), abilityPriority(ability))
	assert.Equal(t, uint(50), ability.Weight)
}

func TestUpdateChannelMonitorSettingsOptionsCommitsWithTemporaryTrafficCleanup(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Create(&Option{Key: "test-setting", Value: "old"}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap["test-setting"] = "old"
	common.OptionMapRWMutex.Unlock()

	priority := int64(90)
	require.NoError(t, db.Create(&Channel{Id: 1, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1,
		Group:     "vip",
		Model:     "model-a",
		Enabled:   true,
		Priority:  &priority,
		Weight:    50,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId:                     1,
		GroupName:                     "vip",
		ModelName:                     "model-a",
		Revision:                      1,
		BasePriority:                  80,
		BaseWeight:                    10,
		TemporaryTrafficKind:          "exploration",
		TemporaryTrafficSince:         100,
		TemporaryTrafficTargetPercent: 5,
	}).Error)

	routingChanged, err := UpdateChannelMonitorSettingsOptions(
		map[string]string{"test-setting": "new"},
		true,
		nil,
	)
	require.NoError(t, err)
	assert.True(t, routingChanged)

	var setting Option
	require.NoError(t, db.Where("key = ?", "test-setting").First(&setting).Error)
	assert.Equal(t, "new", setting.Value)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "new", common.OptionMap["test-setting"])
	common.OptionMapRWMutex.RUnlock()

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1).First(&state).Error)
	assert.Empty(t, state.TemporaryTrafficKind)
	assert.Zero(t, state.TemporaryTrafficSince)
	assert.Zero(t, state.TemporaryTrafficTargetPercent)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", 1).First(&ability).Error)
	assert.Equal(t, int64(80), abilityPriority(ability))
	assert.Equal(t, uint(10), ability.Weight)
}

func TestUpdateChannelMonitorSettingsOptionsReturnsStabilityProbeToDegradedState(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Create(&Option{Key: "test-setting", Value: "old"}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap["test-setting"] = "old"
	common.OptionMapRWMutex.Unlock()

	probePriority := int64(0)
	require.NoError(t, db.Create(&Channel{Id: 1, Status: common.ChannelStatusEnabled}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 1, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &probePriority, Weight: 10,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 1, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		StabilityState: ChannelSmartScheduleStabilityProbing, StabilitySince: 100,
		StabilitySavedPriority: 80, StabilitySavedWeight: 40,
		StabilityReleaseMaxPromptTokens: 1234,
	}).Error)

	routingChanged, err := UpdateChannelMonitorSettingsOptions(
		map[string]string{"test-setting": "new"},
		true,
		nil,
	)
	require.NoError(t, err)
	assert.True(t, routingChanged)

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1).First(&state).Error)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, state.StabilityState)
	assert.Zero(t, state.StabilitySince)
	assert.Equal(t, int64(80), state.StabilitySavedPriority)
	assert.Equal(t, uint(40), state.StabilitySavedWeight)
	assert.Zero(t, state.StabilityReleaseMaxPromptTokens)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", 1).First(&ability).Error)
	assert.Zero(t, abilityPriority(ability))
	assert.Zero(t, ability.Weight)
}

func TestUpdateChannelMonitorSettingsOptionsReappliesActiveManualPrimary(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Create(&Option{Key: "test-setting", Value: "old"}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap["test-setting"] = "old"
	common.OptionMapRWMutex.Unlock()

	temporaryPriority := int64(100)
	otherPriority := int64(90)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 1, Name: "fixed", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "other", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 1, Group: "vip", Model: "model-a", Enabled: true, Priority: &temporaryPriority, Weight: 5},
		{ChannelId: 2, Group: "vip", Model: "model-a", Enabled: true, Priority: &otherPriority, Weight: 50},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{
			ChannelId: 1, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1,
			BasePriority: 80, BaseWeight: 40,
			TemporaryTrafficKind: ChannelSmartScheduleTemporaryTrafficExploration,
			ManualPrimaryUntil:   common.GetTimestamp() + 600, ManualPrimarySaved: true,
			ManualPrimarySavedPriority: 80, ManualPrimarySavedWeight: 40,
		},
		{ChannelId: 2, GroupName: "vip", ModelName: "model-a", ParticipationSet: true, Revision: 1},
	}).Error)

	routingChanged, err := UpdateChannelMonitorSettingsOptions(
		map[string]string{"test-setting": "new"},
		true,
		nil,
	)
	require.NoError(t, err)
	assert.True(t, routingChanged)

	var state ChannelSmartScheduleRouteState
	require.NoError(t, db.Where("channel_id = ?", 1).First(&state).Error)
	assert.Empty(t, state.TemporaryTrafficKind)
	assert.Greater(t, state.ManualPrimaryUntil, common.GetTimestamp())
	assert.True(t, state.ManualPrimarySaved)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", 1).First(&ability).Error)
	assert.Equal(t, int64(91), abilityPriority(ability))
	assert.Equal(t, uint(1000), ability.Weight)
}

func TestUpdateChannelMonitorSettingsOptionsRejectsStaleSmartScheduleRevision(t *testing.T) {
	db := setupChannelMonitorOptionTestDB(t, `{"vip":1}`, `{}`)
	require.NoError(t, db.Create(&[]Option{
		{Key: ChannelSmartScheduleControlRevisionOption, Value: "revision-current"},
		{Key: ChannelMonitorSmartSchedulePerformanceWindowOption, Value: "60"},
	}).Error)
	common.OptionMapRWMutex.Lock()
	common.OptionMap[ChannelSmartScheduleControlRevisionOption] = "revision-stale"
	common.OptionMap[ChannelMonitorSmartSchedulePerformanceWindowOption] = "60"
	common.OptionMapRWMutex.Unlock()

	expectedRevision := "revision-stale"
	routingChanged, err := UpdateChannelMonitorSettingsOptions(
		map[string]string{
			ChannelSmartScheduleControlRevisionOption:          "revision-new",
			ChannelMonitorSmartSchedulePerformanceWindowOption: "120",
		},
		false,
		&expectedRevision,
	)
	require.ErrorIs(t, err, ErrChannelMonitorSettingsChanged)
	assert.False(t, routingChanged)

	var revision Option
	require.NoError(t, db.Where("key = ?", ChannelSmartScheduleControlRevisionOption).First(&revision).Error)
	assert.Equal(t, "revision-current", revision.Value)
	var performanceWindow Option
	require.NoError(t, db.Where("key = ?", ChannelMonitorSmartSchedulePerformanceWindowOption).First(&performanceWindow).Error)
	assert.Equal(t, "60", performanceWindow.Value)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "revision-current", common.OptionMap[ChannelSmartScheduleControlRevisionOption])
	assert.Equal(t, "60", common.OptionMap[ChannelMonitorSmartSchedulePerformanceWindowOption])
	common.OptionMapRWMutex.RUnlock()

	expectedRevision = "revision-current"
	routingChanged, err = UpdateChannelMonitorSettingsOptions(
		map[string]string{
			ChannelSmartScheduleControlRevisionOption:          "revision-new",
			ChannelMonitorSmartSchedulePerformanceWindowOption: "120",
		},
		false,
		&expectedRevision,
	)
	require.NoError(t, err)
	assert.False(t, routingChanged)
	require.NoError(t, db.Where("key = ?", ChannelMonitorSmartSchedulePerformanceWindowOption).First(&performanceWindow).Error)
	assert.Equal(t, "120", performanceWindow.Value)
}

func TestRefreshChannelSmartScheduleOptionsQuotesReservedKeyColumn(t *testing.T) {
	originalDB := DB
	originalDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	DB = db

	var generatedSQL string
	callbackName := "test:channel_smart_schedule_option_query"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		generatedSQL = tx.Statement.SQL.String()
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove(callbackName))
		DB = originalDB
		common.SetDatabaseTypes(originalDatabaseType, originalLogDatabaseType)
		initCol()
	})

	tests := []struct {
		name         string
		databaseType common.DatabaseType
		quotedKey    string
	}{
		{name: "MySQL", databaseType: common.DatabaseTypeMySQL, quotedKey: "`key`"},
		{name: "PostgreSQL", databaseType: common.DatabaseTypePostgreSQL, quotedKey: `"key"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			common.SetDatabaseTypes(test.databaseType, originalLogDatabaseType)
			initCol()
			generatedSQL = ""

			require.NoError(t, RefreshChannelSmartScheduleOptions())
			assert.Contains(t, generatedSQL, "WHERE "+test.quotedKey+" LIKE ?")
			assert.Contains(t, generatedSQL, "ORDER BY "+test.quotedKey+" ASC")
		})
	}
}
