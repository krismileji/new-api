package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

const channelDailyCostProjectionRecoveryInterval = 5 * time.Second

// applyChannelDailyCostOutboxProjection is shared by immediate Stream delivery
// and database recovery. Historical rows stay in the ledger and must not
// recreate expired Redis day hashes when an old Stream message is replayed.
func applyChannelDailyCostOutboxProjection(ctx context.Context, client *redis.Client, records []model.ChannelDailyCostOutbox) error {
	retainedFrom := model.ChannelDailyCostDayStart(time.Now().Unix()) - 24*60*60
	rows := make([]model.ChannelDailyCostOutbox, 0, len(records))
	ids := make([]int64, 0, len(records))
	for _, row := range records {
		if row.RedisProjectedAt != 0 || row.OccurredAt < retainedFrom {
			continue
		}
		rows = append(rows, row)
		ids = append(ids, row.Id)
	}
	if len(rows) == 0 {
		return nil
	}
	if err := projectChannelDailyCostOutboxRows(ctx, client, rows); err != nil {
		return err
	}
	if err := reconcileChannelBalanceCostEvents(ctx, client, rows); err != nil {
		return err
	}
	return model.MarkChannelDailyCostProjectionsApplied(ctx, ids, time.Now().Unix())
}

// recoverRedisCostProjections drains a bounded amount of committed work that
// missed immediate delivery, including direct database writes and task cost
// corrections. Pending=true requests an earlier follow-up for a full batch.
func (runtime *ChannelDailyCostOutboxRuntime) recoverRedisCostProjections(ctx context.Context) (pending bool, err error) {
	deadline := time.Now().Add(2 * time.Second)
	for ctx.Err() == nil {
		opCtx, cancel := context.WithTimeout(ctx, channelDailyCostOutboxDBOperationTimeout)
		rows, err := model.PendingChannelDailyCostProjections(opCtx, channelDailyCostOutboxBatchSize)
		if err == nil {
			err = applyChannelDailyCostOutboxProjection(opCtx, runtime.redisClient, rows)
		}
		cancel()
		if err != nil {
			return true, err
		}
		if len(rows) < channelDailyCostOutboxBatchSize {
			return false, nil
		}
		if time.Now().After(deadline) {
			return true, nil
		}
	}
	return true, ctx.Err()
}
