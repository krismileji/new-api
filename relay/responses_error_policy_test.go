package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
)

func TestApplyResponsesUpstreamErrorPolicy(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		message        string
		skipRetry      bool
		recordErrorLog bool
	}{
		{
			name:           "image generation group restriction",
			statusCode:     http.StatusForbidden,
			message:        "Image generation is not enabled for this group",
			skipRetry:      true,
			recordErrorLog: false,
		},
		{
			name:           "message match is case insensitive",
			statusCode:     http.StatusForbidden,
			message:        "upstream error: IMAGE GENERATION IS NOT ENABLED FOR THIS GROUP",
			skipRetry:      true,
			recordErrorLog: false,
		},
		{
			name:           "other forbidden response",
			statusCode:     http.StatusForbidden,
			message:        "permission denied",
			skipRetry:      false,
			recordErrorLog: true,
		},
		{
			name:           "same message with another status",
			statusCode:     http.StatusBadRequest,
			message:        "Image generation is not enabled for this group",
			skipRetry:      false,
			recordErrorLog: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiErr := types.NewOpenAIError(
				errors.New(test.message),
				types.ErrorCodeBadResponseStatusCode,
				test.statusCode,
			)

			applyResponsesUpstreamErrorPolicy(apiErr)

			assert.Equal(t, test.skipRetry, types.IsSkipRetryError(apiErr))
			assert.Equal(t, test.recordErrorLog, types.IsRecordErrorLog(apiErr))
			assert.Equal(t, test.statusCode, apiErr.StatusCode)
			assert.Equal(t, test.message, apiErr.Error())
		})
	}
}
