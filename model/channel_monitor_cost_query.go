package model

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
)

// ChannelMonitorCostChannelMetadata contains only the channel columns needed
// by the cost overview. It deliberately excludes credentials and relay
// configuration from this read path.
type ChannelMonitorCostChannelMetadata struct {
	Id     int
	Name   string
	Status int
	Remark *string
}

// RunChannelMonitorCostRead executes all database reads that form one cost
// response on the same repeatable, read-only transaction. This is a
// request-scoped consistent read; no completed result is retained or reused.
func RunChannelMonitorCostRead(ctx context.Context, query func(*gorm.DB) error) error {
	if DB == nil {
		return errors.New("渠道监控成本数据库不可用")
	}
	if query == nil {
		return errors.New("渠道监控成本查询不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return DB.WithContext(ctx).Transaction(query, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
}

// GetChannelMonitorCostChannelMetadata reads metadata only for channels that
// have facts in the requested ledger range.
func GetChannelMonitorCostChannelMetadata(ctx context.Context, db *gorm.DB, channelIDs []int) ([]ChannelMonitorCostChannelMetadata, error) {
	if len(channelIDs) == 0 {
		return []ChannelMonitorCostChannelMetadata{}, nil
	}
	queryDB, err := channelMonitorCostQueryDB(ctx, db)
	if err != nil {
		return nil, err
	}
	var channels []ChannelMonitorCostChannelMetadata
	err = queryDB.Model(&Channel{}).
		Select("id", "name", "status", "remark").
		Where("id IN ?", channelIDs).
		Order("id ASC").
		Find(&channels).Error
	return channels, err
}

// GetChannelMonitorCostRatioMetadata reads only cost conversion fields for the
// channels represented in the requested ledger range.
func GetChannelMonitorCostRatioMetadata(ctx context.Context, db *gorm.DB, channelIDs []int) ([]ChannelRatioMonitor, error) {
	if len(channelIDs) == 0 {
		return []ChannelRatioMonitor{}, nil
	}
	queryDB, err := channelMonitorCostQueryDB(ctx, db)
	if err != nil {
		return nil, err
	}
	var monitors []ChannelRatioMonitor
	err = queryDB.Model(&ChannelRatioMonitor{}).
		Select("channel_id", "ratio", "cost_conversion", "updated_time").
		Where("channel_id IN ?", channelIDs).
		Order("channel_id ASC").
		Find(&monitors).Error
	return monitors, err
}

func channelMonitorCostQueryDB(ctx context.Context, db *gorm.DB) (*gorm.DB, error) {
	if db == nil {
		db = DB
	}
	if db == nil {
		return nil, errors.New("渠道监控成本数据库不可用")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return db.WithContext(ctx), nil
}
