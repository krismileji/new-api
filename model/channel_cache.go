package model

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
// channel2advancedCustomConfig caches parsed Advanced Custom (type 58) configs so
// path-aware selection avoids re-parsing JSON per request. Refreshed on full sync.
var channel2advancedCustomConfig map[int]*kitdto.AdvancedCustomConfig
var channelSyncLock sync.RWMutex
var channelCacheRefreshLock sync.Mutex

func InitChannelCache() {
	channelCacheRefreshLock.Lock()
	defer channelCacheRefreshLock.Unlock()
	if !common.MemoryCacheEnabled {
		InvalidatePricingCache()
		rebuildTaskAliasView()
		return
	}
	snapshot, err := loadChannelCacheSnapshot()
	if err != nil {
		common.SysError("load channel cache snapshot failed: " + err.Error())
		return
	}
	newChannelId2channel := make(map[int]*Channel)
	newChannel2advancedCustomConfig := make(map[int]*kitdto.AdvancedCustomConfig)
	channels := snapshot.channels
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
		if channel.Type == constant.ChannelTypeAdvancedCustom {
			if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
				newChannel2advancedCustomConfig[channel.Id] = config
			}
		}
	}
	abilities := snapshot.abilities
	newChannelSmartScheduleRouteCache := buildChannelSmartScheduleRouteCacheFromStates(
		abilities,
		newChannelId2channel,
		snapshot.smartScheduleStates,
		snapshot.smartScheduleGroupPauses,
	)
	newChannelLogicalSmartScheduleRoutingCache, err := logicalSmartScheduleRouteOverlaysFromStates(
		snapshot.logicalScheduleStates,
	)
	if err != nil {
		common.SysError("load logical smart schedule routing cache failed: " + err.Error())
		return
	}
	newLogicalChannelRuntimeCache, err := buildLogicalChannelRuntimeSnapshot(DB)
	if err != nil {
		common.SysError("load logical channel runtime snapshot failed: " + err.Error())
		return
	}
	newChannelSmartScheduleRuntimeRouteIndex := buildChannelSmartScheduleRuntimeRouteIndex(newChannelSmartScheduleRouteCache)
	groups := make(map[string]bool)
	for _, ability := range abilities {
		groups[ability.Group] = true
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	for group := range groups {
		newGroup2model2channels[group] = make(map[string][]int)
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue // skip disabled channels
		}
		groups := strings.Split(channel.Group, ",")
		for _, group := range groups {
			models := strings.Split(channel.Models, ",")
			for _, model := range models {
				if _, ok := newGroup2model2channels[group][model]; !ok {
					newGroup2model2channels[group][model] = make([]int, 0)
				}
				newGroup2model2channels[group][model] = append(newGroup2model2channels[group][model], channel.Id)
			}
		}
	}

	// sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				return newChannelId2channel[channels[i]].GetPriority() > newChannelId2channel[channels[j]].GetPriority()
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	channelSmartScheduleRouteCache = newChannelSmartScheduleRouteCache
	channelLogicalSmartScheduleRoutingCache = newChannelLogicalSmartScheduleRoutingCache
	logicalChannelRuntimeCache = newLogicalChannelRuntimeCache
	logicalChannelRuntimeDirty = false
	channelSmartScheduleRouteCacheDirty = make(map[channelSmartScheduleRoutePool]struct{})
	markLocalChannelSmartScheduleRouteSnapshot(time.Now().UnixMilli())
	channelSmartScheduleRouteSnapshotDirtySince = 0
	channelSmartScheduleRouteSnapshotDirtyWatermark = 0
	publishChannelSmartScheduleRuntimeRouteIndex(newChannelSmartScheduleRuntimeRouteIndex)
	//channelsIDM = newChannelId2channel
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				if oldChannel, ok := channelsIDM[i]; ok {
					// 存在旧的渠道，如果是多key且轮询，保留轮询索引信息
					if oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
						channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
					}
				}
			}
		}
	}
	channelsIDM = newChannelId2channel
	channel2advancedCustomConfig = newChannel2advancedCustomConfig
	channelSyncLock.Unlock()
	// Lock ordering: InvalidatePricingCache acquires updatePricingLock, and
	// GetPricing (holding updatePricingLock) nests channelSyncLock.RLock via
	// loadPricingAdvancedCustomConfigs. channelSyncLock MUST be released before
	// invalidating the pricing cache, otherwise the reversed order deadlocks.
	InvalidatePricingCache()
	if channelSmartScheduleRefreshWorkerIsStarted() && common.RedisMonitorWriteClient() != nil {
		channelSyncLock.Lock()
		markChannelSmartScheduleRouteSnapshotDirty()
		channelSyncLock.Unlock()
		signalChannelSmartScheduleRouteSnapshotDirty()
		enqueueChannelSmartScheduleRefresh(channelSmartScheduleRefreshKey{runtime: true})
	}
	rebuildTaskAliasView()
	common.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

