package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
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
	// Only funding_pending rows carry replayable final-settlement intent.
	// Older pending rows have no such evidence and must never be replayed.
	FundingDelta          int
	FundingTokenID        int
	FundingSubscriptionID int
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
	if master {
		if err := migrateChannelMonitorIncomeGaps(db); err != nil {
			return err
		}
		channelMonitorIncomeGap.Store(0)
		if err := migrateChannelMonitorIncomeParity(db); err != nil {
			return err
		}
	} else {
		var pending []int64
		if err := db.Model(&ChannelMonitorIncome{}).Where("usd_to_cny <> ?", "1").Limit(1).Pluck("id", &pending).Error; err != nil {
			return err
		}
		if len(pending) > 0 {
			return errors.New("渠道收入尚未完成 1:1 修正，请先升级并启动主节点")
		}
	}
	if err := os.MkdirAll(channelMonitorIncomeGapDirectory(), 0700); err != nil {
		common.SysError("收入缺口日志不可用，利润保持待确认: " + err.Error())
	} else if err := RestoreChannelMonitorIncomeGaps(context.Background()); err != nil {
		common.SysError("收入缺口日志恢复失败，利润保持待确认: " + err.Error())
	}
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
	MarkChannelMonitorIncomeGapAt(ctx, 0, time.Now().Unix())
}

func ChannelMonitorIncomeGapSince() int64 { return channelMonitorIncomeGap.Load() }

func PrepareChannelMonitorIncome(ctx context.Context, record *ChannelMonitorIncome) error {
	if record == nil || record.ChannelID <= 0 || record.UserID < 0 || len(record.SettlementKey) != 64 ||
		len(record.APIKeyKey) > 64 || len(record.APIKeyName) > 255 || len(record.ModelName) > 255 || len(record.GroupName) > 255 ||
		(record.BillingSource != "wallet" && record.BillingSource != "subscription") {
		return errors.New("收入结算记录无效")
	}
	// Platform credits are sold 1:1 with CNY. Display currency settings must
	// never inflate the income used for channel profit.
	record.USDToCNY = "1"
	amount, err := ChannelMonitorIncomeAmount(record.Quota, record.QuotaPerUnit, record.USDToCNY)
	if err != nil {
		return err
	}
	record.IncomeNanoCNY = amount
	record.ModelKey = ChannelMonitorDailyCostModelKey(record.ModelName)
	if record.Status == "funding_pending" {
		reserved := record.Quota - int64(record.FundingDelta)
		if record.FundingDelta < -common.MaxQuota || record.FundingDelta > common.MaxQuota || reserved < 0 || reserved > common.MaxQuota ||
			record.FundingTokenID < 0 || record.BillingSource == taskBillingSubscriptionSource && record.FundingSubscriptionID <= 0 {
			return errors.New("收入结算恢复参数无效")
		}
	} else {
		record.Status = "pending"
	}
	record.CreatedAt = time.Now().Unix()
	record.UpdatedAt = record.CreatedAt
	record.DayStart = ChannelDailyCostDayStart(record.CreatedAt)
	// Use an actual INSERT: MySQL clientFoundRows can report one affected row
	// for ON DUPLICATE KEY DO NOTHING, which is not evidence of a new record.
	created := DB.WithContext(ctx).Create(record)
	var saved ChannelMonitorIncome
	if err = DB.WithContext(ctx).Where("settlement_key = ?", record.SettlementKey).First(&saved).Error; err != nil {
		if created.Error == nil && created.RowsAffected == 1 && record.ID > 0 {
			// A successful INSERT is already durable evidence. A subsequent
			// read failure cannot turn this new income into a missing record.
			common.SysError("收入已保存，确认读取等待后台恢复: " + err.Error())
			return nil
		}
		return errors.Join(created.Error, err)
	}
	if saved.UserID != record.UserID || saved.ChannelID != record.ChannelID || saved.BillingSource != record.BillingSource {
		if saved.Status != "reserved" || saved.UserID != record.UserID || saved.BillingSource != record.BillingSource {
			return errors.New("收入结算标识冲突")
		}
	}
	if saved.Status == "reserved" {
		if err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockForUpdate(tx).Where("settlement_key = ?", record.SettlementKey).First(&saved).Error; err != nil {
				return err
			}
			if saved.Status != "reserved" {
				return errors.New("预扣记录状态已改变，请重新核对")
			}
			updates := map[string]any{"channel_id": record.ChannelID, "cost_event_id": record.CostEventID,
				"cost_recorded": 0, "api_key_key": record.APIKeyKey, "api_key_name": record.APIKeyName,
				"model_key": record.ModelKey, "model_name": record.ModelName, "group_name": record.GroupName,
				"updated_at": time.Now().Unix()}
			if record.Status == "funding_pending" {
				if saved.Quota != record.Quota-int64(record.FundingDelta) || saved.FundingTokenID != record.FundingTokenID || saved.FundingSubscriptionID != record.FundingSubscriptionID {
					return errors.New("最终结算与预扣资金不匹配")
				}
				amount, err := ChannelMonitorIncomeAmount(record.Quota, saved.QuotaPerUnit, "1")
				if err != nil {
					return err
				}
				updates["quota"], updates["income_nano_cny"] = record.Quota, amount
				updates["status"], updates["funding_delta"] = "funding_pending", record.FundingDelta
			}
			// Task submissions keep reserved until task insertion and funding
			// adjustment commit together; a crash here still needs manual review.
			if err := tx.Model(&saved).Updates(updates).Error; err != nil {
				return err
			}
			return tx.First(&saved, saved.ID).Error
		}); err != nil {
			return err
		}
	}
	// An early Midjourney refund creates a placeholder before submission has
	// an income row. Fill its attribution without resurrecting refunded money.
	if saved.CostEventID == "" && record.CostEventID != "" {
		if err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockForUpdate(tx).Where("settlement_key = ?", record.SettlementKey).First(&saved).Error; err != nil {
				return err
			}
			if saved.CostEventID != "" {
				return nil
			}
			updates := map[string]any{"cost_event_id": record.CostEventID, "api_key_id": record.APIKeyID,
				"api_key_key": record.APIKeyKey, "api_key_name": record.APIKeyName, "model_key": record.ModelKey,
				"model_name": record.ModelName, "group_name": record.GroupName, "quota_per_unit": record.QuotaPerUnit,
				"day_start": record.DayStart}
			if saved.Status != "settled" {
				updates["quota"], updates["income_nano_cny"] = record.Quota, record.IncomeNanoCNY
			}
			if err := tx.Model(&saved).Updates(updates).Error; err != nil {
				return err
			}
			return tx.First(&saved, saved.ID).Error
		}); err != nil {
			return err
		}
	}
	if saved.CostRecorded == 0 && saved.CostEventID != "" {
		if err := reconcileChannelMonitorIncomeCost(ctx, &saved); err != nil {
			// The validated income and its funding intent already exist. Its
			// cost_recorded=0 is sufficient to block profit until recovery;
			// do not create a permanent missing-income gap for a retryable read.
			common.SysError("收入已保存，成本归属等待后台恢复: " + err.Error())
		}
	}
	*record = saved
	FlushChannelMonitorIncomeGaps(ctx)
	return nil
}

