package middleware

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
)

type captureBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *captureBuffer) append(p []byte, limit int) {
	room := max(0, limit-b.Len())
	b.truncated = b.truncated || len(p) > room
	b.Write(p[:min(room, len(p))])
}

func (b *captureBuffer) appendString(s string, limit int) {
	room := max(0, limit-b.Len())
	b.truncated = b.truncated || len(s) > room
	b.WriteString(s[:min(room, len(s))])
}

func (b *captureBuffer) text() string {
	return strings.ToValidUTF8(b.String(), "")
}

func captureMediaType(header string) string {
	mediaType, _, _ := mime.ParseMediaType(header)
	return mediaType
}

func captureJSON(mediaType string) bool {
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

type captureTool struct {
	Name string `json:"name"`
}

type capturePart struct {
	Type             string        `json:"type"`
	Text             string        `json:"text"`
	Refusal          string        `json:"refusal"`
	Thought          bool          `json:"thought"`
	FunctionCall     *captureTool  `json:"functionCall"`
	FunctionResponse *struct{}     `json:"functionResponse"`
	InlineData       *captureMedia `json:"inlineData"`
	FileData         *captureMedia `json:"fileData"`
	Name             string        `json:"name"`
}

type captureMedia struct {
	MimeType string `json:"mimeType"`
}

type captureContent struct {
	Text  string
	Parts []capturePart
}

func (c *captureContent) UnmarshalJSON(data []byte) error {
	switch common.GetJsonType(data) {
	case "string":
		return common.Unmarshal(data, &c.Text)
	case "array":
		return common.Unmarshal(data, &c.Parts)
	default:
		return nil
	}
}

type captureMessage struct {
	Role    string         `json:"role"`
	Type    string         `json:"type"`
	Content captureContent `json:"content"`
	Parts   []capturePart  `json:"parts"`
}

type captureInput struct {
	Text     string
	Messages []captureMessage
}

func (in *captureInput) UnmarshalJSON(data []byte) error {
	switch common.GetJsonType(data) {
	case "string":
		return common.Unmarshal(data, &in.Text)
	case "array":
		return common.Unmarshal(data, &in.Messages)
	default:
		return nil
	}
}

type capturePrompt struct {
	Text  string
	Items []string
}

func (p *capturePrompt) UnmarshalJSON(data []byte) error {
	switch common.GetJsonType(data) {
	case "string":
		return common.Unmarshal(data, &p.Text)
	case "array":
		// Legacy completions also accepts token IDs. Those are not text.
		if err := common.Unmarshal(data, &p.Items); err != nil {
			p.Items = nil
		}
		return nil
	default:
		return nil
	}
}

func recordCaptureRequest(record *service.ContentCaptureRecord, storage common.BodyStorage, mediaType string, limit int, raw bool) {
	record.RequestSize = storage.Size()
	if !captureJSON(mediaType) && !strings.HasPrefix(mediaType, "text/") {
		record.RequestText = "（非文本请求，仅记录大小）"
		return
	}
	if raw || !captureJSON(mediaType) {
		reader, err := storage.NewReader()
		if err != nil {
			record.RequestText = "（请求读取失败）"
			return
		}
		head, err := io.ReadAll(io.LimitReader(reader, int64(limit)))
		_ = reader.Close()
		if err != nil {
			record.RequestText = "（请求读取失败）"
			return
		}
		text := strings.ToValidUTF8(string(head), "")
		truncated := storage.Size() > int64(limit)
		if raw {
			record.RequestBody, record.RequestTruncated = text, truncated
		}
		if !captureJSON(mediaType) {
			record.RequestText, record.RequestTextTruncated = text, truncated
			return
		}
	}

	var request struct {
		Messages []captureMessage `json:"messages"`
		Input    captureInput     `json:"input"`
		Contents []captureMessage `json:"contents"`
		Prompt   capturePrompt    `json:"prompt"`
	}
	var err error
	if storage.IsDisk() {
		reader, readErr := storage.NewReader()
		if readErr != nil {
			record.RequestText = "（请求读取失败）"
			return
		}
		defer reader.Close()
		err = common.DecodeJsonBody(reader, &request)
	} else {
		body, readErr := storage.Bytes()
		if readErr != nil {
			record.RequestText = "（请求读取失败）"
			return
		}
		err = common.UnmarshalNoCopy(body, &request)
	}
	if err != nil {
		record.RequestText = "（无法提取请求内容）"
		return
	}
	switch {
	case len(request.Messages) > 0:
		record.RequestText, record.RequestTextTruncated = captureUserTurn(request.Messages, false, limit)
	case len(request.Input.Messages) > 0:
		record.RequestText, record.RequestTextTruncated = captureUserTurn(request.Input.Messages, false, limit)
	case len(request.Contents) > 0:
		record.RequestText, record.RequestTextTruncated = captureUserTurn(request.Contents, true, limit)
	default:
		var text captureBuffer
		for _, value := range []string{request.Input.Text, request.Prompt.Text} {
			appendCaptureLine(&text, value, limit)
		}
		for _, value := range request.Prompt.Items {
			appendCaptureLine(&text, value, limit)
		}
		record.RequestText, record.RequestTextTruncated = text.text(), text.truncated
	}
	if record.RequestText == "" {
		record.RequestText = "（无可提取的用户输入）"
	}
}

func captureUserTurn(messages []captureMessage, gemini bool, limit int) (string, bool) {
	start := len(messages)
	for start > 0 {
		message := messages[start-1]
		if (message.Role != "user" && !(gemini && message.Role == "")) || message.isToolResult() {
			break
		}
		start--
	}
	if start == len(messages) {
		if messages[start-1].isToolResult() {
			return "（本次仅提交工具结果）", false
		}
		return "", false
	}
	var text captureBuffer
	var images, audio, videos, files int
	for _, message := range messages[start:] {
		appendCaptureLine(&text, message.Content.Text, limit)
		parts := message.Content.Parts
		if gemini {
			parts = message.Parts
		}
		for _, part := range parts {
			switch part.Type {
			case "", "text", "input_text":
				if !part.Thought {
					appendCaptureLine(&text, part.Text, limit)
				}
			case "image", "image_url", "input_image":
				images++
			case "audio", "input_audio":
				audio++
			case "video", "video_url", "input_video":
				videos++
			case "file", "input_file", "document":
				files++
			}
			media := part.InlineData
			if media == nil {
				media = part.FileData
			}
			if media != nil {
				switch group, _, _ := strings.Cut(media.MimeType, "/"); group {
				case "image":
					images++
				case "audio":
					audio++
				case "video":
					videos++
				default:
					files++
				}
			}
		}
	}
	for _, attachment := range []struct {
		name  string
		count int
	}{{"图片", images}, {"音频", audio}, {"视频", videos}, {"文件", files}} {
		if attachment.count > 0 {
			appendCaptureLine(&text, fmt.Sprintf("［%s × %d］", attachment.name, attachment.count), limit)
		}
	}
	return text.text(), text.truncated
}

func (m captureMessage) isToolResult() bool {
	if m.Role == "tool" || m.Role == "function" || m.Type == "tool_result" || strings.HasSuffix(m.Type, "_call_output") {
		return true
	}
	parts := m.Content.Parts
	if len(m.Parts) > 0 {
		parts = m.Parts
	}
	for _, part := range parts {
		if part.Type != "tool_result" && part.FunctionResponse == nil {
			return false
		}
	}
	return len(parts) > 0
}

func appendCaptureLine(text *captureBuffer, value string, limit int) {
	if value == "" {
		return
	}
	if text.Len() > 0 {
		text.append([]byte("\n"), limit)
	}
	text.appendString(value, limit)
}
