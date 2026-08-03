package model

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedActiveFixedPrimary(t *testing.T, db *gorm.DB, channelId int) {
	t.Helper()
	priority := int64(100)
	weight := uint(40)
	require.NoError(t, db.Create(&Channel{
		Id: channelId, Name: "fixed primary", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channelId, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: channelId, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
		ManualPrimaryUntil: common.GetTimestamp() + 600, ManualPrimarySaved: true,
		ManualPrimarySavedPriority: priority, ManualPrimarySavedWeight: weight,
	}).Error)
}

func TestChannelInsertReappliesActiveFixedPrimary(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		insert func(Channel) error
	}{
		{
			name: "single",
			insert: func(channel Channel) error {
				return channel.Insert()
			},
		},
		{
			name: "batch",
			insert: func(channel Channel) error {
				return BatchInsertChannels([]Channel{channel})
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := setupChannelSmartScheduleRouteTestDB(t)
			seedActiveFixedPrimary(t, db, 9371)
			priority := int64(200)
			weight := uint(100)
			require.NoError(t, testCase.insert(Channel{
				Id: 9372, Name: "new higher route", Status: common.ChannelStatusEnabled,
				Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
			}))

			var fixed Ability
			require.NoError(t, db.Where(&Ability{
				ChannelId: 9371, Group: "vip", Model: "model-a",
			}).First(&fixed).Error)
			assert.Equal(t, int64(201), abilityPriority(fixed))
			assert.Equal(t, uint(1000), fixed.Weight)
		})
	}
}

func TestChannelInsertRollsBackWhenFixedPrimaryCannotBeReapplied(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		insert func(Channel) error
	}{
		{
			name: "single",
			insert: func(channel Channel) error {
				return channel.Insert()
			},
		},
		{
			name: "batch",
			insert: func(channel Channel) error {
				return BatchInsertChannels([]Channel{channel})
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db := setupChannelSmartScheduleRouteTestDB(t)
			seedActiveFixedPrimary(t, db, 9381)
			priority := int64(math.MaxInt64)
			weight := uint(100)
			err := testCase.insert(Channel{
				Id: 9382, Name: "unplaceable route", Status: common.ChannelStatusEnabled,
				Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
			})
			require.Error(t, err)

			var channelCount int64
			require.NoError(t, db.Model(&Channel{}).Where("id = ?", 9382).Count(&channelCount).Error)
			assert.Zero(t, channelCount)
			var abilityCount int64
			require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 9382).Count(&abilityCount).Error)
			assert.Zero(t, abilityCount)
		})
	}
}

func blockModelCreates(t *testing.T, db *gorm.DB, modelName string) (<-chan struct{}, func()) {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	callbackName := "test:block_create_" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "_" + modelName
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != modelName {
			return
		}
		startedOnce.Do(func() { close(started) })
		<-release
	}))
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		_ = db.Callback().Create().Remove(callbackName)
	})
	return started, unblock
}

func TestChannelInsertRollsBackWhenAbilityCreationFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	callbackName := "test:fail_channel_insert_ability_create"
	wantErr := errors.New("ability create failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(wantErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	priority := int64(100)
	weight := uint(100)
	channel := Channel{
		Id: 9301, Name: "atomic insert", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}
	err := channel.Insert()
	require.ErrorIs(t, err, wantErr)

	var channelCount int64
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Count(&channelCount).Error)
	assert.Zero(t, channelCount)
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}

func TestChannelUpdateRollsBackWhenAbilityCreationFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	weight := uint(100)
	channel := Channel{
		Id: 9304, Name: "atomic update", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, channel.Insert())

	callbackName := "test:fail_channel_update_ability_create"
	wantErr := errors.New("ability rebuild failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(wantErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	channel.Group = "default"
	channel.Models = "model-b"
	err := channel.Update()
	require.ErrorIs(t, err, wantErr)

	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	assert.Equal(t, "vip", stored.Group)
	assert.Equal(t, "model-a", stored.Models)

	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "vip", abilities[0].Group)
	assert.Equal(t, "model-a", abilities[0].Model)
}

