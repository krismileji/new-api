package model

import (
	"fmt"

	"gorm.io/gorm"
)

func updateChannelsByTagWithAbilities(tag string, updateData Channel, updateAbilities bool) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := lockForUpdate(tx).
			Where("tag = ?", tag).
			Order("id ASC").
			Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) == 0 {
			return nil
		}

		channelIds := make([]int, len(channels))
		for index := range channels {
			channelIds[index] = channels[index].Id
		}
		if err := tx.Model(&Channel{}).Where("id IN ?", channelIds).Updates(updateData).Error; err != nil {
			return err
		}
		if !updateAbilities {
			return nil
		}
		if err := tx.Where("id IN ?", channelIds).Order("id ASC").Find(&channels).Error; err != nil {
			return err
		}
		for index := range channels {
			if err := channels[index].UpdateAbilities(tx); err != nil {
				return fmt.Errorf("更新渠道 %d 的能力失败: %w", channels[index].Id, err)
			}
		}
		return nil
	})
}

func UpdateChannelUpstreamModelSettings(channelId int, settings string, models *string) error {
	if channelId <= 0 {
		return gorm.ErrMissingWhereClause
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	return DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ?", channelId).First(&channel).Error; err != nil {
			return err
		}
		updates := map[string]any{"settings": settings}
		channel.OtherSettings = settings
		if models != nil {
			updates["models"] = *models
			channel.Models = *models
		}
		if err := tx.Model(&Channel{}).Where("id = ?", channelId).Updates(updates).Error; err != nil {
			return err
		}
		if models == nil {
			return nil
		}
		return channel.UpdateAbilities(tx)
	})
}

func updateAbilityStatusWithPrimaries(channelId int, enabled bool) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockChannelForDependentWriteTx(tx, channelId); err != nil {
			return err
		}
		var abilities []Ability
		if err := tx.Select("group", "model").
			Where("channel_id = ?", channelId).
			Order("channel_id ASC").
			Find(&abilities).Error; err != nil {
			return err
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(abilities)
		if err := lockChannelSmartScheduleRoutePoolsTx(tx, pools); err != nil {
			return err
		}
		if err := tx.Model(&Ability{}).Where("channel_id = ?", channelId).
			Select("enabled").Update("enabled", enabled).Error; err != nil {
			return err
		}
		return reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools)
	})
}

func updateAbilitiesByTagWithPrimaries(tag string, enabled bool) error {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	return DB.Transaction(func(tx *gorm.DB) error {
		var channels []Channel
		if err := lockForUpdate(tx).
			Select("id").
			Where("tag = ?", tag).
			Order("id ASC").
			Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) == 0 {
			return nil
		}
		channelIds := make([]int, len(channels))
		for index := range channels {
			channelIds[index] = channels[index].Id
		}
		var abilities []Ability
		if err := tx.Select("group", "model").
			Where("channel_id IN ?", channelIds).
			Order("channel_id ASC").
			Find(&abilities).Error; err != nil {
			return err
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(abilities)
		if err := lockChannelSmartScheduleRoutePoolsTx(tx, pools); err != nil {
			return err
		}
		if err := tx.Model(&Ability{}).Where("channel_id IN ?", channelIds).
			Select("enabled").Update("enabled", enabled).Error; err != nil {
			return err
		}
		return reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools)
	})
}

func deleteChannelAbilities(channelId int) error {
	if channelId <= 0 {
		return gorm.ErrMissingWhereClause
	}
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	return DB.Transaction(func(tx *gorm.DB) error {
		if err := lockChannelForDependentWriteTx(tx, channelId); err != nil {
			return err
		}
		var abilities []Ability
		if err := tx.Select("group", "model").
			Where("channel_id = ?", channelId).
			Order("channel_id ASC").
			Find(&abilities).Error; err != nil {
			return err
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(abilities)
		if err := lockChannelSmartScheduleRoutePoolsTx(tx, pools); err != nil {
			return err
		}
		if err := tx.Where("channel_id = ?", channelId).Delete(&Ability{}).Error; err != nil {
			return err
		}
		if err := reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools); err != nil {
			return err
		}
		if err := deleteObsoleteChannelSmartScheduleRouteStates(
			tx,
			channelId,
			map[ChannelSmartScheduleRouteKey]struct{}{},
		); err != nil {
			return err
		}
		return nil
	})
}
