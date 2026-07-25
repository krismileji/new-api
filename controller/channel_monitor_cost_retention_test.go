package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
)

func TestChannelMonitorCostRetentionSettingsUsePersistedDays(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "missing uses default", want: defaultChannelMonitorCostRetentionDays},
		{name: "valid value", raw: "365", want: 365},
		{name: "below minimum uses default", raw: "0", want: defaultChannelMonitorCostRetentionDays},
		{name: "above maximum uses default", raw: "3651", want: defaultChannelMonitorCostRetentionDays},
		{name: "invalid value uses default", raw: "invalid", want: defaultChannelMonitorCostRetentionDays},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := map[string]string{}
			if test.raw != "" {
				values[channelMonitorCostRetentionDaysOption] = test.raw
			}
			useChannelMonitorOptionMap(t, values)

			assert.Equal(t, test.want, getChannelMonitorSettings().CostRetentionDays)
		})
	}
}

func TestChannelMonitorCostRetentionCutoffKeepsExactBeijingCalendarDays(t *testing.T) {
	now := time.Date(2026, 7, 25, 7, 30, 0, 0, time.UTC).Unix()
	todayStart := model.ChannelDailyCostDayStart(now)

	assert.Equal(t, todayStart, channelMonitorCostRetentionCutoff(now, 1))
	assert.Equal(
		t,
		todayStart-int64(defaultChannelMonitorCostRetentionDays-1)*channelMonitorCostDaySeconds,
		channelMonitorCostRetentionCutoff(now, defaultChannelMonitorCostRetentionDays),
	)
}
