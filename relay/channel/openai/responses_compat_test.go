package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerNormalizesCustomToolCallID(t *testing.T) {
	c, w := newResponsesTestContext()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"custom_tool_call","id":"fc_custom","call_id":"call_custom","input":"echo ok"},{"type":"function_call","id":"fc_function","call_id":"call_function"}]}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}

	usage, apiErr := OaiResponsesHandler(c, &relaycommon.RelayInfo{}, resp)
	require.NotNil(t, usage)
	require.Nil(t, apiErr)
	assert.Contains(t, w.Body.String(), `"id":"ctc_custom"`)
	assert.Contains(t, w.Body.String(), `"call_id":"call_custom"`)
	assert.Contains(t, w.Body.String(), `"id":"fc_function"`)
}

func TestOaiResponsesStreamHandlerNormalizesCustomToolCallIDs(t *testing.T) {
	c, w := newResponsesTestContext()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"item_id":"fc_custom","item":{"type":"custom_tool_call","id":"fc_custom","call_id":"call_custom","input":"echo ok"}}`,
		`data: {"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"fc_custom","delta":"echo"}`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"custom_tool_call","id":"fc_custom","call_id":"call_custom","input":"echo ok"}]}}`,
		`data: [DONE]`,
		"",
	}, "\n\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}

	usage, apiErr := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{DisablePing: true}, resp)
	require.NotNil(t, usage)
	require.Nil(t, apiErr)
	assert.Contains(t, w.Body.String(), `"item_id":"ctc_custom"`)
	assert.Contains(t, w.Body.String(), `"id":"ctc_custom"`)
	assert.Contains(t, w.Body.String(), `"call_id":"call_custom"`)
	assert.NotContains(t, w.Body.String(), `"id":"fc_custom"`)
}
