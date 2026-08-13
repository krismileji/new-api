package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

type channelModelDetectorResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type channelModelDetectorDialer struct {
	resolver    channelModelDetectorResolver
	dialContext func(ctx context.Context, network, address string) (net.Conn, error)
}

func newChannelModelDetectorHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{Transport: newChannelModelDetectorTransport(net.DefaultResolver, dialer.DialContext)}
}

func newChannelModelDetectorTransport(resolver channelModelDetectorResolver, dialContext func(context.Context, string, string) (net.Conn, error)) *http.Transport {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialContext == nil {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		dialContext = dialer.DialContext
	}
	protectedDialer := &channelModelDetectorDialer{resolver: resolver, dialContext: dialContext}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           protectedDialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

func (d *channelModelDetectorDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d == nil || d.resolver == nil || d.dialContext == nil {
		return nil, fmt.Errorf("检测器安全拨号器未初始化")
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("检测器不支持拨号网络 %q", network)
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("检测器拨号地址无效: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("检测器拨号端口无效")
	}

	if ip := net.ParseIP(host); ip != nil {
		if !channelModelDetectionAllowedTargetIP(ip) {
			return nil, ErrChannelModelDetectionInvalidDetectorTarget
		}
		if !channelModelDetectorNetworkAllowsIP(network, ip) {
			return nil, fmt.Errorf("检测器地址与拨号网络不匹配")
		}
		return d.dialContext(ctx, network, net.JoinHostPort(ip.String(), portText))
	}
	if !channelModelDetectionStaticHostname(host) {
		return nil, ErrChannelModelDetectionInvalidDetectorTarget
	}

	resolved, err := d.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("检测器地址解析失败: %w", err)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("检测器地址未解析到可用 IP")
	}

	candidates := make([]net.IP, 0, len(resolved))
	// Validate every answer before dialing any candidate so mixed DNS results
	// cannot fall through to an otherwise allowed private address.
	for _, resolvedAddress := range resolved {
		if resolvedAddress.Zone != "" || !channelModelDetectionAllowedTargetIP(resolvedAddress.IP) {
			return nil, ErrChannelModelDetectionInvalidDetectorTarget
		}
		if channelModelDetectorNetworkAllowsIP(network, resolvedAddress.IP) {
			candidates = append(candidates, resolvedAddress.IP)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("检测器地址没有与拨号网络匹配的 IP")
	}

	var lastErr error
	for _, ip := range candidates {
		connection, dialErr := d.dialContext(ctx, network, net.JoinHostPort(ip.String(), portText))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func channelModelDetectorNetworkAllowsIP(network string, ip net.IP) bool {
	if network == "tcp4" {
		return ip.To4() != nil
	}
	if network == "tcp6" {
		return ip.To4() == nil
	}
	return true
}
