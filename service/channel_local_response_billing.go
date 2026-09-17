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
	if refund.WalletQuota+refund.TokenQuota+refund.SubscriptionQuota == 0 {
		s.refunded = true
		return nil
	}
	// Cancellation must not abandon reservations after deciding to respond.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := model.QueueChannelLocalResponseRefund(ctx, &refund); err != nil {
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
	var records []model.ChannelLocalResponseRefund
	err := model.DB.WithContext(ctx).Where("cache_applied = ?", false).Order("updated_at, id").Limit(100).Find(&records).Error
	for _, record := range records {
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		if applyErr := model.ApplyChannelLocalResponseRefund(ctx, record.RequestID); applyErr != nil {
			err = errors.Join(err, applyErr)
			model.DB.WithContext(ctx).Model(&record).Update("updated_at", time.Now().Unix())
		}
	}
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, err.Error()
	}
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, status, nil, message); finishErr != nil {
		common.SysError(finishErr.Error())
	}
}

func init() { RegisterSystemTaskHandler(channelLocalResponseRefundHandler{}) }
