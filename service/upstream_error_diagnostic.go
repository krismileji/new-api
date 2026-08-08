package service

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

const (
	UpstreamErrorDiagnosticContextKey constant.ContextKey = "upstream_error_diagnostic"
	// UpstreamResponseStatusContextKey stores the status of the most recent
	// upstream response for the current relay attempt. The retry controller uses
	// it to distinguish a response parsing failure after an accepted request
	// from a transport failure that is safe to retry.
	UpstreamResponseStatusContextKey constant.ContextKey = "upstream_response_status"
	// UpstreamRequestWrittenContextKey records whether net/http finished writing
	// the current request. Task creation retries use it to avoid replaying a POST
	// that may already have created an upstream task before the connection died.
	UpstreamRequestWrittenContextKey constant.ContextKey = "upstream_request_written"

	UpstreamErrorCategoryDNS               = "dns_error"
	UpstreamErrorCategoryConnectionRefused = "connection_refused"
	UpstreamErrorCategoryConnectTimeout    = "connect_timeout"
	UpstreamErrorCategoryResponseTimeout   = "response_timeout"
	UpstreamErrorCategoryTLS               = "tls_error"
	UpstreamErrorCategoryProxy             = "proxy_error"
	UpstreamErrorCategoryConnectionReset   = "connection_reset"
	UpstreamErrorCategoryConnectionClosed  = "connection_closed"
	UpstreamErrorCategoryNetwork           = "network_error"
)

type UpstreamErrorDiagnostic struct {
	Category string `json:"category"`
	Summary  string `json:"summary"`
	Host     string `json:"host,omitempty"`
	Detail   string `json:"detail,omitempty"`
	ViaProxy bool   `json:"via_proxy,omitempty"`
}

func DiagnoseUpstreamRequestError(request *http.Request, err error, viaProxy bool) UpstreamErrorDiagnostic {
	diagnostic := UpstreamErrorDiagnostic{
		Category: UpstreamErrorCategoryNetwork,
		Summary:  "上游网络请求失败",
		Detail:   sanitizedUpstreamErrorDetail(err),
		ViaProxy: viaProxy,
	}
	if request != nil && request.URL != nil {
		diagnostic.Host = request.URL.Host
	}
	if err == nil {
		return diagnostic
	}

	lowerDetail := strings.ToLower(err.Error())
	if errors.Is(err, errRelayStreamFirstResponseTimeout) {
		diagnostic.Category = UpstreamErrorCategoryResponseTimeout
		diagnostic.Summary = "等待上游流式首字超时"
		return diagnostic
	}

	if errors.Is(err, errRelayResponseHeaderTimeout) ||
		strings.Contains(lowerDetail, "client.timeout exceeded while awaiting headers") ||
		strings.Contains(lowerDetail, "timeout awaiting response headers") {
		diagnostic.Category = UpstreamErrorCategoryResponseTimeout
		diagnostic.Summary = "等待上游响应超时"
		return diagnostic
	}

	if viaProxy && (strings.Contains(lowerDetail, "proxyconnect") ||
		strings.Contains(lowerDetail, "proxy error") ||
		strings.Contains(lowerDetail, "proxy scheme") ||
		strings.Contains(lowerDetail, "socks")) {
		diagnostic.Category = UpstreamErrorCategoryProxy
		diagnostic.Summary = "渠道代理连接失败"
		return diagnostic
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		diagnostic.Category = UpstreamErrorCategoryDNS
		if dnsErr.IsTimeout {
			diagnostic.Summary = "上游域名解析超时"
		} else {
			diagnostic.Summary = "上游域名解析失败"
		}
		return diagnostic
	}

	var tlsVerificationErr *tls.CertificateVerificationError
	if errors.As(err, &tlsVerificationErr) ||
		strings.Contains(lowerDetail, "tls handshake timeout") ||
		strings.Contains(lowerDetail, "tls:") ||
		strings.Contains(lowerDetail, "x509:") ||
		strings.Contains(lowerDetail, "server gave http response to https client") {
		diagnostic.Category = UpstreamErrorCategoryTLS
		diagnostic.Summary = "上游 TLS 握手或证书校验失败"
		return diagnostic
	}

	if errors.Is(err, syscall.ECONNREFUSED) ||
		strings.Contains(lowerDetail, "connection refused") ||
		strings.Contains(lowerDetail, "actively refused") {
		diagnostic.Category = UpstreamErrorCategoryConnectionRefused
		diagnostic.Summary = "上游连接被拒绝"
		return diagnostic
	}

	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		strings.Contains(lowerDetail, "connection reset") ||
		strings.Contains(lowerDetail, "forcibly closed") ||
		strings.Contains(lowerDetail, "broken pipe") {
		diagnostic.Category = UpstreamErrorCategoryConnectionReset
		diagnostic.Summary = "上游连接被重置"
		return diagnostic
	}

	if errors.Is(err, io.EOF) ||
		strings.Contains(lowerDetail, "unexpected eof") ||
		strings.Contains(lowerDetail, "上游流式响应在首字前结束") {
		diagnostic.Category = UpstreamErrorCategoryConnectionClosed
		diagnostic.Summary = "上游在返回响应前关闭连接"
		return diagnostic
	}

	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		diagnostic.Category = UpstreamErrorCategoryConnectTimeout
		diagnostic.Summary = "连接上游超时"
		return diagnostic
	}

	if viaProxy {
		diagnostic.Category = UpstreamErrorCategoryProxy
		diagnostic.Summary = "通过渠道代理请求上游失败"
	}
	return diagnostic
}

func sanitizedUpstreamErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	detail := err.Error()
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		detail = urlErr.Err.Error()
	}
	detail = strings.TrimSpace(common.MaskSensitiveInfo(detail))
	runes := []rune(detail)
	if len(runes) > 1024 {
		detail = string(runes[:1024]) + "..."
	}
	return detail
}
