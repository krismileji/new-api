package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokenProtectionCancelOnWrite struct {
	gin.ResponseWriter
	cancel context.CancelCauseFunc
}

func (writer *tokenProtectionCancelOnWrite) Write(data []byte) (int, error) {
	n, err := writer.ResponseWriter.Write(data)
	writer.cancel(&service.TokenAutoDisableCause{Record: model.TokenAutoDisableRecord{ResponseStatus: 451, ResponseMessage: "Key 已被禁用"}})
	return n, err
}

func TestOpenAIStreamTokenProtectionRetainsReceivedUsage(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	c.Writer = &tokenProtectionCancelOnWrite{ResponseWriter: c.Writer, cancel: cancel}
	reader, writer := io.Pipe()
	defer reader.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer writer.Close()
		_, _ = io.WriteString(writer, "data: {\"id\":\"partial\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n"+
			"data: {\"id\":\"partial\",\"model\":\"test\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":4,\"total_tokens\":14}}\n\n")
		<-ctx.Done()
	}()
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, DisablePing: true}
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "test"}
	usage, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: reader})
	<-finished
	require.NotNil(t, service.TokenAutoDisableFromContext(ctx))
	require.Nil(t, apiErr, "partial usage must reach normal settlement rather than the full-refund error path")
	require.NotNil(t, usage)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 4, usage.CompletionTokens)
	assert.Equal(t, 14, usage.TotalTokens)
}

func TestRealtimeTokenProtectionClosesUpstreamAndSendsError(t *testing.T) {
	upgrader := websocket.Upgrader{}
	upstreamGone := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		close(upstreamGone)
	}))
	defer upstream.Close()
	ready := make(chan context.CancelCauseFunc, 1)
	finished := make(chan struct{})
	engine := gin.New()
	engine.GET("/realtime", func(c *gin.Context) {
		client, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer client.Close()
		target, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(upstream.URL, "http"), nil)
		if err != nil {
			return
		}
		defer target.Close()
		ctx, cancel := context.WithCancelCause(c.Request.Context())
		defer cancel(nil)
		c.Request = c.Request.WithContext(ctx)
		ready <- cancel
		_, _ = OpenaiRealtimeHandler(c, &relaycommon.RelayInfo{ClientWs: client, TargetWs: target})
		close(finished)
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/realtime", nil)
	require.NoError(t, err)
	defer client.Close()
	select {
	case cancel := <-ready:
		cancel(&service.TokenAutoDisableCause{Record: model.TokenAutoDisableRecord{ResponseStatus: 451, ResponseMessage: "Key 已被禁用"}})
	case <-time.After(5 * time.Second):
		t.Fatal("realtime session did not start")
	}
	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, message, err := client.ReadMessage()
	require.NoError(t, err)
	var event struct {
		Type  string `json:"type"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(message, &event))
	assert.Equal(t, "error", event.Type)
	assert.Equal(t, "api_key_auto_disabled", event.Error.Code)
	assert.Equal(t, "Key 已被禁用", event.Error.Message)
	_, _, err = client.ReadMessage()
	assert.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation))
	select {
	case <-upstreamGone:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream websocket stayed open")
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("realtime handler did not exit")
	}
}
