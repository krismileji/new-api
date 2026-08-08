package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayResponseStarted(c *gin.Context) bool {
	return c != nil && c.Writer != nil && c.Writer.Written()
}

func relayAttemptResponseStarted(c *gin.Context, info *relaycommon.RelayInfo, relayFormat types.RelayFormat) bool {
	if relayFormat == types.RelayFormatOpenAIRealtime {
		return info != nil && info.TargetWs != nil
	}
	return relayResponseStarted(c)
}

func resetRelayAttemptResponseState(c *gin.Context) {
	if c == nil {
		return
	}
	common.SetContextKey(c, service.UpstreamResponseStatusContextKey, 0)
	common.SetContextKey(c, service.UpstreamRequestWrittenContextKey, false)
}

func markAcceptedUpstreamResponseError(c *gin.Context, apiErr *types.NewAPIError) *types.NewAPIError {
	if c == nil || apiErr == nil || types.IsSkipRetryError(apiErr) {
		return apiErr
	}
	if types.IsModelCapacityError(apiErr) {
		return apiErr
	}
	statusCode := common.GetContextKeyInt(c, service.UpstreamResponseStatusContextKey)
	if apiErr.GetErrorCode() == types.ErrorCodeDoRequestFailed {
		return apiErr
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return apiErr
	}
	// A 2xx response means the provider accepted the request even when the
	// gateway cannot decode its body. Mark the error non-retryable so a second
	// POST cannot create a duplicate upstream operation.
	types.ErrOptionWithSkipRetry()(apiErr)
	return apiErr
}

func relayUserVisibleErrorMessage(c *gin.Context, apiErr *types.NewAPIError) (string, bool) {
	if c == nil || apiErr == nil {
		return "", false
	}
	return service.ResolveUserErrorMessage(
		service.GetConfiguredErrorMessageMapping(),
		string(apiErr.GetErrorCode()),
		apiErr.StatusCode,
	)
}

func relayOpenAIErrorForUser(c *gin.Context, apiErr *types.NewAPIError) types.OpenAIError {
	openAIError := apiErr.ToOpenAIError()
	if message, ok := relayUserVisibleErrorMessage(c, apiErr); ok {
		openAIError.Message = message
	}
	return openAIError
}

func relayClaudeErrorForUser(c *gin.Context, apiErr *types.NewAPIError) types.ClaudeError {
	claudeError := apiErr.ToClaudeError()
	if message, ok := relayUserVisibleErrorMessage(c, apiErr); ok {
		claudeError.Message = message
	}
	return claudeError
}

func writeRelayErrorResponse(c *gin.Context, ws *websocket.Conn, relayFormat types.RelayFormat, apiErr *types.NewAPIError) {
	if c == nil || apiErr == nil {
		return
	}
	if relayFormat != types.RelayFormatOpenAIRealtime && relayResponseStarted(c) {
		return
	}
	if relayFormat != types.RelayFormatOpenAIRealtime && c.GetBool(relaycommon.EventStreamHeadersSetContextKey) {
		header := c.Writer.Header()
		header.Del("Content-Type")
		header.Del("Cache-Control")
		header.Del("Connection")
		header.Del("Transfer-Encoding")
		header.Del("X-Accel-Buffering")
		if c.Keys != nil {
			delete(c.Keys, relaycommon.EventStreamHeadersSetContextKey)
		}
	}

	switch relayFormat {
	case types.RelayFormatOpenAIRealtime:
		// A realtime WebSocket is already established before relay errors reach
		// this path, so it is outside the pre-response replacement boundary.
		helper.WssError(c, ws, apiErr.ToOpenAIError())
	case types.RelayFormatClaude:
		c.JSON(apiErr.StatusCode, gin.H{
			"type":  "error",
			"error": relayClaudeErrorForUser(c, apiErr),
		})
	default:
		c.JSON(apiErr.StatusCode, gin.H{
			"error": relayOpenAIErrorForUser(c, apiErr),
		})
	}
}
