package model

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A later successful write cannot repair missing income. Preserve its period
// without declaring unrelated future days incomplete.
type ChannelMonitorIncomeGap struct {
	ID          int64  `gorm:"primaryKey"`
	GapKey      string `gorm:"size:64;not null;uniqueIndex"`
	ChannelID   int    `gorm:"not null"` // zero means the channel is unknown
	From        int64  `gorm:"column:from_at;not null;index"`
	To          int64  `gorm:"column:to_at;not null;index"`
	CostEventID string `gorm:"-"`
}

var channelMonitorPendingIncomeGaps sync.Map

func MarkChannelMonitorIncomeGapAt(ctx context.Context, channelID int, occurredAt int64) {
	MarkChannelMonitorIncomeGapForCost(ctx, channelID, occurredAt, "")
}

func MarkChannelMonitorIncomeGapForCost(ctx context.Context, channelID int, occurredAt int64, eventID string) {
	if occurredAt <= 0 {
		occurredAt = time.Now().Unix()
	}
	day := ChannelDailyCostDayStart(occurredAt)
	gap := ChannelMonitorIncomeGap{GapKey: fmt.Sprintf("%d:%d", day, max(channelID, 0)), ChannelID: max(channelID, 0), From: day, To: day + 86400}
	if eventID != "" && len(eventID) <= 64 {
		gap.GapKey = ChannelMonitorIncomeKey(eventID, "gap")
		gap.CostEventID, gap.To = eventID, math.MaxInt64
	}
	channelMonitorPendingIncomeGaps.Store(gap.GapKey, gap)
	if err := persistChannelMonitorIncomeGap(gap); err != nil {
		common.SysError("持久化收入缺口日志失败: " + err.Error())
	}
	FlushChannelMonitorIncomeGaps(ctx)
}

func PendingChannelMonitorIncomeGaps() []ChannelMonitorIncomeGap {
	var gaps []ChannelMonitorIncomeGap
	channelMonitorPendingIncomeGaps.Range(func(_, value any) bool {
		gaps = append(gaps, value.(ChannelMonitorIncomeGap))
		return true
	})
	return gaps
}

func FlushChannelMonitorIncomeGaps(ctx context.Context) {
	if DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	for _, gap := range PendingChannelMonitorIncomeGaps() {
		if err := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&gap).Error; err != nil {
			return
		}
		if gap.CostEventID != "" {
			// Serialize with cost projection even if it committed before this
			// gap could be inserted. The journal retains the association on error.
			err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				var event ChannelDailyCostOutbox
				if err := lockForUpdate(tx).Where("event_id = ?", gap.CostEventID).Find(&event).Error; err != nil {
					return err
				}
				if event.ProcessedAt == 0 {
					return nil
				}
				day := ChannelDailyCostDayStart(event.OccurredAt)
				return tx.Model(&ChannelMonitorIncomeGap{}).Where("gap_key = ?", gap.GapKey).
					Updates(map[string]any{"from_at": day, "to_at": day + 86400}).Error
			})
			if err != nil {
				return
			}
		}
		channelMonitorPendingIncomeGaps.Delete(gap.GapKey)
	}
}

// Legacy versions only saved the first failure. Preserve that entire interval
// through the upgrade day instead of erasing it or inventing recovered income.
func migrateChannelMonitorIncomeGaps(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var state ChannelMonitorIncomeState
		if err := lockForUpdate(tx).First(&state, 1).Error; err != nil {
			return err
		}
		if state.GapSince == 0 {
			return nil
		}
		gap := ChannelMonitorIncomeGap{GapKey: fmt.Sprintf("legacy:%d", state.GapSince), From: ChannelDailyCostDayStart(state.GapSince), To: ChannelDailyCostDayStart(time.Now().Unix()) + 86400}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&gap).Error; err != nil {
			return err
		}
		return tx.Model(&state).Update("gap_since", 0).Error
	})
}
