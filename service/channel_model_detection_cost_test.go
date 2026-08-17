package service

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionCostTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-cost.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ChannelModelDetectionBatch{},
		&model.ChannelModelDetectionRun{},
		&model.ChannelModelDetectionExecution{},
		&model.ChannelModelDetectionCostEvent{},
		&model.ChannelDailyCost{},
	))
	t.Cleanup(func() {
		sqlDB, closeErr := db.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func channelModelDetectionCostSnapshotForTest(ratio string, quotaPerUnit int64) ChannelModelDetectionCostSnapshot {
	return ChannelModelDetectionCostSnapshot{CostRatioCNY: &ratio, QuotaPerUnit: &quotaPerUnit}
}

func channelModelDetectionCostAttemptForTest(eventId string, attemptNo int, snapshot ChannelModelDetectionCostSnapshot) ChannelModelDetectionCostAttemptInput {
	return ChannelModelDetectionCostAttemptInput{
		CostEventId:       eventId,
		RunId:             "run-1",
		TargetId:          11,
		ExecutionId:       21,
		ChannelId:         31,
		RequestModel:      "gpt-5.6",
		ClaimedModel:      model.ChannelModelDetectionClaimedModelSol,
		Preset:            model.ChannelModelDetectionPresetMedium,
		DetectorRequestId: "detector-request-1",
		AttemptNo:         attemptNo,
		RequestId:         "relay-request-1",
		EstimatedQuota:    500_000,
		Snapshot:          snapshot,
		CreatedAt:         100,
	}
}

func TestChannelModelDetectionQuotaUsesAuthoritativePreGroupCostBasis(t *testing.T) {
	ctx := newChannelDailyCostTestContext()

	ordinary := CalculateChannelModelDetectionQuota(ctx, &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			ModelRatio:      2,
			CompletionRatio: 4,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 3},
		},
	}, &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 10,
		TotalTokens:      110,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 50,
		},
	})
	require.True(t, ordinary.Reliable)
	assert.Equal(t, int64(570), ordinary.SettledQuota)
	assert.Equal(t, int64(190), ordinary.CostBasisQuota)

	tieredExpr := `tier("base", p * 2000)`
	tiered := CalculateChannelModelDetectionQuota(ctx, &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 3},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   tieredExpr,
			ExprHash:     billingexpr.ExprHashString(tieredExpr),
			GroupRatio:   3,
			QuotaPerUnit: common.QuotaPerUnit,
			ExprVersion:  1,
		},
		BillingRequestInput: &billingexpr.RequestInput{},
	}, &dto.Usage{PromptTokens: 100, TotalTokens: 100})
	require.True(t, tiered.Reliable)
	assert.Equal(t, int64(300_000), tiered.SettledQuota)
	assert.Equal(t, int64(100_000), tiered.CostBasisQuota)
}

func TestChannelModelDetectionQuotaKeepsFrozenQuotaPerUnit(t *testing.T) {
	ctx := newChannelDailyCostTestContext()
	quotaPerUnit := int64(500_000)
	snapshot := ChannelModelDetectionCostSnapshot{QuotaPerUnit: &quotaPerUnit}
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 1_000_000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	result := CalculateChannelModelDetectionQuotaWithSnapshot(ctx, &relaycommon.RelayInfo{
		PriceData: types.PriceData{
			ModelPrice:     0.01,
			UsePrice:       true,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}, &dto.Usage{}, snapshot)

	require.True(t, result.Reliable)
	assert.Equal(t, int64(5_000), result.SettledQuota)
	assert.Equal(t, int64(5_000), result.CostBasisQuota)
}

func TestChannelModelDetectionRequestQuotaExcludesPreviewSafetyMargin(t *testing.T) {
	quotaPerUnit := int64(500_000)
	snapshot := ChannelModelDetectionCostSnapshot{QuotaPerUnit: &quotaPerUnit}
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{
		ModelPrice:     0.01,
		UsePrice:       true,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}}

	requestQuota, requestKnown := CalculateChannelModelDetectionRequestQuota(info, 0, snapshot)
	estimatedQuota, estimateKnown := EstimateChannelModelDetectionQuota(info, 0, snapshot)

	require.True(t, requestKnown)
	require.True(t, estimateKnown)
	assert.Equal(t, int64(5_000), requestQuota)
	assert.Equal(t, int64(5_250), estimatedQuota)
}

