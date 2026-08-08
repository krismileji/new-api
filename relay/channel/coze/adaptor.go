package coze

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *common.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

// ConvertAudioRequest implements channel.Adaptor.
func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *common.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

// ConvertClaudeRequest implements channel.Adaptor.
func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *common.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertEmbeddingRequest implements channel.Adaptor.
func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *common.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertImageRequest implements channel.Adaptor.
func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *common.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertOpenAIRequest implements channel.Adaptor.
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *common.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return convertCozeChatRequest(c, *request), nil
}

// ConvertOpenAIResponsesRequest implements channel.Adaptor.
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *common.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// ConvertRerankRequest implements channel.Adaptor.
func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("not implemented")
}

// DoRequest implements channel.Adaptor.
func (a *Adaptor) DoRequest(c *gin.Context, info *common.RelayInfo, requestBody io.Reader) (any, error) {
	if info.IsStream {
		return channel.DoApiRequest(a, c, info, requestBody)
	}
	// 首先发送创建消息请求，成功后再发送获取消息请求
	// 发送创建消息请求
	resp, err := channel.DoApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("coze create chat returned an empty response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		statusCode := resp.StatusCode
		service.CloseResponseBodyGracefully(resp)
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("bad response status code %d", statusCode),
			types.ErrorCodeBadResponseStatusCode,
			statusCode,
		)
	}
	// 解析 resp
	var cozeResponse CozeChatResponse
	respBody, err := io.ReadAll(resp.Body)
	service.CloseResponseBodyGracefully(resp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	err = rootcommon.Unmarshal(respBody, &cozeResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	if cozeResponse.Code != 0 {
		return nil, errors.New(cozeResponse.Msg)
	}
	if cozeResponse.Data.ConversationId == "" || cozeResponse.Data.Id == "" {
		return nil, types.NewError(errors.New("coze create chat response is missing conversation or chat id"), types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	c.Set("coze_conversation_id", cozeResponse.Data.ConversationId)
	c.Set("coze_chat_id", cozeResponse.Data.Id)
	// 轮询检查消息是否完成
	pollCount := 0
	for {
		pollCount++
		if pollCount > 300 {
			return nil, types.NewError(errors.New("coze chat polling timeout"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
		err, isComplete := checkIfChatComplete(a, c, info)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		} else {
			if isComplete {
				break
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-c.Request.Context().Done():
			timer.Stop()
			if clientGoneErr := types.NewClientGoneErrorFromContext(c.Request.Context(), c.Request.Context().Err()); clientGoneErr != nil {
				return nil, clientGoneErr
			}
			return nil, types.NewError(c.Request.Context().Err(), types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
		}
	}
	// 发送获取消息请求
	return getChatDetail(a, c, info)
}

// DoResponse implements channel.Adaptor.
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *common.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.IsStream {
		usage, err = cozeChatStreamHandler(c, info, resp)
	} else {
		usage, err = cozeChatHandler(c, info, resp)
	}
	return
}

// GetChannelName implements channel.Adaptor.
func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

// GetModelList implements channel.Adaptor.
func (a *Adaptor) GetModelList() []string {
	return ModelList
}

// GetRequestURL implements channel.Adaptor.
func (a *Adaptor) GetRequestURL(info *common.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v3/chat", info.ChannelBaseUrl), nil
}

// Init implements channel.Adaptor.
func (a *Adaptor) Init(info *common.RelayInfo) {

}

// SetupRequestHeader implements channel.Adaptor.
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *common.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}
