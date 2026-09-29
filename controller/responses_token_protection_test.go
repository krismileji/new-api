package controller

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProtectedResponsesWSTest(t *testing.T, status int, handle func(*websocket.Conn, *http.Request)) *responsesWSBillingTest {
	t.Helper()
	fixture := newResponsesWSBillingTest(t, `tier("usage", p * 2)`, handle)
	// Reopen only after registration is enabled, as in production startup.
	fixture.closeAndWait(t)
	t.Setenv("TOKEN_AUTO_DISABLE_JOURNAL_DIR", t.TempDir())
	require.NoError(t, model.DB.AutoMigrate(&model.TokenAutoDisableConfig{}, &model.TokenAutoDisableRecord{}))
	workerCtx, stopWorker := context.WithCancel(context.Background())
	require.NoError(t, service.InitTokenAutoDisable(workerCtx))
	t.Cleanup(func() {
		fixture.closeAndWait(t)
		require.NoError(t, service.FlushChannelDailyCostEvents())
		stopWorker()
		require.NoError(t, model.DB.Where("token_id = ?", fixture.token.Id).Delete(&model.TokenAutoDisableRecord{}).Error)
		require.NoError(t, model.DB.Where("id = ?", 1).Delete(&model.TokenAutoDisableConfig{}).Error)
		resetCtx, stopReset := context.WithCancel(context.Background())
		require.NoError(t, service.InitTokenAutoDisable(resetCtx))
		stopReset()
	})
	_, err := service.SaveTokenAutoDisableSettings(t.Context(), service.TokenAutoDisableSettings{Enabled: true,
		Rules: []service.TokenAutoDisableRule{{Id: "policy", Name: "测试规则", Enabled: true, ChannelIds: []int{fixture.channel.Id}, StatusCodes: []int{status}, Keywords: []string{"policy violation"}, ResponseStatus: 451, ResponseMessage: "此 Key 已禁用"}}})
	require.NoError(t, err)
	fixture.done = make(chan struct{})
	fixture.client, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(fixture.gatewayURL, "http")+"/v1/responses", http.Header{"Authorization": []string{"Bearer sk-" + fixture.token.Key}})
	require.NoError(t, err)
	_, err = service.SaveChannelConcurrencyLimit(t.Context(), fixture.channel.Id, 1)
	require.NoError(t, err)
	return fixture
}

func assertResponsesWSTokenDisabled(t *testing.T, fixture *responsesWSBillingTest, stream string, quotas []int) {
	t.Helper()
	event := readResponsesWSTestEvent(t, fixture.client)
	require.Equal(t, "error", event["type"])
	assert.Equal(t, float64(451), event["status"])
	if stream != "" {
		assert.Equal(t, stream, event["stream_id"])
		assert.Equal(t, "create-protected", event["event_id"])
	}
	errorBody, ok := event["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, string(service.TokenAutoDisabledCode), errorBody["code"])
	assert.Equal(t, "此 Key 已禁用", errorBody["message"])
	require.NoError(t, fixture.client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err := fixture.client.ReadMessage()
	assert.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation), "expected close 1008, got %v", err)
	fixture.closeAndWait(t)
	charged := 0
	for _, quota := range quotas {
		charged += quota
	}
	// BillingSession refunds asynchronously after the socket worker exits.
	// Wait for the public accounting result before asserting exact balances.
	require.Eventually(t, func() bool {
		var user model.User
		var token model.Token
		return model.DB.First(&user, fixture.user.Id).Error == nil &&
			model.DB.First(&token, fixture.token.Id).Error == nil &&
			user.Quota == 100000-charged && token.RemainQuota == 3000-charged
	}, 3*time.Second, 10*time.Millisecond)
	assertResponsesWSAccounting(t, fixture, quotas)
	snapshot, err := service.GetChannelConcurrencySnapshotWithRPM(t.Context())
	require.NoError(t, err)
	assert.Zero(t, snapshot[fixture.channel.Id].Active)
	var records []model.TokenAutoDisableRecord
	require.NoError(t, model.DB.Where("token_id = ?", fixture.token.Id).Find(&records).Error)
	require.Len(t, records, 1)
	assert.Equal(t, http.StatusSwitchingProtocols, records[0].UpstreamStatus)
	assert.Equal(t, fixture.channel.Id, records[0].ChannelId)
	assert.Equal(t, common.TokenStatusDisabled, fixture.token.Status)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fixture.gatewayURL+"/v1/responses", nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusUnavailableForLegalReasons, response.StatusCode)
}

func TestResponsesWSTokenProtectionExternalRevocation(t *testing.T) {
	for _, phase := range []string{"idle-before-create", "active", "idle-after-completion"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newProtectedResponsesWSTest(t, 101, func(ws *websocket.Conn, _ *http.Request) {
				if _, _, err := ws.ReadMessage(); err != nil {
					return
				}
				body := `{"type":"response.created","response":{"id":"protected","status":"in_progress"}}`
				if phase == "idle-after-completion" {
					body = `{"type":"response.completed","response":{"id":"protected","status":"completed","usage":{"input_tokens":20,"output_tokens":5}}}`
				}
				if err := ws.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
					return
				}
				_, _, _ = ws.ReadMessage()
			})
			stream := ""
			var quotas []int
			if phase != "idle-before-create" {
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","event_id":"create-protected","stream_id":"protected","model":"ws-billing","input":"hello"}`)))
				event := readResponsesWSTestEvent(t, fixture.client)
				if phase == "active" {
					require.Equal(t, "response.created", event["type"])
					stream = "protected"
					quotas = []int{0}
				} else {
					require.Equal(t, "response.completed", event["type"])
					quotas = []int{20}
				}
			}
			trigger, unregister, denied := service.RegisterTokenProtection(t.Context(), fixture.token, "external")
			require.Nil(t, denied)
			defer unregister()
			service.ObserveTokenAutoDisableError(trigger, fixture.channel.Id, 101, "policy violation")
			assertResponsesWSTokenDisabled(t, fixture, stream, quotas)
		})
	}
}

func TestResponsesWSTokenProtectionUpstreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		quotas     []int
	}{
		{"rejected-create", `{"type":"error","status":403,"stream_id":"protected","error":{"type":"permission_error","code":"policy","message":"policy violation"}}`, nil},
		{"failed-with-usage", `{"type":"response.failed","stream_id":"protected","response":{"id":"protected","status":"failed","error":{"code":"policy","message":"policy violation"},"usage":{"input_tokens":20,"output_tokens":5}}}`, []int{20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newProtectedResponsesWSTest(t, 101, func(ws *websocket.Conn, _ *http.Request) {
				if _, _, err := ws.ReadMessage(); err != nil {
					return
				}
				if err := ws.WriteMessage(websocket.TextMessage, []byte(tc.body)); err != nil {
					return
				}
				_, _, _ = ws.ReadMessage()
			})
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","event_id":"create-protected","stream_id":"protected","model":"ws-billing","input":"hello"}`)))
			assertResponsesWSTokenDisabled(t, fixture, "protected", tc.quotas)
		})
	}
}

func TestResponsesWSTokenProtectionIgnoresUnrelatedEvents(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"model-output", `{"type":"response.output_text.delta","stream_id":"protected","delta":"policy violation"}`, 101},
		{"another-stream", `{"type":"error","status":403,"stream_id":"other","error":{"message":"policy violation"}}`, 101},
		{"another-response-stream", `{"type":"response.failed","stream_id":"other","response":{"id":"other","error":{"message":"policy violation"}}}`, 101},
		{"body-status-does-not-match-handshake", `{"type":"error","status":403,"stream_id":"protected","error":{"message":"policy violation"}}`, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newProtectedResponsesWSTest(t, tc.status, func(ws *websocket.Conn, _ *http.Request) {
				if _, _, err := ws.ReadMessage(); err != nil {
					return
				}
				if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"protected","status":"in_progress"}}`)); err != nil {
					return
				}
				if err := ws.WriteMessage(websocket.TextMessage, []byte(tc.body)); err != nil {
					return
				}
				if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","stream_id":"protected","response":{"id":"protected","status":"completed","usage":{"input_tokens":20,"output_tokens":5}}}`)); err != nil {
					return
				}
				_, _, _ = ws.ReadMessage()
			})
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","stream_id":"protected","model":"ws-billing","input":"hello"}`)))
			require.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
			event := readResponsesWSTestEvent(t, fixture.client)
			assert.NotEqual(t, float64(451), event["status"])
			if tc.status == 101 {
				require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
			}
			fixture.closeAndWait(t)
			var count int64
			require.NoError(t, model.DB.Model(&model.TokenAutoDisableRecord{}).Where("token_id = ?", fixture.token.Id).Count(&count).Error)
			assert.Zero(t, count)
			require.NoError(t, model.DB.First(fixture.token, fixture.token.Id).Error)
			assert.Equal(t, common.TokenStatusEnabled, fixture.token.Status)
			if tc.status == 101 {
				assertResponsesWSAccounting(t, fixture, []int{20})
			}
		})
	}
}

func TestResponsesWSTokenProtectionIdleUpstreamError(t *testing.T) {
	sendError := make(chan struct{})
	fixture := newProtectedResponsesWSTest(t, 101, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"finished","status":"completed","usage":{"input_tokens":20,"output_tokens":5}}}`)); err != nil {
			return
		}
		select {
		case <-sendError:
		case <-t.Context().Done():
			return
		}
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","error":{"message":"policy violation"}}`)); err != nil {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	close(sendError)
	assertResponsesWSTokenDisabled(t, fixture, "", []int{20})
}

func TestResponsesWSTokenProtectionRefundsRejectedReservation(t *testing.T) {
	created, reject := make(chan struct{}), make(chan struct{})
	fixture := newProtectedResponsesWSTest(t, 101, func(ws *websocket.Conn, _ *http.Request) {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
		}
		close(created)
		select {
		case <-reject:
		case <-t.Context().Done():
			return
		}
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":403,"error":{"message":"policy violation"}}`)); err != nil {
			return
		}
		_, _, _ = ws.ReadMessage()
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{"billing_setting.billing_expr": `{"ws-billing":"tier(\"request\", fixed(0.002))"}`}))
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","event_id":"create-protected","stream_id":"protected","model":"ws-billing","input":"hello"}`)))
	select {
	case <-created:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream create was not received")
	}
	var reservedUser model.User
	var reservedToken model.Token
	require.NoError(t, model.DB.First(&reservedUser, fixture.user.Id).Error)
	require.NoError(t, model.DB.First(&reservedToken, fixture.token.Id).Error)
	assert.Equal(t, 99000, reservedUser.Quota)
	assert.Equal(t, 2000, reservedToken.RemainQuota)
	close(reject)
	assertResponsesWSTokenDisabled(t, fixture, "protected", nil)
}
