package common

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSMTPTimeoutCoversUnresponsiveGreeting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	oldSSL, oldTLS, oldPort := SMTPSSLEnabled, SMTPStartTLSEnabled, SMTPPort
	SMTPSSLEnabled, SMTPStartTLSEnabled, SMTPPort = false, false, 25
	t.Cleanup(func() { SMTPSSLEnabled, SMTPStartTLSEnabled, SMTPPort = oldSSL, oldTLS, oldPort })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	client, err := newSMTPClientWithTimeout(listener.Addr().String(), 50*time.Millisecond)
	require.Error(t, err)
	assert.Nil(t, client)
	var networkError net.Error
	require.ErrorAs(t, err, &networkError)
	assert.True(t, networkError.Timeout())
	conn := <-accepted
	require.NoError(t, conn.Close())
}
