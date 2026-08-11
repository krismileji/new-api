package model

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

const (
	channelMonitorSmartScheduleEnabledOption       = "ChannelMonitorSmartScheduleEnabled"
	channelMonitorSmartScheduleGroupPoliciesOption = "ChannelMonitorSmartScheduleGroupPolicies"
)

type channelSmartScheduleTrafficPolicyGroup struct {
	Group  string    `json:"group"`
	Models *[]string `json:"models"`
}

type channelSmartScheduleTrafficPolicy struct {
	enabled       bool
	allModels     map[string]struct{}
	modelsByGroup map[string]map[string]struct{}
}

type channelSmartScheduleTrafficPolicySnapshot struct {
	rawEnabled  string
	rawPolicies string
	policy      *channelSmartScheduleTrafficPolicy
}

var channelSmartScheduleTrafficPolicyCache atomic.Pointer[channelSmartScheduleTrafficPolicySnapshot]
var channelSmartScheduleTrafficPolicyCacheMu sync.Mutex

func currentChannelSmartScheduleTrafficPolicy() *channelSmartScheduleTrafficPolicy {
	common.OptionMapRWMutex.RLock()
	rawEnabled := common.OptionMap[channelMonitorSmartScheduleEnabledOption]
	rawPolicies := common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption]
	common.OptionMapRWMutex.RUnlock()

	if snapshot := channelSmartScheduleTrafficPolicyCache.Load(); snapshot != nil &&
		snapshot.rawEnabled == rawEnabled && snapshot.rawPolicies == rawPolicies {
		return snapshot.policy
	}

	channelSmartScheduleTrafficPolicyCacheMu.Lock()
	defer channelSmartScheduleTrafficPolicyCacheMu.Unlock()
	if snapshot := channelSmartScheduleTrafficPolicyCache.Load(); snapshot != nil &&
		snapshot.rawEnabled == rawEnabled && snapshot.rawPolicies == rawPolicies {
		return snapshot.policy
	}

	policy := parseChannelSmartScheduleTrafficPolicy(rawEnabled, rawPolicies)
	channelSmartScheduleTrafficPolicyCache.Store(&channelSmartScheduleTrafficPolicySnapshot{
		rawEnabled: rawEnabled, rawPolicies: rawPolicies, policy: policy,
	})
	return policy
}

func parseChannelSmartScheduleTrafficPolicy(rawEnabled string, rawPolicies string) *channelSmartScheduleTrafficPolicy {
	policy := &channelSmartScheduleTrafficPolicy{
		enabled:       rawEnabled == "true",
		allModels:     make(map[string]struct{}),
		modelsByGroup: make(map[string]map[string]struct{}),
	}
	if !policy.enabled {
		return policy
	}

	var configured []channelSmartScheduleTrafficPolicyGroup
	if common.UnmarshalJsonStr(rawPolicies, &configured) != nil {
		return policy
	}
	seenGroups := make(map[string]struct{}, len(configured))
	for _, groupPolicy := range configured {
		group := strings.TrimSpace(groupPolicy.Group)
		if group == "" || groupPolicy.Models == nil {
			return &channelSmartScheduleTrafficPolicy{enabled: true}
		}
		if _, exists := seenGroups[group]; exists {
			return &channelSmartScheduleTrafficPolicy{enabled: true}
		}
		seenGroups[group] = struct{}{}
		if len(*groupPolicy.Models) == 0 {
			policy.allModels[group] = struct{}{}
			continue
		}
		models := make(map[string]struct{}, len(*groupPolicy.Models))
		for _, configuredModel := range *groupPolicy.Models {
			modelName := strings.TrimSpace(configuredModel)
			if modelName == "" {
				return &channelSmartScheduleTrafficPolicy{enabled: true}
			}
			models[modelName] = struct{}{}
		}
		policy.modelsByGroup[group] = models
	}
	return policy
}

func (policy *channelSmartScheduleTrafficPolicy) managesPool(group string, modelName string) bool {
	if policy == nil || !policy.enabled {
		return false
	}
	group = strings.TrimSpace(group)
	modelName = strings.TrimSpace(modelName)
	if _, allModels := policy.allModels[group]; allModels {
		return true
	}
	_, allowed := policy.modelsByGroup[group][modelName]
	return allowed
}

func (policy *channelSmartScheduleTrafficPolicy) managesAnyPool(group string, modelNames []string) bool {
	for _, modelName := range modelNames {
		if policy.managesPool(group, modelName) {
			return true
		}
	}
	return false
}

func (policy *channelSmartScheduleTrafficPolicy) allowsRoute(
	group string,
	modelName string,
	participates bool,
) bool {
	if policy == nil || !policy.enabled {
		return true
	}
	return !policy.managesPool(group, modelName) || participates
}

func filterChannelSmartScheduleTrafficAbilities(
	abilities []Ability,
	group string,
	modelName string,
	policy *channelSmartScheduleTrafficPolicy,
) ([]Ability, error) {
	if policy == nil || !policy.managesPool(group, modelName) {
		return abilities, nil
	}
	if len(abilities) == 0 {
		return nil, nil
	}

	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelId)
	}
	var states []ChannelSmartScheduleRouteState
	if DB == nil {
		return nil, nil
	}
	if err := DB.Select("channel_id").
		Where(
			"group_name = ? AND model_name = ? AND channel_id IN ? AND participation_set = ? AND excluded = ?",
			group, modelName, channelIDs, true, false,
		).
		Find(&states).Error; err != nil {
		return nil, err
	}
	participating := make(map[int]struct{}, len(states))
	for _, state := range states {
		participating[state.ChannelId] = struct{}{}
	}
	filtered := make([]Ability, 0, len(participating))
	for _, ability := range abilities {
		if _, allowed := participating[ability.ChannelId]; allowed {
			filtered = append(filtered, ability)
		}
	}
	return filtered, nil
}

func filterChannelSmartScheduleTrafficCachedRoutes(
	routes []channelSmartScheduleCachedRoute,
	group string,
	modelName string,
	policy *channelSmartScheduleTrafficPolicy,
) []channelSmartScheduleCachedRoute {
	if policy == nil || !policy.managesPool(group, modelName) {
		return routes
	}
	if len(routes) == 0 {
		return nil
	}
	filtered := make([]channelSmartScheduleCachedRoute, 0, len(routes))
	for _, route := range routes {
		if route.participates {
			filtered = append(filtered, route)
		}
	}
	return filtered
}
