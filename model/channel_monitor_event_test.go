package model

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorEventRoundTripPreservesExplicitZeroMeasurements(t *testing.T) {
	zeroFloat := float64(0)
	zeroInt := int64(0)
	zeroStatus := 0
	event := ChannelMonitorEvent{
		EventId:           "event-round-trip",
		EventSequence:     42,
		SchemaVersion:     ChannelMonitorEventSchemaVersion,
		OccurredAt:        1_700_000_000,
		CreatedAt:         1_700_000_001,
		ChannelId:         7,
		Source:            ChannelMonitorEventSourceBusiness,
		Outcome:           ChannelMonitorEventOutcomeSuccess,
		CostStatus:        ChannelMonitorEventCostSettled,
		StatusCode:        &zeroStatus,
		FirstTokenMs:      &zeroFloat,
		TPS:               &zeroFloat,
		PromptTokens:      &zeroInt,
		CompletionTokens:  &zeroInt,
		CacheReadTokens:   &zeroInt,
		CacheWriteTokens:  &zeroInt,
		InputTokens:       &zeroInt,
		AttemptDurationMs: &zeroInt,
		OtherJson:         `{"billing_snapshot":{"version":1}}`,
	}

	data, err := event.Marshal()
	require.NoError(t, err)
	decoded, err := UnmarshalChannelMonitorEvent(data)
	require.NoError(t, err)

	assert.Equal(t, event, decoded)
	require.NotNil(t, decoded.FirstTokenMs)
	assert.Zero(t, *decoded.FirstTokenMs)
	require.NotNil(t, decoded.CacheReadTokens)
	assert.Zero(t, *decoded.CacheReadTokens)
}

func TestChannelMonitorEventTPSMeasurement(t *testing.T) {
	normalTokens := int64(33)
	normalTPS := 8.25
	zeroTPS := 0.0
	shortTokens := int64(1)
	shortTPS := 2000.0

	tests := []struct {
		name               string
		event              ChannelMonitorEvent
		wantTokens         int64
		wantGenerationTime int64
		wantOK             bool
	}{
		{
			name:               "normal measurement",
			event:              ChannelMonitorEvent{CompletionTokens: &normalTokens, TPS: &normalTPS},
			wantTokens:         33,
			wantGenerationTime: 4000,
			wantOK:             true,
		},
		{
			name:   "zero tps",
			event:  ChannelMonitorEvent{CompletionTokens: &normalTokens, TPS: &zeroTPS},
			wantOK: false,
		},
		{
			name:   "missing completion tokens",
			event:  ChannelMonitorEvent{TPS: &normalTPS},
			wantOK: false,
		},
		{
			name:               "sub-millisecond generation time",
			event:              ChannelMonitorEvent{CompletionTokens: &shortTokens, TPS: &shortTPS},
			wantTokens:         1,
			wantGenerationTime: 1,
			wantOK:             true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens, generationTime, ok := test.event.TPSMeasurement()

			assert.Equal(t, test.wantOK, ok)
			assert.Equal(t, test.wantTokens, tokens)
			assert.Equal(t, test.wantGenerationTime, generationTime)
		})
	}
}

func TestNewChannelMonitorEventCreatesValidIdentity(t *testing.T) {
	event := NewChannelMonitorEvent(9, ChannelMonitorEventSourceManualTest, ChannelMonitorEventOutcomeFailure, 1_700_000_000)

	require.NoError(t, event.Validate())
	assert.NotEmpty(t, event.EventId)
	assert.Equal(t, ChannelMonitorEventSchemaVersion, event.SchemaVersion)
	assert.Positive(t, event.CreatedAt)
	assert.Equal(t, 9, event.ChannelId)
}

