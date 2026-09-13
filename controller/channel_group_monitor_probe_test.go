package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunChannelGroupMonitorGroupDispatchesAnthropicMessages(t *testing.T) {
	for _, modelName := range []string{"claude-sonnet-5", "claude-opus-5"} {
		t.Run(modelName, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			withSelfUseModeEnabled(t)
			service.InitHttpClient()
			originalStreamingTimeout := constant.StreamingTimeout
			constant.StreamingTimeout = 5
			t.Cleanup(func() { constant.StreamingTimeout = originalStreamingTimeout })
			originalRetryTimes := common.RetryTimes
			common.RetryTimes = 0
			t.Cleanup(func() { common.RetryTimes = originalRetryTimes })

			user := model.User{
				Username: "claude-group-probe-user", Password: "password",
				Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: 1_000_000,
			}
			require.NoError(t, db.Create(&user).Error)

			var requestCount atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/messages", r.URL.Path)
				assert.Equal(t, "claude-probe-key", r.Header.Get("x-api-key"))
				var request dto.ClaudeRequest
				if !assert.NoError(t, common.DecodeJson(r.Body, &request)) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				assert.Equal(t, modelName, request.Model)
				assert.Equal(t, common.GetPointer(true), request.Stream)
				assert.NotEmpty(t, request.Messages)
				w.Header().Set("Content-Type", "text/event-stream")
				_, err := fmt.Fprintf(w, `event: message_start
data: {"type":"message_start","message":{"id":"msg-probe","type":"message","role":"assistant","model":"%s","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":3,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}

`, modelName)
				assert.NoError(t, err)
			}))
			t.Cleanup(upstream.Close)

			channel := model.Channel{
				Id: 62, Name: "claude-group-probe", Type: constant.ChannelTypeAnthropic,
				Key: "claude-probe-key", Status: common.ChannelStatusEnabled, BaseURL: &upstream.URL,
				Models: modelName, Group: "claude-probe",
			}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&model.Ability{
				Group: channel.Group, Model: modelName, ChannelId: channel.Id, Enabled: true,
			}).Error)
			common.MemoryCacheEnabled = true
			model.InitChannelCache()
			require.NoError(t, db.AutoMigrate(&model.ChannelGroupMonitorExecution{}, &model.ChannelGroupMonitorState{}))

			candidates, err := getChannelGroupMonitorCandidateModels(context.Background(), true)
			require.NoError(t, err)
			require.Contains(t, candidates[channel.Group], modelName)
			claim := model.ChannelGroupMonitorClaim{
				RunId: "claude-group-probe-run", Config: model.ChannelGroupMonitorConfig{Enabled: true, Revision: 1},
			}
			require.NoError(t, runChannelGroupMonitorGroup(
				context.Background(), claim,
				model.ChannelGroupMonitorGroup{GroupName: channel.Group, ProbeModel: modelName},
				candidates, user.Id, nil,
			))

			var execution model.ChannelGroupMonitorExecution
			require.NoError(t, db.Where("run_id = ?", claim.RunId).First(&execution).Error)
			assert.Equal(t, model.ChannelGroupMonitorResultSuccess, execution.Result, execution.ErrorMessage)
			assert.Empty(t, execution.ErrorCode)
			assert.True(t, execution.RequestDispatched)
			assert.Equal(t, channel.Id, execution.ChannelId)
			assert.Equal(t, modelName, execution.ProbeModel)
			assert.Equal(t, int64(1), requestCount.Load())
		})
	}
}

func TestChannelGroupMonitorRejectsInvalidProbeModelsBeforeDispatch(t *testing.T) {
	tests := []struct {
		name      string
		channel   *model.Channel
		modelName string
		message   string
	}{
		{
			name: "missing channel", modelName: "claude-sonnet-5", message: "渠道不存在",
		},
		{
			name: "model removed from channel", modelName: "claude-opus-5",
			channel: &model.Channel{Type: constant.ChannelTypeAnthropic, Models: "claude-sonnet-5"},
			message: "模型 claude-opus-5 不在该渠道支持范围内",
		},
		{
			name: "wildcard probe", modelName: "claude-*",
			channel: &model.Channel{Type: constant.ChannelTypeAnthropic, Models: "*"},
			message: "探测模型必须是长度不超过 255 的具体模型名称",
		},
		{
			name:    "empty model",
			channel: &model.Channel{Type: constant.ChannelTypeAnthropic, Models: "*"},
			message: "探测模型必须是长度不超过 255 的具体模型名称",
		},
		{
			name: "oversized model", modelName: strings.Repeat("a", 256),
			channel: &model.Channel{Type: constant.ChannelTypeAnthropic, Models: "*"},
			message: "探测模型必须是长度不超过 255 的具体模型名称",
		},
		{
			name: "non-text model", modelName: "text-embedding-3-small",
			channel: &model.Channel{Type: constant.ChannelTypeOpenAI, Models: "text-embedding-3-small"},
			message: "模型 text-embedding-3-small 不支持自动文本探测",
		},
		{
			name: "non-text channel", modelName: "test-model",
			channel: &model.Channel{Type: constant.ChannelTypeSunoAPI, Models: "test-model"},
			message: "模型 test-model 不支持自动文本探测",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome := executeChannelStatusProbeModelWithEndpoint(
				withChannelGroupMonitorTestContext(context.Background(), "claude-probe"),
				test.channel, 0, test.modelName, selectChannelGroupMonitorEndpointType(test.channel),
			)
			assert.Equal(t, model.ChannelStatusProbeResultLocalFailure, outcome.Result)
			assert.Equal(t, "model_not_supported", outcome.ErrorCode)
			assert.Equal(t, test.message, outcome.ErrorMessage)
			assert.False(t, outcome.TestExecuted)
			assert.False(t, outcome.ProbeResult.requestDispatched)
		})
	}
}
