package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Income is independent of optional consume logs. A pending row is written
// before settlement so a crash between funding and monitoring remains visible.
type ChannelMonitorIncome struct {
	ID            int64  `gorm:"primaryKey"`
	SettlementKey string `gorm:"size:64;not null;uniqueIndex"`
	DayStart      int64  `gorm:"not null;index:idx_cm_income_day_channel,priority:1"`
	ChannelID     int    `gorm:"not null;index:idx_cm_income_day_channel,priority:2"`
	UserID        int    `gorm:"not null;index"`
	APIKeyID      int    `gorm:"not null;index"`
	APIKeyKey     string `gorm:"size:64;not null"`
	APIKeyName    string `gorm:"size:255;not null"`
	ModelKey      string `gorm:"size:64;not null"`
	ModelName     string `gorm:"size:255;not null"`
	GroupName     string `gorm:"size:255;not null"`
	BillingSource string `gorm:"size:16;not null"`
	QuotaPerUnit  string `gorm:"size:64;not null"`
	USDToCNY      string `gorm:"size:64;not null"`
	Quota         int64  `gorm:"not null"`
	IncomeNanoCNY int64  `gorm:"not null"`
	Status        string `gorm:"size:16;not null;index"`
	CostEventID   string `gorm:"size:64;not null;index"`
	CostRecorded  int    `gorm:"not null"`
	CreatedAt     int64  `gorm:"not null"`
	UpdatedAt     int64  `gorm:"not null"`
}

type ChannelMonitorIncomeState struct {
	ID           int   `gorm:"primaryKey;autoIncrement:false"`
	StartedAt    int64 `gorm:"not null"`
	GapSince     int64 `gorm:"not null"`
	RetainedFrom int64 `gorm:"not null"`
}

var ChannelMonitorIncomeReady atomic.Bool
var channelMonitorIncomeGap atomic.Int64

func InitializeChannelMonitorIncome(db *gorm.DB, master bool) error {
	ChannelMonitorIncomeReady.Store(false)
	channelMonitorIncomeGap.Store(0)
	if db == nil {
		return errors.New("数据库不可用，无法初始化渠道监控收入")
	}
	if master {
		state := ChannelMonitorIncomeState{ID: 1, StartedAt: time.Now().Unix()}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error; err != nil {
			return err
		}
	} else if !db.Migrator().HasTable(&ChannelMonitorIncomeState{}) {
		return nil
	}
	var state ChannelMonitorIncomeState
	if err := db.First(&state, 1).Error; err != nil {
		if !master && errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	channelMonitorIncomeGap.Store(state.GapSince)
	ChannelMonitorIncomeReady.Store(true)
	return nil
}

func ChannelMonitorIncomeKey(requestID, kind string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(kind+":"+requestID)))
}

func ChannelMonitorIncomeAmount(quota int64, quotaPerUnit, usdToCNY string) (int64, error) {
	unit, unitErr := decimal.NewFromString(quotaPerUnit)
	rate, rateErr := decimal.NewFromString(usdToCNY)
	if quota < 0 || quota > int64(common.MaxQuota) || unitErr != nil || rateErr != nil || !unit.IsPositive() || !rate.IsPositive() {
		return 0, errors.New("收入额度或人民币换算参数无效")
	}
	amount := decimal.NewFromInt(quota).Mul(rate).Mul(decimal.NewFromInt(ChannelDailyCostNanoPerCNY)).DivRound(unit, 0)
	if amount.IsNegative() || amount.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, errors.New("收入金额超出可记录范围")
	}
	return amount.IntPart(), nil
}

// Mark gaps independently from logs. The in-memory marker is also flushed by
// the next successful prepare if the database itself was temporarily down.
func MarkChannelMonitorIncomeGap(ctx context.Context) {
	now := time.Now().Unix()
	channelMonitorIncomeGap.CompareAndSwap(0, now)
	if DB != nil {
		DB.WithContext(ctx).Model(&ChannelMonitorIncomeState{}).Where("id = 1 AND gap_since = 0").Update("gap_since", channelMonitorIncomeGap.Load())
	}
}

func ChannelMonitorIncomeGapSince() int64 { return channelMonitorIncomeGap.Load() }

func PrepareChannelMonitorIncome(ctx context.Context, record *ChannelMonitorIncome) error {
	if record == nil || record.ChannelID <= 0 || record.UserID < 0 || len(record.SettlementKey) != 64 ||
		len(record.APIKeyKey) > 64 || len(record.APIKeyName) > 255 || len(record.ModelName) > 255 || len(record.GroupName) > 255 ||
		(record.BillingSource != "wallet" && record.BillingSource != "subscription") {
		return errors.New("收入结算记录无效")
	}
	amount, err := ChannelMonitorIncomeAmount(record.Quota, record.QuotaPerUnit, record.USDToCNY)
	if err != nil {
		return err
	}
	record.IncomeNanoCNY = amount
	record.ModelKey = ChannelMonitorDailyCostModelKey(record.ModelName)
	record.Status = "pending"
	record.CreatedAt = time.Now().Unix()
	record.UpdatedAt = record.CreatedAt
	record.DayStart = ChannelDailyCostDayStart(record.CreatedAt)
	if err = DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(record).Error; err != nil {
		return err
	}
	var saved ChannelMonitorIncome
	if err = DB.WithContext(ctx).Where("settlement_key = ?", record.SettlementKey).First(&saved).Error; err != nil {
		return err
	}
	if saved.UserID != record.UserID || saved.ChannelID != record.ChannelID || saved.BillingSource != record.BillingSource {
		return errors.New("收入结算标识冲突")
	}
	if saved.CostRecorded == 0 && saved.CostEventID != "" {
		// Include unprocessed events in the lock so an in-flight projection
		// cannot miss this income while we miss its uncommitted cost. Insert
		// income first, then follow the worker's outbox-before-income lock order.
		err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var applied ChannelDailyCostOutbox
			if err := lockForUpdate(tx).Where("event_id = ?", saved.CostEventID).First(&applied).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			if applied.ProcessedAt <= 0 {
				return nil
			}
			saved.DayStart = ChannelDailyCostDayStart(applied.OccurredAt)
			if err := tx.Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND cost_recorded = 0", saved.SettlementKey).
				Updates(map[string]any{"cost_recorded": 1, "day_start": saved.DayStart}).Error; err != nil {
				return err
			}
			saved.CostRecorded = 1
			return nil
		})
		if err != nil {
			return err
		}
	}
	*record = saved
	if channelMonitorIncomeGap.Load() > 0 {
		MarkChannelMonitorIncomeGap(ctx)
	}
	return nil
}

