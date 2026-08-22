package controller

import (
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func init() {
	model.RegisterChannelSmartScheduleTrafficPolicyValidator(func(enabled string, rawPolicies string) bool {
		enabledValue, err := strconv.ParseBool(enabled)
		if err != nil || !enabledValue {
			return true
		}
		policies, err := parseChannelSmartScheduleGroupPoliciesWithError(rawPolicies)
		return err == nil && len(policies) > 0
	})
}

type channelMonitorRuntimeSettingsRaw struct {
	enabled           string
	groupPolicies     string
	performanceWindow string
	rateLimitCooldown string
	controlRevision   string
}

type channelMonitorRuntimeSettingsSnapshot struct {
	raw      channelMonitorRuntimeSettingsRaw
	settings channelMonitorSettings
}

var channelMonitorRuntimeSettingsCache atomic.Pointer[channelMonitorRuntimeSettingsSnapshot]
var channelMonitorRuntimeSettingsCacheMu sync.Mutex

func getChannelMonitorRuntimeSettings() channelMonitorSettings {
	common.OptionMapRWMutex.RLock()
	raw := channelMonitorRuntimeSettingsRaw{
		enabled:           common.OptionMap[channelMonitorSmartScheduleEnabledOption],
		groupPolicies:     common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption],
		performanceWindow: common.OptionMap[channelMonitorSmartSchedulePerformanceWindowOption],
		rateLimitCooldown: common.OptionMap[channelMonitorSmartScheduleRateLimitCooldownOption],
		controlRevision:   common.OptionMap[channelMonitorSmartScheduleControlRevisionOption],
	}
	common.OptionMapRWMutex.RUnlock()

	if snapshot := channelMonitorRuntimeSettingsCache.Load(); snapshot != nil && snapshot.raw == raw {
		return snapshot.settings
	}
	channelMonitorRuntimeSettingsCacheMu.Lock()
	defer channelMonitorRuntimeSettingsCacheMu.Unlock()
	if snapshot := channelMonitorRuntimeSettingsCache.Load(); snapshot != nil && snapshot.raw == raw {
		return snapshot.settings
	}

	enabled, err := strconv.ParseBool(raw.enabled)
	if err != nil {
		enabled = false
	}
	performanceWindow, err := strconv.Atoi(raw.performanceWindow)
	if err != nil || !isChannelMonitorSmartScheduleWindowSupported(performanceWindow) {
		performanceWindow = defaultChannelMonitorSmartSchedulePerformanceWindowMinutes
	}
	rateLimitCooldown, err := strconv.Atoi(raw.rateLimitCooldown)
	if err != nil || rateLimitCooldown < 0 || rateLimitCooldown > maxChannelMonitorSmartScheduleRateLimitCooldownSeconds {
		rateLimitCooldown = defaultChannelMonitorSmartScheduleRateLimitCooldownSeconds
	}
	groupPolicies, _ := parseChannelSmartScheduleGroupPoliciesWithError(raw.groupPolicies)
	if len(groupPolicies) == 0 {
		enabled = false
	}
	settings := channelMonitorSettings{
		SmartScheduleEnabled:                  enabled,
		SmartScheduleGroupPolicies:            groupPolicies,
		SmartSchedulePerformanceWindowMinutes: performanceWindow,
		SmartScheduleRateLimitCooldownSeconds: rateLimitCooldown,
		SmartScheduleControlRevision:          raw.controlRevision,
	}
	channelMonitorRuntimeSettingsCache.Store(&channelMonitorRuntimeSettingsSnapshot{
		raw: raw, settings: settings,
	})
	return settings
}
