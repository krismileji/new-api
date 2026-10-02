package service

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

type ChannelMonitorProfitBlock struct {
	From, To  int64
	ChannelID int
	Reason    string
}

// Read the producer queue before opening the ledger snapshot. A worker may
// commit and remove an event afterwards, but cannot make an older DB snapshot
// appear complete by draining the queue between the two reads.
func ReadChannelMonitorProfitCostQueue(ctx context.Context) []ChannelMonitorProfitBlock {
	if !common.RedisEnabled {
		return nil
	}
	client := common.RedisMonitorWriteClient()
	if client == nil {
		return []ChannelMonitorProfitBlock{{Reason: "profit_cost_queue_unavailable"}}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var blocks []ChannelMonitorProfitBlock
	for _, stream := range []string{ChannelDailyCostRedisStream, ChannelDailyCostRedisDeadLetter} {
		// Bounded work: an unreadable or oversized backlog remains uncertain,
		// never silently treat the inspected prefix as the entire queue.
		messages, err := client.XRangeN(ctx, stream, "-", "+", 4097).Result()
		if err != nil || len(messages) > 4096 {
			return []ChannelMonitorProfitBlock{{Reason: "profit_cost_queue_unavailable"}}
		}
		reason := "cost_projection_pending"
		if stream == ChannelDailyCostRedisDeadLetter {
			reason = "profit_cost_unresolved"
		}
		for _, message := range messages {
			payload, ok := message.Values[channelDailyCostRedisFieldPayload].(string)
			var event channelDailyCostOutboxPayload
			if !ok || len(payload) > channelDailyCostOutboxPayloadMaxLength || common.UnmarshalJsonStr(payload, &event) != nil || event.ChannelId <= 0 || event.OccurredAt <= 0 {
				blocks = append(blocks, ChannelMonitorProfitBlock{Reason: reason})
				continue
			}
			day := model.ChannelDailyCostDayStart(event.OccurredAt)
			blocks = append(blocks, ChannelMonitorProfitBlock{From: day, To: day + 86400, ChannelID: event.ChannelId, Reason: reason})
		}
		if stream == ChannelDailyCostRedisStream {
			pending, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: stream, Group: ChannelDailyCostRedisConsumerGroup, Start: "-", End: "+", Count: 4097}).Result()
			if err != nil && !strings.Contains(err.Error(), "NOGROUP") {
				blocks = append(blocks, ChannelMonitorProfitBlock{Reason: "profit_cost_queue_unavailable"})
			} else if len(pending) > 4096 {
				blocks = append(blocks, ChannelMonitorProfitBlock{Reason: "cost_projection_pending"})
			} else {
				ids := make(map[string]bool, len(messages))
				for _, message := range messages {
					ids[message.ID] = true
				}
				for _, message := range pending {
					if !ids[message.ID] {
						blocks = append(blocks, ChannelMonitorProfitBlock{Reason: "cost_projection_pending"})
						break
					}
				}
			}
		}
	}
	return blocks
}
