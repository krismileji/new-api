package controller

import (
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const channelSmartScheduleStabilityReleaseInterval = time.Second

var channelSmartScheduleStabilityReleaseWorkerOnce sync.Once

func startChannelSmartScheduleStabilityReleaseWorker() {
	channelSmartScheduleStabilityReleaseWorkerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(channelSmartScheduleStabilityReleaseInterval)
			defer ticker.Stop()
			for now := range ticker.C {
				if err := runChannelSmartScheduleStabilityReleaseOnce(now.Unix()); err != nil {
					common.SysError("推进智能调度稳定性试放失败: " + err.Error())
				}
			}
		}()
	})
}

func runChannelSmartScheduleStabilityReleaseOnce(now int64) error {
	settings := getChannelMonitorSettings()
	if !settings.SmartScheduleEnabled || len(settings.SmartScheduleGroupPolicies) == 0 {
		return nil
	}
	expiredRoutes, err := model.GetExpiredChannelSmartScheduleDegradedRoutes(now)
	if err != nil || len(expiredRoutes) == 0 {
		return err
	}
	policyByGroup := make(map[string]channelSmartSchedulePolicy, len(settings.SmartScheduleGroupPolicies))
	for _, configured := range settings.SmartScheduleGroupPolicies {
		policyByGroup[configured.Group] = configured.policy()
	}
	pools := make([]model.ChannelSmartScheduleStabilityReleasePool, 0, len(expiredRoutes))
	for _, route := range expiredRoutes {
		policy, configured := policyByGroup[route.Group]
		if !configured || !policy.StabilityEnabled ||
			(len(policy.Models) > 0 && !slices.Contains(policy.Models, route.Model)) {
			continue
		}
		pools = append(pools, model.ChannelSmartScheduleStabilityReleasePool{
			Group:                           route.Group,
			Model:                           route.Model,
			StabilityReleaseMaxPromptTokens: policy.StabilityReleaseMaxPromptTokens,
		})
	}
	if len(pools) == 0 {
		return nil
	}
	result, err := model.AdvanceExpiredChannelSmartScheduleDegradedRoutes(
		now,
		settings.SmartScheduleControlRevision,
		pools,
	)
	if err != nil || !result.Applied || len(result.Released) == 0 {
		return err
	}
	refreshedPools := make(map[channelSmartScheduleRoutePoolKey]struct{}, len(result.Released))
	for _, route := range result.Released {
		pool := channelSmartScheduleRoutePoolKey{group: route.Group, model: route.Model}
		if _, refreshed := refreshedPools[pool]; !refreshed {
			if err := model.RefreshChannelSmartScheduleRoutePoolCache(route.Group, route.Model); err != nil {
				common.SysError("刷新稳定性试放路由缓存失败: " + err.Error())
				model.InitChannelCache()
			}
			refreshedPools[pool] = struct{}{}
		}
		enqueueChannelSmartScheduleAdaptiveRefresh(route.ChannelId, route.Model)
	}
	return nil
}
