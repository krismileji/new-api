package service

import (
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/tidwall/gjson"
)

type ChannelSmallInputText struct {
	Text         string
	InputTokens  int
	OutputTokens int
	Truncated    bool
}

// EstimateChannelSmallInput counts a complete, locally available text context.
// Serializing the selected context also counts tool schemas and message structure;
// it deliberately does not depend on the global billing CountToken switch.
func EstimateChannelSmallInput(info *relaycommon.RelayInfo) (tokens int, outputLimit *int, ok bool) {
	if info == nil || info.Request == nil || info.IsChannelTest {
		return
	}
	var fields []string
	var maxPaths []string
	switch info.Request.(type) {
	case *dto.GeneralOpenAIRequest:
		if info.RelayMode != relayconstant.RelayModeChatCompletions {
			return
		}
		fields = []string{"messages", "tools", "functions"}
		maxPaths = []string{"max_completion_tokens", "max_tokens"}
	case *dto.OpenAIResponsesRequest:
		fields = []string{"input", "instructions", "tools"}
		maxPaths = []string{"max_output_tokens"}
	case *dto.ClaudeRequest:
		fields = []string{"system", "messages", "tools"}
		maxPaths = []string{"max_tokens", "max_tokens_to_sample"}
	case *dto.GeminiChatRequest:
		fields = []string{"contents", "systemInstruction", "tools"}
		maxPaths = []string{"generationConfig.maxOutputTokens"}
	default:
		return
	}
	body, err := common.Marshal(info.Request)
	if err != nil {
		return
	}
	root := gjson.ParseBytes(body)
	for _, path := range []string{"previous_response_id", "conversation", "prompt", "cachedContent", "mcp_servers", "container", "context_management", "requests", "response_format", "text.format", "output_format", "output_config.format", "generationConfig.responseSchema", "generationConfig.responseJsonSchema", "generationConfig.responseMimeType", "generationConfig.responseModalities", "modalities", "audio", "prediction", "stop", "stop_sequences", "generationConfig.stopSequences"} {
		value := root.Get(path)
		if value.Exists() && value.Raw != "null" && value.Raw != "[]" && value.Raw != "{}" && value.String() != "" {
			return
		}
	}
	if root.Get("background").Bool() || root.Get("n").Int() > 1 || root.Get("generationConfig.candidateCount").Int() > 1 {
		return
	}
	choice := root.Get("tool_choice")
	if choice.Exists() && choice.String() != "auto" && choice.String() != "none" {
		kind := choice.Get("type").String()
		if kind != "auto" && kind != "none" {
			return
		}
	}
	if choice := root.Get("function_call"); choice.Exists() && choice.String() != "auto" && choice.String() != "none" {
		return
	}
	if mode := root.Get("toolConfig.functionCallingConfig.mode").String(); mode != "" && mode != "AUTO" && mode != "NONE" {
		return
	}
	contextParts := make(map[string]any, len(fields))
	for _, field := range fields {
		value := root.Get(field)
		if !value.Exists() {
			continue
		}
		if field != "tools" && field != "functions" && !channelSmallInputTextContext(value) {
			return
		}
		contextParts[field] = value.Value()
	}
	if len(contextParts) == 0 {
		return
	}
	encoded, err := common.Marshal(contextParts)
	if err != nil {
		return
	}
	tokens = CountTextToken(string(encoded), info.OriginModelName)
	for _, path := range maxPaths {
		if value := root.Get(path); value.Exists() {
			limit := value.Int()
			if limit < 0 || limit > 1_000_000_000 {
				return 0, nil, false
			}
			outputLimit = common.GetPointer(int(limit))
			break
		}
	}
	return tokens, outputLimit, tokens >= 0
}

func channelSmallInputTextContext(value gjson.Result) bool {
	if !value.IsArray() && !value.IsObject() {
		return true
	}
	valid := true
	value.ForEach(func(key, item gjson.Result) bool {
		switch key.String() {
		case "image_url", "image", "file_id", "file_url", "file_data", "fileData", "inlineData", "input_audio", "audio", "video", "source", "encrypted_content", "thoughtSignature":
			valid = false
		case "type":
			switch item.String() {
			case "message", "text", "input_text", "output_text", "function_call", "function_call_output", "tool_use", "tool_result":
			default:
				valid = false
			}
		default:
			valid = channelSmallInputTextContext(item)
		}
		return valid
	})
	return valid
}

func BuildChannelSmallInputText(text, modelName string, inputTokens int, outputLimit *int) ChannelSmallInputText {
	result := ChannelSmallInputText{Text: text, InputTokens: inputTokens, OutputTokens: CountTextToken(text, modelName)}
	if outputLimit == nil || result.OutputTokens <= *outputLimit {
		return result
	}
	result.Truncated = true
	runes := []rune(text)
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		if CountTextToken(string(runes[:mid]), modelName) <= *outputLimit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	result.Text = string(runes[:low])
	result.OutputTokens = CountTextToken(result.Text, modelName)
	if *outputLimit == 0 {
		result.Text = ""
		result.OutputTokens = 0
	}
	return result
}
