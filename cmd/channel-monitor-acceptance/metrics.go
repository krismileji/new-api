package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type requestCollector struct {
	mu             sync.Mutex
	total          int64
	errors         int64
	bytes          int64
	latencies      []time.Duration
	maxSamples     int
	randomState    uint64
	statusCodes    map[int]int64
	endpointCounts map[string]int64
}

func newRequestCollector(maxSamples int) *requestCollector {
	return &requestCollector{
		maxSamples:     maxSamples,
		randomState:    0x9e3779b97f4a7c15,
		statusCodes:    make(map[int]int64),
		endpointCounts: make(map[string]int64),
	}
}

func (collector *requestCollector) record(endpoint string, latency time.Duration, statusCode int, responseBytes int64, requestErr error) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.total++
	collector.bytes += responseBytes
	collector.endpointCounts[endpoint]++
	if requestErr != nil || statusCode < 200 || statusCode >= 300 {
		collector.errors++
	}
	if statusCode > 0 {
		collector.statusCodes[statusCode]++
	}
	if len(collector.latencies) < collector.maxSamples {
		collector.latencies = append(collector.latencies, latency)
		return
	}
	collector.randomState ^= collector.randomState << 13
	collector.randomState ^= collector.randomState >> 7
	collector.randomState ^= collector.randomState << 17
	index := collector.randomState % uint64(collector.total)
	if index < uint64(collector.maxSamples) {
		collector.latencies[index] = latency
	}
}

type requestSummary struct {
	Requests           int64            `json:"requests"`
	Errors             int64            `json:"errors"`
	ErrorRatePercent   float64          `json:"error_rate_percent"`
	ThroughputPerSec   float64          `json:"throughput_per_second"`
	ResponseBytes      int64            `json:"response_bytes"`
	LatencySamples     int              `json:"latency_samples"`
	LatencySampled     bool             `json:"latency_sampled"`
	P50Milliseconds    float64          `json:"p50_ms"`
	P95Milliseconds    float64          `json:"p95_ms"`
	P99Milliseconds    float64          `json:"p99_ms"`
	MaxMilliseconds    float64          `json:"max_ms"`
	StatusCodes        map[string]int64 `json:"status_codes"`
	RequestsByEndpoint map[string]int64 `json:"requests_by_endpoint"`
}

func (collector *requestCollector) summary(elapsed time.Duration) requestSummary {
	collector.mu.Lock()
	latencies := append([]time.Duration(nil), collector.latencies...)
	total := collector.total
	errors := collector.errors
	responseBytes := collector.bytes
	statusCodes := make(map[string]int64, len(collector.statusCodes))
	for code, count := range collector.statusCodes {
		statusCodes[strconv.Itoa(code)] = count
	}
	endpointCounts := make(map[string]int64, len(collector.endpointCounts))
	for endpoint, count := range collector.endpointCounts {
		endpointCounts[endpoint] = count
	}
	collector.mu.Unlock()

	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	summary := requestSummary{
		Requests:           total,
		Errors:             errors,
		ResponseBytes:      responseBytes,
		LatencySamples:     len(latencies),
		LatencySampled:     int64(len(latencies)) < total,
		StatusCodes:        statusCodes,
		RequestsByEndpoint: endpointCounts,
	}
	if total > 0 {
		summary.ErrorRatePercent = float64(errors) * 100 / float64(total)
	}
	if elapsed > 0 {
		summary.ThroughputPerSec = float64(total) / elapsed.Seconds()
	}
	if len(latencies) > 0 {
		summary.P50Milliseconds = durationMilliseconds(percentile(latencies, 0.50))
		summary.P95Milliseconds = durationMilliseconds(percentile(latencies, 0.95))
		summary.P99Milliseconds = durationMilliseconds(percentile(latencies, 0.99))
		summary.MaxMilliseconds = durationMilliseconds(latencies[len(latencies)-1])
	}
	return summary
}

func percentile(sorted []time.Duration, quantile float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(quantile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

var monitorMetricKeys = map[string]struct{}{
	"generated_at": {}, "data_cutoff_at": {}, "processed_at": {}, "event_watermark": {},
	"snapshot_version": {}, "snapshot_revision": {}, "snapshot_age_seconds": {}, "stale": {},
	"route_snapshot": {},
	"queue_depth":    {}, "pending_count": {}, "consumer_lag_seconds": {},
	"realtime_degraded": {}, "redis_status": {}, "redis_available": {},
	"redis_consumer_running": {}, "degraded_reasons": {},
	"writer_queue_depth": {}, "writer_queue_capacity": {},
	"writer_queued_events": {}, "writer_dropped_events": {},
	"writer_retry_events": {}, "writer_oldest_queued_at": {},
	"writer_queue_age_seconds": {}, "oldest_pending_at": {},
	"last_published_at": {}, "last_processed_at": {}, "retry_count": {},
	"takeover_count": {}, "quarantine_count": {}, "last_quarantined_at": {},
	"runtime_marker_failure_count": {}, "schedule_marker_failure_count": {},
	"marker_release_failure_count": {}, "marker_release_failure_active": {},
	"stream_trim_failure_count": {}, "stream_trim_failure_active": {},
	"cost_stream_pending_count": {}, "cost_stream_unread_count": {},
	"cost_outbox_pending_count": {}, "cost_outbox_oldest_pending_at": {},
	"cost_outbox_retry_count": {}, "cost_publish_failed_count": {},
	"cost_dead_letter_count": {}, "cost_ledger_failed_count": {},
	"redis_pool_stats": {},
}

func extractMonitorMetrics(body []byte) (map[string]any, error) {
	var value any
	if err := common.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	result := make(map[string]any)
	collectMonitorMetrics(result, "", value, false)
	return result, nil
}

func collectMonitorMetrics(result map[string]any, path string, value any, includeAll bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			_, selected := monitorMetricKeys[key]
			if selected {
				switch child.(type) {
				case map[string]any, []any:
					collectMonitorMetrics(result, childPath, child, true)
				default:
					result[childPath] = child
				}
				continue
			}
			collectMonitorMetrics(result, childPath, child, includeAll)
		}
	case []any:
		if includeAll {
			result[path] = typed
			return
		}
		for index, child := range typed {
			collectMonitorMetrics(result, fmt.Sprintf("%s[%d]", path, index), child, false)
		}
	default:
		if includeAll {
			result[path] = typed
		}
	}
}

func numericMetricDeltas(before, after map[string]any) map[string]float64 {
	deltas := make(map[string]float64)
	for path, afterValue := range after {
		afterNumber, afterOK := numericValue(afterValue)
		beforeNumber, beforeOK := numericValue(before[path])
		if afterOK && beforeOK {
			deltas[path] = afterNumber - beforeNumber
		}
	}
	return deltas
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func parsePrometheusMetrics(body []byte, allowedNames []string) map[string]float64 {
	allowed := make(map[string]struct{}, len(allowedNames))
	for _, name := range allowedNames {
		allowed[name] = struct{}{}
	}
	result := make(map[string]float64)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		series := fields[0]
		name := series
		if index := strings.IndexByte(name, '{'); index >= 0 {
			name = name[:index]
		}
		if len(allowed) > 0 {
			if _, ok := allowed[name]; !ok {
				continue
			}
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
			result[series] = value
		}
	}
	return result
}
