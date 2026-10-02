package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func SettleMidjourneyBilling(ctx context.Context, taskID int, pending MidjourneyPendingBilling, tokenID int) (Midjourney, bool, error) {
	if pending.Quota < 0 || pending.Quota > common.MaxQuota || pending.ChannelID <= 0 {
		return Midjourney{}, false, errors.New("Midjourney 初始结算参数无效")
	}
	var task Midjourney
	var applied bool
	var tokenKey string
	err := withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		applied = false
		task = Midjourney{}
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		// BillingChannelId is written only with the initial charge, and kept
		// after refunds. It also makes a zero-price settlement idempotent.
		if task.Status != "FAILURE" && task.BillingChannelId == 0 && task.Quota == 0 {
			fundingTask := Task{UserId: task.UserId}
			if err := applyTaskFundingDelta(tx, &fundingTask, pending.Quota); err != nil {
				return err
			}
			var err error
			tokenKey, err = applyTaskTokenDelta(tx, tokenID, pending.Quota)
			if err != nil {
				return err
			}
			if err := updateTaskUsageInTx(tx, task.UserId, pending.ChannelID, pending.Quota); err != nil {
				return err
			}
			if err := tx.Model(&User{}).Where("id = ?", task.UserId).UpdateColumn("request_count", gorm.Expr("request_count + 1")).Error; err != nil {
				return err
			}
			result := tx.Model(&Midjourney{}).Where("id = ? AND quota = 0 AND billing_channel_id = 0", task.Id).
				Updates(map[string]any{"quota": pending.Quota, "token_id": tokenID, "billing_channel_id": pending.ChannelID})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("Midjourney billing changed concurrently")
			}
			task.Quota, task.TokenId, task.BillingChannelId = pending.Quota, tokenID, pending.ChannelID
			applied = true
		}
		if ChannelMonitorIncomeReady.Load() {
			key := ChannelMonitorIncomeKey(fmt.Sprint(task.Id), "midjourney")
			var income ChannelMonitorIncome
			err := lockForUpdate(tx).Where("settlement_key = ?", key).First(&income).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			amount, err := ChannelMonitorIncomeAmount(int64(task.Quota), income.QuotaPerUnit, "1")
			if err != nil {
				return err
			}
			return tx.Model(&income).Updates(map[string]any{"quota": task.Quota, "income_nano_cny": amount, "usd_to_cny": "1", "status": "settled", "updated_at": time.Now().Unix()}).Error
		}
		return nil
	})
	if err != nil {
		return Midjourney{}, false, err
	}
	if applied && common.RedisEnabled {
		if _, err := cacheApplyUserQuotaDelta(task.UserId, -int64(task.Quota)); err != nil {
			common.SysError("更新 Midjourney 扣费钱包缓存失败: " + err.Error())
		}
		if tokenKey != "" {
			if _, err := cacheApplyTokenQuotaDelta(tokenID, tokenKey, -int64(task.Quota)); err != nil {
				common.SysError("更新 Midjourney 扣费令牌缓存失败: " + err.Error())
			}
		}
	}
	return task, applied, nil
}

// RefundMidjourneyBilling uses the stored quota as the idempotency marker.
// Wallet, token, usage, income and marker commit together, including when a
// different process retries with an old copy of the task.
func RefundMidjourneyBilling(ctx context.Context, taskID int) (Midjourney, error) {
	var refunded Midjourney
	var tokenKey string
	err := withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		refunded = Midjourney{}
		var task Midjourney
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.Quota == 0 {
			if ChannelMonitorIncomeReady.Load() {
				key := ChannelMonitorIncomeKey(fmt.Sprint(task.Id), "midjourney")
				if err := tx.Model(&ChannelMonitorIncome{}).Where("settlement_key = ?", key).
					Updates(map[string]any{"quota": 0, "income_nano_cny": 0, "status": "settled", "updated_at": time.Now().Unix()}).Error; err != nil {
					return err
				}
			}
			return tx.Model(&task).Updates(map[string]any{"status": "FAILURE", "progress": "100%", "billing_channel_id": task.GetBillingChannelId()}).Error
		}
		if task.Quota < 0 || task.Quota > common.MaxQuota {
			return errors.New("Midjourney 退款额度无效")
		}
		fundingTask := Task{UserId: task.UserId}
		if err := applyTaskFundingDelta(tx, &fundingTask, -task.Quota); err != nil {
			return err
		}
		var err error
		tokenKey, err = applyTaskTokenDelta(tx, task.TokenId, -task.Quota)
		if err != nil {
			return err
		}
		billingChannelID := task.GetBillingChannelId()
		var channelCount int64
		if err := tx.Model(&Channel{}).Where("id = ?", billingChannelID).Count(&channelCount).Error; err != nil {
			return err
		}
		if channelCount == 0 {
			// A removed upstream channel has no remaining usage row. Its
			// absence must not withhold the user's refundable balance.
			billingChannelID = 0
		}
		if err := updateTaskUsageInTx(tx, task.UserId, billingChannelID, -task.Quota); err != nil {
			return err
		}
		if ChannelMonitorIncomeReady.Load() {
			key := ChannelMonitorIncomeKey(fmt.Sprint(task.Id), "midjourney")
			if err := tx.Model(&ChannelMonitorIncome{}).Where("settlement_key = ?", key).
				Updates(map[string]any{"quota": 0, "income_nano_cny": 0, "status": "settled", "updated_at": time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&Midjourney{}).Where("id = ? AND quota = ?", task.Id, task.Quota).
			Updates(map[string]any{"quota": 0, "status": "FAILURE", "progress": "100%"})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("Midjourney quota changed concurrently")
		}
		refunded = task
		return nil
	})
	if err != nil {
		return Midjourney{}, err
	}
	if refunded.Quota > 0 && common.RedisEnabled {
		if _, err := cacheApplyUserQuotaDelta(refunded.UserId, int64(refunded.Quota)); err != nil {
			common.SysError("更新 Midjourney 退款钱包缓存失败: " + err.Error())
		}
		if tokenKey != "" {
			if _, err := cacheApplyTokenQuotaDelta(refunded.TokenId, tokenKey, int64(refunded.Quota)); err != nil {
				common.SysError("更新 Midjourney 退款令牌缓存失败: " + err.Error())
			}
		}
	}
	return refunded, nil
}
