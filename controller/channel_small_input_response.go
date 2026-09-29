package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/channelprobe"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func tryChannelSmallInputResponse(c *gin.Context, info *relaycommon.RelayInfo, channelID int) (bool, *types.NewAPIError) {
	if channelID <= 0 || info == nil || info.Request == nil || c.Writer.Written() || !service.IsChannelBusinessRequest(c, info) {
		return false, nil
	}
	policy, err := model.GetChannelProbePolicy(c.Request.Context(), channelID)
	if err != nil {
		return false, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if !policy.AutoProbeDisabled || !policy.SmallInputResponseEnabled {
		return false, nil
	}
	tokens, outputLimit, valid := service.EstimateChannelSmallInput(info)
	if !valid || tokens >= policy.SmallInputThresholdTokens {
		return false, nil
	}
	if setting.ShouldCheckPromptSensitive() {
		meta := info.Request.GetTokenCountMeta()
		if meta != nil {
			if contains, words := service.CheckSensitiveText(meta.CombineText); contains {
				service.RequestPolicy(c).AddEvent(service.PolicyEvent{ErrorCode: string(types.ErrorCodeSensitiveWordsDetected), ErrorSource: "local", Decision: service.PolicyDecision{Action: "stop", Reason: "local_rejection", Source: "global"}, Health: "unchanged"})
				return false, types.NewError(fmt.Errorf("user sensitive words detected: %s", strings.Join(words, ", ")), types.ErrorCodeSensitiveWordsDetected)
			}
		}
	}
	if info.Billing != nil {
		billing, ok := info.Billing.(interface{ FinishWithoutCharge(context.Context) error })
		if !ok {
			return false, types.NewError(errors.New("计费会话无法安全结束本地响应"), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if err := billing.FinishWithoutCharge(c.Request.Context()); err != nil {
			return false, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}
	text := service.BuildChannelSmallInputText(policy.SmallInputResponseText, info.OriginModelName, tokens, outputLimit)
	c.Set(service.ChannelLocalResponseContextKey, true)
	// Local responses are not samples of upstream availability or performance.
	info.PerformanceBusinessRejection = true
	service.EmitChannelLocalResponseEvent(c, info, channelID, policy, tokens)
	channelprobe.WriteLocalTextResponse(c, info.Request, info.OriginModelName, channelprobe.ResponseConfig{
		ResponseText: text.Text, InputTokens: text.InputTokens, OutputTokens: text.OutputTokens, Truncated: text.Truncated,
	})
	return true, nil
}
