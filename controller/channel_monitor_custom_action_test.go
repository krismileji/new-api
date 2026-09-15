package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorCustomActionRefreshIntegration(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/reset" {
			calls.Add(1)
			w.WriteHeader(500)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ratio":2,"balance":5}}`))
	}))
	defer server.Close()
	baseURL := server.URL
	channel := model.Channel{Id: 81, Name: "触发测试", Key: "channel-key", Group: "default", BaseURL: &baseURL}
	require.NoError(t, db.Create(&channel).Error)
	threshold := 10.0
	// Keep the runtime window around local noon independently of the test's wall clock.
	zone := fmt.Sprintf("Etc/GMT%+d", time.Now().UTC().Hour()-12)
	action := service.ChannelMonitorCustomAction{ID: "reset", Name: "余额重置", Enabled: true, Metric: "balance", Operator: "lt", Threshold: &threshold, Timezone: zone, StartTime: "00:00", EndTime: "23:00", DailyLimit: 1, CooldownMinutes: 60, Request: service.ChannelMonitorCustomRequestConfig{Method: "POST", Path: "/reset", BodyType: "none"}}
	config := service.ChannelMonitorCustomUpstreamConfig{
		Ratio:                    service.ChannelMonitorCustomMetricConfig{Source: "http", Request: &service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/metrics"}, Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "data.ratio", Multiplier: 1}},
		Balance:                  service.ChannelMonitorCustomMetricConfig{Source: "http", Result: &service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "data.balance", Multiplier: 1}},
		BalanceReuseRatioRequest: true, Actions: []service.ChannelMonitorCustomAction{action},
	}
	raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	monitor := model.ChannelRatioMonitor{ChannelId: 81, UpstreamType: "custom", UpstreamBaseURL: baseURL, UpstreamRevision: 1, CustomUpstreamConfig: raw}
	require.NoError(t, db.Create(&monitor).Error)
	request := gin.H{"type": "custom", "base_url": baseURL, "custom_config": config}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/81/upstream/test", request)
	ctx.Params = gin.Params{{Key: "id", Value: "81"}}
	TestChannelMonitorUpstreamConfig(ctx)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.Zero(t, calls.Load(), "草稿测试不能重置上游")
	outcome, err := fetchAndRecordChannelMonitorUpstreamRatio(t.Context(), monitor, nil, "", time.Second, channelMonitorRefreshOptions{IncludeSeparateBalance: true}, 0, "测试")
	require.NoError(t, err, "重置失败不能使指标获取失败或触发获取重试")
	assert.Equal(t, 2.0, outcome.Result.Ratio)
	assert.EqualValues(t, 1, calls.Load())
	_, err = fetchAndRecordChannelMonitorUpstreamBalance(t.Context(), monitor, nil, "", time.Second, channelMonitorRefreshOptions{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, calls.Load(), "单独余额刷新共享执行限制")
	saved, err := model.GetChannelRatioMonitor(81)
	require.NoError(t, err)
	view := channelMonitorUpstreamFromModel(saved)
	assert.Equal(t, "failed", view.CustomActionStates["reset"].Status)
	assert.Equal(t, model.ChannelRatioFetchStatusSucceeded, saved.LastFetchStatus)
}
