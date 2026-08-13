package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelModelDetectorResolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (fn channelModelDetectorResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return fn(ctx, host)
}

func channelModelDetectorPipe(t *testing.T) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client
}

func TestChannelModelDetectorDialerRejectsMixedResolution(t *testing.T) {
	dialCalls := 0
	dialer := &channelModelDetectorDialer{
		resolver: channelModelDetectorResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.8")}, {IP: net.ParseIP("8.8.8.8")}}, nil
		}),
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return channelModelDetectorPipe(t), nil
		},
	}

	connection, err := dialer.DialContext(context.Background(), "tcp", "detector.internal:8080")
	require.ErrorIs(t, err, ErrChannelModelDetectionInvalidDetectorTarget)
	assert.Nil(t, connection)
	assert.Zero(t, dialCalls)
}

func TestChannelModelDetectorDialerRevalidatesDNSOnEveryDial(t *testing.T) {
	var mutex sync.Mutex
	lookupCalls := 0
	dialCalls := 0
	dialer := &channelModelDetectorDialer{
		resolver: channelModelDetectorResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			mutex.Lock()
			defer mutex.Unlock()
			lookupCalls++
			if lookupCalls == 1 {
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.9")}}, nil
			}
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.9")}}, nil
		}),
		dialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			mutex.Lock()
			defer mutex.Unlock()
			dialCalls++
			assert.Equal(t, "10.0.0.9:8080", address)
			return channelModelDetectorPipe(t), nil
		},
	}

	connection, err := dialer.DialContext(context.Background(), "tcp", "detector.internal:8080")
	require.NoError(t, err)
	require.NoError(t, connection.Close())

	connection, err = dialer.DialContext(context.Background(), "tcp", "detector.internal:8080")
	require.ErrorIs(t, err, ErrChannelModelDetectionInvalidDetectorTarget)
	assert.Nil(t, connection)
	assert.Equal(t, 2, lookupCalls)
	assert.Equal(t, 1, dialCalls)
}

func TestChannelModelDetectorDialerRejectsInvalidOrDisallowedAddresses(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{name: "missing port", address: "10.0.0.1"},
		{name: "zero port", address: "10.0.0.1:0"},
		{name: "public IPv4", address: "8.8.8.8:443"},
		{name: "link local IPv4", address: "169.254.169.254:80"},
		{name: "unspecified IPv4", address: "0.0.0.0:80"},
		{name: "multicast IPv4", address: "224.0.0.1:80"},
		{name: "link local IPv6", address: "[fe80::1]:443"},
		{name: "unspecified IPv6", address: "[::]:443"},
		{name: "multicast IPv6", address: "[ff02::1]:443"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dialer := &channelModelDetectorDialer{
				resolver: channelModelDetectorResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
					t.Fatal("IP 字面量不应触发 DNS 解析")
					return nil, nil
				}),
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					t.Fatal("非法地址不应进入底层拨号")
					return nil, nil
				},
			}

			connection, err := dialer.DialContext(context.Background(), "tcp", test.address)
			require.Error(t, err)
			assert.Nil(t, connection)
		})
	}
}

func TestChannelModelDetectorDialerRejectsEmptyAndInvalidDNSAnswers(t *testing.T) {
	tests := []struct {
		name      string
		addresses []net.IPAddr
	}{
		{name: "empty"},
		{name: "nil IP", addresses: []net.IPAddr{{}}},
		{name: "scoped IP", addresses: []net.IPAddr{{IP: net.ParseIP("fd00::1"), Zone: "eth0"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dialer := &channelModelDetectorDialer{
				resolver: channelModelDetectorResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
					return test.addresses, nil
				}),
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					t.Fatal("非法 DNS 结果不应进入底层拨号")
					return nil, nil
				},
			}

			connection, err := dialer.DialContext(context.Background(), "tcp", "detector.internal:8080")
			require.Error(t, err)
			assert.Nil(t, connection)
		})
	}
}

func TestChannelModelDetectorTransportDialsValidatedIPAndPreservesTLSHostname(t *testing.T) {
	type requestIdentity struct {
		host       string
		serverName string
	}
	identity := make(chan requestIdentity, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		identity <- requestIdentity{host: request.Host, serverName: request.TLS.ServerName}
		response.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	serverIP := net.ParseIP(serverURL.Hostname())
	require.NotNil(t, serverIP)
	port := serverURL.Port()

	dialed := make(chan string, 1)
	networkDialer := &net.Dialer{}
	transport := newChannelModelDetectorTransport(
		channelModelDetectorResolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			assert.Equal(t, "detector.example.com", host)
			return []net.IPAddr{{IP: serverIP}}, nil
		}),
		func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed <- address
			return networkDialer.DialContext(ctx, network, address)
		},
	)
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12}
	client := &http.Client{Transport: transport}

	response, err := client.Get("https://detector.example.com:" + port + "/api/health")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, net.JoinHostPort(serverIP.String(), port), <-dialed)
	seen := <-identity
	assert.Equal(t, "detector.example.com:"+port, seen.host)
	assert.Equal(t, "detector.example.com", seen.serverName)
}

func TestChannelModelDetectorClientUsesDedicatedDirectTransport(t *testing.T) {
	client, err := NewChannelModelDetectorClient("http://127.0.0.1:18080")
	require.NoError(t, err)
	transport, ok := client.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Nil(t, transport.Proxy)
	assert.NotNil(t, transport.DialContext)

	explicitClient := &http.Client{Transport: channelModelDetectorRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("not used")
	})}
	injected, err := NewChannelModelDetectorClientWithHTTPClient("https://detector.example", explicitClient)
	require.NoError(t, err)
	assert.Same(t, explicitClient, injected.httpClient)
}
