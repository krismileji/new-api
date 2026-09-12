package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchChannelMonitorCustomVariableDraftUsesSavedSecretsWithoutSaving(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	disableChannelMonitorSSRFProtection(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/login", r.URL.Path)
		assert.Equal(t, "saved-login-key", r.Header.Get("X-Api-Key"))
		_, _ = w.Write([]byte(`{"data":{"token":"draft-token"},"password":"must-not-return"}`))
	}))
	defer server.Close()
	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{Id: 71, Name: "自定义测试", Key: "channel-key", Group: "default", BaseURL: &baseURL}).Error)
	ratio, balance := 1.0, 2.0
	config := service.ChannelMonitorCustomUpstreamConfig{
		Ratio:   service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &ratio},
		Balance: service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &balance},
		VariableRequest: &service.ChannelMonitorCustomLegacyVariableRequest{
			Name: "token", Value: "saved-token", RefreshPolicy: "on_failure",
			Request: service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/login", Headers: []service.ChannelMonitorCustomKeyValue{{Key: "X-Api-Key", Value: "saved-login-key", Secret: true}}},
			Result:  service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "data.token"},
		},
	}
	normalized, err := service.NormalizeChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(normalized)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 71, UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: baseURL, CustomUpstreamConfig: raw}).Error)
	draft := service.SanitizeChannelMonitorCustomUpstreamConfig(normalized)
	draft.VariableRequests = append(draft.VariableRequests, service.ChannelMonitorCustomVariableRequest{ID: "unfinished", Name: "未完成请求"})
	// Metric endpoints are deliberately incomplete while configuring the login.
	draft.Ratio = service.ChannelMonitorCustomMetricConfig{Source: "http"}
	requestBody := gin.H{"type": "custom", "base_url": baseURL, "custom_config": draft, "request_id": "legacy-variable"}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/channel/71/upstream/variable/fetch", requestBody)
	ctx.Params = gin.Params{{Key: "id", Value: "71"}}
	FetchChannelMonitorCustomVariable(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Variables []service.ChannelMonitorCustomVariable `json:"variables"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	require.Len(t, response.Data.Variables, 1)
	assert.Equal(t, "token", response.Data.Variables[0].Name)
	assert.Equal(t, "draft-token", response.Data.Variables[0].Value)
	assert.NotContains(t, recorder.Body.String(), "must-not-return")
	assert.NotContains(t, recorder.Body.String(), "saved-login-key")
	monitor, err := model.GetChannelRatioMonitor(71)
	require.NoError(t, err)
	assert.Equal(t, raw, monitor.CustomUpstreamConfig)
}
