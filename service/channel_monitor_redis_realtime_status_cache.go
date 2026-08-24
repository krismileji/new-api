package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"golang.org/x/sync/singleflight"
)

// A realtime status request reads several Redis keys and stream metadata. A
// short process-local cache keeps a page refresh fan-out from repeating those
// reads while preserving sub-second freshness for status indicators.
const channelMonitorRedisRealtimeStatusCacheTTL = 750 * time.Millisecond

type channelMonitorRedisRealtimeStatusCacheEntry struct {
	client      *redis.Client
	status      ChannelMonitorRedisRealtimeStatus
	generatedAt time.Time
}

var channelMonitorRedisRealtimeStatusCacheState struct {
	sync.RWMutex
	entry channelMonitorRedisRealtimeStatusCacheEntry
	valid bool
}

var channelMonitorRedisRealtimeStatusRefresh singleflight.Group

// GetChannelMonitorRedisRealtimeStatus returns the latest status snapshot for
// this process. Calls sharing the same Redis client reuse a snapshot for less
// than a second, and concurrent misses share one Redis refresh. The client
// pointer is part of the cache identity so tests, reconnects, and runtime
// client swaps cannot receive a snapshot from a previous client.
func GetChannelMonitorRedisRealtimeStatus(ctx context.Context) ChannelMonitorRedisRealtimeStatus {
	client := common.RedisMonitorReadClient()
	if !common.RedisEnabled || client == nil {
		invalidateChannelMonitorRedisRealtimeStatusCache()
		return channelMonitorRedisUnavailableStatus()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	if status, ok := loadChannelMonitorRedisRealtimeStatusCache(client, now); ok {
		return status
	}

	key := fmt.Sprintf("%p", client)
	result, _, _ := channelMonitorRedisRealtimeStatusRefresh.Do(key, func() (any, error) {
		// Another caller may have completed the refresh between the fast path
		// above and acquiring the singleflight slot.
		if status, ok := loadChannelMonitorRedisRealtimeStatusCache(client, time.Now()); ok {
			return status, nil
		}
		queryCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisObservabilityTimeout)
		defer cancel()
		status := getChannelMonitorRedisRealtimeStatus(queryCtx, client, time.Now())
		applyChannelDailyCostReliableStatus(&status, client, queryCtx)
		status.RedisPoolStats = common.GetRedisClientPoolStats()
		// A request cancellation or read timeout must not poison the shared
		// snapshot for the next page refresh; a fast Redis error is still a
		// useful unavailable observation and may be cached briefly.
		if queryCtx.Err() == nil {
			storeChannelMonitorRedisRealtimeStatusCache(client, status, time.Now())
		}
		return status, nil
	})
	status, ok := result.(ChannelMonitorRedisRealtimeStatus)
	if !ok {
		return channelMonitorRedisUnavailableStatus()
	}
	return cloneChannelMonitorRedisRealtimeStatus(status)
}

func invalidateChannelMonitorRedisRealtimeStatusCache() {
	channelMonitorRedisRealtimeStatusCacheState.Lock()
	channelMonitorRedisRealtimeStatusCacheState.entry = channelMonitorRedisRealtimeStatusCacheEntry{}
	channelMonitorRedisRealtimeStatusCacheState.valid = false
	channelMonitorRedisRealtimeStatusCacheState.Unlock()
}

func loadChannelMonitorRedisRealtimeStatusCache(
	client *redis.Client,
	now time.Time,
) (ChannelMonitorRedisRealtimeStatus, bool) {
	channelMonitorRedisRealtimeStatusCacheState.RLock()
	entry := channelMonitorRedisRealtimeStatusCacheState.entry
	valid := channelMonitorRedisRealtimeStatusCacheState.valid
	channelMonitorRedisRealtimeStatusCacheState.RUnlock()
	if !valid || entry.client != client || now.Sub(entry.generatedAt) >= channelMonitorRedisRealtimeStatusCacheTTL {
		return ChannelMonitorRedisRealtimeStatus{}, false
	}
	return cloneChannelMonitorRedisRealtimeStatus(entry.status), true
}

func storeChannelMonitorRedisRealtimeStatusCache(
	client *redis.Client,
	status ChannelMonitorRedisRealtimeStatus,
	generatedAt time.Time,
) {
	channelMonitorRedisRealtimeStatusCacheState.Lock()
	channelMonitorRedisRealtimeStatusCacheState.entry = channelMonitorRedisRealtimeStatusCacheEntry{
		client:      client,
		status:      cloneChannelMonitorRedisRealtimeStatus(status),
		generatedAt: generatedAt,
	}
	channelMonitorRedisRealtimeStatusCacheState.valid = true
	channelMonitorRedisRealtimeStatusCacheState.Unlock()
}

func cloneChannelMonitorRedisRealtimeStatus(
	status ChannelMonitorRedisRealtimeStatus,
) ChannelMonitorRedisRealtimeStatus {
	if status.DegradedReasons != nil {
		status.DegradedReasons = append([]string{}, status.DegradedReasons...)
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
