package dify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

const difyUploadedFileCacheKey = "dify_uploaded_file_cache"

func uploadDifyFile(c *gin.Context, info *relaycommon.RelayInfo, user string, media dto.MediaContent) (*DifyFile, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, errors.New("dify channel metadata is missing")
	}
	uploadUrl := fmt.Sprintf("%s/v1/files/upload", info.ChannelBaseUrl)
	if urlErr := channel.ValidateUpstreamURL(uploadUrl, false); urlErr != nil {
		common.SysLog("invalid dify upload URL: " + urlErr.Error())
		return nil, urlErr
	}
	switch media.Type {
	case dto.ContentTypeImageURL:
		// Decode base64 data
		imageMedia := media.GetImageMedia()
		if imageMedia == nil {
			return nil, errors.New("dify image content is invalid")
		}
		base64Data := imageMedia.Url
		// Remove base64 prefix if exists (e.g., "data:image/jpeg;base64,")
		if idx := strings.Index(base64Data, ","); idx != -1 {
			base64Data = base64Data[idx+1:]
		}

		// Decode base64 string
		decodedData, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			common.SysLog("failed to decode base64: " + err.Error())
			return nil, fmt.Errorf("dify image base64 decode failed: %w", err)
		}
		mimeType := imageMedia.MimeType
		if mimeType == "" {
			mimeType = "image/jpeg" // default mime type
		}
		cacheKey := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%x", info.ChannelBaseUrl, info.ApiKey, user, mimeType, sha256.Sum256(decodedData))
		if c != nil {
			if cached, ok := c.Get(difyUploadedFileCacheKey); ok {
				if files, ok := cached.(map[string]DifyFile); ok {
					if file, found := files[cacheKey]; found {
						fileCopy := file
						return &fileCopy, nil
					}
				}
			}
		}

		// Create temporary file
		tempFile, err := os.CreateTemp("", "dify-upload-*")
		if err != nil {
			common.SysLog("failed to create temp file: " + err.Error())
			return nil, fmt.Errorf("create dify upload temp file failed: %w", err)
		}
		defer tempFile.Close()
		defer os.Remove(tempFile.Name())

		// Write decoded data to temp file
		if _, err := tempFile.Write(decodedData); err != nil {
			common.SysLog("failed to write to temp file: " + err.Error())
			return nil, fmt.Errorf("write dify upload temp file failed: %w", err)
		}

		// Create multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add user field
		if err := writer.WriteField("user", user); err != nil {
			common.SysLog("failed to add user field: " + err.Error())
			return nil, fmt.Errorf("build dify upload form failed: %w", err)
		}

		// Create form file with proper mime type
		// Create form file
		part, err := writer.CreateFormFile("file", fmt.Sprintf("image.%s", strings.TrimPrefix(mimeType, "image/")))
		if err != nil {
			common.SysLog("failed to create form file: " + err.Error())
			return nil, fmt.Errorf("create dify upload form file failed: %w", err)
		}

		// Copy file content to form
		if _, err = io.Copy(part, bytes.NewReader(decodedData)); err != nil {
			common.SysLog("failed to copy file content: " + err.Error())
			return nil, fmt.Errorf("copy dify upload file failed: %w", err)
		}
		writer.Close()

		// Create HTTP request
		requestContext := context.Background()
		if c != nil && c.Request != nil {
			requestContext = c.Request.Context()
		}
		req, err := http.NewRequestWithContext(requestContext, http.MethodPost, uploadUrl, body)
		if err != nil {
			common.SysLog("failed to create request: " + err.Error())
			return nil, fmt.Errorf("create dify upload request failed: %w", err)
		}

		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", info.ApiKey))

		// Use the selected channel's proxy and transport settings for the
		// auxiliary upload just like the main relay request.
		client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
		if err != nil {
			return nil, fmt.Errorf("create dify upload client failed: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			common.SysLog("failed to send request: " + err.Error())
			if clientGoneErr := types.NewClientGoneErrorFromContext(requestContext, err); clientGoneErr != nil {
				return nil, clientGoneErr
			}
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("dify file upload failed: %w", err),
				types.ErrorCodeDoRequestFailed,
				http.StatusBadGateway,
			)
		}
		if resp == nil || resp.Body == nil {
			common.SysLog("dify upload returned an empty response")
			return nil, types.NewErrorWithStatusCode(
				errors.New("dify upload returned an empty response"),
				types.ErrorCodeBadResponse,
				http.StatusBadGateway,
			)
		}
		defer resp.Body.Close()
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			common.SysLog(fmt.Sprintf("dify upload failed with status %d", resp.StatusCode))
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("dify upload failed with status %d", resp.StatusCode),
				types.ErrorCodeBadResponseStatusCode,
				resp.StatusCode,
			)
		}

		// Parse response
		var result struct {
			Id string `json:"id"`
		}
		if err := common.DecodeJson(resp.Body, &result); err != nil {
			common.SysLog("failed to decode response: " + err.Error())
			return nil, types.NewError(
				fmt.Errorf("decode dify upload response failed: %w", err),
				types.ErrorCodeBadResponseBody,
				types.ErrOptionWithSkipRetry(),
			)
		}
		if result.Id == "" {
			common.SysLog("dify upload response missing file id")
			return nil, types.NewError(
				errors.New("dify upload response missing file id"),
				types.ErrorCodeBadResponseBody,
				types.ErrOptionWithSkipRetry(),
			)
		}

		file := DifyFile{
			UploadFileId: result.Id,
			Type:         "image",
			TransferMode: "local_file",
		}
		if c != nil {
			cached, _ := c.Get(difyUploadedFileCacheKey)
			files, ok := cached.(map[string]DifyFile)
			if !ok {
				files = make(map[string]DifyFile)
			}
			files[cacheKey] = file
			c.Set(difyUploadedFileCacheKey, files)
		}
		return &file, nil
	}
	return nil, nil
}

