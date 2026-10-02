package model

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// TaskBillingOperation identifies the terminal billing action for a task.
// The operation is applied while holding the task row lock; the task quota is
// therefore the durable idempotency marker for repeated callbacks.
type TaskBillingOperation string

const (
	TaskBillingOperationRefund TaskBillingOperation = "refund"
	TaskBillingOperationSettle TaskBillingOperation = "settle"
)

const taskBillingSubscriptionSource = "subscription"

var ErrTaskBillingCommitUncertain = errors.New("无法核实任务初始结算是否已提交")

// SQLite reports write contention as SQLITE_BUSY/database-is-locked errors.
// A task callback must be retried as a whole transaction: retrying individual
// statements could commit only the funding or usage leg and leave the task
// cost ledger out of sync. The same bounded retry also handles a CAS race on
// SQLite, where FOR UPDATE is intentionally unavailable.
const taskBillingMaxAttempts = 8

func isRetryableTaskBillingError(err error) bool {
	if err == nil {
		return false
	}
	if isRetryableSQLiteLock(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "changed concurrently")
}

func waitTaskBillingRetry(ctx context.Context, attempt int) error {
	// Keep retries short and bounded while allowing the current writer to
	// commit. The context is honored so a canceled poll does not wait on a
	// backoff after its transaction has already failed.
	delay := 10 * time.Millisecond * time.Duration(1<<attempt)
	if delay > 250*time.Millisecond {
		delay = 250 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withTaskBillingTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if DB == nil {
		return errors.New("task billing database is unavailable")
	}
	var err error
	for attempt := 0; attempt < taskBillingMaxAttempts; attempt++ {
		err = DB.WithContext(ctx).Transaction(fn)
		if err == nil || !isRetryableTaskBillingError(err) {
			return err
		}
		if attempt+1 < taskBillingMaxAttempts {
			if waitErr := waitTaskBillingRetry(ctx, attempt); waitErr != nil {
				return waitErr
			}
		}
	}
	return err
}

// TaskBillingApplyResult describes the committed accounting delta. Applied is
// false for an idempotent replay (or a zero delta), while CostChanged can be
// true when a previous accounting attempt committed but its channel cost event
// correction was still pending.
type TaskBillingApplyResult struct {
	Applied       bool
	CostChanged   bool
	PreviousQuota int
	ActualQuota   int
	QuotaDelta    int
	CostNanoCNY   int64
	TaskID        int64
	TokenID       int
	TokenKey      string
	UserID        int
}

// InsertTaskWithBilling makes the initial quota a committed funding fact before
// the task becomes visible to polling. A failed adjustment rolls back the insert
// as well, so the caller still owns (and may refund) the original reservation.
func InsertTaskWithBilling(ctx context.Context, task *Task, reservedQuota int, omitColumns ...string) error {
	if DB == nil {
		return errors.New("任务结算数据库不可用")
	}
	if task == nil || task.ID != 0 || task.Quota < 0 || task.Quota > common.MaxQuota || reservedQuota < 0 || reservedQuota > common.MaxQuota {
		return errors.New("任务初始结算参数无效")
	}
	delta := task.Quota - reservedQuota
	var tokenKey string
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit(omitColumns...).Create(task).Error; err != nil {
			return err
		}
		if err := applyTaskFundingDelta(tx, task, delta); err != nil {
			return err
		}
		var err error
		tokenKey, err = applyTaskTokenDelta(tx, task.PrivateData.TokenId, delta)
		if err != nil {
			return err
		}
		return correctTaskChannelMonitorIncome(tx, task, task.Quota)
	})
	if err != nil {
		if task.ID == 0 {
			return err
		}
		// A lost COMMIT acknowledgement does not mean rollback. Check the
		// durable task before allowing the caller to refund its reservation.
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var committed Task
		checkErr := DB.WithContext(checkCtx).Where("id = ? AND task_id = ?", task.ID, task.TaskID).First(&committed).Error
		if errors.Is(checkErr, gorm.ErrRecordNotFound) {
			task.ID = 0
			return err
		}
		if checkErr != nil {
			return errors.Join(ErrTaskBillingCommitUncertain, err, checkErr)
		}
	}
	if common.RedisEnabled && delta != 0 {
		if task.PrivateData.BillingSource != taskBillingSubscriptionSource {
			if _, err := cacheApplyUserQuotaDelta(task.UserId, -int64(delta)); err != nil {
				common.SysError("更新任务初始结算钱包缓存失败: " + err.Error())
			}
		}
		if tokenKey != "" {
			if _, err := cacheApplyTokenQuotaDelta(task.PrivateData.TokenId, tokenKey, -int64(delta)); err != nil {
				common.SysError("更新任务初始结算令牌缓存失败: " + err.Error())
			}
		}
	}
	return nil
}