func ConfirmChannelMonitorIncome(ctx context.Context, key string) error {
	return DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND status = ?", key, "pending").
		Updates(map[string]any{"status": "settled", "updated_at": time.Now().Unix()}).Error
}

// MarkChannelMonitorIncomeRefundPending hides a charge from confirmed profit
// before a refund changes the user's balance.
func MarkChannelMonitorIncomeRefundPending(ctx context.Context, key string) (bool, bool, error) {
	result := DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND status = ?", key, "settled").
		Updates(map[string]any{"status": "pending", "updated_at": time.Now().Unix()})
	if result.Error != nil {
		return false, false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, true, nil
	}
	var count int64
	err := DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ?", key).Count(&count).Error
	return count > 0, false, err
}

// CancelChannelMonitorIncomeRefund restores the prior settled charge when the
// wallet refund itself failed.
func CancelChannelMonitorIncomeRefund(ctx context.Context, key string) error {
	return DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND status = ?", key, "pending").
		Updates(map[string]any{"status": "settled", "updated_at": time.Now().Unix()}).Error
}

func RefundChannelMonitorIncome(ctx context.Context, key string) error {
	return DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ?", key).
		Updates(map[string]any{"quota": 0, "income_nano_cny": 0, "status": "settled", "updated_at": time.Now().Unix()}).Error
}

// Advance the durable boundary before deleting either side of the profit
// equation. Retrying cleanup can never expose expired costs as zero costs.
func DeleteChannelMonitorIncomeBefore(ctx context.Context, cutoff int64, batchSize int, budget ChannelMonitorCleanupBudget) (bool, error) {
	if !ChannelMonitorIncomeReady.Load() {
		return false, nil
	}
	if err := DB.WithContext(ctx).Model(&ChannelMonitorIncomeState{}).
		Where("id = 1 AND retained_from < ?", cutoff).Update("retained_from", cutoff).Error; err != nil {
		return false, err
	}
	for !budget.Exhausted() {
		var ids []int64
		if err := DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("day_start < ?", cutoff).
			Order("day_start, id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return false, err
		}
		if len(ids) == 0 {
			return false, nil
		}
		if err := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ChannelMonitorIncome{}).Error; err != nil {
			return false, err
		}
	}
	return true, nil
}

func CompleteChannelMonitorTaskIncome(ctx context.Context, requestID string, submittedAt int64, costRecorded bool) error {
	if !ChannelMonitorIncomeReady.Load() {
		return nil
	}
	updates := map[string]any{"day_start": ChannelDailyCostDayStart(submittedAt)}
	if costRecorded {
		updates["cost_recorded"] = 1
	}
	return DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ?", ChannelMonitorIncomeKey(requestID, "request")).Updates(updates).Error
}

func ConfirmChannelMonitorCostIncome(tx *gorm.DB, events []ChannelDailyCostOutbox) error {
	if !ChannelMonitorIncomeReady.Load() || len(events) == 0 {
		return nil
	}
	// Keep both sides on the same day even if a request or realtime session
	// crosses midnight before its cost is finalized.
	byDay := make(map[int64][]string)
	for _, event := range events {
		day := ChannelDailyCostDayStart(event.OccurredAt)
		byDay[day] = append(byDay[day], event.EventId)
	}
	for day, eventIDs := range byDay {
		if err := tx.Model(&ChannelMonitorIncome{}).Where("cost_recorded = 0 AND cost_event_id IN ?", eventIDs).
			Updates(map[string]any{"cost_recorded": 1, "day_start": day}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Task corrections share the funding transaction and reuse the original FX
// snapshot. A repeated callback replaces the total, never adds it twice.
func correctTaskChannelMonitorIncome(tx *gorm.DB, task *Task, quota int) error {
	if !ChannelMonitorIncomeReady.Load() || task.PrivateData.Execution == nil || strings.TrimSpace(task.PrivateData.Execution.RequestID) == "" {
		return nil
	}
	key := ChannelMonitorIncomeKey(task.PrivateData.Execution.RequestID, "request")
	var record ChannelMonitorIncome
	err := lockForUpdate(tx).Where("settlement_key = ?", key).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	amount, err := ChannelMonitorIncomeAmount(int64(quota), record.QuotaPerUnit, record.USDToCNY)
	if err != nil {
		return err
	}
	updates := map[string]any{"quota": quota, "income_nano_cny": amount, "status": "settled", "updated_at": time.Now().Unix()}
	if task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.ChannelCostResolved {
		updates["cost_recorded"] = 1
	}
	return tx.Model(&record).Updates(updates).Error
}
