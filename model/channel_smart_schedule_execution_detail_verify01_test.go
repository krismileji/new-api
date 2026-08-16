package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type verify01SnapshotAdjustment struct {
	ChannelId                 int                               `json:"channel_id"`
	ChannelName               string                            `json:"channel_name"`
	Group                     string                            `json:"group"`
	Model                     string                            `json:"model"`
	Action                    string                            `json:"action"`
	OldPriority               int64                             `json:"old_priority"`
	NewPriority               int64                             `json:"new_priority"`
	OldWeight                 uint                              `json:"old_weight"`
	NewWeight                 uint                              `json:"new_weight"`
	Score                     *float64                          `json:"score,omitempty"`
	ScoreDetails              *ChannelSmartScheduleScoreDetails `json:"score_details,omitempty"`
	Reason                    string                            `json:"reason"`
	PreviousEffectiveTime     int64                             `json:"previous_effective_time,omitempty"`
	PreviousEffectivePriority int64                             `json:"previous_effective_priority"`
	PreviousEffectiveWeight   uint                              `json:"previous_effective_weight"`
}

var (
	verify01SnapshotRowSink      *ChannelSmartScheduleExecutionDetail
	verify01SnapshotPayloadsSink []json.RawMessage
)

func TestVerify01SnapshotCapacityAndExactRoundTrip(t *testing.T) {
	requireVerify01Enabled(t)
	for _, adjustmentCount := range []int{120, 500, 1_000, 5_000} {
		t.Run(fmt.Sprintf("adjustments_%d", adjustmentCount), func(t *testing.T) {
			inputs, expectedJSON := verify01SnapshotInputs(t, adjustmentCount)
			capacityTaskID := fmt.Sprintf("verify01-capacity-%d", adjustmentCount)
			startedAt := time.Now()
			row, err := channelSmartScheduleExecutionDetailSnapshot(
				capacityTaskID,
				inputs,
				1,
			)
			require.NoError(t, err)
			payloads, err := channelSmartScheduleExecutionDetailPayloads(*row)
			require.NoError(t, err)
			roundTripDuration := time.Since(startedAt)

			actualJSON, err := common.Marshal(payloads)
			require.NoError(t, err)
			assert.True(t, bytes.Equal(expectedJSON, actualJSON), "snapshot JSON must round-trip byte-for-byte")
			assert.Equal(t, adjustmentCount, row.ItemCount)

			allocsTaskID := fmt.Sprintf("verify01-allocs-%d", adjustmentCount)
			allocsPerRoundTrip := testing.AllocsPerRun(3, func() {
				measuredRow, measuredErr := channelSmartScheduleExecutionDetailSnapshot(
					allocsTaskID,
					inputs,
					1,
				)
				if measuredErr != nil {
					panic(measuredErr)
				}
				measuredPayloads, measuredErr := channelSmartScheduleExecutionDetailPayloads(*measuredRow)
				if measuredErr != nil {
					panic(measuredErr)
				}
				verify01SnapshotRowSink = measuredRow
				verify01SnapshotPayloadsSink = measuredPayloads
			})

			peakTaskID := fmt.Sprintf("verify01-peak-%d", adjustmentCount)
			peakHeapDelta := verify01SnapshotPeakHeapDelta(func() {
				measuredRow, measuredErr := channelSmartScheduleExecutionDetailSnapshot(
					peakTaskID,
					inputs,
					1,
				)
				if measuredErr != nil {
					panic(measuredErr)
				}
				measuredPayloads, measuredErr := channelSmartScheduleExecutionDetailPayloads(*measuredRow)
				if measuredErr != nil {
					panic(measuredErr)
				}
				verify01SnapshotRowSink = measuredRow
				verify01SnapshotPayloadsSink = measuredPayloads
			})

			t.Logf(
				"VERIFY01_CAPACITY adjustments=%d raw_bytes=%d gzip_bytes=%d gzip_ratio=%.4f roundtrip=%s allocs_per_op=%.0f peak_heap_delta_bytes=%d",
				adjustmentCount,
				len(expectedJSON),
				len(row.PayloadBlob),
				float64(len(row.PayloadBlob))/float64(len(expectedJSON)),
				roundTripDuration,
				allocsPerRoundTrip,
				peakHeapDelta,
			)
		})
	}
}

func requireVerify01Enabled(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("CHANNEL_MONITOR_VERIFY01")) != "1" {
		t.Skip("CHANNEL_MONITOR_VERIFY01=1 is required for the VERIFY-01 capacity and database matrix")
	}
}

