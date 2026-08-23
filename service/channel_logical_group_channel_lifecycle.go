package service

import (
	"errors"
	"math"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// UpdateChannelWithLogicalGroupValidation keeps a grouped channel's effective
// upstream address consistent with every member. Relation changes are written
// in the physical channel transaction, while cache invalidation is published
// only after that transaction commits.
func UpdateChannelWithLogicalGroupValidation(channel *model.Channel) error {
	if channel == nil {
		return gorm.ErrInvalidValue
	}
	relationChanged, err := channel.UpdateWithTransactionHook(validateLogicalGroupChannelUpdate)
	if err != nil {
		return err
	}
	if relationChanged {
		InvalidateLogicalChannelRuntimeCache()
	}
	return nil
}

func validateLogicalGroupChannelUpdate(tx *gorm.DB, previous, proposed *model.Channel) (bool, error) {
	if previous == nil || proposed == nil || previous.LogicalChannelID == nil || *previous.LogicalChannelID <= 0 {
		return false, nil
	}
	if previous.Type == proposed.Type && previous.GetBaseURL() == proposed.GetBaseURL() {
		return false, nil
	}

	normalizedAddress, err := NormalizeLogicalChannelAddressForChannel(proposed)
	if err != nil {
		return false, err
	}
	logicalID := *previous.LogicalChannelID
	group, err := model.LockLogicalChannelGroupForMembership(tx, logicalID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ErrLogicalChannelGroupNotFound
		}
		return false, err
	}
	if group.Revision <= 0 || group.Revision == math.MaxInt64 {
		return false, model.ErrChannelLogicalGroupInvalidRevision
	}
	members, err := model.LockLogicalChannelGroupMembers(tx, []int64{logicalID})
	if err != nil {
		return false, err
	}
	if len(members) == 0 {
		return false, model.ErrChannelLogicalGroupEmptyMembers
	}
	memberIDs := make([]int, 0, len(members))
	foundProposed := false
	for _, member := range members {
		memberIDs = append(memberIDs, member.ChannelID)
		if member.ChannelID == previous.Id {
			foundProposed = true
		}
	}
	if !foundProposed {
		return false, model.ErrChannelLogicalGroupInvalidMember
	}
	sort.Ints(memberIDs)
	var memberChannels []*model.Channel
	if err := tx.Select("id", "type", "base_url", "logical_channel_id").
		Where("id IN ?", memberIDs).
		Order("id ASC").
		Find(&memberChannels).Error; err != nil {
		return false, err
	}
	if len(memberChannels) != len(memberIDs) {
		return false, ErrLogicalChannelGroupChannelMissing
	}
	for _, memberChannel := range memberChannels {
		if memberChannel.Id == previous.Id {
			continue
		}
		if memberChannel.LogicalChannelID == nil || *memberChannel.LogicalChannelID != logicalID {
			return false, model.ErrChannelLogicalGroupInvalidMember
		}
		address, normalizeErr := NormalizeLogicalChannelAddressForChannel(memberChannel)
		if normalizeErr != nil {
			return false, normalizeErr
		}
		if address != normalizedAddress {
			return false, ErrLogicalChannelGroupAddressMismatch
		}
	}

	now := common.GetTimestamp()
	newRevision := group.Revision + 1
	updatedGroup := tx.Model(&model.ChannelLogicalGroup{}).
		Where("id = ? AND revision = ?", logicalID, group.Revision).
		Updates(map[string]any{"revision": newRevision, "updated_at": now})
	if updatedGroup.Error != nil {
		return false, updatedGroup.Error
	}
	if updatedGroup.RowsAffected != 1 {
		return false, model.ErrChannelLogicalGroupRevisionConflict
	}
	fingerprint := LogicalChannelAddressFingerprint(normalizedAddress)
	updatedMembers := tx.Model(&model.ChannelLogicalGroupMember{}).
		Where("logical_group_id = ?", logicalID).
		Updates(map[string]any{"address_fingerprint": fingerprint, "updated_at": now})
	if updatedMembers.Error != nil {
		return false, updatedMembers.Error
	}
	return true, nil
}