func TestChannelInsertAndDeleteCannotLeaveOrphanAbility(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	abilityCreateStarted, releaseAbilityCreate := blockModelCreates(t, db, "Ability")

	priority := int64(100)
	weight := uint(100)
	channel := Channel{
		Id: 9302, Name: "concurrent insert", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}
	insertResult := make(chan error, 1)
	go func() { insertResult <- channel.Insert() }()
	<-abilityCreateStarted

	lockWasFree := channelStatusLock.TryLock()
	if lockWasFree {
		channelStatusLock.Unlock()
	}
	assert.False(t, lockWasFree, "Channel.Insert must hold the channel lifecycle lock while creating abilities")

	deleteStarted := make(chan struct{})
	deleteCount := make(chan int64, 1)
	deleteResult := make(chan error, 1)
	go func() {
		close(deleteStarted)
		count, err := BatchDeleteChannels([]int{channel.Id})
		deleteCount <- count
		deleteResult <- err
	}()
	<-deleteStarted
	releaseAbilityCreate()

	require.NoError(t, <-insertResult)
	require.NoError(t, <-deleteResult)
	assert.Equal(t, int64(1), <-deleteCount)
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", channel.Id).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}

func TestBatchInsertChannelsSerializesWithDelete(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	abilityCreateStarted, releaseAbilityCreate := blockModelCreates(t, db, "Ability")
	priority := int64(100)
	weight := uint(100)
	channels := []Channel{{
		Id: 9304, Name: "batch insert", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}}

	insertResult := make(chan error, 1)
	go func() { insertResult <- BatchInsertChannels(channels) }()
	<-abilityCreateStarted
	lockWasFree := channelStatusLock.TryLock()
	if lockWasFree {
		channelStatusLock.Unlock()
	}
	assert.False(t, lockWasFree, "batch insert must hold the channel lifecycle lock")

	deleteResult := make(chan error, 1)
	go func() {
		_, err := BatchDeleteChannels([]int{9304})
		deleteResult <- err
	}()
	releaseAbilityCreate()
	require.NoError(t, <-insertResult)
	require.NoError(t, <-deleteResult)

	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 9304).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}

func TestBatchInsertChannelsRollsBackAllChannelsWhenAbilityCreationFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	weight := uint(100)
	channels := []Channel{
		{Id: 9305, Name: "batch first", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight},
		{Id: 9306, Name: "batch second", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight},
	}
	callbackName := "test:fail_batch_insert_second_ability"
	wantErr := errors.New("second batch ability failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Ability" {
			return
		}
		if abilities, ok := tx.Statement.Dest.(*[]Ability); ok && len(*abilities) > 0 && (*abilities)[0].ChannelId == 9306 {
			tx.AddError(wantErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	err := BatchInsertChannels(channels)
	require.ErrorIs(t, err, wantErr)
	var channelCount int64
	require.NoError(t, db.Model(&Channel{}).Where("id IN ?", []int{9305, 9306}).Count(&channelCount).Error)
	assert.Zero(t, channelCount)
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id IN ?", []int{9305, 9306}).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}

func TestBatchInsertChannelsDoesNotReportSuccessAfterPanic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	callbackName := "test:panic_batch_insert"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
			panic("batch insert panic")
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	priority := int64(100)
	weight := uint(100)
	require.Panics(t, func() {
		_ = BatchInsertChannels([]Channel{{
			Id: 9307, Name: "panic insert", Status: common.ChannelStatusEnabled,
			Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
		}})
	})

	var channelCount int64
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 9307).Count(&channelCount).Error)
	assert.Zero(t, channelCount)
}

func TestUpdateAbilitiesDoesNotReportSuccessAfterPanic(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	weight := uint(100)
	channel := Channel{
		Id: 9308, Name: "panic ability update", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)

	callbackName := "test:panic_ability_update"
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			panic("ability update panic")
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Delete().Remove(callbackName) })

	require.Panics(t, func() { _ = channel.UpdateAbilities(nil) })
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).
		Where(&Ability{ChannelId: channel.Id, Group: "vip", Model: "model-a"}).
		Count(&abilityCount).Error)
	assert.Equal(t, int64(1), abilityCount)
}

