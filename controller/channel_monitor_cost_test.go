package controller

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetChannelMonitorOverviewIncludesTodayCostState(t *testing.T) {
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
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 1, yesterday+60, 500_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 1, today, 1_250_000_000, 1, 0))
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 2, today, 0, 0, 1))
	require.NoError(t, model.AddChannelDailyCost(context.Background(), 3, today, 0, 1, 0))

	now := time.Date(2026, 7, 22, 4, 0, 0, 0, time.UTC).Unix()
	overview, err := getChannelMonitorCostOverview(context.Background(), 2, now)
	require.NoError(t, err)
	require.Len(t, overview.Items, 2)
	assert.Equal(t, "2026-07-21", overview.Items[0].Date)
	assert.Equal(t, "2026-07-22", overview.Items[1].Date)
	assert.InDelta(t, 3, overview.YesterdayCostCNY, 1e-9)
	assert.InDelta(t, 1.25, overview.TodayCostCNY, 1e-9)
	assert.InDelta(t, 4.25, overview.TotalCostCNY, 1e-9)
	assert.Equal(t, int64(1), overview.Items[1].UnresolvedCount)
	assert.Equal(t, 2, overview.Coverage.IncludedChannelCount)
	assert.Equal(t, 1, overview.Coverage.UnresolvedChannelCount)
	assert.Zero(t, overview.Coverage.FreeGroupChannelCount)
	require.Len(t, overview.Channels, 3)
	assert.Equal(t, 1, overview.Channels[0].ChannelId)
	assert.Equal(t, "已结算渠道", overview.Channels[0].ChannelName)
	assert.InDelta(t, 4.25, overview.Channels[0].CostCNY, 1e-9)
	assert.Equal(t, int64(3), overview.Channels[0].SettledCount)
	assert.Equal(t, 2, overview.Channels[1].ChannelId)
	assert.Equal(t, int64(1), overview.Channels[1].UnresolvedCount)
	assert.Equal(t, 3, overview.Channels[2].ChannelId)
	assert.Equal(t, int64(1), overview.Channels[2].SettledCount)
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
}