func TestAlignChannelModelDetectionCostSnapshotUsesTieredBillingQuotaUnit(t *testing.T) {
	costQuotaPerUnit := int64(1_000_000)
	snapshot, err := AlignChannelModelDetectionCostSnapshot(&relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{QuotaPerUnit: 500_000},
	}, ChannelModelDetectionCostSnapshot{QuotaPerUnit: &costQuotaPerUnit})

	require.NoError(t, err)
	require.NotNil(t, snapshot.QuotaPerUnit)
	assert.Equal(t, int64(500_000), *snapshot.QuotaPerUnit)
}

func TestChannelModelDetectionCostConversionUsesDecimalRoundingAndRejectsInvalidValues(t *testing.T) {
	settled, err := CalculateChannelModelDetectionSettledCostNanoCNY(1, "0.25", 3)
	require.NoError(t, err)
	assert.Equal(t, int64(83_333_333), settled)

	unresolved, err := CalculateChannelModelDetectionUnresolvedCostNanoCNY(1, "0.25", 3)
	require.NoError(t, err)
	assert.Equal(t, int64(83_333_334), unresolved)

	zero, err := CalculateChannelModelDetectionSettledCostNanoCNY(0, "0", 500_000)
	require.NoError(t, err)
	assert.Zero(t, zero)

	invalid := []struct {
		quota        int64
		ratio        string
		quotaPerUnit int64
	}{
		{-1, "1", 500_000},
		{1, "-0.1", 500_000},
		{1, "NaN", 500_000},
		{1, "Inf", 500_000},
		{1, "1", 0},
	}
	for _, test := range invalid {
		_, err := CalculateChannelModelDetectionSettledCostNanoCNY(test.quota, test.ratio, test.quotaPerUnit)
		assert.ErrorIs(t, err, model.ErrChannelModelDetectionInvalidCost)
	}

	_, err = CalculateChannelModelDetectionSettledCostNanoCNY(math.MaxInt64, "1000000", 1)
	assert.ErrorIs(t, err, ErrChannelModelDetectionCostOverflow)
}

func TestChannelModelDetectionCostPrepareIsIdempotentAndFreezesUnknownCost(t *testing.T) {
	db := setupChannelModelDetectionCostTest(t)
	ctx := context.Background()
	known := channelModelDetectionCostAttemptForTest("cost-known", 1, channelModelDetectionCostSnapshotForTest("0.8", 500_000))

	event, created, err := PrepareChannelModelDetectionCostEvent(ctx, db, known)
	require.NoError(t, err)
	assert.True(t, created)
	require.NotNil(t, event.EstimatedCostNanoCNY)
	assert.Equal(t, int64(800_000_000), *event.EstimatedCostNanoCNY)
	assert.Equal(t, model.ChannelModelDetectionDispatchPrepared, event.DispatchState)
	assert.Equal(t, model.ChannelModelDetectionSettlementPending, event.SettlementStatus)

	replayed, created, err := PrepareChannelModelDetectionCostEvent(ctx, db, known)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, event.Id, replayed.Id)

	conflict := known
	conflict.EstimatedQuota++
	_, _, err = PrepareChannelModelDetectionCostEvent(ctx, db, conflict)
	assert.ErrorIs(t, err, ErrChannelModelDetectionCostConflict)

	attemptConflict := known
	attemptConflict.CostEventId = "different-event-id"
	_, _, err = PrepareChannelModelDetectionCostEvent(ctx, db, attemptConflict)
	assert.ErrorIs(t, err, ErrChannelModelDetectionCostConflict)

	unknown := channelModelDetectionCostAttemptForTest("cost-unknown", 2, ChannelModelDetectionCostSnapshot{})
	unknown.DetectorRequestId = "detector-request-2"
	unknownEvent, created, err := PrepareChannelModelDetectionCostEvent(ctx, db, unknown)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Nil(t, unknownEvent.EstimatedCostNanoCNY)
	assert.Nil(t, unknownEvent.CostRatioCNY)
	assert.Nil(t, unknownEvent.QuotaPerUnit)

	var dailyCostRows int64
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Count(&dailyCostRows).Error)
	assert.Zero(t, dailyCostRows)
}

