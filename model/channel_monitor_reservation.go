package model

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var (
	ErrChannelMonitorWalletInsufficient = errors.New("钱包预扣额度不足")
	ErrChannelMonitorTokenInsufficient  = errors.New("令牌预扣额度不足")
)

// ReserveChannelMonitorIncome persists actual reserved funds, not final usage.
// Recovery deliberately ignores reserved rows. An interrupted request requires
// an operator's evidence; it must never become an automatic refund or charge.
func ReserveChannelMonitorIncome(ctx context.Context, record *ChannelMonitorIncome, requestID string) (*SubscriptionPreConsumeResult, error) {
	if record == nil || record.ID != 0 || record.UserID <= 0 || record.ChannelID <= 0 ||
		record.SettlementKey != ChannelMonitorIncomeKey(requestID, "request") || requestID == "" ||
		len(requestID) > 64 || len(record.APIKeyKey) > 64 || len(record.APIKeyName) > 255 ||
		len(record.ModelName) > 255 || len(record.GroupName) > 255 || len(record.QuotaPerUnit) > 64 || len(record.CostEventID) > 64 ||
		record.FundingTokenID < 0 || (record.BillingSource != "wallet" && record.BillingSource != "subscription") {
		return nil, errors.New("预扣收入记录无效")
	}
	amount, err := ChannelMonitorIncomeAmount(record.Quota, record.QuotaPerUnit, "1")
	if err != nil {
		return nil, err
	}
	record.IncomeNanoCNY, record.USDToCNY, record.Status = amount, "1", "reserved"
	record.ModelKey = ChannelMonitorDailyCostModelKey(record.ModelName)
	record.CreatedAt = time.Now().Unix()
	record.UpdatedAt, record.DayStart = record.CreatedAt, ChannelDailyCostDayStart(record.CreatedAt)
	// Batch mode is fixed at startup. Without it there are no local quota
	// batches, and unrelated accounts must rely on DB row locks rather than
	// serialize behind process-wide batch locks.
	if common.BatchUpdateEnabled {
		userQuotaBatchMutationLock.Lock()
		defer userQuotaBatchMutationLock.Unlock()
		tokenQuotaBatchMutationLock.Lock()
		defer tokenQuotaBatchMutationLock.Unlock()
		// A legacy batch must be flushed before consuming its DB balance.
		if batchUpdateStores[BatchUpdateTypeUserQuota][record.UserID] != 0 ||
			batchUpdateStores[BatchUpdateTypeTokenQuota][record.FundingTokenID] != 0 {
			return nil, errors.New("预扣前仍有额度批次待落库，请稍后重试")
		}
	}
	var subscription *SubscriptionPreConsumeResult
	var tokenKey string
	now := GetDBTimestamp()
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Plain INSERT rejects a duplicate request before any debit. A caller
		// must not execute the upstream request again using an old reservation.
		if err := tx.Create(record).Error; err != nil {
			return err
		}
		if record.BillingSource == "subscription" {
			var err error
			subscription, err = preConsumeUserSubscription(tx, requestID, record.UserID, record.Quota, now, true)
			if err != nil {
				return err
			}
			record.FundingSubscriptionID = subscription.UserSubscriptionId
			var sub UserSubscription
			if err := tx.First(&sub, subscription.UserSubscriptionId).Error; err != nil {
				return err
			}
			record.SubscriptionPeriod = &sub.LastResetTime
			record.SubscriptionReserved = record.Quota
			if err := tx.Model(record).Updates(map[string]any{
				"funding_subscription_id": record.FundingSubscriptionID,
				"subscription_period":     sub.LastResetTime, "subscription_reserved": record.Quota,
			}).Error; err != nil {
				return err
			}
		} else {
			var user User
			if err := lockForUpdate(tx).First(&user, record.UserID).Error; err != nil {
				return err
			}
			if int64(user.Quota) < record.Quota {
				return ErrChannelMonitorWalletInsufficient
			}
			if err := applyTaskFundingDelta(tx, &Task{UserId: record.UserID}, int(record.Quota)); err != nil {
				return err
			}
		}
		if record.FundingTokenID > 0 {
			var token Token
			if err := lockForUpdate(tx).First(&token, record.FundingTokenID).Error; err != nil {
				return err
			}
			if token.UserId != record.UserID || !token.UnlimitedQuota && int64(token.RemainQuota) < record.Quota {
				return ErrChannelMonitorTokenInsufficient
			}
			var err error
			tokenKey, err = applyTaskTokenDelta(tx, record.FundingTokenID, int(record.Quota))
			return err
		}
		return nil
	})
	invalidateChannelMonitorFundingCache(record.UserID, record.BillingSource, tokenKey, int(record.Quota))
	if err != nil {
		// Even a lost COMMIT reply returns an error and stops upstream work.
		// If committed, the reserved row remains available for manual review.
		return nil, err
	}
	return subscription, nil
}

