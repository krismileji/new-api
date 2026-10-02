package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelMonitorIncomeReconciliation is an operator's verified funding fact,
// not a request to charge/refund. The expected fields prevent stale reviews.
type ChannelMonitorIncomeReconciliation struct {
	SettlementKey     string `json:"settlement_key"`
	UserID            int    `json:"user_id"`
	ExpectedQuota     int64  `json:"expected_quota"`
	ExpectedUpdatedAt int64  `json:"expected_updated_at"`
	NetChargedQuota   *int64 `json:"net_charged_quota"`
	Operator          string `json:"operator"`
	Evidence          string `json:"evidence"`
	RequestTerminated bool   `json:"request_terminated"`
}

// ReconcileChannelMonitorIncome resolves legacy pending rows or reservations
// whose owning request/process and asynchronous task have been stopped.
// The audit and income update share the primary DB transaction; neither wallet
// nor token/subscription balances are modified. Gaps and cost coverage remain.
func ReconcileChannelMonitorIncome(ctx context.Context, input ChannelMonitorIncomeReconciliation) error {
	input.Operator, input.Evidence = strings.TrimSpace(input.Operator), strings.TrimSpace(input.Evidence)
	if len(input.SettlementKey) != 64 || input.UserID <= 0 || input.ExpectedQuota < 0 || input.ExpectedUpdatedAt <= 0 ||
		input.NetChargedQuota == nil || *input.NetChargedQuota < 0 || *input.NetChargedQuota > common.MaxQuota ||
		input.Operator == "" || len(input.Operator) > 128 || input.Evidence == "" || len(input.Evidence) > 2000 {
		return errors.New("收入核对参数无效，必须提供原记录快照、实际净扣额度、操作人及资金证据")
	}
	return withTaskBillingTransaction(ctx, func(tx *gorm.DB) error {
		var record ChannelMonitorIncome
		if err := lockForUpdate(tx).Where("settlement_key = ?", input.SettlementKey).First(&record).Error; err != nil {
			return err
		}
		if (record.Status != "pending" && record.Status != "reserved") || record.UserID != input.UserID || record.Quota != input.ExpectedQuota || record.UpdatedAt != input.ExpectedUpdatedAt {
			return errors.New("收入记录状态或快照已改变，请重新核对；不能覆盖后台结算或已确认记录")
		}
		if record.Status == "reserved" && !input.RequestTerminated {
			return errors.New("预扣记录可能仍在使用，必须核实原请求及所属进程已终止且无任务继续结算")
		}
		amount, err := ChannelMonitorIncomeAmount(*input.NetChargedQuota, record.QuotaPerUnit, "1")
		if err != nil {
			return err
		}
		audit, err := common.Marshal(input)
		if err != nil {
			return err
		}
		if err := tx.Model(&record).Updates(map[string]any{
			"quota": *input.NetChargedQuota, "income_nano_cny": amount, "usd_to_cny": "1", "status": "settled", "updated_at": time.Now().Unix(),
		}).Error; err != nil {
			return err
		}
		// SystemTask is already the persisted operational history in the main
		// database. Using it avoids a new table and cross-database audit writes.
		return tx.Create(&SystemTask{
			TaskID: "income_review_" + common.GetUUID(), Type: "channel_monitor_income_reconcile",
			Status: SystemTaskStatusSucceeded, Payload: string(audit),
		}).Error
	})
}
