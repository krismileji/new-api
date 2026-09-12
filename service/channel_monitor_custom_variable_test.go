package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func customVariableTestConfig(baseURL, policy, value string) ChannelMonitorUpstreamConfig {
	return ChannelMonitorUpstreamConfig{
		Type: CustomUpstreamType, BaseURL: baseURL, CustomDebug: true,
		CustomConfig: ChannelMonitorCustomUpstreamConfig{
			Ratio: ChannelMonitorCustomMetricConfig{
				Source:  ChannelMonitorCustomSourceHTTP,
				Request: &ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/ratio", Headers: []ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{token}}"}}},
				Result:  &ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "data.ratio", Multiplier: 1},
			},
			Balance: ChannelMonitorCustomMetricConfig{
				Source:  ChannelMonitorCustomSourceHTTP,
				Request: &ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/balance", Query: []ChannelMonitorCustomKeyValue{{Key: "ticket", ValueTemplate: "{{token}}"}}},
				Result:  &ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: "data.balance", Multiplier: 1},
			},
			VariableRequests: []ChannelMonitorCustomVariableRequest{{
				ID: "login", Name: "登录", RefreshPolicy: policy,
				Request:      ChannelMonitorCustomRequestConfig{Method: "POST", Path: "/token", BodyType: "form", Form: []ChannelMonitorCustomKeyValue{{Key: "password", Value: "login-password"}}},
				ResponseType: "json", Variables: []ChannelMonitorCustomVariable{{Name: "token", Value: value, ValuePath: "data.token"}},
			}},
		},
	}
}

func useChannelMonitorCustomVariableTestClient(t *testing.T) {
	t.Helper()
	useChannelMonitorCustomTestFetchSettings(t)
	original := httpClient
	httpClient = &http.Client{}
	t.Cleanup(func() { httpClient = original })
}

func TestChannelMonitorCustomVariableRefreshStrategies(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	for _, tt := range []struct {
		name, policy, initial                string
		balanceOnly, reuse, applicationError bool
		wantRefresh, wantRatio, wantBalance  int32
	}{
		{name: "每次更新先获取", policy: "always", initial: "fresh+&=token", wantRefresh: 1, wantRatio: 1, wantBalance: 1},
		{name: "有效值不刷新", policy: "on_failure", initial: "fresh+&=token", wantRatio: 1, wantBalance: 1},
		{name: "失效后共用新值重试", policy: "on_failure", initial: "expired", wantRefresh: 1, wantRatio: 2, wantBalance: 2},
		{name: "未填初始值先获取", policy: "on_failure", wantRefresh: 1, wantRatio: 1, wantBalance: 1},
		{name: "只更新余额也刷新", policy: "on_failure", initial: "expired", balanceOnly: true, wantRefresh: 1, wantBalance: 2},
		{name: "共用接口只重试一次", policy: "on_failure", initial: "expired", reuse: true, wantRefresh: 1, wantRatio: 2},
		{name: "业务响应取值失败也刷新", policy: "on_failure", initial: "expired", applicationError: true, wantRefresh: 1, wantRatio: 2, wantBalance: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var refreshCalls, ratioCalls, balanceCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					refreshCalls.Add(1)
					assert.Equal(t, "POST", r.Method)
					assert.NoError(t, r.ParseForm())
					assert.Equal(t, "login-password", r.PostForm.Get("password"))
					_, _ = w.Write([]byte(`{"data":{"token":"fresh+&=token"}}`))
					return
				}
				var token string
				if r.URL.Path == "/ratio" {
					ratioCalls.Add(1)
					token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				}
				if r.URL.Path == "/balance" {
					balanceCalls.Add(1)
					token = r.URL.Query().Get("ticket")
				}
				if token != "fresh+&=token" {
					if !tt.applicationError {
						w.WriteHeader(http.StatusUnauthorized)
					}
					_, _ = w.Write([]byte(`{"error":"expired"}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":{"ratio":2.5,"balance":80},"echo":"fresh+&=token"}`))
			}))
			defer server.Close()
			config := customVariableTestConfig(server.URL, tt.policy, tt.initial)
			config.CustomConfig.BalanceReuseRatioRequest = tt.reuse
			if tt.balanceOnly {
				result, err := FetchChannelMonitorUpstreamBalance(t.Context(), config)
				require.NoError(t, err)
				require.NotNil(t, result.Amount)
				assert.Equal(t, 80.0, *result.Amount)
			} else {
				result, err := FetchChannelMonitorUpstreamGroupRatio(t.Context(), config)
				require.NoError(t, err)
				assert.Equal(t, 2.5, result.Ratio)
				require.NotNil(t, result.Balance.Amount)
				assert.Equal(t, 80.0, *result.Balance.Amount)
				require.NotNil(t, result.Debug)
				assert.NotContains(t, result.Debug.ResponsePreview, "fresh+&=token")
			}
			assert.Equal(t, tt.wantRefresh, refreshCalls.Load())
			assert.Equal(t, tt.wantRatio, ratioCalls.Load())
			assert.Equal(t, tt.wantBalance, balanceCalls.Load())
			assert.Equal(t, tt.initial, config.CustomConfig.VariableRequests[0].Variables[0].Value, "草稿请求不能修改调用者的配置")
		})
	}
}

