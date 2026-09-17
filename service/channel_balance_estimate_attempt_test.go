package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelBalanceRealUsageCacheCostsTrainTheFrozenAverage(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	// Only the existing real-accounting writer is replaced. The entire balance
	// lifecycle runs with model.DB=nil, including dispatch, usage and samples.
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{MaxPending: 64, MaxBatchSize: 16},
		func(context.Context, []model.ChannelDailyCostDelta) error { return nil })
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})
	snapshot := channelDailyCostSnapshot{ChannelId: f.config.ChannelID, BalanceConfig: f.config,
		CostRatioCNY: 0.1088, ConversionFactor: 0.0272, QuotaPerUnit: 500_000, Configured: true}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: f.config.ChannelID},
		OriginModelName: "cache-model", Request: &dto.GeneralOpenAIRequest{MaxTokens: common.GetPointer(uint(10))},
		PriceData: types.PriceData{ModelRatio: 2, CompletionRatio: 4, CacheRatio: 0.1, CacheCreationRatio: 1.25,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 77}}}
	info.SetEstimatePromptTokens(100)
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110,
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 50, CachedCreationTokens: 20}}
	for n := 0; n < 5; n++ {
		ctx := newChannelDailyCostTestContext()
		ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
		BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
		PrepareChannelBalanceAttempt(ctx, info)
		MarkChannelDailyCostRequestDispatched(ctx)
		f.advance(time.Millisecond)
		recordTextChannelDailyCost(ctx, info, usage, usage, calculateTextQuotaSummary(ctx, info, usage), false, nil)
		f.advance(time.Millisecond)
	}
	ctx := newChannelDailyCostTestContext()
	ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
	BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
	PrepareChannelBalanceAttempt(ctx, info)
	MarkChannelDailyCostRequestDispatched(ctx)
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	// ((100-50-20) + 50*.1 + 20*1.25 + 10*4) * 2 / 500000 * 4
	assert.Equal(t, 0.008, estimate.CompletedConsumption)
	assert.Equal(t, 0.0016, estimate.InFlightConsumption)
	assert.Equal(t, int64(1), estimate.AverageCount)
	assert.Equal(t, int64(5), estimate.LastSampleCount)
	assert.Equal(t, "cache-model", estimate.LastEstimateModel)
	// Changing the group selling price does not change the sample population.
	_, sample, eligible := channelBalanceRequestBudget(ctx, info, snapshot)
	require.True(t, eligible)
	info.PriceData.GroupRatioInfo = types.GroupRatioInfo{GroupRatio: 999, GroupSpecialRatio: 80, HasSpecialRatio: true}
	_, sameSample, _ := channelBalanceRequestBudget(ctx, info, snapshot)
	assert.Equal(t, sample, sameSample)
	snapshot.CostRatioCNY, snapshot.ConversionFactor = 8, 2
	_, newSample, _ := channelBalanceRequestBudget(ctx, info, snapshot)
	assert.NotEqual(t, sample, newSample)
	// The in-flight request retains its captured cost/conversion parameters.
	f.advance(time.Millisecond)
	recordTextChannelDailyCost(ctx, info, usage, usage, calculateTextQuotaSummary(ctx, info, usage), false, nil)
	estimate, err = GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, 0.0096, estimate.CompletedConsumption)
	assert.Zero(t, estimate.InFlightConsumption)
	assert.Nil(t, model.DB)
}

func TestChannelBalanceExpressionBudgetUsesBeforeGroupPrices(t *testing.T) {
	f := newChannelBalanceFixture(t)
	snapshot := channelDailyCostSnapshot{ChannelId: f.config.ChannelID, Configured: true,
		CostRatioCNY: 0.1088, ConversionFactor: 0.0272, QuotaPerUnit: 500_000}
	expression := "p * 4 + c * 16 + cr * 0.4 + cc * 5"
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: f.config.ChannelID},
		OriginModelName: "expr-model", TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			ExprString: expression, EstimatedPromptTokens: 100, EstimatedCompletionTokens: 10,
			EstimatedQuotaBeforeGroup: 280, EstimatedQuotaAfterGroup: 28_000, GroupRatio: 100, QuotaPerUnit: 500_000}}
	budget, sample, eligible := channelBalanceRequestBudget(newChannelDailyCostTestContext(), info, snapshot)
	assert.Equal(t, int64(2240), budget)
	assert.NotEmpty(t, sample)
	assert.True(t, eligible)
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110,
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 50, CachedCreationTokens: 20}}
	result, err := billingexpr.ComputeTieredQuota(info.TieredBillingSnapshot,
		BuildTieredTokenParams(usage, false, billingexpr.UsedVars(expression)))
	require.NoError(t, err)
	cost, valid := calculateChannelDailyCost(snapshot, result.ActualQuotaBeforeGroup)
	require.True(t, valid)
	amount, err := channelBalanceCostMicro(cost, snapshot.ConversionFactor)
	require.NoError(t, err)
	assert.Equal(t, int64(1600), amount)
}

