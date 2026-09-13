package service

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func customActionTestRule() ChannelMonitorCustomAction {
	threshold := 10.0
	return ChannelMonitorCustomAction{ID: "reset", Name: "余额重置", Enabled: true, Metric: "balance", Operator: "lt", Threshold: &threshold, Timezone: "Asia/Shanghai", StartTime: "00:05", EndTime: "23:00", DailyLimit: 1, CooldownMinutes: 60, Request: ChannelMonitorCustomRequestConfig{Method: "POST", Path: "/reset", BodyType: "json", Body: `{"reset":true}`}, SuccessPath: "success", SuccessValue: "true"}
}

func TestChannelMonitorCustomActionValidation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*ChannelMonitorCustomAction)
	}{
		{"缺失阈值", func(a *ChannelMonitorCustomAction) { a.Threshold = nil }},
		{"无效阈值", func(a *ChannelMonitorCustomAction) { v := math.Inf(1); a.Threshold = &v }},
		{"未知指标", func(a *ChannelMonitorCustomAction) { a.Metric = "cost" }},
		{"未指定时区", func(a *ChannelMonitorCustomAction) { a.Timezone = "" }},
		{"依赖本机时区", func(a *ChannelMonitorCustomAction) { a.Timezone = "Local" }},
		{"跨日时段", func(a *ChannelMonitorCustomAction) { a.StartTime = "23:00"; a.EndTime = "01:00" }},
		{"无效时间", func(a *ChannelMonitorCustomAction) { a.EndTime = "24:00" }},
		{"零次数", func(a *ChannelMonitorCustomAction) { a.DailyLimit = 0 }},
		{"负冷却", func(a *ChannelMonitorCustomAction) { a.CooldownMinutes = -1 }},
		{"倍率过大", func(a *ChannelMonitorCustomAction) { a.Metric = "ratio"; v := 1e7; a.Threshold = &v }},
		{"无效请求", func(a *ChannelMonitorCustomAction) { a.Request.Path = "https://other.example/reset" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			action := customActionTestRule()
			tt.change(&action)
			_, err := normalizeChannelMonitorCustomActions([]ChannelMonitorCustomAction{action}, nil)
			require.Error(t, err)
		})
	}
	zero := 0.0
	action := customActionTestRule()
	action.Threshold = &zero
	_, err := normalizeChannelMonitorCustomActions([]ChannelMonitorCustomAction{action}, nil)
	require.NoError(t, err, "显式零阈值有效")
}

func TestChannelMonitorCustomActionThresholdAndWindow(t *testing.T) {
	for _, tt := range []struct {
		operator string
		value    float64
		want     bool
	}{
		{"lt", 9, true}, {"lt", 10, false}, {"lte", 10, true}, {"gt", 10, false}, {"gt", 11, true}, {"gte", 10, true}, {"lt", math.NaN(), false},
	} {
		action := customActionTestRule()
		action.Operator = tt.operator
		assert.Equal(t, tt.want, action.matches(tt.value), "%s %v", tt.operator, tt.value)
	}
	for _, tt := range []struct {
		utc     string
		allowed bool
		day     string
	}{
		{"2026-09-13T14:59:59Z", true, "2026-09-13"},
		{"2026-09-13T15:00:00Z", false, "2026-09-13"},
		{"2026-09-13T16:00:00Z", false, "2026-09-14"},
		{"2026-09-13T16:05:00Z", true, "2026-09-14"},
	} {
		now, err := time.Parse(time.RFC3339, tt.utc)
		require.NoError(t, err)
		day, cutoff, allowed := customActionTestRule().executionWindow(now)
		assert.Equal(t, tt.allowed, allowed, tt.utc)
		assert.Equal(t, tt.day, day)
		assert.Equal(t, 23, cutoff.Hour())
	}
}

func TestChannelMonitorCustomActionCredentials(t *testing.T) {
	config := customVariableTestConfig("https://upstream.example", "on_failure", "token").CustomConfig
	action := customActionTestRule()
	action.Request.Headers = []ChannelMonitorCustomKeyValue{{Key: "Authorization", Value: "reset-secret"}}
	action.Request.BodySecret = true
	config.Actions = []ChannelMonitorCustomAction{action}
	config, err := NormalizeChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	sanitized := SanitizeChannelMonitorCustomUpstreamConfig(config)
	assert.Empty(t, sanitized.Actions[0].Request.Headers[0].Value)
	assert.Empty(t, sanitized.Actions[0].Request.Body)
	require.True(t, sanitized.Actions[0].Request.Headers[0].HasValue)
	restored, err := NormalizeChannelMonitorCustomUpstreamConfigWithExisting(sanitized, &config)
	require.NoError(t, err)
	assert.Equal(t, "reset-secret", restored.Actions[0].Request.Headers[0].Value)
	assert.Equal(t, `{"reset":true}`, restored.Actions[0].Request.Body)
	sanitized.Actions[0].BaseURL = "https://other.example"
	_, err = NormalizeChannelMonitorCustomUpstreamConfigWithExisting(sanitized, &config)
	require.Error(t, err, "更换执行目标不能自动带出旧密钥")
	config.Actions[0].Request.Headers = []ChannelMonitorCustomKeyValue{{Key: "Authorization", ValueTemplate: "Bearer {{missing}}"}}
	_, err = NormalizeChannelMonitorCustomUpstreamConfig(config)
	require.ErrorContains(t, err, "missing")
}
