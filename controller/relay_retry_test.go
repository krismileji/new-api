package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormerDedicatedRetryErrorsUseConfiguredStatusRules(t *testing.T) {
	originalRanges := operation_setting.AutomaticRetryStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = originalRanges
	})
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: 502, End: 503},
	}

	tests := []struct {
		name       string
		statusCode int
		message    string
		want       bool
	}{
		{name: "400 upstream failed", statusCode: 400, message: "Upstream request failed", want: false},
		{name: "502", statusCode: 502, message: "bad gateway", want: true},
		{name: "503", statusCode: 503, message: "upstream unavailable", want: true},
		{name: "524", statusCode: 524, message: "upstream timeout", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			apiErr := types.NewOpenAIError(errors.New(test.message), types.ErrorCodeBadResponseStatusCode, test.statusCode)

			assert.Equal(t, test.want, shouldRetry(ctx, apiErr, 1))
			assert.False(t, shouldRetry(ctx, apiErr, 0))
		})
	}
}

func TestShouldRetryHonorsSkipRetryAndSpecificChannel(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.NewErrorWithStatusCode(
		errors.New("upstream unavailable"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
	require.False(t, shouldRetry(ctx, apiErr, 1))

	ctx.Set("specific_channel_id", "2")
	apiErr = types.NewOpenAIError(errors.New("upstream unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
	require.False(t, shouldRetry(ctx, apiErr, 1))
}

func TestRetryStopsOnlyForClassifiedClientGone(t *testing.T) {
	originalRanges := operation_setting.AutomaticRetryStatusCodeRanges
	t.Cleanup(func() {
		operation_setting.AutomaticRetryStatusCodeRanges = originalRanges
	})
	operation_setting.AutomaticRetryStatusCodeRanges = []operation_setting.StatusCodeRange{
		{Start: http.StatusServiceUnavailable, End: http.StatusServiceUnavailable},
	}

	requestContext, cancel := context.WithCancel(context.Background())
	cancel()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
	upstreamErr := types.NewOpenAIError(errors.New("upstream unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
	assert.True(t, shouldRetry(ctx, upstreamErr, 1))
	assert.False(t, shouldRetry(ctx, types.NewClientGoneError(context.Canceled), 1))

	assert.True(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{
		StatusCode: http.StatusServiceUnavailable,
		Error:      errors.New("upstream unavailable"),
	}, 1))
	assert.False(t, shouldRetryTaskRelay(ctx, 1, &dto.TaskError{
		StatusCode: http.StatusInternalServerError,
		Error:      types.NewClientGoneError(context.Canceled),
	}, 1))
}

func TestShouldRetry500UsesConfiguredBudget(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.NewOpenAIError(errors.New("internal server error"), types.ErrorCodeBadResponseStatusCode, 500)

	require.True(t, shouldRetry(ctx, apiErr, 1))
	require.False(t, shouldRetry(ctx, apiErr, 0))
}

func TestShouldRetryModelCapacityErrorRegardlessOfUpstreamStatus(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	message := "Selected model is at capacity. Please try a different model."

	for _, statusCode := range []int{http.StatusOK, http.StatusBadRequest} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			apiErr := types.WithOpenAIError(types.OpenAIError{
				Type:    "server_error",
				Code:    "server_is_overloaded",
				Message: message,
			}, statusCode)
			require.True(t, shouldRetry(ctx, apiErr, 1))
			require.False(t, shouldRetry(ctx, apiErr, 0))
		})
	}
}
