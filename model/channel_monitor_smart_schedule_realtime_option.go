package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

const (
	ChannelMonitorSmartScheduleRealtimeRetentionOption   = "ChannelMonitorSmartScheduleRealtimeRetentionMinutes"
	ChannelMonitorSmartScheduleRealtimeSampleLimitOption = "ChannelMonitorSmartScheduleRealtimeSampleLimit"

	ChannelMonitorSmartScheduleMinRealtimeRetentionMinutes     = 5
	ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes     = 1440
	ChannelMonitorSmartScheduleDefaultRealtimeRetentionMinutes = 60
	ChannelMonitorSmartScheduleMinRealtimeSampleLimit          = 1000
	ChannelMonitorSmartScheduleMaxRealtimeSampleLimit          = 200000
	ChannelMonitorSmartScheduleDefaultRealtimeSampleLimit      = 20000
)

type ChannelMonitorSmartScheduleRealtimeSettings struct {
	RetentionMinutes int
	SampleLimit      int
}

type channelMonitorSmartScheduleGroupPolicyRetention struct {
	StabilityWindowMinutes *int `json:"stability_window_minutes"`
}

func ChannelMonitorSmartScheduleMaxPolicyStabilityWindowMinutes(raw string) int {
	if raw == "" {
		return 0
	}
	var policies []channelMonitorSmartScheduleGroupPolicyRetention
	if err := common.UnmarshalJsonStr(raw, &policies); err != nil {
		return 0
	}
	maximum := 0
	for _, policy := range policies {
		if policy.StabilityWindowMinutes != nil &&
			*policy.StabilityWindowMinutes >= 1 &&
			*policy.StabilityWindowMinutes <= ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes {
			maximum = max(maximum, *policy.StabilityWindowMinutes)
		}
	}
	return maximum
}

func ChannelMonitorSmartScheduleRealtimeSettingsFromOptions(
	options map[string]string,
) ChannelMonitorSmartScheduleRealtimeSettings {
	retentionMinutes, err := strconv.Atoi(options[ChannelMonitorSmartScheduleRealtimeRetentionOption])
	if err != nil || retentionMinutes < ChannelMonitorSmartScheduleMinRealtimeRetentionMinutes ||
		retentionMinutes > ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes {
		retentionMinutes = ChannelMonitorSmartScheduleDefaultRealtimeRetentionMinutes
	}
	sampleLimit, err := strconv.Atoi(options[ChannelMonitorSmartScheduleRealtimeSampleLimitOption])
	if err != nil || sampleLimit < ChannelMonitorSmartScheduleMinRealtimeSampleLimit ||
		sampleLimit > ChannelMonitorSmartScheduleMaxRealtimeSampleLimit {
		sampleLimit = ChannelMonitorSmartScheduleDefaultRealtimeSampleLimit
	}
	for _, key := range []string{ChannelMonitorSmartSchedulePerformanceWindowOption} {
		windowMinutes, parseErr := strconv.Atoi(options[key])
		if parseErr == nil && windowMinutes > retentionMinutes &&
			windowMinutes <= ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes {
			retentionMinutes = windowMinutes
		}
	}
	retentionMinutes = max(
		retentionMinutes,
		ChannelMonitorSmartScheduleMaxPolicyStabilityWindowMinutes(
			options[channelMonitorSmartScheduleGroupPoliciesOption],
		),
	)
	return ChannelMonitorSmartScheduleRealtimeSettings{
		RetentionMinutes: retentionMinutes,
		SampleLimit:      sampleLimit,
	}
}

func GetChannelMonitorSmartScheduleRealtimeSettings() ChannelMonitorSmartScheduleRealtimeSettings {
	common.OptionMapRWMutex.RLock()
	options := map[string]string{
		ChannelMonitorSmartScheduleRealtimeRetentionOption:   common.OptionMap[ChannelMonitorSmartScheduleRealtimeRetentionOption],
		ChannelMonitorSmartScheduleRealtimeSampleLimitOption: common.OptionMap[ChannelMonitorSmartScheduleRealtimeSampleLimitOption],
		ChannelMonitorSmartSchedulePerformanceWindowOption:   common.OptionMap[ChannelMonitorSmartSchedulePerformanceWindowOption],
		channelMonitorSmartScheduleGroupPoliciesOption:       common.OptionMap[channelMonitorSmartScheduleGroupPoliciesOption],
	}
	common.OptionMapRWMutex.RUnlock()
	return ChannelMonitorSmartScheduleRealtimeSettingsFromOptions(options)
}
