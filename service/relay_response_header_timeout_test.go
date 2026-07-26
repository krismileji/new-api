package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type relayRoundTripFunc func(*http.Request) (*http.Response, error)

func (function relayRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type relayCloseIdleTransport struct {
	closed bool
}

func (transport *relayCloseIdleTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil
}

func (transport *relayCloseIdleTransport) CloseIdleConnections() {
	transport.closed = true
}

func TestRelayResponseHeaderTimeoutTransportCancelsBlockedAttempt(t *testing.T) {
	transport := &relayResponseHeaderTimeoutTransport{
		base: relayRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, context.Cause(request.Context())
		}),
		timeoutProvider: func() time.Duration {
			return time.Nanosecond
		},
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)

	require.Error(t, err)
	assert.ErrorIs(t, err, errRelayResponseHeaderTimeout)
	assert.Nil(t, response)
}

func TestRelayResponseHeaderTimeoutTransportStopsTimerAfterHeaders(t *testing.T) {
	var upstreamContext context.Context
	transport := &relayResponseHeaderTimeoutTransport{
		base: relayRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			upstreamContext = request.Context()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
		timeoutProvider: func() time.Duration {
			return time.Hour
		},
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)

	require.NoError(t, err)
	require.NotNil(t, response)
	select {
	case <-upstreamContext.Done():
		require.FailNow(t, "upstream context was canceled before the response body closed")
	default:
	}
	require.NoError(t, response.Body.Close())
	assert.ErrorIs(t, context.Cause(upstreamContext), context.Canceled)
}

func TestRelayResponseHeaderTimeoutTransportPreservesClientCancellation(t *testing.T) {
	transport := &relayResponseHeaderTimeoutTransport{
		base: relayRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, context.Cause(request.Context())
		}),
		timeoutProvider: func() time.Duration {
			return time.Hour
		},
	}
	requestContext, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	cancel()

	response, err := transport.RoundTrip(request)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, errRelayResponseHeaderTimeout)
	assert.Nil(t, response)
}

func TestRelayResponseHeaderTimeoutTransportPreservesIdleConnectionCleanup(t *testing.T) {
	base := &relayCloseIdleTransport{}
	transport := &relayResponseHeaderTimeoutTransport{
		base: base,
		timeoutProvider: func() time.Duration {
			return 0
		},
	}

	transport.CloseIdleConnections()

	assert.True(t, base.closed)
}
