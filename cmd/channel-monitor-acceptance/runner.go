package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type acceptanceReport struct {
	GeneratedAt  string           `json:"generated_at"`
	DryRun       bool             `json:"dry_run"`
	ReportScope  string           `json:"report_scope"`
	DoesNotProve []string         `json:"does_not_prove"`
	Config       reportConfig     `json:"config"`
	Plans        []scenarioPlan   `json:"plans"`
	Scenarios    []scenarioReport `json:"scenarios,omitempty"`
	FailedChecks int              `json:"failed_checks"`
}

type scenarioReport struct {
	Plan                    scenarioPlan       `json:"plan"`
	StartedAt               string             `json:"started_at"`
	ElapsedSeconds          float64            `json:"elapsed_seconds"`
	UserRequests            requestSummary     `json:"user_requests"`
	AdminRequests           requestSummary     `json:"admin_requests"`
	AdminRefreshes          int64              `json:"admin_refreshes"`
	ExpectedAdminRequests   int64              `json:"expected_admin_requests"`
	FanoutMismatch          int64              `json:"fanout_mismatch"`
	MonitorBefore           map[string]any     `json:"monitor_before,omitempty"`
	MonitorAfter            map[string]any     `json:"monitor_after,omitempty"`
	MonitorNumericDeltas    map[string]float64 `json:"monitor_numeric_deltas,omitempty"`
	PrometheusBefore        map[string]float64 `json:"prometheus_before,omitempty"`
	PrometheusAfter         map[string]float64 `json:"prometheus_after,omitempty"`
	PrometheusNumericDeltas map[string]float64 `json:"prometheus_numeric_deltas,omitempty"`
	MetricCaptureErrors     []string           `json:"metric_capture_errors,omitempty"`
	Checks                  []acceptanceCheck  `json:"checks"`
}

type acceptanceCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Actual  any    `json:"actual"`
	Limit   any    `json:"limit"`
	Skipped bool   `json:"skipped,omitempty"`
}

type acceptanceRunner struct {
	config acceptanceConfig
	client *http.Client
}

func runAcceptance(config acceptanceConfig) (acceptanceReport, error) {
	report := acceptanceReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		DryRun:      !config.execute,
		ReportScope: "single_environment_load_scenario",
		DoesNotProve: []string{
			"默认仅采集 metrics-path 总览；状态、模型检测和路由专用 snapshot 需将 metrics-path 显式指向对应端点并分别保存报告",
			"告警平台真实投递",
			"Redis 版本、XAUTOCLAIM 与 AOF/等价持久化配置",
			"外部故障已按标签真实生效（仅保留证据哈希，仍需人工复核原始证据）",
			"跨实例接管与 fencing",
			"账务零差异对账",
			"一次性全量回滚演练",
		},
		Config: makeReportConfig(config),
		Plans:  buildScenarioPlans(config),
	}
	if !config.execute {
		return report, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 10_000
	transport.MaxIdleConnsPerHost = 10_000
	transport.IdleConnTimeout = 90 * time.Second
	runner := acceptanceRunner{
		config: config,
		client: newAcceptanceHTTPClient(transport, config.requestTimeout),
	}
	for _, plan := range report.Plans {
		scenario, err := runner.runScenario(plan)
		if err != nil {
			return acceptanceReport{}, fmt.Errorf("场景 %s: %w", plan.Name, err)
		}
		for _, check := range scenario.Checks {
			if !check.Skipped && !check.Passed {
				report.FailedChecks++
			}
		}
		report.Scenarios = append(report.Scenarios, scenario)
	}
	transport.CloseIdleConnections()
	return report, nil
}

