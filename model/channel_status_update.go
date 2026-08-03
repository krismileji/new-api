package model

import (
	"bytes"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

func updateChannelStatusAtomically(channelId int, usingKey string, status int, reason string) (bool, error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()

	changed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ?", channelId).First(&channel).Error; err != nil {
			return err
		}

		previousStatus := channel.Status
		if channel.ChannelInfo.IsMultiKey {
			beforeInfo, err := common.Marshal(channel.ChannelInfo)
			if err != nil {
				return err
			}
			handlerMultiKeyUpdate(&channel, usingKey, status, reason)
			afterInfo, err := common.Marshal(channel.ChannelInfo)
			if err != nil {
				return err
			}
			if previousStatus == channel.Status && bytes.Equal(beforeInfo, afterInfo) {
				return nil
			}
		} else {
			if channel.Status == status {
				return nil
			}
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			channel.Status = status
		}

		statusChanged := channel.Status != previousStatus
		if statusChanged {
			markChannelStatusTransition(&channel)
		}
		if err := tx.Model(&Channel{}).
			Where("id = ?", channel.Id).
			Updates(map[string]any{
				"status":       channel.Status,
				"other_info":   channel.OtherInfo,
				"channel_info": channel.ChannelInfo,
			}).Error; err != nil {
			return err
		}
		if statusChanged {
			var abilities []Ability
			if err := tx.Select("group", "model").
				Where("channel_id = ?", channel.Id).
				Find(&abilities).Error; err != nil {
				return err
			}
			pools := channelSmartScheduleRoutePoolsFromAbilities(abilities)
			if err := lockChannelSmartScheduleRoutePoolsTx(tx, pools); err != nil {
				return err
			}
			if err := tx.Model(&Ability{}).
				Where("channel_id = ?", channel.Id).
				Select("enabled").
				Update("enabled", channel.Status == common.ChannelStatusEnabled).Error; err != nil {
				return err
			}
			if err := reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	if err != nil || !changed {
		return false, err
	}
	if common.MemoryCacheEnabled {
		InitChannelCache()
	}
	return true, nil
}
