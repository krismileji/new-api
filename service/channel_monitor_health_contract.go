package service

import "sort"

// ChannelMonitorHealthStatus is the operator-facing state of the monitoring
// pipeline. The state describes observability coverage and never changes the
// request or billing result.
type ChannelMonitorHealthStatus string

const (
	ChannelMonitorHealthHealthy     ChannelMonitorHealthStatus = "healthy"
	ChannelMonitorHealthDegraded    ChannelMonitorHealthStatus = "degraded"
	ChannelMonitorHealthUnavailable ChannelMonitorHealthStatus = "unavailable"
)

// ChannelMonitorCoverageStatus describes whether the returned window is known
// to be complete. Missing data must remain partial or unavailable rather than
// being represented as zero-valued metrics.
type ChannelMonitorCoverageStatus string

const (
	ChannelMonitorCoverageComplete    ChannelMonitorCoverageStatus = "complete"
	ChannelMonitorCoveragePartial     ChannelMonitorCoverageStatus = "partial"
	ChannelMonitorCoverageUnavailable ChannelMonitorCoverageStatus = "unavailable"
)

type ChannelMonitorMonitoringHealth struct {
	Status             ChannelMonitorHealthStatus `json:"status"`
	DegradedReasons    []string                   `json:"degraded_reasons"`
	FirstDegradedAt    int64                      `json:"first_degraded_at"`
	LastChangedAt      int64                      `json:"last_changed_at"`
	DroppedSampleCount int64                      `json:"dropped_sample_count"`
	PendingCount       int64                      `json:"pending_count"`
	ConsumerLagSeconds int64                      `json:"consumer_lag_seconds"`
}

type ChannelMonitorCoverage struct {
	Status         ChannelMonitorCoverageStatus `json:"status"`
	CoveredFrom    int64                        `json:"covered_from"`
	CoveredThrough int64                        `json:"covered_through"`
	Reasons        []string                     `json:"reasons"`
}

// ChannelMonitorHealthInput contains the small set of runtime observations
// needed to derive the stable health contract. It intentionally has no Redis
// or database dependency so each producer can use the same state rules.
type ChannelMonitorHealthInput struct {
	Now                int64
	RedisAvailable     bool
	ConsumerRunning    bool
	DegradedReasons    []string
	DroppedSampleCount int64
	PendingCount       int64
	ConsumerLagSeconds int64
}

type ChannelMonitorHealthState struct {
	Status          ChannelMonitorHealthStatus
	FirstDegradedAt int64
	LastChangedAt   int64
	Reasons         []string
}

func DeriveChannelMonitorMonitoringHealth(
	input ChannelMonitorHealthInput,
	previous ChannelMonitorHealthState,
) (ChannelMonitorMonitoringHealth, ChannelMonitorHealthState) {
	reasons := append([]string(nil), input.DegradedReasons...)
	if !input.RedisAvailable {
		reasons = appendUniqueHealthReason(reasons, ChannelMonitorRedisDegradedReasonRedisUnavailable)
	} else if !input.ConsumerRunning {
		reasons = appendUniqueHealthReason(reasons, ChannelMonitorRedisDegradedReasonConsumerStopped)
	}
	if input.DroppedSampleCount > 0 {
		reasons = appendUniqueHealthReason(reasons, "samples_dropped")
	}
	if input.PendingCount > 0 || input.ConsumerLagSeconds > 0 {
		reasons = appendUniqueHealthReason(reasons, ChannelMonitorRedisDegradedReasonEventBacklog)
	}
	sort.Strings(reasons)

	status := ChannelMonitorHealthHealthy
	if !input.RedisAvailable {
		status = ChannelMonitorHealthUnavailable
	} else if len(reasons) > 0 {
		status = ChannelMonitorHealthDegraded
	}

	state := ChannelMonitorHealthState{
		Status:          status,
		FirstDegradedAt: previous.FirstDegradedAt,
		LastChangedAt:   previous.LastChangedAt,
		Reasons:         append([]string(nil), reasons...),
	}
	if status == ChannelMonitorHealthDegraded || status == ChannelMonitorHealthUnavailable {
		if previous.Status != ChannelMonitorHealthDegraded && previous.Status != ChannelMonitorHealthUnavailable {
			state.FirstDegradedAt = input.Now
		}
	} else {
		state.FirstDegradedAt = 0
	}
	if previous.Status != status || !sameHealthReasons(previous.Reasons, reasons) {
		state.LastChangedAt = input.Now
	}

	health := ChannelMonitorMonitoringHealth{
		Status:             status,
		DegradedReasons:    reasons,
		FirstDegradedAt:    state.FirstDegradedAt,
		LastChangedAt:      state.LastChangedAt,
		DroppedSampleCount: maxInt64(input.DroppedSampleCount),
		PendingCount:       maxInt64(input.PendingCount),
		ConsumerLagSeconds: maxInt64(input.ConsumerLagSeconds),
	}
	return health, state
}

func DeriveChannelMonitorCoverage(
	available bool,
	requestedFrom int64,
	requestedThrough int64,
	coveredFrom int64,
	coveredThrough int64,
	reasons []string,
) ChannelMonitorCoverage {
	coverageReasons := append([]string(nil), reasons...)
	if !available {
		coverageReasons = appendUniqueHealthReason(coverageReasons, "data_source_unavailable")
	}
	if available && coveredThrough < requestedThrough {
		coverageReasons = appendUniqueHealthReason(coverageReasons, "processing_lag")
	}
	if coveredFrom > requestedFrom && requestedFrom > 0 {
		coverageReasons = appendUniqueHealthReason(coverageReasons, "window_start_uncovered")
	}
	sort.Strings(coverageReasons)

	status := ChannelMonitorCoverageComplete
	if !available {
		status = ChannelMonitorCoverageUnavailable
	} else if len(coverageReasons) > 0 {
		status = ChannelMonitorCoveragePartial
	}
	return ChannelMonitorCoverage{
		Status:         status,
		CoveredFrom:    coveredFrom,
		CoveredThrough: coveredThrough,
		Reasons:        coverageReasons,
	}
}

func appendUniqueHealthReason(reasons []string, reason string) []string {
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func sameHealthReasons(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func maxInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
