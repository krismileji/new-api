package model

import (
	"errors"
	"math"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// detachDeletedChannelsFromLogicalGroupsTx removes physical members in the
// same transaction that deletes their channels. A group with remaining
// members receives a new revision; a group whose last member is deleted is
// removed. Cache invalidation is intentionally left to the outer caller after
// commit so a rollback cannot publish relation state.
func detachDeletedChannelsFromLogicalGroupsTx(tx *gorm.DB, channelIDs []int) (bool, error) {
	if len(channelIDs) == 0 || !tx.Migrator().HasTable(&ChannelLogicalGroup{}) || !tx.Migrator().HasTable(&ChannelLogicalGroupMember{}) {
		return false, nil
	}
	// During a rolling upgrade the relation tables can be visible before the
	// denormalized Channel.logical_channel_id column. Keep deletion usable in
	// that intermediate schema: relation cleanup is still safe, but any write
	// against the optional column must be skipped.
	hasLogicalChannelColumn := tx.Migrator().HasColumn(&Channel{}, "logical_channel_id")
	// Discover candidate groups without taking member locks, then lock groups
	// first and their complete member sets second. A concurrent replacement
	// may make this discovery stale, so membership is rechecked after locking.
	var discoveredMembers []ChannelLogicalGroupMember
	if err := tx.
		Select("id", "logical_group_id", "channel_id").
		Where("channel_id IN ?", channelIDs).
		Order("logical_group_id ASC, channel_id ASC").
		Find(&discoveredMembers).Error; err != nil {
		return false, err
	}
	if len(discoveredMembers) == 0 {
		return false, nil
	}

	groupSet := make(map[int64]struct{}, len(discoveredMembers))
	for _, member := range discoveredMembers {
		groupSet[member.LogicalGroupID] = struct{}{}
	}
	groupIDs := make([]int64, 0, len(groupSet))
	for groupID := range groupSet {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	groupsByID := make(map[int64]ChannelLogicalGroup, len(groupIDs))
	lockedGroupIDs := make([]int64, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		group, err := LockLogicalChannelGroupForMembership(tx, groupID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return false, err
		}
		groupsByID[groupID] = *group
		lockedGroupIDs = append(lockedGroupIDs, groupID)
	}
	if len(lockedGroupIDs) == 0 {
		return false, nil
	}
	lockedMembers, err := LockLogicalChannelGroupMembers(tx, lockedGroupIDs)
	if err != nil {
		return false, err
	}
	deletingChannels := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		deletingChannels[channelID] = struct{}{}
	}
	affectedGroupSet := make(map[int64]struct{}, len(lockedGroupIDs))
	for _, member := range lockedMembers {
		if _, deleting := deletingChannels[member.ChannelID]; deleting {
			affectedGroupSet[member.LogicalGroupID] = struct{}{}
		}
	}
	if len(affectedGroupSet) == 0 {
		return false, nil
	}
	affectedGroupIDs := make([]int64, 0, len(affectedGroupSet))
	groups := make([]ChannelLogicalGroup, 0, len(affectedGroupSet))
	for _, groupID := range lockedGroupIDs {
		if _, affected := affectedGroupSet[groupID]; !affected {
			continue
		}
		affectedGroupIDs = append(affectedGroupIDs, groupID)
		groups = append(groups, groupsByID[groupID])
	}
	if err := tx.Where("channel_id IN ? AND logical_group_id IN ?", channelIDs, affectedGroupIDs).Delete(&ChannelLogicalGroupMember{}).Error; err != nil {
		return false, err
	}

	now := common.GetTimestamp()
	for _, group := range groups {
		var remaining int64
		if err := tx.Model(&ChannelLogicalGroupMember{}).
			Where("logical_group_id = ?", group.Id).
			Count(&remaining).Error; err != nil {
			return false, err
		}
		if remaining == 0 {
			deleted := tx.Where("id = ? AND revision = ?", group.Id, group.Revision).Delete(&ChannelLogicalGroup{})
			if deleted.Error != nil {
				return false, deleted.Error
			}
			if deleted.RowsAffected != 1 {
				return false, ErrChannelLogicalGroupRevisionConflict
			}
			// This should normally match no survivor, but clears any stale
			// denormalized ownership rather than leaving a dangling reference.
			if hasLogicalChannelColumn {
				if err := tx.Model(&Channel{}).
					Where("logical_channel_id = ? AND id NOT IN ?", group.Id, channelIDs).
					Update("logical_channel_id", nil).Error; err != nil {
					return false, err
				}
			}
			continue
		}
		if group.Revision <= 0 || group.Revision == math.MaxInt64 {
			return false, ErrChannelLogicalGroupInvalidRevision
		}
		updated := tx.Model(&ChannelLogicalGroup{}).
			Where("id = ? AND revision = ?", group.Id, group.Revision).
			Updates(map[string]any{"revision": group.Revision + 1, "updated_at": now})
		if updated.Error != nil {
			return false, updated.Error
		}
		if updated.RowsAffected != 1 {
			return false, ErrChannelLogicalGroupRevisionConflict
		}
	}
	return true, nil
}
