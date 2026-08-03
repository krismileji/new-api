package model

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelMonitorStatusTransitionRequiresCurrentMonitorRevision(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}))

	priority := int64(10)
	weight := uint(100)
	channel := Channel{
		Id: 9012, Name: "monitor revision guard", Status: common.ChannelStatusEnabled,
		Priority: &priority, Weight: &weight,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		ChannelId: channel.Id, Group: "default", Model: "gpt-test",
		Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId: channel.Id, UpstreamRevision: 2,
	}).Error)

	changed, revisionCurrent, err := UpdateChannelMonitorStatusIfCurrentRevision(
		channel.Id,
		1,
		common.ChannelStatusEnabled,
		"",
		common.ChannelStatusAutoDisabled,
		"stale decision",
	)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.False(t, revisionCurrent)

	storedChannel, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, storedChannel.Status)

	changed, revisionCurrent, err = UpdateChannelMonitorStatusIfCurrentRevision(
		channel.Id,
		2,
		common.ChannelStatusEnabled,
		"",
		common.ChannelStatusAutoDisabled,
		"current decision",
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, revisionCurrent)

	storedChannel, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
	assert.Equal(t, "current decision", storedChannel.GetOtherInfo()["status_reason"])
}

func TestChannelMonitorStatusSnapshotRejectsAdministratorABA(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	priority := int64(10)
	weight := uint(100)
	channel := Channel{
		Id: 9013, Name: "monitor status ABA", Status: common.ChannelStatusEnabled,
		Priority: &priority, Weight: &weight,
	}
	channel.SetOtherInfo(map[string]any{
		"status_reason":                 "manual operation",
		"status_time":                   int64(123),
		channelMonitorStatusRevisionKey: "initial",
	})
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		ChannelId: channel.Id, Group: "default", Model: "gpt-test",
		Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId: channel.Id, UpstreamRevision: 3,
	}).Error)

	expected := CaptureChannelMonitorStatus(&channel)
	require.True(t, UpdateChannelStatus(
		channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation",
	))
	require.True(t, UpdateChannelStatus(
		channel.Id, "", common.ChannelStatusEnabled, "manual operation",
	))

	current, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	info := current.GetOtherInfo()
	info["status_reason"] = expected.Reason
	info["status_time"] = expected.ChangedAt
	current.SetOtherInfo(info)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).
		Update("other_info", current.OtherInfo).Error)
	current, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, expected.Status, current.Status)
	assert.Equal(t, expected.Reason, CaptureChannelMonitorStatus(current).Reason)
	assert.Equal(t, expected.ChangedAt, CaptureChannelMonitorStatus(current).ChangedAt)
	assert.NotEqual(t, expected.Revision, CaptureChannelMonitorStatus(current).Revision)

	changed, revisionCurrent, _, err := UpdateChannelMonitorStatusIfSnapshotRevision(
		channel.Id,
		3,
		expected,
		common.ChannelStatusAutoDisabled,
		"stale automatic decision",
	)
	require.NoError(t, err)
	assert.True(t, revisionCurrent)
	assert.False(t, changed)

	current, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, current.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestChannelMonitorStatusSnapshotRejectsTagStatusABA(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}))

	tag := "status-aba"
	priority := int64(10)
	weight := uint(100)
	channel := Channel{
		Id: 9014, Name: "tag status ABA", Tag: &tag, Status: common.ChannelStatusEnabled,
		Priority: &priority, Weight: &weight,
	}
	channel.SetOtherInfo(map[string]any{
		"status_reason":                 "管理员标签批量操作",
		"status_time":                   int64(456),
		channelMonitorStatusRevisionKey: "initial-tag",
	})
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		ChannelId: channel.Id, Group: "default", Model: "gpt-test", Tag: &tag,
		Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId: channel.Id, UpstreamRevision: 4,
	}).Error)

	expected := CaptureChannelMonitorStatus(&channel)
	require.NoError(t, DisableChannelByTag(tag))
	require.NoError(t, EnableChannelByTag(tag))

	current, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	info := current.GetOtherInfo()
	info["status_reason"] = expected.Reason
	info["status_time"] = expected.ChangedAt
	current.SetOtherInfo(info)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).
		Update("other_info", current.OtherInfo).Error)
	current, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, expected.Status, current.Status)
	assert.NotEqual(t, expected.Revision, CaptureChannelMonitorStatus(current).Revision)

	changed, revisionCurrent, _, err := UpdateChannelMonitorStatusIfSnapshotRevision(
		channel.Id,
		4,
		expected,
		common.ChannelStatusAutoDisabled,
		"stale automatic decision",
	)
	require.NoError(t, err)
	assert.True(t, revisionCurrent)
	assert.False(t, changed)
}

