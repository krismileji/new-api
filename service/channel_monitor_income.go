package service

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type channelMonitorIncomeConversion struct{ QuotaPerUnit float64 }

var errChannelMonitorIncomeFundingQueued = errors.New("收入结算已持久化，等待自动恢复")

func captureChannelMonitorIncomeConversion(c *gin.Context) {
	if c == nil {
		return
	}
	if _, exists := c.Get("channel_monitor_income_conversion"); !exists {
		c.Set("channel_monitor_income_conversion", channelMonitorIncomeConversion{common.QuotaPerUnit})
	}
}

func prepareChannelMonitorIncome(c *gin.Context, info *relaycommon.RelayInfo, quota int, kind string) *model.ChannelMonitorIncome {
	record, _ := prepareChannelMonitorIncomeResult(c, info, quota, kind, nil)
	return record
}

func prepareChannelMonitorIncomeResult(c *gin.Context, info *relaycommon.RelayInfo, quota int, kind string, fundingDelta *int) (*model.ChannelMonitorIncome, error) {
	record, err := channelMonitorIncomeRecord(c, info, quota, kind)
	if err != nil || record == nil {
		return record, err
	}
	if fundingDelta != nil {
		record.Status, record.FundingDelta = "funding_pending", *fundingDelta
		record.FundingSubscriptionID, record.FundingTokenID = info.SubscriptionId, info.TokenId
		if info.IsPlayground {
			record.FundingTokenID = 0
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	if err := model.PrepareChannelMonitorIncome(ctx, record); err != nil {
		model.MarkChannelMonitorIncomeGapForCost(ctx, info.ChannelId, time.Now().Unix(), record.CostEventID)
		logger.LogWarn(c, "记录收入结算失败，利润统计存在缺口: "+err.Error())
		return nil, err
	}
	return record, nil
}

func channelMonitorIncomeRecord(c *gin.Context, info *relaycommon.RelayInfo, quota int, kind string) (*model.ChannelMonitorIncome, error) {
	if !model.ChannelMonitorIncomeReady.Load() || info == nil || c == nil ||
		c.GetBool(model.ChannelMonitorStatusProbeLogKey) || c.GetBool(model.ChannelMonitorGroupProbeLogKey) {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	captureChannelMonitorIncomeConversion(c)
	value, _ := c.Get("channel_monitor_income_conversion")
	conversion := value.(channelMonitorIncomeConversion)
	channelID := 0
	if info.ChannelMeta != nil {
		channelID = info.ChannelId
	}
	if info.RequestId == "" || info.ChannelMeta == nil || info.ChannelId <= 0 || math.IsNaN(conversion.QuotaPerUnit) || math.IsInf(conversion.QuotaPerUnit, 0) {
		model.MarkChannelMonitorIncomeGapAt(ctx, channelID, time.Now().Unix())
		logger.LogWarn(c, "收入记录缺少请求标识或有效换算参数")
		return nil, errors.New("收入记录缺少请求标识或有效换算参数")
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
		QuotaPerUnit: strconv.FormatFloat(conversion.QuotaPerUnit, 'f', -1, 64), USDToCNY: "1",
	}
	record.CostEventID = channelDailyCostEventId(c, info.ChannelId)
	return record, nil
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

func settleMonitoredIncome(c *gin.Context, info *relaycommon.RelayInfo, record *model.ChannelMonitorIncome, delta int) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	tokenID := info.TokenId
	if info.IsPlayground {
		tokenID = 0
	}
	if record.Status == "funding_pending" {
		if record.FundingDelta != delta || record.FundingTokenID != tokenID || record.FundingSubscriptionID != info.SubscriptionId {
			return errors.New("收入结算恢复指令不匹配")
		}
	} else if err := model.QueueChannelMonitorIncomeFunding(ctx, record, info.SubscriptionId, tokenID, delta); err != nil {
		return err
	}
	if err := model.SettleChannelMonitorIncomeFunding(ctx, record.SettlementKey, info.UserId, info.SubscriptionId, tokenID, delta); err != nil {
		logger.LogWarn(c, "收入结算已持久化，后台将重试: "+err.Error())
		return errors.Join(errChannelMonitorIncomeFundingQueued, err)
	}
	return nil
}

func (s *BillingSession) settleWithIncome(c *gin.Context, actualQuota int) error {
	if !model.ChannelMonitorIncomeReady.Load() || c == nil || c.GetBool(model.ChannelMonitorStatusProbeLogKey) || c.GetBool(model.ChannelMonitorGroupProbeLogKey) {
		return s.Settle(actualQuota)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return nil
	}
	if s.refunded {
		return errors.New("计费会话已退款或结算状态待核对")
	}
	if s.fundingSettled {
		return errors.New("结算已提交或状态待核对，不能再次提交")
	}
	delta := actualQuota - s.preConsumedQuota
	// The first durable income row already includes its replay instruction.
	// Hold the session lock through handoff so Refund cannot race the worker.
	income, err := prepareChannelMonitorIncomeResult(c, s.relayInfo, actualQuota, "request", &delta)
	if err != nil {
		// An INSERT acknowledgement or a subsequent attribution read can fail
		// after the intent committed. Never separately refund that reservation.
		s.fundingSettled = true
		return err
	}
	if income.Quota != int64(actualQuota) {
		return errors.New("收入结算额度与已保存快照不匹配")
	}
	s.fundingSettled = true
	if err := settleMonitoredIncome(c, s.relayInfo, income, delta); err != nil && !errors.Is(err, errChannelMonitorIncomeFundingQueued) {
		return err
	}
	s.fundingSettled, s.settled = true, true
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}
	return nil
}

func postConsumeMonitoredIncome(c *gin.Context, info *relaycommon.RelayInfo, income *model.ChannelMonitorIncome, delta, reserved int, notify bool) (postConsumeQuotaResult, error) {
	if income == nil {
		return postConsumeQuotaWithResult(info, delta, reserved, notify)
	}
	if income.Quota != int64(delta)+int64(reserved) {
		return postConsumeQuotaResult{}, errors.New("收入结算额度与已保存快照不匹配")
	}
	fundingApplied := true
	if err := settleMonitoredIncome(c, info, income, delta); err != nil {
		if !errors.Is(err, errChannelMonitorIncomeFundingQueued) {
			return postConsumeQuotaResult{}, err
		}
		fundingApplied = false
	}
	if info.BillingSource == BillingSourceSubscription {
		info.SubscriptionPostDelta += int64(delta)
	}
	if fundingApplied && notify && delta+reserved != 0 {
		checkAndSendQuotaNotify(info, delta, reserved)
	}
	return postConsumeQuotaResult{FundingApplied: fundingApplied, TokenApplied: fundingApplied && !info.IsPlayground}, nil
}

func (s *BillingSession) fundingCommitted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fundingSettled || s.settled
}

type channelMonitorIncomeRecoveryHandler struct{}

func (channelMonitorIncomeRecoveryHandler) Type() string { return "channel_monitor_income_recovery" }
func (channelMonitorIncomeRecoveryHandler) Enabled() bool {
	return model.ChannelMonitorIncomeReady.Load()
}
func (channelMonitorIncomeRecoveryHandler) Interval() time.Duration { return time.Minute }
func (channelMonitorIncomeRecoveryHandler) NewPayload() any         { return nil }
func (channelMonitorIncomeRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	completed, err := model.RecoverChannelMonitorIncomeFunding(ctx, 100)
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, err.Error()
	}
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"recovered": completed}, message); finishErr != nil {
		common.SysError("更新收入恢复任务状态失败: " + finishErr.Error())
	}
}

