package service

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorCustomVariablesRefreshOnlyDependentRequests(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	for _, tt := range []struct {
		name, ratioPolicy, ratioValue, balanceValue string
		balanceOnly                                 bool
		wantRatioRefresh, wantBalanceRefresh        int32
	}{
		{name: "每次更新与失败刷新可混用", ratioPolicy: "always", ratioValue: "expired", balanceValue: "expired", wantRatioRefresh: 1, wantBalanceRefresh: 1},
		{name: "余额失效不刷新倍率凭据", ratioPolicy: "on_failure", ratioValue: "ratio-token", balanceValue: "expired", wantBalanceRefresh: 1},
		{name: "倍率失效不刷新余额凭据", ratioPolicy: "on_failure", ratioValue: "expired", balanceValue: "balance-token", wantRatioRefresh: 1},
		{name: "仅查余额忽略倍率的空变量", ratioPolicy: "always", ratioValue: "", balanceValue: "expired", balanceOnly: true, wantBalanceRefresh: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var ratioRefresh, balanceRefresh, unusedRefresh atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/token":
					ratioRefresh.Add(1)
					_, _ = w.Write([]byte(`{"data":{"token":"ratio-token","user":42}}`))
				case "/balance-token":
					balanceRefresh.Add(1)
					_, _ = w.Write([]byte(`{"data":{"token":"balance-token"}}`))
				case "/unused":
					unusedRefresh.Add(1)
					w.WriteHeader(500)
				case "/ratio":
					if r.Header.Get("Authorization") != "Bearer ratio-token" {
						w.WriteHeader(401)
						return
					}
					assert.Equal(t, "42", r.URL.Query().Get("user_id"))
					_, _ = w.Write([]byte(`{"data":{"ratio":3}}`))
				case "/balance":
					if r.URL.Query().Get("ticket") != "balance-token" {
						w.WriteHeader(401)
						return
					}
					_, _ = w.Write([]byte(`{"data":{"balance":90}}`))
				}
			}))
			defer server.Close()
			config := customVariableTestConfig(server.URL, tt.ratioPolicy, tt.ratioValue)
			config.CustomConfig.VariableRequests[0].Variables = append(config.CustomConfig.VariableRequests[0].Variables, ChannelMonitorCustomVariable{Name: "user_id", ValuePath: "data.user", Value: "42"})
			config.CustomConfig.Ratio.Request.Query = []ChannelMonitorCustomKeyValue{{Key: "user_id", ValueTemplate: "{{user_id}}"}}
			balanceRequest := ChannelMonitorCustomVariableRequest{ID: "balance-login", Name: "余额认证", RefreshPolicy: "on_failure", Request: ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/balance-token"}, ResponseType: "json", Variables: []ChannelMonitorCustomVariable{{Name: "balance_token", ValuePath: "data.token", Value: tt.balanceValue}}}
			unusedRequest := ChannelMonitorCustomVariableRequest{ID: "unused", Name: "未引用", RefreshPolicy: "always", Request: ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/unused"}, ResponseType: "json", Variables: []ChannelMonitorCustomVariable{{Name: "unused_token", ValuePath: "data.token"}}}
			config.CustomConfig.VariableRequests = append(config.CustomConfig.VariableRequests, balanceRequest, unusedRequest)
			config.CustomConfig.Balance.Request.Query[0].ValueTemplate = "{{balance_token}}"
			if tt.balanceOnly {
				result, err := FetchChannelMonitorUpstreamBalance(t.Context(), config)
				require.NoError(t, err)
				require.NotNil(t, result.Amount)
				assert.Equal(t, 90.0, *result.Amount)
			} else {
				result, err := FetchChannelMonitorUpstreamGroupRatio(t.Context(), config)
				require.NoError(t, err)
				assert.Equal(t, 3.0, result.Ratio)
				require.NotNil(t, result.Balance.Amount)
				assert.Equal(t, 90.0, *result.Balance.Amount)
			}
			assert.Equal(t, tt.wantRatioRefresh, ratioRefresh.Load())
			assert.Equal(t, tt.wantBalanceRefresh, balanceRefresh.Load())
			assert.Zero(t, unusedRefresh.Load())
		})
	}
}

