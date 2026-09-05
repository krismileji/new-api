package model

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const channelMonitorStatusRevisionKey = "channel_monitor_status_revision"

type ChannelMonitorStatusSnapshot struct {
	Status    int
	Reason    string
	ChangedAt int64
	Revision  string
}

func CaptureChannelMonitorStatus(channel *Channel) ChannelMonitorStatusSnapshot {
	if channel == nil {
		return ChannelMonitorStatusSnapshot{}
	}
	info := channel.GetOtherInfo()
	reason, _ := info["status_reason"].(string)
	revision, _ := info[channelMonitorStatusRevisionKey].(string)
	changedAt := int64(0)
	switch value := info["status_time"].(type) {
	case int64:
		changedAt = value
	case int:
		changedAt = int64(value)
	case float64:
		if !math.IsNaN(value) && !math.IsInf(value, 0) && value >= math.MinInt64 && value <= math.MaxInt64 {
			changedAt = int64(value)
		}
	}
	return ChannelMonitorStatusSnapshot{
		Status: channel.Status, Reason: reason, ChangedAt: changedAt, Revision: revision,
	}
}

func markChannelStatusTransition(channel *Channel) {
	info := channel.GetOtherInfo()
	info[channelMonitorStatusRevisionKey] = common.GetUUID()
	channel.SetOtherInfo(info)
}

func updateChannelStatusesByTag(tag string, status int) error {
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
		channelIds := make([]int, 0, len(channels))
		for index := range channels {
			channel := &channels[index]
			channelIds = append(channelIds, channel.Id)
			if channel.Status == status {
				continue
			}
			info := channel.GetOtherInfo()
			info["status_reason"] = "管理员标签批量操作"
			info["status_time"] = common.GetTimestamp()
			info[channelMonitorStatusRevisionKey] = common.GetUUID()
			encodedInfo, err := common.Marshal(info)
			if err != nil {
				return err
			}
			if err := tx.Model(&Channel{}).
				Where("id = ?", channel.Id).
				Updates(map[string]any{"status": status, "other_info": string(encodedInfo)}).Error; err != nil {
				return err
			}
		}
		var abilities []Ability
		if len(channelIds) > 0 {
			if err := tx.Select(commonGroupCol, "model").
				Where("channel_id IN ?", channelIds).
				Find(&abilities).Error; err != nil {
				return err
			}
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(abilities)
		if err := lockChannelSmartScheduleRoutePoolsTx(tx, pools); err != nil {
			return err
		}
		for _, channelId := range channelIds {
			if err := tx.Model(&Ability{}).
				Where("channel_id = ?", channelId).
				Select("enabled").
				Update("enabled", status == common.ChannelStatusEnabled).Error; err != nil {
				return err
			}
		}
		return reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools)
	})
}

// UpdateChannelMonitorStatusIfCurrent applies an automatic monitor status
// transition only when the channel still has the status and reason observed by
// the monitor. This prevents a delayed task from overwriting an administrator's
// newer manual status change.
func UpdateChannelMonitorStatusIfCurrent(
	channelId int,
	expectedStatus int,
	expectedReason string,
	status int,
	reason string,
) (changed bool, err error) {
	changed, _, _, err = updateChannelMonitorStatusIfCurrent(
		channelId, nil,
		ChannelMonitorStatusSnapshot{Status: expectedStatus, Reason: expectedReason}, false,
		status, reason,
	)
	return changed, err
}

// UpdateChannelMonitorStatusIfCurrentRevision also requires the upstream
// monitor configuration to retain the revision that produced the decision.
func UpdateChannelMonitorStatusIfCurrentRevision(
	channelId int,
	expectedRevision int64,
	expectedStatus int,
	expectedReason string,
	status int,
	reason string,
) (changed bool, revisionCurrent bool, err error) {
	changed, revisionCurrent, _, err = updateChannelMonitorStatusIfCurrent(
		channelId, &expectedRevision,
		ChannelMonitorStatusSnapshot{Status: expectedStatus, Reason: expectedReason}, false,
		status, reason,
	)
	return changed, revisionCurrent, err
}

func UpdateChannelMonitorStatusIfSnapshotRevision(
	channelId int,
	expectedRevision int64,
	expected ChannelMonitorStatusSnapshot,
	status int,
	reason string,
) (changed bool, revisionCurrent bool, applied ChannelMonitorStatusSnapshot, err error) {
	return updateChannelMonitorStatusIfCurrent(
		channelId, &expectedRevision, expected, true, status, reason,
	)
}

func updateChannelMonitorStatusIfCurrent(
	channelId int,
	expectedRevision *int64,
	expected ChannelMonitorStatusSnapshot,
	guardTransition bool,
	status int,
	reason string,
) (changed bool, revisionCurrent bool, applied ChannelMonitorStatusSnapshot, err error) {
	channelStatusLock.Lock()
	defer channelStatusLock.Unlock()
	revisionCurrent = true
	err = DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		findErr := lockForUpdate(tx).
			Select("id", "status", "other_info").
			Where("id = ?", channelId).
			First(&channel).Error
		if errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if findErr != nil {
			return findErr
		}
		current := CaptureChannelMonitorStatus(&channel)
		if expectedRevision != nil {
			var monitor ChannelRatioMonitor
			findErr = lockForUpdate(tx).
				Select("channel_id", "upstream_revision").
				Where("channel_id = ?", channelId).
				First(&monitor).Error
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				revisionCurrent = false
				return nil
			}
			if findErr != nil {
				return findErr
			}
			if monitor.UpstreamRevision != *expectedRevision {
				revisionCurrent = false
				return nil
			}
		}
		if current.Status != expected.Status || current.Reason != expected.Reason {
			return nil
		}
		if guardTransition && (current.ChangedAt != expected.ChangedAt || current.Revision != expected.Revision) {
			return nil
		}
		if channel.Status == status && current.Reason == reason {
			return nil
		}
		var abilities []Ability
		if err := tx.Select(commonGroupCol, "model").
			Where("channel_id = ?", channelId).
			Find(&abilities).Error; err != nil {
			return err
		}
		pools := channelSmartScheduleRoutePoolsFromAbilities(abilities)
		if err := lockChannelSmartScheduleRoutePoolsTx(tx, pools); err != nil {
			return err
		}

		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		info[channelMonitorStatusRevisionKey] = common.GetUUID()
		encodedInfo, marshalErr := common.Marshal(info)
		if marshalErr != nil {
			return marshalErr
		}
		if err := tx.Model(&Channel{}).
			Where("id = ?", channelId).
			Updates(map[string]any{
				"status":     status,
				"other_info": string(encodedInfo),
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Ability{}).
			Where("channel_id = ?", channelId).
			Select("enabled").
			Update("enabled", status == common.ChannelStatusEnabled).Error; err != nil {
			return err
		}
		if err := reapplyChannelSmartScheduleRoutePrimariesTx(tx, pools); err != nil {
			return err
		}
		channel.Status = status
		channel.OtherInfo = string(encodedInfo)
		applied = CaptureChannelMonitorStatus(&channel)
		changed = true
		return nil
	})
	if changed && common.MemoryCacheEnabled {
		InitChannelCache()
	}
	return changed, revisionCurrent, applied, err
}
