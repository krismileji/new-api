package model

import (
	"sort"

	"gorm.io/gorm"
)

func lockChannelForDependentWriteTx(tx *gorm.DB, channelId int) error {
	_, err := lockChannelsForDependentWriteTx(tx, []int{channelId})
	return err
}

func lockChannelsForDependentWriteTx(tx *gorm.DB, channelIds []int) ([]Channel, error) {
	seen := make(map[int]struct{}, len(channelIds))
	ids := make([]int, 0, len(channelIds))
	for _, channelId := range channelIds {
		if _, exists := seen[channelId]; exists {
			continue
		}
		seen[channelId] = struct{}{}
		ids = append(ids, channelId)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	sort.Ints(ids)

	var channels []Channel
	if err := lockForUpdate(tx).
		Select("id", "status", "priority", "weight").
		Where("id IN ?", ids).
		Order("id ASC").
		Find(&channels).Error; err != nil {
		return nil, err
	}
	if len(channels) != len(ids) {
		return nil, gorm.ErrRecordNotFound
	}
	return channels, nil
}