// Follow the cost worker's outbox-before-income lock order. Both request-time
// preparation and background recovery use this same cross-day attribution.
func reconcileChannelMonitorIncomeCost(ctx context.Context, record *ChannelMonitorIncome) error {
	day := record.DayStart
	applied := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event ChannelDailyCostOutbox
		if err := lockForUpdate(tx).Where("event_id = ?", record.CostEventID).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if event.ProcessedAt <= 0 {
			return nil
		}
		day = ChannelDailyCostDayStart(event.OccurredAt)
		if err := tx.Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND cost_event_id = ? AND cost_recorded = 0", record.SettlementKey, record.CostEventID).
			Updates(map[string]any{"cost_recorded": 1, "day_start": day}).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err == nil && applied {
		record.DayStart, record.CostRecorded = day, 1
	}
	return err
}

// RecoverChannelMonitorIncomeCosts catches income written after a cost already
// committed when its immediate attribution read failed. No money is changed.
func RecoverChannelMonitorIncomeCosts(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("收入成本恢复批次大小无效")
	}
	processed := DB.Model(&ChannelDailyCostOutbox{}).Select("event_id").Where("processed_at > 0")
	var records []ChannelMonitorIncome
	if err := DB.WithContext(ctx).Where("cost_recorded = 0 AND cost_event_id IN (?)", processed).
		Order("updated_at, id").Limit(min(limit, 100)).Find(&records).Error; err != nil {
		return 0, err
	}
	completed := 0
	var failures error
	for _, record := range records {
		if ctx.Err() != nil {
			return completed, errors.Join(failures, ctx.Err())
		}
		if err := reconcileChannelMonitorIncomeCost(ctx, &record); err != nil {
			failures = errors.Join(failures, fmt.Errorf("收入记录 %d 成本归属恢复失败: %w", record.ID, err))
			if updateErr := DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("id = ? AND cost_recorded = 0", record.ID).Update("updated_at", time.Now().Unix()).Error; updateErr != nil {
				failures = errors.Join(failures, updateErr)
			}
			continue
		}
		if record.CostRecorded == 1 {
			completed++
		}
	}
	return completed, failures
}

