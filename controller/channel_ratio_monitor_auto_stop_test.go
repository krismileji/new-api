package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunChannelRatioMonitorTaskStopsRatioAtConfiguredConsecutiveFailureLimit(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "10",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
	})
	disableChannelMonitorSSRFProtection(t)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "ratio failure", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
	}).Error)

	firstSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, firstSummary.Failed)
	assert.Equal(t, 2, firstSummary.Retried)
	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, 3, monitor.ConsecutiveFailures)
	requestsAfterFailureLimit := requestCount.Load()
	assert.Positive(t, requestsAfterFailureLimit)

	secondSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, secondSummary.Skipped)
	assert.Zero(t, secondSummary.Failed)
	assert.Equal(t, requestsAfterFailureLimit, requestCount.Load())
}

func TestRunChannelRatioMonitorTaskStopsBalanceAtConfiguredConsecutiveFailureLimit(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "10",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
	})
	disableChannelMonitorSSRFProtection(t)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "balance failure", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamAuthType: service.NewAPIUpstreamAuthUser, UpstreamUserId: 42, UpstreamAccessToken: "test-token",
		UpstreamRatioSyncDisabled: true,
	}).Error)

	firstSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, firstSummary.Failed)
	assert.Equal(t, 2, firstSummary.Retried)
	assert.EqualValues(t, 3, requestCount.Load())
	monitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Equal(t, 3, monitor.BalanceConsecutiveFailures)

	secondSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, secondSummary.Skipped)
	assert.Zero(t, secondSummary.Failed)
	assert.EqualValues(t, 3, requestCount.Load())
}

func TestRunChannelRatioMonitorTaskKeepsHealthyUpstreamMetricRunning(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:              "0",
		channelMonitorAutoUpdateConsecutiveFailureLimitOption: "3",
	})
	disableChannelMonitorSSRFProtection(t)

	var ratioRequests atomic.Int32
	var balanceRequests atomic.Int32
	var statusRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/self/groups":
			ratioRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"vip":{"ratio":1.25}}}`))
		case "/api/user/self":
			balanceRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500}}`))
		case "/api/status":
			statusRequests.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":100}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1, Name: "ratio remains active", Key: "ratio-key", Group: "vip", Status: common.ChannelStatusEnabled},
		{Id: 2, Name: "balance remains active", Key: "balance-key", Group: "vip", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{
			ChannelId: 1, Ratio: 1, UpdatedTime: 1,
			UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
			UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 41, UpstreamAccessToken: "ratio-token",
			BalanceConsecutiveFailures: 3,
		},
		{
			ChannelId:    2,
			UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
			UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthUser,
			UpstreamUserId: 42, UpstreamAccessToken: "balance-token",
			ConsecutiveFailures: 3,
		},
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Updated)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Zero(t, summary.Failed)
	assert.EqualValues(t, 1, ratioRequests.Load())
	assert.EqualValues(t, 1, balanceRequests.Load())
	assert.EqualValues(t, 1, statusRequests.Load())

	ratioMonitor, err := model.GetChannelRatioMonitor(1)
	require.NoError(t, err)
	assert.Zero(t, ratioMonitor.ConsecutiveFailures)
	assert.Equal(t, 3, ratioMonitor.BalanceConsecutiveFailures)
	balanceMonitor, err := model.GetChannelRatioMonitor(2)
	require.NoError(t, err)
	assert.Equal(t, 3, balanceMonitor.ConsecutiveFailures)
	assert.Zero(t, balanceMonitor.BalanceConsecutiveFailures)
	require.NotNil(t, balanceMonitor.UpstreamBalance)
	assert.InDelta(t, 5, *balanceMonitor.UpstreamBalance, 1e-9)
}