func TestInitializeRouteStatesAndDeleteCannotLeaveOrphanState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 9303, Name: "concurrent route initialization", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9303, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)
	routeCreateStarted, releaseRouteCreate := blockModelCreates(t, db, "ChannelSmartScheduleRouteState")

	initializeResult := make(chan error, 1)
	go func() { initializeResult <- InitializeChannelSmartScheduleRouteStates() }()
	<-routeCreateStarted

	lockWasFree := channelStatusLock.TryLock()
	if lockWasFree {
		channelStatusLock.Unlock()
	}
	assert.False(t, lockWasFree, "route initialization must hold the channel lifecycle lock")

	deleteStarted := make(chan struct{})
	deleteCount := make(chan int64, 1)
	deleteResult := make(chan error, 1)
	go func() {
		close(deleteStarted)
		count, err := BatchDeleteChannels([]int{9303})
		deleteCount <- count
		deleteResult <- err
	}()
	<-deleteStarted
	releaseRouteCreate()

	require.NoError(t, <-initializeResult)
	require.NoError(t, <-deleteResult)
	assert.Equal(t, int64(1), <-deleteCount)
	var stateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ?", 9303).
		Count(&stateCount).Error)
	assert.Zero(t, stateCount)
}

func TestEditChannelByTagRollsBackAllChannelsWhenAbilityRebuildFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	weight := uint(100)
	tag := "atomic-edit"
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9311, Name: "first", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Tag: &tag, Priority: &priority, Weight: &weight},
		{Id: 9312, Name: "second", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Tag: &tag, Priority: &priority, Weight: &weight},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9311, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight, Tag: &tag},
		{ChannelId: 9312, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight, Tag: &tag},
	}).Error)

	callbackName := "test:fail_atomic_tag_edit_ability_create"
	wantErr := errors.New("tag edit ability create failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Ability" {
			return
		}
		if abilities, ok := tx.Statement.Dest.(*[]Ability); ok && len(*abilities) > 0 && (*abilities)[0].ChannelId == 9312 {
			tx.AddError(wantErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	models := "model-a,model-b"
	err := EditChannelByTag(tag, nil, nil, &models, nil, nil, nil, nil, nil)
	require.ErrorIs(t, err, wantErr)

	var channels []Channel
	require.NoError(t, db.Where("id IN ?", []int{9311, 9312}).Order("id ASC").Find(&channels).Error)
	require.Len(t, channels, 2)
	assert.Equal(t, "model-a", channels[0].Models)
	assert.Equal(t, "model-a", channels[1].Models)
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).
		Where("channel_id IN ? AND model = ?", []int{9311, 9312}, "model-b").
		Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}

func TestBatchSetChannelTagSerializesWithDelete(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	oldTag := "old"
	newTag := "new"
	require.NoError(t, db.Create(&Channel{
		Id: 9321, Name: "batch tag delete", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Tag: &oldTag, Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9321, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100, Tag: &oldTag,
	}).Error)
	abilityCreateStarted, releaseAbilityCreate := blockModelCreates(t, db, "Ability")

	updateResult := make(chan error, 1)
	go func() { updateResult <- BatchSetChannelTag([]int{9321}, &newTag) }()
	<-abilityCreateStarted
	lockWasFree := channelStatusLock.TryLock()
	if lockWasFree {
		channelStatusLock.Unlock()
	}
	assert.False(t, lockWasFree, "batch tag update must hold the channel lifecycle lock")

	deleteResult := make(chan error, 1)
	go func() {
		_, err := BatchDeleteChannels([]int{9321})
		deleteResult <- err
	}()
	releaseAbilityCreate()
	require.NoError(t, <-updateResult)
	require.NoError(t, <-deleteResult)

	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 9321).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
}

func TestFixAbilityRemovesOrphansAgainstCurrentChannels(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 9331, Name: "kept", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9331, Group: "vip", Model: "stale-model", Enabled: true, Priority: &priority, Weight: 100},
		{ChannelId: 9399, Group: "vip", Model: "orphan", Enabled: true, Priority: &priority, Weight: 100},
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 9399, GroupName: "vip", ModelName: "orphan", ParticipationSet: true,
	}).Error)

	success, failed, err := FixAbility()
	require.NoError(t, err)
	assert.Equal(t, 1, success)
	assert.Zero(t, failed)

	var orphanAbilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 9399).Count(&orphanAbilityCount).Error)
	assert.Zero(t, orphanAbilityCount)
	var orphanStateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).Where("channel_id = ?", 9399).Count(&orphanStateCount).Error)
	assert.Zero(t, orphanStateCount)
}

