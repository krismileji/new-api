package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelLocalResponseRefund is an outbox for a BillingSession's zero-charge
// termination. The balances and Applied flag commit in the same transaction.
// It contains no request body, response text or API key.
type ChannelLocalResponseRefund struct {
	ID                int64  `gorm:"primaryKey"`
	RequestID         string `gorm:"type:varchar(64);uniqueIndex"`
	UserID            int
	TokenID           int
	SubscriptionID    int
	WalletQuota       int64  `gorm:"type:bigint"`
	TokenQuota        int64  `gorm:"type:bigint"`
	SubscriptionQuota int64  `gorm:"type:bigint"`
	UserCacheEpoch    string `gorm:"type:varchar(64)"`
	TokenCacheEpoch   string `gorm:"type:varchar(64)"`
	Applied           bool
	CacheApplied      bool  `gorm:"index"`
	CreatedAt         int64 `gorm:"type:bigint"`
	UpdatedAt         int64 `gorm:"type:bigint"`
}

const localRefundCacheEpochScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then return '' end
redis.call('HSETNX', KEYS[1], 'LocalRefundEpoch', ARGV[1])
return redis.call('HGET', KEYS[1], 'LocalRefundEpoch')`

// A generation fence avoids applying a delta to a cache hydrated after commit.
// Replays use an independent marker; no ambiguous Redis reply can double refund.
const localRefundCacheApplyScript = `
if redis.call('EXISTS', KEYS[2]) == 1 then return 0 end
local epoch = redis.call('HGET', KEYS[1], 'LocalRefundEpoch')
if ARGV[1] ~= '' and epoch == ARGV[1] and ARGV[5] == '1' then
  redis.call('HINCRBY', KEYS[1], ARGV[2], ARGV[3])
  if ARGV[4] ~= '' then redis.call('HINCRBY', KEYS[1], ARGV[4], -tonumber(ARGV[3])) end
else
  redis.call('DEL', KEYS[1])
