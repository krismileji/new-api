package service

import (
	"context"
	"errors"
	"math"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelDailyCostServiceTest(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := model.DB
	originalQuotaPerUnit := common.QuotaPerUnit
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "channel-cost.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	model.DB = db
	common.QuotaPerUnit = 500_000
	ResetChannelDailyCostSnapshotCache()
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
		MaxPending:    64,
		MaxBatchSize:  16,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   3,
	}, model.AddChannelDailyCostBatch)
	require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.ChannelDailyCost{}, &model.ChannelDailyAPIKeyCost{}))
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
		model.DB = originalDB
		common.QuotaPerUnit = originalQuotaPerUnit
		ResetChannelDailyCostSnapshotCache()
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func flushChannelDailyCostEvents(t *testing.T) {
	t.Helper()
	require.NoError(t, flushChannelDailyCostEventsForTest())
}

func newChannelDailyCostTestContext() *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return ctx
}

func createChannelDailyCostMonitor(t *testing.T, db *gorm.DB, channelId int, ratio float64) {
	t.Helper()
	conversion, err := MarshalChannelMonitorCostConversion(ChannelMonitorCostConversion{
		Mode:        ChannelMonitorCostConversionRecharge,
		PaidCNY:     10,
		CreditedUSD: 2,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:      channelId,
		Ratio:          ratio,
		UpdatedTime:    1,
		CostConversion: conversion,
	}).Error)
}

func TestChannelDailyCostFreezesRatioPerRequest(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 1, 0.5)

	firstRequest := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(firstRequest, 1)
	require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).Where("channel_id = ?", 1).Update("ratio", 2).Error)
	InvalidateChannelDailyCostSnapshot(1)
	recordChannelDailyCostFromQuota(firstRequest, 1, 500_000)

	secondRequest := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(secondRequest, 1)
	recordChannelDailyCostFromQuota(secondRequest, 1, 500_000)
	flushChannelDailyCostEvents(t)

	var costs []model.ChannelDailyCost
	require.NoError(t, db.Find(&costs).Error)
	require.Len(t, costs, 1)
	assert.Equal(t, int64(12_500_000_000), costs[0].CostNanoCNY)
	assert.Equal(t, int64(2), costs[0].SettledCount)
	assert.Zero(t, costs[0].UnresolvedCount)
}

func TestChannelDailyCostUsesUpstreamRatioWithoutConversion(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	conversion, err := MarshalChannelMonitorCostConversion(ChannelMonitorCostConversion{
		Mode: ChannelMonitorCostConversionNone,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:      7,
		Ratio:          0.8,
		UpdatedTime:    1,
		CostConversion: conversion,
	}).Error)
	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 7)

	recordChannelDailyCostFromQuota(ctx, 7, 500_000)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 7).Error)
	assert.Equal(t, int64(800_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Zero(t, cost.UnresolvedCount)
}

func TestChannelDailyCostAttributesStatusProbeAndExposesSettledAttemptCost(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 8, 0.5)
	ctx := newChannelDailyCostTestContext()
	ctx.Set(model.ChannelMonitorStatusProbeLogKey, true)
	CaptureChannelDailyCostSnapshot(ctx, 8)
	BeginChannelDailyCostAttempt(ctx, 8)

	recordChannelDailyCostFromQuota(ctx, 8, 500_000)
	settledCostNanoCNY := ChannelDailyCostAttemptSettledCost(ctx, 8)
	require.NotNil(t, settledCostNanoCNY)
	assert.Equal(t, int64(2_500_000_000), *settledCostNanoCNY)
	assert.Zero(t, pendingChannelDailyCostEventsForTest())

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 8).Error)
	assert.Equal(t, cost.CostNanoCNY, cost.ProbeCostNanoCNY)

	zeroCost := newChannelDailyCostTestContext()
	zeroCost.Set(model.ChannelMonitorStatusProbeLogKey, true)
	BeginChannelDailyCostAttempt(zeroCost, 10)
	recordChannelDailyCostEvent(zeroCost, channelDailyCostSnapshot{ChannelId: 10}, 0, 1, 0)
	settledZero := ChannelDailyCostAttemptSettledCost(zeroCost, 10)
	require.NotNil(t, settledZero)
	assert.Zero(t, *settledZero)

	unresolved := newChannelDailyCostTestContext()
	unresolved.Set(model.ChannelMonitorStatusProbeLogKey, true)
	BeginChannelDailyCostAttempt(unresolved, 9)
	recordChannelDailyCostUnresolved(unresolved, 9)
	assert.Nil(t, ChannelDailyCostAttemptSettledCost(unresolved, 9))
}

func TestChannelDailyCostAttributesGroupProbeSeparately(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 81, 0.5)
	ctx := newChannelDailyCostTestContext()
	ctx.Set(model.ChannelMonitorGroupProbeLogKey, true)
	CaptureChannelDailyCostSnapshot(ctx, 81)
	BeginChannelDailyCostAttempt(ctx, 81)

	recordChannelDailyCostFromQuota(ctx, 81, 500_000)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 81).Error)
	assert.Equal(t, cost.CostNanoCNY, cost.ProbeCostNanoCNY)
	assert.Equal(t, cost.CostNanoCNY, cost.GroupProbeCostNanoCNY)
}

