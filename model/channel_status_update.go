package model

import (
	"bytes"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

func updateChannelStatusAtomically(channelId int, usingKey string, status int, reason string) (bool, error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()

	var cachedChannel *Channel
	if common.MemoryCacheEnabled {
		cachedChannel, _ = CacheGetChannel(channelId)
	}

	var updatedChannel Channel
	var statusChanged bool
	changed, err := updateChannelOtherInfo(channelId, func(channel *Channel) (map[string]any, error) {
		if status == common.ChannelStatusAutoDisabled && channel.Status == common.ChannelStatusManuallyDisabled {
			return nil, nil
		}
		if cachedChannel != nil && channel.ChannelInfo.IsMultiKey && cachedChannel.ChannelInfo.IsMultiKey {
			channel.ChannelInfo.MultiKeyPollingIndex = cachedChannel.ChannelInfo.MultiKeyPollingIndex
		}

		previousStatus := channel.Status
		previousOtherInfo := channel.OtherInfo
		if channel.ChannelInfo.IsMultiKey {
			beforeInfo, err := common.Marshal(channel.ChannelInfo)
			if err != nil {
				return nil, err
			}
			handlerMultiKeyUpdate(channel, usingKey, status, reason)
			afterInfo, err := common.Marshal(channel.ChannelInfo)
			if err != nil {
				return nil, err
			}
			if previousStatus == channel.Status && bytes.Equal(beforeInfo, afterInfo) && previousOtherInfo == channel.OtherInfo {
				return nil, nil
			}
		} else {
			if channel.Status == status {
				return nil, nil
			}
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			channel.Status = status
		}

		statusChanged = channel.Status != previousStatus
		if statusChanged {
			markChannelStatusTransition(channel)
		}
		updatedChannel = *channel
		updates := map[string]any{"status": channel.Status}
		if channel.ChannelInfo.IsMultiKey {
			updates["channel_info"] = channel.ChannelInfo
		}
		return updates, nil
	}, func(tx *gorm.DB, channel *Channel) error {
		if statusChanged {
			var abilities []Ability
			if err := tx.Select(commonGroupCol, "model").
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
		return nil
	})
	if err != nil || !changed {
		return false, err
	}
	if cachedChannel != nil {
		cachedChannel.Status = updatedChannel.Status
		cachedChannel.OtherInfo = updatedChannel.OtherInfo
		cachedChannel.ChannelInfo = updatedChannel.ChannelInfo
	}
	if common.MemoryCacheEnabled {
		InitChannelCache()
	}
	if !common.IsMasterNode && status == common.ChannelStatusAutoDisabled && channelPluginHasHook(&updatedChannel, "scheduled") {
		BumpSyncTopic(SyncTopicChannels)
	}
	return true, nil
}
