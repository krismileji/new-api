package service

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type channelMonitorIncomeConversion struct{ QuotaPerUnit, USDToCNY float64 }

func captureChannelMonitorIncomeConversion(c *gin.Context) {
	if c == nil {
		return
	}
	if _, exists := c.Get("channel_monitor_income_conversion"); !exists {
		c.Set("channel_monitor_income_conversion", channelMonitorIncomeConversion{common.QuotaPerUnit, operation_setting.USDExchangeRate})
	}
}

func prepareChannelMonitorIncome(c *gin.Context, info *relaycommon.RelayInfo, quota int, kind string) *model.ChannelMonitorIncome {
	if !model.ChannelMonitorIncomeReady.Load() || info == nil || c == nil ||
		c.GetBool(model.ChannelMonitorStatusProbeLogKey) || c.GetBool(model.ChannelMonitorGroupProbeLogKey) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	captureChannelMonitorIncomeConversion(c)
	value, _ := c.Get("channel_monitor_income_conversion")
	conversion := value.(channelMonitorIncomeConversion)
	if info.RequestId == "" || info.ChannelMeta == nil || info.ChannelId <= 0 || math.IsNaN(conversion.QuotaPerUnit) || math.IsInf(conversion.QuotaPerUnit, 0) || math.IsNaN(conversion.USDToCNY) || math.IsInf(conversion.USDToCNY, 0) {
		model.MarkChannelMonitorIncomeGap(ctx)
		logger.LogWarn(c, "收入记录缺少请求标识或有效换算参数")
		return nil
	}
	snapshot := channelDailyCostSnapshotWithCurrentKey(c, channelDailyCostSnapshot{})
	source := info.BillingSource
	if source == "" {
		source = BillingSourceWallet
	}
	record := &model.ChannelMonitorIncome{
		SettlementKey: model.ChannelMonitorIncomeKey(info.RequestId, kind), ChannelID: info.ChannelId,
		UserID: info.UserId, APIKeyID: info.TokenId, APIKeyKey: snapshot.KeyFingerprint, APIKeyName: snapshot.APIKeyName,
		ModelName: info.OriginModelName, GroupName: info.UsingGroup, BillingSource: source, Quota: int64(quota),
		QuotaPerUnit: strconv.FormatFloat(conversion.QuotaPerUnit, 'f', -1, 64), USDToCNY: strconv.FormatFloat(conversion.USDToCNY, 'f', -1, 64),
	}
	record.CostEventID = channelDailyCostEventId(c, info.ChannelId)
	if err := model.PrepareChannelMonitorIncome(ctx, record); err != nil {
		model.MarkChannelMonitorIncomeGap(ctx)
		logger.LogWarn(c, "记录收入结算失败，利润统计存在缺口: "+err.Error())
		return nil
	}
	return record
}

func confirmChannelMonitorIncome(c *gin.Context, record *model.ChannelMonitorIncome) {
	if record == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	if err := model.ConfirmChannelMonitorIncome(ctx, record.SettlementKey); err != nil {
		logger.LogWarn(c, "收入已扣费但记录待确认: "+err.Error())
	}
}

func (s *BillingSession) fundingCommitted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fundingSettled || s.settled
}

// Legacy Midjourney bills outside SettleBilling. Its persisted task ID also
// lets a later refund find the income without retaining request context.
func SettleMonitoredMidjourneyBilling(c *gin.Context, info *relaycommon.RelayInfo, task *model.Midjourney, prepared bool) (bool, error) {
	var income *model.ChannelMonitorIncome
	if prepared && info != nil && info.ChannelMeta != nil && task != nil && task.Id > 0 {
		snapshot := *info
		meta := *info.ChannelMeta
		meta.ChannelId = task.GetBillingChannelId()
		snapshot.ChannelMeta = &meta
		snapshot.RequestId = strconv.Itoa(task.Id)
		snapshot.OriginModelName = CovertMjpActionToModelName(task.Action)
		income = prepareChannelMonitorIncome(c, &snapshot, task.Quota, "midjourney")
	}
	applied, err := SettleMidjourneyTaskBilling(info, task, prepared)
	if applied {
		confirmChannelMonitorIncome(c, income)
	}
	return applied, err
}

func refundChannelMonitorMidjourneyIncome(ctx context.Context, task *model.Midjourney) {
	if !model.ChannelMonitorIncomeReady.Load() {
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	key := model.ChannelMonitorIncomeKey(strconv.Itoa(task.Id), "midjourney")
	if err := model.RefundChannelMonitorIncome(opCtx, key); err != nil {
		model.MarkChannelMonitorIncomeGap(opCtx)
		logger.LogWarn(ctx, "Midjourney 退款收入记录更新失败: "+err.Error())
	}
}

func markChannelMonitorMidjourneyIncomeRefundPending(ctx context.Context, task *model.Midjourney) bool {
	if !model.ChannelMonitorIncomeReady.Load() || task == nil || task.Id <= 0 {
		return false
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	key := model.ChannelMonitorIncomeKey(strconv.Itoa(task.Id), "midjourney")
	found, changed, err := model.MarkChannelMonitorIncomeRefundPending(opCtx, key)
	if err != nil {
		model.MarkChannelMonitorIncomeGap(opCtx)
		logger.LogWarn(ctx, "标记 Midjourney 退款收入待确认失败: "+err.Error())
		return false
	}
	return found && changed
}

func cancelChannelMonitorMidjourneyIncomeRefund(ctx context.Context, task *model.Midjourney, changed bool) {
	if !changed || task == nil {
		return
	}
	opCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	key := model.ChannelMonitorIncomeKey(strconv.Itoa(task.Id), "midjourney")
	if err := model.CancelChannelMonitorIncomeRefund(opCtx, key); err != nil {
		model.MarkChannelMonitorIncomeGap(opCtx)
		logger.LogWarn(ctx, "恢复 Midjourney 退款收入状态失败: "+err.Error())
	}
}