func TestUpdateChannelUpstreamModelSettingsRollsBackModelsWhenAbilityRebuildFails(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 9341, Name: "upstream sync", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, OtherSettings: `{"before":true}`,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9341, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100,
	}).Error)

	callbackName := "test:fail_upstream_model_ability_create"
	wantErr := errors.New("upstream ability create failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(wantErr)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	models := "model-a,model-b"
	err := UpdateChannelUpstreamModelSettings(9341, `{"after":true}`, &models)
	require.ErrorIs(t, err, wantErr)

	var channel Channel
	require.NoError(t, db.First(&channel, 9341).Error)
	assert.Equal(t, "model-a", channel.Models)
	assert.JSONEq(t, `{"before":true}`, channel.OtherSettings)
	var abilities []Ability
	require.NoError(t, db.Where("channel_id = ?", 9341).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "model-a", abilities[0].Model)
}

func TestDeleteAbilitiesRemovesRouteStateAndCannotRaceChannelDelete(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 9351, Name: "delete abilities", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9351, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: 100,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 9351, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)

	require.NoError(t, (&Channel{Id: 9351}).DeleteAbilities())
	var abilityCount int64
	require.NoError(t, db.Model(&Ability{}).Where("channel_id = ?", 9351).Count(&abilityCount).Error)
	assert.Zero(t, abilityCount)
	var stateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).Where("channel_id = ?", 9351).Count(&stateCount).Error)
	assert.Zero(t, stateCount)

	_, err := BatchDeleteChannels([]int{9351})
	require.NoError(t, err)
	require.ErrorIs(t, (&Channel{Id: 9351}).DeleteAbilities(), gorm.ErrRecordNotFound)
}

func TestSaveChannelSmartScheduleRouteConfigAndDeleteCannotLeaveOrphanState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 9361, Name: "route config delete", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9361, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)
	routeCreateStarted, releaseRouteCreate := blockModelCreates(t, db, "ChannelSmartScheduleRouteState")

	configResult := make(chan error, 1)
	go func() {
		_, _, err := SaveChannelSmartScheduleRouteConfig(9361, "vip", "model-a", false)
		configResult <- err
	}()
	<-routeCreateStarted

	lockWasFree := channelStatusLock.TryLock()
	if lockWasFree {
		channelStatusLock.Unlock()
	}
	assert.False(t, lockWasFree, "route configuration must hold the channel lifecycle lock")

	deleteResult := make(chan error, 1)
	go func() {
		_, err := BatchDeleteChannels([]int{9361})
		deleteResult <- err
	}()
	releaseRouteCreate()
	require.NoError(t, <-configResult)
	require.NoError(t, <-deleteResult)

	var stateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ?", 9361).Count(&stateCount).Error)
	assert.Zero(t, stateCount)
}

func TestApplyChannelSmartScheduleRouteResultsAndDeleteCannotLeaveOrphanState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(100)
	require.NoError(t, db.Create(&Channel{
		Id: 9362, Name: "schedule apply delete", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 9362, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)
	routeCreateStarted, releaseRouteCreate := blockModelCreates(t, db, "ChannelSmartScheduleRouteState")

	applyResult := make(chan error, 1)
	go func() {
		_, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
			ChannelId: 9362, Group: "vip", Model: "model-a",
			Status: ChannelSmartScheduleStatusSucceeded, Priority: priority, Weight: 100,
		}})
		applyResult <- err
	}()
	<-routeCreateStarted

	lockWasFree := channelStatusLock.TryLock()
	if lockWasFree {
		channelStatusLock.Unlock()
	}
	assert.False(t, lockWasFree, "schedule result application must hold the channel lifecycle lock")

	deleteResult := make(chan error, 1)
	go func() {
		_, err := BatchDeleteChannels([]int{9362})
		deleteResult <- err
	}()
	releaseRouteCreate()
	require.NoError(t, <-applyResult)
	require.NoError(t, <-deleteResult)

	var stateCount int64
	require.NoError(t, db.Model(&ChannelSmartScheduleRouteState{}).
		Where("channel_id = ?", 9362).Count(&stateCount).Error)
	assert.Zero(t, stateCount)
}
