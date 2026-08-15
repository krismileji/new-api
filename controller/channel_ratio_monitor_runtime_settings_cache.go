package controller

import (
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

type channelMonitorRuntimeSettingsRaw struct {
	enabled           string
	groupPolicies     string
	performanceWindow string
	stabilityWindow   string
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
		stabilityWindow:   common.OptionMap[channelMonitorSmartScheduleStabilityWindowOption],
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
	stabilityWindow, err := strconv.Atoi(raw.stabilityWindow)
	if err != nil || !isChannelMonitorSmartScheduleWindowSupported(stabilityWindow) {
		stabilityWindow = defaultChannelMonitorSmartScheduleStabilityWindowMinutes
	}
	rateLimitCooldown, err := strconv.Atoi(raw.rateLimitCooldown)
	if err != nil || rateLimitCooldown < 0 || rateLimitCooldown > maxChannelMonitorSmartScheduleRateLimitCooldownSeconds {
		rateLimitCooldown = defaultChannelMonitorSmartScheduleRateLimitCooldownSeconds
	}
	groupPolicies := parseChannelSmartScheduleGroupPolicies(raw.groupPolicies)
	if len(groupPolicies) == 0 {
		enabled = false
	}
	settings := channelMonitorSettings{
		SmartScheduleEnabled:                  enabled,
		SmartScheduleGroupPolicies:            groupPolicies,
		SmartSchedulePerformanceWindowMinutes: performanceWindow,
		SmartScheduleStabilityWindowMinutes:   stabilityWindow,
		SmartScheduleRateLimitCooldownSeconds: rateLimitCooldown,
		SmartScheduleControlRevision:          raw.controlRevision,
	}
	channelMonitorRuntimeSettingsCache.Store(&channelMonitorRuntimeSettingsSnapshot{
		raw: raw, settings: settings,
	})
	return settings
}