func ConfirmChannelMonitorIncome(ctx context.Context, key string) error {
	return DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND status IN ?", key, []string{"pending", "refund_unfunded"}).
		Updates(map[string]any{"status": gorm.Expr("CASE WHEN status = 'refund_unfunded' THEN 'refund_pending' ELSE 'settled' END"), "updated_at": time.Now().Unix()}).Error
}

// QueueChannelMonitorIncomeFunding transfers final settlement to durable
// recovery. After success the caller must not refund the reservation separately.
func QueueChannelMonitorIncomeFunding(ctx context.Context, record *ChannelMonitorIncome, subscriptionID, tokenID, delta int) error {
	if record == nil || delta < -common.MaxQuota || delta > common.MaxQuota || tokenID < 0 ||
		(record.BillingSource != "wallet" && record.BillingSource != taskBillingSubscriptionSource) ||
		(record.BillingSource == taskBillingSubscriptionSource && subscriptionID <= 0) {
		return errors.New("收入结算恢复参数无效")
	}
	err := withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		var saved ChannelMonitorIncome
		if err := lockForUpdate(tx).Where("settlement_key = ?", record.SettlementKey).First(&saved).Error; err != nil {
			return err
		}
		if saved.UserID != record.UserID || saved.BillingSource != record.BillingSource || saved.Quota != record.Quota {
			return errors.New("收入结算恢复快照不匹配")
		}
		if saved.Status == "settled" {
			return nil
		}
		if saved.Status == "funding_pending" {
			if saved.FundingDelta != delta || saved.FundingTokenID != tokenID || saved.FundingSubscriptionID != subscriptionID {
				return errors.New("收入结算恢复指令冲突")
			}
			return nil
		}
		if saved.Status != "pending" {
			return errors.New("收入结算状态无效")
		}
		return tx.Model(&saved).Updates(map[string]any{
			"status": "funding_pending", "funding_delta": delta, "funding_token_id": tokenID,
			"funding_subscription_id": subscriptionID, "updated_at": time.Now().Unix(),
		}).Error
	})
	if err == nil {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var saved ChannelMonitorIncome
	if checkErr := DB.WithContext(checkCtx).Where("settlement_key = ?", record.SettlementKey).First(&saved).Error; checkErr != nil {
		if errors.Is(checkErr, gorm.ErrRecordNotFound) {
			return err
		}
		return errors.Join(ErrTaskBillingCommitUncertain, err, checkErr)
	}
	if saved.UserID == record.UserID && saved.BillingSource == record.BillingSource && saved.Quota == record.Quota &&
		saved.FundingDelta == delta && saved.FundingTokenID == tokenID && saved.FundingSubscriptionID == subscriptionID &&
		(saved.Status == "funding_pending" || saved.Status == "settled") {
		return nil
	}
	return err
}

// RecoverChannelMonitorIncomeFunding reads only durable instructions, never
// guesses funding from target quota or from an old pending monitoring record.
func RecoverChannelMonitorIncomeFunding(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("收入恢复批次大小无效")
	}
	var records []ChannelMonitorIncome
	if err := DB.WithContext(ctx).Where("status = ?", "funding_pending").Order("updated_at, id").Limit(min(limit, 100)).Find(&records).Error; err != nil {
		return 0, err
	}
	var failures error
	completed := 0
	for _, record := range records {
		if ctx.Err() != nil {
			return completed, errors.Join(failures, ctx.Err())
		}
		if err := SettleChannelMonitorIncomeFunding(ctx, record.SettlementKey, record.UserID, record.FundingSubscriptionID, record.FundingTokenID, record.FundingDelta); err != nil {
			failures = errors.Join(failures, fmt.Errorf("收入记录 %d 恢复失败: %w", record.ID, err))
			// Rotate failures so one permanently invalid record cannot starve
			// the next batch of otherwise recoverable settlements.
			if updateErr := DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("id = ? AND status = ?", record.ID, "funding_pending").Update("updated_at", time.Now().Unix()).Error; updateErr != nil {
				failures = errors.Join(failures, updateErr)
			}
			continue
		}
		completed++
	}
	return completed, failures
}

