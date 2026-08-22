package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type channelConcurrencyLimitUpdateRequest struct {
	ConcurrencyLimit *int `json:"concurrency_limit"`
}

func GetChannelMonitorConcurrency(c *gin.Context) {
	snapshot, err := service.GetChannelConcurrencySnapshot(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"channels":     snapshot,
			"generated_at": common.GetTimestamp(),
		},
	})
}

func UpdateChannelMonitorConcurrencyLimit(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "无效的渠道 ID")
		return
	}
	if _, err = model.GetChannelById(channelID, false); err != nil {
		common.ApiError(c, err)
		return
	}

	var request channelConcurrencyLimitUpdateRequest
	if err = common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	if request.ConcurrencyLimit == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供渠道并发限制"})
		return
	}
	if *request.ConcurrencyLimit < 0 || *request.ConcurrencyLimit > service.MaxChannelConcurrencyLimit {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道并发限制必须在 0 到 100000 之间"})
		return
	}
	currentLimit := 0
	currentMonitor, currentErr := model.GetChannelRatioMonitor(channelID)
	if currentErr == nil {
		currentLimit = currentMonitor.ConcurrencyLimit
	} else if !errors.Is(currentErr, gorm.ErrRecordNotFound) {
		common.ApiError(c, currentErr)
		return
	}
	monitor, err := service.SaveChannelConcurrencyLimit(c.Request.Context(), channelID, *request.ConcurrencyLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if currentLimit != monitor.ConcurrencyLimit {
		recordManageAudit(c, "channel.monitor_concurrency_limit_update", map[string]interface{}{
			"id": channelID, "concurrency_limit": monitor.ConcurrencyLimit,
		})
	}
	common.ApiSuccess(c, gin.H{
		"concurrency_limit": monitor.ConcurrencyLimit,
	})
}

func acquireRelayChannelConcurrency(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	retryParam *service.RetryParam,
	retryRouting *relayRetryRouting,
	channel *model.Channel,
	allowAlternative bool,
) (*model.Channel, *service.ChannelConcurrencyLease, *types.NewAPIError) {
	if retryRouting == nil {
		retryRouting = newRelayRetryRouting()
	}
	var lastSetupError *types.NewAPIError
	for {
		if channel != nil {
			lease, acquired, status, err := service.AcquireChannelConcurrency(c.Request.Context(), channel.Id)
			if err != nil {
				return nil, nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
			}
			if acquired {
				return channel, lease, nil
			}

			if !allowAlternative {
				return nil, nil, channelConcurrencySaturatedError(channel.Id, status)
			}
			if _, specificChannel := c.Get("specific_channel_id"); specificChannel {
				return nil, nil, channelConcurrencySaturatedError(channel.Id, status)
			}

			retryRouting.exclude(channel.Id)
		}

		var selectGroup string
		selected, selectGroup, err := retryRouting.selectChannelCurrentRound(retryParam)
		if err != nil {
			return nil, nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（并发重选）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if selected == nil {
			if lastSetupError != nil {
				return nil, nil, lastSetupError
			}
			return nil, nil, types.NewErrorWithStatusCode(errors.New("当前分组上游负载已达到渠道并发限制，请稍后再试"), types.ErrorCodeGetChannelFailed, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
		}
		channel = selected
		if retryParam.TokenGroup == "auto" && selectGroup != "" {
			common.SetContextKey(c, constant.ContextKeyAutoGroup, selectGroup)
		}
		info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
		modelName := retryParam.ModelName
		if modelName == "" {
			modelName = info.OriginModelName
		}
		if setupErr := middleware.SetupContextForRetry(c, channel, modelName); setupErr != nil {
			if !types.IsChannelError(setupErr) || types.IsSkipRetryError(setupErr) {
				return nil, nil, setupErr
			}
			lastSetupError = setupErr
			retryRouting.exclude(channel.Id)
			channel = nil
			continue
		}
		lastSetupError = nil
	}
}

func channelConcurrencySaturatedError(channelID int, status service.ChannelConcurrencyStatus) *types.NewAPIError {
	message := fmt.Sprintf("渠道 #%d 当前并发 %d 已达到限制 %d，请稍后再试", channelID, status.Active, status.Limit)
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeGetChannelFailed, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
}

func relayWithChannelConcurrency(c *gin.Context, info *relaycommon.RelayInfo, relayFormat types.RelayFormat, lease *service.ChannelConcurrencyLease) *types.NewAPIError {
	defer lease.Release()
	resetRelayAttemptResponseState(c)
	var apiErr *types.NewAPIError
	switch relayFormat {
	case types.RelayFormatOpenAIRealtime:
		apiErr = relay.WssHelper(c, info)
	case types.RelayFormatClaude:
		apiErr = relay.ClaudeHelper(c, info)
	case types.RelayFormatGemini:
		apiErr = geminiRelayHandler(c, info)
	default:
		apiErr = relayHandler(c, info)
	}
	return markAcceptedUpstreamResponseError(c, apiErr)
}

func relayTaskWithChannelConcurrency(c *gin.Context, info *relaycommon.RelayInfo, lease *service.ChannelConcurrencyLease) (*relay.TaskSubmitResult, *dto.TaskError) {
	defer lease.Release()
	resetRelayAttemptResponseState(c)
	return relay.RelayTaskSubmit(c, info)
}
