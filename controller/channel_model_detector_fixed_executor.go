package controller

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const channelModelDetectorRelayMaxResponseBytes = 16 << 20

var (
	ErrChannelModelDetectorFixedChannelUnavailable = errors.New("模型检测固定渠道不可用")
	ErrChannelModelDetectorResponseTooLarge        = errors.New("模型检测渠道响应过大")
)

type ChannelModelDetectorFixedExecutor struct {
	DB               *gorm.DB
	MaxResponseBytes int64
}

func NewChannelModelDetectorFixedExecutor(db *gorm.DB) *ChannelModelDetectorFixedExecutor {
	return &ChannelModelDetectorFixedExecutor{DB: db, MaxResponseBytes: channelModelDetectorRelayMaxResponseBytes}
}

func (executor *ChannelModelDetectorFixedExecutor) ExecuteChannelModelDetectorAttempt(ctx context.Context, execution service.ChannelModelDetectorRelayExecution) (result service.ChannelModelDetectorRelayUpstreamResult, returnedErr error) {
	if ctx == nil || execution.Source != service.ChannelModelDetectorRequestSource || execution.RunID == "" || execution.TargetID <= 0 || execution.ExecutionID <= 0 || execution.ChannelID <= 0 || execution.AttemptNo <= 0 || len(execution.RequestBody) == 0 {
		return result, service.ErrChannelModelDetectorRelayInvalidRequest
	}
	db := executor.DB
	if db == nil {
		db = model.DB
	}
	if db == nil {
		return result, service.ErrChannelModelDetectorRelayUnavailable
	}

	var run model.ChannelModelDetectionRun
	if err := db.WithContext(ctx).Where("run_id = ? AND channel_id = ?", execution.RunID, execution.ChannelID).First(&run).Error; err != nil {
		return result, ErrChannelModelDetectorFixedChannelUnavailable
	}
	var storedExecution model.ChannelModelDetectionExecution
	if err := db.WithContext(ctx).Where(
		"id = ? AND run_id = ? AND target_id = ? AND channel_id = ?",
		execution.ExecutionID, execution.RunID, execution.TargetID, execution.ChannelID,
	).First(&storedExecution).Error; err != nil {
		return result, ErrChannelModelDetectorFixedChannelUnavailable
	}
	if storedExecution.RequestModel != execution.RequestModel || storedExecution.ClaimedModel != execution.ClaimedModel || storedExecution.Preset != execution.Preset {
		return result, service.ErrChannelModelDetectorRelayInvalidRequest
	}

	var channel model.Channel
	if err := db.WithContext(ctx).Where("id = ?", execution.ChannelID).First(&channel).Error; err != nil || !channelModelDetectorChannelAllowed(run.Trigger, channel.Status) {
		return result, ErrChannelModelDetectorFixedChannelUnavailable
	}
	pricingUserID := run.PricingContextUserId
	if pricingUserID <= 0 {
		var root model.User
		if err := db.WithContext(ctx).Select("id").Where("role = ?", common.RoleRootUser).Order("id ASC").First(&root).Error; err != nil || root.Id <= 0 {
			return result, errors.New("模型检测计价账号不可用")
		}
		pricingUserID = root.Id
	}
	var pricingUser model.User
	if err := db.WithContext(ctx).Where("id = ?", pricingUserID).First(&pricingUser).Error; err != nil {
		return result, errors.New("模型检测计价账号不可用")
	}

	maxResponseBytes := executor.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = channelModelDetectorRelayMaxResponseBytes
	}
	writer := newChannelModelDetectorResponseWriter(maxResponseBytes)
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/responses", bytes.NewReader(execution.RequestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.RequestIdKey, common.NewRequestId())
	pricingUser.ToBaseUser().WriteContext(c)
	common.SetContextKey(c, constant.ContextKeyUserId, pricingUserID)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, pricingUser.Group)
	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())

	if setupErr := middleware.SetupContextForSelectedChannel(c, &channel, execution.RequestModel); setupErr != nil {
		return result, setupErr
	}
	request, err := helper.GetAndValidateResponsesRequest(c)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeInvalidRequest)
	}
	info := relaycommon.GenRelayInfoResponses(c, request)
	info.InitRequestConversionChain()
	info.IsChannelTest = true
	info.InitChannelMeta(c)
	requestInput, err := helper.BuildBillingExprRequestInputFromRequest(request, info.RequestHeaders)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeJsonMarshalFailed)
	}
	info.BillingRequestInput = &requestInput
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return result, types.NewError(err, types.ErrorCodeChannelModelMappedError)
	}
	if request.Model == "" {
		request.Model = execution.RequestModel
	}

	tokenMeta := request.GetTokenCountMeta()
	promptTokens, err := service.EstimateRequestToken(c, tokenMeta, info)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeCountTokenFailed)
	}
	info.SetEstimatePromptTokens(promptTokens)
	if _, err := helper.ModelPriceHelper(c, info, promptTokens, tokenMeta); err != nil {
		return result, types.NewError(err, types.ErrorCodeModelPriceError)
	}

	snapshot, snapshotErr := service.CaptureChannelModelDetectionCostSnapshot(channel.Id)
	if snapshotErr == nil {
		snapshot, snapshotErr = service.AlignChannelModelDetectionCostSnapshot(info, snapshot)
	}
	if snapshotErr != nil {
		snapshot = service.ChannelModelDetectionCostSnapshot{}
	}
	keyFingerprint, keyDisplay := model.ChannelDailyCostAPIKeyIdentity(info.ApiKey)
	costEventID := common.GetUUID()
	requestID := c.GetString(common.RequestIdKey)
	prepared, _, err := service.PrepareChannelModelDetectionCostEvent(ctx, db, service.ChannelModelDetectionCostAttemptInput{
		CostEventId:            costEventID,
		RunId:                  execution.RunID,
		TargetId:               execution.TargetID,
		ExecutionId:            execution.ExecutionID,
		ChannelId:              execution.ChannelID,
		RequestModel:           execution.RequestModel,
		ClaimedModel:           execution.ClaimedModel,
		Preset:                 execution.Preset,
		DetectorRequestId:      execution.DetectorRequestID,
		AttemptNo:              execution.AttemptNo,
		RequestId:              requestID,
		UpstreamKeyId:          fmt.Sprintf("channel:%d:key:%d", channel.Id, info.ChannelMultiKeyIndex),
		UpstreamKeyFingerprint: keyFingerprint,
		UpstreamKeyDisplay:     keyDisplay,
		EstimatedQuota:         0,
		Snapshot:               snapshot,
	})
	if err != nil {
		return result, err
	}
	service.BeginChannelModelDetectionTransport(c, db, prepared.CostEventId)
	costFinalized := false
	defer func() {
		dispatched, dispatchErr := service.ChannelModelDetectionTransportStatus(c)
		result.Dispatched = dispatched || dispatchErr != nil
		result.RequestID = requestID
		result.UpstreamRequestID = strings.TrimSpace(c.GetString(common.UpstreamRequestIdKey))
		if dispatchErr != nil {
			markErr := service.MarkChannelModelDetectionTransportUnresolved(c, service.ChannelModelDetectionCostUnresolvedInput{
				UpstreamRequestId:     result.UpstreamRequestID,
				ErrorCode:             "dispatch_state_persist_failed",
				SanitizedErrorMessage: "模型检测发送状态写入失败",
			})
			if markErr != nil && returnedErr == nil {
				returnedErr = markErr
			}
			return
		}
		if !dispatched {
			_, markErr := service.MarkChannelModelDetectionCostEventNotStarted(ctx, db, prepared.CostEventId, 0)
			if markErr != nil && returnedErr == nil {
				returnedErr = markErr
			}
			return
		}
		if costFinalized {
			return
		}
		_, markErr := service.MarkChannelModelDetectionCostEventUnresolved(ctx, db, service.ChannelModelDetectionCostUnresolvedInput{
			CostEventId:           prepared.CostEventId,
			UpstreamRequestId:     result.UpstreamRequestID,
			ErrorCode:             "actual_usage_unavailable",
			SanitizedErrorMessage: "模型检测请求未返回可核验 Usage，成本待核实",
		})
		if markErr != nil && returnedErr == nil {
			returnedErr = markErr
		}
	}()

	adaptor := relay.GetAdaptor(info.ApiType)
	if adaptor == nil {
		return result, types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType)
	}
	adaptor.Init(info)
	requestCopy, err := common.DeepCopy(request)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeInvalidRequest)
	}
	convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *requestCopy)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	convertedRequest = ensureChannelModelDetectorStreamUsage(convertedRequest)
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			var returnErr *relaycommon.ParamOverrideReturnError
			if errors.As(err, &returnErr) {
				return result, relaycommon.NewAPIErrorFromParamOverride(returnErr)
			}
			return result, types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid)
		}
	}
	requestBody, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return result, types.NewError(err, types.ErrorCodeConvertRequestFailed)
	}
	defer closer.Close()

	response, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return result, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	var httpResponse *http.Response
	if response != nil {
		httpResponse, _ = response.(*http.Response)
	}
	if httpResponse == nil {
		return result, types.NewError(errors.New("模型检测上游响应缺失"), types.ErrorCodeBadResponse)
	}
	result.StatusCode = httpResponse.StatusCode
	if httpResponse.StatusCode != http.StatusOK {
		apiErr := service.RelayErrorHandler(ctx, httpResponse, false)
		return result, apiErr
	}

	usageValue, responseErr := adaptor.DoResponse(c, httpResponse, info)
	if writer.err != nil {
		return result, writer.err
	}
	result.ContentType = writer.Header().Get("Content-Type")
	result.ResponseBody = writer.Bytes()
	result.UsagePayload = result.ResponseBody
	if responseErr != nil {
		return result, responseErr
	}
	usage, ok := usageValue.(*dto.Usage)
	authoritativeUsage, usageErr := service.NormalizeChannelModelDetectorUsage(result.ResponseBody)
	if errors.Is(usageErr, service.ErrChannelModelDetectorUsageUnavailable) && ok && usage != nil &&
		!common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens) {
		// A converted adaptor can parse authoritative provider usage but emit a
		// response envelope that does not retain the provider's original shape.
		// Reuse that non-estimated DTO rather than dropping a billable request.
		authoritativeUsage, usageErr = service.NormalizeChannelModelDetectorDTOUsage(usage)
	}
	if usageErr != nil {
		return result, nil
	}
	if !ok {
		usage = nil
	}
	usage, usageErr = service.MergeChannelModelDetectorAuthoritativeUsage(usage, authoritativeUsage)
	if usageErr != nil {
		return result, nil
	}
	quota := service.CalculateChannelModelDetectionQuotaWithSnapshot(c, info, usage, snapshot)
	if !quota.Reliable || quota.Usage.InputTokens != authoritativeUsage.InputTokens || quota.Usage.OutputTokens != authoritativeUsage.OutputTokens || quota.Usage.TotalTokens != authoritativeUsage.TotalTokens {
		return result, nil
	}
	result.Usage = &quota.Usage
	_, err = service.SettleChannelModelDetectionCostEvent(ctx, db, service.ChannelModelDetectionCostSettlementInput{
		CostEventId:       prepared.CostEventId,
		SettledQuota:      quota.SettledQuota,
		CostBasisQuota:    quota.CostBasisQuota,
		InputTokens:       quota.Usage.InputTokens,
		OutputTokens:      quota.Usage.OutputTokens,
		TotalTokens:       quota.Usage.TotalTokens,
		UsageSource:       quota.Usage.Source,
		UsageAvailable:    true,
		UpstreamRequestId: c.GetString(common.UpstreamRequestIdKey),
	})
	if err == nil {
		costFinalized = true
	}
	return result, err
}

