package model

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Separate progress from the income row so a failed/locked settlement cannot
// block moving to the next account. Each queue has its own bounded sweep.
type ChannelMonitorIncomeRecoveryCursor struct {
	Kind      string `gorm:"size:16;primaryKey"`
	AfterID   int64  `gorm:"not null"`
	ThroughID int64  `gorm:"not null"`
	Revision  string `gorm:"size:36;not null"`
}

func loadChannelMonitorRecoveryBatch[T any](ctx context.Context, kind string, query *gorm.DB, limit int) ([]T, *ChannelMonitorIncomeRecoveryCursor, error) {
	cursor := &ChannelMonitorIncomeRecoveryCursor{Kind: kind, Revision: common.GetUUID()}
	createErr := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(cursor).Error
	if err := DB.WithContext(ctx).Where("kind = ?", kind).Take(cursor).Error; err != nil {
		return nil, nil, errors.Join(createErr, err)
	}
	query = query.WithContext(ctx).Model(new(T)).Session(&gorm.Session{})
	var records []T
	if cursor.ThroughID > 0 {
		if err := query.Where("id > ? AND id <= ?", cursor.AfterID, cursor.ThroughID).
			Order("id ASC").Limit(min(limit, 100)).Find(&records).Error; err != nil {
			return nil, nil, err
		}
	}
	if len(records) == 0 {
		var last struct{ ID int64 }
		err := query.Select("id").Order("id DESC").Take(&last).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, cursor, nil
		}
		if err != nil {
			return nil, nil, err
		}
		cursor.AfterID, cursor.ThroughID = 0, last.ID
		if err := query.Where("id <= ?", cursor.ThroughID).Order("id ASC").Limit(min(limit, 100)).Find(&records).Error; err != nil {
			return nil, nil, err
		}
	}
	return records, cursor, nil
}

func (cursor *ChannelMonitorIncomeRecoveryCursor) advance(ctx context.Context, id int64) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	revision := common.GetUUID()
	result := DB.WithContext(ctx).Model(&ChannelMonitorIncomeRecoveryCursor{}).
		Where("kind = ? AND revision = ?", cursor.Kind, cursor.Revision).
		Updates(map[string]any{"after_id": id, "through_id": cursor.ThroughID, "revision": revision})
	if result.Error != nil {
		// A lost commit response may still have persisted our exact revision.
		// Verify before proceeding; never infer success from the target ID.
		var saved ChannelMonitorIncomeRecoveryCursor
		if err := DB.WithContext(ctx).Where("kind = ?", cursor.Kind).Take(&saved).Error; err != nil {
			return false, errors.Join(result.Error, err)
		}
		if saved.Revision != revision || saved.AfterID != id || saved.ThroughID != cursor.ThroughID {
			return false, result.Error
		}
	} else if result.RowsAffected != 1 {
		return false, nil // Another worker owns the newer scan position.
	}
	cursor.AfterID, cursor.Revision = id, revision
	return true, nil
}
