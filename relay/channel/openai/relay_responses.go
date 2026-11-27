package openai

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
)

func normalizeModelCapacityError(apiErr *types.NewAPIError) *types.NewAPIError {
	if types.IsModelCapacityError(apiErr) && apiErr.StatusCode >= http.StatusOK && apiErr.StatusCode < http.StatusMultipleChoices {
		apiErr.StatusCode = http.StatusServiceUnavailable
	}
	return apiErr
}

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	normalizedBody, normalizeErr := dto.NormalizeResponsesJSON(responseBody)
	if normalizeErr != nil {
		logger.LogWarn(c, "responses compatibility normalization skipped: "+normalizeErr.Error())
	} else {
		responseBody = normalizedBody
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && (oaiError.Type != "" || oaiError.Message != "" || oaiError.Code != nil) {
		return nil, normalizeModelCapacityError(types.WithOpenAIError(*oaiError, resp.StatusCode))
	}

	info.ObserveResponseModel(responsesResponse.Model)
	responseBody = rewriteSGLangResponsesCreatedAt(info, responseBody, "created_at", responsesResponse.CreatedAt)

	// compute usage
	usage := &dto.Usage{}
	service.ApplyResponsesUsage(usage, responsesResponse.Usage)
	// Count actual tool invocations from Output (not tool declarations).
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall:
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		case dto.BuildInCallFileSearchCall:
			info.CountBillableToolCall(dto.BuildInCallFileSearchCall, "")
		case dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(dto.BuildInCallFunctionCall, output.Name)
		}
	}
	info.ApplyVendorToolUsage(responseBody)

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	for i := range responsesResponse.Output {
		imageCounter.Observe(&responsesResponse.Output[i], &i)
	}
	imageCounter.Commit(info)

	if info.RetryEmptyResponse && usage.CompletionTokens == 0 && !c.Writer.Written() {
		info.CompleteEmptyResponse = func() *types.NewAPIError {
			service.IOCopyBytesGracefully(c, resp, responseBody)
			return nil
		}
		return usage, newEmptyResponseError()
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, common.LogText("invalid response or response body"))
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	accumulator := service.NewResponsesUsageAccumulator(info)
	var streamErr *types.NewAPIError
	var pending []responsesStreamFrame
	if info.RetryEmptyResponse {
		info.DisablePing = true
	}
	var emptyCompletion bool

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal stream response: %s", err.Error()))
			sr.Error(err)
			return
		}
		normalizedData, normalizeErr := dto.NormalizeResponsesJSON([]byte(data))
		if normalizeErr != nil {
			logger.LogWarn(c, "responses compatibility normalization skipped: "+normalizeErr.Error())
		} else if string(normalizedData) != data {
			var normalizedResponse dto.ResponsesStreamResponse
			if err := common.Unmarshal(normalizedData, &normalizedResponse); err != nil {
				logger.LogWarn(c, "responses compatibility output skipped: "+err.Error())
			} else {
				data = string(normalizedData)
				streamResponse = normalizedResponse
			}
		}
		if streamResponse.Type == "response.failed" && streamResponse.Response != nil {
			if oaiError := streamResponse.Response.GetOpenAIError(); oaiError != nil && (oaiError.Type != "" || oaiError.Message != "" || oaiError.Code != nil) {
				candidate := normalizeModelCapacityError(types.WithOpenAIError(*oaiError, resp.StatusCode))
				if types.IsModelCapacityError(candidate) {
					info.ObserveResponseModel(streamResponse.Response.Model)
					streamErr = candidate
					sr.Stop(candidate)
					return
				}
			}
		}
		if streamResponse.Response != nil {
			data = string(rewriteSGLangResponsesCreatedAt(info, []byte(data), "response.created_at", streamResponse.Response.CreatedAt))
		}
		payload := common.StringToByteSlice(data)
		accumulator.Observe(&streamResponse, payload)
		completed := (streamResponse.Type == "response.completed" || streamResponse.Type == "response.done") &&
			streamResponse.Response != nil && info.StreamStatus.ResponseOutcome() == string(relaycommon.ResponseOutcomeCompleted)
		if info.RetryEmptyResponse {
			pending = append(pending, responsesStreamFrame{kind: streamResponse.Type, data: data})
			if completed {
				emptyCompletion = streamResponse.Response.Usage == nil || streamResponse.Response.Usage.OutputTokens == 0
				sr.Done()
			}
			return
		}
		if err := helper.ResponseChunkData(c, streamResponse, data); err != nil {
			sr.Stop(err)
			return
		}
		if completed {
			sr.Done()
		}
	})
	if streamErr != nil {
		return nil, streamErr
	}
	finish := func() *types.NewAPIError {
		for _, frame := range pending {
			if err := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: frame.kind}, frame.data); err != nil {
				info.StreamStatus.RecordError(err.Error())
				break
			}
		}
		return nil
	}
	common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, info.StreamStatus)
	info.StreamStatus.RequireTerminal()
	usage := accumulator.Finish()
	if emptyCompletion && !c.Writer.Written() && !info.StreamStatus.HasErrors() && c.Request.Context().Err() == nil {
		info.CompleteEmptyResponse = finish
		return usage, newEmptyResponseError()
	}
	finish()
	return usage, nil
}

func rewriteSGLangResponsesCreatedAt(info *relaycommon.RelayInfo, payload []byte, path string, createdAt dto.IntValue) []byte {
	if info.GetChannelType() != constant.ChannelTypeSGLang {
		return payload
	}
	if !ojson.GetBytes(payload, path).Exists() {
		return payload
	}
	patched, err := ojson.SetBytes(payload, path, int(createdAt))
	if err != nil {
		return payload
	}
	return patched
}