func TestChannelModelDetectionCostTransportBoundaryAndSettlementAreMonotonic(t *testing.T) {
	db := setupChannelModelDetectionCostTest(t)
	ctx := context.Background()
	snapshot := channelModelDetectionCostSnapshotForTest("0.8", 500_000)

	notStartedInput := channelModelDetectionCostAttemptForTest("cost-not-started", 1, snapshot)
	_, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, notStartedInput)
	require.NoError(t, err)
	notStarted, err := MarkChannelModelDetectionCostEventNotStarted(ctx, db, notStartedInput.CostEventId, 101)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionDispatchNotStarted, notStarted.DispatchState)
	assert.Equal(t, model.ChannelModelDetectionSettlementNotApplicable, notStarted.SettlementStatus)

	replayedNotStarted, err := MarkChannelModelDetectionCostEventNotStarted(ctx, db, notStartedInput.CostEventId, 102)
	require.NoError(t, err)
	assert.Equal(t, notStarted.Id, replayedNotStarted.Id)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, notStartedInput.CostEventId, 103)
	assert.ErrorIs(t, err, ErrChannelModelDetectionCostConflict)

	settledInput := channelModelDetectionCostAttemptForTest("cost-settled", 2, snapshot)
	settledInput.DetectorRequestId = "detector-request-2"
	_, _, err = PrepareChannelModelDetectionCostEvent(ctx, db, settledInput)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, settledInput.CostEventId, 104)
	require.NoError(t, err)

	settlement := ChannelModelDetectionCostSettlementInput{
		CostEventId:       settledInput.CostEventId,
		SettledQuota:      500_000,
		CostBasisQuota:    250_000,
		InputTokens:       10,
		OutputTokens:      20,
		TotalTokens:       30,
		UpstreamRequestId: "upstream-1",
		SettledAt:         105,
	}
	settled, err := SettleChannelModelDetectionCostEvent(ctx, db, settlement)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionSettlementSettled, settled.SettlementStatus)
	require.NotNil(t, settled.SettledCostNanoCNY)
	assert.Equal(t, int64(400_000_000), *settled.SettledCostNanoCNY)

	replayedSettlement, err := SettleChannelModelDetectionCostEvent(ctx, db, settlement)
	require.NoError(t, err)
	assert.Equal(t, settled.Id, replayedSettlement.Id)
	var dailyCost model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", settledInput.ChannelId).First(&dailyCost).Error)
	assert.Equal(t, int64(400_000_000), dailyCost.CostNanoCNY)
	assert.Equal(t, int64(400_000_000), dailyCost.ModelDetectionCostNanoCNY)
	assert.Equal(t, int64(1), dailyCost.SettledCount)
	assert.Zero(t, dailyCost.UnresolvedCount)

	settlement.TotalTokens++
	_, err = SettleChannelModelDetectionCostEvent(ctx, db, settlement)
	assert.ErrorIs(t, err, ErrChannelModelDetectionCostConflict)
	_, err = MarkChannelModelDetectionCostEventUnresolved(ctx, db, ChannelModelDetectionCostUnresolvedInput{CostEventId: settledInput.CostEventId})
	assert.ErrorIs(t, err, ErrChannelModelDetectionCostConflict)
}

func TestChannelModelDetectionSettlementUsesOriginalDayAndDefaultsSettlementTimestamp(t *testing.T) {
	db := setupChannelModelDetectionCostTest(t)
	ctx := context.Background()
	input := channelModelDetectionCostAttemptForTest("cost-default-settled-at", 1, channelModelDetectionCostSnapshotForTest("0.8", 500_000))
	_, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, input)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, input.CostEventId, 104)
	require.NoError(t, err)

	settled, err := SettleChannelModelDetectionCostEvent(ctx, db, ChannelModelDetectionCostSettlementInput{
		CostEventId: input.CostEventId, SettledQuota: 500_000, CostBasisQuota: 250_000,
		InputTokens: 10, OutputTokens: 20, TotalTokens: 30,
	})
	require.NoError(t, err)
	assert.Positive(t, settled.SettledAt)

	var dailyCost model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", input.ChannelId).First(&dailyCost).Error)
	assert.Equal(t, model.ChannelDailyCostDayStart(input.CreatedAt), dailyCost.DayStart)
	assert.Equal(t, input.CreatedAt, dailyCost.UpdatedAt)
}

