package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelBalanceResponsesTerminalRetiresOnlyKnownGeneration(t *testing.T) {
	for _, tc := range []struct {
		name, terminal string
		mode           int
		settled, done  bool
		wantFinished   bool
	}{
		{"completed", "completed", relayconstant.RelayModeResponses, true, true, true},
		{"incomplete", "incomplete", relayconstant.RelayModeResponses, true, true, true},
		{"cancelled", "cancelled", relayconstant.RelayModeResponses, true, true, true},
		{"failed with usage", "failed", relayconstant.RelayModeResponses, true, true, true},
		{"missing usage", "completed", relayconstant.RelayModeResponses, false, true, false},
		{"disconnected", "", relayconstant.RelayModeResponses, true, false, false},
		{"realtime incremental usage", "completed", relayconstant.RelayModeRealtime, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChannelBalanceFixture(t)
			f.sync(24)
			ctx := newChannelDailyCostTestContext()
			snapshot := channelDailyCostSnapshot{ChannelId: f.config.ChannelID, BalanceConfig: f.config, ConversionFactor: 1}
			ctx.Set(channelDailyCostSnapshotContextKey, snapshot)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: f.config.ChannelID},
				RelayMode: tc.mode, ClientWs: &websocket.Conn{}, StreamStatus: relaycommon.NewStreamStatus()}
			BeginChannelDailyCostAttempt(ctx, f.config.ChannelID)
			PrepareChannelBalanceAttempt(ctx, info)
			MarkChannelDailyCostRequestDispatched(ctx)
			f.advance(time.Millisecond)
			switch tc.terminal {
			case "completed":
				info.StreamStatus.MarkCompleted()
			case "incomplete":
				info.StreamStatus.MarkIncomplete("max_output_tokens")
			case "cancelled":
				info.StreamStatus.MarkCancelled()
			case "failed":
				info.StreamStatus.MarkFailed("upstream", "server_error", 500)
			}
			if tc.done {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, context.Canceled)
			}
			eventID := channelDailyCostEventId(ctx, f.config.ChannelID)
			finishChannelBalanceAttempt(ctx, snapshot, eventID, 2_000_000_000, tc.settled, false)
			// Repeated finalization may not deduct twice or retire an uncertain job.
			finishChannelBalanceAttempt(ctx, snapshot, eventID, 2_000_000_000, tc.settled, false)
			estimate, err := GetChannelBalanceEstimate(t.Context(), f.config)
			require.NoError(t, err)
			if tc.wantFinished {
				assert.Zero(t, estimate.InFlightCount)
				assert.Zero(t, estimate.UnknownCount)
				assert.Equal(t, 2.0, estimate.CompletedConsumption)
				assert.Equal(t, 22.0, estimate.EstimatedBalance)
			} else {
				assert.Equal(t, int64(1), estimate.InFlightCount)
				assert.Equal(t, int64(1), estimate.UnknownCount)
				assert.Zero(t, estimate.CompletedConsumption)
				assert.False(t, estimate.Complete)
			}
			assert.Zero(t, estimate.AverageCount)
			assert.Nil(t, model.DB, "WebSocket completion cannot require a SQL query")
		})
	}
}

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
		OriginModelName: "expr-model", Request: &dto.GeneralOpenAIRequest{MaxTokens: common.GetPointer(uint(10))},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			ExprString: expression, EstimatedPromptTokens: 100, EstimatedCompletionTokens: 0,
			EstimatedQuotaBeforeGroup: 200, EstimatedQuotaAfterGroup: 20_000, GroupRatio: 100, QuotaPerUnit: 500_000}}
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

func TestChannelBalanceExpressionBudgetIndependentOfUserReservation(t *testing.T) {
	snapshot := channelDailyCostSnapshot{Configured: true,
		CostRatioCNY: 0.1088, ConversionFactor: 0.0272, QuotaPerUnit: 500_000}
	for _, tc := range []struct {
		name, expression string
		multiplier       float64
		reservation      float64
		prompt, output   int
		mode             int
		imageCount       *int
		want             int64
	}{
		{name: "half input reservation", expression: "p * 4 + c * 16", multiplier: 0.5, reservation: 100, prompt: 100, output: 10, want: 2240},
		{name: "larger input reservation", expression: "p * 4 + c * 16", multiplier: 2.5, reservation: 500, prompt: 100, output: 10, want: 2240},
		{name: "output only is not free", expression: "c * 16", multiplier: 1, prompt: 100, output: 10, want: 640},
		{name: "omitted output limit", expression: "p * 4 + c * 16", multiplier: 1, reservation: 200, prompt: 100, want: 525888},
		{name: "embedding has no output budget", expression: "p * 4 + c * 16", multiplier: 1, reservation: 200, prompt: 100, mode: relayconstant.RelayModeEmbeddings, want: 1600},
		{name: "rerank has no output budget", expression: "p * 4 + c * 16", multiplier: 1, reservation: 200, prompt: 100, mode: relayconstant.RelayModeRerank, want: 1600},
		{name: "outbound image quantity", expression: `tier("image", fixed(0.01)) * image_count`, multiplier: 2.5, reservation: 15000, prompt: 100, imageCount: common.GetPointer(3), want: 120000},
		{name: "frozen request rule", expression: `tier("base", p * 4 + c * 16) * (param("service_tier") == "fast" ? 2 : 1)`, multiplier: 0.5, reservation: 200, prompt: 100, output: 10, want: 4480},
		{name: "invalid expression remains unknown", expression: "invalid(", multiplier: 1, prompt: 100, output: 10, want: -1},
		{name: "overflow remains unknown", expression: "p * 1e100", multiplier: 1, prompt: 100, output: 10, want: -1},
		{name: "invalid prompt remains unknown", expression: "p * 4 + c * 16", multiplier: 1, prompt: -1, output: 10, want: -1},
		{name: "unbounded output remains unknown", expression: "p * 4 + c * 16", multiplier: 1, reservation: 200, prompt: 100, output: common.MaxQuota, want: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{}
			if tc.output > 0 {
				request.MaxTokens = common.GetPointer(uint(tc.output))
			}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, OriginModelName: "expr-model", Request: request, RelayMode: tc.mode,
				BillingRequestInput: &billingexpr.RequestInput{Body: []byte(`{"service_tier":"fast","n":1}`), ImageCount: common.GetPointer(1)},
				TieredBillingSnapshot: &billingexpr.BillingSnapshot{
					ExprString: tc.expression, EstimatedPromptTokens: tc.prompt,
					EstimatedImageCount: tc.imageCount, PreConsumeMultiplier: tc.multiplier,
					EstimatedQuotaBeforeGroup: tc.reservation,
					GroupRatio:                100, QuotaPerUnit: 500_000}}
			original := *info.TieredBillingSnapshot
			budget, _, _ := channelBalanceRequestBudget(newChannelDailyCostTestContext(), info, snapshot)
			assert.Equal(t, tc.want, budget)
			assert.Equal(t, original, *info.TieredBillingSnapshot, "balance protection must not change the user's billing snapshot")
			assert.Equal(t, 1, *info.BillingRequestInput.ImageCount, "outbound quantity must not rewrite frozen request input")
		})
	}
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
