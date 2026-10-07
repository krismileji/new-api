package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// FinishWithoutCharge transfers ownership of the reservation to a durable,
// idempotent refund before allowing a local success response.
func (s *BillingSession) FinishWithoutCharge(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refunded {
		return nil
	}
	if s.settled || s.fundingSettled {
		return errors.New("请求已结算，不能改为本地响应")
	}
	refund := model.ChannelLocalResponseRefund{RequestID: s.relayInfo.RequestId, UserID: s.relayInfo.UserId}
	if !s.relayInfo.IsPlayground && s.tokenConsumed > 0 {
		refund.TokenID = s.relayInfo.TokenId
		refund.TokenQuota = int64(s.tokenConsumed)
	}
	switch funding := s.funding.(type) {
	case *WalletFunding:
		refund.WalletQuota = int64(funding.consumed)
	case *SubscriptionFunding:
		refund.SubscriptionID = funding.subscriptionId
		refund.SubscriptionQuota = funding.preConsumed + int64(s.extraReserved)
	default:
		return errors.New("该计费会话不支持本地响应退款")
	}
	if refund.WalletQuota+refund.TokenQuota+refund.SubscriptionQuota == 0 && s.reservationKey == "" {
		s.refunded = true
		return nil
	}
	// Cancellation must not abandon reservations after deciding to respond.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := model.QueueChannelLocalResponseRefund(ctx, &refund); err != nil {
		if errors.Is(err, model.ErrTaskBillingCommitUncertain) {
			// The local transfer owns both outcomes of an ambiguous commit.
			// A separate ordinary refund could credit the same debit twice.
			s.refunded = true
		}
		return err
	}
	s.refunded = true
	if err := model.ApplyChannelLocalResponseRefund(ctx, refund.RequestID); err != nil {
		common.SysError("本地响应退款已持久化，等待重试: " + err.Error())
	}
	return nil
}

type channelLocalResponseRefundHandler struct{}

func (channelLocalResponseRefundHandler) Type() string            { return "channel_local_response_refund" }
func (channelLocalResponseRefundHandler) Enabled() bool           { return true }
func (channelLocalResponseRefundHandler) Interval() time.Duration { return time.Minute }
func (channelLocalResponseRefundHandler) NewPayload() any         { return nil }
func (channelLocalResponseRefundHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	completed, err := model.RecoverChannelLocalResponseRefunds(ctx, 100)
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, err.Error()
	}
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"refunded": completed}, message); finishErr != nil {
		common.SysError(finishErr.Error())
	}
}

func init() { RegisterSystemTaskHandler(channelLocalResponseRefundHandler{}) }

func runChannelLocalRefundTransferRecovery(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		batchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		_, err := model.RecoverChannelLocalRefundTransfers(batchCtx, 100)
		cancel()
		if err != nil {
			common.SysError("本机退款入队确认失败，保留批次稍后重试: " + err.Error())
		}
	}
}