func TestChannelModelDetectionCostUnresolvedKeepsKnownEstimateOrNull(t *testing.T) {
	db := setupChannelModelDetectionCostTest(t)
	ctx := context.Background()

	knownInput := channelModelDetectionCostAttemptForTest("cost-unresolved-known", 1, channelModelDetectionCostSnapshotForTest("0.8", 500_000))
	_, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, knownInput)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, knownInput.CostEventId, 101)
	require.NoError(t, err)
	known, err := MarkChannelModelDetectionCostEventUnresolved(ctx, db, ChannelModelDetectionCostUnresolvedInput{
		CostEventId:           knownInput.CostEventId,
		ErrorCode:             "upstream_timeout",
		SanitizedErrorMessage: "上游请求超时",
		UpdatedAt:             102,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionUsageLocalEstimate, known.UsageSource)
	require.NotNil(t, known.EstimatedCostNanoCNY)
	assert.Equal(t, int64(800_000_000), *known.EstimatedCostNanoCNY)

	replayedKnown, err := MarkChannelModelDetectionCostEventUnresolved(ctx, db, ChannelModelDetectionCostUnresolvedInput{
		CostEventId:           knownInput.CostEventId,
		ErrorCode:             "upstream_timeout",
		SanitizedErrorMessage: "上游请求超时",
		UpdatedAt:             103,
	})
	require.NoError(t, err)
	assert.Equal(t, known.Id, replayedKnown.Id)

	unknownInput := channelModelDetectionCostAttemptForTest("cost-unresolved-unknown", 2, ChannelModelDetectionCostSnapshot{})
	unknownInput.DetectorRequestId = "detector-request-2"
	_, _, err = PrepareChannelModelDetectionCostEvent(ctx, db, unknownInput)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, unknownInput.CostEventId, 104)
	require.NoError(t, err)
	unknown, err := MarkChannelModelDetectionCostEventUnresolved(ctx, db, ChannelModelDetectionCostUnresolvedInput{CostEventId: unknownInput.CostEventId, UpdatedAt: 105})
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionUsageLocalEstimate, unknown.UsageSource)
	assert.Nil(t, unknown.EstimatedCostNanoCNY)

	aggregate, err := AggregateChannelModelDetectionCostEvents(ctx, db, ChannelModelDetectionCostFilter{RunId: "run-1"})
	require.NoError(t, err)
	assert.Equal(t, ChannelModelDetectionCostStatusUnresolved, aggregate.Status)
	assert.Equal(t, int64(2), aggregate.UnresolvedRequestCount)
	assert.Equal(t, int64(1), aggregate.UnresolvedCostUnknownCount)
	require.NotNil(t, aggregate.UnresolvedCostNanoCNY)
	assert.Equal(t, int64(800_000_000), *aggregate.UnresolvedCostNanoCNY)
	assert.Equal(t, int64(1_000_000), aggregate.EstimatedQuota)
	require.NotNil(t, aggregate.EstimatedCostNanoCNY)
	assert.Equal(t, int64(800_000_000), *aggregate.EstimatedCostNanoCNY)
	assert.Equal(t, int64(1), aggregate.CostEstimateUnknownCount)
}