func GetRandomSatisfiedChannel(
	group string,
	model string,
	retry int,
	filters []dto.ChannelFilter,
	options ...ChannelSelectionOptions,
) (*Channel, error) {
	selectionOptions := channelSelectionOptions(options)
	selectionOptions.Filters = filters
	requestPath := requestPathFromChannelFilters(filters)
	trafficPolicy := currentChannelSmartScheduleTrafficPolicy()
	if selectionOptions.HasExcludedChannels() {
		retry = 0
	}
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		return getRandomSatisfiedChannelWithoutCacheWithTrafficPolicy(
			group, model, retry, requestPath, selectionOptions, trafficPolicy,
		)
	}
	managedModelNames := channelSmartScheduleRouteModelNames(model)
	managedPool := trafficPolicy != nil && trafficPolicy.managesAnyPool(group, managedModelNames)
	if managedPool && channelSmartScheduleRefreshWorkerIsStarted() &&
		!channelSmartScheduleRouteSnapshotUsable(time.Now()) {
		return nil, ErrChannelSmartScheduleRouteSnapshotUnavailable
	}
	if IsLogicalChannelGroupingEnabled() && managedPool {
		channelSyncLock.RLock()
		runtimeDirty := logicalChannelRuntimeDirty
		routeSnapshotAvailable := channelSmartScheduleRouteCache != nil
		runtimeSnapshotRequired := channelSmartScheduleRouteNeedsRuntime(group, managedModelNames)
		runtimeSnapshotAvailable := logicalChannelRuntimeCache != nil || !runtimeSnapshotRequired
		routePoolDirty := false
		for _, managedModelName := range managedModelNames {
			if trafficPolicy.managesPool(group, managedModelName) {
				if _, dirty := channelSmartScheduleRouteCacheDirty[channelSmartScheduleRoutePool{group: group, model: managedModelName}]; dirty {
					routePoolDirty = true
					break
				}
			}
		}
		channelSyncLock.RUnlock()
		if runtimeDirty {
			// Dirty state only schedules a deduplicated background rebuild.
			enqueueChannelSmartScheduleRefresh(channelSmartScheduleRefreshKey{runtime: true})
		}
		if runtimeDirty || routePoolDirty {
			for _, managedModelName := range managedModelNames {
				if trafficPolicy.managesPool(group, managedModelName) {
					enqueueChannelSmartScheduleRefresh(channelSmartScheduleRefreshKey{
						group: group, model: managedModelName,
					})
				}
			}
		}
		if !runtimeSnapshotAvailable || !routeSnapshotAvailable {
			return nil, ErrChannelSmartScheduleRouteSnapshotUnavailable
		}
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if channel, handled, err := getRandomSatisfiedChannelByAbilityWithTrafficPolicy(
		group, model, retry, requestPath, selectionOptions, trafficPolicy,
	); handled {
		return channel, err
	}

	// First, try to find channels with the exact model name.
	channels, _ := filterCandidateIDs(group2model2channels[group][model], model, filters)
	channels = filterChannelIDsBySelectionOptions(channels, selectionOptions)

	// If no channels found, try to find channels with the normalized model name.
	if len(channels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		channels, _ = filterCandidateIDs(group2model2channels[group][normalizedModel], model, filters)
		channels = filterChannelIDsBySelectionOptions(channels, selectionOptions)
	}

	if len(channels) == 0 {
		return nil, nil
	}

	if len(channels) == 1 {
		if channel, ok := channelsIDM[channels[0]]; ok {
			return channel, nil
		}
		return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channels[0])
	}

	uniquePriorities := make(map[int]bool)
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			uniquePriorities[int(channel.GetPriority())] = true
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}
	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	// get the priority for the given retry number
	var sumWeight = 0
	var targetChannels []*Channel
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			if channel.GetPriority() == targetPriority {
				sumWeight += channel.GetWeight()
				targetChannels = append(targetChannels, channel)
			}
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}

	if len(targetChannels) == 0 {
		return nil, errors.New(fmt.Sprintf("no channel found, group: %s, model: %s, priority: %d", group, model, targetPriority))
	}

	// smoothing factor and adjustment
	smoothingFactor := 1
	smoothingAdjustment := 0

	if sumWeight == 0 {
		// when all channels have weight 0, set sumWeight to the number of channels and set smoothing adjustment to 100
		// each channel's effective weight = 100
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		// when the average weight is less than 10, set smoothing factor to 100
		smoothingFactor = 100
	}

	// Calculate the total weight of all channels up to endIdx
	totalWeight := sumWeight * smoothingFactor

	// Generate a random value in the range [0, totalWeight)
	randomWeight := rand.Intn(totalWeight)

	// Find a channel based on its weight
	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return channel, nil
		}
	}
	// return null if no channel is not found
	return nil, errors.New("channel not found")
}

