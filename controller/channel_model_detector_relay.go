package controller

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ChannelModelDetectorRelayHandler is the narrow HTTP boundary used by the
// separately deployed official detector. It intentionally receives a fully
// constructed service relay so route registration cannot silently fall back
// to ordinary user-token or channel-selection behavior.
type ChannelModelDetectorRelayHandler struct {
	Relay           service.ChannelModelDetectorRelayRunner
	MaxRequestBytes int64
}

func NewChannelModelDetectorRelayHandler(relay service.ChannelModelDetectorRelayRunner) *ChannelModelDetectorRelayHandler {
	return &ChannelModelDetectorRelayHandler{
		Relay:           relay,
		MaxRequestBytes: service.ChannelModelDetectorRelayMaxRequestBytes,
	}
}

// PostChannelModelDetectorRelay handles POST /internal/model-detector/v1/responses.
// The caller is the official detector, not a browser or a normal API client.
func (handler *ChannelModelDetectorRelayHandler) PostChannelModelDetectorRelay(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	if handler == nil || handler.Relay == nil {
		writeChannelModelDetectorRelayError(c, http.StatusServiceUnavailable, "模型检测固定渠道暂不可用")
		return
	}

	bearer, ok := channelModelDetectorBearer(c.GetHeader("Authorization"))
	if !ok {
		writeChannelModelDetectorRelayError(c, http.StatusUnauthorized, "模型检测任务凭证无效")
		return
	}
	if !channelModelDetectorJSONContentType(c.GetHeader("Content-Type")) {
		writeChannelModelDetectorRelayError(c, http.StatusUnsupportedMediaType, "模型检测请求必须使用 JSON")
		return
	}

	maxBytes := handler.MaxRequestBytes
	if maxBytes <= 0 {
		maxBytes = service.ChannelModelDetectorRelayMaxRequestBytes
	}
	body, err := readChannelModelDetectorRequestBody(c.Request.Body, maxBytes)
	if err != nil {
		if errors.Is(err, common.ErrRequestBodyTooLarge) {
			writeChannelModelDetectorRelayError(c, http.StatusRequestEntityTooLarge, "模型检测请求体过大")
			return
		}
		writeChannelModelDetectorRelayError(c, http.StatusBadRequest, "模型检测请求体无法读取")
		return
	}

	requestID := channelModelDetectorRequestID(c)
	result, executeErr := handler.Relay.Execute(c.Request.Context(), service.ChannelModelDetectorRelayRequest{
		BearerToken:       bearer,
		DetectorRequestID: requestID,
		Body:              body,
	})
	if executeErr != nil {
		writeChannelModelDetectorRelayExecutionError(c, executeErr)
		return
	}

	upstream := result.Upstream
	if upstream.StatusCode <= 0 {
		upstream.StatusCode = http.StatusOK
	}
	if upstream.ContentType != "" {
		c.Header("Content-Type", upstream.ContentType)
	}
	c.Header("X-Request-Id", firstNonEmpty(upstream.RequestID, requestID))
	c.Status(upstream.StatusCode)
	if len(upstream.ResponseBody) > 0 {
		_, _ = c.Writer.Write(upstream.ResponseBody)
	}
}

func channelModelDetectorRequestID(c *gin.Context) string {
	if c != nil {
		for _, header := range []string{"X-GPT56-Request-Id", "Idempotency-Key"} {
			value := strings.TrimSpace(c.GetHeader(header))
			if value != "" && len(value) <= 256 {
				return value
			}
		}
	}
	return common.GetUUID()
}

func channelModelDetectorBearer(header string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	return parts[1], true
}

func channelModelDetectorJSONContentType(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return false
	}
	mediaType := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
	return mediaType == "application/json" || mediaType == "application/json-seq"
}

func readChannelModelDetectorRequestBody(body io.Reader, maxBytes int64) ([]byte, error) {
	if body == nil || maxBytes <= 0 {
		return nil, common.ErrRequestBodyTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, common.ErrRequestBodyTooLarge
	}
	return bytes.TrimSpace(data), nil
}

func writeChannelModelDetectorRelayExecutionError(c *gin.Context, err error) {
	statusCode := http.StatusBadGateway
	message := "模型检测渠道请求失败"
	var upstreamErr *relaytypes.NewAPIError
	switch {
	case errors.Is(err, service.ErrChannelModelDetectorRelayInvalidRequest):
		statusCode, message = http.StatusBadRequest, "模型检测请求无效"
	case errors.Is(err, service.ErrChannelModelDetectorTokenInvalid),
		errors.Is(err, service.ErrChannelModelDetectorTokenExpired),
		errors.Is(err, service.ErrChannelModelDetectorTokenRevoked),
		errors.Is(err, service.ErrChannelModelDetectorTokenModelMismatch):
		statusCode, message = http.StatusUnauthorized, "模型检测任务凭证无效"
	case errors.Is(err, service.ErrChannelModelDetectorTokenReplay):
		statusCode, message = http.StatusConflict, "模型检测请求已处理"
	case errors.Is(err, service.ErrChannelModelDetectorTokenBudgetExceeded),
		errors.Is(err, service.ErrChannelModelDetectorRelayBusy):
		statusCode, message = http.StatusTooManyRequests, "模型检测请求暂时无法发送"
	case errors.As(err, &upstreamErr) && upstreamErr.StatusCode == http.StatusTooManyRequests:
		statusCode = http.StatusTooManyRequests
	case errors.Is(err, service.ErrChannelModelDetectorRelayUnavailable):
		statusCode, message = http.StatusServiceUnavailable, "模型检测固定渠道暂不可用"
	}
	writeChannelModelDetectorRelayError(c, statusCode, message)
}

func writeChannelModelDetectorRelayError(c *gin.Context, statusCode int, message string) {
	if c == nil {
		return
	}
	if statusCode < http.StatusBadRequest || statusCode > 599 {
		statusCode = http.StatusBadGateway
	}
	c.Header("Content-Type", "application/json")
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "model_detector_relay_error",
			"code":    statusCode,
		},
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
