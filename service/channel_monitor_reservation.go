package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// prepareMonitoredReservation replaces only the monitored pre-consume path;
// ordinary upstream billing keeps its existing behavior when monitoring is off.
func (s *BillingSession) prepareMonitoredReservation(c *gin.Context, quota int) (bool, *types.NewAPIError) {
	if !model.ChannelMonitorIncomeReady.Load() || c == nil || c.GetBool(model.ChannelMonitorStatusProbeLogKey) || c.GetBool(model.ChannelMonitorGroupProbeLogKey) {
		return false, nil
	}
	info := *s.relayInfo
	// Selection has updated the request context before pre-consume, while
	// the provider handler initializes ChannelMeta only after pre-consume.
	// Read attribution on this copy without resetting the live relay state.
	if channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId); channelID > 0 {
		info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: channelID}
	}
	info.BillingSource = s.funding.Source()
	record, err := channelMonitorIncomeRecord(c, &info, quota, "request")
	if err != nil {
		return true, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	record.FundingTokenID = info.TokenId
	if info.IsPlayground {
		record.FundingTokenID = 0
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
	defer cancel()
	sub, err := model.ReserveChannelMonitorIncome(ctx, record, info.RequestId)
	if err != nil {
		return true, channelMonitorReservationError(err)
	}
	s.reservationKey = record.SettlementKey
	s.preConsumedQuota, s.tokenConsumed = quota, quota
	switch funding := s.funding.(type) {
	case *WalletFunding:
		funding.consumed, funding.directQuota = quota, true
	case *SubscriptionFunding:
		funding.subscriptionId, funding.preConsumed = sub.UserSubscriptionId, sub.PreConsumed
		funding.AmountTotal, funding.AmountUsedAfter = sub.AmountTotal, sub.AmountUsedAfter
		if plan, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(sub.UserSubscriptionId); err == nil && plan != nil {
			funding.PlanId, funding.PlanTitle = plan.PlanId, plan.PlanTitle
		}
	}
	s.syncRelayInfo()
	return true, nil
}

func (s *BillingSession) adjustMonitoredReservation(target int, available bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := model.AdjustChannelMonitorReservation(ctx, s.reservationKey, s.relayInfo.UserId, s.preConsumedQuota, target, available)
	if err != nil {
		// A COMMIT response can be lost. Leave the durable reservation for
		// review instead of refunding based on the stale in-memory amount.
		if errors.Is(err, model.ErrTaskBillingCommitUncertain) {
			s.fundingSettled = true
			common.SysError("预扣调整未确认，保留资金等待人工核对: " + err.Error())
		}
		return channelMonitorReservationError(err)
	}
	delta := target - s.preConsumedQuota
	s.preConsumedQuota, s.tokenConsumed = target, target
	s.extraReserved += delta
	if funding, ok := s.funding.(*WalletFunding); ok {
		funding.consumed = target
	}
	s.syncRelayInfo()
	return nil
}

func channelMonitorReservationError(err error) *types.NewAPIError {
	code, status := types.ErrorCodeUpdateDataError, http.StatusInternalServerError
	if errors.Is(err, model.ErrChannelMonitorWalletInsufficient) || strings.Contains(err.Error(), "no active subscription") ||
		strings.Contains(err.Error(), "subscription quota insufficient") || strings.Contains(err.Error(), "subscription used exceeds total") {
		code, status = types.ErrorCodeInsufficientUserQuota, http.StatusForbidden
	} else if errors.Is(err, model.ErrChannelMonitorTokenInsufficient) {
		code, status = types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden
	}
	return types.NewErrorWithStatusCode(err, code, status, types.ErrOptionWithSkipRetry())
}