// newAcceptanceHTTPClient keeps an executed acceptance run scoped to the
// explicitly configured target. A redirect is an unexpected target change,
// not part of the load contract, so leave the response for the collector to
// record instead of following it.
func newAcceptanceHTTPClient(transport http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (runner *acceptanceRunner) runScenario(plan scenarioPlan) (scenarioReport, error) {
	monitorBefore, prometheusBefore, metricErrors := runner.captureMetrics(context.Background(), "前置")
	userCollector := newRequestCollector(runner.config.maxLatencySamples)
	adminCollector := newRequestCollector(runner.config.maxLatencySamples)
	var adminRefreshes atomic.Int64
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), runner.config.duration)
	defer cancel()

	var workers sync.WaitGroup
	workers.Add(plan.UserConcurrency + plan.AdminUsers)
	for range plan.UserConcurrency {
		go func() {
			defer workers.Done()
			runner.runUserWorker(ctx, userCollector)
		}()
	}
	for range plan.AdminUsers {
		go func() {
			defer workers.Done()
			runner.runAdminWorker(ctx, adminCollector, &adminRefreshes)
		}()
	}
	workers.Wait()
	elapsed := time.Since(startedAt)
	monitorAfter, prometheusAfter, afterMetricErrors := runner.captureMetrics(context.Background(), "后置")
	metricErrors = append(metricErrors, afterMetricErrors...)

	userSummary := userCollector.summary(elapsed)
	adminSummary := adminCollector.summary(elapsed)
	expectedAdminRequests := adminRefreshes.Load() * int64(plan.ExpectedRequestsPerRefresh)
	scenario := scenarioReport{
		Plan:                    plan,
		StartedAt:               startedAt.UTC().Format(time.RFC3339Nano),
		ElapsedSeconds:          elapsed.Seconds(),
		UserRequests:            userSummary,
		AdminRequests:           adminSummary,
		AdminRefreshes:          adminRefreshes.Load(),
		ExpectedAdminRequests:   expectedAdminRequests,
		FanoutMismatch:          adminSummary.Requests - expectedAdminRequests,
		MonitorBefore:           monitorBefore,
		MonitorAfter:            monitorAfter,
		MonitorNumericDeltas:    numericMetricDeltas(monitorBefore, monitorAfter),
		PrometheusBefore:        prometheusBefore,
		PrometheusAfter:         prometheusAfter,
		PrometheusNumericDeltas: numericMetricDeltasFloat(prometheusBefore, prometheusAfter),
		MetricCaptureErrors:     metricErrors,
	}
	scenario.Checks = runner.evaluateChecks(scenario)
	return scenario, nil
}

func (runner *acceptanceRunner) runUserWorker(ctx context.Context, collector *requestCollector) {
	for ctx.Err() == nil {
		// The duration stops new work. An in-flight request is allowed to finish
		// under the HTTP client timeout so the report does not count the phase
		// boundary itself as an application error.
		request, err := runner.newRequest(context.Background(), runner.config.userMethod, runner.config.userPath, runner.config.userBody, runner.config.userToken, "")
		if err != nil {
			return
		}
		runner.performRequest(request, collector)
	}
}

func (runner *acceptanceRunner) runAdminWorker(ctx context.Context, collector *requestCollector, refreshes *atomic.Int64) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			refreshes.Add(1)
			runner.runAdminRefresh(context.Background(), collector)
			timer.Reset(runner.config.adminRefreshInterval)
		}
	}
}

func (runner *acceptanceRunner) runAdminRefresh(ctx context.Context, collector *requestCollector) {
	endpoints := adminViewEndpoints[runner.config.adminView]
	var requests sync.WaitGroup
	requests.Add(len(endpoints))
	for _, endpoint := range endpoints {
		endpoint := endpoint
		go func() {
			defer requests.Done()
			request, err := runner.newRequest(ctx, endpoint.method, endpoint.path, nil, runner.config.adminToken, runner.config.adminCookie)
			if err != nil {
				collector.record(endpoint.method+" "+endpoint.path, 0, 0, 0, err)
				return
			}
			runner.performRequest(request, collector)
		}()
	}
	requests.Wait()
}

func (runner *acceptanceRunner) newRequest(ctx context.Context, method, path string, body []byte, token, cookie string) (*http.Request, error) {
	requestURL, err := runner.resolveURL(path)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "channel-monitor-cm10-acceptance/1")
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	return request, nil
}

func (runner *acceptanceRunner) resolveURL(path string) (string, error) {
	reference, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	return runner.config.baseURL.ResolveReference(reference).String(), nil
}

func (runner *acceptanceRunner) performRequest(request *http.Request, collector *requestCollector) {
	startedAt := time.Now()
	response, err := runner.client.Do(request)
	latency := time.Since(startedAt)
	endpoint := request.Method + " " + request.URL.EscapedPath()
	if strings.HasPrefix(request.URL.Path, "/api/channel_monitor/") && request.URL.RawQuery != "" {
		endpoint += "?" + request.URL.RawQuery
	}
	if err != nil {
		collector.record(endpoint, latency, 0, 0, err)
		return
	}
	defer response.Body.Close()
	responseBytes, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 16<<20))
	collector.record(endpoint, latency, response.StatusCode, responseBytes, readErr)
}

func (runner *acceptanceRunner) captureMetrics(ctx context.Context, phase string) (map[string]any, map[string]float64, []string) {
	monitor, err := runner.fetchMonitorSnapshot(ctx)
	errors := make([]string, 0, 2)
	if err != nil {
		errors = append(errors, fmt.Sprintf("%s监控快照: %s", phase, err))
		monitor = make(map[string]any)
	}
	prometheus := make(map[string]float64)
	if runner.config.prometheusPath != "" {
		prometheus, err = runner.fetchPrometheusSnapshot(ctx)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s Prometheus 快照: %s", phase, err))
			prometheus = make(map[string]float64)
		}
	}
	return monitor, prometheus, errors
}