func requestOpenAI2Dify(c *gin.Context, info *relaycommon.RelayInfo, request dto.GeneralOpenAIRequest) (*DifyChatRequest, error) {
	difyReq := DifyChatRequest{
		Inputs:           make(map[string]interface{}),
		AutoGenerateName: false,
	}

	user := request.User
	if len(user) == 0 {
		user = json.RawMessage(helper.GetResponseID(c))
	}
	var stringUser string
	err := common.Unmarshal(user, &stringUser)
	if err != nil {
		common.SysLog("failed to unmarshal user: " + err.Error())
		stringUser = helper.GetResponseID(c)
	}
	difyReq.User = stringUser

	files := make([]DifyFile, 0)
	var content strings.Builder
	for _, message := range request.Messages {
		if message.Role == "system" {
			content.WriteString("SYSTEM: \n" + message.StringContent() + "\n")
		} else if message.Role == "assistant" {
			content.WriteString("ASSISTANT: \n" + message.StringContent() + "\n")
		} else {
			parseContent := message.ParseContent()
			for _, mediaContent := range parseContent {
				switch mediaContent.Type {
				case dto.ContentTypeText:
					content.WriteString("USER: \n" + mediaContent.Text + "\n")
				case dto.ContentTypeImageURL:
					media := mediaContent.GetImageMedia()
					if media == nil {
						return nil, errors.New("dify image content is invalid")
					}
					var file *DifyFile
					if media.IsRemoteImage() {
						// 修复 #2083: 远程图片分支此前未初始化 file，
						// 导致 file.Type = ... 触发 nil pointer dereference
						// 而 panic（500: "invalid memory address or nil pointer dereference"）。
						file = &DifyFile{
							Type:         media.MimeType,
							TransferMode: "remote_url",
							URL:          media.Url,
						}
					} else {
						var uploadErr error
						file, uploadErr = uploadDifyFile(c, info, difyReq.User, mediaContent)
						if uploadErr != nil {
							return nil, uploadErr
						}
					}
					if file != nil {
						files = append(files, *file)
					}
				}
			}
		}
	}
	difyReq.Query = content.String()
	difyReq.Files = files
	mode := "blocking"
	if lo.FromPtrOr(request.Stream, false) {
		mode = "streaming"
	}
	difyReq.ResponseMode = mode
	return &difyReq, nil
}

