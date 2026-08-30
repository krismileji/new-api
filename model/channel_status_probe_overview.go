package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

func channelStatusProbeOverviewDB(ctx context.Context, db *gorm.DB) (*gorm.DB, error) {
	if db == nil {
		db = DB
	}
	if db == nil {
		return nil, errors.New("渠道状态探测查询数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return db.WithContext(ctx), nil
}

// GetChannelsForStatusProbeOverview loads only the channel fields used by the
// status probe overview. Large provider settings and runtime metadata stay out
// of the polling path.
func GetChannelsForStatusProbeOverview(ctx context.Context, db *gorm.DB, channelIDs []int) ([]*Channel, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	if channelIDs != nil && len(channelIDs) == 0 {
		return []*Channel{}, nil
	}
	var channels []*Channel
	query := resolveChannelSortOptions(false, nil).Apply(queryDB).
		Select("id", "type", "status", "name", "models", commonGroupCol, "remark")
	if channelIDs != nil {
		query = query.Where("id IN ?", channelIDs)
	}
	err = query.Find(&channels).Error
	return channels, err
}

// GetChannelGroupsForStatusProbeOverview loads the lightweight channel/group
// projection used to keep filter options stable while a model filter is active.
func GetChannelGroupsForStatusProbeOverview(ctx context.Context, db *gorm.DB) ([]*Channel, error) {
	queryDB, err := channelStatusProbeOverviewDB(ctx, db)
	if err != nil {
		return nil, err
	}
	var channels []*Channel
	err = queryDB.Select("id", commonGroupCol).Order("id ASC").Find(&channels).Error
	return channels, err
}
