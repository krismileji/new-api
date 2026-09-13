package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelMonitorDiagnosticsTestResponse struct {
	Success bool                              `json:"success"`
	Message string                            `json:"message"`
	Data    service.ChannelMonitorDiagnostics `json:"data"`
}

func requestChannelMonitorDiagnostics(t *testing.T, handler gin.HandlerFunc, method, path, body string) channelMonitorDiagnosticsTestResponse {
	t.Helper()
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	handler(context)
	require.Equal(t, http.StatusOK, response.Code)
	var payload channelMonitorDiagnosticsTestResponse
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	return payload
}

func TestChannelMonitorDiagnosticsAPIResetRequiresCurrentDay(t *testing.T) {
	server := miniredis.RunT(t)
	server.SetTime(time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC))
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousEnabled, previousClient := common.RedisEnabled, common.RDBMonitorWrite
	common.RedisEnabled, common.RDBMonitorWrite = true, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDBMonitorWrite = previousEnabled, previousClient
		require.NoError(t, client.Close())
	})
	initial := requestChannelMonitorDiagnostics(t, GetChannelMonitorDiagnostics, http.MethodGet, "/api/channel_monitor/diagnostics", "")
	require.True(t, initial.Success)
	assert.Zero(t, initial.Data.LastResetAt)
	for _, body := range []string{"invalid-json", "{}", `{"day_start":1}`} {
		rejected := requestChannelMonitorDiagnostics(t, ResetChannelMonitorDiagnostics, http.MethodPost, "/api/channel_monitor/diagnostics/reset", body)
		assert.False(t, rejected.Success)
		assert.NotEmpty(t, rejected.Message)
	}
	unchanged := requestChannelMonitorDiagnostics(t, GetChannelMonitorDiagnostics, http.MethodGet, "/api/channel_monitor/diagnostics", "")
	require.True(t, unchanged.Success)
	assert.Zero(t, unchanged.Data.LastResetAt)
	body, err := common.Marshal(map[string]int64{"day_start": initial.Data.DayStart})
	require.NoError(t, err)
	reset := requestChannelMonitorDiagnostics(t, ResetChannelMonitorDiagnostics, http.MethodPost, "/api/channel_monitor/diagnostics/reset", string(body))
	require.True(t, reset.Success)
	assert.Equal(t, initial.Data.DayStart, reset.Data.DayStart)
	assert.Equal(t, initial.Data.ObservedAt, reset.Data.LastResetAt)
	assert.Zero(t, reset.Data.RetryCount)
}

func TestChannelMonitorDiagnosticsAPIRedisUnavailableDoesNotClaimZeroCounts(t *testing.T) {
	previousEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousEnabled })
	read := requestChannelMonitorDiagnostics(t, GetChannelMonitorDiagnostics, http.MethodGet, "/api/channel_monitor/diagnostics", "")
	assert.False(t, read.Success)
	assert.Contains(t, read.Message, "读取失败")
	reset := requestChannelMonitorDiagnostics(t, ResetChannelMonitorDiagnostics, http.MethodPost, "/api/channel_monitor/diagnostics/reset", `{"day_start":1}`)
	assert.False(t, reset.Success)
	assert.Contains(t, reset.Message, "重置今日诊断失败")
}