func TestChannelMonitorCustomVariablesLegacyConfigMigrates(t *testing.T) {
	var legacy ChannelMonitorCustomUpstreamConfig
	err := common.UnmarshalJsonStr(`{"version":1,"ratio":{"source":"fixed","fixed_value":1},"balance":{"source":"fixed","fixed_value":0},"variable_request":{"name":"token","value":"saved-token","has_value":true,"refresh_policy":"on_failure","request":{"method":"GET","path":"/login","body_type":"none"},"result":{"response_type":"json","value_path":"data.token","multiplier":1}}}`, &legacy)
	require.NoError(t, err)
	config, err := NormalizeChannelMonitorCustomUpstreamConfig(legacy)
	require.NoError(t, err)
	require.Len(t, config.VariableRequests, 1)
	assert.Nil(t, config.VariableRequest)
	assert.Equal(t, "legacy-variable", config.VariableRequests[0].ID)
	assert.Equal(t, "saved-token", config.VariableRequests[0].Variables[0].Value)
	assert.Equal(t, "on_failure", config.VariableRequests[0].RefreshPolicy)
	raw, err := MarshalChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	assert.Contains(t, raw, `"variable_requests"`)
	assert.NotContains(t, raw, `"variable_request":`)
	parsed, err := ParseChannelMonitorCustomUpstreamConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, config, parsed)
}

func TestChannelMonitorCustomVariablesRejectDuplicateNamesAndPartialMapping(t *testing.T) {
	config := customVariableTestConfig("https://upstream.example", "always", "saved-token").CustomConfig
	second := config.VariableRequests[0]
	second.ID = "second"
	config.VariableRequests = append(config.VariableRequests, second)
	_, err := NormalizeChannelMonitorCustomUpstreamConfig(config)
	require.ErrorContains(t, err, "重复")
	useChannelMonitorCustomVariableTestClient(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":{"token":"new-token"}}`)) }))
	defer server.Close()
	upstream := customVariableTestConfig(server.URL, "always", "old-token")
	upstream.CustomConfig.VariableRequests[0].Variables = append(upstream.CustomConfig.VariableRequests[0].Variables, ChannelMonitorCustomVariable{Name: "user_id", ValuePath: "data.user_id", Value: "old-id"})
	values, err := FetchChannelMonitorCustomVariables(t.Context(), upstream, "login")
	require.ErrorContains(t, err, "user_id")
	assert.Nil(t, values, "任一映射失败，不得回填其他变量")
	assert.Equal(t, "old-token", upstream.CustomConfig.VariableRequests[0].Variables[0].Value)
}

func TestChannelMonitorCustomVariablesPreserveSecretsAfterReordering(t *testing.T) {
	config := customVariableTestConfig("https://upstream.example", "on_failure", "first-token").CustomConfig
	second := config.VariableRequests[0]
	second.ID, second.Name = "second", "第二个请求"
	second.Variables = []ChannelMonitorCustomVariable{{Name: "other_token", ValuePath: "data.token", Value: "second-token"}}
	config.VariableRequests = append(config.VariableRequests, second)
	normalized, err := NormalizeChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	draft := SanitizeChannelMonitorCustomUpstreamConfig(normalized)
	draft.VariableRequests[0], draft.VariableRequests[1] = draft.VariableRequests[1], draft.VariableRequests[0]
	restored, err := NormalizeChannelMonitorCustomUpstreamConfigWithExisting(draft, &normalized)
	require.NoError(t, err)
	assert.Equal(t, "second-token", restored.VariableRequests[0].Variables[0].Value)
	assert.Equal(t, "first-token", restored.VariableRequests[1].Variables[0].Value)
}
