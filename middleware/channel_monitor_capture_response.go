package middleware

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Bound temporary JSON parsing memory independently of the saved text limit.
// On SSE streams this applies per event, so a large event cannot hide later ones.
const captureResponseJSONLimit = 4 << 20

type capturedResponseWriter struct {
	gin.ResponseWriter
	limit      int
	rawEnabled bool
	mediaType  string
	size       int64
	raw        captureBuffer
	json       captureBuffer
	visible    captureResponseText
	sse        captureSSE
}

func (w *capturedResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.capture(p[:n])
	return n, err
}

func (w *capturedResponseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	w.capture([]byte(s[:n]))
	return n, err
}

func (w *capturedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *capturedResponseWriter) capture(p []byte) {
	if len(p) == 0 {
		return
	}
	if w.size == 0 {
		w.mediaType = captureMediaType(w.Header().Get("Content-Type"))
	}
	w.size += int64(len(p))
	switch {
	case w.mediaType == "text/event-stream":
		w.sse.write(p, &w.visible, w.limit)
	case captureJSON(w.mediaType):
		w.json.append(p, captureResponseJSONLimit)
	case strings.HasPrefix(w.mediaType, "text/"):
		w.visible.text.append(p, w.limit)
	default:
		return
	}
	if w.rawEnabled {
		w.raw.append(p, w.limit)
	}
}

func (w *capturedResponseWriter) recordResponse(record *service.ContentCaptureRecord) {
	record.IsStream = captureMediaType(w.Header().Get("Content-Type")) == "text/event-stream"
	record.ResponseSize = w.size
	record.ResponseBody, record.ResponseTruncated = w.raw.text(), w.raw.truncated
	if record.IsStream {
		w.sse.flush(&w.visible, w.limit)
	} else if w.json.truncated {
		record.ResponseText = "（JSON 响应超过解析上限）"
		record.ResponseTextTruncated = true
		return
	} else if w.json.Len() > 0 {
		var response captureResponse
		if common.Unmarshal(w.json.Bytes(), &response) == nil {
			if w.Status() >= http.StatusBadRequest && response.Error.Message == "" {
				response.Error.Message = response.Message
			}
			w.visible.appendResponse(response, w.limit)
		}
	}

	if w.visible.text.Len() == 0 && w.visible.err.Len() == 0 {
		if w.visible.tools.Len() > 0 {
			w.visible.text = w.visible.tools
		} else if w.Status() >= http.StatusBadRequest {
			w.visible.err.appendString("请求失败", w.limit)
		}
	}
	record.ResponseText, record.ResponseTextTruncated = w.visible.result(w.limit)
	record.ResponseTextTruncated = record.ResponseTextTruncated || w.sse.skipped
	if record.ResponseText == "" {
		record.ResponseText = "（无可提取的模型回复）"
	}
}

type captureSSE struct {
	line    captureBuffer
	data    captureBuffer
	skipped bool
}

func (s *captureSSE) write(p []byte, output *captureResponseText, limit int) {
	for len(p) > 0 {
		line, rest, complete := bytes.Cut(p, []byte("\n"))
		s.line.append(line, captureResponseJSONLimit)
		if !complete {
			return
		}
		s.finishLine(output, limit)
		p = rest
	}
}

func (s *captureSSE) finishLine(output *captureResponseText, limit int) {
	line := bytes.TrimSuffix(s.line.Bytes(), []byte("\r"))
	switch {
	case s.line.truncated:
		s.data.truncated = true
	case len(line) == 0:
		s.finishEvent(output, limit)
	default:
		if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			data = bytes.TrimPrefix(data, []byte(" "))
			if s.data.Len() > 0 {
				s.data.append([]byte("\n"), captureResponseJSONLimit)
			}
			s.data.append(data, captureResponseJSONLimit)
		}
	}
	s.line.resetEventBuffer()
}

func (s *captureSSE) finishEvent(output *captureResponseText, limit int) {
	if s.data.truncated {
		s.skipped = true
	} else if s.data.Len() > 0 && !bytes.Equal(bytes.TrimSpace(s.data.Bytes()), []byte("[DONE]")) {
		var event captureResponse
		if common.Unmarshal(s.data.Bytes(), &event) == nil {
			output.appendEvent(event, limit)
		}
	}
	s.data.resetEventBuffer()
}

// Avoid retaining the backing array of a large event for the rest of the stream.
func (b *captureBuffer) resetEventBuffer() {
	if b.Cap() > service.ContentCaptureMaxBodyKB<<10 {
		*b = captureBuffer{}
		return
	}
	b.Reset()
	b.truncated = false
}

func (s *captureSSE) flush(output *captureResponseText, limit int) {
	if s.line.Len() > 0 {
		s.finishLine(output, limit)
	}
	s.finishEvent(output, limit)
}

