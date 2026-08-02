package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var (
	errRelayResponseHeaderTimeout      = errors.New("等待上游响应超时")
	errRelayStreamFirstResponseTimeout = errors.New("等待上游流式首字超时")
)

type relayStreamFirstResponseTimeoutContextKey struct{}

type relayResponseHeaderTimeoutTransport struct {
	base            http.RoundTripper
	timeoutProvider func() time.Duration
}

type relayResponseBody struct {
	io.ReadCloser
	cancel                     context.CancelCauseFunc
	requestContext             context.Context
	timeout                    time.Duration
	timeoutError               error
	timer                      *time.Timer
	timerDone                  <-chan struct{}
	timerStopOnce              sync.Once
	waitForStreamFirstResponse bool
}

type relayPrefetchedResponseBody struct {
	io.Reader
	io.Closer
}

func newRelayResponseHeaderTimeoutTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &relayResponseHeaderTimeoutTransport{
		base:            base,
		timeoutProvider: common.GetRelayResponseHeaderTimeout,
	}
}

// WithRelayStreamFirstResponseTimeout tells the relay transport to keep the
// configured response timer running until the first meaningful stream event.
// The marker is intentionally request-scoped so non-relay HTTP users retain
// response-header timeout semantics.
func WithRelayStreamFirstResponseTimeout(request *http.Request) *http.Request {
	if request == nil {
		return nil
	}
	ctx := context.WithValue(request.Context(), relayStreamFirstResponseTimeoutContextKey{}, true)
	return request.WithContext(ctx)
}

func (transport *relayResponseHeaderTimeoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	timeout := transport.timeoutProvider()
	if timeout <= 0 {
		return transport.base.RoundTrip(request)
	}

	waitForStreamFirstResponse, _ := request.Context().Value(relayStreamFirstResponseTimeoutContextKey{}).(bool)
	timeoutError := error(errRelayResponseHeaderTimeout)
	if waitForStreamFirstResponse {
		timeoutError = errRelayStreamFirstResponseTimeout
	}
	requestContext, cancel := context.WithCancelCause(request.Context())
	timerDone := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		cancel(timeoutError)
		close(timerDone)
	})
	response, err := transport.base.RoundTrip(request.Clone(requestContext))
	stopTimer := func() {
		if !timer.Stop() {
			<-timerDone
		}
	}
	if err != nil {
		stopTimer()
		cause := context.Cause(requestContext)
		cancel(nil)
		if errors.Is(cause, timeoutError) {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, fmt.Errorf("%w（%s）", timeoutError, timeout)
		}
		return response, err
	}
	if errors.Is(context.Cause(requestContext), timeoutError) {
		stopTimer()
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		cancel(nil)
		return nil, fmt.Errorf("%w（%s）", timeoutError, timeout)
	}
	if response == nil || response.Body == nil {
		stopTimer()
		cancel(nil)
		return response, nil
	}
	keepTimerUntilFirstResponse := waitForStreamFirstResponse && response.StatusCode >= 200 && response.StatusCode < 300
	if !keepTimerUntilFirstResponse {
		stopTimer()
		timer = nil
		timerDone = nil
	}

	response.Body = &relayResponseBody{
		ReadCloser:                 response.Body,
		cancel:                     cancel,
		requestContext:             requestContext,
		timeout:                    timeout,
		timeoutError:               timeoutError,
		timer:                      timer,
		timerDone:                  timerDone,
		waitForStreamFirstResponse: keepTimerUntilFirstResponse,
	}
	return response, nil
}

// WaitForRelayStreamFirstResponse blocks before the response reaches a
// provider parser, then puts the bytes it inspected back in front of the body.
// This is what makes a first-response timeout retryable: no upstream model
// event has been handed to the downstream response writer when it returns an
// error.
func WaitForRelayStreamFirstResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return errors.New("上游流式响应体为空")
	}
	body, ok := response.Body.(*relayResponseBody)
	if !ok || !body.waitForStreamFirstResponse {
		return nil
	}

	reader := bufio.NewReader(body)
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	contentType = strings.ToLower(contentType)
	if contentType != "text/event-stream" &&
		contentType != "application/x-ndjson" &&
		contentType != "application/json-seq" &&
		contentType != "application/stream+json" {
		if _, err := reader.Peek(1); err != nil {
			return body.firstResponseError(err)
		}
		if errors.Is(context.Cause(body.requestContext), body.timeoutError) {
			return body.firstResponseError(body.timeoutError)
		}
		body.stopTimer()
		response.Body = &relayPrefetchedResponseBody{Reader: reader, Closer: body}
		return nil
	}

	var prefix bytes.Buffer
	eventName := ""
	for {
		line, err := reader.ReadString('\n')
		prefix.WriteString(line)
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		meaningful := isRelayStreamFirstResponsePayload(trimmed)
		if contentType == "text/event-stream" {
			meaningful = false
			if trimmed == "" {
				eventName = ""
			} else if strings.HasPrefix(trimmed, "event:") {
				eventName = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "event:")))
			} else if strings.HasPrefix(trimmed, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				meaningful = eventName != "ping" && eventName != "heartbeat" &&
					eventName != "keepalive" && isRelayStreamFirstResponsePayload(payload)
			}
		}
		if errors.Is(context.Cause(body.requestContext), body.timeoutError) {
			return body.firstResponseError(body.timeoutError)
		}
		if meaningful {
			body.stopTimer()
			response.Body = &relayPrefetchedResponseBody{
				Reader: io.MultiReader(bytes.NewReader(prefix.Bytes()), reader),
				Closer: body,
			}
			return nil
		}
		if err != nil {
			return body.firstResponseError(err)
		}
	}
}

func isRelayStreamFirstResponsePayload(payload string) bool {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "[DONE]" {
		return false
	}
	switch strings.ToLower(payload) {
	case "ping", "[ping]", "heartbeat", "[heartbeat]", "keepalive", "[keepalive]":
		return false
	}
	var envelope struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	if common.UnmarshalJsonStr(payload, &envelope) == nil {
		eventType := strings.ToLower(strings.TrimSpace(envelope.Type))
		eventName := strings.ToLower(strings.TrimSpace(envelope.Event))
		if eventType == "ping" || eventType == "heartbeat" || eventType == "keepalive" ||
			eventName == "ping" || eventName == "heartbeat" || eventName == "keepalive" {
			return false
		}
	}
	return true
}

func (transport *relayResponseHeaderTimeoutTransport) CloseIdleConnections() {
	if closer, ok := transport.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (body *relayResponseBody) stopTimer() {
	body.timerStopOnce.Do(func() {
		if body.timer != nil && !body.timer.Stop() && body.timerDone != nil {
			<-body.timerDone
		}
	})
}

func (body *relayResponseBody) firstResponseError(readErr error) error {
	body.stopTimer()
	cause := context.Cause(body.requestContext)
	_ = body.Close()
	if errors.Is(cause, body.timeoutError) {
		return fmt.Errorf("%w（%s）", body.timeoutError, body.timeout)
	}
	if cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	if errors.Is(cause, context.Canceled) && !errors.Is(readErr, io.EOF) {
		return cause
	}
	if errors.Is(readErr, io.EOF) {
		return errors.New("上游流式响应在首字前结束")
	}
	return fmt.Errorf("读取上游流式首字失败: %w", readErr)
}

func (body *relayResponseBody) Close() error {
	body.stopTimer()
	defer body.cancel(nil)
	return body.ReadCloser.Close()
}
