package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// resolveRelayUserVisibleErrorMessage bypasses processing for whitelisted error
// codes, removes globally configured keywords when they match, and otherwise
// falls back to the global error-message mapping. Matching is checked against
// the original error so administrators retain it.
func resolveRelayUserVisibleErrorMessage(c *gin.Context, raw string, errorCode string, statusCode int) (string, bool) {
	if service.ShouldBypassErrorMessageHandling(errorCode, statusCode) {
		if strings.TrimSpace(raw) != "" {
			return raw, true
		}
		return "", false
	}
	if message, matched := service.ResolveConfiguredErrorMessage(raw); matched {
		return message, true
	}
	return service.ResolveUserErrorMessage(
		service.GetConfiguredErrorMessageMapping(),
		errorCode,
		statusCode,
	)
}
