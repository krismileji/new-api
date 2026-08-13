package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelModelDetectorRoundTripper func(*http.Request) (*http.Response, error)

func (fn channelModelDetectorRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func detectorContractPreset(t *testing.T, hash string) ChannelModelDetectorPresetConfig {
	t.Helper()
	data, err := common.Marshal(map[string]any{
		"mode":           "single",
		"preset":         "low",
		"workers":        8,
		"config_hash":    hash,
		"future_setting": map[string]any{"enabled": true},
	})
	require.NoError(t, err)
	var preset ChannelModelDetectorPresetConfig
	require.NoError(t, common.Unmarshal(data, &preset))
	return preset
}

func writeDetectorContractJSON(t *testing.T, response http.ResponseWriter, status int, value any) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err = response.Write(data)
	require.NoError(t, err)
}

func TestChannelModelDetectorClientContractCallsOfficialEndpoints(t *testing.T) {
	var mutex sync.Mutex
	token := "session-one"
	preset := detectorContractPreset(t, "hash-low")
	seenPaths := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		seenPaths[request.Method+" "+request.URL.Path]++
		mutex.Unlock()
		assert.Equal(t, "Bearer proxy-secret", request.Header.Get("Authorization"))

		switch request.URL.Path {
		case channelModelDetectorHealthPath:
			assert.Equal(t, http.MethodGet, request.Method)
			assert.Empty(t, request.Header.Get("X-GPT56-Session"))
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"status": "ok", "binding": "127.0.0.1", "state_transport": "polling", "future_health": 1,
			})
		case channelModelDetectorBootstrapPath:
			assert.Equal(t, http.MethodGet, request.Method)
			assert.Empty(t, request.Header.Get("X-GPT56-Session"))
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"session_token":      token,
				"schema_version":     2,
				"single_presets":     map[string]any{"low": preset, "medium": preset, "high": preset},
				"continuous_presets": map[string]any{},
				"schema":             map[string]any{},
				"probe_catalog":      []any{},
				"future_bootstrap":   map[string]any{"value": 7},
			})
		case channelModelDetectorEstimatePath:
			assert.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, token, request.Header.Get("X-GPT56-Session"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			var payload map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(body, &payload))
			var received ChannelModelDetectorPresetConfig
			require.NoError(t, common.Unmarshal(payload["config"], &received))
			assert.JSONEq(t, string(preset["future_setting"]), string(received["future_setting"]))
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"total_requests": 14, "fixed_32k_requests": 0, "approximate_fixed_32k_input_tokens": 0, "future_estimate": true,
			})
		case channelModelDetectorStartPath:
			assert.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, token, request.Header.Get("X-GPT56-Session"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			var payload map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(body, &payload))
			assert.Contains(t, string(payload["api_key"]), "task-secret")
			assert.JSONEq(t, string(preset["future_setting"]), func() string {
				var config ChannelModelDetectorPresetConfig
				require.NoError(t, common.Unmarshal(payload["config"], &config))
				return string(config["future_setting"])
			}())
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"started": true, "session_id": "official-session", "official": true, "config_hash": "hash-low", "future_start": "kept",
			})
		case channelModelDetectorStatusPath:
			assert.Equal(t, http.MethodGet, request.Method)
			assert.Empty(t, request.Header.Get("X-GPT56-Session"))
			mutex.Lock()
			statusCall := seenPaths[request.Method+" "+request.URL.Path]
			mutex.Unlock()
			if statusCall == 1 {
				writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{"status": "idle"})
				return
			}
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"status": "complete", "session_id": "official-session", "config_hash": "hash-low",
				"claimed_model": "gpt-5.6-sol", "safe_endpoint": "https://relay.example/internal/model-detector/v1",
				"report_available": true, "progress": map[string]any{"planned": 14, "logical_completed": 14, "future_progress": 9},
				"future_status": "kept",
			})
		case channelModelDetectorReportPath:
			assert.Equal(t, http.MethodGet, request.Method)
			assert.Empty(t, request.Header.Get("X-GPT56-Session"))
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"schema_version": 3, "scoring_version": "score-v3", "session_id": "official-session", "config_hash": "hash-low",
				"baseline_id": "baseline", "baseline_sha256": "baseline-sha", "build_hash": "build-sha", "official": true,
				"candidate_configuration_without_key": map[string]any{"model": "gpt-5.6-sol"},
				"outcome_code":                        "juice_pass_fingerprint_strong", "overall_verdict": "通过", "future_report": map[string]any{"proof": 1},
			})
		case channelModelDetectorStopPath:
			assert.Equal(t, http.MethodPost, request.Method)
			assert.Equal(t, token, request.Header.Get("X-GPT56-Session"))
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"accepted": true, "stopping": true, "session_id": "official-session", "previous_status": "running",
				"current_status": "stopping", "active_requests_cancelled": 2, "future_stop": true,
			})
		default:
			t.Fatalf("unexpected detector path %s", request.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewChannelModelDetectorClient(server.URL, ChannelModelDetectorClientOptions{
		HTTPClient: server.Client(), ProxyToken: "proxy-secret",
	})
	require.NoError(t, err)

	health, err := client.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ok", health.Status)
	assert.Contains(t, health.Raw, "future_health")

	bootstrap, err := client.Bootstrap(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "session-one", bootstrap.SessionToken)
	assert.Contains(t, bootstrap.Raw, "future_bootstrap")
	assert.NotContains(t, bootstrap.Raw, "session_token")
	serializedBootstrap, err := common.Marshal(bootstrap)
	require.NoError(t, err)
	assert.NotContains(t, string(serializedBootstrap), "session-one")
	low, ok := bootstrap.Preset("low")
	require.True(t, ok)
	assert.Contains(t, low, "future_setting")

	estimate, err := client.Estimate(context.Background(), low)
	require.NoError(t, err)
	require.NotNil(t, estimate.TotalRequests)
	assert.EqualValues(t, 14, *estimate.TotalRequests)
	assert.Contains(t, estimate.Raw, "future_estimate")

	started, err := client.Start(context.Background(), ChannelModelDetectorStartRequest{
		BaseURL: "https://relay.example/internal/model-detector/v1/", APIKey: "task-secret", Model: "gpt-5.6-sol", Config: low,
	})
	require.NoError(t, err)
	assert.Equal(t, "official-session", started.SessionID)
	assert.Contains(t, started.Raw, "future_start")

	status, err := client.Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "complete", status.Status)
	assert.Contains(t, status.Raw, "future_status")

	report, err := client.Report(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "score-v3", report.ScoringVersion)
	assert.Equal(t, "baseline-sha", report.BaselineSHA256)
	assert.Equal(t, "build-sha", report.BuildHash)
	assert.Equal(t, "gpt-5.6-sol", report.ClaimedModel)
	assert.Contains(t, report.Raw, "future_report")
	serializedReport, err := common.Marshal(report)
	require.NoError(t, err)
	assert.Contains(t, string(serializedReport), "future_report")

	stopped, err := client.Stop(context.Background())
	require.NoError(t, err)
	require.NotNil(t, stopped.Accepted)
	assert.True(t, *stopped.Accepted)
	assert.Contains(t, stopped.Raw, "future_stop")

	mutex.Lock()
	defer mutex.Unlock()
	for _, endpoint := range []string{
		http.MethodGet + " " + channelModelDetectorHealthPath,
		http.MethodGet + " " + channelModelDetectorBootstrapPath,
		http.MethodPost + " " + channelModelDetectorEstimatePath,
		http.MethodPost + " " + channelModelDetectorStartPath,
		http.MethodGet + " " + channelModelDetectorReportPath,
		http.MethodPost + " " + channelModelDetectorStopPath,
	} {
		assert.Equal(t, 1, seenPaths[endpoint], endpoint)
	}
	assert.Equal(t, 2, seenPaths[http.MethodGet+" "+channelModelDetectorStatusPath])
}