func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return c, nil
}

func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		return &channel.ChannelInfo, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return &c.ChannelInfo, nil
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.Status = status
	}
	if status != common.ChannelStatusEnabled {
		// delete the channel from group2model2channels
		for group, model2channels := range group2model2channels {
			for model, channels := range model2channels {
				for i, channelId := range channels {
					if channelId == id {
						// remove the channel from the slice
						group2model2channels[group][model] = append(channels[:i], channels[i+1:]...)
						break
					}
				}
			}
		}
	}
}

func CacheUpdateChannel(channel *Channel) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	if channel == nil {
		channelSyncLock.Unlock()
		return
	}

	if channelsIDM == nil {
		channelsIDM = make(map[int]*Channel)
	}
	logicalChannelChanged := false
	if oldChannel, ok := channelsIDM[channel.Id]; ok {
		logicalChannelChanged = (oldChannel.LogicalChannelID == nil) != (channel.LogicalChannelID == nil)
		if !logicalChannelChanged && oldChannel.LogicalChannelID != nil && channel.LogicalChannelID != nil {
			logicalChannelChanged = *oldChannel.LogicalChannelID != *channel.LogicalChannelID
		}
		logger.LogDebug(nil, "CacheUpdateChannel before: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, oldChannel.ChannelInfo.MultiKeyPollingIndex)
	}
	channelsIDM[channel.Id] = channel
	if channel2advancedCustomConfig == nil {
		channel2advancedCustomConfig = make(map[int]*kitdto.AdvancedCustomConfig)
	}
	delete(channel2advancedCustomConfig, channel.Id)
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		if config := channel.GetOtherSettings().AdvancedCustom; config != nil {
			channel2advancedCustomConfig[channel.Id] = config
		}
	}
	logger.LogDebug(nil, "CacheUpdateChannel after: id=%d, name=%s, status=%d, polling_index=%d", channel.Id, channel.Name, channel.Status, channel.ChannelInfo.MultiKeyPollingIndex)
	// Lock ordering: do NOT hold channelSyncLock while calling
	// InvalidatePricingCache. GetPricing acquires updatePricingLock first and then
	// channelSyncLock.RLock (via loadPricingAdvancedCustomConfigs); acquiring
	// updatePricingLock while holding channelSyncLock would be an AB-BA deadlock.
	channelSyncLock.Unlock()
	InvalidatePricingCache()
	if logicalChannelChanged {
		InvalidateLogicalChannelRuntimeCache()
	}
}