func TestDisableChannelByTagRollsBackChannelWhenAbilityUpdateFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	tag := "atomic-tag-disable"
	priority := int64(10)
	channel := Channel{Id: 9015, Name: "tag atomic", Tag: &tag, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "default", Model: "gpt-test", Tag: &tag,
		Enabled: true, Priority: &priority, Weight: 100,
	}).Error)

	forcedErr := errors.New("forced ability update failure")
	callbackName := "test:fail_tag_ability_status_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			tx.AddError(forcedErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	err := DisableChannelByTag(tag)
	require.ErrorIs(t, err, forcedErr)
	require.NoError(t, db.Callback().Update().Remove(callbackName))

	stored, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Empty(t, CaptureChannelMonitorStatus(stored).Revision)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestUpdateChannelStatusRollsBackBeforePublishingCacheWhenAbilityUpdateFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	channelSyncLock.Lock()
	originalGroupRoutes := group2model2channels
	originalChannels := channelsIDM
	originalAdvancedConfigs := channel2advancedCustomConfig
	originalSmartRoutes := channelSmartScheduleRouteCache
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelSyncLock.Lock()
		group2model2channels = originalGroupRoutes
		channelsIDM = originalChannels
		channel2advancedCustomConfig = originalAdvancedConfigs
		channelSmartScheduleRouteCache = originalSmartRoutes
		channelSyncLock.Unlock()
	})

	priority := int64(10)
	channel := Channel{
		Id: 9024, Name: "atomic cached status", Status: common.ChannelStatusEnabled,
		Group: "default", Models: "gpt-test", Priority: &priority,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "default", Model: "gpt-test",
		Enabled: true, Priority: &priority, Weight: 100,
	}).Error)
	InitChannelCache()

	forcedErr := errors.New("forced cached ability update failure")
	callbackName := "test:fail_cached_ability_status_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			tx.AddError(forcedErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	changed := UpdateChannelStatus(
		channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation",
	)
	assert.False(t, changed)
	require.NoError(t, db.Callback().Update().Remove(callbackName))

	stored, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Empty(t, CaptureChannelMonitorStatus(stored).Revision)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, cached.Status)
}

func TestUpdateChannelStatusRestoresEnabledMultiKeyWithoutDisablingAbility(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	priority := int64(10)
	channel := Channel{
		Id: 9025, Name: "multi-key recovery", Key: "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeySize:           2,
			MultiKeyStatusList:     map[int]int{0: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason: map[int]string{0: "temporary failure"},
			MultiKeyDisabledTime:   map[int]int64{0: 123},
		},
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "default", Model: "gpt-test",
		Enabled: true, Priority: &priority, Weight: 100,
	}).Error)

	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))
	stored, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyStatusList, 0)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyDisabledReason, 0)
	assert.NotContains(t, stored.ChannelInfo.MultiKeyDisabledTime, 0)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestUpdateChannelStatusManualMultiKeyRecoveryClearsAllKeyFailures(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	priority := int64(10)
	channel := Channel{
		Id: 9026, Name: "manual multi-key recovery", Key: "key-a\nkey-b",
		Status: common.ChannelStatusAutoDisabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeySize:           2,
			MultiKeyStatusList:     map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled},
			MultiKeyDisabledReason: map[int]string{0: "failure a", 1: "failure b"},
			MultiKeyDisabledTime:   map[int]int64{0: 123, 1: 456},
		},
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "default", Model: "gpt-test",
		Enabled: false, Priority: &priority, Weight: 100,
	}).Error)

	require.True(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusEnabled, "manual operation"))
	stored, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Empty(t, stored.ChannelInfo.MultiKeyStatusList)
	assert.Empty(t, stored.ChannelInfo.MultiKeyDisabledReason)
	assert.Empty(t, stored.ChannelInfo.MultiKeyDisabledTime)
	var ability Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestUpdateChannelStatusSerializesWithDeleteWhenCacheDisabled(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := Channel{Id: 9016, Name: "status delete serialization", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)

	statusRead := make(chan struct{})
	releaseStatus := make(chan struct{})
	callbackName := "test:block_status_after_channel_read"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "channels" {
			return
		}
		select {
		case <-statusRead:
			return
		default:
			close(statusRead)
			<-releaseStatus
		}
	}))
	callbackRegistered := true
	t.Cleanup(func() {
		if callbackRegistered {
			_ = db.Callback().Query().Remove(callbackName)
		}
	})

	statusDone := make(chan bool, 1)
	go func() {
		statusDone <- UpdateChannelStatus(
			channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation",
		)
	}()
	<-statusRead

	type deleteResult struct {
		count int64
		err   error
	}
	deleteStarted := make(chan struct{})
	deleteDone := make(chan deleteResult, 1)
	go func() {
		close(deleteStarted)
		count, err := BatchDeleteChannels([]int{channel.Id})
		deleteDone <- deleteResult{count: count, err: err}
	}()
	<-deleteStarted

	var earlyDelete *deleteResult
	select {
	case result := <-deleteDone:
		earlyDelete = &result
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseStatus)
	require.True(t, <-statusDone)
	require.NoError(t, db.Callback().Query().Remove(callbackName))
	callbackRegistered = false

	result := deleteResult{}
	if earlyDelete == nil {
		result = <-deleteDone
	} else {
		result = *earlyDelete
	}
	require.NoError(t, result.err)
	assert.Equal(t, int64(1), result.count)
	assert.Nil(t, earlyDelete, "删除不应绕过状态更新持有的渠道状态锁")

	var count int64
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateChannelStatusDoesNotRecreateDeletedChannel(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })

	channel := Channel{Id: 9017, Name: "status delete race", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	deletedCount, err := BatchDeleteChannels([]int{channel.Id})
	require.NoError(t, err)
	require.Equal(t, int64(1), deletedCount)

	changed := UpdateChannelStatus(
		channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation",
	)
	assert.False(t, changed)

	var count int64
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func TestChannelMonitorAutomaticRecoveryKeepsFixedPrimaryHighest(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	fixedPriority := int64(50)
	recoveredPriority := int64(200)
	fixedWeight := uint(100)
	recoveredWeight := uint(100)
	recovering := Channel{
		Id: 9021, Name: "recovering higher priority", Status: common.ChannelStatusAutoDisabled,
		Group: "vip", Models: "model-a", Priority: &recoveredPriority, Weight: &recoveredWeight,
	}
	recovering.SetOtherInfo(map[string]any{
		"status_reason": "temporary upstream failure",
		"status_time":   int64(100),
	})
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9020, Name: "fixed primary", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a", Priority: &fixedPriority, Weight: &fixedWeight},
		recovering,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9020, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: fixedWeight},
		{ChannelId: 9021, Group: "vip", Model: "model-a", Enabled: false, Priority: &recoveredPriority, Weight: recoveredWeight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 9020, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 9021, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	require.NoError(t, db.Create(&ChannelRatioMonitor{ChannelId: 9021, UpstreamRevision: 7}).Error)
	_, err := SaveChannelSmartScheduleRoutePrimary(
		9020, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)

	current, err := GetChannelById(9021, true)
	require.NoError(t, err)
	changed, revisionCurrent, _, err := UpdateChannelMonitorStatusIfSnapshotRevision(
		9021, 7, CaptureChannelMonitorStatus(current), common.ChannelStatusEnabled, "",
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, revisionCurrent)

	var fixedAbility Ability
	var recoveredAbility Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 9020, Group: "vip", Model: "model-a"}).First(&fixedAbility).Error)
	require.NoError(t, db.Where(&Ability{ChannelId: 9021, Group: "vip", Model: "model-a"}).First(&recoveredAbility).Error)
	assert.True(t, recoveredAbility.Enabled)
	assert.Greater(t, abilityPriority(fixedAbility), abilityPriority(recoveredAbility))
	assert.Equal(t, uint(1000), fixedAbility.Weight)
}