func TestChannelModelDetectorContractRefreshesSessionAndPreservesPreset(t *testing.T) {
	var mutex sync.Mutex
	bootstrapCount := 0
	seenEstimateTokens := make([]string, 0, 2)
	seenFutureValues := make([]int, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case channelModelDetectorBootstrapPath:
			mutex.Lock()
			bootstrapCount++
			current := bootstrapCount
			mutex.Unlock()
			preset := detectorContractPreset(t, "hash")
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"session_token": "token-" + string(rune('0'+current)), "schema_version": 2,
				"single_presets": map[string]any{"low": preset, "medium": preset, "high": preset},
			})
		case channelModelDetectorEstimatePath:
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			var envelope struct {
				Config struct {
					FutureSetting struct {
						Enabled bool `json:"enabled"`
					} `json:"future_setting"`
				} `json:"config"`
			}
			require.NoError(t, common.Unmarshal(body, &envelope))
			mutex.Lock()
			seenEstimateTokens = append(seenEstimateTokens, request.Header.Get("X-GPT56-Session"))
			if envelope.Config.FutureSetting.Enabled {
				seenFutureValues = append(seenFutureValues, 1)
			}
			mutex.Unlock()
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{"total_requests": 14, "fixed_32k_requests": 0})
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewChannelModelDetectorClientWithHTTPClient(server.URL, server.Client())
	require.NoError(t, err)

	first, err := client.Bootstrap(context.Background())
	require.NoError(t, err)
	firstLow, ok := first.Preset("low")
	require.True(t, ok)
	_, err = client.Estimate(context.Background(), firstLow)
	require.NoError(t, err)

	second, err := client.Bootstrap(context.Background())
	require.NoError(t, err)
	secondLow, ok := second.Preset("low")
	require.True(t, ok)
	_, err = client.Estimate(context.Background(), secondLow)
	require.NoError(t, err)

	mutex.Lock()
	defer mutex.Unlock()
	assert.Equal(t, []string{"token-1", "token-2"}, seenEstimateTokens)
	assert.Equal(t, []int{1, 1}, seenFutureValues)
}

