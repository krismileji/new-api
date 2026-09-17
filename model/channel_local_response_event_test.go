package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelSmallInputResponseEventRejectsUpstreamEffects(t *testing.T) {
	base := NewChannelMonitorEvent(7, ChannelMonitorEventSourceLocalResponse, ChannelMonitorEventOutcomeSuccess, 1800000000)
	require.NoError(t, base.Validate())
	for _, tc := range []struct {
		name   string
		mutate func(*ChannelMonitorEvent)
	}{
		{"dispatch", func(e *ChannelMonitorEvent) { e.RequestDispatched = true }},
		{"scheduling", func(e *ChannelMonitorEvent) { e.SchedulingEligible = true }},
		{"health", func(e *ChannelMonitorEvent) { e.RuntimeProtectionEligible = true }},
		{"settled cost", func(e *ChannelMonitorEvent) { e.CostStatus = ChannelMonitorEventCostSettled; e.SettledCostNanoCNY = 1 }},
		{"unresolved cost", func(e *ChannelMonitorEvent) {
			e.CostStatus = ChannelMonitorEventCostUnresolved
			e.UnresolvedCostNanoCNY = 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { event := base.Clone(); tc.mutate(&event); require.Error(t, event.Validate()) })
	}
}
