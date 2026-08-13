package service

import (
	"context"
	"errors"
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
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
	resetChannelDailyCostEstimateCalibratorForTest()
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
		resetChannelDailyCostEstimateCalibratorForTest()
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

	settledRequest := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(settledRequest, 18)
	BeginChannelDailyCostAttempt(settledRequest, 18)
	MarkChannelDailyCostRequestDispatched(settledRequest)
	recordChannelDailyCostFromQuota(settledRequest, 18, 500_000)
	FinalizeChannelDailyCostAttempt(settledRequest, 18, false)
	flushChannelDailyCostEvents(t)

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 18).Error)
	assert.Equal(t, int64(1), cost.SettledCount)
	assert.Equal(t, int64(1), cost.UnresolvedCount)
}

func TestChannelDailyCostUnresolvedTextUsesRequestEstimate(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	createChannelDailyCostMonitor(t, db, 19, 0.2)

	tests := []struct {
		name            string
		maxTokens       int
		wantCostNanoCNY int64
	}{
		{name: "explicit max tokens", maxTokens: 1_000, wantCostNanoCNY: 5_250_000},
		{name: "default max tokens", wantCostNanoCNY: 35_456_400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newChannelDailyCostTestContext()
			CaptureChannelDailyCostSnapshot(ctx, 19)
			relayInfo := &relaycommon.RelayInfo{
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 19},
				OriginModelName: "dynamic-text-model",
				RelayMode:       relayconstant.RelayModeChatCompletions,
				PriceData: types.PriceData{
					ModelRatio:      1,
					CompletionRatio: 2,
					GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 7},
				},
			}
			relayInfo.SetEstimatePromptTokens(500)
			BeginChannelDailyCostAttemptWithEstimate(ctx, 19, relayInfo, test.maxTokens)
			MarkChannelDailyCostRequestDispatched(ctx)
			FinalizeChannelDailyCostAttempt(ctx, 19, false)
			flushChannelDailyCostEvents(t)

			var cost model.ChannelDailyCost
			require.NoError(t, db.First(&cost, "channel_id = ?", 19).Error)
			assert.Equal(t, test.wantCostNanoCNY, cost.CostNanoCNY)
			assert.Zero(t, cost.SettledCount)
			assert.Equal(t, int64(1), cost.UnresolvedCount)
			require.NoError(t, db.Where("channel_id = ?", 19).Delete(&model.ChannelDailyCost{}).Error)
		})
	}
}

func TestChannelDailyCostUnresolvedEstimateKeepsRequestMultipliersAndDropsGroupRatio(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "fixed-image-model",
		PriceData: types.PriceData{
			UsePrice:       true,
			ModelPrice:     0.02,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 9},
		},
	}
	relayInfo.PriceData.AddOtherRatio("n", 3)

	quota := channelDailyCostEstimateQuotaBeforeGroup(relayInfo, 0, 250_000)
	assert.InDelta(t, 15_000, quota, 1e-9)
}

func TestChannelDailyCostUnresolvedEstimateCoversAudioOutputMultiplier(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "audio-model",
		RelayMode:       relayconstant.RelayModeRealtime,
		PriceData: types.PriceData{
			ModelRatio:           2,
			CompletionRatio:      3,
			AudioRatio:           4,
			AudioCompletionRatio: 5,
		},
	}
	relayInfo.SetEstimatePromptTokens(100)

	quota := channelDailyCostEstimateQuotaBeforeGroup(relayInfo, 200, common.QuotaPerUnit)
	assert.InDelta(t, 12_000, quota, 1e-9)
}

