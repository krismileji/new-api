package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// resolveRelayUserVisibleErrorMessage bypasses processing for whitelisted error
// codes, applies the configured error-message mapping, and then removes
// configured keywords from the resulting user-visible message. Matching is
// checked against the original error so administrators retain it.
func resolveRelayUserVisibleErrorMessage(c *gin.Context, raw string, errorCode string, statusCode int) (string, bool) {
	if service.ShouldBypassErrorMessageHandling(errorCode, statusCode) {
		if strings.TrimSpace(raw) != "" {
			return raw, true
		}
		return "", false
	}

	// Mapping and keyword masking are cumulative. A matching keyword must not
	// short-circuit a more specific error-code/status mapping.
	mappedMessage, mappingMatched := service.ResolveUserErrorMessage(
		service.GetConfiguredErrorMessageMapping(),
		errorCode,
		statusCode,
	)
	visibleMessage := raw
	if mappingMatched {
		visibleMessage = mappedMessage
	}
	if maskedMessage, keywordMatched := service.ResolveConfiguredErrorMessage(visibleMessage); keywordMatched {
		return maskedMessage, true
	}
	if mappingMatched {
		return visibleMessage, true
	}
	return "", false
}
