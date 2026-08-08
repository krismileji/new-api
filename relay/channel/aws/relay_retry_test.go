package aws

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleNovaRequestRejectsEmptyAcceptedMessageWithoutRetry(t *testing.T) {
	client := newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"output":{"message":{"content":[]}}}`)),
			Request:    request,
		}, nil
	}))

	recorder := httptest.NewRecorder()
	c := newAwsTestContext(recorder, context.Background())
	info := newAwsTestRelayInfo()
	info.IsStream = false
	a := &Adaptor{AwsClient: client, AwsReq: newAwsInvokeModelInput()}

	err, usage := handleNovaRequest(c, info, a)
	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, relaytypes.ErrorCodeBadResponseBody, err.GetErrorCode())
	assert.True(t, relaytypes.IsSkipRetryError(err))
}

func TestAwsStreamHandlerRejectsEmptyStream(t *testing.T) {
	originalRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	t.Cleanup(func() {
		common.RelayTimeout = originalRelayTimeout
	})

	client := newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		return newAwsStreamResponse(request, io.NopCloser(bytes.NewReader(nil))), nil
	}))
	adaptor := &Adaptor{AwsClient: client, AwsReq: newAwsStreamInput()}

	err, usage := awsStreamHandler(newAwsTestContext(httptest.NewRecorder(), context.Background()), newAwsTestRelayInfo(), adaptor)
	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, relaytypes.ErrorCodeBadResponseBody, err.GetErrorCode())
	assert.True(t, relaytypes.IsSkipRetryError(err))
}

func TestAwsStreamHandlerSkipsRetryAfterSdkTimeoutWithAcceptedStream(t *testing.T) {
	originalRelayTimeout := common.RelayTimeout
	common.RelayTimeout = 1
	t.Cleanup(func() {
		common.RelayTimeout = originalRelayTimeout
	})

	client := newAwsTestClient(awsHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		reader, writer := io.Pipe()
		go func() {
			<-request.Context().Done()
			_ = writer.CloseWithError(request.Context().Err())
		}()
		return newAwsStreamResponse(request, reader), nil
	}))
	adaptor := &Adaptor{AwsClient: client, AwsReq: newAwsStreamInput()}

	err, usage := awsStreamHandler(
		newAwsTestContext(httptest.NewRecorder(), context.Background()),
		newAwsTestRelayInfo(),
		adaptor,
	)
	require.Error(t, err)
	assert.Nil(t, usage)
	assert.Equal(t, relaytypes.ErrorCodeAwsInvokeError, err.GetErrorCode())
	assert.True(t, relaytypes.IsSkipRetryError(err))
}
