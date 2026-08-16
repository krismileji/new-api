package service

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelModelDetectionDailyLedgerTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-model-detection-daily-ledger.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
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

func channelModelDetectionDailyLedgerAttempt(eventID string, channelID int, createdAt int64) ChannelModelDetectionCostAttemptInput {
	ratio := "0.8"
	quotaPerUnit := int64(500_000)
	return ChannelModelDetectionCostAttemptInput{
		CostEventId:       eventID,
		RunId:             "run-" + eventID,
		TargetId:          101,
		ExecutionId:       201,
		ChannelId:         channelID,
		RequestModel:      "gpt-5.6-sol",
		ClaimedModel:      model.ChannelModelDetectionClaimedModelSol,
		Preset:            model.ChannelModelDetectionPresetLow,
		DetectorRequestId: "request-" + eventID,
		AttemptNo:         1,
		EstimatedQuota:    250_000,
		Snapshot: ChannelModelDetectionCostSnapshot{
			CostRatioCNY: &ratio,
			QuotaPerUnit: &quotaPerUnit,
		},
		CreatedAt: createdAt,
	}
}

func channelModelDetectionDailyLedgerSettlement(eventID string, settledAt int64) ChannelModelDetectionCostSettlementInput {
	return ChannelModelDetectionCostSettlementInput{
		CostEventId:       eventID,
		SettledQuota:      250_000,
		CostBasisQuota:    250_000,
		InputTokens:       2,
		OutputTokens:      1,
		TotalTokens:       3,
		UsageSource:       model.ChannelModelDetectionUsageUpstreamAuthoritative,
		UsageAvailable:    true,
		UpstreamRequestId: "upstream-" + eventID,
		SettledAt:         settledAt,
	}
}

func markChannelModelDetectionDailyLedgerUnresolved(t *testing.T, db *gorm.DB, input ChannelModelDetectionCostSettlementInput, updatedAt int64) {
	t.Helper()
	settledQuota := input.SettledQuota
	costBasisQuota := input.CostBasisQuota
	_, err := MarkChannelModelDetectionCostEventUnresolved(context.Background(), db, ChannelModelDetectionCostUnresolvedInput{
		CostEventId:           input.CostEventId,
		UsageSource:           input.UsageSource,
		UsageAvailable:        input.UsageAvailable,
		InputTokens:           input.InputTokens,
		OutputTokens:          input.OutputTokens,
		TotalTokens:           input.TotalTokens,
		SettledQuota:          &settledQuota,
		CostBasisQuota:        &costBasisQuota,
		UpstreamRequestId:     input.UpstreamRequestId,
		ErrorCode:             "temporary_cost_failure",
		SanitizedErrorMessage: "模型检测成本暂不可结算",
		UpdatedAt:             updatedAt,
	})
	require.NoError(t, err)
}