// AdjustChannelMonitorReservation handles live-request reserve increases.
// Neither this function nor its caller is a recovery
// worker: an orphaned reservation is never adjusted automatically.
func AdjustChannelMonitorReservation(ctx context.Context, key string, userID, expected, target int, requireAvailable bool) error {
	if expected < 0 || expected > common.MaxQuota || target < expected || target > common.MaxQuota {
		return errors.New("预扣调整额度无效")
	}
	var saved ChannelMonitorIncome
	var tokenKey string
	commitAttempted := false
	delta := target - expected
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("settlement_key = ?", key).First(&saved).Error; err != nil {
			return err
		}
		if saved.UserID != userID || saved.Status != "reserved" || saved.Quota != int64(expected) {
			return errors.New("预扣记录已改变，请核对后处理")
		}
		if delta > 0 && requireAvailable && saved.BillingSource == "wallet" {
			var user User
			if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
				return err
			}
			if user.Quota < delta {
				return ErrChannelMonitorWalletInsufficient
			}
		}
		task := Task{UserId: userID, PrivateData: TaskPrivateData{BillingSource: saved.BillingSource, SubscriptionId: saved.FundingSubscriptionID}}
		if delta > 0 && saved.BillingSource == taskBillingSubscriptionSource {
			var sub UserSubscription
			if err := lockForUpdate(tx).First(&sub, saved.FundingSubscriptionID).Error; err != nil {
				return err
			}
			if saved.SubscriptionPeriod == nil {
				// Older reservations cannot safely infer cross-period supplements.
				if sub.LastResetTime > saved.CreatedAt {
					return errors.New("旧版订阅预扣缺少周期证据，请先核对")
				}
				saved.SubscriptionPeriod = &sub.LastResetTime
				saved.SubscriptionReserved = saved.Quota
			}
			if *saved.SubscriptionPeriod != sub.LastResetTime {
				saved.SubscriptionReserved = 0
			}
			saved.SubscriptionPeriod = &sub.LastResetTime
			saved.SubscriptionReserved += int64(delta)
		}
		if err := applyTaskFundingDelta(tx, &task, delta); err != nil {
			return err
		}
		if delta > 0 && saved.FundingTokenID > 0 {
			var token Token
			if err := lockForUpdate(tx).First(&token, saved.FundingTokenID).Error; err != nil {
				return err
			}
			if !token.UnlimitedQuota && token.RemainQuota < delta {
				return ErrChannelMonitorTokenInsufficient
			}
		}
		var err error
		tokenKey, err = applyTaskTokenDelta(tx, saved.FundingTokenID, delta)
		if err != nil {
			return err
		}
		amount, err := ChannelMonitorIncomeAmount(int64(target), saved.QuotaPerUnit, "1")
		if err != nil {
			return err
		}
		updates := map[string]any{"quota": target, "income_nano_cny": amount, "updated_at": time.Now().Unix()}
		if saved.SubscriptionPeriod != nil {
			updates["subscription_period"] = *saved.SubscriptionPeriod
			updates["subscription_reserved"] = saved.SubscriptionReserved
		}
		if err := tx.Model(&saved).Updates(updates).Error; err != nil {
			return err
		}
		commitAttempted = true
		return nil
	})
	invalidateChannelMonitorFundingCache(saved.UserID, saved.BillingSource, tokenKey, delta)
	if err != nil {
		if commitAttempted {
			return errors.Join(ErrTaskBillingCommitUncertain, err)
		}
		return err // No blind retry of an uncertain commit.
	}
	return nil
}

func invalidateChannelMonitorFundingCache(userID int, source, tokenKey string, delta int) {
	if !common.RedisEnabled || common.RDB == nil || delta == 0 {
		return
	}
	// The DB is authoritative. Invalidate even after an uncertain COMMIT;
	// applying a delta could double-count a cache hydrated after that commit.
	var keys []string
	if source != taskBillingSubscriptionSource && userID > 0 {
		keys = append(keys, getUserCacheKey(userID))
	}
	if tokenKey != "" {
		keys = append(keys, getTokenCacheKey(tokenKey))
	}
	if len(keys) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := invalidateFundingCacheKeys(ctx, common.RDB, keys); err != nil {
		common.SysError("清理结算余额缓存失败: " + err.Error())
	}
}