func TestChannelDailyCostUnresolvedEstimateAddsDeclaredToolToEveryBillingMode(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	operation_setting.SetToolPriceForTest(dto.BuildInToolFileSearch, 4)
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest(dto.BuildInToolFileSearch)
	})

	tests := []struct {
		name      string
		relayInfo *relaycommon.RelayInfo
		wantQuota float64
	}{
		{
			name: "fixed price",
			relayInfo: &relaycommon.RelayInfo{
				OriginModelName: "fixed-tool-model",
				PriceData:       types.PriceData{UsePrice: true, ModelPrice: 0.02},
			},
			wantQuota: 12_000,
		},
		{
			name: "dynamic ratio",
			relayInfo: &relaycommon.RelayInfo{
				OriginModelName: "dynamic-tool-model",
				RelayMode:       relayconstant.RelayModeEmbeddings,
				PriceData:       types.PriceData{ModelRatio: 1, CompletionRatio: 1},
			},
			wantQuota: 2_500,
		},
		{
			name: "tiered expression",
			relayInfo: &relaycommon.RelayInfo{
				OriginModelName: "tiered-tool-model",
				TieredBillingSnapshot: &billingexpr.BillingSnapshot{
					EstimatedQuotaBeforeGroup: 7_000,
				},
			},
			wantQuota: 9_000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.relayInfo.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{
				BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
					dto.BuildInToolFileSearch: {
						ToolName:  dto.BuildInToolFileSearch,
						CallCount: 0,
					},
				},
			}
			quota := channelDailyCostEstimateQuotaBeforeGroup(test.relayInfo, 0, 500_000)
			assert.InDelta(t, test.wantQuota, quota, 1e-9)
		})
	}
}

func TestChannelDailyCostUnresolvedAlphaSearchIncludesOneToolCall(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	operation_setting.SetToolPriceForTest(dto.BuildInToolWebSearchPreview, 10)
	t.Cleanup(func() {
		operation_setting.DeleteToolPriceForTest(dto.BuildInToolWebSearchPreview)
	})
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "search-model",
		RelayMode:       relayconstant.RelayModeAlphaSearch,
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
		},
	}

	quota := channelDailyCostEstimateQuotaBeforeGroup(relayInfo, 0, 500_000)
	assert.InDelta(t, 13_692, quota, 1e-9)
}

func TestChannelDailyCostUnresolvedEstimateCoversObservedShortfall(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	for _, sample := range []struct {
		estimated float64
		actual    float64
	}{
		{estimated: 100, actual: 120},
		{estimated: 1_000, actual: 1_200},
	} {
		observeChannelDailyCostEstimate(20, "adaptive-model", sample.estimated, sample.actual)
	}

	// The largest observed under-estimation factor is 1.2. The next unresolved
	// estimate keeps that factor and then receives the 5% safety margin.
	assert.InDelta(t, 126, channelDailyCostEstimatedQuota(20, "adaptive-model", 100), 1e-9)
	assert.InDelta(t, 105, channelDailyCostEstimatedQuota(21, "adaptive-model", 100), 1e-9)
	assert.InDelta(t, 105, channelDailyCostEstimatedQuota(20, "other-model", 100), 1e-9)
}

func TestChannelDailyCostEstimateCalibrationFreezesPerAttempt(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	ctx := newChannelDailyCostTestContext()
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "frozen-calibration-model",
		RelayMode:       relayconstant.RelayModeChatCompletions,
		PriceData:       types.PriceData{ModelRatio: 1, CompletionRatio: 1},
	}
	relayInfo.SetEstimatePromptTokens(500)

	BeginChannelDailyCostAttemptWithEstimate(ctx, 25, relayInfo, 100)
	_, _, frozenEstimate, _ := channelDailyCostAttemptEstimate(ctx, 25)
	observeChannelDailyCostEstimate(25, "frozen-calibration-model", 600, 1_200)
	_, _, unchangedEstimate, _ := channelDailyCostAttemptEstimate(ctx, 25)

	assert.InDelta(t, 630, frozenEstimate, 1e-9)
	assert.InDelta(t, frozenEstimate, unchangedEstimate, 1e-9)
	assert.Greater(t, channelDailyCostEstimatedQuota(25, "frozen-calibration-model", 600), unchangedEstimate)
}

func TestChannelDailyCostEstimateKeepsLargestRecentUnderestimate(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	observeChannelDailyCostEstimate(26, "rolling-calibration-model", 100, 300)
	for range channelDailyCostEstimateCalibrationWindow - 1 {
		observeChannelDailyCostEstimate(26, "rolling-calibration-model", 100, 120)
	}
	assert.InDelta(t, 315, channelDailyCostEstimatedQuota(26, "rolling-calibration-model", 100), 1e-9)

	observeChannelDailyCostEstimate(26, "rolling-calibration-model", 100, 120)
	assert.InDelta(t, 126, channelDailyCostEstimatedQuota(26, "rolling-calibration-model", 100), 1e-9)
}