func TestChannelModelDetectorContractCheckCompatibilityUsesDynamicLowEstimate(t *testing.T) {
	preset := detectorContractPreset(t, "dynamic-hash")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case channelModelDetectorHealthPath:
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{"status": "ok", "binding": "127.0.0.1", "state_transport": "polling"})
		case channelModelDetectorBootstrapPath:
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"session_token": "session", "schema_version": 2,
				"single_presets": map[string]any{"low": preset, "medium": preset, "high": preset},
			})
		case channelModelDetectorEstimatePath:
			assert.Equal(t, "session", request.Header.Get("X-GPT56-Session"))
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"total_requests": 31, "fixed_32k_requests": 4, "approximate_fixed_32k_input_tokens": 135168,
			})
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewChannelModelDetectorClientWithHTTPClient(server.URL, server.Client())
	require.NoError(t, err)

	result, err := client.CheckCompatibility(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result.LowEstimate.TotalRequests)
	assert.EqualValues(t, 31, *result.LowEstimate.TotalRequests)
	require.NotNil(t, result.LowEstimate.Fixed32KRequests)
	assert.EqualValues(t, 4, *result.LowEstimate.Fixed32KRequests)
}

func TestChannelModelDetectorClientStartTimeoutReconcilesBeforeReturning(t *testing.T) {
	preset := detectorContractPreset(t, "start-hash")
	var statusCalls int
	client, err := NewChannelModelDetectorClient("https://detector.example", ChannelModelDetectorClientOptions{
		RequestTimeout: 20 * time.Millisecond,
		HTTPClient: &http.Client{Transport: channelModelDetectorRoundTripper(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case channelModelDetectorBootstrapPath:
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"session_token":"token","schema_version":2,"single_presets":{"low":{"config_hash":"start-hash"},"medium":{"config_hash":"start-hash"},"high":{"config_hash":"start-hash"}}}`)),
					Request:    request,
				}, nil
			case channelModelDetectorStartPath:
				<-request.Context().Done()
				return nil, request.Context().Err()
			case channelModelDetectorStatusPath:
				statusCalls++
				if statusCalls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"idle","session_id":"old-session"}`)), Request: request}, nil
				}
				updatedAt, err := common.Marshal(time.Now().UTC().Format(time.RFC3339Nano))
				require.NoError(t, err)
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"status":"running","session_id":"new-session","updated_at":` + string(updatedAt) + `,"config_hash":"start-hash","claimed_model":"gpt-5.6-sol","safe_endpoint":"https://relay.example/v1"}`)),
					Request:    request,
				}, nil
			default:
				t.Fatalf("unexpected path %s", request.URL.Path)
				return nil, nil
			}
		})},
	})
	require.NoError(t, err)
	_, err = client.Bootstrap(context.Background())
	require.NoError(t, err)

	result, err := client.Start(context.Background(), ChannelModelDetectorStartRequest{
		BaseURL: "https://relay.example/v1", APIKey: "task-secret", Model: "gpt-5.6-sol", Config: preset, PreviousSessionID: "old-session",
	})
	require.NoError(t, err)
	assert.True(t, result.Reconciled)
	assert.Equal(t, "new-session", result.SessionID)
	assert.Equal(t, 2, statusCalls)
}

func TestChannelModelDetectorClientStartTimeoutRejectsStatusWithoutIdentity(t *testing.T) {
	preset := detectorContractPreset(t, "start-hash")
	statusCalls := 0
	client, err := NewChannelModelDetectorClient("https://detector.example", ChannelModelDetectorClientOptions{
		RequestTimeout: 20 * time.Millisecond,
		HTTPClient: &http.Client{Transport: channelModelDetectorRoundTripper(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case channelModelDetectorBootstrapPath:
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"session_token":"token","schema_version":2,"single_presets":{"low":{"config_hash":"start-hash"},"medium":{"config_hash":"start-hash"},"high":{"config_hash":"start-hash"}}}`)), Request: request}, nil
			case channelModelDetectorStartPath:
				<-request.Context().Done()
				return nil, request.Context().Err()
			case channelModelDetectorStatusPath:
				statusCalls++
				if statusCalls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"idle","session_id":"old-session"}`)), Request: request}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"running","session_id":"new-session","config_hash":"start-hash"}`)), Request: request}, nil
			default:
				t.Fatalf("unexpected path %s", request.URL.Path)
				return nil, nil
			}
		})},
	})
	require.NoError(t, err)
	_, err = client.Bootstrap(context.Background())
	require.NoError(t, err)

	_, err = client.Start(context.Background(), ChannelModelDetectorStartRequest{
		BaseURL: "https://relay.example/v1", APIKey: "task-secret", Model: "gpt-5.6-sol", Config: preset, PreviousSessionID: "old-session",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChannelModelDetectorSubmissionUnknown)
}