func TestChannelMonitorCustomVariableRetryStopsAndRedactsErrors(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	for _, refreshFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "重试失败不循环", true: "独立请求失败不泄露响应"}[refreshFails], func(t *testing.T) {
			var refreshCalls, metricCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					refreshCalls.Add(1)
					if refreshFails {
						w.WriteHeader(403)
					}
					_, _ = w.Write([]byte(`{"data":{"token":"private-returned-token"},"password":"login-password"}`))
					return
				}
				metricCalls.Add(1)
				w.WriteHeader(401)
				_, _ = w.Write([]byte("private-returned-token login-password expired"))
			}))
			defer server.Close()
			config := customVariableTestConfig(server.URL, "on_failure", "expired")
			config.SkipBalance = true
			_, err := FetchChannelMonitorUpstreamGroupRatio(t.Context(), config)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "private-returned-token")
			assert.NotContains(t, err.Error(), "login-password")
			assert.EqualValues(t, 1, refreshCalls.Load())
			wantCalls := int32(2)
			if refreshFails {
				wantCalls = 1
			}
			assert.Equal(t, wantCalls, metricCalls.Load())
		})
	}
}

func TestChannelMonitorCustomVariablePreservesSecretsAndTemplates(t *testing.T) {
	config := customVariableTestConfig("https://upstream.example", "on_failure", "saved-token").CustomConfig
	normalized, err := NormalizeChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	sanitized := SanitizeChannelMonitorCustomUpstreamConfig(normalized)
	encoded, err := common.Marshal(sanitized)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "saved-token")
	assert.NotContains(t, string(encoded), "login-password")
	assert.Equal(t, "Bearer {{token}}", sanitized.Ratio.Request.Headers[0].ValueTemplate)
	assert.True(t, sanitized.VariableRequests[0].Variables[0].HasValue)
	restored, err := NormalizeChannelMonitorCustomUpstreamConfigWithExisting(sanitized, &normalized)
	require.NoError(t, err)
	assert.Equal(t, normalized, restored)
	sanitized.VariableRequests[0].BaseURL = "https://other.example"
	_, err = NormalizeChannelMonitorCustomUpstreamConfigWithExisting(sanitized, &normalized)
	require.Error(t, err, "变更独立请求主机后不能沿用隐藏的登录密码")
}

