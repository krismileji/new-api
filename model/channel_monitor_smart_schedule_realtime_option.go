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
	for _, key := range []string{
		ChannelMonitorSmartSchedulePerformanceWindowOption,
		ChannelMonitorSmartScheduleStabilityWindowOption,
	} {
		windowMinutes, parseErr := strconv.Atoi(options[key])
		if parseErr == nil && windowMinutes > retentionMinutes &&
			windowMinutes <= ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes {
			retentionMinutes = windowMinutes
		}
	}
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
		ChannelMonitorSmartScheduleStabilityWindowOption:     common.OptionMap[ChannelMonitorSmartScheduleStabilityWindowOption],
	}
	common.OptionMapRWMutex.RUnlock()
	return ChannelMonitorSmartScheduleRealtimeSettingsFromOptions(options)
}
