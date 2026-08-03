package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedChannelDeleteCleanupData(t *testing.T, db *gorm.DB, channelId int, status int) {
	t.Helper()
	priority := int64(80)
	require.NoError(t, db.Create(&Channel{
		Id: channelId, Name: "delete cleanup", Status: status,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: channelId, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: 100,
	}).Error)
	require.NoError(t, db.Create(&ChannelRatioMonitor{ChannelId: channelId, Ratio: 1}).Error)
	require.NoError(t, db.Create(&ChannelRatioHistory{ChannelId: channelId, OldRatio: 1, NewRatio: 2}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: channelId, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleModelSampleState{
		ChannelId: channelId, ModelName: "model-a",
	}).Error)
	require.NoError(t, db.Create(&ChannelDailyCost{
		ChannelId: channelId, DayStart: 1, CreatedAt: 1, UpdatedAt: 1,
	}).Error)
	require.NoError(t, db.Create(&ChannelDailyAPIKeyCost{
		ChannelId: channelId, DayStart: 1, KeyFingerprint: "key", KeyDisplay: "key",
		CreatedAt: 1, UpdatedAt: 1,
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorMinuteMetric{
		MinuteStart: 60, ChannelId: channelId, ModelKey: "model", GroupKey: "group", APIKeyKey: "key",
		ModelName: "model-a", GroupName: "vip", APIKeyName: "key",
	}).Error)
	require.NoError(t, db.Create(&ChannelMonitorMinuteDurationBucket{
		MinuteStart: 60, ChannelId: channelId, ModelKey: "model", GroupKey: "group",
		BucketIndex: 1, ModelName: "model-a", GroupName: "vip", Count: 1,
	}).Error)
}

func TestChannelDeletionRemovesConfigurationAndKeepsHistory(t *testing.T) {
	tests := []struct {
		name       string
		statuses   []int
		delete     func() (int64, error)
		deletedIds []int
	}{
		{
			name: "single",
			statuses: []int{
				common.ChannelStatusEnabled,
				common.ChannelStatusEnabled,
				common.ChannelStatusEnabled,
			},
			delete: func() (int64, error) {
				return 1, (&Channel{Id: 1801}).Delete()
			},
			deletedIds: []int{1801},
		},
		{
			name: "batch",
			statuses: []int{
				common.ChannelStatusEnabled,
				common.ChannelStatusEnabled,
				common.ChannelStatusEnabled,
			},
			delete: func() (int64, error) {
				return BatchDeleteChannels([]int{1803, 1801, 1801})
			},
			deletedIds: []int{1801, 1803},
		},
		{
			name: "disabled",
			statuses: []int{
				common.ChannelStatusAutoDisabled,
				common.ChannelStatusEnabled,
				common.ChannelStatusManuallyDisabled,
			},
			delete:     DeleteDisabledChannel,
			deletedIds: []int{1801, 1803},
		},
		{
			name: "status",
			statuses: []int{
				common.ChannelStatusAutoDisabled,
				common.ChannelStatusEnabled,
				common.ChannelStatusManuallyDisabled,
			},
			delete: func() (int64, error) {
				return DeleteChannelByStatus(common.ChannelStatusAutoDisabled)
			},
			deletedIds: []int{1801},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelSmartScheduleRouteTestDB(t)
			require.NoError(t, db.AutoMigrate(
				&ChannelRatioHistory{},
				&ChannelDailyCost{},
				&ChannelDailyAPIKeyCost{},
				&ChannelMonitorMinuteMetric{},
				&ChannelMonitorMinuteDurationBucket{},
			))
			for index, channelId := range []int{1801, 1802, 1803} {
				seedChannelDeleteCleanupData(t, db, channelId, test.statuses[index])
			}

			deletedCount, err := test.delete()
			require.NoError(t, err)
			assert.Equal(t, int64(len(test.deletedIds)), deletedCount)

			deleted := make(map[int]struct{}, len(test.deletedIds))
			for _, channelId := range test.deletedIds {
				deleted[channelId] = struct{}{}
			}
			configurationTables := []any{
				&Ability{},
				&ChannelRatioMonitor{},
				&ChannelSmartScheduleRouteState{},
				&ChannelSmartScheduleModelSampleState{},
			}
			historyTables := []any{
				&ChannelRatioHistory{},
				&ChannelDailyCost{},
				&ChannelDailyAPIKeyCost{},
				&ChannelMonitorMinuteMetric{},
				&ChannelMonitorMinuteDurationBucket{},
			}
			for _, channelId := range []int{1801, 1802, 1803} {
				wantCount := int64(1)
				if _, wasDeleted := deleted[channelId]; wasDeleted {
					wantCount = 0
				}
				var channelCount int64
				require.NoError(t, db.Model(&Channel{}).Where("id = ?", channelId).Count(&channelCount).Error)
				assert.Equal(t, wantCount, channelCount, "channel %d", channelId)
				for _, table := range configurationTables {
					var count int64
					require.NoError(t, db.Model(table).Where("channel_id = ?", channelId).Count(&count).Error)
					assert.Equal(t, wantCount, count, "table %T channel %d", table, channelId)
				}
				for _, table := range historyTables {
					var count int64
					require.NoError(t, db.Model(table).Where("channel_id = ?", channelId).Count(&count).Error)
					assert.Equal(t, int64(1), count, "history table %T channel %d", table, channelId)
				}
			}
		})
	}
}

func TestChannelDeletionPreventsLateSampleRecreation(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.Create(&Channel{
		Id: 1901, Name: "deleted before sample", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleModelSampleState{
		ChannelId: 1901, ModelName: "model-a",
	}).Error)
	require.NoError(t, (&Channel{Id: 1901}).Delete())

	_, err := SaveChannelSmartScheduleModelSample(ChannelSmartScheduleModelSampleResult{
		ChannelId: 1901, Model: "model-a", Time: 100, Success: true,
	})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var count int64
	require.NoError(t, db.Model(&ChannelSmartScheduleModelSampleState{}).
		Where("channel_id = ?", 1901).
		Count(&count).Error)
	assert.Zero(t, count)
}