func TestChannelMonitorCustomVariableRejectsInvalidMappingAndTemplates(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*ChannelMonitorCustomUpstreamConfig)
	}{
		{"未知变量", func(c *ChannelMonitorCustomUpstreamConfig) { c.Ratio.Request.Headers[0].ValueTemplate = "{{missing}}" }},
		{"模板语法错误", func(c *ChannelMonitorCustomUpstreamConfig) { c.Ratio.Request.Headers[0].ValueTemplate = "{{token}" }},
		{"不允许循环依赖", func(c *ChannelMonitorCustomUpstreamConfig) {
			c.VariableRequests[0].Request.Headers = []ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "{{token}}"}}
		}},
		{"不能向请求头注入换行", func(c *ChannelMonitorCustomUpstreamConfig) {
			c.VariableRequests[0].Variables[0].Value = "token\r\nOther: injected"
		}},
		{"无效策略", func(c *ChannelMonitorCustomUpstreamConfig) { c.VariableRequests[0].RefreshPolicy = "never" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := customVariableTestConfig("https://upstream.example", "on_failure", "token").CustomConfig
			tt.change(&config)
			_, err := NormalizeChannelMonitorCustomUpstreamConfig(config)
			require.Error(t, err)
		})
	}
}

func TestChannelMonitorCustomVariableDraftExtraction(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	for _, tt := range []struct {
		name, body, responseType, want string
		fails                          bool
	}{
		{name: "JSON字符串", body: `{"data":{"token":"new-token"}}`, responseType: "json", want: "new-token"},
		{name: "JSON零值", body: `{"data":{"token":0}}`, responseType: "json", want: "0"},
		{name: "纯文本", body: "new-token\n", responseType: "text", want: "new-token"},
		{name: "JSON空值", body: `{"data":{"token":null}}`, responseType: "json", fails: true},
		{name: "JSON对象", body: `{"data":{"token":{}}}`, responseType: "json", fails: true},
		{name: "缺少映射", body: `{"data":{}}`, responseType: "json", fails: true},
		{name: "映射为空", body: `{"data":{"token":""}}`, responseType: "json", fails: true},
		{name: "拒绝换行", body: `{"data":{"token":"token\r\nX: injected"}}`, responseType: "json", fails: true},
		{name: "拒绝超长值", body: strings.Repeat("x", 8193), responseType: "text", fails: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tt.body)) }))
			defer server.Close()
			config := customVariableTestConfig("https://unused.example", "on_failure", "old")
			config.CustomConfig.VariableRequests[0].BaseURL = server.URL
			config.CustomConfig.VariableRequests[0].ResponseType = tt.responseType
			value, err := FetchChannelMonitorCustomVariables(context.Background(), config, "login")
			if tt.fails {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, value[0].Value)
		})
	}
}

func TestChannelMonitorCustomVariableHonorsFetchProtectionAndCancellation(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	config := customVariableTestConfig("https://upstream.example", "always", "")
	config.CustomConfig.VariableRequests[0].BaseURL = server.URL
	previousProtectedClient := ssrfProtectedHTTPClient
	ssrfProtectedHTTPClient = newProtectedFetchHTTPClient()
	t.Cleanup(func() { ssrfProtectedHTTPClient = previousProtectedClient })
	*system_setting.GetFetchSetting() = system_setting.FetchSetting{EnableSSRFProtection: true, AllowPrivateIp: false, AllowedPorts: []string{"1-65535"}}
	_, err := FetchChannelMonitorCustomVariables(t.Context(), config, "login")
	require.Error(t, err)
	assert.Zero(t, calls.Load())
	system_setting.GetFetchSetting().EnableSSRFProtection = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = FetchChannelMonitorCustomVariables(ctx, config, "login")
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, calls.Load())
}

func TestChannelMonitorCustomVariableSkipsUnusedRequest(t *testing.T) {
	useChannelMonitorCustomVariableTestClient(t)
	var loginCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { loginCalls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	config := customVariableTestConfig(server.URL, "always", "")
	fixed := 8.0
	config.CustomConfig.Ratio = ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &fixed}
	config.CustomConfig.Balance = ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: &fixed}
	result, err := FetchChannelMonitorUpstreamGroupRatio(t.Context(), config)
	require.NoError(t, err)
	assert.Equal(t, fixed, result.Ratio)
	assert.Zero(t, loginCalls.Load())
}