func streamResponseDify2OpenAI(difyResponse DifyChunkChatCompletionResponse) *dto.ChatCompletionsStreamResponse {
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "dify",
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	if strings.HasPrefix(difyResponse.Event, "workflow_") {
		if constant.DifyDebug {
			text := "Workflow: " + difyResponse.Data.WorkflowId
			if difyResponse.Event == "workflow_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if strings.HasPrefix(difyResponse.Event, "node_") {
		if constant.DifyDebug {
			text := "Node: " + difyResponse.Data.NodeType
			if difyResponse.Event == "node_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if difyResponse.Event == "message" || difyResponse.Event == "agent_message" {
		if difyResponse.Answer == "<details style=\"color:gray;background-color: #f8f8f8;padding: 8px;border-radius: 4px;\" open> <summary> Thinking... </summary>\n" {
			difyResponse.Answer = "<think>"
		} else if difyResponse.Answer == "</details>" {
			difyResponse.Answer = "</think>"
		}

		choice.Delta.SetContentString(difyResponse.Answer)
	}
	response.Choices = append(response.Choices, choice)
	return &response
}

func difyStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(errors.New("dify returned an empty response"), types.ErrorCodeBadResponse)
	}
	var responseText string
	usage := &dto.Usage{}
	var nodeToken int
	helper.SetEventStreamHeaders(c)
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var difyResponse DifyChunkChatCompletionResponse
		if err := common.Unmarshal([]byte(data), &difyResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			sr.Error(err)
			return
		}
		if difyResponse.Event == "message_end" {
			usage = &difyResponse.MetaData.Usage
			sr.Done()
			return
		} else if difyResponse.Event == "error" {
			sr.Stop(fmt.Errorf("dify error event"))
			return
		}
		openaiResponse := *streamResponseDify2OpenAI(difyResponse)
		if len(openaiResponse.Choices) != 0 {
			responseText += openaiResponse.Choices[0].Delta.GetContentString()
			if openaiResponse.Choices[0].Delta.ReasoningContent != nil {
				nodeToken += 1
			}
		}
		if err := helper.ObjectData(c, openaiResponse); err != nil {
			common.SysLog(err.Error())
			sr.Error(err)
		}
	})
	if info.StreamStatus == nil {
		return nil, types.NewError(errors.New("dify stream status is unavailable"), types.ErrorCodeBadResponse)
	}
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
		return nil, types.NewClientGoneError(c.Request.Context().Err())
	}
	if info.StreamStatus.EndReason != relaycommon.StreamEndReasonDone || info.StreamStatus.HasErrors() {
		streamErr := info.StreamStatus.EndError
		if streamErr == nil {
			streamErr = errors.New("dify stream ended before completion")
		}
		return nil, types.NewError(streamErr, types.ErrorCodeBadResponseBody)
	}
	helper.Done(c)
	if usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	usage.CompletionTokens += nodeToken
	return usage, nil
}

func difyHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(errors.New("dify returned an empty response"), types.ErrorCodeBadResponse)
	}
	defer service.CloseResponseBodyGracefully(resp)
	var difyResponse DifyChatCompletionResponse
	responseBody, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	err = common.Unmarshal(responseBody, &difyResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	fullTextResponse := dto.OpenAITextResponse{
		Id:      difyResponse.ConversationId,
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Usage:   difyResponse.MetaData.Usage,
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role:    "assistant",
			Content: difyResponse.Answer,
		},
		FinishReason: "stop",
	}
	fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	c.Writer.Write(jsonResponse)
	return &difyResponse.MetaData.Usage, nil
}