end
redis.call('SET', KEYS[2], '1', 'EX', 604800)
return 1`

func QueueChannelLocalResponseRefund(ctx context.Context, refund *ChannelLocalResponseRefund) error {
	if refund == nil || refund.RequestID == "" || len(refund.RequestID) > 64 || refund.UserID <= 0 {
		return errors.New("本地响应退款缺少有效的请求归属")
	}
	for _, amount := range []int64{refund.WalletQuota, refund.TokenQuota, refund.SubscriptionQuota} {
		if amount < 0 || amount > common.MaxWalletQuota {
			return errors.New("本地响应退款额度超出范围")
		}
	}
	if refund.WalletQuota > 0 && refund.SubscriptionQuota > 0 {
		return errors.New("本地响应退款资金来源冲突")
	}
	refund.CreatedAt = time.Now().Unix()
	refund.UpdatedAt = refund.CreatedAt
	if common.RedisEnabled && common.RDB != nil {
		if refund.WalletQuota > 0 {
			refund.UserCacheEpoch, _ = common.RDB.Eval(ctx, localRefundCacheEpochScript, []string{getUserCacheKey(refund.UserID)}, common.GetUUID()).Text()
		}
		if refund.TokenQuota > 0 {
			var token Token
			if err := DB.WithContext(ctx).Select("key").First(&token, refund.TokenID).Error; err != nil {
				return err
			}
			refund.TokenCacheEpoch, _ = common.RDB.Eval(ctx, localRefundCacheEpochScript, []string{getTokenCacheKey(token.Key)}, common.GetUUID()).Text()
		}
	}
	userQuotaBatchMutationLock.Lock()
	defer userQuotaBatchMutationLock.Unlock()
	tokenQuotaBatchMutationLock.Lock()
	defer tokenQuotaBatchMutationLock.Unlock()
	// Make any in-memory pre-consume durable together with the intent. A crash
	// after enqueue must not replay a refund for a debit that never reached DB.
	userPending := batchUpdateStores[BatchUpdateTypeUserQuota][refund.UserID]
	tokenPending := batchUpdateStores[BatchUpdateTypeTokenQuota][refund.TokenID]
	var saved ChannelLocalResponseRefund
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(refund).Error; err != nil {
			return err
		}
		if err := tx.Where("request_id = ?", refund.RequestID).First(&saved).Error; err != nil {
			return err
		}
		if saved.UserID != refund.UserID || saved.TokenID != refund.TokenID || saved.SubscriptionID != refund.SubscriptionID || saved.WalletQuota != refund.WalletQuota || saved.TokenQuota != refund.TokenQuota || saved.SubscriptionQuota != refund.SubscriptionQuota {
			return errors.New("本地响应退款请求已存在且金额不一致")
		}
		if userPending != 0 {
			var user User
			if err := lockForUpdate(tx).Select("id", "quota").First(&user, refund.UserID).Error; err != nil {
				return err
			}
			balance, err := channelLocalRefundBalance(int64(user.Quota), int64(userPending), 0)
			if err != nil {
				return err
			}
			if err := tx.Model(&user).Update("quota", balance).Error; err != nil {
				return err
			}
		}
		if tokenPending != 0 && refund.TokenID > 0 {
			var token Token
			if err := lockForUpdate(tx).Select("id", "remain_quota", "used_quota").First(&token, refund.TokenID).Error; err != nil {
				return err
			}
			balance, err := channelLocalRefundBalance(int64(token.RemainQuota), int64(tokenPending), 0)
			if err != nil {
				return err
			}
			used, err := channelLocalRefundBalance(int64(token.UsedQuota), -int64(tokenPending), 0)
			if err != nil {
				return err
			}
			if err := tx.Model(&token).Updates(map[string]any{"remain_quota": balance, "used_quota": used}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	delete(batchUpdateStores[BatchUpdateTypeUserQuota], refund.UserID)
	delete(batchUpdateStores[BatchUpdateTypeTokenQuota], refund.TokenID)
	*refund = saved
	return nil
}

func ApplyChannelLocalResponseRefund(ctx context.Context, requestID string) error {
	// Drain this process's pending pre-consumes into the same transaction. This
	// also keeps the existing batch writer from racing with balance validation.
	userQuotaBatchMutationLock.Lock()
	defer userQuotaBatchMutationLock.Unlock()
	tokenQuotaBatchMutationLock.Lock()
	defer tokenQuotaBatchMutationLock.Unlock()
	var refund ChannelLocalResponseRefund
	var token Token
	var drainedUser, drainedToken bool
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&refund).Error; err != nil {
			return err
		}
		if refund.Applied {
			return nil
		}
		if refund.WalletQuota > 0 {
			var user User
			if err := lockForUpdate(tx).Select("id", "quota").First(&user, refund.UserID).Error; err != nil {
				return err
			}
			pending := int64(batchUpdateStores[BatchUpdateTypeUserQuota][refund.UserID])
			balance, err := channelLocalRefundBalance(int64(user.Quota), pending, refund.WalletQuota)
			if err != nil {
				return err
			}
			if err := tx.Model(&User{}).Where("id = ?", refund.UserID).Update("quota", balance).Error; err != nil {
				return err
			}
			drainedUser = true
		}
		if refund.SubscriptionQuota > 0 {
			var record SubscriptionPreConsumeRecord
			if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&record).Error; err != nil {
				return err
			}
			if record.UserId != refund.UserID || record.UserSubscriptionId != refund.SubscriptionID || record.Status != "consumed" {
				return errors.New("订阅预扣记录与本地响应退款不一致")
			}
			var sub UserSubscription
			if err := lockForUpdate(tx).First(&sub, refund.SubscriptionID).Error; err != nil {
				return err
			}
			// A reset ends the old quota period; never subtract old reservations
			// from usage accumulated in the new period.
			if sub.LastResetTime <= record.CreatedAt {
				used := max(int64(0), sub.AmountUsed-refund.SubscriptionQuota)
				if err := tx.Model(&sub).Update("amount_used", used).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&record).Update("status", "refunded").Error; err != nil {
				return err
			}
		}
		if refund.TokenQuota > 0 {
			if err := lockForUpdate(tx).Select("id", "key", "remain_quota", "used_quota").First(&token, refund.TokenID).Error; err != nil {
				return err
			}
			pending := int64(batchUpdateStores[BatchUpdateTypeTokenQuota][refund.TokenID])
			balance, err := channelLocalRefundBalance(int64(token.RemainQuota), pending, refund.TokenQuota)
			if err != nil {
				return err
			}
			used, err := channelLocalRefundBalance(int64(token.UsedQuota), -pending, -refund.TokenQuota)
			if err != nil {
				return err
			}
			if err := tx.Model(&Token{}).Where("id = ?", refund.TokenID).Updates(map[string]any{"remain_quota": balance, "used_quota": used}).Error; err != nil {
				return err
			}
			drainedToken = true
		}
		return tx.Model(&refund).Updates(map[string]any{"applied": true, "updated_at": time.Now().Unix()}).Error
	})
	if err != nil {
		return err
	}
	if drainedUser {
		delete(batchUpdateStores[BatchUpdateTypeUserQuota], refund.UserID)
	}
	if drainedToken {
		delete(batchUpdateStores[BatchUpdateTypeTokenQuota], refund.TokenID)
	}
	if refund.CacheApplied {
		return nil
	}
	if common.RedisEnabled && common.RDB != nil {
		fresh := 0
		if time.Now().Unix()-refund.CreatedAt < 6*86400 {
			fresh = 1
		}
		if refund.WalletQuota > 0 {
			key := getUserCacheKey(refund.UserID)
			if err := common.RDB.Eval(ctx, localRefundCacheApplyScript, []string{key, fmt.Sprintf("local_response_refund:user:%s", refund.RequestID)}, refund.UserCacheEpoch, "Quota", refund.WalletQuota, "", fresh).Err(); err != nil {
				return err
			}
		}
		if refund.TokenQuota > 0 {
			if token.Key == "" {
				if err := DB.WithContext(ctx).Select("key").First(&token, refund.TokenID).Error; err != nil {
					return err
				}
			}
			key := getTokenCacheKey(token.Key)
			if err := common.RDB.Eval(ctx, localRefundCacheApplyScript, []string{key, fmt.Sprintf("local_response_refund:token:%s", refund.RequestID)}, refund.TokenCacheEpoch, "RemainQuota", refund.TokenQuota, "UsedQuota", fresh).Err(); err != nil {
				return err
			}
		}
	}
	return DB.WithContext(ctx).Model(&refund).Updates(map[string]any{"cache_applied": true, "updated_at": time.Now().Unix()}).Error
}

func channelLocalRefundBalance(balance, pending, refund int64) (int64, error) {
	if pending > 0 && balance > math.MaxInt64-pending || pending < 0 && balance < math.MinInt64-pending {
		return 0, errors.New("本地响应退款余额溢出")
	}
	balance += pending
	if refund > 0 && balance > common.MaxWalletQuota-refund || refund < 0 && balance < -common.MaxWalletQuota-refund {
		return 0, errors.New("本地响应退款余额超出范围")
	}
	return balance + refund, nil
}
