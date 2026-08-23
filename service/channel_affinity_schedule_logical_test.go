package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupLogicalAffinityCooldownTest(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.MemoryCacheEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Setenv(model.ChannelLogicalGroupGlobalEnableEnv, "true")
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.ChannelSmartScheduleRouteState{},
		&model.ChannelSmartScheduleGroupPause{},
		&model.ChannelLogicalGroup{},
		&model.ChannelLogicalGroupMember{},
	))

	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalEnabled, hadEnabled := common.OptionMap["ChannelMonitorSmartScheduleEnabled"]
	originalPolicies, hadPolicies := common.OptionMap[model.ChannelMonitorSmartScheduleGroupPoliciesOption]
	common.OptionMap["ChannelMonitorSmartScheduleEnabled"] = "true"
	common.OptionMap[model.ChannelMonitorSmartScheduleGroupPoliciesOption] = `[{"group":"vip","models":["model-a"]}]`
	common.OptionMapRWMutex.Unlock()
	ClearChannelRateLimitCooldowns()

	t.Cleanup(func() {
		ClearChannelRateLimitCooldowns()
		common.OptionMapRWMutex.Lock()
		if hadEnabled {
			common.OptionMap["ChannelMonitorSmartScheduleEnabled"] = originalEnabled
		} else {
			delete(common.OptionMap, "ChannelMonitorSmartScheduleEnabled")
		}
		if hadPolicies {
			common.OptionMap[model.ChannelMonitorSmartScheduleGroupPoliciesOption] = originalPolicies
		} else {
			delete(common.OptionMap, model.ChannelMonitorSmartScheduleGroupPoliciesOption)
		}
		common.OptionMapRWMutex.Unlock()
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func seedLogicalAffinityCooldownTest(t *testing.T, db *gorm.DB, firstStability string) {
	t.Helper()
	logicalID := int64(9800)
	priority := int64(100)
	require.NoError(t, db.Create(&model.ChannelLogicalGroup{
		Id: logicalID, Name: "affinity-cooldown",
		Status: model.ChannelLogicalGroupStatusEnabled, Revision: 1,
	}).Error)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 9801, Name: "preferred-a", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
		{Id: 9802, Name: "member-b", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
	}).Error)
	fingerprint := strings.Repeat("a", 64)
	require.NoError(t, db.Create(&[]model.ChannelLogicalGroupMember{
		{LogicalGroupID: logicalID, ChannelID: 9801, Weight: 100, AddressFingerprint: fingerprint},
		{LogicalGroupID: logicalID, ChannelID: 9802, Weight: 0, AddressFingerprint: fingerprint},
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{ChannelId: 9801, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: 9802, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelSmartScheduleRouteState{
		{
			ChannelId: 9801, GroupName: "vip", ModelName: "model-a",
			ParticipationSet: true, StabilityState: firstStability,
		},
		{ChannelId: 9802, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
}

func TestLogicalAffinitySelectsSiblingWhenPreferredMemberIsCoolingDown(t *testing.T) {
	db := setupLogicalAffinityCooldownTest(t)
	seedLogicalAffinityCooldownTest(t, db, "")
	StartChannelRateLimitCooldown(9801, "model-a", 60)

	assert.Equal(t, model.ChannelSmartScheduleAffinityEligible, PreferredChannelAffinityStatus(
		"vip", "model-a", 9801, "/v1/chat/completions",
	))
	selected, err := SelectPreferredChannelAffinityMember(
		"vip", "model-a", 9801, "/v1/chat/completions",
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9802, selected.Id)
}

func TestLogicalAffinitySelectsEligibleSiblingInsidePinnedCandidate(t *testing.T) {
	db := setupLogicalAffinityCooldownTest(t)
	seedLogicalAffinityCooldownTest(t, db, model.ChannelSmartScheduleStabilityDegraded)

	assert.Equal(t, model.ChannelSmartScheduleAffinityEligible, PreferredChannelAffinityStatus(
		"vip", "model-a", 9801, "/v1/chat/completions",
	))
	selected, err := SelectPreferredChannelAffinityMember(
		"vip", "model-a", 9801, "/v1/chat/completions",
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9802, selected.Id)
}

func TestLogicalAffinityDoesNotSelectCoolingSibling(t *testing.T) {
	db := setupLogicalAffinityCooldownTest(t)
	seedLogicalAffinityCooldownTest(t, db, "")
	StartChannelRateLimitCooldown(9802, "model-a", 60)

	assert.Equal(t, model.ChannelSmartScheduleAffinityEligible, PreferredChannelAffinityStatus(
		"vip", "model-a", 9801, "/v1/chat/completions",
	))
	selected, err := SelectPreferredChannelAffinityMember(
		"vip", "model-a", 9801, "/v1/chat/completions",
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, 9801, selected.Id)
}

func TestLogicalAffinityIsUnavailableWhenEveryMemberIsCoolingDown(t *testing.T) {
	db := setupLogicalAffinityCooldownTest(t)
	seedLogicalAffinityCooldownTest(t, db, "")
	StartChannelRateLimitCooldown(9801, "model-a", 60)
	StartChannelRateLimitCooldown(9802, "model-a", 60)

	assert.Equal(t, model.ChannelSmartScheduleAffinityTemporarilyUnavailable, PreferredChannelAffinityStatus(
		"vip", "model-a", 9801, "/v1/chat/completions",
	))
	selected, err := SelectPreferredChannelAffinityMember(
		"vip", "model-a", 9801, "/v1/chat/completions",
	)
	assert.ErrorIs(t, err, model.ErrLogicalChannelSelectionNoAvailableMembers)
	assert.Nil(t, selected)
}
