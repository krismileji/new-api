package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// AcquireChannelBalanceProbeLease tracks transports that bypass the business
// concurrency limiter, including manual tests and automatic health checks.
// A separate key avoids double-counting probes that already hold a routing lease.
func AcquireChannelBalanceProbeLease(ctx context.Context, channelID int) (*ChannelConcurrencyLease, error) {
	if !common.RedisEnabled {
		return nil, nil
	}
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return nil, ErrChannelBalanceUnavailable
	}
	opCtx, cancel := context.WithTimeout(nonNilContext(ctx), channelBalanceOperationTimeout)
	defer cancel()
	key := fmt.Sprintf("channel_balance:{%d}:probe_active", channelID)
	member := common.GetUUID()
	_, err := channelBalanceProbeAcquireScript.Run(opCtx, client, []string{key}, member, channelConcurrencyLeaseTTL.Milliseconds()).Result()
	if err != nil {
		return nil, fmt.Errorf("记录渠道测试进行中状态失败: %w", err)
	}
	return newChannelConcurrencyRedisLease(client, key, member), nil
}

var channelBalanceProbeAcquireScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now - tonumber(ARGV[2]))
redis.call('ZADD', KEYS[1], now, ARGV[1])
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]) * 2)
return 1
`)
