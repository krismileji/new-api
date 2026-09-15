package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/go-redis/redis/v8"
)

type ChannelBalancePolicyHandler func(context.Context, ChannelBalanceConfig, ChannelBalanceEstimate) (bool, error)

var channelBalancePolicyHandler atomic.Pointer[ChannelBalancePolicyHandler]
var channelBalancePolicySlots = make(chan struct{}, 32)

func RegisterChannelBalancePolicyHandler(handler ChannelBalancePolicyHandler) {
	channelBalancePolicyHandler.Store(&handler)
}

// Redis remembers the last handled transition across nodes. An unchanged
// decision performs no database work; failed effects can retry on the next
// request or periodic balance refresh. The worker always re-reads live state.
func notifyChannelBalancePolicy(config ChannelBalanceConfig, raw string) {
	handler := channelBalancePolicyHandler.Load()
	if handler == nil || *handler == nil {
		return
	}
	estimate, err := decodeChannelBalanceEstimate(raw)
	if err != nil || estimate.Decision == "unknown" || estimate.Decision == estimate.AppliedDecision {
		return
	}
	if estimate.Decision == "ok" && estimate.AppliedDecision == "" {
		return // The periodic recovery path handles an already-disabled channel.
	}
	// An older in-flight request retains its cost parameters, but the policy
	// decision belongs to the live configuration stored in this account's state.
	config.Revision = estimate.Revision
	select {
	case channelBalancePolicySlots <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-channelBalancePolicySlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := applyChannelBalancePolicyTransitions(ctx, config, *handler); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("渠道 #%d 实时余额保护状态更新失败: %v", config.ChannelID, err))
		}
	}()
}

func applyChannelBalancePolicyTransitions(ctx context.Context, config ChannelBalanceConfig, handler ChannelBalancePolicyHandler) error {
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return ErrChannelBalanceUnavailable
	}
	key, token := config.key()+":effect", common.GetUUID()
	locked, err := client.SetNX(ctx, key, token, 10*time.Second).Result()
	if err != nil || !locked {
		return err
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), channelBalanceOperationTimeout)
		defer releaseCancel()
		_ = channelBalanceReleaseLockScript.Run(releaseCtx, client, []string{key}, token).Err()
	}()
	for ctx.Err() == nil {
		current, readErr := GetChannelBalanceEstimate(ctx, config)
		if readErr != nil || current.Decision == "unknown" || current.Decision == current.AppliedDecision {
			return readErr
		}
		handled, handleErr := handler(ctx, config, current)
		if handleErr != nil || !handled {
			return handleErr
		}
		// Record what was actually handled, even if another request changed the
		// decision. Re-read under the same lease until the latest transition is
		// handled, then release and compare atomically to avoid a lost wake-up.
		released, ackErr := channelBalancePolicyAcknowledgeScript.Run(ctx, client,
			[]string{config.key() + ":state", key}, token, current.Epoch, current.Decision).Int()
		if ackErr != nil || released != 0 {
			return ackErr
		}
	}
	return ctx.Err()
}

var channelBalancePolicyAcknowledgeScript = redis.NewScript(`
if redis.call('GET', KEYS[2]) ~= ARGV[1] or redis.call('HGET', KEYS[1], 'epoch') ~= ARGV[2] then return 1 end
redis.call('HSET', KEYS[1], 'applied_decision', ARGV[3])
if redis.call('HGET', KEYS[1], 'decision') == ARGV[3] then
  redis.call('DEL', KEYS[2])
  return 1
end
return 0
`)
