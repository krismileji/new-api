package channelprobe

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

const (
	validatedRequestContextKey = "channel_probe_validated_request"
)

type channelProbeResponsesInput struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type channelProbeResponsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type channelProbeResponsesJSONOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type channelProbeResponsesJSONOutput struct {
	Type    string                                   `json:"type"`
	Role    string                                   `json:"role"`
	Content []channelProbeResponsesJSONOutputContent `json:"content"`
}

type channelProbeResponsesStreamOutputContent struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
	Logprobs    []any  `json:"logprobs"`
}

type channelProbeResponsesStreamOutput struct {
	Type    string                                     `json:"type"`
	ID      string                                     `json:"id"`
	Status  string                                     `json:"status"`
	Role    string                                     `json:"role"`
	Content []channelProbeResponsesStreamOutputContent `json:"content"`
}

type channelProbeResponsesInputTokenDetails struct {
	CacheWriteTokens int `json:"cache_write_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

type channelProbeResponsesOutputTokenDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type channelProbeResponsesUsage struct {
	InputTokens         int                                     `json:"input_tokens"`
	InputTokensDetails  channelProbeResponsesInputTokenDetails  `json:"input_tokens_details"`
	OutputTokens        int                                     `json:"output_tokens"`
	OutputTokensDetails channelProbeResponsesOutputTokenDetails `json:"output_tokens_details"`
	TotalTokens         int                                     `json:"total_tokens"`
	PromptTokensDetails channelProbeResponsesInputTokenDetails  `json:"prompt_tokens_details"`
}

type channelProbeResponsesImageTokenDetails struct {
	ImageTokens int `json:"image_tokens"`
	TextTokens  int `json:"text_tokens"`
}

type channelProbeResponsesImageUsage struct {
	InputTokens         int                                    `json:"input_tokens"`
	InputTokensDetails  channelProbeResponsesImageTokenDetails `json:"input_tokens_details"`
	OutputTokens        int                                    `json:"output_tokens"`
	OutputTokensDetails channelProbeResponsesImageTokenDetails `json:"output_tokens_details"`
	TotalTokens         int                                    `json:"total_tokens"`
}

type channelProbeResponsesToolUsage struct {
	ImageGen  channelProbeResponsesImageUsage `json:"image_gen"`
	WebSearch struct {
		NumRequests int `json:"num_requests"`
	} `json:"web_search"`
}

type channelProbeResponsesFields struct {
	ID                   string                         `json:"id"`
	Object               string                         `json:"object"`
	CreatedAt            int64                          `json:"created_at"`
	Status               string                         `json:"status"`
	Background           bool                           `json:"background"`
	CompletedAt          *int64                         `json:"completed_at"`
	Error                any                            `json:"error"`
	FrequencyPenalty     float64                        `json:"frequency_penalty"`
	IncompleteDetails    any                            `json:"incomplete_details"`
	Instructions         json.RawMessage                `json:"instructions"`
	MaxOutputTokens      *uint                          `json:"max_output_tokens"`
	MaxToolCalls         *uint                          `json:"max_tool_calls"`
	Model                string                         `json:"model"`
	Moderation           json.RawMessage                `json:"moderation"`
	ParallelToolCalls    json.RawMessage                `json:"parallel_tool_calls"`
	PresencePenalty      float64                        `json:"presence_penalty"`
	PreviousResponseID   any                            `json:"previous_response_id"`
	PromptCacheKey       json.RawMessage                `json:"prompt_cache_key"`
	PromptCacheRetention json.RawMessage                `json:"prompt_cache_retention"`
	Reasoning            *dto.Reasoning                 `json:"reasoning"`
	SafetyIdentifier     json.RawMessage                `json:"safety_identifier"`
	ServiceTier          string                         `json:"service_tier"`
	Store                json.RawMessage                `json:"store"`
	Temperature          float64                        `json:"temperature"`
	Text                 json.RawMessage                `json:"text"`
	ToolChoice           json.RawMessage                `json:"tool_choice"`
	ToolUsage            channelProbeResponsesToolUsage `json:"tool_usage"`
	Tools                json.RawMessage                `json:"tools"`
	TopLogProbs          int                            `json:"top_logprobs"`
	TopP                 float64                        `json:"top_p"`
	Truncation           json.RawMessage                `json:"truncation"`
	Usage                *channelProbeResponsesUsage    `json:"usage"`
	User                 json.RawMessage                `json:"user"`
	Metadata             json.RawMessage                `json:"metadata"`
}

type channelProbeResponsesResponse struct {
	channelProbeResponsesFields
	Output []channelProbeResponsesJSONOutput `json:"output"`
}

type channelProbeResponsesStreamResponse struct {
	channelProbeResponsesFields
	Output []channelProbeResponsesStreamOutput `json:"output"`
}

type channelProbeResponsesEvent struct {
	Type           string                                    `json:"type"`
	SequenceNumber int                                       `json:"sequence_number"`
	Response       *channelProbeResponsesStreamResponse      `json:"response,omitempty"`
	OutputIndex    *int                                      `json:"output_index,omitempty"`
	ContentIndex   *int                                      `json:"content_index,omitempty"`
	ItemID         string                                    `json:"item_id,omitempty"`
	Delta          string                                    `json:"delta,omitempty"`
	Text           string                                    `json:"text,omitempty"`
	Logprobs       *[]any                                    `json:"logprobs,omitempty"`
	Item           *channelProbeResponsesStreamOutput        `json:"item,omitempty"`
	Part           *channelProbeResponsesStreamOutputContent `json:"part,omitempty"`
}

type channelProbeChatResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []channelProbeChatChoice `json:"choices"`
	Usage   channelProbeChatUsage    `json:"usage"`
}

type channelProbeChatChoice struct {
	Index        int                     `json:"index"`
	Message      channelProbeChatMessage `json:"message"`
	Logprobs     any                     `json:"logprobs"`
	FinishReason string                  `json:"finish_reason"`
}

type channelProbeChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Refusal any    `json:"refusal"`
}

type channelProbeChatUsage struct {
	PromptTokens            int                                     `json:"prompt_tokens"`
	CompletionTokens        int                                     `json:"completion_tokens"`
	TotalTokens             int                                     `json:"total_tokens"`
	PromptTokensDetails     channelProbeResponsesInputTokenDetails  `json:"prompt_tokens_details"`
	CompletionTokensDetails channelProbeResponsesOutputTokenDetails `json:"completion_tokens_details"`
}

type validatedRequest struct {
	format  types.RelayFormat
	request dto.Request
}

// IsChannelMonitorProbeResponseEnabled reports whether local probe responses are enabled.
func IsChannelMonitorProbeResponseEnabled() bool {
	return GetResponseConfig().Enabled
}

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if tryChannelProbeResponse(c) {
			c.Abort()
			return
		}
		c.Next()
	}
}

func tryChannelProbeResponse(c *gin.Context) bool {
	if c.Request.Method != http.MethodPost {
		return false
	}

	var relayFormat types.RelayFormat
	var relayMode int
	switch c.Request.URL.Path {
	case "/v1/responses":
		relayFormat = types.RelayFormatOpenAIResponses
		relayMode = relayconstant.RelayModeResponses
	case "/v1/chat/completions":
		relayFormat = types.RelayFormatOpenAI
		relayMode = relayconstant.RelayModeChatCompletions
	default:
		return false
	}
	config := GetResponseConfig()
	if !config.Enabled {
		return false
	}

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		return false
	}
	c.Set(validatedRequestContextKey, validatedRequest{format: relayFormat, request: request})
	if !matchesChannelProbeRequest(relayMode, request, config.MatchInput) {
		return false
	}

	delayRangeMs := config.MaxDelayMs - config.MinDelayMs
	delayMs := config.MinDelayMs + rand.IntN(delayRangeMs+1)
	serveChannelProbeResponse(c, relayMode, request, config, time.Duration(delayMs)*time.Millisecond)
	return true
}

func ValidatedRequest(c *gin.Context, format types.RelayFormat) (dto.Request, bool) {
	value, exists := c.Get(validatedRequestContextKey)
	if !exists {
		return nil, false
	}
	request, ok := value.(validatedRequest)
	if !ok || request.format != format || request.request == nil {
		return nil, false
	}
	return request.request, true
}

func matchesChannelProbeRequest(relayMode int, request dto.Request, matchInput string) bool {
	switch relayMode {
	case relayconstant.RelayModeResponses:
		responsesRequest, ok := request.(*dto.OpenAIResponsesRequest)
		if !ok || strings.TrimSpace(responsesRequest.PreviousResponseID) != "" ||
			channelProbeRawHasValue(responsesRequest.Conversation) {
			return false
		}
		return matchesChannelProbeResponsesInput(responsesRequest.Input, matchInput)
	case relayconstant.RelayModeChatCompletions:
		chatRequest, ok := request.(*dto.GeneralOpenAIRequest)
		if !ok || chatRequest.Input != nil || chatRequest.Prompt != nil ||
			chatRequest.Prefix != nil || chatRequest.Suffix != nil {
			return false
		}

		userMessages := 0
		for _, message := range chatRequest.Messages {
			switch strings.ToLower(strings.TrimSpace(message.Role)) {
			case "system", "developer":
				continue
			case "user":
				userMessages++
				if !matchesChannelProbeChatContent(message.Content, matchInput) {
					return false
				}
			default:
				return false
			}
		}
		return userMessages == 1
	default:
		return false
	}
}

func matchesChannelProbeResponsesInput(rawInput json.RawMessage, matchInput string) bool {
	switch common.GetJsonType(rawInput) {
	case "string":
		var input string
		return common.Unmarshal(rawInput, &input) == nil && isChannelProbeInput(input, matchInput)
	case "array":
		var inputs []channelProbeResponsesInput
		if common.Unmarshal(rawInput, &inputs) != nil || len(inputs) == 0 {
			return false
		}

		userInputs := 0
		for _, input := range inputs {
			inputType := strings.ToLower(strings.TrimSpace(input.Type))
			if inputType != "" && inputType != "message" {
				return false
			}
			switch strings.ToLower(strings.TrimSpace(input.Role)) {
			case "system", "developer":
				continue
			case "user":
				userInputs++
				if !matchesChannelProbeResponsesContent(input.Content, matchInput) {
					return false
				}
			default:
				return false
			}
		}
		return userInputs == 1
	default:
		return false
	}
}

func matchesChannelProbeResponsesContent(rawContent json.RawMessage, matchInput string) bool {
	if common.GetJsonType(rawContent) == "string" {
		var content string
		return common.Unmarshal(rawContent, &content) == nil && isChannelProbeInput(content, matchInput)
	}
	if common.GetJsonType(rawContent) != "array" {
		return false
	}

	var content []channelProbeResponsesContent
	if common.Unmarshal(rawContent, &content) != nil || len(content) != 1 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(content[0].Type), "input_text") &&
		isChannelProbeInput(content[0].Text, matchInput)
}

func matchesChannelProbeChatContent(content any, matchInput string) bool {
	if text, ok := content.(string); ok {
		return isChannelProbeInput(text, matchInput)
	}

	rawContent, err := common.Marshal(content)
	if err != nil {
		return false
	}
	var parts []channelProbeResponsesContent
	if common.Unmarshal(rawContent, &parts) != nil || len(parts) != 1 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(parts[0].Type), "text") &&
		isChannelProbeInput(parts[0].Text, matchInput)
}

func channelProbeRawHasValue(raw json.RawMessage) bool {
	value := bytes.TrimSpace(raw)
	return len(value) > 0 && !bytes.Equal(value, []byte("null")) && !bytes.Equal(value, []byte(`""`))
}

func isChannelProbeInput(value string, matchInput string) bool {
	return strings.EqualFold(strings.TrimSpace(value), matchInput)
}

func serveChannelProbeResponse(
	c *gin.Context,
	relayMode int,
	request dto.Request,
	config ResponseConfig,
	delay time.Duration,
) {
	createdAt := time.Now().Unix()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-c.Request.Context().Done():
		return
	case <-timer.C:
	}
	completedAt := time.Now().Unix()

	switch relayMode {
	case relayconstant.RelayModeResponses:
		responsesRequest := request.(*dto.OpenAIResponsesRequest)
		if responsesRequest.IsStream(c.Request) {
			writeChannelProbeResponsesStream(
				c,
				buildChannelProbeResponsesStreamResponse(responsesRequest, config, createdAt, completedAt),
			)
			return
		}
		response := buildChannelProbeResponsesResponse(responsesRequest, config, createdAt, completedAt)
		data, err := common.Marshal(response)
		if err == nil {
			c.Data(http.StatusOK, "application/json; charset=utf-8", data)
		}
	case relayconstant.RelayModeChatCompletions:
		chatRequest := request.(*dto.GeneralOpenAIRequest)
		if chatRequest.IsStream(c.Request) {
			writeChannelProbeChatStream(c, chatRequest, config, createdAt)
			return
		}
		response := channelProbeChatResponse{
			ID:      "chatcmpl-" + common.GetUUID(),
			Object:  "chat.completion",
			Created: createdAt,
			Model:   chatRequest.Model,
			Choices: []channelProbeChatChoice{{
				Index: 0,
				Message: channelProbeChatMessage{
					Role:    "assistant",
					Content: config.ResponseText,
				},
				FinishReason: "stop",
			}},
			Usage: channelProbeChatUsage{
				PromptTokens:     config.InputTokens,
				CompletionTokens: config.OutputTokens,
				TotalTokens:      config.TotalTokens(),
				PromptTokensDetails: channelProbeResponsesInputTokenDetails{
					CacheWriteTokens: config.CacheWriteTokens,
					CachedTokens:     config.CachedTokens,
				},
			},
		}
		data, err := common.Marshal(response)
		if err == nil {
			c.Data(http.StatusOK, "application/json; charset=utf-8", data)
		}
	}
}

func buildChannelProbeResponsesResponse(
	request *dto.OpenAIResponsesRequest,
	config ResponseConfig,
	createdAt int64,
	completedAt int64,
) *channelProbeResponsesResponse {
	fields := buildChannelProbeResponsesFields(request, config, createdAt, completedAt)
	return &channelProbeResponsesResponse{
		channelProbeResponsesFields: fields,
		Output: []channelProbeResponsesJSONOutput{{
			Type: "message",
			Role: "assistant",
			Content: []channelProbeResponsesJSONOutputContent{{
				Type: "output_text",
				Text: config.ResponseText,
			}},
		}},
	}
}

func buildChannelProbeResponsesStreamResponse(
	request *dto.OpenAIResponsesRequest,
	config ResponseConfig,
	createdAt int64,
	completedAt int64,
) *channelProbeResponsesStreamResponse {
	fields := buildChannelProbeResponsesFields(request, config, createdAt, completedAt)
	return &channelProbeResponsesStreamResponse{
		channelProbeResponsesFields: fields,
		Output: []channelProbeResponsesStreamOutput{{
			Type:   "message",
			ID:     channelProbeResponseID("msg_"),
			Status: "completed",
			Role:   "assistant",
			Content: []channelProbeResponsesStreamOutputContent{{
				Type:        "output_text",
				Text:        config.ResponseText,
				Annotations: []any{},
				Logprobs:    []any{},
			}},
		}},
	}
}

func buildChannelProbeResponsesFields(
	request *dto.OpenAIResponsesRequest,
	config ResponseConfig,
	createdAt int64,
	completedAt int64,
) channelProbeResponsesFields {
	temperature := 1.0
	if request.Temperature != nil {
		temperature = *request.Temperature
	}
	topP := 1.0
	if request.TopP != nil {
		topP = *request.TopP
	}
	topLogProbs := 0
	if request.TopLogProbs != nil {
		topLogProbs = *request.TopLogProbs
	}
	serviceTier := strings.TrimSpace(request.ServiceTier)
	if serviceTier == "" {
		serviceTier = "default"
	}
	var previousResponseID any
	if strings.TrimSpace(request.PreviousResponseID) != "" {
		previousResponseID = request.PreviousResponseID
	}

	return channelProbeResponsesFields{
		ID:                   channelProbeResponseID("resp_"),
		Object:               "response",
		CreatedAt:            createdAt,
		Status:               "completed",
		CompletedAt:          &completedAt,
		Instructions:         request.Instructions,
		MaxOutputTokens:      request.MaxOutputTokens,
		MaxToolCalls:         request.MaxToolCalls,
		Model:                request.Model,
		Moderation:           request.Moderation,
		ParallelToolCalls:    channelProbeRawOrDefault(request.ParallelToolCalls, "true"),
		PreviousResponseID:   previousResponseID,
		PromptCacheKey:       request.PromptCacheKey,
		PromptCacheRetention: request.PromptCacheRetention,
		Reasoning:            request.Reasoning,
		SafetyIdentifier:     request.SafetyIdentifier,
		ServiceTier:          serviceTier,
		Store:                channelProbeRawOrDefault(request.Store, "false"),
		Temperature:          temperature,
		Text:                 channelProbeRawOrDefault(request.Text, `{"format":{"type":"text"}}`),
		ToolChoice:           channelProbeRawOrDefault(request.ToolChoice, `"auto"`),
		Tools:                channelProbeRawOrDefault(request.Tools, "[]"),
		TopLogProbs:          topLogProbs,
		TopP:                 topP,
		Truncation:           channelProbeRawOrDefault(request.Truncation, `"disabled"`),
		Usage: &channelProbeResponsesUsage{
			InputTokens: config.InputTokens,
			InputTokensDetails: channelProbeResponsesInputTokenDetails{
				CacheWriteTokens: config.CacheWriteTokens,
				CachedTokens:     config.CachedTokens,
			},
			OutputTokens:        config.OutputTokens,
			OutputTokensDetails: channelProbeResponsesOutputTokenDetails{},
			TotalTokens:         config.TotalTokens(),
			PromptTokensDetails: channelProbeResponsesInputTokenDetails{
				CacheWriteTokens: config.CacheWriteTokens,
				CachedTokens:     config.CachedTokens,
			},
		},
		User:     request.User,
		Metadata: channelProbeRawOrDefault(request.Metadata, "{}"),
	}
}

func channelProbeResponseID(prefix string) string {
	return prefix + common.GetUUID() + common.GetUUID()
}

func channelProbeRawOrDefault(value json.RawMessage, fallback string) json.RawMessage {
	if !channelProbeRawHasValue(value) {
		return json.RawMessage(fallback)
	}
	return value
}

func writeChannelProbeResponsesStream(c *gin.Context, response *channelProbeResponsesStreamResponse) {
	helper.SetEventStreamHeaders(c)
	outputIndex := 0
	contentIndex := 0
	emptyLogprobs := []any{}
	completedOutput := response.Output[0]
	inProgressOutput := completedOutput
	inProgressOutput.Status = "in_progress"
	inProgressOutput.Content = []channelProbeResponsesStreamOutputContent{}
	inProgressResponse := *response
	inProgressResponse.Status = "in_progress"
	inProgressResponse.CompletedAt = nil
	inProgressResponse.Output = []channelProbeResponsesStreamOutput{}
	inProgressResponse.Usage = nil
	emptyPart := channelProbeResponsesStreamOutputContent{
		Type:        "output_text",
		Text:        "",
		Annotations: []any{},
		Logprobs:    []any{},
	}
	completedPart := completedOutput.Content[0]

	events := []channelProbeResponsesEvent{
		{Type: "response.created", SequenceNumber: 0, Response: &inProgressResponse},
		{Type: "response.in_progress", SequenceNumber: 1, Response: &inProgressResponse},
		{
			Type:           "response.output_item.added",
			SequenceNumber: 2,
			OutputIndex:    &outputIndex,
			Item:           &inProgressOutput,
		},
		{
			Type:           "response.content_part.added",
			SequenceNumber: 3,
			OutputIndex:    &outputIndex,
			ContentIndex:   &contentIndex,
			ItemID:         completedOutput.ID,
			Part:           &emptyPart,
		},
		{
			Type:           "response.output_text.delta",
			SequenceNumber: 4,
			OutputIndex:    &outputIndex,
			ContentIndex:   &contentIndex,
			ItemID:         completedOutput.ID,
			Delta:          completedPart.Text,
			Logprobs:       &emptyLogprobs,
		},
		{
			Type:           "response.output_text.done",
			SequenceNumber: 5,
			OutputIndex:    &outputIndex,
			ContentIndex:   &contentIndex,
			ItemID:         completedOutput.ID,
			Text:           completedPart.Text,
			Logprobs:       &emptyLogprobs,
		},
		{
			Type:           "response.content_part.done",
			SequenceNumber: 6,
			OutputIndex:    &outputIndex,
			ContentIndex:   &contentIndex,
			ItemID:         completedOutput.ID,
			Part:           &completedPart,
		},
		{
			Type:           "response.output_item.done",
			SequenceNumber: 7,
			OutputIndex:    &outputIndex,
			Item:           &completedOutput,
		},
		{Type: "response.completed", SequenceNumber: 8, Response: response},
	}
	for _, event := range events {
		data, err := common.Marshal(event)
		if err != nil {
			return
		}
		if helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data)) != nil {
			return
		}
	}
}

func writeChannelProbeChatStream(
	c *gin.Context,
	request *dto.GeneralOpenAIRequest,
	config ResponseConfig,
	createdAt int64,
) {
	helper.SetEventStreamHeaders(c)
	responseID := "chatcmpl-" + common.GetUUID()
	start := helper.GenerateStartEmptyResponse(responseID, createdAt, request.Model, nil)
	content := &dto.ChatCompletionsStreamResponse{
		Id:      responseID,
		Object:  "chat.completion.chunk",
		Created: createdAt,
		Model:   request.Model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: common.GetPointer(config.ResponseText),
			},
		}},
	}
	stop := helper.GenerateStopResponse(responseID, createdAt, request.Model, "stop")
	for _, chunk := range []*dto.ChatCompletionsStreamResponse{start, content, stop} {
		if helper.ObjectData(c, chunk) != nil {
			return
		}
	}
	if request.StreamOptions != nil && request.StreamOptions.IncludeUsage {
		usage := dto.Usage{
			PromptTokens:     config.InputTokens,
			CompletionTokens: config.OutputTokens,
			TotalTokens:      config.TotalTokens(),
			PromptTokensDetails: dto.InputTokenDetails{
				CacheWriteTokens: config.CacheWriteTokens,
				CachedTokens:     config.CachedTokens,
			},
			CompletionTokenDetails: dto.OutputTokenDetails{},
			InputTokens:            config.InputTokens,
			OutputTokens:           config.OutputTokens,
			InputTokensDetails: &dto.InputTokenDetails{
				CacheWriteTokens: config.CacheWriteTokens,
				CachedTokens:     config.CachedTokens,
			},
		}
		if helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseID, createdAt, request.Model, usage)) != nil {
			return
		}
	}
	helper.Done(c)
}