func TestChannelDailyCostUsesQuotaBeforeFreeGroup(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 2, 0.2)
	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 2)
	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 2},
		OriginModelName: "test-model",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 0,
			},
		},
	}
	usage := &dto.Usage{PromptTokens: 1_000, TotalTokens: 1_000}
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Zero(t, summary.Quota)

	recordTextChannelDailyCost(ctx, relayInfo, usage, usage, summary, false, nil)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 2).Error)
	assert.Equal(t, int64(2_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
}

func TestChannelDailyCostSettlesToolSurchargeWithoutTokens(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 15, 0.2)
	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 15)
	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 15},
		OriginModelName: "test-model",
		StartTime:       time.Now(),
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					ToolName:  dto.BuildInToolWebSearchPreview,
					CallCount: 1,
				},
			},
		},
	}
	usage := &dto.Usage{}
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.True(t, summary.hasBillableUsage())

	recordTextChannelDailyCost(ctx, relayInfo, usage, usage, summary, false, nil)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 15).Error)
	assert.Equal(t, int64(10_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Zero(t, cost.UnresolvedCount)
}

func TestChannelDailyCostSettlesFixedPriceWithoutAuthoritativeUsage(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 16, 0.2)
	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 16)
	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 16},
		OriginModelName: "fixed-price-model",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.02,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	usage := &dto.Usage{BillingUsage: &dto.BillingUsage{Estimated: true}}
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 10_000, summary.Quota)

	recordTextChannelDailyCost(ctx, relayInfo, usage, usage, summary, false, nil)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 16).Error)
	assert.Equal(t, int64(20_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Zero(t, cost.UnresolvedCount)
}

func TestChannelDailyCostSettlesFixedPriceAudioWithoutTokens(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 17, 0.2)
	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 17)
	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 17},
	}

	recordAudioChannelDailyCost(ctx, relayInfo, QuotaInfo{
		UsePrice:   true,
		ModelPrice: 0.03,
		GroupRatio: 1,
	}, 0, false, false, nil)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 17).Error)
	assert.Equal(t, int64(30_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Zero(t, cost.UnresolvedCount)
}

func TestTextChannelDailyCostKeepsFrozenQuotaPerUnit(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	for _, channelId := range []int{27, 28, 29, 30} {
		createChannelDailyCostMonitor(t, db, channelId, 0.5)
	}

	fixedContext := newChannelDailyCostTestContext()
	toolContext := newChannelDailyCostTestContext()
	geminiContext := newChannelDailyCostTestContext()
	tieredContext := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(fixedContext, 27)
	CaptureChannelDailyCostSnapshot(toolContext, 28)
	CaptureChannelDailyCostSnapshot(geminiContext, 29)
	CaptureChannelDailyCostSnapshot(tieredContext, 30)

	common.QuotaPerUnit = 1_000_000

	fixedInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 27},
		OriginModelName: "fixed-price-model",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.02,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	fixedUsage := &dto.Usage{BillingUsage: &dto.BillingUsage{Estimated: true}}
	recordTextChannelDailyCost(
		fixedContext,
		fixedInfo,
		fixedUsage,
		fixedUsage,
		calculateTextQuotaSummary(fixedContext, fixedInfo, fixedUsage),
		false,
		nil,
	)

	toolInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 28},
		OriginModelName: "test-model",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					ToolName:  dto.BuildInToolWebSearchPreview,
					CallCount: 1,
				},
			},
		},
	}
	toolUsage := &dto.Usage{}
	recordTextChannelDailyCost(
		toolContext,
		toolInfo,
		toolUsage,
		toolUsage,
		calculateTextQuotaSummary(toolContext, toolInfo, toolUsage),
		false,
		nil,
	)

	geminiInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 29},
		OriginModelName: "gemini-2.5-flash",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	geminiUsage := &dto.Usage{
		PromptTokens: 1_000_000,
		PromptTokensDetails: dto.InputTokenDetails{
			AudioTokens: 1_000_000,
		},
	}
	recordTextChannelDailyCost(
		geminiContext,
		geminiInfo,
		geminiUsage,
		geminiUsage,
		calculateTextQuotaSummary(geminiContext, geminiInfo, geminiUsage),
		false,
		nil,
	)

	tieredInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 30},
		OriginModelName: "test-model",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{QuotaPerUnit: 250_000},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: {
					ToolName:  dto.BuildInToolWebSearchPreview,
					CallCount: 1,
				},
			},
		},
	}
	tieredUsage := &dto.Usage{PromptTokens: 1}
	recordTextChannelDailyCost(
		tieredContext,
		tieredInfo,
		tieredUsage,
		tieredUsage,
		calculateTextQuotaSummary(tieredContext, tieredInfo, tieredUsage),
		true,
		&billingexpr.TieredResult{ActualQuotaBeforeGroup: 2_500},
	)
	flushChannelDailyCostEvents(t)

	expectedCosts := map[int]int64{
		27: 50_000_000,
		28: 25_000_000,
		29: 2_500_000_000,
		30: 50_000_000,
	}
	for channelId, expectedCost := range expectedCosts {
		var cost model.ChannelDailyCost
		require.NoError(t, db.First(&cost, "channel_id = ?", channelId).Error)
		assert.Equal(t, expectedCost, cost.CostNanoCNY, "channel %d", channelId)
		assert.Equal(t, int64(1), cost.SettledCount, "channel %d", channelId)
		assert.Zero(t, cost.UnresolvedCount, "channel %d", channelId)
	}
}