func BenchmarkVerify01SnapshotRoundTrip(b *testing.B) {
	for _, adjustmentCount := range []int{120, 500, 1_000, 5_000} {
		inputs, expectedJSON := verify01SnapshotInputs(b, adjustmentCount)
		benchmarkTaskID := fmt.Sprintf("verify01-benchmark-%d", adjustmentCount)
		referenceRow, err := channelSmartScheduleExecutionDetailSnapshot(
			benchmarkTaskID,
			inputs,
			1,
		)
		require.NoError(b, err)

		b.Run(fmt.Sprintf("adjustments_%d", adjustmentCount), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				row, snapshotErr := channelSmartScheduleExecutionDetailSnapshot(
					benchmarkTaskID,
					inputs,
					1,
				)
				if snapshotErr != nil {
					b.Fatal(snapshotErr)
				}
				payloads, decodeErr := channelSmartScheduleExecutionDetailPayloads(*row)
				if decodeErr != nil {
					b.Fatal(decodeErr)
				}
				verify01SnapshotRowSink = row
				verify01SnapshotPayloadsSink = payloads
			}
			b.ReportMetric(float64(adjustmentCount), "adjustments/op")
			b.ReportMetric(float64(len(expectedJSON)), "raw-B/op")
			b.ReportMetric(float64(len(referenceRow.PayloadBlob)), "gzip-B/op")
			b.ReportMetric(float64(len(referenceRow.PayloadBlob))/float64(len(expectedJSON)), "gzip/raw")
		})
	}
}

