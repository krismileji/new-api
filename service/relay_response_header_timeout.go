package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var errRelayResponseHeaderTimeout = errors.New("等待上游响应超时")

type relayResponseHeaderTimeoutTransport struct {
	base            http.RoundTripper
	timeoutProvider func() time.Duration
}

type relayResponseBody struct {
	io.ReadCloser
	cancel context.CancelCauseFunc
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

func (transport *relayResponseHeaderTimeoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	timeout := transport.timeoutProvider()
	if timeout <= 0 {
		return transport.base.RoundTrip(request)
	}

	requestContext, cancel := context.WithCancelCause(request.Context())
	timerDone := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		cancel(errRelayResponseHeaderTimeout)
		close(timerDone)
	})
	response, err := transport.base.RoundTrip(request.Clone(requestContext))
	timerStopped := timer.Stop()
	if !timerStopped {
		<-timerDone
	}
	if err != nil {
		cause := context.Cause(requestContext)
		cancel(nil)
		if errors.Is(cause, errRelayResponseHeaderTimeout) {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, fmt.Errorf("%w（%s）", errRelayResponseHeaderTimeout, timeout)
		}
		return response, err
	}
	if errors.Is(context.Cause(requestContext), errRelayResponseHeaderTimeout) {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		cancel(nil)
		return nil, fmt.Errorf("%w（%s）", errRelayResponseHeaderTimeout, timeout)
	}
	if response == nil || response.Body == nil {
		cancel(nil)
		return response, nil
	}

	response.Body = &relayResponseBody{
		ReadCloser: response.Body,
		cancel:     cancel,
	}
	return response, nil
}

func (transport *relayResponseHeaderTimeoutTransport) CloseIdleConnections() {
	if closer, ok := transport.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (body *relayResponseBody) Close() error {
	defer body.cancel(nil)
	return body.ReadCloser.Close()
}
