package model

import (
	"fmt"

	"gorm.io/gorm"
)

// Repair only monitoring amounts, using each record's original quota unit.
// Committing bounded batches makes an interrupted upgrade safe to resume.
// Lock before reading quota so task refunds/adjustments cannot be overwritten
// with an amount computed from stale quota.
func migrateChannelMonitorIncomeParity(db *gorm.DB) error {
	var lastID int64
	for {
		var rows []ChannelMonitorIncome
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := lockForUpdate(tx).Where("id > ? AND usd_to_cny <> ?", lastID, "1").
				Order("id").Limit(250).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				amount, err := ChannelMonitorIncomeAmount(row.Quota, row.QuotaPerUnit, "1")
				if err != nil {
					return fmt.Errorf("修正渠道收入记录 %d 失败: %w", row.ID, err)
				}
				if err := tx.Model(&ChannelMonitorIncome{}).Where("id = ?", row.ID).
					Updates(map[string]any{"income_nano_cny": amount, "usd_to_cny": "1", "updated_at": row.UpdatedAt}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		lastID = rows[len(rows)-1].ID
	}
}
