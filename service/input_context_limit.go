package service

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/tiktoken-go/tokenizer"
	"github.com/tiktoken-go/tokenizer/codec"
)

const MaxInputContextTokens = 272000

const geminiThoughtSignatureBypass = "context_engineering_is_the_way_to_go"

var (
	enableInputContextLimit = common.GetEnvOrDefaultBool("ENABLE_272K_CONTEXT_LIMIT", true)
	contextEncodersOnce     sync.Once
	contextCl100kEncoder    tokenizer.Codec
	contextO200kEncoder     tokenizer.Codec
)

func InputContextLimitEnabled() bool {
	return enableInputContextLimit
}

func IsInputContextLimitFormat(format types.RelayFormat) bool {
	switch format {
	case types.RelayFormatOpenAI,
		types.RelayFormatClaude,
		types.RelayFormatGemini,
		types.RelayFormatOpenAIResponses,
		types.RelayFormatOpenAIResponsesCompaction,
		types.RelayFormatRerank,
		types.RelayFormatEmbedding:
		return true
	default:
		return false
	}
}

// EnforceInputContextLimit checks the complete JSON body. It deliberately uses
// the larger cl100k/o200k count so new model names cannot silently fall back to
// a tokenizer that undercounts their input.
func EnforceInputContextLimit(body []byte, format types.RelayFormat) (int, *types.NewAPIError) {
	if !InputContextLimitEnabled() || len(body) == 0 {
		return 0, nil
	}

	contextEncodersOnce.Do(func() {
		contextCl100kEncoder = codec.NewCl100kBase()
		contextO200kEncoder = codec.NewO200kBase()
	})

	text := string(body)
	cl100kTokens, err := contextCl100kEncoder.Count(text)
	if err != nil {
		return 0, types.NewError(fmt.Errorf("count input context with cl100k: %w", err), types.ErrorCodeCountTokenFailed, types.ErrOptionWithSkipRetry())
	}
	o200kTokens, err := contextO200kEncoder.Count(text)
	if err != nil {
		return 0, types.NewError(fmt.Errorf("count input context with o200k: %w", err), types.ErrorCodeCountTokenFailed, types.ErrOptionWithSkipRetry())
	}

	contextTokens := max(cl100kTokens, o200kTokens)
	if apiErr := EnforceInputContextTokenLimit(contextTokens); apiErr != nil {
		return contextTokens, apiErr
	}
	if err := validateInputContextState(body, format); err != nil {
		return contextTokens, types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeInvalidRequest,
			http.StatusOK,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return contextTokens, nil
}

func EnforceInputContextTokenLimit(contextTokens int) *types.NewAPIError {
	if !InputContextLimitEnabled() || contextTokens <= MaxInputContextTokens {
		return nil
	}
	return types.NewErrorWithStatusCode(
		fmt.Errorf("input context length %d exceeds the maximum allowed context length of %d tokens", contextTokens, MaxInputContextTokens),
		types.ErrorCodeInvalidRequest,
		http.StatusOK,
		types.ErrOptionWithSkipRetry(),
	)
}

func validateInputContextState(body []byte, format types.RelayFormat) error {
	switch format {
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction,
		types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini:
	default:
		return nil
	}

	var root map[string]any
	if err := common.Unmarshal(body, &root); err != nil {
		return fmt.Errorf("invalid JSON while checking input context: %w", err)
	}

	switch format {
	case types.RelayFormatOpenAIResponses:
		return validateResponsesContext(root, false)
	case types.RelayFormatOpenAIResponsesCompaction:
		return validateResponsesContext(root, true)
	case types.RelayFormatOpenAI:
		return validateOpenAIContext(root)
	case types.RelayFormatClaude:
		return validateClaudeContext(root)
	case types.RelayFormatGemini:
		return validateGeminiContext(root, "request")
	default:
		return nil
	}
}

func validateResponsesContext(root map[string]any, compact bool) error {
	if valueString(root["previous_response_id"]) != "" {
		return unsupportedInputContextState("previous_response_id")
	}
	if value, exists := root["conversation"]; exists && value != nil {
		return unsupportedInputContextState("conversation")
	}
	if value, exists := root["prompt"]; exists && value != nil {
		return unsupportedInputContextState("prompt")
	}
	if reasoning, ok := root["reasoning"].(map[string]any); ok && hasValue(reasoning["context"]) {
		return unsupportedInputContextState("reasoning.context")
	}
	if !compact {
		if err := validateResponsesTools(root["tools"]); err != nil {
			return err
		}
	}
	return validateResponsesItems(root["input"])
}

func validateResponsesItems(value any) error {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	for index, itemValue := range items {
		item, ok := itemValue.(map[string]any)
		if !ok {
			continue
		}
		path := fmt.Sprintf("input[%d]", index)
		itemType := strings.ToLower(valueString(item["type"]))
		if itemType == "item_reference" {
			return unsupportedInputContextState(path + ".item_reference")
		}
		if hasValue(item["encrypted_content"]) {
			return unsupportedInputContextState(path + ".encrypted_content")
		}
		if err := validateResponsesContentPart(item, path); err != nil {
			return err
		}
		if err := validateResponsesContentParts(item["content"], path+".content"); err != nil {
			return err
		}
		if output, ok := item["output"].([]any); ok {
			if err := validateResponsesContentParts(output, path+".output"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateResponsesContentParts(value any, path string) error {
	parts, ok := value.([]any)
	if !ok {
		return nil
	}
	for index, partValue := range parts {
		part, ok := partValue.(map[string]any)
		if !ok {
			continue
		}
		if err := validateResponsesContentPart(part, fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func validateResponsesContentPart(part map[string]any, path string) error {
	if hasValue(part["file_id"]) {
		return unsupportedInputContextState(path + ".file_id")
	}
	var referenceKeys []string
	switch strings.ToLower(valueString(part["type"])) {
	case "input_file":
		referenceKeys = []string{"file", "file_url", "url"}
	case "input_image", "computer_screenshot":
		referenceKeys = []string{"image_url", "url"}
	case "input_video":
		referenceKeys = []string{"video_url", "url"}
	case "input_audio":
		referenceKeys = []string{"input_audio", "url"}
	default:
		return nil
	}
	for _, key := range referenceKeys {
		value, exists := part[key]
		if !exists {
			continue
		}
		if object, ok := value.(map[string]any); ok {
			if hasValue(object["file_id"]) {
				return unsupportedInputContextState(path + "." + key + ".file_id")
			}
			for _, nestedKey := range []string{"url", "file_url"} {
				if isRemoteReference(object[nestedKey]) {
					return unsupportedInputContextState(path + "." + key + "." + nestedKey)
				}
			}
			continue
		}
		if (key == "file" || key == "input_audio") && isExplicitRemoteReference(value) {
			return unsupportedInputContextState(path + "." + key)
		}
		if key != "file" && key != "input_audio" && isRemoteReference(value) {
			return unsupportedInputContextState(path + "." + key)
		}
	}
	return nil
}

func validateResponsesTools(value any) error {
	for index, tool := range objectMaps(value) {
		toolType := strings.ToLower(valueString(tool["type"]))
		if toolType == "file_search" || toolType == "mcp" || toolType == "code_interpreter" || strings.HasPrefix(toolType, "web_search") {
			return unsupportedInputContextState(fmt.Sprintf("tools[%d]", index))
		}
	}
	return nil
}

func validateOpenAIContext(root map[string]any) error {
	if fieldProvided(root, "web_search_options") || fieldProvided(root, "search_parameters") ||
		isEnabled(root["enable_search"]) || isEnabled(root["web_search"]) {
		return unsupportedInputContextState("server-side search")
	}
	if err := validateResponsesTools(root["tools"]); err != nil {
		return err
	}

	messages, ok := root["messages"].([]any)
	if !ok {
		return nil
	}
	for messageIndex, messageValue := range messages {
		message, ok := messageValue.(map[string]any)
		if !ok {
			continue
		}
		path := fmt.Sprintf("messages[%d]", messageIndex)
		if audio, ok := message["audio"].(map[string]any); ok && hasValue(audio["id"]) {
			return unsupportedInputContextState(path + ".audio.id")
		}
		if err := validateOpenAIReasoningDetails(message["reasoning_details"], path+".reasoning_details"); err != nil {
			return err
		}
		if err := validateOpenAIToolCalls(message["tool_calls"], path+".tool_calls"); err != nil {
			return err
		}
		parts, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for partIndex, partValue := range parts {
			part, ok := partValue.(map[string]any)
			if !ok {
				continue
			}
			partPath := fmt.Sprintf("%s.content[%d]", path, partIndex)
			switch strings.ToLower(valueString(part["type"])) {
			case "file", "input_file":
				file, _ := part["file"].(map[string]any)
				if hasValue(file["file_id"]) || hasValue(part["file_id"]) {
					return unsupportedInputContextState(partPath + ".file_id")
				}
			case "image_url":
				if isRemoteReference(part["image_url"]) {
					return unsupportedInputContextState(partPath + ".image_url")
				}
			case "video_url":
				if isRemoteReference(part["video_url"]) {
					return unsupportedInputContextState(partPath + ".video_url")
				}
			case "audio_url":
				if isRemoteReference(part["audio_url"]) {
					return unsupportedInputContextState(partPath + ".audio_url")
				}
			}
		}
	}
	return nil
}

func validateOpenAIReasoningDetails(value any, path string) error {
	details, ok := value.([]any)
	if !ok {
		return nil
	}
	for index, detailValue := range details {
		detail, ok := detailValue.(map[string]any)
		if !ok {
			continue
		}
		detailType := strings.ToLower(valueString(detail["type"]))
		if strings.Contains(detailType, "encrypted") && hasValue(detail["data"]) {
			return unsupportedInputContextState(fmt.Sprintf("%s[%d]", path, index))
		}
	}
	return nil
}

func validateOpenAIToolCalls(value any, path string) error {
	toolCalls, ok := value.([]any)
	if !ok {
		return nil
	}
	for index, toolCallValue := range toolCalls {
		toolCall, ok := toolCallValue.(map[string]any)
		if !ok {
			continue
		}
		extraContent, _ := toolCall["extra_content"].(map[string]any)
		google, _ := extraContent["google"].(map[string]any)
		if hasValue(google["thought_signature"]) {
			return unsupportedInputContextState(fmt.Sprintf("%s[%d].extra_content.google.thought_signature", path, index))
		}
	}
	return nil
}

func validateClaudeContext(root map[string]any) error {
	if value, exists := root["container"]; exists && value != nil {
		return unsupportedInputContextState("container")
	}
	if hasValue(root["mcp_servers"]) {
		return unsupportedInputContextState("mcp_servers")
	}
	if err := validateClaudeBlocks(root["system"], "system"); err != nil {
		return err
	}
	messages, _ := root["messages"].([]any)
	for index, messageValue := range messages {
		message, ok := messageValue.(map[string]any)
		if !ok {
			continue
		}
		if err := validateClaudeBlocks(message["content"], fmt.Sprintf("messages[%d].content", index)); err != nil {
			return err
		}
	}
	for index, tool := range objectMaps(root["tools"]) {
		toolType := strings.ToLower(valueString(tool["type"]))
		if strings.HasPrefix(toolType, "web_search") || strings.HasPrefix(toolType, "web_fetch") || strings.HasPrefix(toolType, "code_execution") {
			return unsupportedInputContextState(fmt.Sprintf("tools[%d]", index))
		}
	}
	return nil
}

func validateClaudeBlocks(value any, path string) error {
	blocks, ok := value.([]any)
	if !ok {
		return nil
	}
	for index, blockValue := range blocks {
		block, ok := blockValue.(map[string]any)
		if !ok {
			continue
		}
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		blockType := strings.ToLower(valueString(block["type"]))
		if blockType == "redacted_thinking" {
			return unsupportedInputContextState(blockPath)
		}
		if blockType == "image" || blockType == "document" {
			source, _ := block["source"].(map[string]any)
			if strings.EqualFold(valueString(source["type"]), "file") || hasValue(source["file_id"]) {
				return unsupportedInputContextState(blockPath + ".source.file_id")
			}
			if isRemoteReference(source["url"]) {
				return unsupportedInputContextState(blockPath + ".source.url")
			}
		}
		if blockType == "tool_result" {
			if err := validateClaudeBlocks(block["content"], blockPath+".content"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateGeminiContext(root map[string]any, path string) error {
	if hasValue(root["cachedContent"]) || hasValue(root["cached_content"]) {
		return unsupportedInputContextState(path + ".cachedContent")
	}
	if err := validateGeminiContent(root["systemInstruction"], path+".systemInstruction"); err != nil {
		return err
	}
	if err := validateGeminiContent(root["system_instruction"], path+".system_instruction"); err != nil {
		return err
	}
	contents, _ := root["contents"].([]any)
	for index, contentValue := range contents {
		if err := validateGeminiContent(contentValue, fmt.Sprintf("%s.contents[%d]", path, index)); err != nil {
			return err
		}
	}
	requests, _ := root["requests"].([]any)
	for index, requestValue := range requests {
		request, ok := requestValue.(map[string]any)
		if !ok {
			continue
		}
		if err := validateGeminiContext(request, fmt.Sprintf("%s.requests[%d]", path, index)); err != nil {
			return err
		}
	}
	for index, tool := range objectMaps(root["tools"]) {
		for _, key := range []string{
			"urlContext", "url_context",
			"googleSearch", "google_search",
			"googleSearchRetrieval", "google_search_retrieval",
			"codeExecution", "code_execution",
			"fileSearch", "file_search",
			"retrieval", "enterpriseWebSearch", "enterprise_web_search",
			"googleMaps", "google_maps", "computerUse", "computer_use",
		} {
			if fieldProvided(tool, key) {
				return unsupportedInputContextState(fmt.Sprintf("%s.tools[%d].%s", path, index, key))
			}
		}
	}
	return nil
}

func validateGeminiContent(value any, path string) error {
	content, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	parts, _ := content["parts"].([]any)
	for index, partValue := range parts {
		part, ok := partValue.(map[string]any)
		if !ok {
			continue
		}
		partPath := fmt.Sprintf("%s.parts[%d]", path, index)
		fileData, _ := part["fileData"].(map[string]any)
		if fileData == nil {
			fileData, _ = part["file_data"].(map[string]any)
		}
		if hasValue(fileData["fileUri"]) || hasValue(fileData["file_uri"]) {
			return unsupportedInputContextState(partPath + ".fileData.fileUri")
		}
		signature := valueString(part["thoughtSignature"])
		if signature == "" {
			signature = valueString(part["thought_signature"])
		}
		if signature != "" && signature != geminiThoughtSignatureBypass {
			return unsupportedInputContextState(partPath + ".thoughtSignature")
		}
		functionResponse, _ := part["functionResponse"].(map[string]any)
		if functionResponse == nil {
			functionResponse, _ = part["function_response"].(map[string]any)
		}
		if nestedParts, ok := functionResponse["parts"].([]any); ok {
			if err := validateGeminiContent(map[string]any{"parts": nestedParts}, partPath+".functionResponse"); err != nil {
				return err
			}
		}
	}
	return nil
}

func unsupportedInputContextState(path string) error {
	return fmt.Errorf("input context at %s cannot be safely determined locally", path)
}

func valueString(value any) string {
	valueStr, _ := value.(string)
	return strings.TrimSpace(valueStr)
}

func hasValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	case bool:
		return typed
	default:
		return true
	}
}

func isEnabled(value any) bool {
	if enabled, ok := value.(bool); ok {
		return enabled
	}
	return hasValue(value)
}

func fieldProvided(object map[string]any, key string) bool {
	value, exists := object[key]
	return exists && value != nil
}

func objectMaps(value any) []map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return []map[string]any{typed}
	case []any:
		objects := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				objects = append(objects, object)
			}
		}
		return objects
	default:
		return nil
	}
}

func isRemoteReference(value any) bool {
	if object, ok := value.(map[string]any); ok {
		value = object["url"]
	}
	reference := valueString(value)
	return reference != "" && !strings.HasPrefix(strings.ToLower(reference), "data:")
}

func isExplicitRemoteReference(value any) bool {
	reference := strings.ToLower(valueString(value))
	for _, prefix := range []string{"http://", "https://", "ftp://", "file://", "gs://", "s3://"} {
		if strings.HasPrefix(reference, prefix) {
			return true
		}
	}
	return false
}
