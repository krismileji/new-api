package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
)

type channelMonitorCurrentRPMRow struct {
	ChannelId int   `gorm:"column:channel_id"`
	RPM       int64 `gorm:"column:rpm"`
}

// GetChannelMonitorCurrentRPM returns the number of visible consume requests
// for each channel in the requested time window.
func GetChannelMonitorCurrentRPM(ctx context.Context, startTimestamp int64) (map[int]int, error) {
	rpmByChannel := make(map[int]int)
	if LOG_DB == nil {
		return rpmByChannel, nil
	}

	var rows []channelMonitorCurrentRPMRow
	query := userVisibleLogs(LOG_DB.WithContext(ctx).Table("logs")).
		Select("logs.channel_id AS channel_id, COUNT(*) AS rpm").
		Where("logs.type = ?", LogTypeConsume).
		Where("logs.created_at >= ?", startTimestamp).
		Where("logs.channel_id > ?", 0).
		Group("logs.channel_id")
	if err := query.Scan(&rows).Error; err != nil {
		common.SysError("failed to query channel current rpm: " + err.Error())
		return nil, errors.New("查询渠道当前 RPM 失败")
	}
	for _, row := range rows {
		if row.ChannelId > 0 && row.RPM > 0 {
			rpmByChannel[row.ChannelId] = int(row.RPM)
		}
	}
	return rpmByChannel, nil
}
