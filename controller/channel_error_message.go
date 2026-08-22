package controller

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// resolveRelayUserVisibleErrorMessage removes globally configured keywords when
// they match; otherwise it falls back to the global error-message mapping.
// Matching is checked against the original error so administrators retain it.
func resolveRelayUserVisibleErrorMessage(c *gin.Context, raw string, errorCode string, statusCode int) (string, bool) {
	if message, matched := service.ResolveConfiguredErrorMessage(raw); matched {
		return message, true
	}
	return service.ResolveUserErrorMessage(
		service.GetConfiguredErrorMessageMapping(),
		errorCode,
		statusCode,
	)
}
