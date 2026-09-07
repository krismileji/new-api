package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type channelSmartScheduleTraffic struct {
	ChannelID              int              `json:"channel_id"`
	Group                  string           `json:"group"`
	Model                  string           `json:"model"`
	WindowStart            int64            `json:"window_start"`
	WindowEnd              int64            `json:"window_end"`
	CoverageStart          int64            `json:"coverage_start"`
	DataCutoffAt           int64            `json:"data_cutoff_at"`
	EventWatermark         uint64           `json:"event_watermark"`
	Available              bool             `json:"available"`
	Complete               bool             `json:"complete"`
	AttemptCount           int64            `json:"attempt_count"`
	FinalSuccessCount      int64            `json:"final_success_count"`
	RetryRequestCount      int64            `json:"retry_request_count"`
	RetryCountKnown        bool             `json:"retry_count_known"`
	SourceCounts           map[string]int64 `json:"source_counts"`
	LogicalMemberCount     int64            `json:"logical_member_count"`
	RateLimitFallbackCount int64            `json:"rate_limit_fallback_count"`
}

// Traffic uses all dispatched business events, before scheduler eligibility,
// observation resets or health-status filters. Shares are scoped to listed routes.
func channelSmartScheduleActualTraffic(route model.ChannelSmartScheduleRoute,
	window service.ChannelMonitorRedisRouteHealthWindow, windowStart, windowEnd, trafficStartedAt int64,
) channelSmartScheduleTraffic {
	coverage := max(window.Snapshot.CoverageStart, trafficStartedAt)
	result := channelSmartScheduleTraffic{
		ChannelID: route.ChannelId, Group: route.Group, Model: route.Model,
		WindowStart: windowStart, WindowEnd: windowEnd, CoverageStart: coverage,
		Available:    trafficStartedAt > 0,
		Complete:     trafficStartedAt > 0 && coverage <= windowStart && !window.Snapshot.SampleLimitTruncated,
		SourceCounts: make(map[string]int64), RetryCountKnown: true,
	}
	seenEvents := make(map[string]bool)
	successfulRequests := make(map[string]bool)
	retriedRequests := make(map[string]bool)
	for _, sample := range window.Samples {
		if sample.GroupName != route.Group || sample.Source != model.ChannelMonitorEventSourceBusiness ||
			!sample.RequestDispatched || sample.FinalRetrySummary || sample.OccurredAt < windowStart || sample.OccurredAt > windowEnd {
			continue
		}
		modelKnown := sample.Routing != nil && sample.Routing.Model != ""
		if modelKnown && sample.Routing.Source != "specific_channel" && sample.Routing.Source != "other" {
			if sample.Routing.Model != route.Model {
				continue
			}
		} else {
			modelKnown = sample.RequestModel == route.Model
			if sample.RequestModel != "" && !modelKnown && ratio_setting.FormatMatchingModelName(sample.RequestModel) != route.Model {
				continue
			}
		}
		if seenEvents[sample.EventID] {
			continue
		}
		seenEvents[sample.EventID] = true
		if !modelKnown || sample.RequestFingerprint == "" {
			result.Complete = false
		}
		result.Available = true
		result.AttemptCount++
		result.DataCutoffAt = max(result.DataCutoffAt, sample.OccurredAt)
		result.EventWatermark = max(result.EventWatermark, sample.EventSequence)
		requestID := sample.RequestFingerprint
		if requestID == "" {
			requestID = sample.EventID
		}
		if sample.Outcome == model.ChannelMonitorEventOutcomeSuccess && sample.IsFinalAttempt && !successfulRequests[requestID] {
			result.FinalSuccessCount++
			successfulRequests[requestID] = true
		}
		if sample.IsRetryAttempt {
			if sample.RequestFingerprint == "" {
				result.RetryCountKnown = false
			} else if !retriedRequests[requestID] {
				result.RetryRequestCount++
				retriedRequests[requestID] = true
			}
		}
		source := "unknown"
		if sample.Routing != nil {
			source = sample.Routing.Source
			if sample.Routing.LogicalChannelID > 0 {
				result.LogicalMemberCount++
			}
			if sample.Routing.RateLimitFallback {
				result.RateLimitFallbackCount++
			}
		}
		result.SourceCounts[source]++
	}
	return result
}
