package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const tokenAutoDisableBodyLimit = 1 << 20

// Observation never consumes a separate copy of the response. The adaptor sees
// exactly the bytes it would normally read; successful model text is excluded.
func ProtectTokenUpstreamResponse(ctx context.Context, channelId int, resp *http.Response) bool {
	request, _ := ctx.Value(tokenProtectionContextKey{}).(*tokenProtectionRequest)
	if request == nil || resp == nil || resp.Body == nil {
		return false
	}
	if _, wrapped := resp.Body.(*tokenProtectionResponseBody); wrapped {
		return true
	}
	m := request.manager
	m.mu.Lock()
	enabled := m.settings.Enabled
	applicable := false
	for _, rule := range m.settings.Rules {
		if !rule.Enabled {
			continue
		}
		for _, status := range rule.StatusCodes {
			applicable = applicable || status == resp.StatusCode
		}
	}
	m.mu.Unlock()
	if !enabled || !applicable {
		return false
	}
	resp.Body = &tokenProtectionResponseBody{ReadCloser: resp.Body, ctx: ctx, channelId: channelId, status: resp.StatusCode,
		stream: strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") && resp.StatusCode >= 200 && resp.StatusCode < 300}
	return true
}

type tokenProtectionResponseBody struct {
	io.ReadCloser
	ctx               context.Context
	channelId, status int
	stream, overflow  bool
	buffer            []byte
}

func (body *tokenProtectionResponseBody) Read(p []byte) (int, error) {
	n, err := body.ReadCloser.Read(p)
	if body.ctx.Err() != nil {
		return n, err
	}
	if body.stream {
		for _, chunk := range bytes.SplitAfter(p[:n], []byte{'\n'}) {
			if len(body.buffer)+len(chunk) > tokenAutoDisableBodyLimit {
				body.overflow = true
			}
			if !body.overflow {
				body.buffer = append(body.buffer, chunk...)
			}
			if len(chunk) == 0 || chunk[len(chunk)-1] != '\n' {
				continue
			}
			if !body.overflow {
				line := bytes.TrimSpace(body.buffer)
				if bytes.HasPrefix(line, []byte("data:")) {
					ObserveTokenAutoDisableJSON(body.ctx, body.channelId, body.status, bytes.TrimSpace(line[5:]))
				}
			}
			body.buffer = body.buffer[:0]
			body.overflow = false
		}
		return n, err
	}
	remaining := tokenAutoDisableBodyLimit - len(body.buffer)
	if remaining > n {
		remaining = n
	}
	body.buffer = append(body.buffer, p[:remaining]...)
	if body.status < 200 || body.status >= 300 {
		message := tokenAutoDisableJSONMessage(body.buffer)
		if message == "" {
			message = string(body.buffer)
		}
		ObserveTokenAutoDisableError(body.ctx, body.channelId, body.status, message)
	} else if err == io.EOF {
		ObserveTokenAutoDisableJSON(body.ctx, body.channelId, body.status, body.buffer)
	}
	return n, err
}

func ObserveTokenAutoDisableJSON(ctx context.Context, channelId, status int, data []byte) {
	if message := tokenAutoDisableJSONMessage(data); message != "" {
		ObserveTokenAutoDisableError(ctx, channelId, status, message)
	}
}

func tokenAutoDisableJSONMessage(data []byte) string {
	var envelope struct {
		Type     string          `json:"type"`
		Event    string          `json:"event"`
		Error    json.RawMessage `json:"error"`
		Message  string          `json:"message"`
		Response *struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if common.Unmarshal(data, &envelope) != nil {
		return ""
	}
	raw := envelope.Error
	if envelope.Type == "response.failed" && envelope.Response != nil {
		raw = envelope.Response.Error
	}
	if len(raw) == 0 || string(raw) == "null" {
		if envelope.Type == "error" || envelope.Event == "error" {
			return envelope.Message
		}
		return ""
	}
	var message string
	if common.Unmarshal(raw, &message) == nil {
		return message
	}
	var detail struct {
		Message string `json:"message"`
	}
	if common.Unmarshal(raw, &detail) == nil && detail.Message != "" {
		return detail.Message
	}
	return string(raw)
}