// Independent task types give each queue its own lease and time budget. A
// blocked settlement must not prevent cost attribution or cache repair.
type channelMonitorIncomeCostRecoveryHandler struct{}

func (channelMonitorIncomeCostRecoveryHandler) Type() string {
	return "channel_monitor_income_cost_recovery"
}
func (channelMonitorIncomeCostRecoveryHandler) Enabled() bool {
	return model.ChannelMonitorIncomeReady.Load()
}
func (channelMonitorIncomeCostRecoveryHandler) Interval() time.Duration { return time.Minute }
func (channelMonitorIncomeCostRecoveryHandler) NewPayload() any         { return nil }
func (channelMonitorIncomeCostRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	completed, err := model.RecoverChannelMonitorIncomeCosts(ctx, 100)
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, err.Error()
	}
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"cost_recovered": completed}, message); finishErr != nil {
		common.SysError("更新收入成本恢复任务状态失败: " + finishErr.Error())
	}
}

type channelMonitorFundingCacheRecoveryHandler struct{}

func (channelMonitorFundingCacheRecoveryHandler) Type() string {
	return "channel_monitor_funding_cache_recovery"
}
func (channelMonitorFundingCacheRecoveryHandler) Enabled() bool           { return common.RedisEnabled }
func (channelMonitorFundingCacheRecoveryHandler) Interval() time.Duration { return time.Minute }
func (channelMonitorFundingCacheRecoveryHandler) NewPayload() any         { return nil }
func (channelMonitorFundingCacheRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	completed, err := model.RecoverChannelMonitorFundingCaches(ctx, 500)
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, err.Error()
	}
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"cache_recovered": completed}, message); finishErr != nil {
		common.SysError("更新额度缓存恢复任务状态失败: " + finishErr.Error())
	}
}