func TestChannelDailyCostPerCallEstimateUsesFrozenQuotaUnit(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	ctx := newChannelDailyCostTestContext()
	ctx.Set(channelDailyCostSnapshotContextKey, channelDailyCostSnapshot{
		ChannelId:    22,
		QuotaPerUnit: 250_000,
		Configured:   true,
	})
	priceData := types.PriceData{UsePrice: true, ModelPrice: 0.04}
	BeginPerCallChannelDailyCostAttempt(ctx, 22, "task-model", priceData)

	_, baseQuota, quota, calibrated := channelDailyCostAttemptEstimate(ctx, 22)
	assert.InDelta(t, 10_000, baseQuota, 1e-9)
	assert.InDelta(t, 10_500, quota, 1e-9)
	assert.True(t, calibrated)
}

func TestChannelDailyCostPerCallEstimateRefreshesAfterSubmitAdjustment(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	ctx := newChannelDailyCostTestContext()
	ctx.Set(channelDailyCostSnapshotContextKey, channelDailyCostSnapshot{
		ChannelId:    27,
		QuotaPerUnit: 500_000,
		Configured:   true,
	})
	priceData := types.PriceData{UsePrice: true, ModelPrice: 0.02}
	BeginPerCallChannelDailyCostAttempt(ctx, 27, "task-model", priceData)
	priceData.AddOtherRatio("seconds", 3)
	RefreshPerCallChannelDailyCostAttempt(ctx, 27, "task-model", priceData)

	_, baseQuota, quota, calibrated := channelDailyCostAttemptEstimate(ctx, 27)
	assert.InDelta(t, 30_000, baseQuota, 1e-9)
	assert.InDelta(t, 31_500, quota, 1e-9)
	assert.True(t, calibrated)
}

func TestChannelDailyCostRetryAttemptsKeepSeparateEstimates(t *testing.T) {
	setupChannelDailyCostServiceTest(t)
	ctx := newChannelDailyCostTestContext()
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "retry-model",
		RelayMode:       relayconstant.RelayModeChatCompletions,
		PriceData:       types.PriceData{ModelRatio: 1, CompletionRatio: 1},
	}
	relayInfo.SetEstimatePromptTokens(500)

	BeginChannelDailyCostAttemptWithEstimate(ctx, 23, relayInfo, 100)
	_, firstBaseQuota, firstQuota, _ := channelDailyCostAttemptEstimate(ctx, 23)
	relayInfo.PriceData.CompletionRatio = 4
	BeginChannelDailyCostAttemptWithEstimate(ctx, 24, relayInfo, 200)
	_, secondBaseQuota, secondQuota, _ := channelDailyCostAttemptEstimate(ctx, 24)

	assert.InDelta(t, 600, firstBaseQuota, 1e-9)
	assert.InDelta(t, 630, firstQuota, 1e-9)
	assert.InDelta(t, 1_300, secondBaseQuota, 1e-9)
	assert.InDelta(t, 1_365, secondQuota, 1e-9)
	_, _, staleQuota, _ := channelDailyCostAttemptEstimate(ctx, 23)
	assert.Zero(t, staleQuota)
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
	RecordChannelTestDailyCost(tiered, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 4},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
		},
	}, 0, &billingexpr.TieredResult{ActualQuotaBeforeGroup: 2_500}, &dto.Usage{TotalTokens: 1}, true)
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

	assert.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, SettledDelta: 1}))
	assert.False(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 2, OccurredAt: 100, SettledDelta: 1}))
	assert.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 101, SettledDelta: 2}))
	require.NoError(t, batcher.flushAll())

	require.Len(t, written, 1)
	assert.Equal(t, int64(3), written[0].SettledDelta)
	assert.Equal(t, int64(1), batcher.droppedCount())
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

	require.True(t, batcher.enqueue(model.ChannelDailyCostDelta{ChannelId: 1, OccurredAt: 100, SettledDelta: 2, UnresolvedDelta: 1}))
	require.Error(t, batcher.flushAll())
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 1, batcher.pendingCount())
	assert.Zero(t, batcher.droppedCount())
	require.NoError(t, batcher.flushAll())
	assert.Equal(t, 3, attempts)
	require.Len(t, written, 1)
	assert.Equal(t, int64(2), written[0].SettledDelta)
	assert.Equal(t, int64(1), written[0].UnresolvedDelta)
}
