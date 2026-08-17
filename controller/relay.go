package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		relayInfo   *relaycommon.RelayInfo
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			writeRelayErrorResponse(c, ws, relayFormat, newAPIError)
		}
	}()

	request, requestCached := channelprobe.ValidatedRequest(c, relayFormat)
	var err error
	if !requestCached {
		request, err = helper.GetAndValidateRequest(c, relayFormat)
	}
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err = relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	if relayInfo.RelayMode == relayconstant.RelayModeImagesGenerations {
		if _, enabled := ratio_setting.GetImageRatio(relayInfo.OriginModelName); !enabled {
			newAPIError = types.NewErrorWithStatusCode(
				errors.New("image generation is currently not supported"),
				types.ErrorCodeInvalidRequest,
				http.StatusOK,
				types.ErrOptionWithSkipRetry(),
			)
			return
		}
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewError(err, types.ErrorCodeSensitiveWordsDetected)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}
	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:              c,
		TokenGroup:       relayInfo.TokenGroup,
		ModelName:        relayInfo.OriginModelName,
		RequestPath:      c.Request.URL.Path,
		Retry:            common.GetPointer(0),
		SelectionOptions: service.ChannelSelectionOptionsForRequest(c, tokens),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil
	retryRouting := newRelayRetryRouting()
	fastFailureRetryBudget := &relayFastFailureRetryBudget{}
	attemptIndex := 0
	finalRetryLogPending := false
	finalRetryAttemptDuration := time.Duration(0)
	var finalRetryChannelError *types.ChannelError
	attemptState, err := relaycommon.NewRelayAttemptState(c, relayInfo)
	if err != nil {
		newAPIError = types.NewError(
			fmt.Errorf("创建重试请求快照失败: %w", err),
			types.ErrorCodeInvalidRequest,
			types.ErrOptionWithSkipRetry(),
		)
		return
	}

	for retryParam.GetRetry() <= common.RetryTimes {
		if attemptIndex > 0 {
			err = attemptState.Reset(c, relayInfo)
		}
		if err != nil {
			newAPIError = types.NewError(
				fmt.Errorf("恢复重试请求快照失败: %w", err),
				types.ErrorCodeInvalidRequest,
				types.ErrOptionWithSkipRetry(),
			)
			break
		}
		common.SetContextKey(c, service.UpstreamErrorDiagnosticContextKey, nil)
		relayInfo.RetryIndex = attemptIndex
		channel, channelErr := getChannel(c, relayInfo, retryParam, retryRouting)
		if channelErr != nil {
			if retryRouting.candidatesExhausted() && relayInfo.LastError != nil {
				newAPIError = relayInfo.LastError
				break
			}
			// No upstream attempt was made for this terminal error. Do not
			// reuse the previous retry's channel/duration in a final summary.
			finalRetryLogPending = false
			finalRetryChannelError = nil
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			finalRetryLogPending = false
			finalRetryChannelError = nil
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		channel, concurrencyLease, concurrencyErr := acquireRelayChannelConcurrency(c, relayInfo, retryParam, retryRouting, channel, true)
		if concurrencyErr != nil {
			finalRetryLogPending = false
			finalRetryChannelError = nil
			newAPIError = concurrencyErr
			break
		}
		addUsedChannel(c, channel.Id)
		if billingErr := prepareRelayBillingForSelectedGroup(c, relayInfo, tokens, meta); billingErr != nil {
			concurrencyLease.Release()
			finalRetryLogPending = false
			finalRetryChannelError = nil
			newAPIError = billingErr
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		service.BeginChannelDailyCostAttempt(c, channel.Id)
		attemptStartedAt := time.Now()
		service.BeginChannelMonitorPerformanceAttempt(c, attemptStartedAt)
		newAPIError = relayWithChannelConcurrency(c, relayInfo, relayFormat, concurrencyLease)
		attemptDuration := time.Since(attemptStartedAt)
		service.FinalizeChannelDailyCostAttempt(c, channel.Id, false)

		if newAPIError == nil {
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError

		responseStarted := relayAttemptResponseStarted(c, relayInfo, relayFormat)
		retryDecision, fastFailureRetryDelay := fastFailureRetryBudget.decide(
			relayRetryGroup(c, retryParam.TokenGroup),
			retryParam.ModelName,
			channel.Id,
			attemptDuration,
			!responseStarted && isFastFailureSameChannelRetryable(c, newAPIError),
			!responseStarted && shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()),
		)
		shouldRetry := retryDecision != relayRetryNone
		if shouldRetry {
			retryParam.IsRetry = true
		}
		switch retryDecision {
		case relayRetryFastFailureSameChannel:
			retryRouting.retrySameChannel(channel, relayRetryGroup(c, retryParam.TokenGroup))
		case relayRetryOrdinary:
			retryParam.IncreaseRetry()
			retryRouting.exclude(channel.Id)
		}
		channelError := newRelayChannelError(c, channel)
		processChannelErrorWithTiming(
			c, *channelError, newAPIError, shouldRetry, attemptIndex > 0, &attemptDuration, false,
		)
		finalRetryLogPending = shouldRetry
		if shouldRetry {
			finalRetryAttemptDuration = attemptDuration
			finalRetryChannelError = channelError
		} else {
			finalRetryChannelError = nil
		}

		if !shouldRetry {
			break
		}
		if retryDecision == relayRetryFastFailureSameChannel &&
			!waitForRelayFastFailureRetry(c.Request.Context(), fastFailureRetryDelay) {
			break
		}
		attemptIndex++
	}
	if newAPIError != nil && finalRetryLogPending && finalRetryChannelError != nil {
		processChannelErrorWithTiming(c, *finalRetryChannelError,
			newAPIError, false, false, &finalRetryAttemptDuration, true)
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam, retryRoutings ...*relayRetryRouting) (*model.Channel, *types.NewAPIError) {
	if info == nil || retryParam == nil {
		return nil, types.NewError(
			errors.New("重试渠道参数无效"),
			types.ErrorCodeGetChannelFailed,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if info.ChannelMeta == nil && (retryParam == nil || !retryParam.IsRetry) {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	var retryRouting *relayRetryRouting
	if len(retryRoutings) > 0 {
		retryRouting = retryRoutings[len(retryRoutings)-1]
	}
	if retryRouting == nil {
		retryRouting = newRelayRetryRouting()
	}
	modelName := info.OriginModelName
	if retryParam != nil && retryParam.ModelName != "" {
		modelName = retryParam.ModelName
	}
	setupFailedChannels := make(map[int]struct{})
	var lastSetupError *types.NewAPIError
	for {
		channel, selectGroup, err := retryRouting.selectChannel(retryParam)
		if retryParam.TokenGroup == "auto" && channel != nil && selectGroup != "" {
			common.SetContextKey(c, constant.ContextKeyAutoGroup, selectGroup)
		}
		if err != nil {
			return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, modelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if channel == nil {
			if lastSetupError != nil {
				return nil, lastSetupError
			}
			return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, modelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if _, repeated := setupFailedChannels[channel.Id]; repeated {
			return nil, lastSetupError
		}

		info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
		setupErr := middleware.SetupContextForRetry(c, channel, modelName)
		if setupErr == nil {
			return channel, nil
		}
		if !types.IsChannelError(setupErr) || types.IsSkipRetryError(setupErr) {
			return channel, setupErr
		}
		setupFailedChannels[channel.Id] = struct{}{}
		lastSetupError = setupErr
		retryRouting.exclude(channel.Id)
	}
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if types.IsClientGoneError(openaiErr) {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsModelCapacityError(openaiErr) {
		if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
			return false
		}
		return true
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError, isRetryAttempt bool) {
	processChannelErrorWithTiming(c, channelError, err, isRetryAttempt, false, nil, false)
}

func processChannelErrorWithTiming(
	c *gin.Context,
	channelError types.ChannelError,
	err *types.NewAPIError,
	isRetryAttempt bool,
	monitorRetryAttempt bool,
	attemptDuration *time.Duration,
	finalRetrySummary bool,
) {
	// Automatic channel tests are maintenance checks, not production traffic.
	// They may still auto-disable a channel below, but must never mutate the
	// smart-schedule runtime state or stability samples.
	runtimeProtectionEligible := !finalRetrySummary && !isChannelTestContext(c)
	if !isChannelTestContext(c) {
		service.EmitChannelMonitorFailureEvent(
			c,
			channelError.ChannelId,
			c.GetString("original_model"),
			err,
			monitorRetryAttempt,
			finalRetrySummary || !isRetryAttempt,
			finalRetrySummary,
			service.WasChannelDailyCostRequestDispatched(c),
			runtimeProtectionEligible,
			attemptDuration,
		)
	}

	if !finalRetrySummary {
		logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
		// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
		// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
		if service.ShouldDisableChannel(err) && channelError.AutoBan {
			gopool.Go(func() {
				service.DisableChannel(channelError, err.ErrorWithStatusCode())
			})
		}
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		if usingGroup == "" {
			usingGroup = c.GetString("group")
		}
		if autoGroup := common.GetContextKeyString(c, constant.ContextKeyAutoGroup); autoGroup != "" {
			usingGroup = autoGroup
		}
		channelId := channelError.ChannelId
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = channelError.ChannelName
		other["channel_type"] = channelError.ChannelType
		userVisibleMessage, hasUserVisibleMessage := service.ResolveUserErrorMessage(
			service.GetConfiguredErrorMessageMapping(),
			string(err.GetErrorCode()),
			err.StatusCode,
		)
		if hasUserVisibleMessage {
			other["user_visible_error_message"] = userVisibleMessage
		}
		if isChannelTestContext(c) {
			other[model.ChannelMonitorChannelTestLogKey] = true
		}
		if attemptDuration != nil {
			attemptDurationMs := attemptDuration.Milliseconds()
			if attemptDurationMs < 0 {
				attemptDurationMs = 0
			}
			other["channel_monitor_attempt_duration_ms"] = attemptDurationMs
		}
		if finalRetrySummary {
			other["channel_monitor_final_retry_summary"] = true
		}
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := channelError.IsMultiKey
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		if diagnostic, ok := common.GetContextKeyType[service.UpstreamErrorDiagnostic](c, service.UpstreamErrorDiagnosticContextKey); ok {
			adminInfo["upstream_error"] = diagnostic
		}
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), usingGroup, other, isRetryAttempt)
	}
}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *taskdto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		originalDescription := fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)
		description := originalDescription
		if message, ok := service.ResolveUserErrorMessage(
			service.GetConfiguredErrorMessageMapping(),
			fmt.Sprintf("%d", mjErr.Code),
			statusCode,
		); ok {
			description = message
		}
		c.JSON(statusCode, gin.H{
			"description": description,
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, originalDescription))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskError(c, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskError(c, &taskdto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *taskdto.TaskError
	finalRetryLogPending := false
	finalRetryAttemptDuration := time.Duration(0)
	var finalRetryChannelError *types.ChannelError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:              c,
		TokenGroup:       relayInfo.TokenGroup,
		ModelName:        relayInfo.OriginModelName,
		RequestPath:      c.Request.URL.Path,
		Retry:            common.GetPointer(0),
		SelectionOptions: service.ChannelSelectionOptionsForRequest(c, 0),
	}
	retryRouting := newRelayRetryRouting()
	fastFailureRetryBudget := &relayFastFailureRetryBudget{}
	attemptIndex := 0
	successfulChannelID := 0
	attemptState, err := relaycommon.NewRelayAttemptState(c, relayInfo)
	if err != nil {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("创建重试请求快照失败: %w", err), "retry_state_failed", http.StatusInternalServerError)
		respondTaskError(c, taskErr)
		return
	}

	for retryParam.GetRetry() <= common.RetryTimes {
		if attemptIndex > 0 {
			err = attemptState.Reset(c, relayInfo)
		}
		if err != nil {
			taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("恢复重试请求快照失败: %w", err), "retry_state_failed", http.StatusInternalServerError)
			break
		}
		common.SetContextKey(c, service.UpstreamErrorDiagnosticContextKey, nil)
		relayInfo.RetryIndex = attemptIndex
		var channel *model.Channel
		allowAlternative := true

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			lockedCh, taskErr = refreshLockedTaskChannel(relayInfo, lockedCh)
			if taskErr != nil {
				finalRetryLogPending = false
				finalRetryChannelError = nil
				break
			}
			channel = lockedCh
			allowAlternative = false
			if setupErr := middleware.SetupContextForRetry(c, channel, relayInfo.OriginModelName); setupErr != nil {
				finalRetryLogPending = false
				finalRetryChannelError = nil
				taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
				break
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam, retryRouting)
			if channelErr != nil {
				if retryRouting.candidatesExhausted() && taskErr != nil {
					break
				}
				finalRetryLogPending = false
				finalRetryChannelError = nil
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			finalRetryLogPending = false
			finalRetryChannelError = nil
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		channel, concurrencyLease, concurrencyErr := acquireRelayChannelConcurrency(c, relayInfo, retryParam, retryRouting, channel, allowAlternative)
		if concurrencyErr != nil {
			finalRetryLogPending = false
			finalRetryChannelError = nil
			taskErr = service.TaskErrorWrapperLocal(concurrencyErr.Err, "channel_concurrency_limit", concurrencyErr.StatusCode)
			break
		}
		addUsedChannel(c, channel.Id)
		c.Request.Body = io.NopCloser(bodyStorage)

		service.BeginChannelDailyCostAttempt(c, channel.Id)
		attemptStartedAt := time.Now()
		service.BeginChannelMonitorPerformanceAttempt(c, attemptStartedAt)
		result, taskErr = relayTaskWithChannelConcurrency(c, relayInfo, concurrencyLease)
		attemptDuration := time.Since(attemptStartedAt)
		if retryParam.ModelName == "" && relayInfo.OriginModelName != "" {
			retryParam.ModelName = relayInfo.OriginModelName
		}
		if taskErr == nil {
			successfulChannelID = channel.Id
			break
		}
		service.FinalizeChannelDailyCostAttempt(c, channel.Id, false)
		responseStarted := relayResponseStarted(c)
		retryModelName := retryParam.ModelName
		if retryModelName == "" {
			retryModelName = relayInfo.OriginModelName
		}
		retryDecision, fastFailureRetryDelay := fastFailureRetryBudget.decide(
			relayRetryGroup(c, retryParam.TokenGroup),
			retryModelName,
			channel.Id,
			attemptDuration,
			!responseStarted && shouldRetryTaskRelay(c, channel.Id, taskErr, 1),
			!responseStarted && shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()),
		)
		shouldRetry := retryDecision != relayRetryNone
		if shouldRetry {
			retryParam.IsRetry = true
		}
		switch retryDecision {
		case relayRetryFastFailureSameChannel:
			if allowAlternative {
				retryRouting.retrySameChannel(channel, relayRetryGroup(c, retryParam.TokenGroup))
			}
		case relayRetryOrdinary:
			retryParam.IncreaseRetry()
			if allowAlternative {
				retryRouting.exclude(channel.Id)
			}
		}

		if !taskErr.LocalError {
			channelError := newRelayChannelError(c, channel)
			processChannelErrorWithTiming(
				c, *channelError, taskErrorForChannelLog(taskErr), shouldRetry,
				attemptIndex > 0, &attemptDuration, false,
			)
			finalRetryLogPending = shouldRetry
			if shouldRetry {
				finalRetryAttemptDuration = attemptDuration
				finalRetryChannelError = channelError
			} else {
				finalRetryChannelError = nil
			}
		} else {
			finalRetryLogPending = false
			finalRetryChannelError = nil
		}

		if !shouldRetry {
			break
		}
		if retryDecision == relayRetryFastFailureSameChannel &&
			!waitForRelayFastFailureRetry(c.Request.Context(), fastFailureRetryDelay) {
			break
		}
		attemptIndex++
	}
	if taskErr != nil && finalRetryLogPending && finalRetryChannelError != nil {
		processChannelErrorWithTiming(c, *finalRetryChannelError,
			taskErrorForChannelLog(taskErr),
			false, false, &finalRetryAttemptDuration, true)
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.NodeName = common.NodeName
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios(),
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		service.LogTaskConsumption(c, relayInfo, task)
		service.FinalizeChannelDailyCostAttempt(c, successfulChannelID, false)
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func taskErrorForChannelLog(taskErr *taskdto.TaskError) *types.NewAPIError {
	if taskErr == nil {
		return nil
	}
	err := taskErr.Error
	if err == nil {
		err = errors.New(taskErr.Message)
	}
	errorCode := types.ErrorCode(taskErr.Code)
	if errorCode == "" {
		errorCode = types.ErrorCodeBadResponseStatusCode
	}
	return types.NewOpenAIError(err, errorCode, taskErr.StatusCode)
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *taskdto.TaskError) {
	if taskErr == nil || relayResponseStarted(c) {
		return
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	if message, ok := service.ResolveUserErrorMessage(
		service.GetConfiguredErrorMessageMapping(),
		taskErr.Code,
		taskErr.StatusCode,
	); ok {
		taskErr.Message = message
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *taskdto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if types.IsClientGoneError(taskErr.Error) {
		return false
	}
	if taskErr.LocalError {
		return false
	}
	var apiErr *types.NewAPIError
	if errors.As(taskErr.Error, &apiErr) && types.IsSkipRetryError(apiErr) {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
