package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnoseUpstreamRequestErrorClassifiesTransportFailures(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		viaProxy bool
		category string
		summary  string
	}{
		{
			name:     "dns lookup",
			err:      &url.Error{Op: "Post", URL: "https://api.secret.example/v1", Err: &net.DNSError{Name: "api.secret.example", IsNotFound: true}},
			category: UpstreamErrorCategoryDNS,
			summary:  "上游域名解析失败",
		},
		{
			name:     "connection refused",
			err:      fmtWrap(syscall.ECONNREFUSED),
			category: UpstreamErrorCategoryConnectionRefused,
			summary:  "上游连接被拒绝",
		},
		{
			name:     "response header timeout",
			err:      fmtWrap(errRelayResponseHeaderTimeout),
			category: UpstreamErrorCategoryResponseTimeout,
			summary:  "等待上游响应超时",
		},
		{
			name:     "stream first response timeout",
			err:      fmtWrap(errRelayStreamFirstResponseTimeout),
			category: UpstreamErrorCategoryResponseTimeout,
			summary:  "等待上游流式首字超时",
		},
		{
			name:     "connect timeout",
			err:      context.DeadlineExceeded,
			category: UpstreamErrorCategoryConnectTimeout,
			summary:  "连接上游超时",
		},
		{
			name:     "tls failure",
			err:      errors.New("tls: handshake failure"),
			category: UpstreamErrorCategoryTLS,
			summary:  "上游 TLS 握手或证书校验失败",
		},
		{
			name:     "connection reset",
			err:      fmtWrap(syscall.ECONNRESET),
			category: UpstreamErrorCategoryConnectionReset,
			summary:  "上游连接被重置",
		},
		{
			name:     "stream ended before first response",
			err:      errors.New("上游流式响应在首字前结束"),
			category: UpstreamErrorCategoryConnectionClosed,
			summary:  "上游在返回响应前关闭连接",
		},
		{
			name:     "proxy configuration",
			err:      errors.New("unsupported proxy scheme"),
			viaProxy: true,
			category: UpstreamErrorCategoryProxy,
			summary:  "渠道代理连接失败",
		},
	}

	request, err := http.NewRequest(http.MethodPost, "https://api.secret.example:8443/v1/responses?key=secret", nil)
	require.NoError(t, err)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostic := DiagnoseUpstreamRequestError(request, test.err, test.viaProxy)

			assert.Equal(t, test.category, diagnostic.Category)
			assert.Equal(t, test.summary, diagnostic.Summary)
			assert.Equal(t, "api.secret.example:8443", diagnostic.Host)
			assert.Equal(t, test.viaProxy, diagnostic.ViaProxy)
			assert.NotContains(t, diagnostic.Detail, "?key=secret")
		})
	}
}

func TestDiagnoseUpstreamRequestErrorRemovesURLFromAdminDetail(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://api.secret.example/v1/responses?key=secret", nil)
	require.NoError(t, err)
	requestErr := &url.Error{
		Op:  "Post",
		URL: request.URL.String(),
		Err: &net.DNSError{Name: "api.secret.example", IsNotFound: true},
	}

	diagnostic := DiagnoseUpstreamRequestError(request, requestErr, false)

	assert.Equal(t, "api.secret.example", diagnostic.Host)
	assert.NotContains(t, diagnostic.Detail, request.URL.String())
	assert.NotContains(t, diagnostic.Detail, "api.secret.example")
}

func fmtWrap(err error) error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: err}
}
