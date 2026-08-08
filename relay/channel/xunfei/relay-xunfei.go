package xunfei

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// https://console.xfyun.cn/services/cbm
// https://www.xfyun.cn/doc/spark/Web.html

func requestOpenAI2Xunfei(request dto.GeneralOpenAIRequest, xunfeiAppId string, domain string) *XunfeiChatRequest {
	messages := make([]XunfeiMessage, 0, len(request.Messages))
	shouldCovertSystemMessage := !strings.HasSuffix(request.Model, "3.5")
	for _, message := range request.Messages {
		if message.Role == "system" && shouldCovertSystemMessage {
			messages = append(messages, XunfeiMessage{
				Role:    "user",
				Content: message.StringContent(),
			})
			messages = append(messages, XunfeiMessage{
				Role:    "assistant",
				Content: "Okay",
			})
		} else {
			messages = append(messages, XunfeiMessage{
				Role:    message.Role,
				Content: message.StringContent(),
			})
		}
	}
	xunfeiRequest := XunfeiChatRequest{}
	xunfeiRequest.Header.AppId = xunfeiAppId
	xunfeiRequest.Parameter.Chat.Domain = domain
	xunfeiRequest.Parameter.Chat.Temperature = request.Temperature
	xunfeiRequest.Parameter.Chat.TopK = lo.FromPtrOr(request.N, 0)
	xunfeiRequest.Parameter.Chat.MaxTokens = request.GetMaxTokens()
	xunfeiRequest.Payload.Message.Text = messages
	return &xunfeiRequest
}

func responseXunfei2OpenAI(response *XunfeiChatResponse) *dto.OpenAITextResponse {
	if len(response.Payload.Choices.Text) == 0 {
		response.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role:    "assistant",
			Content: response.Payload.Choices.Text[0].Content,
		},
		FinishReason: constant.FinishReasonStop,
	}
	fullTextResponse := dto.OpenAITextResponse{
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Choices: []dto.OpenAITextResponseChoice{choice},
		Usage:   response.Payload.Usage.Text,
	}
	return &fullTextResponse
}

func streamResponseXunfei2OpenAI(xunfeiResponse *XunfeiChatResponse) *dto.ChatCompletionsStreamResponse {
	if len(xunfeiResponse.Payload.Choices.Text) == 0 {
		xunfeiResponse.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	choice.Delta.SetContentString(xunfeiResponse.Payload.Choices.Text[0].Content)
	if xunfeiResponse.Payload.Choices.Status == 2 {
		choice.FinishReason = &constant.FinishReasonStop
	}
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "SparkDesk",
		Choices: []dto.ChatCompletionsStreamResponseChoice{choice},
	}
	return &response
}

func buildXunfeiAuthUrl(hostUrl string, apiKey, apiSecret string) string {
	HmacWithShaToBase64 := func(algorithm, data, key string) string {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(data))
		encodeData := mac.Sum(nil)
		return base64.StdEncoding.EncodeToString(encodeData)
	}
	ul, err := url.Parse(hostUrl)
	if err != nil {
		fmt.Println(err)
	}
	date := time.Now().UTC().Format(time.RFC1123)
	signString := []string{"host: " + ul.Host, "date: " + date, "GET " + ul.Path + " HTTP/1.1"}
	sign := strings.Join(signString, "\n")
	sha := HmacWithShaToBase64("hmac-sha256", sign, apiSecret)
	authUrl := fmt.Sprintf("hmac username=\"%s\", algorithm=\"%s\", headers=\"%s\", signature=\"%s\"", apiKey,
		"hmac-sha256", "host date request-line", sha)
	authorization := base64.StdEncoding.EncodeToString([]byte(authUrl))
	v := url.Values{}
	v.Add("host", ul.Host)
	v.Add("date", date)
	v.Add("authorization", authorization)
	callUrl := hostUrl + "?" + v.Encode()
	return callUrl
}

