package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorVariableGroupAPIImportAndDraft(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorVariableGroup{}))
	disableChannelMonitorSSRFProtection(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "saved-password", r.Header.Get("X-Password"))
		_, _ = w.Write([]byte(`{"token":"draft-token","private":"must-not-return"}`))
	}))
	defer server.Close()
	ratio, balance := 1.0, 0.0
	legacy := service.ChannelMonitorCustomUpstreamConfig{
		Ratio:   service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &ratio},
		Balance: service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &balance},
		VariableRequest: &service.ChannelMonitorCustomLegacyVariableRequest{
			Name: "token", Value: "saved-token", RefreshPolicy: "on_failure",
			Request: service.ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/login", Headers: []service.ChannelMonitorCustomKeyValue{{Key: "X-Password", Value: "saved-password", Secret: true}}},
			Result:  service.ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "token"},
		},
	}
	raw, err := service.MarshalChannelMonitorCustomUpstreamConfig(legacy)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 17, UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: server.URL, CustomUpstreamConfig: raw}).Error)
	input := service.ChannelMonitorVariableGroupConfig{Name: "共享凭据", BaseURL: server.URL, RequestTimeout: 30, SourceChannelID: 17, VariableRequests: service.SanitizeChannelMonitorCustomUpstreamConfig(legacy).VariableRequests}
	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/variable_groups", input)
	SaveChannelMonitorVariableGroup(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Success bool                                      `json:"success"`
		Data    service.ChannelMonitorVariableGroupConfig `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.NotContains(t, recorder.Body.String(), "saved-password")
	assert.NotContains(t, recorder.Body.String(), "saved-token")
	group, err := model.GetChannelMonitorVariableGroup(t.Context(), response.Data.ID)
	require.NoError(t, err)
	assert.Contains(t, group.Config, "saved-password")
	assert.Contains(t, group.Config, "saved-token")
	monitor, err := model.GetChannelRatioMonitor(17)
	require.NoError(t, err)
	assert.Equal(t, raw, monitor.CustomUpstreamConfig, "另存不应立即覆盖渠道配置")

	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodGet, "/api/channel_monitor/variable_groups", nil)
	ListChannelMonitorVariableGroups(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "共享凭据")
	assert.NotContains(t, recorder.Body.String(), "saved-password")
	assert.NotContains(t, recorder.Body.String(), "saved-token")

	draft := struct {
		service.ChannelMonitorVariableGroupConfig
		RequestID string `json:"request_id"`
	}{response.Data, "legacy-variable"}
	ctx, recorder = newChannelMonitorControllerContext(t, http.MethodPost, "/api/channel_monitor/variable_groups/fetch", draft)
	FetchChannelMonitorVariableGroupDraft(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "draft-token")
	assert.NotContains(t, recorder.Body.String(), "must-not-return")
	assert.NotContains(t, recorder.Body.String(), "saved-password")
	after, err := model.GetChannelMonitorVariableGroup(t.Context(), group.ID)
	require.NoError(t, err)
	assert.Equal(t, group, after)
}

func TestChannelMonitorVariableGroupReferenceValidation(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelMonitorVariableGroup{}))
	baseURL := "https://upstream.example"
	channel := &model.Channel{Id: 13, BaseURL: &baseURL}
	ratio, balance := 1.0, 0.0
	config := service.ChannelMonitorCustomUpstreamConfig{Ratio: service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &ratio}, Balance: service.ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &balance}, VariableGroupID: 999}
	_, err := resolveChannelMonitorUpstreamRequest(channel, channelMonitorUpstreamRequest{Type: service.CustomUpstreamType, BaseURL: baseURL, CustomConfig: &config}, false)
	require.ErrorContains(t, err, "不存在")
}
