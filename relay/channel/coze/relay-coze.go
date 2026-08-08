package coze

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func convertCozeChatRequest(c *gin.Context, request dto.GeneralOpenAIRequest) *CozeChatRequest {
	var messages []CozeEnterMessage
	// 将 request的messages的role为user的content转换为CozeMessage
	for _, message := range request.Messages {
		if message.Role == "user" {
			messages = append(messages, CozeEnterMessage{
				Role:    "user",
				Content: message.Content,
				// TODO: support more content type
				ContentType: "text",
			})
		}
	}
	user := request.User
	if len(user) == 0 {
		user = json.RawMessage(helper.GetResponseID(c))
	}
	cozeRequest := &CozeChatRequest{
		BotId:              c.GetString("bot_id"),
		UserId:             user,
		AdditionalMessages: messages,
		Stream:             lo.FromPtrOr(request.Stream, false),
	}
	return cozeRequest
}

func cozeChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(errors.New("coze returned an empty response"), types.ErrorCodeBadResponse)
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		statusCode := resp.StatusCode
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("bad response status code %d", statusCode),
			types.ErrorCodeBadResponseStatusCode,
			statusCode,
		)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	// convert coze response to openai response
	var response dto.TextResponse
	var cozeResponse CozeChatDetailResponse
	response.Model = info.UpstreamModelName
	err = common.Unmarshal(responseBody, &cozeResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	if cozeResponse.Code != 0 {
		return nil, types.NewError(errors.New(cozeResponse.Msg), types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	// 从上下文获取 usage
	var usage dto.Usage
	usage.PromptTokens = c.GetInt("coze_input_count")
	usage.CompletionTokens = c.GetInt("coze_output_count")
	usage.TotalTokens = c.GetInt("coze_token_count")
	response.Usage = usage
	response.Id = helper.GetResponseID(c)

	var responseContent json.RawMessage
	for _, data := range cozeResponse.Data {
		if data.Type == "answer" {
			responseContent = data.Content
			response.Created = data.CreatedAt
		}
	}
	// 添加 response.Choices
	response.Choices = []dto.OpenAITextResponseChoice{
		{
			Index:        0,
			Message:      dto.Message{Role: "assistant", Content: responseContent},
			FinishReason: "stop",
		},
	}
	jsonResponse, err := common.Marshal(response)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)

	return &usage, nil
}

func cozeChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(errors.New("coze returned an empty response"), types.ErrorCodeBadResponse)
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("bad response status code %d", resp.StatusCode),
			types.ErrorCodeBadResponseStatusCode,
			resp.StatusCode,
		)
	}
	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	helper.SetEventStreamHeaders(c)
	id := helper.GetResponseID(c)
	var responseText string

	var currentEvent string
	var currentData string
	var usage = &dto.Usage{}
	completed := false

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if currentEvent != "" && currentData != "" {
				eventCompleted, eventErr := handleCozeEvent(c, currentEvent, currentData, &responseText, usage, id, info)
				if eventErr != nil {
					return nil, types.NewError(eventErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
				}
				completed = completed || eventCompleted
				currentEvent = ""
				currentData = ""
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(line[6:])
			continue
		}

		if strings.HasPrefix(line, "data:") {
			currentData = strings.TrimSpace(line[5:])
			continue
		}
	}

	// Last event
	if currentEvent != "" && currentData != "" {
		eventCompleted, eventErr := handleCozeEvent(c, currentEvent, currentData, &responseText, usage, id, info)
		if eventErr != nil {
			return nil, types.NewError(eventErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
		completed = completed || eventCompleted
	}

	if err := scanner.Err(); err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	if !completed {
		return nil, types.NewError(errors.New("coze stream ended before completion"), types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	helper.Done(c)

	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, responseText, info.UpstreamModelName, c.GetInt("coze_input_count"))
	}

	return usage, nil
}

func handleCozeEvent(c *gin.Context, event string, data string, responseText *string, usage *dto.Usage, id string, info *relaycommon.RelayInfo) (bool, error) {
	switch event {
	case "conversation.chat.completed":
		// 将 data 解析为 CozeChatResponseData
		var chatData CozeChatResponseData
		err := common.Unmarshal([]byte(data), &chatData)
		if err != nil {
			return false, fmt.Errorf("unmarshal coze completion event: %w", err)
		}

		usage.PromptTokens = chatData.Usage.InputCount
		usage.CompletionTokens = chatData.Usage.OutputCount
		usage.TotalTokens = chatData.Usage.TokenCount

		finishReason := "stop"
		stopResponse := helper.GenerateStopResponse(id, common.GetTimestamp(), info.UpstreamModelName, finishReason)
		if err := helper.ObjectData(c, stopResponse); err != nil {
			return false, err
		}
		return true, nil

	case "conversation.message.delta":
		// 将 data 解析为 CozeChatV3MessageDetail
		var messageData CozeChatV3MessageDetail
		err := common.Unmarshal([]byte(data), &messageData)
		if err != nil {
			return false, fmt.Errorf("unmarshal coze message event: %w", err)
		}

		var content string
		err = common.Unmarshal(messageData.Content, &content)
		if err != nil {
			return false, fmt.Errorf("unmarshal coze message content: %w", err)
		}

		*responseText += content

		openaiResponse := dto.ChatCompletionsStreamResponse{
			Id:      id,
			Object:  "chat.completion.chunk",
			Created: common.GetTimestamp(),
			Model:   info.UpstreamModelName,
		}

		choice := dto.ChatCompletionsStreamResponseChoice{
			Index: 0,
		}
		choice.Delta.SetContentString(content)
		openaiResponse.Choices = append(openaiResponse.Choices, choice)

		if err := helper.ObjectData(c, openaiResponse); err != nil {
			return false, err
		}
		return false, nil

	case "error":
		var errorData CozeError
		err := common.Unmarshal([]byte(data), &errorData)
		if err != nil {
			return false, fmt.Errorf("unmarshal coze error event: %w", err)
		}
		return false, fmt.Errorf("coze stream error %d: %s", errorData.Code, errorData.Message)
	}
	return false, nil
}

func checkIfChatComplete(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (error, bool) {
	requestURL := fmt.Sprintf("%s/v3/chat/retrieve", info.ChannelBaseUrl)

	requestURL = requestURL + "?conversation_id=" + c.GetString("coze_conversation_id") + "&chat_id=" + c.GetString("coze_chat_id")
	if urlErr := channel.ValidateUpstreamURL(requestURL, false); urlErr != nil {
		return urlErr, false
	}
	// 将 conversationId和chatId作为参数发送get请求
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return err, false
	}
	req = req.WithContext(c.Request.Context())
	err = a.SetupRequestHeader(c, &req.Header, info)
	if err != nil {
		return err, false
	}

	resp, err := doRequest(req, info) // 调用 doRequest
	if err != nil {
		return err, false
	}
	if resp == nil { // 确保在 doRequest 失败时 resp 不为 nil 导致 panic
		return fmt.Errorf("resp is nil"), false
	}
	if resp.Body == nil {
		return fmt.Errorf("response body is nil"), false
	}
	defer resp.Body.Close() // 确保响应体被关闭
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("bad response status code %d", resp.StatusCode),
			types.ErrorCodeBadResponseStatusCode,
			resp.StatusCode,
		), false
	}

	// 解析 resp 到 CozeChatResponse
	var cozeResponse CozeChatResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body failed: %w", err), false
	}
	err = common.Unmarshal(responseBody, &cozeResponse)
	if err != nil {
		return fmt.Errorf("unmarshal response body failed: %w", err), false
	}
	if cozeResponse.Code != 0 {
		return fmt.Errorf("coze chat retrieve failed: %s", cozeResponse.Msg), false
	}
	if cozeResponse.Data.Status == "completed" {
		// 在上下文设置 usage
		c.Set("coze_token_count", cozeResponse.Data.Usage.TokenCount)
		c.Set("coze_output_count", cozeResponse.Data.Usage.OutputCount)
		c.Set("coze_input_count", cozeResponse.Data.Usage.InputCount)
		return nil, true
	} else if cozeResponse.Data.Status == "failed" || cozeResponse.Data.Status == "canceled" || cozeResponse.Data.Status == "requires_action" {
		return fmt.Errorf("chat status: %s", cozeResponse.Data.Status), false
	} else {
		return nil, false
	}
}