func TestChannelModelDetectionDailyLedgerUnresolvedReplayAndSettlement(t *testing.T) {
	db := setupChannelModelDetectionDailyLedgerTest(t)
	ctx := context.Background()
	createdAt := time.Date(2026, time.August, 17, 15, 59, 50, 0, time.UTC).Unix()
	settledAt := createdAt + 20
	attempt := channelModelDetectionDailyLedgerAttempt("daily-transition", 401, createdAt)
	settlement := channelModelDetectionDailyLedgerSettlement(attempt.CostEventId, settledAt)

	prepared, created, err := PrepareChannelModelDetectionCostEvent(ctx, db, attempt)
	require.NoError(t, err)
	assert.True(t, created)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, prepared.CostEventId, createdAt+1)
	require.NoError(t, err)

	markChannelModelDetectionDailyLedgerUnresolved(t, db, settlement, createdAt+2)
	markChannelModelDetectionDailyLedgerUnresolved(t, db, settlement, createdAt+3)

	var unresolvedDaily model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", attempt.ChannelId).First(&unresolvedDaily).Error)
	assert.Equal(t, model.ChannelDailyCostDayStart(createdAt), unresolvedDaily.DayStart)
	assert.Zero(t, unresolvedDaily.CostNanoCNY)
	assert.Zero(t, unresolvedDaily.ModelDetectionCostNanoCNY)
	assert.Zero(t, unresolvedDaily.SettledCount)
	assert.Equal(t, int64(1), unresolvedDaily.UnresolvedCount)

	settled, err := SettleChannelModelDetectionCostEvent(ctx, db, settlement)
	require.NoError(t, err)
	assert.Equal(t, model.ChannelModelDetectionSettlementSettled, settled.SettlementStatus)
	assert.Equal(t, createdAt, settled.CreatedAt)
	assert.Equal(t, settledAt, settled.SettledAt)

	_, err = SettleChannelModelDetectionCostEvent(ctx, db, settlement)
	require.NoError(t, err)

	var rows []model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", attempt.ChannelId).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, model.ChannelDailyCostDayStart(createdAt), rows[0].DayStart)
	assert.Equal(t, int64(400_000_000), rows[0].CostNanoCNY)
	assert.Equal(t, int64(400_000_000), rows[0].ModelDetectionCostNanoCNY)
	assert.Equal(t, int64(1), rows[0].SettledCount)
	assert.Zero(t, rows[0].UnresolvedCount)
	assert.NotEqual(t, model.ChannelDailyCostDayStart(settledAt), rows[0].DayStart)
}

func TestChannelModelDetectionDailyLedgerDefaultsCreatedAtAndUsesItForDirectSettlement(t *testing.T) {
	db := setupChannelModelDetectionDailyLedgerTest(t)
	ctx := context.Background()

	before := time.Now().Unix()
	defaultAttempt := channelModelDetectionDailyLedgerAttempt("default-created-at", 402, 0)
	defaultPrepared, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, defaultAttempt)
	require.NoError(t, err)
	after := time.Now().Unix()
	assert.GreaterOrEqual(t, defaultPrepared.CreatedAt, before)
	assert.LessOrEqual(t, defaultPrepared.CreatedAt, after)

	createdAt := time.Date(2026, time.August, 18, 15, 59, 50, 0, time.UTC).Unix()
	settledAt := createdAt + 20
	attempt := channelModelDetectionDailyLedgerAttempt("direct-settlement", 403, createdAt)
	prepared, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, attempt)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, prepared.CostEventId, createdAt+1)
	require.NoError(t, err)
	_, err = SettleChannelModelDetectionCostEvent(ctx, db, channelModelDetectionDailyLedgerSettlement(prepared.CostEventId, settledAt))
	require.NoError(t, err)

	var daily model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", attempt.ChannelId).First(&daily).Error)
	assert.Equal(t, model.ChannelDailyCostDayStart(createdAt), daily.DayStart)
	assert.NotEqual(t, model.ChannelDailyCostDayStart(settledAt), daily.DayStart)
	assert.Equal(t, int64(1), daily.SettledCount)
	assert.Zero(t, daily.UnresolvedCount)
}

func TestChannelModelDetectionDailyLedgerSettlementRollsBackOnOverflow(t *testing.T) {
	db := setupChannelModelDetectionDailyLedgerTest(t)
	ctx := context.Background()
	createdAt := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC).Unix()
	attempt := channelModelDetectionDailyLedgerAttempt("rollback-overflow", 404, createdAt)
	settlement := channelModelDetectionDailyLedgerSettlement(attempt.CostEventId, createdAt+10)

	prepared, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, attempt)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, prepared.CostEventId, createdAt+1)
	require.NoError(t, err)
	markChannelModelDetectionDailyLedgerUnresolved(t, db, settlement, createdAt+2)
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).
		Where("channel_id = ?", attempt.ChannelId).
		Updates(map[string]any{
			"cost_nano_cny":                 int64(math.MaxInt64),
			"model_detection_cost_nano_cny": int64(math.MaxInt64),
		}).Error)

	_, err = SettleChannelModelDetectionCostEvent(ctx, db, settlement)
	require.Error(t, err)

	var event model.ChannelModelDetectionCostEvent
	require.NoError(t, db.Where("cost_event_id = ?", attempt.CostEventId).First(&event).Error)
	assert.Equal(t, model.ChannelModelDetectionSettlementUnresolved, event.SettlementStatus)
	assert.Nil(t, event.SettledCostNanoCNY)

	var daily model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", attempt.ChannelId).First(&daily).Error)
	assert.Equal(t, int64(math.MaxInt64), daily.CostNanoCNY)
	assert.Equal(t, int64(math.MaxInt64), daily.ModelDetectionCostNanoCNY)
	assert.Zero(t, daily.SettledCount)
	assert.Equal(t, int64(1), daily.UnresolvedCount)
}

