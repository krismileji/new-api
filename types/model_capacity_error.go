package types

import (
	"fmt"
	"strings"
)

var modelCapacityErrorCodes = map[string]struct{}{
	"server_is_overloaded":    {},
	"server_overloaded":       {},
	"model_capacity_exceeded": {},
	"model_at_capacity":       {},
	"model_overloaded":        {},
}

// IsModelCapacityError identifies an upstream capacity response that can be
// retried on another channel, including responses that use a successful HTTP
// status and report the failure inside the response payload.
func IsModelCapacityError(err *NewAPIError) bool {
	if err == nil {
		return false
	}

	if isModelCapacityErrorCode(string(err.GetErrorCode())) {
		return true
	}

	messages := []string{err.Error()}
	switch relayError := err.RelayError.(type) {
	case OpenAIError:
		if isModelCapacityErrorCode(fmt.Sprintf("%v", relayError.Code)) || isModelCapacityErrorCode(relayError.Type) {
			return true
		}
		messages = append(messages, relayError.Message)
	case *OpenAIError:
		if relayError != nil {
			if isModelCapacityErrorCode(fmt.Sprintf("%v", relayError.Code)) || isModelCapacityErrorCode(relayError.Type) {
				return true
			}
			messages = append(messages, relayError.Message)
		}
	}

	for _, message := range messages {
		message = strings.ToLower(strings.TrimSpace(message))
		if strings.Contains(message, "at capacity") && strings.Contains(message, "model") {
			return true
		}
	}

	return false
}

func isModelCapacityErrorCode(code string) bool {
	_, ok := modelCapacityErrorCodes[strings.ToLower(strings.TrimSpace(code))]
	return ok
}
