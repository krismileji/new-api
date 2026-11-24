package openai

import (
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/MoeForge/moego/ojson"
)

type responsesStreamFrame struct {
	kind string
	data string
}

func isResponsesMetadataFrame(kind string, payload []byte) bool {
	switch kind {
	case "response.created", "response.queued", "response.in_progress":
		return !hasResponsesOutput(ojson.GetBytesNoCopy(payload, "response.output"))
	case dto.ResponsesOutputTypeItemAdded, dto.ResponsesOutputTypeItemDone:
		return !hasResponsesOutputItem(ojson.GetBytesNoCopy(payload, "item"))
	case "response.content_part.added", "response.content_part.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		return !hasResponsesOutputPart(ojson.GetBytesNoCopy(payload, "part"))
	case "response.output_text.delta", "response.refusal.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		return ojson.GetBytesNoCopy(payload, "delta").String() == ""
	case "response.output_text.done", "response.reasoning_text.done", "response.reasoning_summary_text.done":
		return ojson.GetBytesNoCopy(payload, "text").String() == ""
	case "response.refusal.done":
		return ojson.GetBytesNoCopy(payload, "refusal").String() == ""
	}
	return false
}

func hasResponsesOutput(output ojson.Result) bool {
	for _, item := range output.Array() {
		if hasResponsesOutputItem(item) {
			return true
		}
	}
	return false
}

func hasResponsesOutputItem(item ojson.Result) bool {
	switch item.Get("type").String() {
	case "":
		return false
	case "message", "reasoning":
		if item.Get("encrypted_content").String() != "" {
			return true
		}
		for _, field := range []string{"content", "summary"} {
			for _, part := range item.Get(field).Array() {
				if hasResponsesOutputPart(part) {
					return true
				}
			}
		}
		return false
	default:
		return true
	}
}

func hasResponsesOutputPart(part ojson.Result) bool {
	switch part.Get("type").String() {
	case "":
		return false
	case "text", "output_text", "summary_text", "reasoning_text":
		return part.Get("text").String() != ""
	case "refusal":
		return part.Get("refusal").String() != ""
	default:
		return true
	}
}