func TestAudioChannelDailyCostKeepsFrozenQuotaPerUnit(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 31, 0.5)
	createChannelDailyCostMonitor(t, db, 32, 0.5)
	createChannelDailyCostMonitor(t, db, 33, 0.5)

	fixedContext := newChannelDailyCostTestContext()
	tieredContext := newChannelDailyCostTestContext()
	tokenContext := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(fixedContext, 31)
	CaptureChannelDailyCostSnapshot(tieredContext, 32)
	CaptureChannelDailyCostSnapshot(tokenContext, 33)
	common.QuotaPerUnit = 1_000_000

	recordAudioChannelDailyCost(fixedContext, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 31},
	}, QuotaInfo{
		UsePrice:   true,
		ModelPrice: 0.03,
		GroupRatio: 1,
	}, 0, false, false, nil)

	recordAudioChannelDailyCost(tieredContext, &relaycommon.RelayInfo{
		ChannelMeta:           &relaycommon.ChannelMeta{ChannelId: 32},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{QuotaPerUnit: 250_000},
	}, QuotaInfo{}, 1, true, true, &billingexpr.TieredResult{ActualQuotaBeforeGroup: 2_500})

	recordAudioChannelDailyCost(tokenContext, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 33},
	}, QuotaInfo{
		ModelName:  "test-model",
		ModelRatio: 2,
		GroupRatio: 1,
		InputDetails: TokenDetails{
			TextTokens:  100,
			AudioTokens: 50,
		},
		OutputDetails: TokenDetails{
			TextTokens:  20,
			AudioTokens: 10,
		},
	}, 180, true, false, nil)
	flushChannelDailyCostEvents(t)

	var fixedCost model.ChannelDailyCost
	require.NoError(t, db.First(&fixedCost, "channel_id = ?", 31).Error)
	assert.Equal(t, int64(75_000_000), fixedCost.CostNanoCNY)
	assert.Equal(t, int64(1), fixedCost.SettledCount)

	var tieredCost model.ChannelDailyCost
	require.NoError(t, db.First(&tieredCost, "channel_id = ?", 32).Error)
	assert.Equal(t, int64(25_000_000), tieredCost.CostNanoCNY)
	assert.Equal(t, int64(1), tieredCost.SettledCount)

	var tokenCost model.ChannelDailyCost
	require.NoError(t, db.First(&tokenCost, "channel_id = ?", 33).Error)
	assert.Equal(t, int64(1_800_000), tokenCost.CostNanoCNY)
	assert.Equal(t, int64(1), tokenCost.SettledCount)
}

func TestChannelDailyCostAttemptOnlyRecordsDispatchedUnsettledRequests(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 18, 0.2)

	localFailure := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(localFailure, 18)
	BeginChannelDailyCostAttempt(localFailure, 18)
	FinalizeChannelDailyCostAttempt(localFailure, 18, false)
	assert.Zero(t, pendingChannelDailyCostEventsForTest())

	dispatchedFailure := newChannelDailyCostTestContext()
	dispatchedFailure.Set("channel_test", true)
	CaptureChannelDailyCostSnapshot(dispatchedFailure, 18)
	BeginChannelDailyCostAttempt(dispatchedFailure, 18)
	MarkChannelDailyCostRequestDispatched(dispatchedFailure)
	assert.True(t, dispatchedFailure.GetBool("channel_test_request_dispatched"))
	FinalizeChannelDailyCostAttempt(dispatchedFailure, 18, false)
	FinalizeChannelDailyCostAttempt(dispatchedFailure, 18, false)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 18).Error)
	assert.Zero(t, cost.CostNanoCNY)
	assert.Zero(t, cost.SettledCount)
	assert.Equal(t, int64(1), cost.UnresolvedCount)

	settledRequest := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(settledRequest, 18)
	BeginChannelDailyCostAttempt(settledRequest, 18)
	MarkChannelDailyCostRequestDispatched(settledRequest)
	recordChannelDailyCostFromQuota(settledRequest, 18, 500_000)
	FinalizeChannelDailyCostAttempt(settledRequest, 18, false)
	flushChannelDailyCostEvents(t)

	require.NoError(t, db.First(&cost, "channel_id = ?", 18).Error)
	assert.Equal(t, int64(1_000_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Equal(t, int64(1), cost.UnresolvedCount)
}

func TestChannelDailyCostTracksUnconfiguredChannelsAndTieredSettlements(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	unconfiguredContext := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(unconfiguredContext, 3)
	recordChannelDailyCostFromQuota(unconfiguredContext, 3, 500_000)
	recordChannelDailyCostUnresolved(unconfiguredContext, 3)
	RecordPerCallChannelDailyCost(unconfiguredContext, 3, "test-model", types.PriceData{ModelPrice: 1, UsePrice: true})
	assert.Equal(t, 1, pendingChannelDailyCostEventsForTest())

	createChannelDailyCostMonitor(t, db, 4, 0.2)
	tiered := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(tiered, 4)
	tieredExpr := `tier("base", p * 5000)`
	RecordChannelTestDailyCost(tiered, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 4},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   tieredExpr,
			ExprHash:     billingexpr.ExprHashString(tieredExpr),
			GroupRatio:   1,
			QuotaPerUnit: common.QuotaPerUnit,
			ExprVersion:  1,
		},
	}, 0, &billingexpr.TieredResult{ActualQuotaBeforeGroup: 2_500}, &dto.Usage{PromptTokens: 1, TotalTokens: 1}, true)
	flushChannelDailyCostEvents(t)

	var unconfigured model.ChannelDailyCost
	require.NoError(t, db.First(&unconfigured, "channel_id = ?", 3).Error)
	assert.Zero(t, unconfigured.CostNanoCNY)
	assert.Zero(t, unconfigured.SettledCount)
	assert.Equal(t, int64(3), unconfigured.UnresolvedCount)

	var settled model.ChannelDailyCost
	require.NoError(t, db.First(&settled, "channel_id = ?", 4).Error)
	assert.Equal(t, int64(5_000_000), settled.CostNanoCNY)
	assert.Equal(t, int64(1), settled.SettledCount)
}

