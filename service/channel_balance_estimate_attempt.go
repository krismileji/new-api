package service

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

const channelBalanceRelayInfoContextKey = "channel_balance_relay_info"

type channelBalanceAttempt struct {
	Config              ChannelBalanceConfig
	ConversionFactor    float64
	Sample              string
	SampleEligible      bool
	Finished            bool
	CompletionUncertain bool
	Model               string
}

// PrepareChannelBalanceAttempt uses the already-priced relay request. The
// transport hook reads it after provider-specific request pricing is ready.
func PrepareChannelBalanceAttempt(ctx *gin.Context, info *relaycommon.RelayInfo) {
	ctx.Set(channelBalanceRelayInfoContextKey, info)
}

func startChannelBalanceAttempt(ctx *gin.Context, state *channelDailyCostAttemptState) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.Balance != nil {
		return
	}
	value, _ := ctx.Get(channelDailyCostSnapshotContextKey)
	snapshot, _ := value.(channelDailyCostSnapshot)
	// Read the channel locator from the primary before a new reservation. Other
	// nodes may still have a pricing snapshot cached when an account is rebound.
	// In-flight attempts retain the captured locator and never take this path again.
	if client := common.RedisMonitorWriteClient(); common.RedisEnabled && client != nil {
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelBalanceOperationTimeout)
		encoded, readErr := client.Get(readCtx, fmt.Sprintf("channel_balance:{%d}:config", state.ChannelId)).Result()
		cancel()
		var current ChannelBalanceConfig
		if readErr == nil && common.UnmarshalJsonStr(encoded, &current) == nil && current.ChannelID == state.ChannelId &&
			(current.AccountID != snapshot.BalanceConfig.AccountID || (current.AccountID > 0 && current.Revision != snapshot.BalanceConfig.Revision)) {
			snapshot.BalanceConfig = current
			snapshot.ChannelId = state.ChannelId
			if current.AccountID > 0 {
				snapshot.CostRatioCNY, snapshot.ConversionFactor, snapshot.Configured = current.CostRatioCNY, current.ConversionFactor, current.PriceConfigured
			}
			ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
		}
	}
	if snapshot.ChannelId != state.ChannelId || snapshot.BalanceConfig.Account == "" {
		// A missing price snapshot still needs an unknown reservation. Read the
		// monitor's cached identity only; never load SQL for an estimate.
		client := common.RedisMonitorReadClient()
		if !common.RedisEnabled || client == nil {
			return
		}
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelBalanceOperationTimeout)
		encoded, err := client.Get(readCtx, fmt.Sprintf("channel_balance:{%d}:config", state.ChannelId)).Result()
		cancel()
		if err != nil || common.UnmarshalJsonStr(encoded, &snapshot.BalanceConfig) != nil {
			return
		}
		snapshot.ChannelId = state.ChannelId
		ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
	}
	if !snapshot.BalanceConfig.Enabled {
		return
	}
	attempt := &channelBalanceAttempt{Config: snapshot.BalanceConfig, ConversionFactor: snapshot.ConversionFactor}
	state.Balance = attempt
	budget := int64(-1)
	if value, exists := ctx.Get(channelBalanceRelayInfoContextKey); exists {
		if info, valid := value.(*relaycommon.RelayInfo); valid && info != nil && info.ChannelMeta != nil && info.ChannelId == state.ChannelId {
			budget, attempt.Sample, attempt.SampleEligible = channelBalanceRequestBudget(ctx, info, snapshot)
			attempt.CompletionUncertain = info.ClientWs != nil || info.TaskRelayInfo != nil
			attempt.Model = info.OriginModelName
		}
	}
	eligible := "0"
	if attempt.SampleEligible {
		eligible = "1"
	}
	recovery, _ := common.Marshal(attempt)
	raw, err := runChannelBalanceOperation(ctx, attempt.Config, "start", state.CostEventId, attempt.Sample,
		common.GetUUID(), budget, state.CostEventId, eligible, string(recovery), attempt.Model)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("渠道 #%d 进行中费用预估失败: %v", state.ChannelId, err))
		return
	}
	notifyChannelBalancePolicy(attempt.Config, raw)
}

