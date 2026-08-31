package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"golang.org/x/sync/singleflight"
)

var channelMonitorRedisRealtimeStatusQueries singleflight.Group

// GetChannelMonitorRedisRealtimeStatus reads the current Redis status. Only
// callers overlapping the same in-flight query share work; completed results
// are never retained for a later page request.
func GetChannelMonitorRedisRealtimeStatus(ctx context.Context) ChannelMonitorRedisRealtimeStatus {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		return channelMonitorRedisUnavailableStatus()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	key := fmt.Sprintf("%p", client)
	resultChannel := channelMonitorRedisRealtimeStatusQueries.DoChan(key, func() (any, error) {
		queryCtx, cancel := context.WithTimeout(context.Background(), channelMonitorRedisObservabilityTimeout)
		defer cancel()
		status := getChannelMonitorRedisRealtimeStatus(queryCtx, client, time.Now())
		applyChannelDailyCostReliableStatus(&status, client, queryCtx)
		status.RedisPoolStats = common.GetRedisClientPoolStats()
		return status, nil
	})

	select {
	case <-ctx.Done():
		return channelMonitorRedisUnavailableStatus()
	case result := <-resultChannel:
		if result.Err != nil {
			return channelMonitorRedisUnavailableStatus()
		}
		status, ok := result.Val.(ChannelMonitorRedisRealtimeStatus)
		if !ok {
			return channelMonitorRedisUnavailableStatus()
		}
		return cloneChannelMonitorRedisRealtimeStatus(status)
	}
}

func cloneChannelMonitorRedisRealtimeStatus(
	status ChannelMonitorRedisRealtimeStatus,
) ChannelMonitorRedisRealtimeStatus {
	if status.DegradedReasons != nil {
		status.DegradedReasons = append([]string{}, status.DegradedReasons...)
	}
	if status.RedisPoolDegradedRoles != nil {
		status.RedisPoolDegradedRoles = append([]ChannelMonitorRedisPoolDegradedRole{}, status.RedisPoolDegradedRoles...)
	}
	if status.RedisPoolStats != nil {
		poolStats := status.RedisPoolStats
		status.RedisPoolStats = make(map[common.RedisClientRole]common.RedisClientPoolStats, len(poolStats))
		for role, stats := range poolStats {
			status.RedisPoolStats[role] = stats
		}
	}
	return status
}
