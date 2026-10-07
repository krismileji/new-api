package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelLocalResponseRefund is an outbox for a BillingSession's zero-charge
// termination. The balances and Applied flag commit in the same transaction.
// It contains no request body, response text or API key.
type ChannelLocalResponseRefund struct {
	ID                   int64  `gorm:"primaryKey"`
	RequestID            string `gorm:"type:varchar(64);uniqueIndex"`
	UserID               int
	TokenID              int
	SubscriptionID       int
	WalletQuota          int64 `gorm:"type:bigint"`
	TokenQuota           int64 `gorm:"type:bigint"`
	SubscriptionQuota    int64 `gorm:"type:bigint"`
	SubscriptionPeriod   *int64
	SubscriptionReserved int64
	BatchTransferID      string `gorm:"type:varchar(36)"`
	UserCacheEpoch       string `gorm:"type:varchar(64)"`
	TokenCacheEpoch      string `gorm:"type:varchar(64)"`
	Applied              bool
	CacheApplied         bool  `gorm:"index"`
	CreatedAt            int64 `gorm:"type:bigint"`
	UpdatedAt            int64 `gorm:"type:bigint"`
}

// Only an unacknowledged enqueue lives here. Its captured batch is no longer
// visible to the ordinary batch writer. The request ID and transfer ID make
// retrying the transaction safe even when both COMMIT and readback fail.
type channelLocalRefundTransfer struct {
	mu           sync.Mutex
	sequence     uint64
	refund       ChannelLocalResponseRefund
	userPending  int
	tokenPending int
	detached     bool
	done         bool
}

var channelLocalRefundTransfers sync.Map
var channelLocalRefundTransferSequence atomic.Uint64
var channelLocalRefundTransferScan struct {
	sync.Mutex
	after, through uint64
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
	transfer := &channelLocalRefundTransfer{sequence: channelLocalRefundTransferSequence.Add(1), refund: *refund}
	transfer.refund.ID = 0
	transfer.refund.Applied, transfer.refund.CacheApplied = false, false
	transfer.refund.BatchTransferID = common.GetUUID()
	value, _ := channelLocalRefundTransfers.LoadOrStore(refund.RequestID, transfer)
	transfer = value.(*channelLocalRefundTransfer)
	if !transfer.mu.TryLock() {
		return errors.New("退款入队正在确认，请稍后重试")
	}
	defer transfer.mu.Unlock()
	if !sameChannelLocalRefund(&transfer.refund, refund) {
		return errors.New("本地响应退款请求已存在且金额不一致")
	}
	if err := transfer.enqueue(ctx); err != nil {
		if !transfer.detached {
			channelLocalRefundTransfers.CompareAndDelete(refund.RequestID, transfer)
		} else {
			err = errors.Join(ErrTaskBillingCommitUncertain, err)
		}
		return err
	}
	*refund = transfer.refund
	return nil
}

func sameChannelLocalRefund(saved, refund *ChannelLocalResponseRefund) bool {
	return saved.UserID == refund.UserID && saved.TokenID == refund.TokenID && saved.SubscriptionID == refund.SubscriptionID &&
		saved.WalletQuota == refund.WalletQuota && saved.TokenQuota == refund.TokenQuota && saved.SubscriptionQuota == refund.SubscriptionQuota
}

