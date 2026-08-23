package model

import (
	"sort"

	"gorm.io/gorm"
)

// LockChannelsForLogicalGroupMembership loads the physical channels used by a
// logical-group relation change in a stable lock order. lockForUpdate keeps the
// query portable by omitting unsupported FOR UPDATE syntax on SQLite.
func LockChannelsForLogicalGroupMembership(tx *gorm.DB, channelIDs []int) ([]*Channel, error) {
	if tx == nil {
		return nil, gorm.ErrInvalidDB
	}
	ids := append([]int(nil), channelIDs...)
	sort.Ints(ids)
	channels := make([]*Channel, 0, len(ids))
	err := lockForUpdate(tx.Model(&Channel{})).
		Select("id", "name", "type", "status", "base_url", "logical_channel_id").
		Where("id IN ?", ids).
		Order("id ASC").
		Find(&channels).Error
	return channels, err
}

// LockLogicalChannelGroupForMembership serializes relation mutations for one
// logical group. Callers lock candidate channels first, then the group, then
// its member rows so physical deletion and member replacement share one order.
func LockLogicalChannelGroupForMembership(tx *gorm.DB, groupID int64) (*ChannelLogicalGroup, error) {
	if tx == nil {
		return nil, gorm.ErrInvalidDB
	}
	var group ChannelLogicalGroup
	if err := lockForUpdate(tx).
		Select("id", "status", "revision", "name", "remark", "created_at", "updated_at").
		Where("id = ?", groupID).
		First(&group).Error; err != nil {
		return nil, err
	}
	return &group, nil
}

// LockLogicalChannelGroupMembers locks complete member sets in stable group and
// channel order. Reading the full set avoids deciding against a partial group.
func LockLogicalChannelGroupMembers(tx *gorm.DB, groupIDs []int64) ([]ChannelLogicalGroupMember, error) {
	if tx == nil {
		return nil, gorm.ErrInvalidDB
	}
	ids := append([]int64(nil), groupIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	members := make([]ChannelLogicalGroupMember, 0)
	err := lockForUpdate(tx).
		Where("logical_group_id IN ?", ids).
		Order("logical_group_id ASC, channel_id ASC").
		Find(&members).Error
	return members, err
}
