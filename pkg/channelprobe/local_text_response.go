package channelprobe

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// WriteLocalTextResponse reuses protocol writers without probe delay or fake
// usage. The caller has already validated the request's text output contract.
func WriteLocalTextResponse(c *gin.Context, request dto.Request, modelName string, config ResponseConfig) {
	c.Header("X-New-Api-Response-Source", "local_response")
	if c.Request.Context().Err() != nil {
		return
	}
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		serveChannelProbeResponse(c, relayconstant.RelayModeChatCompletions, request, config, 0)
	case *dto.OpenAIResponsesRequest:
		now := time.Now().Unix()
		response := buildChannelProbeResponsesStreamResponse(req, config, now, now)
		if config.Truncated {
			response.Status = "incomplete"
			response.CompletedAt = nil
			response.IncompleteDetails = gin.H{"reason": "max_output_tokens"}
			response.Output[0].Status = "incomplete"
		}
		if req.IsStream(c.Request) {
			writeChannelProbeResponsesStream(c, response)
			return
		}
		data, err := common.Marshal(response)
		if err == nil {
			c.Data(http.StatusOK, "application/json; charset=utf-8", data)
		}
	case *dto.ClaudeRequest:
		stopReason := "end_turn"
		if config.Truncated {
			stopReason = "max_tokens"
		}
		usage := gin.H{"input_tokens": config.InputTokens, "output_tokens": config.OutputTokens}
		response := gin.H{"id": "msg_" + common.GetUUID(), "type": "message", "role": "assistant", "model": modelName,
			"content": []any{gin.H{"type": "text", "text": config.ResponseText}}, "stop_reason": stopReason, "stop_sequence": nil, "usage": usage}
		if !req.IsStream(c.Request) {
			data, err := common.Marshal(response)
			if err == nil {
				c.Data(http.StatusOK, "application/json; charset=utf-8", data)
			}
			return
		}
		helper.SetEventStreamHeaders(c)
		start := gin.H{"id": response["id"], "type": "message", "role": "assistant", "model": modelName, "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": gin.H{"input_tokens": config.InputTokens, "output_tokens": 0}}
		events := []gin.H{
			{"type": "message_start", "message": start},
			{"type": "content_block_start", "index": 0, "content_block": gin.H{"type": "text", "text": ""}},
			{"type": "content_block_delta", "index": 0, "delta": gin.H{"type": "text_delta", "text": config.ResponseText}},
			{"type": "content_block_stop", "index": 0},
			{"type": "message_delta", "delta": gin.H{"stop_reason": stopReason, "stop_sequence": nil}, "usage": gin.H{"output_tokens": config.OutputTokens}},
			{"type": "message_stop"},
		}
		for _, event := range events {
			if c.Request.Context().Err() != nil {
				return
			}
			data, err := common.Marshal(event)
			if err != nil {
				return
			}
			if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event["type"], data); err != nil {
				return
			}
			c.Writer.Flush()
		}
	case *dto.GeminiChatRequest:
		finishReason := "STOP"
		if config.Truncated {
			finishReason = "MAX_TOKENS"
		}
		response := gin.H{"candidates": []any{gin.H{"index": 0, "finishReason": finishReason,
			"content": gin.H{"role": "model", "parts": []any{gin.H{"text": config.ResponseText}}}}},
			"modelVersion": modelName, "responseId": common.GetUUID(),
			"usageMetadata": gin.H{"promptTokenCount": config.InputTokens, "candidatesTokenCount": config.OutputTokens, "totalTokenCount": config.TotalTokens()}}
		var payload any = response
		stream := req.IsStream(c.Request)
		if stream && c.Query("alt") != "sse" {
			payload = []any{response}
		}
		data, err := common.Marshal(payload)
		if err != nil {
			return
		}
		if stream && c.Query("alt") == "sse" {
			helper.SetEventStreamHeaders(c)
			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
				return
			}
			c.Writer.Flush()
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	}
}