func applyTaskTokenDelta(tx *gorm.DB, tokenID, delta int) (string, error) {
	if tokenID <= 0 || delta == 0 {
		return "", nil
	}
	var token Token
	if err := lockForUpdate(tx).First(&token, tokenID).Error; err != nil {
		return "", err
	}
	remain, ok := addTaskBillingQuota(int64(token.RemainQuota), -int64(delta))
	if !ok {
		return "", errors.New("令牌剩余额度超出范围")
	}
	used, ok := addTaskBillingQuota(int64(token.UsedQuota), int64(delta))
	if !ok {
		return "", errors.New("令牌已用额度超出范围")
	}
	err := tx.Model(&Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"remain_quota": remain, "used_quota": used, "accessed_time": common.GetTimestamp(),
	}).Error
	return token.Key, err
}

// ApplyTaskBilling atomically applies a task refund or settlement to the main
// database. It locks the task row first, then updates the funding source,
// token, usage counters, task quota, and (when present) the task cost event in
// one transaction. Replaying the same callback observes the already-updated
// task quota and performs no second accounting adjustment.
//
// SQLite permits only one writer. A short bounded retry handles SQLITE_BUSY
// without turning a transient lock conflict into a silently partial billing
// operation; MySQL/PostgreSQL errors are returned immediately.
func ApplyTaskBilling(ctx context.Context, task *Task, operation TaskBillingOperation, actualQuota int) (TaskBillingApplyResult, error) {
	if task == nil {
		return TaskBillingApplyResult{}, errors.New("task is nil")
	}
	if operation != TaskBillingOperationRefund && operation != TaskBillingOperationSettle {
		return TaskBillingApplyResult{}, errors.New("unknown task billing operation")
	}
	if operation == TaskBillingOperationSettle && actualQuota < 0 {
		return TaskBillingApplyResult{}, errors.New("task billing actual quota must not be negative")
	}
	if operation == TaskBillingOperationSettle && actualQuota > common.MaxQuota {
		return TaskBillingApplyResult{}, errors.New("task billing actual quota exceeds int32 range")
	}
	if DB == nil {
		return TaskBillingApplyResult{}, errors.New("task billing database is unavailable")
	}

	var result TaskBillingApplyResult
	var err error
	for attempt := 0; attempt < taskBillingMaxAttempts; attempt++ {
		result, err = applyTaskBillingOnce(ctx, task, operation, actualQuota)
		if err == nil || !isRetryableTaskBillingError(err) {
			return result, err
		}
		if attempt+1 < taskBillingMaxAttempts {
			if waitErr := waitTaskBillingRetry(ctx, attempt); waitErr != nil {
				return TaskBillingApplyResult{}, waitErr
			}
		}
	}
	return result, err
}

