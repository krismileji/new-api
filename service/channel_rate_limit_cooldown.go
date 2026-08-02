package service

import (
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type channelRateLimitCooldownKey struct {
	channelId int
	modelName string
}

var channelRateLimitCooldowns = struct {
	sync.Mutex
	untilByRoute map[channelRateLimitCooldownKey]int64
}{
	untilByRoute: make(map[channelRateLimitCooldownKey]int64),
}

// StartChannelRateLimitCooldown temporarily removes one upstream channel/model
// route from new selections. Repeated 429 responses may extend, but never
// shorten, an active cooldown.
func StartChannelRateLimitCooldown(channelId int, modelName string, durationSeconds int) {
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || modelName == "" || durationSeconds <= 0 {
		return
	}
	until := common.GetTimestamp() + int64(durationSeconds)
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}

	channelRateLimitCooldowns.Lock()
	if current := channelRateLimitCooldowns.untilByRoute[key]; current < until {
		channelRateLimitCooldowns.untilByRoute[key] = until
	}
	channelRateLimitCooldowns.Unlock()
}

func ClearChannelRateLimitCooldowns() {
	channelRateLimitCooldowns.Lock()
	channelRateLimitCooldowns.untilByRoute = make(map[channelRateLimitCooldownKey]int64)
	channelRateLimitCooldowns.Unlock()
}

func ChannelRateLimitCooldownUntil(channelId int, modelName string) int64 {
	modelName = strings.TrimSpace(modelName)
	if channelId <= 0 || modelName == "" {
		return 0
	}
	key := channelRateLimitCooldownKey{channelId: channelId, modelName: modelName}
	now := common.GetTimestamp()

	channelRateLimitCooldowns.Lock()
	until := channelRateLimitCooldowns.untilByRoute[key]
	if until <= now {
		delete(channelRateLimitCooldowns.untilByRoute, key)
		until = 0
	}
	channelRateLimitCooldowns.Unlock()
	return until
}

func channelRateLimitCooldownChannelIds(modelName string, now int64) []int {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil
	}

	channelRateLimitCooldowns.Lock()
	channelIds := make([]int, 0)
	for key, until := range channelRateLimitCooldowns.untilByRoute {
		if until <= now {
			delete(channelRateLimitCooldowns.untilByRoute, key)
			continue
		}
		if key.modelName == modelName {
			channelIds = append(channelIds, key.channelId)
		}
	}
	channelRateLimitCooldowns.Unlock()
	sort.Ints(channelIds)
	return channelIds
}

func applyChannelRateLimitCooldowns(
	modelName string,
	options model.ChannelSelectionOptions,
) model.ChannelSelectionOptions {
	cooldownChannelIds := channelRateLimitCooldownChannelIds(modelName, common.GetTimestamp())
	if len(cooldownChannelIds) == 0 {
		return options
	}

	excluded := make(map[int]struct{}, len(options.ExcludedChannelIds)+len(cooldownChannelIds))
	for _, channelId := range options.ExcludedChannelIds {
		excluded[channelId] = struct{}{}
	}
	for _, channelId := range cooldownChannelIds {
		excluded[channelId] = struct{}{}
	}
	options.ExcludedChannelIds = make([]int, 0, len(excluded))
	for channelId := range excluded {
		options.ExcludedChannelIds = append(options.ExcludedChannelIds, channelId)
	}
	sort.Ints(options.ExcludedChannelIds)
	return options
}