func ensureChannelModelDetectorStreamUsage(request any) any {
	switch value := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if value == nil || value.Stream == nil || !*value.Stream {
			return request
		}
		if value.StreamOptions == nil {
			value.StreamOptions = &dto.StreamOptions{}
		}
		value.StreamOptions.IncludeUsage = true
	case dto.GeneralOpenAIRequest:
		if value.Stream == nil || !*value.Stream {
			return request
		}
		if value.StreamOptions == nil {
			value.StreamOptions = &dto.StreamOptions{}
		}
		value.StreamOptions.IncludeUsage = true
		return value
	}
	return request
}

func channelModelDetectorChannelAllowed(trigger string, status int) bool {
	if status == common.ChannelStatusEnabled {
		return true
	}
	return trigger == model.ChannelModelDetectionTriggerManual &&
		(status == common.ChannelStatusManuallyDisabled || status == common.ChannelStatusAutoDisabled)
}

type channelModelDetectorResponseWriter struct {
	header   http.Header
	status   int
	maxBytes int64
	body     bytes.Buffer
	err      error
}

func newChannelModelDetectorResponseWriter(maxBytes int64) *channelModelDetectorResponseWriter {
	return &channelModelDetectorResponseWriter{header: make(http.Header), maxBytes: maxBytes}
}