func channelBalanceRequestBudget(ctx *gin.Context, info *relaycommon.RelayInfo, snapshot channelDailyCostSnapshot) (int64, string, bool) {
	if info.AudioUsage || info.ClientWs != nil || info.TaskRelayInfo != nil {
		return -1, "", false
	}
	price := info.PriceData
	price.GroupRatioInfo.GroupRatio = 1
	price.GroupRatioInfo.GroupSpecialRatio = 0
	price.GroupRatioInfo.HasSpecialRatio = false
	price.FreeModel, price.Quota, price.QuotaToPreConsume = false, 0, 0
	var tier string
	var expression string
	var rules []billingexpr.RequestRuleTrace
	quotaBeforeGroup := 0.0
	if tiered := info.TieredBillingSnapshot; tiered != nil {
		expression, tier = tiered.ExprString, tiered.EstimatedTier
		quotaBeforeGroup = tiered.EstimatedQuotaBeforeGroup
		snapshot.QuotaPerUnit = tiered.QuotaPerUnit
		request := billingexpr.RequestInput{}
		if info.BillingRequestInput != nil {
			request = *info.BillingRequestInput
		}
		_, trace, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{
			P: float64(tiered.EstimatedPromptTokens), Len: float64(tiered.EstimatedPromptTokens), C: float64(tiered.EstimatedCompletionTokens),
		}, request)
		if err != nil {
			return -1, "", false
		}
		rules = trace.RequestRules
	} else {
		prompt := info.GetEstimatePromptTokens()
		completion := 8192
		if info.Request != nil {
			if meta := info.Request.GetTokenCountMeta(); meta != nil && meta.MaxTokens > 0 {
				completion = meta.MaxTokens
			}
		}
		if info.RelayMode == relayconstant.RelayModeEmbeddings || info.RelayMode == relayconstant.RelayModeRerank {
			completion = 0
		}
		// Request validators impose tighter protocol bounds. This check also
		// protects internal/probe callers before forming the synthetic usage.
		if prompt < 0 || prompt > common.MaxQuota/2 || completion > common.MaxQuota/2 {
			return -1, "", false
		}
		copied := *info
		copied.PriceData, copied.QuotaClamp = price, nil
		summary := calculateTextQuotaSummaryWithQuotaPerUnit(ctx, &copied, &dto.Usage{
			PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion,
		}, snapshot.QuotaPerUnit)
		if copied.QuotaClamp != nil {
			return -1, "", false
		}
		quotaBeforeGroup = float64(summary.Quota)
	}
	// Include effective model prices, upstream ratio and request billing rules;
	// exclude local user/group prices and token counts so samples can be shared.
	fingerprint, err := common.Marshal([]any{info.OriginModelName, info.UpstreamModelName, price, price.OtherRatios(),
		snapshot.CostRatioCNY, snapshot.ConversionFactor, snapshot.QuotaPerUnit, expression, tier, rules})
	if err != nil {
		return -1, "", false
	}
	sample := fmt.Sprintf("%x", sha256.Sum256(fingerprint))
	eligible := channelDailyCostSourceKind(ctx) == "business" && !ctx.GetBool("channel_test")
	cost, valid := calculateChannelDailyCost(snapshot, quotaBeforeGroup)
	if !valid {
		return -1, sample, eligible
	}
	budget, err := channelBalanceCostMicro(cost, snapshot.ConversionFactor)
	if err != nil {
		return -1, sample, eligible
	}
	return budget, sample, eligible
}

func finishChannelBalanceAttempt(ctx *gin.Context, snapshot channelDailyCostSnapshot, eventID string, costNanoCNY int64, settled, completionUncertain bool) {
	if ctx == nil || snapshot.BalanceConfig.Account == "" || !snapshot.BalanceConfig.Enabled {
		return
	}
	var state *channelDailyCostAttemptState
	if value, ok := ctx.Get(channelDailyCostAttemptContextKey); ok {
		state, _ = value.(*channelDailyCostAttemptState)
	}
	attempt := &channelBalanceAttempt{Config: snapshot.BalanceConfig, ConversionFactor: snapshot.ConversionFactor}
	if state != nil && state.ChannelId == snapshot.ChannelId {
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.Balance != nil {
			attempt = state.Balance
		} else {
			state.Balance = attempt
		}
	}
	if attempt.Finished {
		return
	}
	attempt.CompletionUncertain = attempt.CompletionUncertain || completionUncertain
	// Incremental WebSocket usage and async task submission are not evidence
	// that the upstream job ended. Keep coverage incomplete for those paths.
	settled = settled && !attempt.CompletionUncertain
	amount := int64(-1)
	if settled {
		if value, err := channelBalanceCostMicro(costNanoCNY, attempt.ConversionFactor); err == nil {
			amount = value
		}
	}
	eligible := "0"
	if attempt.SampleEligible && settled {
		eligible = "1"
	}
	uncertain := "0"
	if attempt.CompletionUncertain {
		uncertain = "1"
	}
	raw, err := runChannelBalanceOperation(ctx, attempt.Config, "finish", eventID, attempt.Sample,
		common.GetUUID(), amount, eventID, eligible, 0, uncertain)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("渠道 #%d 已完成费用预估更新失败: %v", snapshot.ChannelId, err))
		return
	}
	attempt.Finished = settled && amount >= 0
	notifyChannelBalancePolicy(attempt.Config, raw)
}

// Unknown asynchronous task charges must not become samples for synchronous
// requests or be presented as final upstream debits at submission time.
func finishChannelBalanceTaskAttempt(ctx *gin.Context, snapshot channelDailyCostSnapshot) {
	finishChannelBalanceAttempt(ctx, snapshot, channelDailyCostEventId(ctx, snapshot.ChannelId), 0, false, true)
}
