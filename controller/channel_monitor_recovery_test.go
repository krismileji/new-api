package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorRecoveryBeforeBackgroundCheck(t *testing.T) {
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/channel_monitor/health", nil)
	GetChannelMonitorRecovery(context)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Success bool                           `json:"success"`
		Data    service.ChannelMonitorRecovery `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Equal(t, "checking", payload.Data.RecoveryStatus)
	assert.False(t, payload.Data.RecoveryConfirmed)
	assert.NotEqual(t, service.ChannelMonitorHealthHealthy, payload.Data.Status)
}