func TestChannelTestDailyCostUsesFullPreGroupSettlementMath(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 20, 0.5)
	createChannelDailyCostMonitor(t, db, 21, 0.5)

	tokenPriced := newChannelDailyCostTestContext()
	tokenPriced.Set(model.ChannelMonitorStatusProbeLogKey, true)
	CaptureChannelDailyCostSnapshot(tokenPriced, 20)
	RecordChannelTestDailyCost(tokenPriced, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 20},
		PriceData: types.PriceData{
			ModelRatio:      2,
			CompletionRatio: 4,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 3},
		},
	}, 999_999, nil, &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 10,
		TotalTokens:      110,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 50,
		},
	}, true)

	fixedPriceData := types.PriceData{
		ModelPrice:     0.01,
		UsePrice:       true,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 7},
	}
	fixedPriceData.AddOtherRatio("n", 3)
	fixedPrice := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(fixedPrice, 21)
	RecordChannelTestDailyCost(fixedPrice, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 21},
		PriceData:   fixedPriceData,
	}, 1, nil, &dto.Usage{}, false)
	flushChannelDailyCostEvents(t)

	var tokenPricedCost model.ChannelDailyCost
	require.NoError(t, db.First(&tokenPricedCost, "channel_id = ?", 20).Error)
	assert.Equal(t, int64(950_000), tokenPricedCost.CostNanoCNY)
	assert.Equal(t, tokenPricedCost.CostNanoCNY, tokenPricedCost.ProbeCostNanoCNY)
	assert.Equal(t, int64(1), tokenPricedCost.SettledCount)
	assert.Zero(t, tokenPricedCost.UnresolvedCount)

	var fixedPriceCost model.ChannelDailyCost
	require.NoError(t, db.First(&fixedPriceCost, "channel_id = ?", 21).Error)
	assert.Equal(t, int64(75_000_000), fixedPriceCost.CostNanoCNY)
	assert.Equal(t, int64(1), fixedPriceCost.SettledCount)
	assert.Zero(t, fixedPriceCost.UnresolvedCount)
}

func TestChannelTestDailyCostKeepsRequestQuotaPerUnit(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 26, 0.5)
	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 26)

	common.QuotaPerUnit = 1_000_000
	RecordChannelTestDailyCost(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 26},
		PriceData: types.PriceData{
			ModelPrice:     0.01,
			UsePrice:       true,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}, 0, nil, &dto.Usage{}, false)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 26).Error)
	assert.Equal(t, int64(25_000_000), cost.CostNanoCNY)
}

func TestChannelTestDailyCostUsesEffectiveMultimodalBillingUsage(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 22, 0.5)

	priceData := types.PriceData{
		ModelRatio:      2,
		CompletionRatio: 4,
		CacheRatio:      0.1,
		ImageRatio:      0.5,
		GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 7},
	}
	priceData.AddOtherRatio("batch", 3)
	ctx := newChannelDailyCostTestContext()
	ctx.Set(model.ChannelMonitorStatusProbeLogKey, true)
	CaptureChannelDailyCostSnapshot(ctx, 22)
	RecordChannelTestDailyCost(ctx, &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 22},
		OriginModelName: "gemini-2.5-flash",
		PriceData:       priceData,
	}, 0, nil, &dto.Usage{
		PromptTokens:     9_999,
		CompletionTokens: 9_999,
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
			PromptTokenCount:        100,
			CandidatesTokenCount:    10,
			TotalTokenCount:         110,
			CachedContentTokenCount: 20,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 60},
				{Modality: "IMAGE", TokenCount: 30},
				{Modality: "AUDIO", TokenCount: 10},
			},
		}),
	}, true)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 22).Error)
	assert.Equal(t, int64(2_985_000), cost.CostNanoCNY)
	assert.Equal(t, cost.CostNanoCNY, cost.ProbeCostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Zero(t, cost.UnresolvedCount)
}

