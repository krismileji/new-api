package controller

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	channelStatusProbeOverviewCacheDefaultTTLMilliseconds = 3000
	channelStatusProbeOverviewCacheMaxTTLMilliseconds     = 30000
)

type channelStatusProbeOverviewCacheKey struct {
	db            *gorm.DB
	generation    uint64
	selectedModel string
}

type channelStatusProbeOverviewCacheEntry struct {
	expiresAt time.Time
	response  channelStatusProbeOverviewResponse
}

var channelStatusProbeOverviewCache = struct {
	sync.RWMutex
	items map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry
}{
	items: make(map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry),
}

var channelStatusProbeOverviewSingleflight singleflight.Group
var channelStatusProbeOverviewCacheGeneration atomic.Uint64

func channelStatusProbeOverviewCacheTTL() time.Duration {
	milliseconds := common.GetEnvOrDefault(
		"CHANNEL_STATUS_PROBE_OVERVIEW_CACHE_TTL_MS",
		channelStatusProbeOverviewCacheDefaultTTLMilliseconds,
	)
	if milliseconds <= 0 {
		return 0
	}
	milliseconds = min(milliseconds, channelStatusProbeOverviewCacheMaxTTLMilliseconds)
	return time.Duration(milliseconds) * time.Millisecond
}

func (key channelStatusProbeOverviewCacheKey) singleflightKey() string {
	return fmt.Sprintf("%p:%d:%q", key.db, key.generation, key.selectedModel)
}

func loadChannelStatusProbeOverviewCacheEntry(
	key channelStatusProbeOverviewCacheKey,
	now time.Time,
) (channelStatusProbeOverviewResponse, bool) {
	channelStatusProbeOverviewCache.RLock()
	entry, exists := channelStatusProbeOverviewCache.items[key]
	channelStatusProbeOverviewCache.RUnlock()
	if !exists || !now.Before(entry.expiresAt) {
		return channelStatusProbeOverviewResponse{}, false
	}
	return entry.response, true
}

func storeChannelStatusProbeOverviewCacheEntry(
	key channelStatusProbeOverviewCacheKey,
	now time.Time,
	ttl time.Duration,
	response channelStatusProbeOverviewResponse,
) {
	channelStatusProbeOverviewCache.Lock()
	defer channelStatusProbeOverviewCache.Unlock()
	if key.generation != channelStatusProbeOverviewCacheGeneration.Load() {
		return
	}
	for cachedKey, entry := range channelStatusProbeOverviewCache.items {
		if !now.Before(entry.expiresAt) {
			delete(channelStatusProbeOverviewCache.items, cachedKey)
		}
	}
	channelStatusProbeOverviewCache.items[key] = channelStatusProbeOverviewCacheEntry{
		expiresAt: now.Add(ttl),
		response:  response,
	}
}

func getChannelStatusProbeOverviewCached(selectedModel string) (channelStatusProbeOverviewResponse, error) {
	ttl := channelStatusProbeOverviewCacheTTL()
	if ttl <= 0 {
		return buildChannelStatusProbeOverview(selectedModel, common.GetTimestamp())
	}

	for {
		key := channelStatusProbeOverviewCacheKey{
			db:            model.DB,
			generation:    channelStatusProbeOverviewCacheGeneration.Load(),
			selectedModel: selectedModel,
		}
		now := time.Now()
		if response, exists := loadChannelStatusProbeOverviewCacheEntry(key, now); exists {
			if key.generation == channelStatusProbeOverviewCacheGeneration.Load() {
				return response, nil
			}
			continue
		}

		result, err, _ := channelStatusProbeOverviewSingleflight.Do(key.singleflightKey(), func() (any, error) {
			loadTime := time.Now()
			if response, exists := loadChannelStatusProbeOverviewCacheEntry(key, loadTime); exists {
				return response, nil
			}
			response, loadErr := buildChannelStatusProbeOverview(selectedModel, common.GetTimestamp())
			if loadErr != nil {
				return nil, loadErr
			}
			storeChannelStatusProbeOverviewCacheEntry(key, loadTime, ttl, response)
			return response, nil
		})
		if err != nil {
			return channelStatusProbeOverviewResponse{}, err
		}
		if key.generation != channelStatusProbeOverviewCacheGeneration.Load() {
			continue
		}
		return result.(channelStatusProbeOverviewResponse), nil
	}
}

func invalidateChannelStatusProbeOverviewCache() {
	channelStatusProbeOverviewCache.Lock()
	channelStatusProbeOverviewCacheGeneration.Add(1)
	channelStatusProbeOverviewCache.items = make(map[channelStatusProbeOverviewCacheKey]channelStatusProbeOverviewCacheEntry)
	channelStatusProbeOverviewCache.Unlock()
}
