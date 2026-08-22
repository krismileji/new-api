package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayErrorResponseMasksConfiguredGlobalKeywordForUser(t *testing.T) {
	originalKeywords := setGlobalErrorMessageKeywords(t, "secret upstream detail")
	t.Cleanup(func() { setGlobalErrorMessageKeywords(t, originalKeywords) })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(errors.New("secret upstream detail: invalid key"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	writeRelayErrorResponse(c, nil, types.RelayFormatOpenAI, apiErr)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), ": invalid key")
	assert.NotContains(t, recorder.Body.String(), "secret upstream detail")
}

func TestRelayUserVisibleErrorMessageLeavesOriginalErrorForAdminLogs(t *testing.T) {
	originalKeywords := setGlobalErrorMessageKeywords(t, "secret upstream detail")
	t.Cleanup(func() { setGlobalErrorMessageKeywords(t, originalKeywords) })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	apiErr := types.NewOpenAIError(errors.New("secret upstream detail: invalid key"), types.ErrorCodeBadResponse, http.StatusBadGateway)

	message, ok := relayUserVisibleErrorMessage(c, apiErr)

	require.True(t, ok)
	assert.Equal(t, ": invalid key", message)
	assert.Equal(t, "secret upstream detail: invalid key", apiErr.Error())
}

func setGlobalErrorMessageKeywords(t *testing.T, value string) string {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	original := common.OptionMap[service.ErrorMessageKeywordsOptionKey]
	common.OptionMap[service.ErrorMessageKeywordsOptionKey] = value
	common.OptionMapRWMutex.Unlock()
	return original
}
