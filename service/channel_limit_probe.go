package service

import (
	"context"
	"errors"
	"fmt"
)

type channelConcurrencyHeldKey struct{}

var ErrChannelLimitProbeBusy = errors.New("共享上游额度不足，本次探测未发送")

func WithChannelConcurrencyLease(ctx context.Context, channelID int, lease *ChannelConcurrencyLease) context.Context {
	if lease != nil && lease.Context != nil {
		ctx = lease.Context
	}
	return context.WithValue(ctx, channelConcurrencyHeldKey{}, channelID)
}

// Manual test transports may bypass the business limiter. Existing callers
// pass their held physical lease to avoid counting a probe twice.
func AcquireChannelLimitProbeLease(ctx context.Context, channelID int) (*ChannelConcurrencyLease, error) {
	if held, _ := ctx.Value(channelConcurrencyHeldKey{}).(int); held == channelID {
		return nil, nil
	}
	// Track every probe so a channel can join a group with its existing usage.
	// Checking membership before acquisition would race with online edits.
	lease, acquired, _, err := AcquireChannelConcurrency(WithChannelProbeTrigger(ctx, "manual"), channelID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChannelLimitProbeBusy, err)
	}
	if !acquired {
		return nil, ErrChannelLimitProbeBusy
	}
	return lease, nil
}
