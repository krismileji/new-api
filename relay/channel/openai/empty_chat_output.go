package openai

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/MoeForge/moego/ojson"
)

func newEmptyResponseError() *types.NewAPIError {
	return types.NewOpenAIError(errors.New("上游响应没有输出内容"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
}

// isEmptyChatOutput recognizes Chat metadata frames and empty choices. Read the
// wire payload so refusal, audio and provider extensions survive the check.
func isEmptyChatOutput(data []byte, choicePayload string) bool {
	if !ojson.ValidBytes(data) {
		return false
	}
	response := ojson.ParseBytes(data)
	if response.Type == ojson.Null {
		return true
	}
	if !response.IsObject() || response.Get("error").Type != ojson.Null ||
		response.Get("type").String() == "error" {
		return false
	}
	choices := response.Get("choices")
	if !choices.IsArray() {
		return choices.Type == ojson.Null
	}
	for _, choice := range choices.Array() {
		finishReason := choice.Get("finish_reason").String()
		if !choice.IsObject() || (finishReason != "" && finishReason != "stop") ||
			choice.Get("text").String() != "" {
			return false
		}
		payload := choice.Get(choicePayload)
		if payload.Type == ojson.Null {
			continue
		}
		if !payload.IsObject() {
			return false
		}
		for key, value := range payload.Map() {
			if key == "role" || key == "obfuscation" || value.Type == ojson.Null {
				continue
			}
			if value.Type == ojson.String && value.String() == "" ||
				value.IsArray() && len(value.Array()) == 0 ||
				value.IsObject() && len(value.Map()) == 0 {
				continue
			}
			return false
		}
	}
	return true
}
