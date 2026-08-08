package channel

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type pingKeepAliveResponseBody struct {
	io.ReadCloser
	stop context.CancelFunc
	done <-chan struct{}
	once sync.Once
}

func (b *pingKeepAliveResponseBody) Close() error {
	b.once.Do(func() {
		if b.stop != nil {
			b.stop()
		}
		if b.done != nil {
			<-b.done
		}
	})
	return b.ReadCloser.Close()
}

func attachPingKeepAlive(c *gin.Context, response *http.Response, info *common.RelayInfo) {
	if c == nil || response == nil || response.Body == nil || info == nil || !info.IsStream || info.DisablePing {
		return
	}
	settings := operation_setting.GetGeneralSetting()
	if !settings.PingIntervalEnabled {
		return
	}
	interval := time.Duration(settings.PingIntervalSeconds) * time.Second
	stop, done := startPingKeepAlive(c, interval)
	response.Body = &pingKeepAliveResponseBody{
		ReadCloser: response.Body,
		stop:       stop,
		done:       done,
	}
}