func TestChannelTestDailyCostTieredSettlementIncludesToolSurchargeBeforeGroup(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 23, 0.5)

	ctx := newChannelDailyCostTestContext()
	ctx.Set(model.ChannelMonitorStatusProbeLogKey, true)
	CaptureChannelDailyCostSnapshot(ctx, 23)
	expr := `tier("base", p * 2)`
	RecordChannelTestDailyCost(ctx, &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 23},
		OriginModelName: "test-model",
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 4},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   expr,
			ExprHash:     billingexpr.ExprHashString(expr),
			GroupRatio:   4,
			QuotaPerUnit: common.QuotaPerUnit,
			ExprVersion:  1,
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearch: {
					ToolName:  dto.BuildInToolWebSearch,
					CallCount: 1,
				},
			},
		},
	}, 0, nil, &dto.Usage{PromptTokens: 1_000, TotalTokens: 1_000}, true)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 23).Error)
	assert.Equal(t, int64(30_000_000), cost.CostNanoCNY)
	assert.Equal(t, cost.CostNanoCNY, cost.ProbeCostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Zero(t, cost.UnresolvedCount)
}

func TestChannelTestDailyCostLeavesSaturatedTieredSettlementUnresolved(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 24, 0.5)

	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 24)
	expr := `tier("base", p * 10000000000)`
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 24},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   expr,
			ExprHash:     billingexpr.ExprHashString(expr),
			GroupRatio:   1,
			QuotaPerUnit: common.QuotaPerUnit,
			ExprVersion:  1,
		},
	}
	RecordChannelTestDailyCost(ctx, info, 0, nil, &dto.Usage{PromptTokens: 1, TotalTokens: 1}, true)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 24).Error)
	assert.Zero(t, cost.CostNanoCNY)
	assert.Zero(t, cost.SettledCount)
	assert.Equal(t, int64(1), cost.UnresolvedCount)
	require.NotNil(t, info.QuotaClamp)
	assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}

func TestChannelTestDailyCostIgnoresTieredGroupRatioSaturation(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 25, 0.5)

	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 25)
	expr := `tier("base", p * 2)`
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 25},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:  "tiered_expr",
			ExprString:   expr,
			ExprHash:     billingexpr.ExprHashString(expr),
			GroupRatio:   float64(common.MaxQuota),
			QuotaPerUnit: common.QuotaPerUnit,
			ExprVersion:  1,
		},
	}
	RecordChannelTestDailyCost(ctx, info, 0, nil, &dto.Usage{PromptTokens: 1_000, TotalTokens: 1_000}, true)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 25).Error)
	assert.Equal(t, int64(5_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Zero(t, cost.UnresolvedCount)
	require.NotNil(t, info.QuotaClamp)
	assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}

func TestChannelDailyCostSnapshotCoalescesConcurrentCacheMisses(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 13, 0.2)
	var queryCount atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:count_channel_daily_cost_snapshot", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "ChannelRatioMonitor" {
			queryCount.Add(1)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove("test:count_channel_daily_cost_snapshot"))
	})
	InvalidateChannelDailyCostSnapshot(13)

	const requests = 24
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(requests)
	for range requests {
		go func() {
			defer waitGroup.Done()
			<-start
			snapshot, err := getChannelDailyCostSnapshot(13)
			assert.NoError(t, err)
			assert.True(t, snapshot.Configured)
		}()
	}
	close(start)
	waitGroup.Wait()
	assert.Equal(t, int64(1), queryCount.Load())
}

func TestChannelDailyCostCachedRatioUsesCurrentQuotaPerUnit(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 22, 0.5)

	first, err := getChannelDailyCostSnapshot(22)
	require.NoError(t, err)
	assert.Equal(t, float64(500_000), first.QuotaPerUnit)

	common.QuotaPerUnit = 750_000
	second, err := getChannelDailyCostSnapshot(22)
	require.NoError(t, err)
	assert.Equal(t, float64(750_000), second.QuotaPerUnit)
	assert.Equal(t, first.CostRatioCNY, second.CostRatioCNY)
}

func TestChannelDailyCostSnapshotReturnsAFallbackWhenConfigurationParsingFails(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId:      14,
		Ratio:          0.2,
		UpdatedTime:    1,
		CostConversion: "{invalid",
	}).Error)

	snapshot, err := getChannelDailyCostSnapshot(14)
	require.Error(t, err)
	assert.Equal(t, 14, snapshot.ChannelId)
	assert.Equal(t, common.QuotaPerUnit, snapshot.QuotaPerUnit)
	assert.False(t, snapshot.Configured)
}

func TestChannelDailyCostLeavesEstimatedUsageUnresolved(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 5, 0.2)

	localUsageContext := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(localUsageContext, 5)
	common.SetContextKey(localUsageContext, constant.ContextKeyLocalCountTokens, true)
	localUsage := &dto.Usage{PromptTokens: 1_000, TotalTokens: 1_000}
	localSummary := calculateTextQuotaSummary(localUsageContext, &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 5},
		OriginModelName: "test-model",
		StartTime:       time.Now(),
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}, localUsage)
	recordTextChannelDailyCost(localUsageContext, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 5},
	}, localUsage, localUsage, localSummary, false, nil)

	estimatedUsageContext := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(estimatedUsageContext, 5)
	estimatedUsage := &dto.Usage{
		PromptTokens: 1_000,
		TotalTokens:  1_000,
		BillingUsage: &dto.BillingUsage{Estimated: true},
	}
	recordTextChannelDailyCost(estimatedUsageContext, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 5},
	}, estimatedUsage, estimatedUsage, textQuotaSummary{TotalTokens: 1_000}, false, nil)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 5).Error)
	assert.Zero(t, cost.CostNanoCNY)
	assert.Zero(t, cost.SettledCount)
	assert.Equal(t, int64(2), cost.UnresolvedCount)
}

