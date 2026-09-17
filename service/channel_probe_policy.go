package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

var ErrChannelAutoProbeDisabled = errors.New("渠道已禁止自动探测，保留手动检测")
var ErrChannelProbePolicyUnavailable = errors.New("无法读取渠道探测策略，本轮跳过")

type channelProbeTriggerKey struct{}

func WithChannelProbeTrigger(ctx context.Context, trigger string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, channelProbeTriggerKey{}, trigger)
}

func ChannelProbeTrigger(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	trigger, _ := ctx.Value(channelProbeTriggerKey{}).(string)
	return trigger
}

// Direct admin tests are manual. Every scheduler supplies an explicit trigger;
// unknown scheduled payloads must be normalized to scheduled by their handler.
func CheckChannelProbeAllowed(ctx context.Context, channelID int) error {
	return CheckChannelProbeAllowedWithDB(ctx, model.DB, channelID)
}

func CheckChannelProbeAllowedWithDB(ctx context.Context, db *gorm.DB, channelID int) error {
	if ChannelProbeTrigger(ctx) != model.ChannelStatusProbeTriggerScheduled {
		return nil
	}
	policy, err := model.GetChannelProbePolicyWithDB(ctx, db, channelID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrChannelProbePolicyUnavailable, err)
	}
	if policy.AutoProbeDisabled {
		return ErrChannelAutoProbeDisabled
	}
	return nil
}

func IsChannelProbePolicySkip(err error) bool {
	return errors.Is(err, ErrChannelAutoProbeDisabled) || errors.Is(err, ErrChannelProbePolicyUnavailable)
}
