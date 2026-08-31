package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

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
	RPMLimit         *int `json:"rpm_limit"`
}

const (
	channelConcurrencyWaitInterval = 100 * time.Millisecond
)

func GetChannelMonitorConcurrency(c *gin.Context) {
	channelIDs, err := model.GetChannelIDsForMonitor(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	configs, err := model.GetChannelConcurrencyConfigsForChannelIDsWithContext(c.Request.Context(), channelIDs)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	statuses, err := service.GetChannelConcurrencySnapshotWithRPMForChannelIDsAndConfigs(
		c.Request.Context(), channelIDs, configs,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"channels":     statuses,
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
	if _, err = model.GetChannelForMonitorWithContext(c.Request.Context(), channelID); err != nil {
		common.ApiError(c, err)
		return
	}

	var request channelConcurrencyLimitUpdateRequest
	if err = common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
		return
	}
	if request.ConcurrencyLimit == nil && request.RPMLimit == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供渠道并发或 RPM 限制"})
		return
	}
	currentLimit := 0
	currentRPMLimit := 0
	currentMonitor, currentErr := model.GetChannelRatioMonitorWithContext(c.Request.Context(), channelID)
	if currentErr == nil {
		currentLimit = currentMonitor.ConcurrencyLimit
		currentRPMLimit = currentMonitor.RPMLimit
	} else if !errors.Is(currentErr, gorm.ErrRecordNotFound) {
		common.ApiError(c, currentErr)
		return
	}
	concurrencyLimit := currentLimit
	if request.ConcurrencyLimit != nil {
		concurrencyLimit = *request.ConcurrencyLimit
	}
	rpmLimit := currentRPMLimit
	if request.RPMLimit != nil {
		rpmLimit = *request.RPMLimit
	}
	if concurrencyLimit < 0 || concurrencyLimit > service.MaxChannelConcurrencyLimit {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道并发限制必须在 0 到 100000 之间"})
		return
	}
	if rpmLimit < 0 || rpmLimit > service.MaxChannelRPMLimit {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道 RPM 限制必须在 0 到 100000 之间"})
		return
	}
	monitor, err := service.SaveChannelConcurrencyLimits(c.Request.Context(), channelID, &concurrencyLimit, &rpmLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if currentLimit != monitor.ConcurrencyLimit || currentRPMLimit != monitor.RPMLimit {
		recordManageAudit(c, "channel.monitor_concurrency_limit_update", map[string]interface{}{
			"id": channelID, "concurrency_limit": monitor.ConcurrencyLimit, "rpm_limit": monitor.RPMLimit,
		})
	}
	common.ApiSuccess(c, gin.H{
		"concurrency_limit": monitor.ConcurrencyLimit,
		"rpm_limit":         monitor.RPMLimit,
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
	var lastSaturatedError *types.NewAPIError
	var saturatedChannels []int
	waitDuration := time.Duration(getChannelMonitorSettings().ChannelConcurrencyWaitSeconds) * time.Second
	waitDeadline := time.Now().Add(waitDuration)
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
				lastSaturatedError = channelConcurrencySaturatedError(channel.Id, status)
				if !waitForChannelConcurrency(c.Request.Context(), waitDeadline) {
					return nil, nil, lastSaturatedError
				}
				continue
			}
			if _, specificChannel := c.Get("specific_channel_id"); specificChannel {
				lastSaturatedError = channelConcurrencySaturatedError(channel.Id, status)
				if !waitForChannelConcurrency(c.Request.Context(), waitDeadline) {
					return nil, nil, lastSaturatedError
				}
				continue
			}

			_, alreadyExcluded := retryRouting.excluded[channel.Id]
			retryRouting.exclude(channel.Id)
			lastSaturatedError = channelConcurrencySaturatedError(channel.Id, status)
			if !alreadyExcluded {
				saturatedChannels = append(saturatedChannels, channel.Id)
			}
		}

		var selectGroup string
		selected, selectGroup, err := retryRouting.selectChannelCurrentRound(retryParam)
		if err != nil {
			return nil, nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（并发重选）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if selected == nil {
			if len(saturatedChannels) == 0 {
				if lastSetupError != nil {
					return nil, nil, lastSetupError
				}
				return nil, nil, types.NewErrorWithStatusCode(errors.New("当前分组暂无可用渠道，请稍后再试"), types.ErrorCodeGetChannelFailed, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
			}
			if !waitForChannelConcurrency(c.Request.Context(), waitDeadline) {
				if lastSaturatedError != nil {
					return nil, nil, lastSaturatedError
				}
				if lastSetupError != nil {
					return nil, nil, lastSetupError
				}
				return nil, nil, types.NewErrorWithStatusCode(errors.New("当前分组暂无可用渠道，请稍后再试"), types.ErrorCodeGetChannelFailed, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry())
			}
			for _, saturatedChannelID := range saturatedChannels {
				retryRouting.restore(saturatedChannelID)
			}
			saturatedChannels = nil
			channel = nil
			continue
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

func waitForChannelConcurrency(ctx context.Context, deadline time.Time) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	interval := channelConcurrencyWaitInterval
	if remaining < interval {
		interval = remaining
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return time.Now().Before(deadline)
	}
}

func channelConcurrencySaturatedError(channelID int, status service.ChannelConcurrencyStatus) *types.NewAPIError {
	message := fmt.Sprintf("渠道 #%d 当前并发 %d 已达到限制 %d，请稍后再试", channelID, status.Active, status.Limit)
	if status.RPMLimit > 0 && status.CurrentRPM >= status.RPMLimit {
		message = fmt.Sprintf("渠道 #%d 当前 RPM %d 已达到限制 %d，请稍后再试", channelID, status.CurrentRPM, status.RPMLimit)
	}
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

func relayTaskWithChannelConcurrency(c *gin.Context, info *relaycommon.RelayInfo, lease *service.ChannelConcurrencyLease, submit taskSubmitAttempt) (*relay.TaskSubmitResult, *dto.TaskError) {
	defer lease.Release()
	resetRelayAttemptResponseState(c)
	return submit(c, info)
}
