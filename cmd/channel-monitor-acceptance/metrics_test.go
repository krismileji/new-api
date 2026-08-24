package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestCollectorReportsExactPercentilesAndErrors(t *testing.T) {
	collector := newRequestCollector(1000)
	latencies := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	for _, milliseconds := range latencies {
		status := 200
		if milliseconds == 20 {
			status = 503
		}
		collector.record("GET /fixture", time.Duration(milliseconds)*time.Millisecond, status, 10, nil)
	}

	summary := collector.summary(time.Second)
	assert.Equal(t, int64(20), summary.Requests)
	assert.Equal(t, int64(1), summary.Errors)
	assert.InDelta(t, 5, summary.ErrorRatePercent, 0.0001)
	assert.InDelta(t, 10, summary.P50Milliseconds, 0.0001)
	assert.InDelta(t, 19, summary.P95Milliseconds, 0.0001)
	assert.InDelta(t, 20, summary.P99Milliseconds, 0.0001)
	assert.InDelta(t, 20, summary.ThroughputPerSec, 0.0001)
	assert.Equal(t, int64(20), summary.RequestsByEndpoint["GET /fixture"])
}

func TestExtractMonitorMetricsAndDeltasPreservePoolRoles(t *testing.T) {
	before, err := extractMonitorMetrics([]byte(`{
  "success": true,
  "data": {
    "writer_dropped_events": 2,
    "writer_queue_depth": 3,
    "redis_pool_stats": {
      "monitor_read": {"hits": 10, "timeouts": 1},
      "user": {"hits": 20, "timeouts": 0}
    },
	"cost_ledger_failed_count": 2,
	"route_snapshot": {"revision": 7, "snapshot_age_seconds": 3},
    "channels": [{"id": 1}]
  }
}`))
	require.NoError(t, err)
	after, err := extractMonitorMetrics([]byte(`{
  "data": {
    "writer_dropped_events": 5,
    "writer_queue_depth": 1,
    "redis_pool_stats": {
      "monitor_read": {"hits": 14, "timeouts": 1},
      "user": {"hits": 30, "timeouts": 0}
    },
	"cost_ledger_failed_count": 5,
	"route_snapshot": {"revision": 8, "snapshot_age_seconds": 4}
  }
}`))
	require.NoError(t, err)

	assert.NotContains(t, before, "data.channels[0].id")
	assert.Equal(t, float64(10), before["data.redis_pool_stats.monitor_read.hits"])
	assert.Equal(t, float64(2), before["data.cost_ledger_failed_count"])
	assert.Equal(t, float64(7), before["data.route_snapshot.revision"])
	deltas := numericMetricDeltas(before, after)
	assert.Equal(t, float64(3), deltas["data.writer_dropped_events"])
	assert.Equal(t, float64(4), deltas["data.redis_pool_stats.monitor_read.hits"])
	assert.Equal(t, float64(10), deltas["data.redis_pool_stats.user.hits"])
	assert.Equal(t, float64(3), deltas["data.cost_ledger_failed_count"])
	assert.Equal(t, float64(1), deltas["data.route_snapshot.revision"])
}

func TestParsePrometheusMetricsFiltersConfiguredSeries(t *testing.T) {
	metrics := parsePrometheusMetrics([]byte(`# HELP pool_wait Redis waits
pool_wait{role="user"} 7
pool_wait{role="monitor_read"} 3
unrelated_total 99
`), []string{"pool_wait"})

	assert.Equal(t, map[string]float64{
		`pool_wait{role="user"}`:         7,
		`pool_wait{role="monitor_read"}`: 3,
	}, metrics)
}