func TestChannelModelDetectionCostMissingSnapshotFallsBackToUnresolvedAndCanReconcile(t *testing.T) {
	db := setupChannelModelDetectionCostTest(t)
	ctx := context.Background()
	input := channelModelDetectionCostAttemptForTest("cost-reconcile", 1, ChannelModelDetectionCostSnapshot{})
	_, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, input)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, input.CostEventId, 101)
	require.NoError(t, err)

	unresolved, err := SettleChannelModelDetectionCostEvent(ctx, db, ChannelModelDetectionCostSettlementInput{
		CostEventId:       input.CostEventId,
		SettledQuota:      400,
		CostBasisQuota:    300,
		InputTokens:       10,
		OutputTokens:      5,
		TotalTokens:       15,
		UpstreamRequestId: "upstream-1",
		SettledAt:         102,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionSettlementUnresolved, unresolved.SettlementStatus)
	assert.Equal(t, model.ChannelModelDetectionUsageUpstreamAuthoritative, unresolved.UsageSource)
	assert.True(t, unresolved.UsageAvailable)
	assert.Nil(t, unresolved.SettledCostNanoCNY)
	assert.Equal(t, channelModelDetectionCostErrorSnapshotUnavailable, unresolved.ErrorCode)

	ratio := "0.8"
	quotaPerUnit := int64(500_000)
	require.NoError(t, db.Model(&model.ChannelModelDetectionCostEvent{}).
		Where("cost_event_id = ?", input.CostEventId).
		Updates(map[string]any{"cost_ratio_cny": ratio, "quota_per_unit": quotaPerUnit}).Error)

	settled, err := SettleChannelModelDetectionCostEvent(ctx, db, ChannelModelDetectionCostSettlementInput{
		CostEventId:       input.CostEventId,
		SettledQuota:      400,
		CostBasisQuota:    300,
		InputTokens:       10,
		OutputTokens:      5,
		TotalTokens:       15,
		UpstreamRequestId: "upstream-1",
		SettledAt:         103,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionSettlementSettled, settled.SettlementStatus)
	require.NotNil(t, settled.SettledCostNanoCNY)
	assert.Equal(t, int64(480_000), *settled.SettledCostNanoCNY)
}

func TestChannelModelDetectionCostSettlesExplicitLocalEstimateWithoutClaimingAuthoritativeUsage(t *testing.T) {
	db := setupChannelModelDetectionCostTest(t)
	ctx := context.Background()
	input := channelModelDetectionCostAttemptForTest("cost-local-estimate", 1, channelModelDetectionCostSnapshotForTest("0.8", 500_000))
	_, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, input)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, input.CostEventId, 101)
	require.NoError(t, err)

	settled, err := SettleChannelModelDetectionCostEvent(ctx, db, ChannelModelDetectionCostSettlementInput{
		CostEventId:    input.CostEventId,
		SettledQuota:   400,
		CostBasisQuota: 300,
		InputTokens:    10,
		OutputTokens:   5,
		TotalTokens:    15,
		UsageSource:    model.ChannelModelDetectionUsageLocalEstimate,
		UsageAvailable: false,
		SettledAt:      102,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionSettlementSettled, settled.SettlementStatus)
	assert.Equal(t, model.ChannelModelDetectionUsageLocalEstimate, settled.UsageSource)
	assert.False(t, settled.UsageAvailable)
	require.NotNil(t, settled.SettledCostNanoCNY)
}