func verify01SnapshotInputs(tb testing.TB, adjustmentCount int) ([]ChannelSmartScheduleExecutionDetailInput, []byte) {
	tb.Helper()
	inputs := make([]ChannelSmartScheduleExecutionDetailInput, 0, adjustmentCount)
	payloads := make([]json.RawMessage, 0, adjustmentCount)
	groups := []string{"default", "vip", "internal", "batch"}
	models := []string{"gpt-5", "gpt-5-mini", "claude-sonnet-4", "gemini-2.5-pro", "deepseek-v3"}
	actions := []string{"updated", "unchanged", "protected"}
	reasons := []string{
		"评分最高，调整为主渠道；样本、稳定性和经济性均满足策略阈值",
		"评分变化未达到切换阈值，保留当前优先级和流量权重",
		"健康压力仍在保护窗口内，本轮保留备用流量并等待下一次完整调度",
	}
	for index := 0; index < adjustmentCount; index++ {
		score := 0.55 + float64(index%400)/1_000
		costRatio := 0.8 + float64(index%50)/100
		groupRatio := 1.0 + float64(index%4)/10
		grossMargin := 0.35 + float64(index%20)/100
		firstTokenMs := 350.0 + float64(index%700)
		tps := 40.0 + float64(index%120)
		stability := 0.90 + float64(index%90)/1_000
		minimumCostRatio := 0.75
		maximumCostRatio := 1.50
		minimumFirstTokenMs := 250.0
		maximumFirstTokenMs := 1_800.0
		minimumTPS := 20.0
		maximumTPS := 180.0
		priority := int64(100 - index%5)
		adjustment := verify01SnapshotAdjustment{
			ChannelId:                 10_000 + index,
			ChannelName:               fmt.Sprintf("脱敏渠道-%05d", index),
			Group:                     groups[index%len(groups)],
			Model:                     models[index%len(models)],
			Action:                    actions[index%len(actions)],
			OldPriority:               priority - 1,
			NewPriority:               priority,
			OldWeight:                 uint(100 + index%50),
			NewWeight:                 uint(150 + index%80),
			Score:                     &score,
			Reason:                    reasons[index%len(reasons)],
			PreviousEffectiveTime:     1_754_900_000 + int64(index),
			PreviousEffectivePriority: priority - 1,
			PreviousEffectiveWeight:   uint(100 + index%50),
			ScoreDetails: &ChannelSmartScheduleScoreDetails{
				Version:               ChannelSmartScheduleScoreDetailsVersion,
				WindowStart:           1_754_896_400,
				WindowEnd:             1_754_900_000,
				DataCutoffAt:          1_754_899_995,
				EventWatermark:        uint64(9_000_000 + index),
				Strategy:              "smart",
				MinSamples:            20,
				MinComparableChannels: 2,
				ComparisonState:       ChannelSmartScheduleComparisonComparable,
				SampleScope:           "group_model",
				SampleGroupCount:      4,
				Economics: &ChannelSmartScheduleEconomicsDetails{
					CostRatio:    &costRatio,
					GroupRatio:   &groupRatio,
					GrossMargin:  &grossMargin,
					EconomicRole: "balanced",
				},
				Inputs: ChannelSmartScheduleScoreInputs{
					CostRatio:    ChannelSmartScheduleScoreInput{Value: &costRatio, SampleCount: 360},
					FirstTokenMs: ChannelSmartScheduleScoreInput{Value: &firstTokenMs, SampleCount: 340},
					TPS:          ChannelSmartScheduleScoreInput{Value: &tps, SampleCount: 330},
					Stability:    ChannelSmartScheduleScoreInput{Value: &stability, SampleCount: 360},
				},
				Cohort: ChannelSmartScheduleScoreCohort{
					Priority:     &priority,
					CostRatio:    ChannelSmartScheduleScoreRange{Minimum: &minimumCostRatio, Maximum: &maximumCostRatio, AvailableCount: 8},
					FirstTokenMs: ChannelSmartScheduleScoreRange{Minimum: &minimumFirstTokenMs, Maximum: &maximumFirstTokenMs, AvailableCount: 8},
					TPS:          ChannelSmartScheduleScoreRange{Minimum: &minimumTPS, Maximum: &maximumTPS, AvailableCount: 8},
				},
				Components: ChannelSmartScheduleScoreComponents{
					CostRatio:    ChannelSmartScheduleScoreComponent{Available: true, ComparisonState: ChannelSmartScheduleComparisonComparable, RawValue: &costRatio, NormalizedScore: &score, ConfiguredWeightPercent: 30, EffectiveWeightPercent: 30},
					FirstTokenMs: ChannelSmartScheduleScoreComponent{Available: true, ComparisonState: ChannelSmartScheduleComparisonComparable, RawValue: &firstTokenMs, NormalizedScore: &score, ConfiguredWeightPercent: 25, EffectiveWeightPercent: 25},
					TPS:          ChannelSmartScheduleScoreComponent{Available: true, ComparisonState: ChannelSmartScheduleComparisonComparable, RawValue: &tps, NormalizedScore: &score, ConfiguredWeightPercent: 25, EffectiveWeightPercent: 25},
				},
				BusinessScore: &score,
				Stability: ChannelSmartScheduleStabilityScoreDetails{
					Enabled: true, Available: true, Applied: true, RawScore: &stability,
					ConfiguredWeightPercent: 20, EffectiveWeightPercent: 20,
					BusinessContribution: score * 0.8, Contribution: stability * 0.2,
				},
				Health: ChannelSmartScheduleHealthDetails{
					State: "healthy", Evidence: true, Pressure: 0.08, ErrorPressure: 0.02,
					LatencyPressure: 0.06, SampleCount: 360, WindowMinutes: 60, WindowRequests: 360,
					ErrorRequestPercent: 0.5, RiskRequestPercent: 1.2,
					FirstTokenWarningRequestPercent: 2.3, HealthyRequestPercent: 96.0,
				},
				FinalScore: &score,
				Decision: ChannelSmartScheduleScoreDecision{
					ApplyMode: "full", CurrentPrimaryChannelId: 10_000, RawWinnerChannelId: 10_000 + index,
					SelectedPrimaryChannelId: 10_000 + index, ActualPrimaryChannelId: 10_000 + index,
					SelectedPrimary: index%8 == 0, BaseRank: index + 1, BasePriority: priority,
					BaseWeight: uint(100 + index%50), AppliedPriority: priority, AppliedWeight: uint(150 + index%80),
					ActualHighestPriority: priority, ActualTopLayerChannelIds: []int{10_000 + index, 20_000 + index},
					TemporaryTrafficKind: "none", TemporaryTrafficTargetPercent: 0,
					PrimarySwitchThresholdPercent: 5, PrimaryTrafficPercent: 80,
					SelectionReason: reasons[index%len(reasons)], AdjustmentReason: reasons[(index+1)%len(reasons)],
					Reason: reasons[(index+2)%len(reasons)],
				},
			},
		}
		encoded, err := common.Marshal(adjustment)
		require.NoError(tb, err)
		inputs = append(inputs, ChannelSmartScheduleExecutionDetailInput{
			AdjustmentIndex: index,
			Payload:         adjustment,
		})
		payloads = append(payloads, json.RawMessage(encoded))
	}
	expectedJSON, err := common.Marshal(payloads)
	require.NoError(tb, err)
	return inputs, expectedJSON
}

func verify01SnapshotPeakHeapDelta(operation func()) uint64 {
	verify01SnapshotRowSink = nil
	verify01SnapshotPayloadsSink = nil
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	stop := make(chan struct{})
	peak := make(chan uint64, 1)
	go func() {
		ticker := time.NewTicker(100 * time.Microsecond)
		defer ticker.Stop()
		maximum := baseline.HeapAlloc
		for {
			select {
			case <-ticker.C:
				var current runtime.MemStats
				runtime.ReadMemStats(&current)
				if current.HeapAlloc > maximum {
					maximum = current.HeapAlloc
				}
			case <-stop:
				var current runtime.MemStats
				runtime.ReadMemStats(&current)
				if current.HeapAlloc > maximum {
					maximum = current.HeapAlloc
				}
				peak <- maximum
				return
			}
		}
	}()
	operation()
	close(stop)
	maximum := <-peak
	verify01SnapshotRowSink = nil
	verify01SnapshotPayloadsSink = nil
	if maximum <= baseline.HeapAlloc {
		return 0
	}
	return maximum - baseline.HeapAlloc
}
