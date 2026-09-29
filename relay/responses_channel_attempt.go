package relay

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// ResponsesWSChannelAdmission connects each generation to the controller's
// existing routing, local-response and physical/shared capacity policies.
type ResponsesWSChannelAdmission func(*gin.Context, *relaycommon.RelayInfo, *service.RetryParam, *model.Channel, bool) (*model.Channel, *service.ChannelConcurrencyLease, [][]byte, *types.NewAPIError)

const responsesWSAdmissionContextKey = "responses_ws_channel_admission"

func SetResponsesWSChannelAdmission(c *gin.Context, admit ResponsesWSChannelAdmission) {
	c.Set(responsesWSAdmissionContextKey, admit)
}

type responsesWSChannelAttempt struct {
	ctx       *gin.Context
	request   *http.Request
	lease     *service.ChannelConcurrencyLease
	channelID int
	started   bool
}

func (s *responsesWSSession) admitChannel(c *gin.Context, info *relaycommon.RelayInfo, retry *service.RetryParam, channel *model.Channel, allowAlternative bool) (*model.Channel, *responsesWSChannelAttempt, [][]byte, *types.NewAPIError) {
	value, _ := c.Get(responsesWSAdmissionContextKey)
	admit, _ := value.(ResponsesWSChannelAdmission)
	if admit == nil {
		return channel, nil, nil, nil
	}
	selected, lease, local, apiErr := admit(c, info, retry, channel, allowAlternative)
	if apiErr != nil || len(local) > 0 {
		lease.Release()
		return selected, nil, local, apiErr
	}
	if selected == nil || lease == nil {
		lease.Release()
		return nil, nil, nil, types.NewError(errors.New("渠道准入未返回有效租约"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	attempt := &responsesWSChannelAttempt{ctx: c, request: c.Request, lease: lease, channelID: selected.Id}
	if lease.Context != nil {
		c.Request = c.Request.WithContext(lease.Context)
	}
	info.ClientWs = s.client
	return selected, attempt, nil, nil
}

func (attempt *responsesWSChannelAttempt) begin(info *relaycommon.RelayInfo) {
	if attempt == nil {
		return
	}
	service.CaptureChannelDailyCostSnapshot(attempt.ctx, attempt.channelID)
	service.BeginChannelDailyCostAttempt(attempt.ctx, attempt.channelID)
	service.PrepareChannelBalanceAttempt(attempt.ctx, info)
	service.BeginChannelMonitorPerformanceAttempt(attempt.ctx, time.Now())
	service.SetTokenProtectionChannel(attempt.ctx.Request.Context(), attempt.channelID)
	attempt.started = true
}

func (attempt *responsesWSChannelAttempt) finish() {
	if attempt == nil {
		return
	}
	if attempt.started {
		service.FinalizeChannelDailyCostAttempt(attempt.ctx, attempt.channelID, false)
	}
	attempt.lease.Release()
	attempt.ctx.Request = attempt.request
}

func (s *responsesWSSession) completeLocalResponse(state *responsesWSCallState, frames [][]byte, streamID string) *types.NewAPIError {
	for _, frame := range frames {
		var event map[string]common.RawMessage
		if err := common.Unmarshal(frame, &event); err != nil {
			return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
		if streamID != "" {
			encoded, err := common.Marshal(streamID)
			if err != nil {
				return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			event["stream_id"] = encoded
		}
		body, err := common.Marshal(event)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
		var kind string
		_ = common.Unmarshal(event["type"], &kind)
		if kind == "response.completed" || kind == "response.incomplete" {
			state.terminal = &responsesWSMessage{kind: websocket.TextMessage, body: body}
			return nil
		}
		if err := s.writeClient(websocket.TextMessage, body); err != nil {
			state.closeAfter = true
			return types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
	}
	return types.NewError(errors.New("本地响应缺少结束事件"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
}