type captureResponse struct {
	Type         string              `json:"type"`
	Text         string              `json:"text"`
	Message      string              `json:"message"`
	Error        captureError        `json:"error"`
	Content      []capturePart       `json:"content"`
	ContentBlock capturePart         `json:"content_block"`
	Delta        common.RawMessage   `json:"delta"`
	Item         captureOutputItem   `json:"item"`
	Output       []captureOutputItem `json:"output"`
	Response     *captureResponse    `json:"response"`
	Choices      []captureChoice     `json:"choices"`
	Candidates   []struct {
		Index   int `json:"index"`
		Content struct {
			Parts []capturePart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

type captureOutputItem struct {
	Type    string        `json:"type"`
	Name    string        `json:"name"`
	Content []capturePart `json:"content"`
}

type captureChoice struct {
	Index   int                  `json:"index"`
	Text    string               `json:"text"`
	Message captureChoiceMessage `json:"message"`
	Delta   captureChoiceMessage `json:"delta"`
}

type captureChoiceMessage struct {
	Content      captureContent `json:"content"`
	Refusal      string         `json:"refusal"`
	FunctionCall captureTool    `json:"function_call"`
	ToolCalls    []struct {
		Function captureTool `json:"function"`
	} `json:"tool_calls"`
}

type captureError struct {
	Message string `json:"message"`
}

func (e *captureError) UnmarshalJSON(data []byte) error {
	if common.GetJsonType(data) == "string" {
		return common.Unmarshal(data, &e.Message)
	}
	type errorObject captureError
	return common.Unmarshal(data, (*errorObject)(e))
}

type captureResponseText struct {
	text  captureBuffer
	tools captureBuffer
	err   captureBuffer
}

func (out *captureResponseText) result(limit int) (string, bool) {
	truncated := out.text.truncated || out.err.truncated
	if out.err.Len() > 0 {
		appendCaptureLine(&out.text, "错误："+out.err.text(), limit)
	}
	return out.text.text(), truncated || out.text.truncated
}

func (out *captureResponseText) appendResponse(response captureResponse, limit int) {
	if response.Error.Message != "" {
		out.err.appendString(response.Error.Message, limit)
		return
	}
	out.text.appendString(response.Text, limit)
	out.appendParts(response.Content, limit)
	for _, choice := range response.Choices {
		if choice.Index == 0 {
			out.text.appendString(choice.Text, limit)
			out.appendChoice(choice.Message, limit)
			out.appendChoice(choice.Delta, limit)
			break
		}
	}
	for _, candidate := range response.Candidates {
		if candidate.Index == 0 {
			out.appendParts(candidate.Content.Parts, limit)
			break
		}
	}
	for _, item := range response.Output {
		if item.Type == "message" {
			if len(item.Content) > 0 && out.text.Len() > 0 {
				out.text.appendString("\n", limit)
			}
			out.appendParts(item.Content, limit)
		} else if strings.HasSuffix(item.Type, "_call") {
			out.appendTool(item.Name, limit)
		}
	}
}

func (out *captureResponseText) appendEvent(event captureResponse, limit int) {
	switch event.Type {
	case "response.output_text.delta", "response.refusal.delta":
		var text string
		if common.Unmarshal(event.Delta, &text) == nil {
			out.text.appendString(text, limit)
		}
	case "response.output_item.added", "response.output_item.done":
		if strings.HasSuffix(event.Item.Type, "_call") {
			out.appendTool(event.Item.Name, limit)
		}
	case "response.completed", "response.incomplete", "response.failed":
		if event.Response != nil {
			if out.text.Len() == 0 {
				out.appendResponse(*event.Response, limit)
			} else if event.Response.Error.Message != "" {
				out.err.appendString(event.Response.Error.Message, limit)
			}
		}
	case "content_block_start":
		out.appendParts([]capturePart{event.ContentBlock}, limit)
	case "content_block_delta":
		var part capturePart
		if common.Unmarshal(event.Delta, &part) == nil && part.Type == "text_delta" {
			out.text.appendString(part.Text, limit)
		}
	case "error":
		message := event.Error.Message
		if message == "" {
			message = event.Message
		}
		out.err.appendString(message, limit)
	default:
		out.appendResponse(event, limit)
	}
}

func (out *captureResponseText) appendParts(parts []capturePart, limit int) {
	for _, part := range parts {
		switch part.Type {
		case "", "text", "output_text":
			if !part.Thought {
				out.text.appendString(part.Text, limit)
			}
		case "refusal":
			out.text.appendString(part.Refusal, limit)
		case "tool_use", "server_tool_use":
			out.appendTool(part.Name, limit)
		}
		if part.FunctionCall != nil {
			out.appendTool(part.FunctionCall.Name, limit)
		}
	}
}

func (out *captureResponseText) appendChoice(message captureChoiceMessage, limit int) {
	out.text.appendString(message.Content.Text, limit)
	out.text.appendString(message.Refusal, limit)
	out.appendParts(message.Content.Parts, limit)
	out.appendTool(message.FunctionCall.Name, limit)
	for _, tool := range message.ToolCalls {
		out.appendTool(tool.Function.Name, limit)
	}
}

func (out *captureResponseText) appendTool(name string, limit int) {
	name = strings.TrimSpace(name)
	if name == "" || out.tools.truncated {
		return
	}
	line := "调用工具：" + strings.ReplaceAll(name, "\n", "")
	for existing := range strings.SplitSeq(out.tools.String(), "\n") {
		if existing == line {
			return
		}
	}
	appendCaptureLine(&out.tools, line, limit)
}