// The caller holds transfer.mu. A detached batch is retried with the original
// snapshot, never recaptured from newer requests' ordinary batch entries.
func (transfer *channelLocalRefundTransfer) enqueue(ctx context.Context) error {
	if transfer.done {
		return nil
	}
	refund := &transfer.refund
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
	firstBatch := common.BatchUpdateEnabled && !transfer.detached
	if firstBatch {
		// Enqueue has not transferred ownership yet; preserve the existing
		// batch-drain behavior instead of rejecting a refund on contention.
		userQuotaBatchMutationLock.Lock()
		defer userQuotaBatchMutationLock.Unlock()
		tokenQuotaBatchMutationLock.Lock()
		defer tokenQuotaBatchMutationLock.Unlock()
	}
	// Make any in-memory pre-consume durable together with the intent. A crash
	// after enqueue must not replay a refund for a debit that never reached DB.
	if firstBatch {
		transfer.userPending = batchUpdateStores[BatchUpdateTypeUserQuota][refund.UserID]
		transfer.tokenPending = batchUpdateStores[BatchUpdateTypeTokenQuota][refund.TokenID]
	}
	userPending, tokenPending := transfer.userPending, transfer.tokenPending
	var saved ChannelLocalResponseRefund
	commitAttempted, ownBatch, existing := false, false, false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A prior successful enqueue already owns its debit. New unrelated
		// batches must not be drained when replaying the same request.
		lookupErr := lockForUpdate(tx).Where("request_id = ?", refund.RequestID).First(&saved).Error
		if lookupErr == nil {
			if !sameChannelLocalRefund(&saved, refund) {
				return errors.New("本地响应退款请求已存在且金额不一致")
			}
			ownBatch = saved.BatchTransferID == refund.BatchTransferID
			existing = true
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return lookupErr
		}
		// Never trust caller-supplied refund evidence. Capture it while holding
		// the reservation lock, before transferring ownership to this outbox.
		refund.ID = 0
		refund.SubscriptionPeriod, refund.SubscriptionReserved = nil, 0
		if err := tx.Create(refund).Error; err != nil {
			return err
		}
		saved = *refund
		if ChannelMonitorIncomeReady.Load() && !saved.Applied {
			var income ChannelMonitorIncome
			err := lockForUpdate(tx).Where("settlement_key = ?", ChannelMonitorIncomeKey(refund.RequestID, "request")).First(&income).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				if (income.Status != "reserved" && income.Status != "refund_pending") || income.UserID != refund.UserID ||
					income.Quota != refund.WalletQuota+refund.SubscriptionQuota ||
					income.FundingSubscriptionID != refund.SubscriptionID ||
					(refund.TokenQuota > 0 && (income.FundingTokenID != refund.TokenID || refund.TokenQuota != income.Quota)) ||
					(income.FundingTokenID > 0 && income.Quota > 0 && refund.TokenQuota == 0) {
					return errors.New("退款与收入预扣记录不一致")
				}
				if income.SubscriptionPeriod != nil {
					saved.SubscriptionPeriod = income.SubscriptionPeriod
					saved.SubscriptionReserved = income.SubscriptionReserved
				}
				if err := tx.Model(&income).Updates(map[string]any{"status": "refund_pending", "updated_at": time.Now().Unix()}).Error; err != nil {
					return err
				}
			}
		}
		if saved.SubscriptionQuota > 0 && !saved.Applied {
			var receipt SubscriptionPreConsumeRecord
			err := lockForUpdate(tx).Where("request_id = ?", saved.RequestID).First(&receipt).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && (receipt.UserId != saved.UserID || receipt.UserSubscriptionId != saved.SubscriptionID || receipt.Status != "consumed") {
				return errors.New("订阅预扣记录与本地响应退款不一致")
			}
			if saved.SubscriptionPeriod == nil {
				if err != nil {
					return err
				}
				var sub UserSubscription
				if err := lockForUpdate(tx).First(&sub, saved.SubscriptionID).Error; err != nil {
					return err
				}
				saved.SubscriptionPeriod = &sub.LastResetTime
				if sub.LastResetTime <= receipt.CreatedAt {
					saved.SubscriptionReserved = saved.SubscriptionQuota
				} else if saved.SubscriptionQuota > receipt.PreConsumed {
					return errors.New("旧版订阅跨周期补扣缺少退款证据，请先核对")
				}
			}
			if saved.SubscriptionReserved < 0 || saved.SubscriptionReserved > saved.SubscriptionQuota {
				return errors.New("订阅周期预扣额度与退款不一致")
			}
			if err := tx.Model(&saved).Updates(map[string]any{"subscription_period": *saved.SubscriptionPeriod, "subscription_reserved": saved.SubscriptionReserved}).Error; err != nil {
				return err
			}
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
		ownBatch, commitAttempted = true, true
		return nil
	})
	if firstBatch && (err == nil && ownBatch || commitAttempted) {
		delete(batchUpdateStores[BatchUpdateTypeUserQuota], refund.UserID)
		delete(batchUpdateStores[BatchUpdateTypeTokenQuota], refund.TokenID)
		transfer.detached = true
	}
	if commitAttempted {
		transfer.detached = true
	}
	if existing {
		// A matching row read inside this transaction proves the enqueue is
		// durable already; this read-only transaction has nothing to commit.
		err = nil
	}
	if err != nil {
		if !transfer.detached {
			channelLocalRefundTransfers.CompareAndDelete(refund.RequestID, transfer)
		} else {
			err = errors.Join(ErrTaskBillingCommitUncertain, err)
		}
		return err
	}
	if transfer.detached && !ownBatch {
		// Another node won the unique request ID after our transaction rolled
		// back. Its row cannot include our local batch; put that batch back.
		if !userQuotaBatchMutationLock.TryLock() {
			return errors.New("用户额度批次正在更新，退款入队稍后确认")
		}
		if !tokenQuotaBatchMutationLock.TryLock() {
			userQuotaBatchMutationLock.Unlock()
			return errors.New("令牌额度批次正在更新，退款入队稍后确认")
		}
		defer userQuotaBatchMutationLock.Unlock()
		defer tokenQuotaBatchMutationLock.Unlock()
		// Match the normal batch writer's signed-integer limit, without
		// clipping a transferred batch or modifying either half on overflow.
		userCurrent := batchUpdateStores[BatchUpdateTypeUserQuota][refund.UserID]
		tokenCurrent := batchUpdateStores[BatchUpdateTypeTokenQuota][refund.TokenID]
		userSum, tokenSum := userCurrent+userPending, tokenCurrent+tokenPending
		if userPending > 0 && userSum < userCurrent || userPending < 0 && userSum > userCurrent ||
			tokenPending > 0 && tokenSum < tokenCurrent || tokenPending < 0 && tokenSum > tokenCurrent {
			return errors.New("退款入队批次还原溢出，保留记录等待核对")
		}
		batchUpdateStores[BatchUpdateTypeUserQuota][refund.UserID] = userSum
		if refund.TokenID > 0 {
			batchUpdateStores[BatchUpdateTypeTokenQuota][refund.TokenID] = tokenSum
		}
	}
	*refund = saved
	transfer.done = true
	channelLocalRefundTransfers.CompareAndDelete(refund.RequestID, transfer)
	return nil
}

