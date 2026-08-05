package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsModelCapacityError(t *testing.T) {
	tests := []struct {
		name    string
		code    any
		message string
		want    bool
	}{
		{name: "server overloaded code", code: "server_is_overloaded", message: "temporary upstream failure", want: true},
		{name: "capacity exceeded code", code: "model_capacity_exceeded", message: "temporary upstream failure", want: true},
		{name: "capacity message", code: "unknown_error", message: "Selected model is at capacity. Please try a different model.", want: true},
		{name: "case insensitive capacity message", code: "unknown_error", message: "SELECTED MODEL IS AT CAPACITY", want: true},
		{name: "ordinary error", code: "server_error", message: "invalid prompt", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apiErr := WithOpenAIError(OpenAIError{
				Code:    test.code,
				Message: test.message,
			}, http.StatusBadRequest)
			require.NotNil(t, apiErr)
			assert.Equal(t, test.want, IsModelCapacityError(apiErr))
		})
	}

	assert.False(t, IsModelCapacityError(nil))
	assert.True(t, IsModelCapacityError(NewError(errors.New("model is currently at capacity"), ErrorCodeBadResponseStatusCode)))
}
