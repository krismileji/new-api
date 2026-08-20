package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeResponsesInputOnlyChangesCustomToolIDs(t *testing.T) {
	raw := []byte(`[
		{"type":"custom_tool_call","id":"fc_custom","call_id":"fc_call_custom"},
		{"type":"function_call","id":"fc_function","call_id":"call_function"}
	]`)

	normalized, err := NormalizeResponsesInput(raw)
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"type":"custom_tool_call","id":"ctc_custom","call_id":"fc_call_custom"},
		{"type":"function_call","id":"fc_function","call_id":"call_function"}
	]`, string(normalized))
}

func TestNormalizeResponsesInputPreservesStringItems(t *testing.T) {
	raw := []byte(`["plain text",{"type":"custom_tool_call","id":"fc_custom"}]`)

	normalized, err := NormalizeResponsesInput(raw)
	require.NoError(t, err)
	assert.JSONEq(t, `["plain text",{"type":"custom_tool_call","id":"ctc_custom"}]`, string(normalized))
}

func TestNormalizeResponsesJSONUpdatesOutputAndCustomToolSSEItemIDs(t *testing.T) {
	raw := []byte(`{"type":"response.output_item.done","item_id":"fc_custom","item":{"type":"custom_tool_call","id":"fc_custom","call_id":"call_custom"}}`)

	normalized, err := NormalizeResponsesJSON(raw)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"response.output_item.done","item_id":"ctc_custom","item":{"type":"custom_tool_call","id":"ctc_custom","call_id":"call_custom"}}`, string(normalized))

	delta := []byte(`{"type":"response.custom_tool_call_input.delta","item_id":"fc_custom","delta":"x"}`)
	normalized, err = NormalizeResponsesJSON(delta)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"response.custom_tool_call_input.delta","item_id":"ctc_custom","delta":"x"}`, string(normalized))

	nested := []byte(`{"type":"response.completed","response":{"output":[{"type":"custom_tool_call","id":"fc_custom","call_id":"call_custom"},{"type":"function_call","id":"fc_function","call_id":"call_function"}]}}`)
	normalized, err = NormalizeResponsesJSON(nested)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"response.completed","response":{"output":[{"type":"custom_tool_call","id":"ctc_custom","call_id":"call_custom"},{"type":"function_call","id":"fc_function","call_id":"call_function"}]}}`, string(normalized))
}

func TestNormalizeResponsesJSONUpdatesPassThroughRequestInput(t *testing.T) {
	raw := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"custom_tool_call","id":"fc_custom","call_id":"call_custom"}]}`)

	normalized, err := NormalizeResponsesJSON(raw)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"gpt-5.6-sol","input":[{"type":"custom_tool_call","id":"ctc_custom","call_id":"call_custom"}]}`, string(normalized))
}

func TestNormalizeResponsesJSONPreservesUnrelatedLargeNumbers(t *testing.T) {
	raw := []byte(`{"temperature":9007199254740993,"input":[{"type":"custom_tool_call","id":"fc_custom"}]}`)

	normalized, err := NormalizeResponsesJSON(raw)
	require.NoError(t, err)
	assert.Contains(t, string(normalized), `"temperature":9007199254740993`)
	assert.Contains(t, string(normalized), `"id":"ctc_custom"`)
}