func TestChannelModelDetectionDailyLedgerConcurrentUnresolvedWinnerSettlesExactlyOnce(t *testing.T) {
	db := setupChannelModelDetectionDailyLedgerTest(t)
	ctx := context.Background()
	createdAt := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC).Unix()
	attempt := channelModelDetectionDailyLedgerAttempt("concurrent-unresolved-winner", 405, createdAt)
	settlement := channelModelDetectionDailyLedgerSettlement(attempt.CostEventId, createdAt+10)

	prepared, _, err := PrepareChannelModelDetectionCostEvent(ctx, db, attempt)
	require.NoError(t, err)
	_, err = MarkChannelModelDetectionCostEventDispatched(ctx, db, prepared.CostEventId, createdAt+1)
	require.NoError(t, err)

	type settlementBarrierKey struct{}
	barrierKey := settlementBarrierKey{}
	settlementReachedQuery := make(chan struct{}, 1)
	releaseSettlement := make(chan struct{})
	const callbackName = "channel_model_detection_settlement_barrier"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(barrierKey) != true {
			return
		}
		select {
		case settlementReachedQuery <- struct{}{}:
		default:
		}
		<-releaseSettlement
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove(callbackName))
	})

	settlementResult := make(chan error, 1)
	go func() {
		_, settleErr := SettleChannelModelDetectionCostEvent(context.WithValue(ctx, barrierKey, true), db, settlement)
		settlementResult <- settleErr
	}()
	<-settlementReachedQuery

	settledQuota := settlement.SettledQuota
	costBasisQuota := settlement.CostBasisQuota
	_, unresolvedErr := MarkChannelModelDetectionCostEventUnresolved(ctx, db, ChannelModelDetectionCostUnresolvedInput{
		CostEventId:           settlement.CostEventId,
		UsageSource:           settlement.UsageSource,
		UsageAvailable:        settlement.UsageAvailable,
		InputTokens:           settlement.InputTokens,
		OutputTokens:          settlement.OutputTokens,
		TotalTokens:           settlement.TotalTokens,
		SettledQuota:          &settledQuota,
		CostBasisQuota:        &costBasisQuota,
		UpstreamRequestId:     settlement.UpstreamRequestId,
		ErrorCode:             "temporary_cost_failure",
		SanitizedErrorMessage: "模型检测成本暂不可结算",
		UpdatedAt:             createdAt + 2,
	})
	close(releaseSettlement)
	require.NoError(t, unresolvedErr)
	require.NoError(t, <-settlementResult)

	_, err = SettleChannelModelDetectionCostEvent(ctx, db, settlement)
	require.NoError(t, err)

	var event model.ChannelModelDetectionCostEvent
	require.NoError(t, db.Where("cost_event_id = ?", attempt.CostEventId).First(&event).Error)
	assert.Equal(t, model.ChannelModelDetectionSettlementSettled, event.SettlementStatus)

	var rows []model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", attempt.ChannelId).Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(400_000_000), rows[0].CostNanoCNY)
	assert.Equal(t, int64(400_000_000), rows[0].ModelDetectionCostNanoCNY)
	assert.Equal(t, int64(1), rows[0].SettledCount)
	assert.Zero(t, rows[0].UnresolvedCount)
}