func getChatDetail(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo) (*http.Response, error) {
	requestURL := fmt.Sprintf("%s/v3/chat/message/list", info.ChannelBaseUrl)

	requestURL = requestURL + "?conversation_id=" + c.GetString("coze_conversation_id") + "&chat_id=" + c.GetString("coze_chat_id")
	if urlErr := channel.ValidateUpstreamURL(requestURL, false); urlErr != nil {
		return nil, urlErr
	}
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}
	req = req.WithContext(c.Request.Context())
	err = a.SetupRequestHeader(c, &req.Header, info)
	if err != nil {
		return nil, fmt.Errorf("setup request header failed: %w", err)
	}
	resp, err := doRequest(req, info)
	if err != nil {
		return nil, types.NewError(fmt.Errorf("do request failed: %w", err), types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if resp == nil {
		return nil, types.NewError(errors.New("coze chat detail returned an empty response"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		statusCode := resp.StatusCode
		service.CloseResponseBodyGracefully(resp)
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("bad response status code %d", statusCode),
			types.ErrorCodeBadResponseStatusCode,
			statusCode,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return resp, nil
}

func doRequest(req *http.Request, info *relaycommon.RelayInfo) (*http.Response, error) {
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil { // 增加对 client.Do(req) 返回错误的检查
		return nil, fmt.Errorf("client.Do failed: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return nil, errors.New("client.Do returned an empty response")
	}
	// _ = resp.Body.Close()
	return resp, nil
}
