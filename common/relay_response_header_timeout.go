package common

import (
	"sync/atomic"
	"time"
)

const (
	RelayResponseHeaderTimeoutOptionKey      = "RelayResponseHeaderTimeoutSeconds"
	DefaultRelayResponseHeaderTimeoutSeconds = 0
	MaxRelayResponseHeaderTimeoutSeconds     = 600
)

var relayResponseHeaderTimeoutSeconds atomic.Int64

func GetRelayResponseHeaderTimeoutSeconds() int {
	return int(relayResponseHeaderTimeoutSeconds.Load())
}

func GetRelayResponseHeaderTimeout() time.Duration {
	return time.Duration(GetRelayResponseHeaderTimeoutSeconds()) * time.Second
}

func SetRelayResponseHeaderTimeoutSeconds(seconds int) {
	if seconds < 0 || seconds > MaxRelayResponseHeaderTimeoutSeconds {
		seconds = DefaultRelayResponseHeaderTimeoutSeconds
	}
	relayResponseHeaderTimeoutSeconds.Store(int64(seconds))
}