func TestChannelModelDetectionCostAggregationIsReplaySafeAndDoesNotDuplicateDailyCost(t *testing.T) {
	db := setupChannelModelDetectionCostTest(t)
	ctx := context.Background()
	batchId := "batch-1"
	require.NoError(t, db.Create(&model.ChannelModelDetectionBatch{
		BatchId: batchId, GlobalConfigRevision: 1, Preset: model.ChannelModelDetectionPresetMedium,
		ScheduledFor: 100, Status: model.ChannelModelDetectionBatchStatusRunning,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelModelDetectionRun{
		RunId: "run-1", BatchId: &batchId, ChannelId: 31, Trigger: model.ChannelModelDetectionTriggerScheduled,
		Preset: model.ChannelModelDetectionPresetMedium, PresetSource: model.ChannelModelDetectionPresetSourceScheduledDefault,
		Status: model.ChannelModelDetectionRunStatusRunning,
	}).Error)
	execution := model.ChannelModelDetectionExecution{
		RunId: "run-1", TargetKey: "target-1", TargetId: 11, ChannelId: 31,
		RequestModel: "gpt-5.6", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
		Preset: model.ChannelModelDetectionPresetMedium, Status: model.ChannelModelDetectionExecutionStatusRunning,
	}
	require.NoError(t, db.Create(&execution).Error)

	snapshot := channelModelDetectionCostSnapshotForTest("0.8", 500_000)
	settledInput := channelModelDetectionCostAttemptForTest("cost-settled", 1, snapshot)
	settledInput.ExecutionId = execution.Id
	_, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, settledInput)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, settledInput.CostEventId, 101)
	require.NoError(t, err)
	_, err = SettleChannelModelDetectionCostEvent(ctx, db, ChannelModelDetectionCostSettlementInput{
		CostEventId: settledInput.CostEventId, SettledQuota: 500_000, CostBasisQuota: 250_000,
		InputTokens: 10, OutputTokens: 20, TotalTokens: 30, SettledAt: 102,
	})
	require.NoError(t, err)

	unresolvedInput := channelModelDetectionCostAttemptForTest("cost-unresolved", 2, snapshot)
	unresolvedInput.DetectorRequestId = "detector-request-2"
	unresolvedInput.ExecutionId = execution.Id
	_, _, err = PrepareChannelModelDetectionCostEvent(ctx, db, unresolvedInput)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, unresolvedInput.CostEventId, 103)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventUnresolved(ctx, db, ChannelModelDetectionCostUnresolvedInput{CostEventId: unresolvedInput.CostEventId, UpdatedAt: 104})
	require.NoError(t, err)

	notStartedInput := channelModelDetectionCostAttemptForTest("cost-not-started", 3, snapshot)
	notStartedInput.DetectorRequestId = "detector-request-3"
	notStartedInput.ExecutionId = execution.Id
	_, _, err = PrepareChannelModelDetectionCostEvent(ctx, db, notStartedInput)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventNotStarted(ctx, db, notStartedInput.CostEventId, 105)
	require.NoError(t, err)

	aggregate, err := RebuildChannelModelDetectionExecutionCost(ctx, db, execution.Id)
	require.NoError(t, err)
	assert.Equal(t, ChannelModelDetectionCostStatusPartial, aggregate.Status)
	assert.Equal(t, int64(1), aggregate.SettledRequestCount)
	assert.Equal(t, int64(1), aggregate.UnresolvedRequestCount)
	assert.Equal(t, int64(1), aggregate.NotStartedRequestCount)
	assert.Equal(t, int64(500_000), aggregate.SettledQuota)
	assert.Equal(t, int64(250_000), aggregate.CostBasisQuota)
	assert.False(t, aggregate.UsageAvailable)
	require.NotNil(t, aggregate.SettledCostNanoCNY)
	assert.Equal(t, int64(400_000_000), *aggregate.SettledCostNanoCNY)
	require.NotNil(t, aggregate.UnresolvedCostNanoCNY)
	assert.Equal(t, int64(800_000_000), *aggregate.UnresolvedCostNanoCNY)
	assert.Equal(t, int64(1_000_000), aggregate.EstimatedQuota)
	require.NotNil(t, aggregate.EstimatedCostNanoCNY)
	assert.Equal(t, int64(1_600_000_000), *aggregate.EstimatedCostNanoCNY)

	firstRun, err := RebuildChannelModelDetectionRunCost(ctx, db, "run-1")
	require.NoError(t, err)
	secondRun, err := RebuildChannelModelDetectionRunCost(ctx, db, "run-1")
	require.NoError(t, err)
	assert.Equal(t, firstRun, secondRun)

	firstBatch, err := RebuildChannelModelDetectionBatchCost(ctx, db, batchId)
	require.NoError(t, err)
	secondBatch, err := RebuildChannelModelDetectionBatchCost(ctx, db, batchId)
	require.NoError(t, err)
	assert.Equal(t, firstBatch, secondBatch)

	var run model.ChannelModelDetectionRun
	require.NoError(t, db.Where("run_id = ?", "run-1").First(&run).Error)
	assert.Equal(t, int64(1), run.SettledRequestCount)
	assert.Equal(t, int64(1), run.UnresolvedRequestCount)
	assert.Equal(t, int64(1_000_000), run.EstimatedQuota)
	require.NotNil(t, run.EstimatedCostNanoCNY)
	assert.Equal(t, int64(1_600_000_000), *run.EstimatedCostNanoCNY)
	assert.Zero(t, run.CostEstimateUnknownCount)
	assert.Equal(t, int64(400_000_000), *run.SettledCostNanoCNY)
	assert.Equal(t, int64(800_000_000), *run.UnresolvedCostNanoCNY)

	var storedExecution model.ChannelModelDetectionExecution
	require.NoError(t, db.First(&storedExecution, execution.Id).Error)
	assert.Equal(t, int64(1_000_000), storedExecution.EstimatedQuota)
	require.NotNil(t, storedExecution.EstimatedCostNanoCNY)
	assert.Equal(t, int64(1_600_000_000), *storedExecution.EstimatedCostNanoCNY)

	var batch model.ChannelModelDetectionBatch
	require.NoError(t, db.Where("batch_id = ?", batchId).First(&batch).Error)
	assert.Equal(t, int64(1_000_000), batch.EstimatedQuota)
	require.NotNil(t, batch.EstimatedCostNanoCNY)
	assert.Equal(t, int64(1_600_000_000), *batch.EstimatedCostNanoCNY)

	var dailyCosts []model.ChannelDailyCost
	require.NoError(t, db.Order("id ASC").Find(&dailyCosts).Error)
	require.Len(t, dailyCosts, 1)
	assert.Equal(t, int64(400_000_000), dailyCosts[0].CostNanoCNY)
	assert.Equal(t, int64(400_000_000), dailyCosts[0].ModelDetectionCostNanoCNY)
	assert.Equal(t, int64(1), dailyCosts[0].SettledCount)
	assert.Equal(t, int64(1), dailyCosts[0].UnresolvedCount)
}

