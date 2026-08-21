package controller

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelMonitorOverviewUsesPersistedTodayCostState(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 10, Name: "已配置", Key: "key-10"},
		{Id: 11, Name: "未配置", Key: "key-11"},
		{Id: 12, Name: "不换算零成本", Key: "key-12"},
	}).Error)
	conversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{
		Mode: service.ChannelMonitorCostConversionNone,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 10, Ratio: 1, UpdatedTime: 1, CostConversion: conversion},
		{ChannelId: 11, Ratio: 1, UpdatedTime: 0, CostConversion: conversion},
		{ChannelId: 12, Ratio: 0.8, UpdatedTime: 1, CostConversion: conversion},
	}).Error)
	now := common.GetTimestamp()
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 10, now, 1_250_000_000, 1, 1))
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 11, now, 0, 0, 1))
	settled := model.NewChannelMonitorEvent(10, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	settled.EventId = "overview-settled"
	settled.RequestDispatched = true
	settled.IsFinalAttempt = true
	settled.CostStatus = model.ChannelMonitorEventCostSettled
	settled.SettledCostNanoCNY = 9_000_000_000
	unresolved := model.NewChannelMonitorEvent(11, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeFailure, now)
	unresolved.EventId = "overview-unresolved"
	unresolved.RequestDispatched = true
	unresolved.CostStatus = model.ChannelMonitorEventCostUnresolved
	emitChannelMonitorControllerRealtimeEvents(t, settled, unresolved)

	ctx, recorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor", nil)
	GetChannelMonitorOverview(ctx)
	require.Equal(t, 200, recorder.Code)
	type channelCostState struct {
		Id                       int     `json:"id"`
		TodayCostCNY             float64 `json:"today_cost_cny"`
		TodayCostConfigured      bool    `json:"today_cost_configured"`
		TodayCostComplete        bool    `json:"today_cost_complete"`
		TodayCostUnresolvedCount int64   `json:"today_cost_unresolved_count"`
	}
	var response struct {
		Data struct {
			Channels []channelCostState `json:"channels"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data.Channels, 3)
	byId := make(map[int]channelCostState, len(response.Data.Channels))
	for _, channel := range response.Data.Channels {
		byId[channel.Id] = channel
	}
	assert.InDelta(t, 1.25, byId[10].TodayCostCNY, 1e-9)
	assert.True(t, byId[10].TodayCostConfigured)
	assert.False(t, byId[10].TodayCostComplete)
	assert.Equal(t, int64(1), byId[10].TodayCostUnresolvedCount)
	assert.False(t, byId[11].TodayCostConfigured)
	assert.False(t, byId[11].TodayCostComplete)
	assert.Zero(t, byId[12].TodayCostCNY)
	assert.True(t, byId[12].TodayCostConfigured)
	assert.True(t, byId[12].TodayCostComplete)
}

func TestGetChannelMonitorCostOverviewReadsSettledDailyFacts(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 1, Name: "已结算渠道", Key: "key-1"},
		{Id: 2, Name: "成本未确认渠道", Key: "key-2"},
		{Id: 3, Name: "零成本渠道", Key: "key-3"},
	}).Error)

	yesterday := time.Date(2026, 7, 21, 15, 58, 0, 0, time.UTC).Unix()
	today := time.Date(2026, 7, 21, 16, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 1, yesterday, 2_500_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCostWithProbe(context.Background(), 1, yesterday+60, 500_000_000, 500_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCostWithModelDetection(context.Background(), db, 1, yesterday+30, 300_000_000, 300_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCostWithProbe(context.Background(), 1, today, 1_250_000_000, 250_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCostWithModelDetection(context.Background(), db, 1, today+60, 150_000_000, 150_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 2, today, 0, 0, 1))
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 3, today, 0, 1, 0))
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).
		Where("channel_id = ? AND day_start = ?", 1, channelMonitorCostDayStart(yesterday)).
		Update("group_probe_cost_nano_cny", int64(200_000_000)).Error)
	require.NoError(t, db.Model(&model.ChannelDailyCost{}).
		Where("channel_id = ? AND day_start = ?", 1, channelMonitorCostDayStart(today)).
		Update("group_probe_cost_nano_cny", int64(100_000_000)).Error)

	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	overview, err := getChannelMonitorCostOverview(context.Background(), 2, now)
	require.NoError(t, err)
	require.Len(t, overview.Items, 2)
	assert.Equal(t, "2026-07-21", overview.Items[0].Date)
	assert.Equal(t, "2026-07-22", overview.Items[1].Date)
	assert.InDelta(t, 3.3, overview.YesterdayCostCNY, 1e-9)
	assert.InDelta(t, 1.4, overview.TodayCostCNY, 1e-9)
	assert.InDelta(t, 4.7, overview.TotalCostCNY, 1e-9)
	assert.InDelta(t, 0.5, overview.YesterdayProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.25, overview.TodayProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.75, overview.TotalProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.2, overview.YesterdayGroupProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.1, overview.TodayGroupProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.3, overview.TotalGroupProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.3, overview.YesterdayModelDetectionCostCNY, 1e-9)
	assert.InDelta(t, 0.15, overview.TodayModelDetectionCostCNY, 1e-9)
	assert.InDelta(t, 0.45, overview.TotalModelDetectionCostCNY, 1e-9)
	assert.InDelta(t, 0.5, overview.Items[0].ProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.25, overview.Items[1].ProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.2, overview.Items[0].GroupProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.1, overview.Items[1].GroupProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.3, overview.Items[0].ModelDetectionCostCNY, 1e-9)
	assert.InDelta(t, 0.15, overview.Items[1].ModelDetectionCostCNY, 1e-9)
	assert.Equal(t, int64(3), overview.Items[0].SettledCount)
	assert.Equal(t, int64(3), overview.Items[1].SettledCount)
	assert.Equal(t, int64(1), overview.Items[1].UnresolvedCount)
	assert.Equal(t, 2, overview.Coverage.IncludedChannelCount)
	assert.Equal(t, 1, overview.Coverage.UnresolvedChannelCount)
	assert.Equal(t, int64(6), overview.Coverage.SettledCount)
	assert.Equal(t, int64(1), overview.Coverage.UnresolvedCount)
	assert.Equal(t, 3, overview.Coverage.MissingCostConfigChannelCount)
	assert.Zero(t, overview.Coverage.FreeGroupChannelCount)
	require.Len(t, overview.Channels, 3)
	assert.Equal(t, 1, overview.Channels[0].ChannelId)
	assert.Equal(t, "已结算渠道", overview.Channels[0].ChannelName)
	assert.InDelta(t, 4.7, overview.Channels[0].CostCNY, 1e-9)
	assert.InDelta(t, 0.75, overview.Channels[0].ProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.3, overview.Channels[0].GroupProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.45, overview.Channels[0].ModelDetectionCostCNY, 1e-9)
	assert.Equal(t, int64(5), overview.Channels[0].SettledCount)
	assert.Equal(t, 2, overview.Channels[1].ChannelId)
	assert.Equal(t, int64(1), overview.Channels[1].UnresolvedCount)
	assert.Equal(t, 3, overview.Channels[2].ChannelId)
	assert.Equal(t, int64(1), overview.Channels[2].SettledCount)
}

func TestGetChannelMonitorCostSummaryRejectsCrossChannelOverflow(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, db.Create(&[]model.ChannelDailyCost{
		{ChannelId: 1, DayStart: channelMonitorCostDayStart(now), CostNanoCNY: math.MaxInt64, SettledCount: 1, CreatedAt: now, UpdatedAt: now},
		{ChannelId: 2, DayStart: channelMonitorCostDayStart(now), CostNanoCNY: 1, SettledCount: 1, CreatedAt: now, UpdatedAt: now},
	}).Error)

	_, err := getChannelMonitorCostSummary(context.Background(), 1, now, 0)
	require.ErrorContains(t, err, "超过 int64 范围")
}

func TestGetChannelMonitorCostOverviewDateQueryScopesDetailsAndKeepsRangeTrend(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id:     41,
		Name:   "按日统计渠道",
		Key:    "key-41",
		Status: common.ChannelStatusEnabled,
	}).Error)

	now := common.GetTimestamp()
	todayStart := channelMonitorCostDayStart(now)
	yesterdayStart := todayStart - channelMonitorCostDaySeconds
	yesterdayFingerprint, yesterdayDisplay := model.ChannelDailyCostAPIKeyIdentityForToken(201, "yesterday-key")
	todayFingerprint, todayDisplay := model.ChannelDailyCostAPIKeyIdentityForToken(202, "today-key")
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(
		context.Background(), 41, yesterdayStart+60, 2_000_000_000, 1, 0,
		201, "昨日 Key", yesterdayFingerprint, yesterdayDisplay,
	))
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(
		context.Background(), 41, todayStart+60, 3_000_000_000, 1, 0,
		202, "今日 Key", todayFingerprint, todayDisplay,
	))

	detailDate := channelMonitorCostDate(yesterdayStart)
	ctx, recorder := newChannelMonitorControllerContext(
		t, "GET", "/api/channel_monitor/cost?days=3&date="+detailDate, nil,
	)
	GetChannelMonitorCostOverview(ctx)
	require.Equal(t, 200, recorder.Code)
	var response struct {
		Data channelMonitorCostOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	overview := response.Data

	assert.Equal(t, detailDate, overview.DetailDate)
	require.Len(t, overview.ChartItems, 3)
	assert.InDelta(t, 5, overview.TotalCostCNY, 1e-9)
	assert.InDelta(t, 2, overview.YesterdayCostCNY, 1e-9)
	assert.InDelta(t, 3, overview.TodayCostCNY, 1e-9)
	require.Len(t, overview.Channels, 1)
	assert.InDelta(t, 2, overview.Channels[0].CostCNY, 1e-9)
	require.Len(t, overview.APIKeys, 1)
	assert.Equal(t, 201, overview.APIKeys[0].APIKeyId)
	assert.Equal(t, "昨日 Key", overview.APIKeys[0].APIKeyName)
}

func TestGetChannelMonitorCostOverviewOrdersChannelMetadataByStatusAndRatio(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	lowRemark := "  低倍率主线路  "
	highRemark := "高倍率线路"
	disabledRemark := "停用备用线路"
	missingRatioRemark := "未配置倍率"
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 51, Name: "高倍率启用", Key: "key-51", Status: common.ChannelStatusEnabled, Remark: &highRemark},
		{Id: 52, Name: "低倍率启用", Key: "key-52", Status: common.ChannelStatusEnabled, Remark: &lowRemark},
		{Id: 53, Name: "低倍率停用", Key: "key-53", Status: common.ChannelStatusManuallyDisabled, Remark: &disabledRemark},
		{Id: 54, Name: "未配置倍率启用", Key: "key-54", Status: common.ChannelStatusEnabled, Remark: &missingRatioRemark},
	}).Error)
	require.NoError(t, db.Create(&[]model.ChannelRatioMonitor{
		{ChannelId: 51, Ratio: 1.5, UpdatedTime: 1},
		{ChannelId: 52, Ratio: 0.5, UpdatedTime: 1},
		{ChannelId: 53, Ratio: 0.2, UpdatedTime: 1},
	}).Error)

	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	dayStart := channelMonitorCostDayStart(now)
	for _, channelId := range []int{51, 52, 53, 54} {
		require.NoError(t, model.AddChannelDailyCost(
			context.Background(), channelId, dayStart+60, 1_000_000_000, 1, 0,
		))
	}

	overview, err := getChannelMonitorCostOverviewForChannelPageAtDay(
		context.Background(), 1, now, 0, 1, 1, dayStart,
	)
	require.NoError(t, err)
	require.Len(t, overview.Channels, 4)

	assert.Equal(t, []int{52, 51, 54, 53}, []int{
		overview.Channels[0].ChannelId,
		overview.Channels[1].ChannelId,
		overview.Channels[2].ChannelId,
		overview.Channels[3].ChannelId,
	})
	assert.Equal(t, "低倍率主线路", overview.Channels[0].ChannelRemark)
	assert.Equal(t, common.ChannelStatusEnabled, overview.Channels[0].Status)
	require.NotNil(t, overview.Channels[0].CostRatio)
	assert.InDelta(t, 0.5, *overview.Channels[0].CostRatio, 1e-9)
	assert.Nil(t, overview.Channels[2].CostRatio)
	assert.Equal(t, 1, overview.Coverage.MissingCostConfigChannelCount)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, overview.Channels[3].Status)
}

func TestGetChannelMonitorCostOverviewSummarySkipsDetailQueries(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 1, now-channelMonitorCostDaySeconds, 2_000_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCostWithProbe(context.Background(), 1, now, 1_000_000_000, 200_000_000, 1, 1))
	require.NoError(t, model.AddChannelDailyCostWithModelDetection(context.Background(), db, 1, now, 300_000_000, 300_000_000, 1, 0))
	event := model.NewChannelMonitorEvent(1, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	event.EventId = "summary-realtime-cost"
	event.RequestDispatched = true
	event.IsFinalAttempt = true
	event.CostStatus = model.ChannelMonitorEventCostSettled
	event.SettledCostNanoCNY = 1_300_000_000
	emitChannelMonitorControllerRealtimeEvents(t, event)

	var detailQueries atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:count_channel_monitor_cost_details", func(tx *gorm.DB) {
		if tx.Statement == nil {
			return
		}
		if tx.Statement.Table == "channel_daily_api_key_costs" || tx.Statement.Table == "channels" {
			detailQueries.Add(1)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove("test:count_channel_monitor_cost_details"))
	})

	ctx, recorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?days=2&summary_only=true", nil)
	GetChannelMonitorCostOverview(ctx)
	require.Equal(t, 200, recorder.Code)
	var response struct {
		Data channelMonitorCostOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.InDelta(t, 2, response.Data.YesterdayCostCNY, 1e-9)
	assert.InDelta(t, 1.3, response.Data.TodayCostCNY, 1e-9)
	assert.InDelta(t, 0.2, response.Data.TodayProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.3, response.Data.TodayModelDetectionCostCNY, 1e-9)
	assert.InDelta(t, 3.3, response.Data.TotalCostCNY, 1e-9)
	assert.InDelta(t, 0.2, response.Data.TotalProbeCostCNY, 1e-9)
	assert.InDelta(t, 0.3, response.Data.TotalModelDetectionCostCNY, 1e-9)
	assert.Equal(t, 1, response.Data.Coverage.IncludedChannelCount)
	assert.Equal(t, 1, response.Data.Coverage.UnresolvedChannelCount)
	assert.Equal(t, int64(3), response.Data.Coverage.SettledCount)
	assert.Equal(t, int64(1), response.Data.Coverage.UnresolvedCount)
	assert.Empty(t, response.Data.Channels)
	assert.Empty(t, response.Data.APIKeys)
	assert.Zero(t, detailQueries.Load())
}

func TestApplyChannelMonitorRealtimeCostPreservesPersistedLedger(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	now := common.GetTimestamp()
	todayStart := channelMonitorCostDayStart(now)
	realtime := model.NewChannelMonitorEvent(62, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	realtime.EventId = "cost-ledger-must-not-be-overwritten"
	realtime.RequestDispatched = true
	realtime.IsFinalAttempt = true
	realtime.CostStatus = model.ChannelMonitorEventCostSettled
	realtime.SettledCostNanoCNY = 9_000_000_000
	realtime.APIKeyId = 602
	realtime.APIKeyName = "其他渠道 Key"
	emitChannelMonitorControllerRealtimeEvents(t, realtime)

	persistedDay := channelMonitorCostDay{
		Date: channelMonitorCostDate(todayStart), StartAt: todayStart,
		CostCNY: 2, ProbeCostCNY: 0.25, ModelDetectionCostCNY: 0.5,
		SettledCount: 3, UnresolvedCount: 1,
	}
	overview := channelMonitorCostOverview{
		TodayCostCNY: 2, TodayProbeCostCNY: 0.25, TodayModelDetectionCostCNY: 0.5,
		TotalCostCNY: 2, TotalProbeCostCNY: 0.25, TotalModelDetectionCostCNY: 0.5,
		Coverage: channelMonitorCostCoverage{
			IncludedChannelCount: 1, UnresolvedChannelCount: 1,
			MissingCostConfigChannelCount: 4, FreeGroupChannelCount: 5,
			SettledCount: 3, UnresolvedCount: 1,
		},
		Items:      []channelMonitorCostDay{persistedDay},
		ChartItems: []channelMonitorCostDay{persistedDay},
		Channels: []channelMonitorCostChannel{{
			ChannelId: 61, ChannelName: "账本渠道", CostCNY: 2,
			ProbeCostCNY: 0.25, ModelDetectionCostCNY: 0.5,
			SettledCount: 3, UnresolvedCount: 1,
		}},
		APIKeys: []channelMonitorCostAPIKey{{
			APIKeyId: 601, APIKeyName: "账本 Key", CostCNY: 2,
			SettledCount: 3, UnresolvedCount: 1,
			Channels: []channelMonitorCostAPIKeyChannel{{
				ChannelId: 61, ChannelName: "账本渠道", CostCNY: 2,
				SettledCount: 3, UnresolvedCount: 1,
			}},
		}},
	}
	require.NoError(t, applyChannelMonitorRealtimeCost(
		context.Background(), &overview, 1, now, 61, todayStart, false,
	))

	assert.Equal(t, 2.0, overview.TodayCostCNY)
	assert.Equal(t, 0.25, overview.TodayProbeCostCNY)
	assert.Equal(t, 0.5, overview.TodayModelDetectionCostCNY)
	assert.Equal(t, 2.0, overview.TotalCostCNY)
	assert.Equal(t, 0.25, overview.TotalProbeCostCNY)
	assert.Equal(t, 0.5, overview.TotalModelDetectionCostCNY)
	assert.Equal(t, channelMonitorCostCoverage{
		IncludedChannelCount: 1, UnresolvedChannelCount: 1,
		MissingCostConfigChannelCount: 4, FreeGroupChannelCount: 5,
		SettledCount: 3, UnresolvedCount: 1,
	}, overview.Coverage)
	require.Len(t, overview.Items, 1)
	assert.Equal(t, persistedDay, overview.Items[0])
	require.Len(t, overview.ChartItems, 1)
	assert.Equal(t, persistedDay, overview.ChartItems[0])
	require.Len(t, overview.Channels, 1)
	assert.Equal(t, 61, overview.Channels[0].ChannelId)
	assert.Equal(t, 2.0, overview.Channels[0].CostCNY)
	require.Len(t, overview.APIKeys, 1)
	assert.Equal(t, 601, overview.APIKeys[0].APIKeyId)
	assert.Equal(t, 2.0, overview.APIKeys[0].CostCNY)
	require.Len(t, overview.APIKeys[0].Channels, 1)
	assert.Equal(t, 61, overview.APIKeys[0].Channels[0].ChannelId)
}

func TestGetChannelMonitorCostOverviewChannelFilterUsesPersistedAPIKeyCosts(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 61, Name: "目标渠道", Key: "target-channel-key"},
		{Id: 62, Name: "其他渠道", Key: "other-channel-key"},
	}).Error)
	now := common.GetTimestamp()
	fingerprint, display := model.ChannelDailyCostAPIKeyIdentityForToken(601, "target-channel-key")
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(
		context.Background(), 61, now, 2_000_000_000, 1, 0,
		601, "目标 Key", fingerprint, display,
	))
	realtime := model.NewChannelMonitorEvent(62, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, now)
	realtime.EventId = "other-channel-realtime-cost"
	realtime.RequestDispatched = true
	realtime.IsFinalAttempt = true
	realtime.CostStatus = model.ChannelMonitorEventCostSettled
	realtime.SettledCostNanoCNY = 9_000_000_000
	realtime.APIKeyId = 602
	realtime.APIKeyName = "其他渠道 Key"
	emitChannelMonitorControllerRealtimeEvents(t, realtime)

	date := channelMonitorCostDate(channelMonitorCostDayStart(now))
	ctx, recorder := newChannelMonitorControllerContext(
		t, "GET", "/api/channel_monitor/cost?days=1&channel_id=61&date="+date, nil,
	)
	GetChannelMonitorCostOverview(ctx)
	require.Equal(t, 200, recorder.Code)
	var response struct {
		Data channelMonitorCostOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))

	assert.InDelta(t, 2, response.Data.TodayCostCNY, 1e-9)
	assert.InDelta(t, 2, response.Data.TotalCostCNY, 1e-9)
	require.Len(t, response.Data.Channels, 1)
	assert.Equal(t, 61, response.Data.Channels[0].ChannelId)
	require.Len(t, response.Data.APIKeys, 1)
	assert.Equal(t, 601, response.Data.APIKeys[0].APIKeyId)
	assert.InDelta(t, 2, response.Data.APIKeys[0].CostCNY, 1e-9)
	require.Len(t, response.Data.APIKeys[0].Channels, 1)
	assert.Equal(t, 61, response.Data.APIKeys[0].Channels[0].ChannelId)
	assert.InDelta(t, 2, response.Data.APIKeys[0].Channels[0].CostCNY, 1e-9)
	assert.Equal(t, 1, response.Data.Coverage.MissingCostConfigChannelCount)
}

func TestGetChannelMonitorCostOverviewAccumulatesAPIKeyCostsAcrossDays(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 71, Name: "跨日渠道", Key: "multi-day-key"}).Error)
	now := common.GetTimestamp()
	todayStart := channelMonitorCostDayStart(now)
	yesterdayStart := todayStart - channelMonitorCostDaySeconds
	fingerprint, display := model.ChannelDailyCostAPIKeyIdentityForToken(701, "multi-day-key")
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(
		context.Background(), 71, yesterdayStart+60, 2_000_000_000, 1, 0,
		701, "跨日 Key", fingerprint, display,
	))
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(
		context.Background(), 71, todayStart+60, 3_000_000_000, 1, 0,
		701, "跨日 Key", fingerprint, display,
	))
	realtime := model.NewChannelMonitorEvent(71, model.ChannelMonitorEventSourceBusiness, model.ChannelMonitorEventOutcomeSuccess, todayStart+60)
	realtime.EventId = "multi-day-today-realtime-cost"
	realtime.RequestDispatched = true
	realtime.IsFinalAttempt = true
	realtime.CostStatus = model.ChannelMonitorEventCostSettled
	realtime.SettledCostNanoCNY = 3_000_000_000
	realtime.APIKeyId = 701
	realtime.APIKeyName = "跨日 Key"
	emitChannelMonitorControllerRealtimeEvents(t, realtime)

	ctx, recorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?days=2", nil)
	GetChannelMonitorCostOverview(ctx)
	require.Equal(t, 200, recorder.Code)
	var response struct {
		Data channelMonitorCostOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))

	require.Len(t, response.Data.APIKeys, 1)
	assert.Equal(t, 701, response.Data.APIKeys[0].APIKeyId)
	assert.InDelta(t, 5, response.Data.APIKeys[0].CostCNY, 1e-9)
	assert.Equal(t, int64(2), response.Data.APIKeys[0].SettledCount)
	require.Len(t, response.Data.APIKeys[0].Channels, 1)
	assert.Equal(t, 71, response.Data.APIKeys[0].Channels[0].ChannelId)
	assert.InDelta(t, 5, response.Data.APIKeys[0].Channels[0].CostCNY, 1e-9)
	assert.Equal(t, int64(2), response.Data.APIKeys[0].Channels[0].SettledCount)
}

func TestGetChannelMonitorCostOverviewGroupsAPIKeysAcrossChannelsWithoutSecrets(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channelAlphaRemark := "  主力线路  "
	channelBetaRemark := "备用线路"
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 21, Name: "渠道甲", Key: "sk-channel-alpha", Remark: &channelAlphaRemark},
		{Id: 22, Name: "渠道乙", Key: "sk-channel-beta", Remark: &channelBetaRemark},
	}).Error)

	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	keyA, displayA := model.ChannelDailyCostAPIKeyIdentityForToken(101, "sk-channel-alpha")
	keyB, displayB := model.ChannelDailyCostAPIKeyIdentityForToken(102, "sk-channel-beta")
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(context.Background(), 21, now, 1_500_000_000, 1, 0, 101, "生产 Key", keyA, displayA))
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(context.Background(), 21, now, 500_000_000, 1, 0, 102, "备用 Key", keyB, displayB))
	require.NoError(t, model.AddChannelDailyCostWithAPIKeyAndToken(context.Background(), 22, now, 2_000_000_000, 1, 1, 101, "生产 Key", keyA, displayA))

	overview, err := getChannelMonitorCostOverview(context.Background(), 1, now)
	require.NoError(t, err)
	require.Len(t, overview.APIKeys, 2)
	assert.Equal(t, 101, overview.APIKeys[0].APIKeyId)
	assert.Equal(t, "生产 Key", overview.APIKeys[0].APIKeyName)
	assert.InDelta(t, 3.5, overview.APIKeys[0].CostCNY, 1e-9)
	require.Len(t, overview.APIKeys[0].Channels, 2)
	assert.Equal(t, 22, overview.APIKeys[0].Channels[0].ChannelId)
	assert.Equal(t, "备用线路", overview.APIKeys[0].Channels[0].ChannelRemark)
	assert.Equal(t, int64(1), overview.APIKeys[0].Channels[0].UnresolvedCount)
	assert.Equal(t, 21, overview.APIKeys[0].Channels[1].ChannelId)
	assert.Equal(t, "主力线路", overview.APIKeys[0].Channels[1].ChannelRemark)
	assert.Equal(t, 102, overview.APIKeys[1].APIKeyId)
	assert.Equal(t, "备用 Key", overview.APIKeys[1].APIKeyName)
	assert.Equal(t, displayA, overview.APIKeys[0].APIKey)
	assert.NotContains(t, overview.APIKeys[0].APIKey, "sk-channel")
	assert.NotContains(t, overview.APIKeys[1].APIKey, "sk-channel")

	ctx, recorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?days=90", nil)
	GetChannelMonitorCostOverview(ctx)
	require.Equal(t, 200, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "sk-channel-alpha")
	assert.NotContains(t, recorder.Body.String(), "sk-channel-beta")
	assert.Contains(t, recorder.Body.String(), displayA)

	filteredCtx, filteredRecorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?days=90&channel_id=21", nil)
	GetChannelMonitorCostOverview(filteredCtx)
	require.Equal(t, 200, filteredRecorder.Code)
	var filteredResponse struct {
		Data channelMonitorCostOverview `json:"data"`
	}
	require.NoError(t, common.Unmarshal(filteredRecorder.Body.Bytes(), &filteredResponse))
	require.Len(t, filteredResponse.Data.Channels, 1)
	assert.Equal(t, 21, filteredResponse.Data.Channels[0].ChannelId)
	assert.InDelta(t, 2, filteredResponse.Data.Channels[0].CostCNY, 1e-9)
	require.Len(t, filteredResponse.Data.APIKeys, 2)
	for _, apiKey := range filteredResponse.Data.APIKeys {
		require.Len(t, apiKey.Channels, 1)
		assert.Equal(t, 21, apiKey.Channels[0].ChannelId)
		assert.Equal(t, "主力线路", apiKey.Channels[0].ChannelRemark)
	}
}

func TestGetChannelMonitorCostOverviewKeepsUnattributedChannelsVisible(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	legacyChannelRemark := "历史补录"
	adminChannelRemark := "后台测试"
	require.NoError(t, db.Create(&[]model.Channel{
		{Id: 31, Name: "旧记录渠道", Key: "key-31", Remark: &legacyChannelRemark},
		{Id: 32, Name: "后台测试渠道", Key: "key-32", Remark: &adminChannelRemark},
	}).Error)
	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 31, now, 2_000_000_000, 2, 0))
	fingerprint, display := model.ChannelDailyCostAPIKeyIdentity("sk-test-upstream")
	require.NoError(t, model.AddChannelDailyCostWithAPIKey(context.Background(), 32, now, 1_000_000_000, 1, 0, fingerprint, display))

	overview, err := getChannelMonitorCostOverview(context.Background(), 1, now)
	require.NoError(t, err)
	require.Len(t, overview.APIKeys, 2)
	byName := make(map[string]channelMonitorCostAPIKey, len(overview.APIKeys))
	for _, item := range overview.APIKeys {
		byName[item.APIKeyName] = item
	}
	unattributed := byName["未识别 API Key"]
	require.Len(t, unattributed.Channels, 1)
	assert.Equal(t, 31, unattributed.Channels[0].ChannelId)
	assert.Equal(t, "历史补录", unattributed.Channels[0].ChannelRemark)
	assert.InDelta(t, 2, unattributed.Channels[0].CostCNY, 1e-9)
	legacy := byName["上游 Key "+display]
	require.Len(t, legacy.Channels, 1)
	assert.Equal(t, 32, legacy.Channels[0].ChannelId)
	assert.Equal(t, "后台测试", legacy.Channels[0].ChannelRemark)
}

func TestGetChannelMonitorCostOverviewRejectsAPIKeyAttributionAboveChannelTotal(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	dayStart := channelMonitorCostDayStart(now)
	fingerprint, display := model.ChannelDailyCostAPIKeyIdentityForToken(91, "sk-inconsistent")
	require.NoError(t, db.Create(&model.ChannelDailyCost{
		ChannelId: 91, DayStart: dayStart, CostNanoCNY: 100, SettledCount: 1,
		CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelDailyAPIKeyCost{
		ChannelId: 91, DayStart: dayStart, APIKeyId: 91, APIKeyName: "异常 Key",
		KeyFingerprint: fingerprint, KeyDisplay: display,
		CostNanoCNY: 101, SettledCount: 1, CreatedAt: now, UpdatedAt: now,
	}).Error)

	_, err := getChannelMonitorCostOverview(context.Background(), 1, now)
	require.ErrorContains(t, err, "API Key 成本归属超过渠道总额")
}

func TestGetChannelMonitorCostOverviewRejectsInvalidDays(t *testing.T) {
	setupChannelMonitorControllerTestDB(t)
	for _, days := range []string{"0", "91", "invalid"} {
		t.Run(days, func(t *testing.T) {
			ctx, recorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?days="+days, nil)

			GetChannelMonitorCostOverview(ctx)

			assert.Equal(t, 400, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "统计天数必须在 1 到 90 之间")
		})
	}
	ctx, recorder := newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?page=0", nil)
	GetChannelMonitorCostOverview(ctx)
	assert.Equal(t, 400, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "页码必须为正整数")

	ctx, recorder = newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?summary_only=invalid", nil)
	GetChannelMonitorCostOverview(ctx)
	assert.Equal(t, 400, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "摘要模式参数必须为布尔值")

	for _, detailDate := range []string{"invalid", "1970-01-01"} {
		ctx, recorder = newChannelMonitorControllerContext(t, "GET", "/api/channel_monitor/cost?days=7&date="+detailDate, nil)
		GetChannelMonitorCostOverview(ctx)
		assert.Equal(t, 400, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "统计日期必须在所选时间范围内")
	}
}

func TestGetChannelMonitorCostOverviewUsesServerPaginationForDates(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 31, Name: "分页渠道", Key: "key-31"}).Error)
	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	for index := 0; index < 10; index++ {
		require.NoError(t, model.AddChannelDailyCost(
			context.Background(),
			31,
			now-int64(index)*channelMonitorCostDaySeconds,
			int64(index+1)*100_000_000,
			1,
			0,
		))
	}

	firstPage, err := getChannelMonitorCostOverviewPage(context.Background(), 10, now, 1, 3)
	require.NoError(t, err)
	assert.Equal(t, 1, firstPage.ItemPage)
	assert.Equal(t, 3, firstPage.ItemPageSize)
	assert.Equal(t, 4, firstPage.ItemPageCount)
	assert.Equal(t, 10, firstPage.ItemTotal)
	require.Len(t, firstPage.Items, 3)
	assert.Equal(t, 10, len(firstPage.ChartItems))

	lastPage, err := getChannelMonitorCostOverviewPage(context.Background(), 10, now, 4, 3)
	require.NoError(t, err)
	assert.Equal(t, 4, lastPage.ItemPage)
	require.Len(t, lastPage.Items, 1)
	assert.Equal(t, firstPage.Items[0].StartAt-channelMonitorCostDaySeconds*7, lastPage.Items[0].StartAt)
}
