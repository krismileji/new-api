package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunAdminRefreshSendsExactlyOneRequestPerCurrentViewEndpoint(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "Bearer admin-fixture", request.Header.Get("Authorization"))
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	runner := acceptanceRunner{
		config: acceptanceConfig{
			baseURL:           baseURL,
			adminToken:        "admin-fixture",
			adminView:         "channels",
			maxLatencySamples: 1000,
		},
		client: server.Client(),
	}
	collector := newRequestCollector(1000)

	runner.runAdminRefresh(context.Background(), collector)

	summary := collector.summary(time.Second)
	expected := int64(len(adminViewEndpoints["channels"]))
	assert.Equal(t, expected, requests.Load())
	assert.Equal(t, expected, summary.Requests)
	assert.Zero(t, summary.Errors)
}

func TestFetchMonitorSnapshotParsesFixtureMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
  "data": {
    "event_watermark": 12,
    "writer_queue_depth": 4,
    "writer_dropped_events": 0
  }
}`))
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	runner := acceptanceRunner{
		config: acceptanceConfig{
			baseURL:     baseURL,
			metricsPath: "/api/channel_monitor/",
			adminToken:  "admin-fixture",
		},
		client: server.Client(),
	}

	metrics, err := runner.fetchMonitorSnapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, float64(12), metrics["data.event_watermark"])
	assert.Equal(t, float64(4), metrics["data.writer_queue_depth"])
}

func TestFetchMonitorSnapshotRejectsFailedSuccessEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("{\"success\":false,\"data\":{}}"))
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	runner := acceptanceRunner{
		config: acceptanceConfig{baseURL: baseURL, metricsPath: "/api/channel_monitor/", adminToken: "admin-fixture"},
		client: server.Client(),
	}
	_, err = runner.fetchMonitorSnapshot(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "success=false")
}

func TestEvaluateChecksFailsWhenRequiredMonitorMetricIsMissing(t *testing.T) {
	runner := acceptanceRunner{config: acceptanceConfig{
		maxUserErrorRatePercent:  -1,
		maxAdminErrorRatePercent: -1,
		maxWriterDroppedDelta:    0,
	}}
	checks := runner.evaluateChecks(scenarioReport{})
	var writerCheck acceptanceCheck
	for _, check := range checks {
		if check.Name == "writer_dropped_events_delta" {
			writerCheck = check
			break
		}
	}
	assert.False(t, writerCheck.Passed)
	assert.False(t, writerCheck.Skipped)
	assert.Equal(t, "missing", writerCheck.Actual)
}

func TestEvaluateChecksFailsWhenMetricCaptureFailed(t *testing.T) {
	runner := acceptanceRunner{config: acceptanceConfig{
		maxUserErrorRatePercent:  -1,
		maxAdminErrorRatePercent: -1,
		maxWriterDroppedDelta:    -1,
	}}
	checks := runner.evaluateChecks(scenarioReport{MetricCaptureErrors: []string{"前置监控快照: unavailable"}})
	var captureCheck acceptanceCheck
	for _, check := range checks {
		if check.Name == "metric_capture" {
			captureCheck = check
			break
		}
	}
	assert.False(t, captureCheck.Passed)
	assert.Equal(t, 1, captureCheck.Actual)
}

func TestDryRunReportDeclaresExternalEvidenceBoundary(t *testing.T) {
	config := acceptanceConfig{
		environment:          "test",
		scenarioLabel:        "normal",
		adminView:            "status-probe",
		userConcurrency:      []int{100, 500, 1000},
		adminUsers:           []int{10, 50},
		duration:             time.Second,
		adminRefreshInterval: time.Second,
		userMethod:           http.MethodPost,
		userPath:             "/v1/chat/completions",
	}

	report, err := runAcceptance(config)
	require.NoError(t, err)
	assert.True(t, report.DryRun)
	assert.Equal(t, "single_environment_load_scenario", report.ReportScope)
	assert.True(t, report.Config.RequiredMatrixShape)
	assert.Contains(t, report.DoesNotProve, "告警平台真实投递")
	assert.Contains(t, report.DoesNotProve, "跨实例接管与 fencing")
	assert.Contains(t, report.DoesNotProve, "账务零差异对账")
	assert.Contains(t, report.DoesNotProve, "一次性全量回滚演练")
	assert.Contains(t, report.DoesNotProve,
		"默认仅采集 metrics-path 总览；状态、模型检测和路由专用 snapshot 需将 metrics-path 显式指向对应端点并分别保存报告",
	)
}

func TestAcceptanceHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirected" {
			redirected.Add(1)
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(writer, request, "/redirected", http.StatusFound)
	}))
	defer server.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := newAcceptanceHTTPClient(transport, time.Second)
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusFound, response.StatusCode)
	assert.Zero(t, redirected.Load())
}
