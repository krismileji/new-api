package controller

import (
	"bytes"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// The shared local-response policy writes SSE through Gin. Capture those local
// events for WebSocket delivery without flushing the internal HTTP runner.
type responsesWSLocalWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *responsesWSLocalWriter) Write(data []byte) (int, error) { return w.body.Write(data) }
func (w *responsesWSLocalWriter) WriteString(data string) (int, error) {
	return w.body.WriteString(data)
}
func (w *responsesWSLocalWriter) Flush() {}

func admitResponsesWSChannel(c *gin.Context, info *relaycommon.RelayInfo, retry *service.RetryParam, channel *model.Channel, allowAlternative bool) (*model.Channel, *service.ChannelConcurrencyLease, [][]byte, *types.NewAPIError) {
	request, ok := info.Request.(*dto.OpenAIResponsesRequest)
	if !ok {
		return nil, nil, nil, types.NewError(errors.New("Responses 请求类型无效"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	localRequest := *request
	localRequest.Stream = common.GetPointer(true)
	writer := &responsesWSLocalWriter{ResponseWriter: c.Writer}
	info.Request, c.Writer = &localRequest, writer
	defer func() { info.Request, c.Writer = request, writer.ResponseWriter }()
	if retry == nil {
		retry = &service.RetryParam{Ctx: c, TokenGroup: info.TokenGroup, ModelName: info.OriginModelName, RequestPath: c.Request.URL.Path, Retry: common.GetPointer(0)}
	}
	retry.SelectionOptions = service.ChannelSelectionOptionsForRequest(c, info.GetEstimatePromptTokens())
	allowAlternative = allowAlternative && !service.GetChannelConstraints(c).SuppressesRetry()
	selected, lease, apiErr := acquireRelayChannelConcurrency(c, info, retry, newRelayRetryRouting(), channel, allowAlternative)
	if apiErr != nil || !c.GetBool(service.ChannelLocalResponseContextKey) {
		return selected, lease, nil, apiErr
	}
	info.PerformanceBusinessRejection = true
	var frames [][]byte
	for line := range strings.SplitSeq(writer.body.String(), "\n") {
		if data, found := strings.CutPrefix(line, "data: "); found {
			frames = append(frames, []byte(strings.TrimSpace(data)))
		}
	}
	if len(frames) == 0 {
		return selected, lease, nil, types.NewError(errors.New("本地响应生成失败"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
	}
	return selected, lease, frames, nil
}