// SettleChannelMonitorIncomeFunding commits the money and its confirmation in
// one transaction. A pending row is never used as evidence of a prior charge.
func SettleChannelMonitorIncomeFunding(ctx context.Context, key string, userID, subscriptionID, tokenID, delta int) error {
	if delta < -common.MaxQuota || delta > common.MaxQuota {
		return errors.New("收入结算差额超出范围")
	}
	var tokenKey string
	var source string
	var applied bool
	err := withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		applied = false
		var income ChannelMonitorIncome
		if err := lockForUpdate(tx).Where("settlement_key = ?", key).First(&income).Error; err != nil {
			return err
		}
		if income.UserID != userID {
			return errors.New("收入结算用户不匹配")
		}
		if income.Status == "settled" {
			return nil
		}
		if income.Status != "pending" && income.Status != "funding_pending" {
			return errors.New("收入结算状态无效")
		}
		if income.Status == "funding_pending" && (income.FundingDelta != delta || income.FundingTokenID != tokenID || income.FundingSubscriptionID != subscriptionID) {
			return errors.New("收入结算恢复指令不匹配")
		}
		source = income.BillingSource
		if source == taskBillingSubscriptionSource && subscriptionID <= 0 {
			return errors.New("收入结算缺少订阅")
		}
		task := Task{UserId: userID, PrivateData: TaskPrivateData{BillingSource: source, SubscriptionId: subscriptionID}}
		if err := applyTaskFundingDelta(tx, &task, delta); err != nil {
			return err
		}
		var err error
		tokenKey, err = applyTaskTokenDelta(tx, tokenID, delta)
		if err != nil {
			return err
		}
		result := tx.Model(&income).Where("status = ?", income.Status).Updates(map[string]any{"status": "settled", "updated_at": time.Now().Unix()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("income settlement changed concurrently")
		}
		applied = true
		return nil
	})
	if applied {
		// A reader may already have cached the committed balance. Never apply
		// the delta again, and invalidate even if COMMIT/readback is uncertain.
		invalidateChannelMonitorFundingCache(userID, source, tokenKey, delta)
	}
	if err != nil {
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var income ChannelMonitorIncome
		checkErr := DB.WithContext(checkCtx).Where("settlement_key = ?", key).First(&income).Error
		if checkErr != nil {
			return errors.Join(ErrTaskBillingCommitUncertain, err, checkErr)
		}
		if income.Status != "settled" || income.UserID != userID {
			return err
		}
	}
	return nil
}

// MarkChannelMonitorIncomeRefundPending hides a charge from confirmed profit
// before a refund changes the user's balance.
func MarkChannelMonitorIncomeRefundPending(ctx context.Context, key string) (bool, bool, error) {
	result := DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND status IN ?", key, []string{"settled", "pending"}).
		Updates(map[string]any{"status": gorm.Expr("CASE WHEN status = 'settled' THEN 'refund_pending' ELSE 'refund_unfunded' END"), "updated_at": time.Now().Unix()})
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
	return DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("settlement_key = ? AND status IN ?", key, []string{"refund_pending", "refund_unfunded"}).
		Updates(map[string]any{"status": gorm.Expr("CASE WHEN status = 'refund_pending' THEN 'settled' ELSE 'pending' END"), "updated_at": time.Now().Unix()}).Error
}