func TestEnableChannelByTagKeepsFixedPrimaryHighest(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	tag := "recover-fixed-primary"
	fixedPriority := int64(50)
	recoveredPriority := int64(200)
	fixedWeight := uint(100)
	recoveredWeight := uint(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9022, Name: "fixed primary", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a", Priority: &fixedPriority, Weight: &fixedWeight},
		{Id: 9023, Name: "tagged higher priority", Tag: &tag, Status: common.ChannelStatusManuallyDisabled,
			Group: "vip", Models: "model-a", Priority: &recoveredPriority, Weight: &recoveredWeight},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9022, Group: "vip", Model: "model-a", Enabled: true, Priority: &fixedPriority, Weight: fixedWeight},
		{ChannelId: 9023, Group: "vip", Model: "model-a", Tag: &tag, Enabled: false, Priority: &recoveredPriority, Weight: recoveredWeight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 9022, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 9023, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	_, err := SaveChannelSmartScheduleRoutePrimary(
		9022, "vip", "model-a", ChannelSmartScheduleRoutePrimaryOptions{DurationMinutes: 10},
	)
	require.NoError(t, err)

	require.NoError(t, EnableChannelByTag(tag))
	var recoveredChannel Channel
	require.NoError(t, db.First(&recoveredChannel, 9023).Error)
	assert.Equal(t, common.ChannelStatusEnabled, recoveredChannel.Status)
	var fixedAbility Ability
	var recoveredAbility Ability
	require.NoError(t, db.Where(&Ability{ChannelId: 9022, Group: "vip", Model: "model-a"}).First(&fixedAbility).Error)
	require.NoError(t, db.Where(&Ability{ChannelId: 9023, Group: "vip", Model: "model-a"}).First(&recoveredAbility).Error)
	assert.True(t, recoveredAbility.Enabled)
	assert.Greater(t, abilityPriority(fixedAbility), abilityPriority(recoveredAbility))
	assert.Equal(t, uint(1000), fixedAbility.Weight)
}
