package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ChannelDailyCostNanoPerCNY int64 = 1_000_000_000
	channelDailyCostDaySeconds int64 = 24 * 60 * 60
	channelDailyCostUTC8Offset int64 = 8 * 60 * 60
)

// ChannelDailyCost stores the immutable cost facts settled for one channel on
// one Beijing calendar day. Monetary values use CNY nanos to avoid float
// accumulation drift.
type ChannelDailyCost struct {
	Id              int64 `gorm:"primaryKey"`
	ChannelId       int   `gorm:"not null;uniqueIndex:idx_channel_daily_cost_day"`
	DayStart        int64 `gorm:"not null;uniqueIndex:idx_channel_daily_cost_day;index:idx_channel_daily_cost_day_start"`
	CostNanoCNY     int64 `gorm:"not null"`
	SettledCount    int64 `gorm:"not null"`
	UnresolvedCount int64 `gorm:"not null"`
	CreatedAt       int64 `gorm:"not null"`
	UpdatedAt       int64 `gorm:"not null"`
}

// ChannelDailyCostDayTotal is a database-aggregated daily total. Keeping the
// aggregation in the query lets callers page calendar ranges without loading
// every channel row for the whole range into memory.
type ChannelDailyCostDayTotal struct {
	DayStart        int64 `gorm:"column:day_start"`
	CostNanoCNY     int64 `gorm:"column:cost_nano_cny"`
	UnresolvedCount int64 `gorm:"column:unresolved_count"`
}

// ChannelDailyCostDayStart returns the UTC timestamp at which the containing
// Beijing calendar day starts.
func ChannelDailyCostDayStart(timestamp int64) int64 {
	return ((timestamp+channelDailyCostUTC8Offset)/channelDailyCostDaySeconds)*channelDailyCostDaySeconds - channelDailyCostUTC8Offset
}

// AddChannelDailyCost atomically adds one settled or unresolved event to the
// single daily row for a channel.
func AddChannelDailyCost(ctx context.Context, channelId int, occurredAt int64, costNanoCNY int64, settledDelta int64, unresolvedDelta int64) error {
	return addChannelDailyCost(DB.WithContext(ctx), channelId, occurredAt, costNanoCNY, settledDelta, unresolvedDelta)
}

func addChannelDailyCost(tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64, settledDelta int64, unresolvedDelta int64) error {
	if channelId <= 0 {
		return errors.New("channel id must be positive")
	}
	if costNanoCNY < 0 {
		return errors.New("daily cost must not be negative")
	}
	if settledDelta < 0 || unresolvedDelta < 0 || settledDelta+unresolvedDelta <= 0 {
		return errors.New("daily cost event count must be positive")
	}

	record := ChannelDailyCost{
		ChannelId:       channelId,
		DayStart:        ChannelDailyCostDayStart(occurredAt),
		CostNanoCNY:     costNanoCNY,
		SettledCount:    settledDelta,
		UnresolvedCount: unresolvedDelta,
		CreatedAt:       occurredAt,
		UpdatedAt:       occurredAt,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "day_start"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"cost_nano_cny":    gorm.Expr("cost_nano_cny + ?", costNanoCNY),
			"settled_count":    gorm.Expr("settled_count + ?", settledDelta),
			"unresolved_count": gorm.Expr("unresolved_count + ?", unresolvedDelta),
			"updated_at":       occurredAt,
		}),
	}).Create(&record).Error
}

func GetChannelDailyCosts(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelDailyCost, error) {
	return getChannelDailyCosts(ctx, startTimestamp, endTimestamp, 0)
}

func GetChannelDailyCostsForChannel(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCost, error) {
	return getChannelDailyCosts(ctx, startTimestamp, endTimestamp, channelId)
}

// GetChannelDailyCostDayTotals returns one aggregated row per calendar day in
// the requested range. The range should be limited to the requested page by
// the caller when displaying paginated date details.
func GetChannelDailyCostDayTotals(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCostDayTotal, error) {
	return getChannelDailyCostDayTotals(ctx, startTimestamp, endTimestamp, channelId, 0)
}

// GetChannelDailyCostDayTotalsPage applies a database-side limit to an
// already-bounded calendar page. The caller still supplies the page's date
// range so days without a recorded row can be filled by the presentation
// layer without changing the page shape.
func GetChannelDailyCostDayTotalsPage(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, pageSize int) ([]ChannelDailyCostDayTotal, error) {
	return getChannelDailyCostDayTotals(ctx, startTimestamp, endTimestamp, channelId, pageSize)
}

func getChannelDailyCostDayTotals(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, pageSize int) ([]ChannelDailyCostDayTotal, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyCostDayTotal{}, nil
	}
	query := DB.WithContext(ctx).
		Model(&ChannelDailyCost{}).
		Select("day_start, SUM(cost_nano_cny) AS cost_nano_cny, SUM(unresolved_count) AS unresolved_count").
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp)
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	if pageSize > 0 {
		query = query.Limit(pageSize)
	}
	var totals []ChannelDailyCostDayTotal
	err := query.Group("day_start").Order("day_start ASC").Find(&totals).Error
	return totals, err
}

func getChannelDailyCosts(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyCost, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyCost{}, nil
	}
	query := DB.WithContext(ctx).
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp)
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	var costs []ChannelDailyCost
	err := query.Order("day_start ASC, channel_id ASC").Find(&costs).Error
	return costs, err
}
