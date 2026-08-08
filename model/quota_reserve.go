package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

// DecreaseUserQuotaIfEnough atomically deducts quota only when the database
// balance can cover it. It bypasses delayed batch updates for retry reserves.
func DecreaseUserQuotaIfEnough(id int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	userQuotaBatchMutationLock.Lock()
	defer userQuotaBatchMutationLock.Unlock()

	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	pendingDelta := batchUpdateStores[BatchUpdateTypeUserQuota][id]
	delete(batchUpdateStores[BatchUpdateTypeUserQuota], id)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()

	restorePendingDelta := func() {
		if pendingDelta == 0 {
			return
		}
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		batchUpdateStores[BatchUpdateTypeUserQuota][id] += pendingDelta
		batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	}

	result := DB.Model(&User{}).
		Where("id = ? AND quota + ? >= ?", id, pendingDelta, quota).
		Update("quota", gorm.Expr("quota + ?", pendingDelta-quota))
	if result.Error != nil {
		restorePendingDelta()
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		restorePendingDelta()
		return false, nil
	}
	gopool.Go(func() {
		if err := cacheDecrUserQuota(id, int64(quota)); err != nil {
			common.SysLog("failed to decrease user quota cache: " + err.Error())
		}
	})
	return true, nil
}

// DecreaseTokenQuotaIfEnough atomically applies pending token-quota mutations
// and deducts quota only when the effective database balance can cover it.
// This bypasses delayed token batch updates without losing their accounting.
func DecreaseTokenQuotaIfEnough(id int, key string, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return true, nil
	}
	tokenQuotaBatchMutationLock.Lock()
	defer tokenQuotaBatchMutationLock.Unlock()

	batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
	pendingDelta := batchUpdateStores[BatchUpdateTypeTokenQuota][id]
	delete(batchUpdateStores[BatchUpdateTypeTokenQuota], id)
	batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()

	restorePendingDelta := func() {
		if pendingDelta == 0 {
			return
		}
		batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
		batchUpdateStores[BatchUpdateTypeTokenQuota][id] += pendingDelta
		batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
	}

	result := DB.Model(&Token{}).
		Where("id = ? AND remain_quota + ? >= ?", id, pendingDelta, quota).
		Updates(map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", pendingDelta-quota),
			"used_quota":    gorm.Expr("used_quota - ?", pendingDelta-quota),
			"accessed_time": common.GetTimestamp(),
		})
	if result.Error != nil {
		restorePendingDelta()
		return false, result.Error
	}
	if result.RowsAffected != 1 {
		restorePendingDelta()
		return false, nil
	}
	if common.RedisEnabled {
		gopool.Go(func() {
			if err := cacheDecrTokenQuota(key, int64(quota)); err != nil {
				common.SysLog("failed to decrease token quota cache: " + err.Error())
			}
		})
	}
	return true, nil
}