func init() {
	RegisterSystemTaskHandler(channelMonitorIncomeRecoveryHandler{})
	RegisterSystemTaskHandler(channelMonitorIncomeCostRecoveryHandler{})
	RegisterSystemTaskHandler(channelMonitorFundingCacheRecoveryHandler{})
}

// Legacy Midjourney bills outside SettleBilling. Its persisted task ID also
// lets a later refund find the income without retaining request context.
func SettleMonitoredMidjourneyBilling(c *gin.Context, info *relaycommon.RelayInfo, task *model.Midjourney, prepared bool) (bool, error) {
	var income *model.ChannelMonitorIncome
	if prepared && info != nil && info.ChannelMeta != nil && task != nil && task.Id > 0 && task.PendingBilling != nil {
		snapshot := *info
		meta := *info.ChannelMeta
		meta.ChannelId = task.PendingBilling.ChannelID
		snapshot.ChannelMeta = &meta
		snapshot.RequestId = strconv.Itoa(task.Id)
		snapshot.OriginModelName = CovertMjpActionToModelName(task.Action)
		income = prepareChannelMonitorIncome(c, &snapshot, task.PendingBilling.Quota, "midjourney")
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
		model.MarkChannelMonitorIncomeGapAt(opCtx, task.GetBillingChannelId(), task.SubmitTime/1000)
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
	if err := model.PrepareChannelMonitorMidjourneyRefund(opCtx, task); err != nil {
		model.MarkChannelMonitorIncomeGapAt(opCtx, task.GetBillingChannelId(), task.SubmitTime/1000)
		logger.LogWarn(ctx, "创建 Midjourney 退款收入记录失败: "+err.Error())
		return false
	}
	found, changed, err := model.MarkChannelMonitorIncomeRefundPending(opCtx, key)
	if err != nil {
		model.MarkChannelMonitorIncomeGapAt(opCtx, task.GetBillingChannelId(), task.SubmitTime/1000)
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
		model.MarkChannelMonitorIncomeGapAt(opCtx, task.GetBillingChannelId(), task.SubmitTime/1000)
		logger.LogWarn(ctx, "恢复 Midjourney 退款收入状态失败: "+err.Error())
	}
}