func TestChannelDailyCostPersistsAfterRequestCancellation(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 6, 0.2)
	ctx := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(ctx, 6)

	requestContext, cancel := context.WithCancel(ctx.Request.Context())
	ctx.Request = ctx.Request.WithContext(requestContext)
	cancel()
	recordChannelDailyCostFromQuota(ctx, 6, 500_000)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 6).Error)
	assert.Equal(t, int64(1_000_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
}

func TestChannelDailyCostAttributesSettlementsToTheSelectedAPIKey(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 8, 0.2)

	firstContext := newChannelDailyCostTestContext()
	firstContext.Set("token_id", 11)
	firstContext.Set("token_name", "生产 Key")
	common.SetContextKey(firstContext, constant.ContextKeyChannelKey, "sk-selected-alpha")
	CaptureChannelDailyCostSnapshot(firstContext, 8)
	recordChannelDailyCostFromQuota(firstContext, 8, 500_000)

	secondContext := newChannelDailyCostTestContext()
	secondContext.Set("token_id", 12)
	secondContext.Set("token_name", "备用 Key")
	common.SetContextKey(secondContext, constant.ContextKeyChannelKey, "sk-selected-beta")
	CaptureChannelDailyCostSnapshot(secondContext, 8)
	recordChannelDailyCostFromQuota(secondContext, 8, 500_000)
	flushChannelDailyCostEvents(t)

	var total model.ChannelDailyCost
	require.NoError(t, db.First(&total, "channel_id = ?", 8).Error)
	assert.Equal(t, int64(2), total.SettledCount)
	var keyRows []model.ChannelDailyAPIKeyCost
	require.NoError(t, db.Order("api_key_id ASC").Find(&keyRows).Error)
	require.Len(t, keyRows, 2)
	assert.Equal(t, int64(1), keyRows[0].SettledCount)
	assert.Equal(t, int64(1), keyRows[1].SettledCount)
	assert.Equal(t, 11, keyRows[0].APIKeyId)
	assert.Equal(t, "生产 Key", keyRows[0].APIKeyName)
	assert.Equal(t, 12, keyRows[1].APIKeyId)
	assert.Equal(t, "备用 Key", keyRows[1].APIKeyName)
	assert.Equal(t, total.CostNanoCNY, keyRows[0].CostNanoCNY+keyRows[1].CostNanoCNY)
	assert.NotContains(t, keyRows[0].KeyDisplay, "sk-selected")
	assert.NotContains(t, keyRows[1].KeyDisplay, "sk-selected")
}

func TestChannelDailyCostResolvesTokenNameWhenContextOnlyHasTheID(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}))
	require.NoError(t, db.Create(&model.Token{Id: 29, UserId: 1, Key: "inbound-key", Name: "从令牌表解析的名称"}).Error)
	createChannelDailyCostMonitor(t, db, 9, 0.2)

	ctx := newChannelDailyCostTestContext()
	ctx.Set("token_id", 29)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "sk-selected")
	CaptureChannelDailyCostSnapshot(ctx, 9)
	recordChannelDailyCostFromQuota(ctx, 9, 500_000)
	flushChannelDailyCostEvents(t)

	rows, err := model.GetChannelDailyAPIKeyCosts(context.Background(), 0, time.Now().Add(24*time.Hour).Unix())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 29, rows[0].APIKeyId)
	assert.Equal(t, "从令牌表解析的名称", rows[0].APIKeyName)
}

func TestChannelDailyCostAggregatesWithoutWritingOnTheRequestPath(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 10, 0.2)
	ctx := newChannelDailyCostTestContext()
	ctx.Set("token_id", 31)
	common.SetContextKey(ctx, constant.ContextKeyChannelKey, "sk-batched")
	CaptureChannelDailyCostSnapshot(ctx, 10)

	recordChannelDailyCostFromQuota(ctx, 10, 500_000)
	recordChannelDailyCostFromQuota(ctx, 10, 500_000)

	var count int64
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).Count(&count).Error)
	assert.Zero(t, count)
	assert.Equal(t, 1, pendingChannelDailyCostEventsForTest())
	flushChannelDailyCostEvents(t)

	var total model.ChannelDailyCost
	require.NoError(t, db.First(&total).Error)
	assert.Equal(t, int64(2_000_000_000), total.CostNanoCNY)
	assert.Equal(t, int64(2), total.SettledCount)
	var detail model.ChannelDailyAPIKeyCost
	require.NoError(t, db.First(&detail).Error)
	assert.Equal(t, total.CostNanoCNY, detail.CostNanoCNY)
	assert.Equal(t, total.SettledCount, detail.SettledCount)
}

