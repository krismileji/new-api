package dto

import (
	"encoding/json"
	"strings"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const responsesCustomToolCallType = "custom_tool_call"

// NormalizeResponsesInput fixes custom tool item IDs emitted by compatible
// upstreams that incorrectly use the function-call prefix. Call IDs are
// intentionally untouched because they identify the tool result contract.
func NormalizeResponsesInput(raw []byte) ([]byte, error) {
	normalized, _, err := normalizeResponsesArray(raw)
	return normalized, err
}

// NormalizeResponsesJSON fixes custom tool item IDs in Responses request and
// response envelopes while preserving unrelated raw JSON values.
func NormalizeResponsesJSON(raw []byte) ([]byte, error) {
	if kitutil.GetJsonType(raw) != "object" {
		return raw, nil
	}
	normalized, changed, err := normalizeResponsesEnvelope(raw)
	if err != nil {
		return nil, err
	}
	if !changed {
		return raw, nil
	}
	return normalized, nil
}

func normalizeResponsesEnvelope(raw []byte) ([]byte, bool, error) {
	var envelope map[string]json.RawMessage
	if err := kitutil.Unmarshal(raw, &envelope); err != nil {
		return nil, false, err
	}
	changed := false

	if input, ok := envelope["input"]; ok {
		normalized, inputChanged, err := normalizeResponsesArray(input)
		if err != nil {
			return nil, false, err
		}
		if inputChanged {
			envelope["input"] = normalized
			changed = true
		}
	}

	if response, ok := envelope["response"]; ok && kitutil.GetJsonType(response) == "object" {
		normalized, responseChanged, err := normalizeResponsesEnvelope(response)
		if err != nil {
			return nil, false, err
		}
		if responseChanged {
			envelope["response"] = normalized
			changed = true
		}
	}

	if item, ok := envelope["item"]; ok {
		normalized, itemChanged, originalID, err := normalizeCustomToolItem(item)
		if err != nil {
			return nil, false, err
		}
		if itemChanged {
			envelope["item"] = normalized
			changed = true
			if itemID, ok := rawString(envelope["item_id"]); ok && itemID == originalID {
				envelope["item_id"] = rawStringMessage(normalizeCustomToolCallID(itemID))
			}
		}
	}

	if output, ok := envelope["output"]; ok {
		normalized, outputChanged, err := normalizeResponsesArray(output)
		if err != nil {
			return nil, false, err
		}
		if outputChanged {
			envelope["output"] = normalized
			changed = true
		}
	}

	eventType, _ := rawString(envelope["type"])
	if strings.HasPrefix(eventType, "response.custom_tool_call_input.") {
		if itemID, ok := rawString(envelope["item_id"]); ok {
			if normalized := normalizeCustomToolCallID(itemID); normalized != itemID {
				envelope["item_id"] = rawStringMessage(normalized)
				changed = true
			}
		}
	}

	if !changed {
		return raw, false, nil
	}
	normalized, err := kitutil.Marshal(envelope)
	return normalized, true, err
}

func normalizeResponsesArray(raw []byte) ([]byte, bool, error) {
	if kitutil.GetJsonType(raw) != "array" {
		return raw, false, nil
	}
	var items []json.RawMessage
	if err := kitutil.Unmarshal(raw, &items); err != nil {
		return nil, false, err
	}
	changed := false
	for i, item := range items {
		normalized, itemChanged, _, err := normalizeCustomToolItem(item)
		if err != nil {
			return nil, false, err
		}
		if itemChanged {
			items[i] = normalized
			changed = true
		}
	}
	if !changed {
		return raw, false, nil
	}
	normalized, err := kitutil.Marshal(items)
	return normalized, true, err
}

func normalizeCustomToolItem(raw []byte) ([]byte, bool, string, error) {
	if kitutil.GetJsonType(raw) != "object" {
		return raw, false, "", nil
	}
	var item map[string]json.RawMessage
	if err := kitutil.Unmarshal(raw, &item); err != nil {
		return nil, false, "", err
	}
	typeName, _ := rawString(item["type"])
	if typeName != responsesCustomToolCallType {
		return raw, false, "", nil
	}
	id, ok := rawString(item["id"])
	if !ok {
		return raw, false, "", nil
	}
	normalized := normalizeCustomToolCallID(id)
	if normalized == id {
		return raw, false, id, nil
	}
	item["id"] = rawStringMessage(normalized)
	result, err := kitutil.Marshal(item)
	return result, true, id, err
}

func rawString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || kitutil.GetJsonType(raw) != "string" {
		return "", false
	}
	var value string
	if err := kitutil.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func rawStringMessage(value string) json.RawMessage {
	message, _ := kitutil.Marshal(value)
	return message
}

func normalizeCustomToolCallID(id string) string {
	if strings.HasPrefix(id, "fc_") {
		return "ctc_" + strings.TrimPrefix(id, "fc_")
	}
	return id
}