func TestChannelBalanceOutboxRecoveryNeverUsesLedgerDeliveryAsDebitTime(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	ctx := newChannelDailyCostTestContext()
	snapshot := channelDailyCostSnapshot{ChannelId: f.config.ChannelID, BalanceConfig: f.config, ConversionFactor: .0272}
	ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
	BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
	MarkChannelDailyCostRequestDispatched(ctx)
	id := channelDailyCostEventId(ctx, f.config.ChannelID)
	// The real request completed, but its Redis completion write was lost.
	// The next raw snapshot already includes the debit; durable delivery is later.
	f.sync(22)
	row := model.ChannelDailyCostOutbox{EventId: id, ChannelId: f.config.ChannelID, CostNanoCNY: 54_400_000, SettledDelta: 1}
	require.NoError(t, reconcileChannelBalanceCostEvents(t.Context(), f.client, []model.ChannelDailyCostOutbox{row}))
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Zero(t, estimate.InFlightCount)
	assert.Zero(t, estimate.CompletedConsumption, "a late outbox delivery is not another debit")
	assert.False(t, estimate.Complete, "wait for a fresh confirmed snapshot after the gap")
	assert.Equal(t, "unknown", estimate.Decision)
	estimate = f.sync(22)
	assert.True(t, estimate.Complete)
	require.NoError(t, reconcileChannelBalanceCostEvents(t.Context(), f.client, []model.ChannelDailyCostOutbox{row}))
	estimate, err = GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.True(t, estimate.Complete, "replaying an already reconciled event does nothing")
	assert.Equal(t, 22.0, estimate.EstimatedBalance)
	assert.Nil(t, model.DB)
}

func TestChannelBalanceUnknownReservationResolvesOnceBeforeSync(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	f.operation("start", "cancelled", "model", 3_000_000, true, 0)
	f.operation("finish", "cancelled", "model", -1, false, 0)
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.False(t, estimate.Complete)
	assert.Equal(t, int64(1), estimate.UnknownCount)
	assert.Equal(t, 3.0, estimate.InFlightConsumption)
	estimate = f.operation("finish", "cancelled", "model", 2_000_000, true, 0)
	assert.True(t, estimate.Complete)
	assert.Equal(t, 22.0, estimate.EstimatedBalance)
	assert.Zero(t, estimate.InFlightCount)
	// A configuration/ratio revision retains active reservations, while an
	// account change isolates the new upstream balance from the old account.
	f.operation("start", "old-price", "model", 3_000_000, false, 0)
	f.monitor.UpstreamRevision++
	require.NoError(t, ConfigureChannelBalanceEstimate(t.Context(), f.monitor))
	f.config = ChannelBalanceConfigForMonitor(f.monitor)
	assert.Equal(t, 3.0, f.sync(22).InFlightConsumption)
	f.monitor.UpstreamAccount = "different-account"
	f.config = ChannelBalanceConfigForMonitor(f.monitor)
	assert.Zero(t, f.sync(50).InFlightConsumption)
}

func TestChannelBalanceSyncFailureAndLostCoverageCannotEnable(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	f.operation("start", "pending", "model", 3_000_000, false, 0)
	token, err := BeginChannelBalanceSync(t.Context(), f.monitor)
	require.NoError(t, err)
	require.NoError(t, FailChannelBalanceSync(t.Context(), token))
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.False(t, estimate.Complete)
	assert.Equal(t, 21.0, estimate.EstimatedBalance)
	assert.NotEqual(t, "ok", estimate.Decision)
	f.operation("gap", "", "", 0, false, 0)
	f.operation("start", "huge-budget", "model", 100_000_000, false, 0)
	estimate, err = GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, "unknown", estimate.Decision, "incomplete coverage cannot disable solely from a partial estimate")
	// State retention is bounded, but expiry must not look like a free request.
	f.advance(49 * time.Hour)
	estimate, err = GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.False(t, estimate.Available)
	assert.Zero(t, estimate.InFlightCount)
	assert.True(t, f.sync(24).Complete, "a new confirmed idle snapshot recovers expired state")
	assert.Nil(t, model.DB, "all failure paths remain Redis-only")
}

func TestChannelBalanceMissingPriceSnapshotRegistersUnknownWithoutSQL(t *testing.T) {
	f := newChannelBalanceFixture(t)
	f.sync(24)
	ctx := newChannelDailyCostTestContext()
	BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
	MarkChannelDailyCostRequestDispatched(ctx)
	estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
	require.NoError(t, err)
	assert.Equal(t, int64(1), estimate.InFlightCount)
	assert.Equal(t, int64(1), estimate.UnknownCount)
	assert.False(t, estimate.Complete)
	assert.Nil(t, model.DB)
}