func (writer *channelModelDetectorResponseWriter) Header() http.Header { return writer.header }

func (writer *channelModelDetectorResponseWriter) WriteHeader(statusCode int) {
	if writer.status == 0 {
		writer.status = statusCode
	}
}

func (writer *channelModelDetectorResponseWriter) WriteHeaderNow() {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
}

func (writer *channelModelDetectorResponseWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	if int64(writer.body.Len())+int64(len(data)) > writer.maxBytes {
		writer.err = ErrChannelModelDetectorResponseTooLarge
		return 0, writer.err
	}
	return writer.body.Write(data)
}

func (writer *channelModelDetectorResponseWriter) WriteString(value string) (int, error) {
	return writer.Write([]byte(value))
}

func (writer *channelModelDetectorResponseWriter) Status() int {
	if writer.status == 0 {
		return http.StatusOK
	}
	return writer.status
}

func (writer *channelModelDetectorResponseWriter) Size() int { return writer.body.Len() }
func (writer *channelModelDetectorResponseWriter) Written() bool {
	return writer.status != 0 || writer.body.Len() > 0
}
func (writer *channelModelDetectorResponseWriter) Flush()              {}
func (writer *channelModelDetectorResponseWriter) Pusher() http.Pusher { return nil }
func (writer *channelModelDetectorResponseWriter) CloseNotify() <-chan bool {
	return make(chan bool)
}
func (writer *channelModelDetectorResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("模型检测内部响应不支持连接劫持")
}
func (writer *channelModelDetectorResponseWriter) Bytes() []byte {
	return append([]byte(nil), writer.body.Bytes()...)
}

var _ gin.ResponseWriter = (*channelModelDetectorResponseWriter)(nil)
var _ io.StringWriter = (*channelModelDetectorResponseWriter)(nil)
