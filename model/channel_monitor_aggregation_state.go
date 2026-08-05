package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const channelMonitorAggregationStateID = 1

type ChannelMonitorAggregationState struct {
	ID               int   `gorm:"primaryKey"`
	CompletedThrough int64 `gorm:"not null"`
	Revision         int64 `gorm:"not null;default:0"`
	UpdatedAt        int64 `gorm:"not null"`
}

func ensureChannelMonitorAggregationState(ctx context.Context) error {
	state := ChannelMonitorAggregationState{
		ID:        channelMonitorAggregationStateID,
		UpdatedAt: common.GetTimestamp(),
	}
	return DB.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&state).Error
}

func lockChannelMonitorAggregationState(tx *gorm.DB) (ChannelMonitorAggregationState, error) {
	var state ChannelMonitorAggregationState
	err := lockForUpdate(tx).
		Where("id = ?", channelMonitorAggregationStateID).
		Take(&state).Error
	return state, err
}

func updateChannelMonitorAggregationStateWithTx(
	tx *gorm.DB,
	state ChannelMonitorAggregationState,
	completedThrough int64,
	publishWatermark bool,
) error {
	updates := map[string]any{
		"revision":   gorm.Expr("revision + ?", 1),
		"updated_at": common.GetTimestamp(),
	}
	if publishWatermark && completedThrough > state.CompletedThrough {
		updates["completed_through"] = completedThrough
	}
	return tx.Model(&ChannelMonitorAggregationState{}).
		Where("id = ?", channelMonitorAggregationStateID).
		Updates(updates).Error
}

func GetChannelMonitorAggregationCompletedThrough(ctx context.Context) (int64, error) {
	var state ChannelMonitorAggregationState
	err := DB.WithContext(ctx).
		Select("completed_through").
		Where("id = ?", channelMonitorAggregationStateID).
		Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return state.CompletedThrough, nil
}

func AdvanceChannelMonitorAggregationCompletedThrough(ctx context.Context, completedThrough int64) error {
	if completedThrough <= 0 {
		return nil
	}
	updatedAt := common.GetTimestamp()
	state := ChannelMonitorAggregationState{
		ID:               channelMonitorAggregationStateID,
		CompletedThrough: completedThrough,
		UpdatedAt:        updatedAt,
	}
	if err := DB.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&state).Error; err != nil {
		return err
	}
	return DB.WithContext(ctx).
		Model(&ChannelMonitorAggregationState{}).
		Where("id = ? AND completed_through < ?", channelMonitorAggregationStateID, completedThrough).
		Updates(map[string]any{
			"completed_through": completedThrough,
			"updated_at":        updatedAt,
		}).Error
}
