package model

import (
	"sort"

	"gorm.io/gorm"
)

const channelDeleteBatchSize = 200

func deleteChannelsWithMonitorData(channelIds []int) (int64, error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	seen := make(map[int]struct{}, len(channelIds))
	ids := make([]int, 0, len(channelIds))
	for _, channelId := range channelIds {
		if channelId <= 0 {
			continue
		}
		if _, exists := seen[channelId]; exists {
			continue
		}
		seen[channelId] = struct{}{}
		ids = append(ids, channelId)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	sort.Ints(ids)

	var deletedCount int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		deletedCount, err = deleteChannelRowsWithMonitorDataTx(tx, ids)
		return err
	})
	return deletedCount, err
}

func deleteChannelsByStatusesWithMonitorData(statuses []int64) (int64, error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	if len(statuses) == 0 {
		return 0, nil
	}
	var deletedCount int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var channelIds []int
		if err := lockForUpdate(tx.Model(&Channel{}).Select("id").Where("status IN ?", statuses)).
			Order("id ASC").
			Pluck("id", &channelIds).Error; err != nil {
			return err
		}
		if len(channelIds) == 0 {
			return nil
		}
		var err error
		deletedCount, err = deleteChannelRowsWithMonitorDataTx(tx, channelIds)
		return err
	})
	return deletedCount, err
}

func deleteChannelRowsWithMonitorDataTx(tx *gorm.DB, channelIds []int) (int64, error) {
	ids := append([]int(nil), channelIds...)
	sort.Ints(ids)
	existingChannelIds := make([]int, 0, len(ids))
	for start := 0; start < len(ids); start += channelDeleteBatchSize {
		end := min(start+channelDeleteBatchSize, len(ids))
		var channels []Channel
		if err := lockForUpdate(tx).
			Select("id").
			Where("id IN ?", ids[start:end]).
			Order("id ASC").
			Find(&channels).Error; err != nil {
			return 0, err
		}
		for index := range channels {
			existingChannelIds = append(existingChannelIds, channels[index].Id)
		}
	}

	var deletedCount int64
	for start := 0; start < len(existingChannelIds); start += channelDeleteBatchSize {
		end := min(start+channelDeleteBatchSize, len(existingChannelIds))
		existingIds := existingChannelIds[start:end]
		var abilities []Ability
		if err := tx.Select("channel_id", "group", "model").
			Where("channel_id IN ?", existingIds).
			Order("channel_id ASC").
			Find(&abilities).Error; err != nil {
			return 0, err
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(abilities)
		if tx.Migrator().HasTable(&ChannelRatioMonitor{}) {
			var monitors []ChannelRatioMonitor
			if err := lockForUpdate(tx).
				Where("channel_id IN ?", existingIds).
				Order("channel_id ASC").
				Find(&monitors).Error; err != nil {
				return 0, err
			}
		}
		if _, err := lockChannelSmartScheduleRoutePoolStatesTx(tx, pools); err != nil {
			return 0, err
		}
		if tx.Migrator().HasTable(&ChannelSmartScheduleModelSampleState{}) {
			var sampleStates []ChannelSmartScheduleModelSampleState
			if err := lockForUpdate(tx).
				Where("channel_id IN ?", existingIds).
				Order("channel_id ASC, model_name ASC").
				Find(&sampleStates).Error; err != nil {
				return 0, err
			}
		}
		if _, err := lockChannelSmartScheduleRoutePoolAbilitiesTx(tx, pools); err != nil {
			return 0, err
		}
		result := tx.Where("id IN ?", existingIds).Delete(&Channel{})
		if result.Error != nil {
			return 0, result.Error
		}
		deletedCount += result.RowsAffected
		if err := tx.Where("channel_id IN ?", existingIds).Delete(&Ability{}).Error; err != nil {
			return 0, err
		}
		// Keep route states until pool reconciliation has observed any fixed
		// intent belonging to a deleted channel.
		if err := reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools); err != nil {
			return 0, err
		}
		if err := deleteChannelModelDetectionDataTx(tx, existingIds); err != nil {
			return 0, err
		}
		if err := deleteChannelMonitorDataTx(tx, existingIds); err != nil {
			return 0, err
		}
	}
	return deletedCount, nil
}

func deleteChannelMonitorDataTx(tx *gorm.DB, channelIds []int) error {
	monitorTables := []any{
		&ChannelMonitorMinuteRouteMetric{},
		&ChannelMonitorMinuteAPIKeyMetric{},
		&ChannelMonitorMinuteDurationBucket{},
		&ChannelRatioMonitor{},
		&ChannelSmartScheduleRouteState{},
		&ChannelSmartScheduleGroupPause{},
		&ChannelSmartScheduleModelSampleState{},
		&ChannelStatusProbeConfig{},
		&ChannelStatusProbeState{},
		&ChannelStatusProbeExecution{},
	}
	for _, table := range monitorTables {
		if !tx.Migrator().HasTable(table) {
			continue
		}
		if err := tx.Where("channel_id IN ?", channelIds).Delete(table).Error; err != nil {
			return err
		}
	}
	return nil
}
