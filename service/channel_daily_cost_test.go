package service

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
	require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}, &model.ChannelDailyCost{}))
	t.Cleanup(func() {
		model.DB = originalDB
		common.QuotaPerUnit = originalQuotaPerUnit
		ResetChannelDailyCostSnapshotCache()
		require.NoError(t, sqlDB.Close())
	})
	return db
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

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 2).Error)
	assert.Equal(t, int64(2_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
}

func TestChannelDailyCostTracksUnresolvedAndTieredSettlements(t *testing.T) {
	db := setupChannelDailyCostServiceTest(t)
	unconfigured := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(unconfigured, 3)
	recordChannelDailyCostFromQuota(unconfigured, 3, 500_000)

	createChannelDailyCostMonitor(t, db, 4, 0.2)
	tiered := newChannelDailyCostTestContext()
	CaptureChannelDailyCostSnapshot(tiered, 4)
	RecordChannelTestDailyCost(tiered, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 4},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
		},
	}, 0, &billingexpr.TieredResult{ActualQuotaBeforeGroup: 2_500}, &dto.Usage{TotalTokens: 1}, true)

	var unresolved model.ChannelDailyCost
	require.NoError(t, db.First(&unresolved, "channel_id = ?", 3).Error)
	assert.Zero(t, unresolved.SettledCount)
	assert.Equal(t, int64(1), unresolved.UnresolvedCount)

	var settled model.ChannelDailyCost
	require.NoError(t, db.First(&settled, "channel_id = ?", 4).Error)
	assert.Equal(t, int64(5_000_000), settled.CostNanoCNY)
	assert.Equal(t, int64(1), settled.SettledCount)
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

	var cost model.ChannelDailyCost
	require.NoError(t, db.First(&cost, "channel_id = ?", 6).Error)
	assert.Equal(t, int64(1_000_000_000), cost.CostNanoCNY)
	assert.Equal(t, int64(1), cost.SettledCount)
}
