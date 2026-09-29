package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeMonitorDrainPreservesTerminalUsage(t *testing.T) {
	for _, phase := range []string{"active", "idle"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newResponsesWSBillingTest(t, `tier("usage", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
				if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.created","session":{}}`)); err != nil {
					return
				}
				_, request, err := ws.ReadMessage()
				if err != nil {
					return
				}
				var create map[string]any
				if !assert.NoError(t, common.Unmarshal(request, &create)) {
					return
				}
				assert.NotEmpty(t, create["event_id"], "anonymous creates need an upstream error correlation ID")
				if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"rt"}}`)); err != nil {
					return
				}
				_, control, err := ws.ReadMessage()
				if err != nil {
					return
				}
				assert.Contains(t, string(control), "response.cancel", "new create must be rejected before reaching upstream")
				_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"rt","status":"cancelled","usage":{"total_tokens":25,"input_tokens":20,"output_tokens":5,"input_token_details":{"text_tokens":20},"output_token_details":{"text_tokens":5}}}}`))
				_, _, _ = ws.ReadMessage()
			})
			disable := setupResponsesMonitorDisable(t, fixture.channel, "low-balance")
			done := make(chan *dto.RealtimeUsage, 1)
			engine := gin.New()
			engine.GET("/realtime", func(c *gin.Context) {
				client, err := (&websocket.Upgrader{}).Upgrade(c.Writer, c.Request, nil)
				if !assert.NoError(t, err) {
					return
				}
				defer client.Close()
				target, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(*fixture.channel.BaseURL, "http"), nil)
				if !assert.NoError(t, err) {
					return
				}
				defer target.Close()
				apiErr, usage := openai.OpenaiRealtimeHandler(c, &relaycommon.RelayInfo{
					ClientWs: client, TargetWs: target, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: fixture.channel.Id, UpstreamModelName: "gpt-4o"},
					UsePrice: true, OriginModelName: "gpt-4o",
				})
				assert.Nil(t, apiErr)
				done <- usage
			})
			server := httptest.NewServer(engine)
			t.Cleanup(server.Close)
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/realtime", nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			require.Equal(t, "session.created", readResponsesWSTestEvent(t, client)["type"])
			if phase == "active" {
				require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`)))
				require.Equal(t, "response.created", readResponsesWSTestEvent(t, client)["type"])
			}
			disable()
			if phase == "active" {
				require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","event_id":"second"}`)))
				rejection := readResponsesWSTestEvent(t, client)
				require.Equal(t, "error", rejection["type"])
				assert.Equal(t, "channel_disabled", rejection["error"].(map[string]any)["code"])
				require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","response_id":"rt"}`)))
				require.Equal(t, "response.done", readResponsesWSTestEvent(t, client)["type"])
			}
			_, _, err = client.ReadMessage()
			require.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation), "%v", err)
			select {
			case usage := <-done:
				if phase == "active" {
					assert.Equal(t, 25, usage.TotalTokens)
					assert.Equal(t, 20, usage.InputTokens)
					assert.Equal(t, 5, usage.OutputTokens)
				} else {
					assert.Zero(t, usage.TotalTokens)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Realtime handler did not finish")
			}
			fixture.closeAndWait(t)
			require.NoError(t, service.FlushChannelDailyCostEvents())
			var logs int64
			require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
			assert.Zero(t, logs, "fixed-price handler only accumulates usage; settlement remains with its caller")
		})
	}
}
