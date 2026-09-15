package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

// Use records already delivered by the real cost outbox. No extra SQL reads or
// writes are introduced. Successful synchronous completions remove their Redis
// recovery marker immediately, so normal late ledger delivery is a no-op.
func reconcileChannelBalanceCostEvents(ctx context.Context, client *redis.Client, rows []model.ChannelDailyCostOutbox) error {
	if !common.RedisEnabled || client == nil {
		return nil
	}
	pipeline := client.Pipeline()
	commands := make(map[string]*redis.StringCmd)
	for _, row := range rows {
		if row.SettledDelta != 1 || row.UnresolvedDelta != 0 || row.ProjectionEventId != "" {
			continue
		}
		commands[row.EventId] = pipeline.Get(ctx, fmt.Sprintf("channel_balance:{%d}:recovery:%s", row.ChannelId, row.EventId))
	}
	if len(commands) == 0 {
		return nil
	}
	if _, err := pipeline.Exec(ctx); err != nil && err != redis.Nil {
		return err
	}
	for _, row := range rows {
		command := commands[row.EventId]
		if command == nil || command.Err() == redis.Nil {
			continue
		}
		if err := command.Err(); err != nil {
			return err
		}
		var attempt channelBalanceAttempt
		if err := common.UnmarshalJsonStr(command.Val(), &attempt); err != nil {
			return err
		}
		if attempt.Config.ChannelID != row.ChannelId || attempt.CompletionUncertain {
			continue
		}
		if _, err := runChannelBalanceOperation(ctx, attempt.Config, "reconcile", row.EventId, attempt.Sample,
			common.GetUUID(), 0, row.EventId, "0", 0); err != nil {
			return err
		}
	}
	return nil
}