func TestChannelMonitorEventValidationRejectsInvalidMeasurementsAndMetadata(t *testing.T) {
	valid := ChannelMonitorEvent{
		EventId:       "event-valid",
		SchemaVersion: ChannelMonitorEventSchemaVersion,
		OccurredAt:    1_700_000_000,
		CreatedAt:     1_700_000_001,
		ChannelId:     1,
		Source:        ChannelMonitorEventSourceBusiness,
		Outcome:       ChannelMonitorEventOutcomeSuccess,
		CostStatus:    ChannelMonitorEventCostNone,
	}
	negative := int64(-1)
	nan := math.NaN()

	tests := []struct {
		name   string
		mutate func(*ChannelMonitorEvent)
	}{
		{name: "missing event id", mutate: func(event *ChannelMonitorEvent) { event.EventId = "" }},
		{name: "unsupported schema", mutate: func(event *ChannelMonitorEvent) { event.SchemaVersion++ }},
		{name: "invalid source", mutate: func(event *ChannelMonitorEvent) { event.Source = "unknown" }},
		{name: "invalid outcome", mutate: func(event *ChannelMonitorEvent) { event.Outcome = "unknown" }},
		{name: "invalid cost status", mutate: func(event *ChannelMonitorEvent) { event.CostStatus = "unknown" }},
		{name: "negative tokens", mutate: func(event *ChannelMonitorEvent) { event.InputTokens = &negative }},
		{name: "nan tps", mutate: func(event *ChannelMonitorEvent) { event.TPS = &nan }},
		{name: "negative cost", mutate: func(event *ChannelMonitorEvent) { event.SettledCostNanoCNY = -1 }},
		{name: "cost without status", mutate: func(event *ChannelMonitorEvent) { event.SettledCostNanoCNY = 1 }},
		{name: "mixed settled cost", mutate: func(event *ChannelMonitorEvent) {
			event.CostStatus = ChannelMonitorEventCostSettled
			event.UnresolvedCostNanoCNY = 1
		}},
		{name: "invalid other json", mutate: func(event *ChannelMonitorEvent) { event.OtherJson = "{" }},
		{name: "oversized other json", mutate: func(event *ChannelMonitorEvent) {
			event.OtherJson = `"` + strings.Repeat("a", ChannelMonitorEventMaxOtherJsonBytes) + `"`
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := valid
			test.mutate(&event)
			assert.Error(t, event.Validate())
		})
	}
}

func TestChannelMonitorEventCostStatusDistinguishesZeroSettlementFromUnresolved(t *testing.T) {
	settled := NewChannelMonitorEvent(1, ChannelMonitorEventSourceBusiness, ChannelMonitorEventOutcomeSuccess, 1_700_000_000)
	settled.EventId = "event-zero-settlement"
	settled.CostStatus = ChannelMonitorEventCostSettled
	unresolved := NewChannelMonitorEvent(1, ChannelMonitorEventSourceBusiness, ChannelMonitorEventOutcomeUnresolved, 1_700_000_000)
	unresolved.EventId = "event-unresolved"
	unresolved.CostStatus = ChannelMonitorEventCostUnresolved

	require.NoError(t, settled.Validate())
	require.NoError(t, unresolved.Validate())
	assert.Zero(t, settled.SettledCostNanoCNY)
	assert.Zero(t, unresolved.UnresolvedCostNanoCNY)
	assert.NotEqual(t, settled.CostStatus, unresolved.CostStatus)
}

func TestChannelMonitorEventCloneFreezesOptionalMeasurements(t *testing.T) {
	firstTokenMs := 125.0
	inputTokens := int64(20)
	event := NewChannelMonitorEvent(1, ChannelMonitorEventSourceBusiness, ChannelMonitorEventOutcomeSuccess, 1_700_000_000)
	event.FirstTokenMs = &firstTokenMs
	event.InputTokens = &inputTokens

	cloned := event.Clone()
	firstTokenMs = 300
	inputTokens = 40

	require.NotNil(t, cloned.FirstTokenMs)
	assert.Equal(t, 125.0, *cloned.FirstTokenMs)
	require.NotNil(t, cloned.InputTokens)
	assert.Equal(t, int64(20), *cloned.InputTokens)
}