func TestChannelDailyCostBatcherIsBoundedAndKeepsAggregatingExistingKeys(t *testing.T) {
	var written []model.ChannelDailyCostDelta
	batcher := newChannelDailyCostBatcher(channelDailyCostBatcherConfig{
		MaxPending:    1,
		MaxBatchSize:  1,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
	}, func(_ context.Context, deltas []model.ChannelDailyCostDelta) error {
		written = append(written, deltas...)
		return nil
	})
	t.Cleanup(batcher.stop)

	assert.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, CostNanoCNY: 10, ProbeCostNanoCNY: 3, SettledDelta: 1}))
	assert.False(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 2, OccurredAt: 100, SettledDelta: 1}))
	assert.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 101, CostNanoCNY: 5, ProbeCostNanoCNY: 2, SettledDelta: 2}))
	require.NoError(t, batcher.flushAll())

	require.Len(t, written, 1)
	assert.Equal(t, int64(3), written[0].SettledDelta)
	assert.Equal(t, int64(15), written[0].CostNanoCNY)
	assert.Equal(t, int64(5), written[0].ProbeCostNanoCNY)
}

func TestChannelDailyCostLegacyBatcherStripsEventIDBeforeDirectWrite(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
		MaxPending:    4,
		MaxBatchSize:  4,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
		AutoFlush:     false,
	}, nil)
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), nil)
	})

	when := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	delta := model.ChannelDailyCostDelta{
		EventId:      "legacy-mode-event",
		ChannelId:    29,
		OccurredAt:   when,
		CostNanoCNY:  125,
		SettledDelta: 1,
	}
	require.True(t, enqueueChannelDailyCost(delta))
	require.NoError(t, flushChannelDailyCostEventsForTest())

	var total model.ChannelDailyCost
	require.NoError(t, db.Where("channel_id = ?", delta.ChannelId).First(&total).Error)
	assert.Equal(t, delta.CostNanoCNY, total.CostNanoCNY)
	assert.Equal(t, delta.SettledDelta, total.SettledCount)
}

func TestGetChannelDailyCostPendingCountReadsInMemoryQueue(t *testing.T) {
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
		MaxPending:    4,
		MaxBatchSize:  2,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
		AutoFlush:     false,
	}, func(context.Context, []model.ChannelDailyCostDelta) error { return nil })
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})

	assert.Zero(t, GetChannelDailyCostPendingCount())
	require.True(t, enqueueChannelDailyCost(model.ChannelDailyCostDelta{
		ChannelId: 1, OccurredAt: 100, CostNanoCNY: 10, SettledDelta: 1,
	}))
	assert.Equal(t, 1, GetChannelDailyCostPendingCount())
}

func TestChannelDailyCostFallsBackToSynchronousWriteWhenBufferIsFull(t *testing.T) {
	var written []model.ChannelDailyCostDelta
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
		MaxPending:    1,
		MaxBatchSize:  2,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
		AutoFlush:     false,
	}, func(_ context.Context, deltas []model.ChannelDailyCostDelta) error {
		written = append(written, deltas...)
		return nil
	})
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})

	require.True(t, enqueueChannelDailyCost(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, CostNanoCNY: 10, SettledDelta: 1}))
	require.True(t, enqueueChannelDailyCost(model.ChannelDailyCostDelta{ChannelId: 2, OccurredAt: 100, CostNanoCNY: 20, SettledDelta: 1}))

	require.Len(t, written, 1)
	assert.Equal(t, 2, written[0].ChannelId)
	assert.Equal(t, int64(20), written[0].CostNanoCNY)
	assert.Equal(t, 1, pendingChannelDailyCostEventsForTest())
	require.NoError(t, flushChannelDailyCostEventsForTest())
	require.Len(t, written, 2)
	assert.Equal(t, 1, written[1].ChannelId)
}

func TestChannelDailyCostSynchronousFallbackFailureDoesNotMarkAttemptRecorded(t *testing.T) {
	attempts := 0
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
		MaxPending:    1,
		MaxBatchSize:  2,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
		AutoFlush:     false,
	}, func(_ context.Context, _ []model.ChannelDailyCostDelta) error {
		attempts++
		return errors.New("database unavailable")
	})
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})

	require.True(t, enqueueChannelDailyCost(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, SettledDelta: 1}))
	ctx := newChannelDailyCostTestContext()
	BeginChannelDailyCostAttempt(ctx, 2)
	MarkChannelDailyCostRequestDispatched(ctx)
	ctx.Set(channelDailyCostSnapshotContextKey, channelDailyCostSnapshot{ChannelId: 2})

	assert.False(t, recordChannelDailyCostEvent(ctx, channelDailyCostSnapshot{ChannelId: 2}, 25, 1, 0))
	stateValue, exists := ctx.Get(channelDailyCostAttemptContextKey)
	require.True(t, exists)
	state, ok := stateValue.(*channelDailyCostAttemptState)
	require.True(t, ok)
	state.mu.Lock()
	assert.False(t, state.Recorded)
	assert.False(t, state.Recording)
	state.mu.Unlock()
	assert.Nil(t, ChannelDailyCostAttemptSettledCost(ctx, 2))

	FinalizeChannelDailyCostAttempt(ctx, 2, false)
	FinalizeChannelDailyCostAttempt(ctx, 2, false)
	assert.Equal(t, 3, attempts)
	state.mu.Lock()
	assert.False(t, state.Recorded)
	assert.False(t, state.Recording)
	state.mu.Unlock()
}