func xunfeiStreamHandler(c *gin.Context, textRequest dto.GeneralOpenAIRequest, appId string, apiSecret string, apiKey string) (*dto.Usage, *types.NewAPIError) {
	domain, authUrl := getXunfeiAuthUrl(c, apiKey, apiSecret, textRequest.Model)
	dataChan, stopChan, errorChan, cancel, err := xunfeiMakeRequest(c.Request.Context(), textRequest, domain, authUrl, appId)
	if err != nil {
		if clientGoneErr := types.NewClientGoneErrorFromContext(c.Request.Context(), err); clientGoneErr != nil {
			return nil, clientGoneErr
		}
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed)
	}
	defer cancel()
	service.MarkChannelDailyCostRequestDispatched(c)
	helper.SetEventStreamHeaders(c)
	var usage dto.Usage
	var streamErr *types.NewAPIError
	c.Stream(func(w io.Writer) bool {
		select {
		case xunfeiResponse := <-dataChan:
			usage.PromptTokens += xunfeiResponse.Payload.Usage.Text.PromptTokens
			usage.CompletionTokens += xunfeiResponse.Payload.Usage.Text.CompletionTokens
			usage.TotalTokens += xunfeiResponse.Payload.Usage.Text.TotalTokens
			response := streamResponseXunfei2OpenAI(&xunfeiResponse)
			jsonResponse, err := common.Marshal(response)
			if err != nil {
				common.SysLog("error marshalling stream response: " + err.Error())
				streamErr = types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
				return false
			}
			c.Render(-1, common.CustomEvent{Data: "data: " + string(jsonResponse)})
			return true
		case requestErr := <-errorChan:
			if requestErr == nil {
				streamErr = types.NewError(errors.New("xunfei upstream response failed"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			} else if apiErr, ok := requestErr.(*types.NewAPIError); ok {
				streamErr = apiErr
			} else {
				streamErr = types.NewError(requestErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			return false
		case <-stopChan:
			c.Render(-1, common.CustomEvent{Data: "data: [DONE]"})
			return false
		case <-c.Request.Context().Done():
			return false
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if requestErr := c.Request.Context().Err(); requestErr != nil {
		return nil, types.NewClientGoneError(requestErr)
	}
	return &usage, nil
}

func xunfeiHandler(c *gin.Context, textRequest dto.GeneralOpenAIRequest, appId string, apiSecret string, apiKey string) (*dto.Usage, *types.NewAPIError) {
	domain, authUrl := getXunfeiAuthUrl(c, apiKey, apiSecret, textRequest.Model)
	dataChan, stopChan, errorChan, cancel, err := xunfeiMakeRequest(c.Request.Context(), textRequest, domain, authUrl, appId)
	if err != nil {
		if clientGoneErr := types.NewClientGoneErrorFromContext(c.Request.Context(), err); clientGoneErr != nil {
			return nil, clientGoneErr
		}
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed)
	}
	defer cancel()
	service.MarkChannelDailyCostRequestDispatched(c)
	var usage dto.Usage
	var content string
	var xunfeiResponse XunfeiChatResponse
	stop := false
	for !stop {
		select {
		case xunfeiResponse = <-dataChan:
			if len(xunfeiResponse.Payload.Choices.Text) == 0 {
				continue
			}
			content += xunfeiResponse.Payload.Choices.Text[0].Content
			usage.PromptTokens += xunfeiResponse.Payload.Usage.Text.PromptTokens
			usage.CompletionTokens += xunfeiResponse.Payload.Usage.Text.CompletionTokens
			usage.TotalTokens += xunfeiResponse.Payload.Usage.Text.TotalTokens
		case requestErr := <-errorChan:
			if requestErr == nil {
				return nil, types.NewError(errors.New("xunfei upstream response failed"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
			if apiErr, ok := requestErr.(*types.NewAPIError); ok {
				return nil, apiErr
			}
			return nil, types.NewError(requestErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		case stop = <-stopChan:
		case <-c.Request.Context().Done():
			return nil, types.NewClientGoneError(c.Request.Context().Err())
		}
	}
	if len(xunfeiResponse.Payload.Choices.Text) == 0 {
		xunfeiResponse.Payload.Choices.Text = []XunfeiChatResponseTextItem{
			{
				Content: "",
			},
		}
	}
	xunfeiResponse.Payload.Choices.Text[0].Content = content

	response := responseXunfei2OpenAI(&xunfeiResponse)
	jsonResponse, err := common.Marshal(response)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	_, _ = c.Writer.Write(jsonResponse)
	return &usage, nil
}

func xunfeiMakeRequest(ctx context.Context, textRequest dto.GeneralOpenAIRequest, domain, authUrl, appId string) (chan XunfeiChatResponse, chan bool, chan error, context.CancelFunc, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithCancel(ctx)
	d := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}
	conn, resp, err := d.DialContext(requestCtx, authUrl, nil)
	if err != nil {
		cancel()
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}
		if statusCode >= http.StatusContinue && statusCode <= 599 {
			return nil, nil, nil, nil, types.NewErrorWithStatusCode(
				fmt.Errorf("websocket handshake failed: %w", err),
				types.ErrorCodeBadResponseStatusCode,
				statusCode,
			)
		}
		return nil, nil, nil, nil, err
	}
	if conn == nil || resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
		cancel()
		if conn != nil {
			_ = conn.Close()
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		status := http.StatusBadGateway
		if resp != nil {
			if resp.StatusCode >= http.StatusContinue && resp.StatusCode <= 599 {
				status = resp.StatusCode
			}
		}
		return nil, nil, nil, nil, types.NewErrorWithStatusCode(
			fmt.Errorf("websocket handshake failed with status %d", status),
			types.ErrorCodeBadResponseStatusCode,
			status,
		)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}

	data := requestOpenAI2Xunfei(textRequest, appId, domain)
	err = conn.WriteJSON(data)
	if err != nil {
		cancel()
		_ = conn.Close()
		return nil, nil, nil, nil, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}

	dataChan := make(chan XunfeiChatResponse)
	stopChan := make(chan bool)
	errorChan := make(chan error, 1)
	connectionDone := make(chan struct{})
	go func() {
		defer close(connectionDone)
		defer func() {
			conn.Close()
		}()
		go func() {
			select {
			case <-requestCtx.Done():
				_ = conn.Close()
			case <-connectionDone:
			}
		}()
		normalStop := false
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if requestCtx.Err() != nil {
					return
				}
				common.SysLog("error reading stream response: " + err.Error())
				select {
				case errorChan <- types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry()):
				case <-requestCtx.Done():
				}
				return
			}
			var response XunfeiChatResponse
			err = common.Unmarshal(msg, &response)
			if err != nil {
				common.SysLog("error unmarshalling stream response: " + err.Error())
				select {
				case errorChan <- types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry()):
				case <-requestCtx.Done():
				}
				return
			}
			select {
			case dataChan <- response:
			case <-requestCtx.Done():
				return
			}
			if response.Payload.Choices.Status == 2 {
				normalStop = true
				break
			}
		}
		if normalStop {
			select {
			case stopChan <- true:
			case <-requestCtx.Done():
			}
		}
	}()

	return dataChan, stopChan, errorChan, cancel, nil
}

func apiVersion2domain(apiVersion string) string {
	switch apiVersion {
	case "v1.1":
		return "lite"
	case "v2.1":
		return "generalv2"
	case "v3.1":
		return "generalv3"
	case "v3.5":
		return "generalv3.5"
	case "v4.0":
		return "4.0Ultra"
	}
	return "general" + apiVersion
}

func getXunfeiAuthUrl(c *gin.Context, apiKey string, apiSecret string, modelName string) (string, string) {
	apiVersion := getAPIVersion(c, modelName)
	domain := apiVersion2domain(apiVersion)
	authUrl := buildXunfeiAuthUrl(fmt.Sprintf("wss://spark-api.xf-yun.com/%s/chat", apiVersion), apiKey, apiSecret)
	return domain, authUrl
}

func getAPIVersion(c *gin.Context, modelName string) string {
	query := c.Request.URL.Query()
	apiVersion := query.Get("api-version")
	if apiVersion != "" {
		return apiVersion
	}
	parts := strings.Split(modelName, "-")
	if len(parts) == 2 {
		apiVersion = parts[1]
		return apiVersion

	}
	apiVersion = c.GetString("api_version")
	if apiVersion != "" {
		return apiVersion
	}
	apiVersion = "v1.1"
	common.SysLog("api_version not found, using default: " + apiVersion)
	return apiVersion
}