func TestChannelModelDetectorClientStartTimeoutReturnsSubmissionUnknown(t *testing.T) {
	preset := detectorContractPreset(t, "start-hash")
	statusCalls := 0
	client, err := NewChannelModelDetectorClient("https://detector.example", ChannelModelDetectorClientOptions{
		RequestTimeout: 20 * time.Millisecond,
		HTTPClient: &http.Client{Transport: channelModelDetectorRoundTripper(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case channelModelDetectorBootstrapPath:
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"session_token":"token","schema_version":2,"single_presets":{"low":{"config_hash":"start-hash"},"medium":{"config_hash":"start-hash"},"high":{"config_hash":"start-hash"}}}`)), Request: request}, nil
			case channelModelDetectorStartPath:
				<-request.Context().Done()
				return nil, request.Context().Err()
			case channelModelDetectorStatusPath:
				statusCalls++
				if statusCalls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"idle","session_id":"old-session"}`)), Request: request}, nil
				}
				updatedAt, err := common.Marshal(time.Now().UTC().Format(time.RFC3339Nano))
				require.NoError(t, err)
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"running","session_id":"old-session","updated_at":` + string(updatedAt) + `,"config_hash":"start-hash","claimed_model":"gpt-5.6-sol","safe_endpoint":"https://relay.example/v1"}`)), Request: request}, nil
			default:
				t.Fatalf("unexpected path %s", request.URL.Path)
				return nil, nil
			}
		})},
	})
	require.NoError(t, err)
	_, err = client.Bootstrap(context.Background())
	require.NoError(t, err)

	_, err = client.Start(context.Background(), ChannelModelDetectorStartRequest{
		BaseURL: "https://relay.example/v1", APIKey: "task-secret", Model: "gpt-5.6-sol", Config: preset, PreviousSessionID: "old-session",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChannelModelDetectorSubmissionUnknown)
	var detectorErr *ChannelModelDetectorError
	require.ErrorAs(t, err, &detectorErr)
	require.NotNil(t, detectorErr.ReconciledStatus)
	assert.Equal(t, "old-session", detectorErr.ReconciledStatus.SessionID)
	assert.NotContains(t, err.Error(), "task-secret")
}

func TestChannelModelDetectorClientStartRejectsBusySessionBeforeSubmission(t *testing.T) {
	preset := detectorContractPreset(t, "start-hash")
	startCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case channelModelDetectorBootstrapPath:
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
				"session_token": "token", "single_presets": map[string]any{"low": preset, "medium": preset, "high": preset},
			})
		case channelModelDetectorStatusPath:
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{"status": "running", "session_id": "external-session"})
		case channelModelDetectorStartPath:
			startCalls++
			writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{"started": true, "session_id": "should-not-start", "config_hash": "start-hash"})
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	client, err := NewChannelModelDetectorClientWithHTTPClient(server.URL, server.Client())
	require.NoError(t, err)
	_, err = client.Bootstrap(context.Background())
	require.NoError(t, err)

	_, err = client.Start(context.Background(), ChannelModelDetectorStartRequest{
		BaseURL: "https://relay.example/v1", APIKey: "task-secret", Model: "gpt-5.6-sol", Config: preset,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChannelModelDetectorBusy)
	assert.Zero(t, startCalls)
}

func TestChannelModelDetectorClientClassifiesFailuresAndLimitsResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		maxBytes   int64
		wantKind   ChannelModelDetectorErrorKind
	}{
		{name: "busy", statusCode: http.StatusBadRequest, body: `{"error":"检测正在运行或停止中，请等待当前会话结束"}`, wantKind: ChannelModelDetectorErrorBusy},
		{name: "unauthorized", statusCode: http.StatusForbidden, body: `{"error":"本地会话令牌无效，请刷新页面"}`, wantKind: ChannelModelDetectorErrorUnauthorized},
		{name: "incompatible endpoint", statusCode: http.StatusNotFound, body: `{"error":"接口不存在"}`, wantKind: ChannelModelDetectorErrorIncompatible},
		{name: "oversized", statusCode: http.StatusOK, body: `{"status":"ok","padding":"` + strings.Repeat("x", 512) + `"}`, maxBytes: 64, wantKind: ChannelModelDetectorErrorResponseTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path == channelModelDetectorBootstrapPath {
					writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{
						"session_token": "token", "single_presets": map[string]any{"low": map[string]any{}, "medium": map[string]any{}, "high": map[string]any{}},
					})
					return
				}
				if test.name == "busy" && request.URL.Path == channelModelDetectorStatusPath {
					writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{"status": "idle"})
					return
				}
				response.WriteHeader(test.statusCode)
				_, err := response.Write([]byte(test.body))
				require.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			client, err := NewChannelModelDetectorClient(server.URL, ChannelModelDetectorClientOptions{HTTPClient: server.Client(), MaxResponseBytes: test.maxBytes})
			require.NoError(t, err)
			if test.name != "oversized" {
				_, err = client.Bootstrap(context.Background())
				require.NoError(t, err)
			}

			if test.name == "busy" {
				_, err = client.Start(context.Background(), ChannelModelDetectorStartRequest{BaseURL: "https://relay.example/v1", APIKey: "secret", Model: "gpt-5.6-sol", Config: ChannelModelDetectorPresetConfig{}})
			} else if test.name == "oversized" {
				_, err = client.Health(context.Background())
			} else {
				_, err = client.Estimate(context.Background(), ChannelModelDetectorPresetConfig{})
			}
			require.Error(t, err)
			var detectorErr *ChannelModelDetectorError
			require.ErrorAs(t, err, &detectorErr)
			assert.Equal(t, test.wantKind, detectorErr.Kind)
		})
	}

	t.Run("transport unavailable", func(t *testing.T) {
		client, err := NewChannelModelDetectorClient("https://detector.example", ChannelModelDetectorClientOptions{
			HTTPClient: &http.Client{Transport: channelModelDetectorRoundTripper(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial failed")
			})},
		})
		require.NoError(t, err)
		_, err = client.Health(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrChannelModelDetectorUnavailable)
	})
}

func TestChannelModelDetectorContractRejectsMissingRequiredCapability(t *testing.T) {
	var bootstrapCalls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		bootstrapCalls++
		presets := map[string]any{"low": map[string]any{}, "medium": map[string]any{}, "high": map[string]any{}}
		if bootstrapCalls > 1 {
			delete(presets, "high")
		}
		writeDetectorContractJSON(t, response, http.StatusOK, map[string]any{"session_token": "token", "single_presets": presets})
	}))
	t.Cleanup(server.Close)
	client, err := NewChannelModelDetectorClientWithHTTPClient(server.URL, server.Client())
	require.NoError(t, err)

	_, err = client.Bootstrap(context.Background())
	require.NoError(t, err)
	_, err = client.Bootstrap(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrChannelModelDetectorIncompatible)
	_, err = client.Estimate(context.Background(), ChannelModelDetectorPresetConfig{})
	require.Error(t, err)
	var detectorErr *ChannelModelDetectorError
	require.ErrorAs(t, err, &detectorErr)
	assert.Equal(t, ChannelModelDetectorErrorSessionRequired, detectorErr.Kind)
}

func TestChannelModelDetectorClientRequiresBootstrapForPost(t *testing.T) {
	client, err := NewChannelModelDetectorClient("https://detector.example", ChannelModelDetectorClientOptions{
		HTTPClient: &http.Client{Transport: channelModelDetectorRoundTripper(func(*http.Request) (*http.Response, error) {
			t.Fatal("request should not be sent without bootstrap")
			return nil, nil
		})},
	})
	require.NoError(t, err)

	_, err = client.Estimate(context.Background(), ChannelModelDetectorPresetConfig{})
	require.Error(t, err)
	var detectorErr *ChannelModelDetectorError
	require.ErrorAs(t, err, &detectorErr)
	assert.Equal(t, ChannelModelDetectorErrorSessionRequired, detectorErr.Kind)
}

func TestChannelModelDetectorClientStartRequestDoesNotSerializeAPIKey(t *testing.T) {
	data, err := common.Marshal(ChannelModelDetectorStartRequest{
		BaseURL: "https://relay.example/v1", APIKey: "task-secret", Model: "gpt-5.6-sol",
		Config: detectorContractPreset(t, "hash"), PreviousSessionID: "old-session",
	})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "task-secret")
	assert.NotContains(t, string(data), "api_key")
	assert.Contains(t, string(data), "old-session")
}

func TestChannelModelDetectorClientNormalizeURL(t *testing.T) {
	normalized, err := NormalizeChannelModelDetectorURL(" https://detector.example/proxy/ ")
	require.NoError(t, err)
	assert.Equal(t, "https://detector.example/proxy", normalized)

	for _, value := range []string{"", "ftp://detector.example", "https://user:pass@detector.example", "https://detector.example?token=secret", "https://detector.example/#fragment"} {
		_, err := NormalizeChannelModelDetectorURL(value)
		assert.Error(t, err, value)
	}
}

func TestChannelModelDetectorClientReadsProxyTokenFromEnvironment(t *testing.T) {
	t.Setenv("GPT56_DETECTOR_PROXY_TOKEN", "environment-secret")
	client, err := NewChannelModelDetectorClient("https://detector.example", ChannelModelDetectorClientOptions{
		HTTPClient: &http.Client{Transport: channelModelDetectorRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("not used")
		})},
	})
	require.NoError(t, err)
	assert.Equal(t, "environment-secret", client.proxyToken)

	explicit, err := NewChannelModelDetectorClient("https://detector.example", ChannelModelDetectorClientOptions{
		HTTPClient: &http.Client{Transport: channelModelDetectorRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("not used")
		})},
		ProxyToken: "explicit-secret",
	})
	require.NoError(t, err)
	assert.Equal(t, "explicit-secret", explicit.proxyToken)
}