// Reserve a refund row even if polling wins before submission prepares income.
// Its distinct status prevents submission confirmation from clearing the refund.
func PrepareChannelMonitorMidjourneyRefund(ctx context.Context, task *Midjourney) error {
	key := ChannelMonitorIncomeKey(fmt.Sprint(task.Id), "midjourney")
	occurredAt := task.SubmitTime / 1000
	if occurredAt <= 0 {
		occurredAt = time.Now().Unix()
	}
	record := ChannelMonitorIncome{SettlementKey: key, ChannelID: task.GetBillingChannelId(), UserID: task.UserId,
		BillingSource: "wallet", QuotaPerUnit: fmt.Sprint(common.QuotaPerUnit), USDToCNY: "1",
		DayStart: ChannelDailyCostDayStart(occurredAt), Status: "pending", CreatedAt: time.Now().Unix()}
	return DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error
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
	journalIncomplete, err := cleanupChannelMonitorIncomeGapJournal(ctx, cutoff, budget.Slice(3))
	if err != nil {
		common.SysError("清理收入缺口日志失败: " + err.Error())
		journalIncomplete = true
	}
	gapBudget := budget.Slice(2)
	for !gapBudget.Exhausted() {
		var ids []int64
		gaps := DB.WithContext(ctx).Model(&ChannelMonitorIncomeGap{}).Where("to_at <= ?", cutoff)
		if journalIncomplete {
			// Event journals need their final-day rows until their files have
			// been removed. Otherwise a later replay would reopen the interval.
			gaps = gaps.Where("LENGTH(gap_key) <> ?", 64)
		}
		if err := gaps.
			Order("to_at, id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return false, err
		}
		if len(ids) == 0 {
			break
		}
		if err := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ChannelMonitorIncomeGap{}).Error; err != nil {
			return false, err
		}
	}
	journalIncomplete = journalIncomplete || gapBudget.Exhausted()
	for !budget.Exhausted() {
		var ids []int64
		if err := DB.WithContext(ctx).Model(&ChannelMonitorIncome{}).Where("day_start < ? AND status NOT IN ?", cutoff, []string{"funding_pending", "reserved"}).
			Order("day_start, id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return false, err
		}
		if len(ids) == 0 {
			return journalIncomplete, nil
		}
		if err := DB.WithContext(ctx).Where("id IN ? AND status NOT IN ?", ids, []string{"funding_pending", "reserved"}).Delete(&ChannelMonitorIncome{}).Error; err != nil {
			return false, err
		}
	}
	return true, nil
}

func CompleteChannelMonitorTaskIncome(ctx context.Context, taskID int64) error {
	if !ChannelMonitorIncomeReady.Load() {
		return nil
	}
	// Polling may have refunded or supplemented the task before its initial
	// income existed. Follow polling's task-before-income lock order and read
	// the durable quota instead of the HTTP request's stale snapshot.
	return withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.PrivateData.Execution == nil || task.PrivateData.Execution.RequestID == "" {
			return errors.New("任务收入缺少请求标识")
		}
		var income ChannelMonitorIncome
		if err := lockForUpdate(tx).Where("settlement_key = ?", ChannelMonitorIncomeKey(task.PrivateData.Execution.RequestID, "request")).First(&income).Error; err != nil {
			return err
		}
		if err := correctTaskChannelMonitorIncome(tx, &task, task.Quota); err != nil {
			return err
		}
		return tx.Model(&ChannelMonitorIncome{}).
			Where("settlement_key = ?", ChannelMonitorIncomeKey(task.PrivateData.Execution.RequestID, "request")).
			Update("day_start", ChannelDailyCostDayStart(task.SubmitTime)).Error
	})
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
		gapKeys := make([]string, 0, len(eventIDs))
		for _, eventID := range eventIDs {
			gapKeys = append(gapKeys, ChannelMonitorIncomeKey(eventID, "gap"))
		}
		if err := tx.Model(&ChannelMonitorIncomeGap{}).Where("gap_key IN ?", gapKeys).
			Updates(map[string]any{"from_at": day, "to_at": day + 86400}).Error; err != nil {
			return err
		}
		if err := tx.Model(&ChannelMonitorIncome{}).Where("cost_recorded = 0 AND cost_event_id IN ?", eventIDs).
			Updates(map[string]any{"cost_recorded": 1, "day_start": day}).Error; err != nil {
			return err
		}
	}
	return nil
}

// Task corrections share the funding transaction and reuse the original quota
// unit. A repeated callback replaces the total, never adds it twice.
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
	amount, err := ChannelMonitorIncomeAmount(int64(quota), record.QuotaPerUnit, "1")
	if err != nil {
		return err
	}
	updates := map[string]any{"quota": quota, "income_nano_cny": amount, "usd_to_cny": "1", "status": "settled", "updated_at": time.Now().Unix()}
	if task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.ChannelCostResolved {
		updates["cost_recorded"] = 1
	}
	return tx.Model(&record).Updates(updates).Error
}
