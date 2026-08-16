package model

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelMonitorSmartScheduleRealtimeSettingsFromOptions(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]string
		want    ChannelMonitorSmartScheduleRealtimeSettings
	}{
		{
			name: "missing values use defaults",
			want: ChannelMonitorSmartScheduleRealtimeSettings{
				RetentionMinutes: ChannelMonitorSmartScheduleDefaultRealtimeRetentionMinutes,
				SampleLimit:      ChannelMonitorSmartScheduleDefaultRealtimeSampleLimit,
			},
		},
		{
			name: "valid values are preserved",
			options: map[string]string{
				ChannelMonitorSmartScheduleRealtimeRetentionOption:   "180",
				ChannelMonitorSmartScheduleRealtimeSampleLimitOption: "50000",
			},
			want: ChannelMonitorSmartScheduleRealtimeSettings{RetentionMinutes: 180, SampleLimit: 50000},
		},
		{
			name: "retention covers configured windows",
			options: map[string]string{
				ChannelMonitorSmartScheduleRealtimeRetentionOption: "60",
				ChannelMonitorSmartSchedulePerformanceWindowOption: "120",
				ChannelMonitorSmartScheduleStabilityWindowOption:   "90",
			},
			want: ChannelMonitorSmartScheduleRealtimeSettings{
				RetentionMinutes: 120,
				SampleLimit:      ChannelMonitorSmartScheduleDefaultRealtimeSampleLimit,
			},
		},
		{
			name: "out of range values use defaults",
			options: map[string]string{
				ChannelMonitorSmartScheduleRealtimeRetentionOption:   strconv.Itoa(ChannelMonitorSmartScheduleMaxRealtimeRetentionMinutes + 1),
				ChannelMonitorSmartScheduleRealtimeSampleLimitOption: strconv.Itoa(ChannelMonitorSmartScheduleMaxRealtimeSampleLimit + 1),
			},
			want: ChannelMonitorSmartScheduleRealtimeSettings{
				RetentionMinutes: ChannelMonitorSmartScheduleDefaultRealtimeRetentionMinutes,
				SampleLimit:      ChannelMonitorSmartScheduleDefaultRealtimeSampleLimit,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, ChannelMonitorSmartScheduleRealtimeSettingsFromOptions(test.options))
		})
	}
}
