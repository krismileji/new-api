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
	CoveredFrom      int64 `gorm:"not null;default:0"`
	CompletedThrough int64 `gorm:"not null"`
	Revision         int64 `gorm:"not null;default:0"`
	UpdatedAt        int64 `gorm:"not null"`
}

type ChannelMonitorAggregationCoverage struct {
	CoveredFrom      int64 `json:"covered_from"`
	CompletedThrough int64 `json:"completed_through"`
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
	startTimestamp int64,
	completedThrough int64,
	publishWatermark bool,
	extendCoverage bool,
) error {
	updates := map[string]any{
		"revision":   gorm.Expr("revision + ?", 1),
		"updated_at": common.GetTimestamp(),
	}
	if publishWatermark && completedThrough > state.CompletedThrough {
		updates["completed_through"] = completedThrough
	}
	if extendCoverage {
		coverageAnchor := state.CoveredFrom
		if coverageAnchor <= 0 {
			coverageAnchor = state.CompletedThrough
		}
		connected := coverageAnchor > 0 && startTimestamp <= coverageAnchor && completedThrough >= coverageAnchor
		if coverageAnchor <= 0 && publishWatermark {
			connected = true
		}
		if connected && (state.CoveredFrom <= 0 || startTimestamp < state.CoveredFrom) {
			updates["covered_from"] = startTimestamp
		}
	}
	return tx.Model(&ChannelMonitorAggregationState{}).
		Where("id = ?", channelMonitorAggregationStateID).
		Updates(updates).Error
}

func GetChannelMonitorAggregationCoverage(ctx context.Context) (ChannelMonitorAggregationCoverage, error) {
	var state ChannelMonitorAggregationState
	err := DB.WithContext(ctx).
		Select("covered_from", "completed_through").
		Where("id = ?", channelMonitorAggregationStateID).
		Take(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelMonitorAggregationCoverage{}, nil
	}
	if err != nil {
		return ChannelMonitorAggregationCoverage{}, err
	}
	return ChannelMonitorAggregationCoverage{
		CoveredFrom: state.CoveredFrom, CompletedThrough: state.CompletedThrough,
	}, nil
}

func GetChannelMonitorAggregationCompletedThrough(ctx context.Context) (int64, error) {
	coverage, err := GetChannelMonitorAggregationCoverage(ctx)
	if err != nil {
		return 0, err
	}
	return coverage.CompletedThrough, nil
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

func TrimChannelMonitorAggregationCoverage(ctx context.Context, coveredFrom int64) error {
	if coveredFrom <= 0 || !DB.Migrator().HasTable(&ChannelMonitorAggregationState{}) {
		return nil
	}
	return DB.WithContext(ctx).
		Model(&ChannelMonitorAggregationState{}).
		Where("id = ? AND covered_from > ? AND covered_from < ?", channelMonitorAggregationStateID, 0, coveredFrom).
		Updates(map[string]any{
			"covered_from": coveredFrom,
			"revision":     gorm.Expr("revision + ?", 1),
			"updated_at":   common.GetTimestamp(),
		}).Error
}