func applyTaskBillingOnce(ctx context.Context, requested *Task, operation TaskBillingOperation, actualQuota int) (TaskBillingApplyResult, error) {
	result := TaskBillingApplyResult{}
	var cacheUserDelta int64
	var cacheTokenDelta int64
	var cacheTokenID int
	var cacheTokenKey string
	var costContextChanged bool
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		query := lockForUpdate(tx)
		if requested.ID > 0 {
			query = query.Where("id = ?", requested.ID)
		} else if strings.TrimSpace(requested.TaskID) != "" {
			query = query.Where("task_id = ?", strings.TrimSpace(requested.TaskID)).Order("id ASC")
		} else {
			return errors.New("task id is missing")
		}
		if err := query.First(&task).Error; err != nil {
			return err
		}

		result.TaskID = task.ID
		result.UserID = task.UserId
		result.PreviousQuota = task.Quota
		result.TokenID = task.PrivateData.TokenId
		if task.Quota < 0 || task.Quota > common.MaxQuota {
			return errors.New("task quota is outside int32 range")
		}
		// Terminal status is part of the billing contract. A late success
		// callback must not re-charge a task already refunded as FAILURE (and a
		// late failure must not refund a settled SUCCESS). Direct helper callers
		// may still settle/refund an IN_PROGRESS task for compatibility.
		if (operation == TaskBillingOperationRefund && task.Status == TaskStatusSuccess) ||
			(operation == TaskBillingOperationSettle && task.Status == TaskStatusFailure) {
			result.ActualQuota = task.Quota
			return nil
		}
		// Keep the persisted expression/conversion frozen, but retain completion
		// facts even when polling wins the race with initial cost registration.
		if bc := task.PrivateData.BillingContext; operation == TaskBillingOperationSettle &&
			bc != nil && bc.ChannelCostSnapshot != nil && bc.TieredSnapshot != nil {
			if incoming := requested.PrivateData.BillingContext; incoming != nil && incoming.TieredSnapshot != nil {
				snapshot := *bc.TieredSnapshot
				snapshot.UsageFacts = make(map[string]any, len(bc.TieredSnapshot.UsageFacts)+len(incoming.TieredSnapshot.UsageFacts))
				maps.Copy(snapshot.UsageFacts, bc.TieredSnapshot.UsageFacts)
				maps.Copy(snapshot.UsageFacts, incoming.TieredSnapshot.UsageFacts)
				snapshot.EstimatedTier = incoming.TieredSnapshot.EstimatedTier
				bc.TieredSnapshot = &snapshot
				costContextChanged = true
			}
		}
		targetQuota := task.Quota
		if operation == TaskBillingOperationRefund {
			targetQuota = 0
		} else {
			targetQuota = actualQuota
		}
		result.ActualQuota = targetQuota
		delta64 := int64(targetQuota) - int64(task.Quota)
		if delta64 > int64(math.MaxInt) || delta64 < int64(math.MinInt) {
			return errors.New("task billing quota delta overflows int")
		}
		result.QuotaDelta = int(delta64)

		// A task may have already been settled/refunded. Keep processing an
		// outstanding cost-event correction, but never repeat accounting.
		if result.QuotaDelta != 0 {
			if err := applyTaskFundingDelta(tx, &task, result.QuotaDelta); err != nil {
				return err
			}
			if task.PrivateData.TokenId > 0 {
				key, err := applyTaskTokenDelta(tx, task.PrivateData.TokenId, result.QuotaDelta)
				if err != nil {
					return err
				}
				cacheTokenDelta = -delta64
				cacheTokenID = task.PrivateData.TokenId
				cacheTokenKey = key
			}
			if err := updateTaskUsageInTx(tx, task.UserId, task.ChannelId, result.QuotaDelta); err != nil {
				return err
			}
			result.Applied = true
			// Redis user quota tracks wallet balance, so it moves opposite to
			// the consume-positive billing delta.
			cacheUserDelta = -delta64
		}

		// The retention boundary is committed before any cost rows are deleted.
		// Expired reporting history must not prevent a real funding correction.
		historyExpired := false
		if ChannelMonitorIncomeReady.Load() && tx.Migrator().HasTable(&ChannelMonitorIncomeState{}) {
			var state ChannelMonitorIncomeState
			err := tx.Select("id", "retained_from").First(&state, 1).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			historyExpired = task.SubmitTime > 0 && ChannelDailyCostDayStart(task.SubmitTime) < state.RetainedFrom
		}
		if eventID := taskBillingCostEventID(&task); eventID != "" && !historyExpired {
			var existing ChannelTaskCostEvent
			err := lockForUpdate(tx).Where("cost_event_id = ?", eventID).First(&existing).Error
			if err == nil {
				oldCost := existing.CostNanoCNY
				var target func(ChannelTaskCostEvent) (int64, error)
				if operation == TaskBillingOperationRefund {
					target = func(ChannelTaskCostEvent) (int64, error) { return 0, nil }
				} else if bc := task.PrivateData.BillingContext; bc != nil && bc.ChannelCostSnapshot != nil && bc.TieredSnapshot != nil {
					target = func(ChannelTaskCostEvent) (int64, error) {
						return bc.ChannelCostSnapshot.ExpressionCost(bc.TieredSnapshot)
					}
				} else {
					target = func(event ChannelTaskCostEvent) (int64, error) {
						if event.InitialQuota <= 0 {
							return 0, errors.New("task cost event initial quota must be positive for quota correction")
						}
						cost := decimal.NewFromInt(event.InitialCostNanoCNY).
							Mul(decimal.NewFromInt(int64(targetQuota))).
							Div(decimal.NewFromInt(event.InitialQuota)).Round(0)
						if cost.IsNegative() || cost.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
							return 0, errors.New("corrected task cost exceeds int64")
						}
						return cost.IntPart(), nil
					}
				}
				cost, updateErr := updateChannelTaskCostEventTx(tx, eventID, common.GetTimestamp(), target)
				if updateErr != nil {
					if errors.Is(updateErr, gorm.ErrRecordNotFound) {
						return errors.New("task cost history changed concurrently")
					}
					return updateErr
				}
				result.CostNanoCNY = cost
				result.CostChanged = cost != oldCost
				if task.PrivateData.BillingContext != nil {
					task.PrivateData.BillingContext.ChannelCostNanoCNY = cost
					task.PrivateData.BillingContext.ChannelCostResolved = true
					costContextChanged = true
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			} else if task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.ChannelCostResolved {
				// A previously resolved task must retain its cost-event row. If
				// it disappeared, abort the whole billing transaction so a repair
				// can recreate the event before balances are finalized.
				return errors.New("resolved task cost event is missing or history changed concurrently")
			}
		}
		if !historyExpired {
			if err := correctTaskChannelMonitorIncome(tx, &task, targetQuota); err != nil {
				return err
			}
		}
		// Persist the task quota and the cost-resolution marker together, after
		// every accounting leg has succeeded. If either update fails the outer
		// transaction rolls back funding, usage, and the cost ledger as one unit.
		if result.QuotaDelta != 0 || costContextChanged {
			updates := map[string]any{}
			if result.QuotaDelta != 0 {
				updates["quota"] = targetQuota
			}
			if costContextChanged {
				updates["private_data"] = task.PrivateData
			}
			query := tx.Model(&Task{}).Where("id = ?", task.ID)
			if result.QuotaDelta != 0 {
				query = query.Where("quota = ?", task.Quota)
			}
			updatedTask := query.Updates(updates)
			if updatedTask.Error != nil {
				return updatedTask.Error
			}
			if updatedTask.RowsAffected != 1 {
				return errors.New("task quota changed concurrently")
			}
		}
		return nil
	})
	if err != nil {
		return TaskBillingApplyResult{}, err
	}

	// Keep Redis quota caches aligned after the durable DB commit. Cache
	// failures are observable and do not invalidate the committed transaction;
	// the normal cache hydration path will recover from the database.
	if cacheUserDelta != 0 && common.RedisEnabled {
		userID, delta := result.UserID, cacheUserDelta
		gopool.Go(func() {
			if _, cacheErr := cacheApplyUserQuotaDelta(userID, delta); cacheErr != nil {
				common.SysLog(fmt.Sprintf("failed to sync task billing user quota cache: %s", cacheErr.Error()))
			}
		})
	}
	if cacheTokenDelta != 0 && common.RedisEnabled && cacheTokenKey != "" {
		delta, tokenID, key := cacheTokenDelta, cacheTokenID, cacheTokenKey
		gopool.Go(func() {
			if _, cacheErr := cacheApplyTokenQuotaDelta(tokenID, key, delta); cacheErr != nil {
				common.SysLog(fmt.Sprintf("failed to sync task billing token quota cache: %s", cacheErr.Error()))
			}
		})
	}
	return result, nil
}