func ApplyChannelLocalResponseRefund(ctx context.Context, requestID string) error {
	// Enqueue already made this request's debit durable. Other pending batches
	// belong to independent requests and must stay with their original writer.
	var refund ChannelLocalResponseRefund
	var token Token
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&refund).Error; err != nil {
			return err
		}
		if refund.Applied {
			return nil
		}
		if ChannelMonitorIncomeReady.Load() {
			// Queue transferred ownership under the same income-row lock used
			// by settlement. Close that reservation together with the refund.
			if err := tx.Model(&ChannelMonitorIncome{}).
				Where("settlement_key = ? AND status = ?", ChannelMonitorIncomeKey(requestID, "request"), "refund_pending").
				Updates(map[string]any{"status": "settled", "quota": 0, "income_nano_cny": 0, "cost_recorded": 1, "updated_at": time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		if refund.WalletQuota > 0 {
			var user User
			if err := lockForUpdate(tx).Select("id", "quota").First(&user, refund.UserID).Error; err != nil {
				return err
			}
			balance, err := channelLocalRefundBalance(int64(user.Quota), 0, refund.WalletQuota)
			if err != nil {
				return err
			}
			if err := tx.Model(&User{}).Where("id = ?", refund.UserID).Update("quota", balance).Error; err != nil {
				return err
			}
			if err := queueChannelMonitorFundingCacheRepair(tx, getUserCacheKey(refund.UserID)); err != nil {
				return err
			}
		}
		if refund.SubscriptionQuota > 0 {
			var record SubscriptionPreConsumeRecord
			receiptErr := lockForUpdate(tx).Where("request_id = ?", requestID).First(&record).Error
			if receiptErr != nil && !errors.Is(receiptErr, gorm.ErrRecordNotFound) {
				return receiptErr
			}
			if receiptErr != nil && refund.SubscriptionPeriod == nil {
				// Old queued intents without a surviving receipt need manual
				// evidence; never manufacture a historical debit during migration.
				return receiptErr
			}
			if receiptErr == nil && (record.UserId != refund.UserID || record.UserSubscriptionId != refund.SubscriptionID || record.Status != "consumed") {
				return errors.New("订阅预扣记录与本地响应退款不一致")
			}
			var sub UserSubscription
			if err := lockForUpdate(tx).First(&sub, refund.SubscriptionID).Error; err != nil {
				return err
			}
			// A reset ends the old quota period; never subtract old reservations
			// from usage accumulated in the new period.
			quota := int64(0)
			if refund.SubscriptionPeriod != nil {
				if *refund.SubscriptionPeriod == sub.LastResetTime {
					quota = refund.SubscriptionReserved
				}
			} else if sub.LastResetTime <= record.CreatedAt {
				quota = refund.SubscriptionQuota
			} else if refund.SubscriptionQuota > record.PreConsumed {
				return errors.New("旧版订阅跨周期补扣缺少退款证据，请先核对")
			}
			if quota > 0 {
				used := max(int64(0), sub.AmountUsed-quota)
				if err := tx.Model(&sub).Update("amount_used", used).Error; err != nil {
					return err
				}
			}
			if receiptErr == nil {
				if err := tx.Model(&record).Update("status", "refunded").Error; err != nil {
					return err
				}
			}
		}
		if refund.TokenQuota > 0 {
			if err := lockForUpdate(tx).Select("id", "key", "remain_quota", "used_quota").First(&token, refund.TokenID).Error; err != nil {
				return err
			}
			balance, err := channelLocalRefundBalance(int64(token.RemainQuota), 0, refund.TokenQuota)
			if err != nil {
				return err
			}
			used, err := channelLocalRefundBalance(int64(token.UsedQuota), 0, -refund.TokenQuota)
			if err != nil {
				return err
			}
			if err := tx.Model(&Token{}).Where("id = ?", refund.TokenID).Updates(map[string]any{"remain_quota": balance, "used_quota": used}).Error; err != nil {
				return err
			}
			if err := queueChannelMonitorFundingCacheRepair(tx, getTokenCacheKey(token.Key)); err != nil {
				return err
			}
		}
		return tx.Model(&refund).Updates(map[string]any{"applied": true, "updated_at": time.Now().Unix()}).Error
	})
	if err != nil {
		return err
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

// Enqueue ambiguity contains process-local batch state, so every node retries
// its own transfers; the master-only durable refund task cannot see that state.
func RecoverChannelLocalRefundTransfers(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("退款入队确认批次大小无效")
	}
	if !channelLocalRefundTransferScan.TryLock() {
		return 0, nil
	}
	defer channelLocalRefundTransferScan.Unlock()
	var pending []*channelLocalRefundTransfer
	channelLocalRefundTransfers.Range(func(_, value any) bool {
		pending = append(pending, value.(*channelLocalRefundTransfer))
		return true
	})
	slices.SortFunc(pending, func(a, b *channelLocalRefundTransfer) int {
		if a.sequence < b.sequence {
			return -1
		}
		if a.sequence > b.sequence {
			return 1
		}
		return 0
	})
	after, through := channelLocalRefundTransferScan.after, channelLocalRefundTransferScan.through
	start := 0
	for start < len(pending) && pending[start].sequence <= after {
		start++
	}
	if start == len(pending) || pending[start].sequence > through {
		start = 0
		if len(pending) > 0 {
			through = pending[len(pending)-1].sequence
		}
		channelLocalRefundTransferScan.through = through
	}
	pending = pending[start:]
	completed := 0
	var failures error
	for _, transfer := range pending[:min(max(limit, 0), len(pending))] {
		if ctx.Err() != nil {
			return completed, errors.Join(failures, ctx.Err())
		}
		if transfer.sequence > through {
			break
		}
		channelLocalRefundTransferScan.after = transfer.sequence
		if !transfer.mu.TryLock() {
			continue
		}
		// Ordinary in-flight enqueue owns its batch locks; only retry detached
		// transfers that have already relinquished the ordinary batch writer.
		if !transfer.detached {
			transfer.mu.Unlock()
			continue
		}
		recordCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := transfer.enqueue(recordCtx)
		cancel()
		transfer.mu.Unlock()
		if err != nil {
			failures = errors.Join(failures, err)
		} else {
			completed++
		}
	}
	return completed, failures
}

func RecoverChannelLocalResponseRefunds(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("退款恢复批次大小无效")
	}
	records, cursor, err := loadChannelMonitorRecoveryBatch[ChannelLocalResponseRefund](ctx, "refund", DB.Where("cache_applied = ?", false), limit)
	if err != nil {
		return 0, err
	}
	completed := 0
	var failures error
	for _, record := range records {
		if advanced, err := cursor.advance(ctx, record.ID); err != nil || !advanced {
			return completed, errors.Join(failures, err)
		}
		recordCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := ApplyChannelLocalResponseRefund(recordCtx, record.RequestID)
		cancel()
		if err != nil {
			failures = errors.Join(failures, fmt.Errorf("退款记录 %d 恢复失败: %w", record.ID, err))
			continue
		}
		completed++
	}
	return completed, failures
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