func TestChannelModelDetectionCostAggregationRejectsOverflow(t *testing.T) {
	max := int64(math.MaxInt64)
	events := []model.ChannelModelDetectionCostEvent{
		{
			CostEventId: "one", RunId: "run", TargetId: 1, ExecutionId: 1, ChannelId: 1,
			RequestModel: "model", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
			Preset: model.ChannelModelDetectionPresetLow, DetectorRequestId: "request-1", AttemptNo: 1,
			DispatchState: model.ChannelModelDetectionDispatchDispatched, SettlementStatus: model.ChannelModelDetectionSettlementUnresolved,
			UsageSource: model.ChannelModelDetectionUsageLocalEstimate, EstimatedQuota: max,
			EstimatedCostNanoCNY: &max, CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI,
		},
		{
			CostEventId: "two", RunId: "run", TargetId: 1, ExecutionId: 1, ChannelId: 1,
			RequestModel: "model", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
			Preset: model.ChannelModelDetectionPresetLow, DetectorRequestId: "request-2", AttemptNo: 1,
			DispatchState: model.ChannelModelDetectionDispatchDispatched, SettlementStatus: model.ChannelModelDetectionSettlementUnresolved,
			UsageSource: model.ChannelModelDetectionUsageLocalEstimate, EstimatedQuota: 1,
			EstimatedCostNanoCNY: func() *int64 { value := int64(1); return &value }(), CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI,
		},
	}

	_, err := aggregateChannelModelDetectionCostEventList(events)
	assert.True(t, errors.Is(err, ErrChannelModelDetectionCostOverflow))
}

func TestChannelModelDetectionCostFormatPreservesNullAndNineDecimals(t *testing.T) {
	assert.Nil(t, FormatChannelModelDetectionCostCNY(nil))
	negative := int64(-1)
	assert.Nil(t, FormatChannelModelDetectionCostCNY(&negative))
	cost := int64(25_680_000)
	formatted := FormatChannelModelDetectionCostCNY(&cost)
	require.NotNil(t, formatted)
	assert.Equal(t, "0.025680000", *formatted)
}

func TestChannelModelDetectionCostAggregationPreservesZeroTokenAuthoritativeUsage(t *testing.T) {
	zero := int64(0)
	events := []model.ChannelModelDetectionCostEvent{{
		CostEventId: "zero-usage", RunId: "run", TargetId: 1, ExecutionId: 1, ChannelId: 1,
		RequestModel: "model", ClaimedModel: model.ChannelModelDetectionClaimedModelSol,
		Preset: model.ChannelModelDetectionPresetLow, DetectorRequestId: "request", AttemptNo: 1,
		DispatchState: model.ChannelModelDetectionDispatchDispatched, SettlementStatus: model.ChannelModelDetectionSettlementSettled,
		UsageSource: model.ChannelModelDetectionUsageUpstreamAuthoritative, UsageAvailable: true,
		SettledQuota: &zero, CostBasisQuota: &zero, SettledCostNanoCNY: &zero,
		CostScope: model.ChannelModelDetectionCostScopeChannelUpstreamAPI,
	}}

	aggregate, err := aggregateChannelModelDetectionCostEventList(events)
	require.NoError(t, err)
	assert.True(t, aggregate.UsageAvailable)
	assert.Zero(t, aggregate.TotalTokens)
}