func TestChannelDailyCostRejectsInvalidEventWithoutSynchronousWrite(t *testing.T) {
	writes := 0
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
		MaxPending:    1,
		MaxBatchSize:  1,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
		AutoFlush:     false,
	}, func(_ context.Context, _ []model.ChannelDailyCostDelta) error {
		writes++
		return nil
	})
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})

	assert.False(t, enqueueChannelDailyCost(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100}))
	assert.Zero(t, writes)
	assert.Zero(t, pendingChannelDailyCostEventsForTest())
}

func TestChannelDailyCostRejectsOverflowingAggregateWithoutMarkingItRecorded(t *testing.T) {
	var written []model.ChannelDailyCostDelta
	resetChannelDailyCostBatcherForTest(channelDailyCostBatcherConfig{
		MaxPending:    2,
		MaxBatchSize:  2,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
		AutoFlush:     false,
	}, func(_ context.Context, deltas []model.ChannelDailyCostDelta) error {
		written = append(written, deltas...)
		return nil
	})
	t.Cleanup(func() {
		resetChannelDailyCostBatcherForTest(defaultChannelDailyCostBatcherConfig(), model.AddChannelDailyCostBatch)
	})

	now := common.GetTimestamp()
	require.True(t, enqueueChannelDailyCost(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: now, CostNanoCNY: math.MaxInt64, SettledDelta: 1}))
	ctx := newChannelDailyCostTestContext()
	BeginChannelDailyCostAttempt(ctx, 1)
	MarkChannelDailyCostRequestDispatched(ctx)
	assert.False(t, recordChannelDailyCostEvent(ctx, channelDailyCostSnapshot{ChannelId: 1}, 1, 1, 0))

	assert.Empty(t, written)
	stateValue, exists := ctx.Get(channelDailyCostAttemptContextKey)
	require.True(t, exists)
	state, ok := stateValue.(*channelDailyCostAttemptState)
	require.True(t, ok)
	state.mu.Lock()
	assert.False(t, state.Recorded)
	state.mu.Unlock()
	require.NoError(t, flushChannelDailyCostEventsForTest())
	require.Len(t, written, 1)
	assert.Equal(t, int64(math.MaxInt64), written[0].CostNanoCNY)
}

func TestChannelDailyCostBatcherRetriesAFlushWithoutDuplicatingTheBatch(t *testing.T) {
	attempts := 0
	var written []model.ChannelDailyCostDelta
	batcher := newChannelDailyCostBatcher(channelDailyCostBatcherConfig{
		MaxPending:    4,
		MaxBatchSize:  4,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   3,
	}, func(_ context.Context, deltas []model.ChannelDailyCostDelta) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary database failure")
		}
		written = append(written, deltas...)
		return nil
	})
	t.Cleanup(batcher.stop)

	require.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, SettledDelta: 2}))
	require.NoError(t, batcher.flushAll())
	assert.Equal(t, 3, attempts)
	require.Len(t, written, 1)
	assert.Equal(t, int64(2), written[0].SettledDelta)
}

func TestChannelDailyCostBatcherRetainsAFailedBatchForLaterRetry(t *testing.T) {
	attempts := 0
	var written []model.ChannelDailyCostDelta
	batcher := newChannelDailyCostBatcher(channelDailyCostBatcherConfig{
		MaxPending:    2,
		MaxBatchSize:  2,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   2,
	}, func(_ context.Context, deltas []model.ChannelDailyCostDelta) error {
		attempts++
		if attempts <= 2 {
			return errors.New("database unavailable")
		}
		written = append(written, deltas...)
		return nil
	})
	t.Cleanup(batcher.stop)

	require.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, CostNanoCNY: 100, ProbeCostNanoCNY: 30, SettledDelta: 2, UnresolvedDelta: 1}))
	require.Error(t, batcher.flushAll())
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 1, batcher.pendingCount())
	require.NoError(t, batcher.flushAll())
	assert.Equal(t, 3, attempts)
	require.Len(t, written, 1)
	assert.Equal(t, int64(2), written[0].SettledDelta)
	assert.Equal(t, int64(1), written[0].UnresolvedDelta)
	assert.Equal(t, int64(30), written[0].ProbeCostNanoCNY)
}

func TestChannelDailyCostBatcherKeepsFailedBatchSeparateFromNewOverflowingAggregate(t *testing.T) {
	attempts := 0
	var batcher *channelDailyCostBatcher
	var written []model.ChannelDailyCostDelta
	batcher = newChannelDailyCostBatcher(channelDailyCostBatcherConfig{
		MaxPending:    2,
		MaxBatchSize:  1,
		FlushInterval: time.Hour,
		DBTimeout:     time.Second,
		MaxAttempts:   1,
		AutoFlush:     false,
	}, func(_ context.Context, deltas []model.ChannelDailyCostDelta) error {
		attempts++
		if attempts == 1 {
			require.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 101, CostNanoCNY: 1, SettledDelta: 1}))
			return errors.New("database unavailable")
		}
		written = append(written, deltas...)
		return nil
	})
	t.Cleanup(batcher.stop)

	require.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, CostNanoCNY: math.MaxInt64, SettledDelta: 1}))
	require.Error(t, batcher.flushAll())
	assert.Equal(t, 2, batcher.pendingCount())
	require.NoError(t, batcher.flushAll())
	require.Len(t, written, 2)
	assert.Equal(t, int64(math.MaxInt64), written[0].CostNanoCNY)
	assert.Equal(t, int64(1), written[1].CostNanoCNY)
}