func taskBillingCostEventID(task *Task) string {
	if task == nil || task.PrivateData.BillingContext == nil {
		return ""
	}
	return strings.TrimSpace(task.PrivateData.BillingContext.ChannelCostEventId)
}

func applyTaskFundingDelta(tx *gorm.DB, task *Task, delta int) error {
	if task == nil || delta == 0 {
		return nil
	}
	if task.PrivateData.BillingSource == taskBillingSubscriptionSource && task.PrivateData.SubscriptionId > 0 {
		var sub UserSubscription
		if err := lockForUpdate(tx).Where("id = ?", task.PrivateData.SubscriptionId).First(&sub).Error; err != nil {
			return err
		}
		delta64 := int64(delta)
		if sub.AmountUsed < 0 {
			return errors.New("subscription used quota must not be negative")
		}
		newUsed := sub.AmountUsed
		if delta64 < 0 {
			if sub.AmountUsed < -delta64 {
				newUsed = 0
			} else {
				var ok bool
				newUsed, ok = addTaskBillingInt64(sub.AmountUsed, delta64)
				if !ok {
					return errors.New("subscription used quota underflow")
				}
			}
		} else {
			var ok bool
			newUsed, ok = addTaskBillingInt64(sub.AmountUsed, delta64)
			if !ok {
				return errors.New("subscription used quota overflow")
			}
		}
		if delta > 0 && (newUsed < sub.AmountUsed || (sub.AmountTotal > 0 && newUsed > sub.AmountTotal)) {
			return errors.New("subscription used exceeds total")
		}
		return tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("amount_used", newUsed).Error
	}
	// A positive billing delta consumes wallet quota; a negative delta refunds
	// it. Subscription usage below follows the opposite (consume-positive)
	// convention, so keep the wallet expression explicit here.
	var user User
	if err := lockForUpdate(tx).Where("id = ?", task.UserId).First(&user).Error; err != nil {
		return err
	}
	newQuota, ok := addTaskBillingQuota(int64(user.Quota), -int64(delta))
	if !ok {
		return errors.New("user quota overflow")
	}
	result := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", int(newQuota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func updateTaskUsageInTx(tx *gorm.DB, userID, channelID, delta int) error {
	if userID > 0 {
		var user User
		if err := lockForUpdate(tx).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		newUsedQuota, ok := addTaskBillingQuota(int64(user.UsedQuota), int64(delta))
		if !ok {
			return errors.New("user used quota overflow")
		}
		result := tx.Model(&User{}).Where("id = ?", userID).
			Update("used_quota", int(newUsedQuota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
	}
	if channelID > 0 {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ?", channelID).First(&channel).Error; err != nil {
			return err
		}
		newUsedQuota, ok := addTaskBillingInt64(channel.UsedQuota, int64(delta))
		if !ok {
			return errors.New("channel used quota overflow")
		}
		result := tx.Model(&Channel{}).Where("id = ?", channelID).
			Update("used_quota", newUsedQuota)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

func addTaskBillingInt64(current, delta int64) (int64, bool) {
	if delta > 0 && current > math.MaxInt64-delta {
		return 0, false
	}
	if delta < 0 && current < math.MinInt64-delta {
		return 0, false
	}
	return current + delta, true
}

func addTaskBillingQuota(current, delta int64) (int, bool) {
	next, ok := addTaskBillingInt64(current, delta)
	// Per-request quota is int32-bounded at the public entry points. Wallet
	// balances and accumulated token/user usage have a larger persisted domain.
	if !ok || next < -int64(common.MaxWalletQuota) || next > int64(common.MaxWalletQuota) || next < int64(math.MinInt) || next > int64(math.MaxInt) {
		return 0, false
	}
	return int(next), true
}

func isRetryableSQLiteLock(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database table is locked")
}