func (runner *acceptanceRunner) fetchMonitorSnapshot(ctx context.Context) (map[string]any, error) {
	request, err := runner.newRequest(ctx, http.MethodGet, runner.config.metricsPath, nil, runner.config.adminToken, runner.config.adminCookie)
	if err != nil {
		return nil, err
	}
	response, err := runner.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("读取监控快照: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("监控快照返回 HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Success *bool `json:"success"`
	}
	if err := common.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("解析监控快照 envelope: %w", err)
	}
	if envelope.Success != nil && !*envelope.Success {
		return nil, fmt.Errorf("监控快照 success=false")
	}
	metrics, err := extractMonitorMetrics(body)
	if err != nil {
		return nil, fmt.Errorf("解析监控快照: %w", err)
	}
	return metrics, nil
}

func (runner *acceptanceRunner) fetchPrometheusSnapshot(ctx context.Context) (map[string]float64, error) {
	request, err := runner.newRequest(ctx, http.MethodGet, runner.config.prometheusPath, nil, runner.config.adminToken, runner.config.adminCookie)
	if err != nil {
		return nil, err
	}
	response, err := runner.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("读取 Prometheus 指标: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Prometheus 指标返回 HTTP %d", response.StatusCode)
	}
	return parsePrometheusMetrics(body, runner.config.prometheusMetricNames), nil
}

func (runner *acceptanceRunner) evaluateChecks(scenario scenarioReport) []acceptanceCheck {
	checks := []acceptanceCheck{
		thresholdDurationCheck("user_p95", scenario.UserRequests.P95Milliseconds, runner.config.maxUserP95),
		thresholdDurationCheck("user_p99", scenario.UserRequests.P99Milliseconds, runner.config.maxUserP99),
		thresholdFloatCheck("user_error_rate_percent", scenario.UserRequests.ErrorRatePercent, runner.config.maxUserErrorRatePercent),
		thresholdDurationCheck("admin_p95", scenario.AdminRequests.P95Milliseconds, runner.config.maxAdminP95),
		thresholdDurationCheck("admin_p99", scenario.AdminRequests.P99Milliseconds, runner.config.maxAdminP99),
		thresholdFloatCheck("admin_error_rate_percent", scenario.AdminRequests.ErrorRatePercent, runner.config.maxAdminErrorRatePercent),
		{Name: "admin_request_fanout", Passed: absInt64(scenario.FanoutMismatch) <= runner.config.maxFanoutMismatch, Actual: scenario.FanoutMismatch, Limit: runner.config.maxFanoutMismatch},
		{Name: "metric_capture", Passed: len(scenario.MetricCaptureErrors) == 0, Actual: len(scenario.MetricCaptureErrors), Limit: 0},
	}
	droppedDelta, found := metricDeltaBySuffix(scenario.MonitorNumericDeltas, ".writer_dropped_events")
	writerCheck := acceptanceCheck{Name: "writer_dropped_events_delta", Actual: droppedDelta, Limit: runner.config.maxWriterDroppedDelta}
	if runner.config.maxWriterDroppedDelta < 0 {
		writerCheck.Skipped = true
		writerCheck.Passed = true
	} else if !found {
		writerCheck.Actual = "missing"
		writerCheck.Passed = false
	} else {
		writerCheck.Passed = droppedDelta <= runner.config.maxWriterDroppedDelta
	}
	return append(checks, writerCheck)
}

func thresholdDurationCheck(name string, actualMilliseconds float64, limit time.Duration) acceptanceCheck {
	check := acceptanceCheck{Name: name, Actual: actualMilliseconds, Limit: durationMilliseconds(limit)}
	if limit <= 0 {
		check.Passed = true
		check.Skipped = true
		return check
	}
	check.Passed = actualMilliseconds <= durationMilliseconds(limit)
	return check
}

func thresholdFloatCheck(name string, actual, limit float64) acceptanceCheck {
	check := acceptanceCheck{Name: name, Actual: actual, Limit: limit}
	if limit < 0 {
		check.Passed = true
		check.Skipped = true
		return check
	}
	check.Passed = actual <= limit
	return check
}

func metricDeltaBySuffix(values map[string]float64, suffix string) (float64, bool) {
	for path, value := range values {
		if strings.HasSuffix("."+path, suffix) {
			return value, true
		}
	}
	return 0, false
}

func numericMetricDeltasFloat(before, after map[string]float64) map[string]float64 {
	result := make(map[string]float64)
	for name, afterValue := range after {
		if beforeValue, ok := before[name]; ok {
			result[name] = afterValue - beforeValue
		}
	}
	return result
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
