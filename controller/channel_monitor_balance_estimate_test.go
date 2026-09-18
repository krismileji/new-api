package controller

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The released 39485b5be monitor schema differs by these two retired fields.
// They are a migration fixture only, never read by the new runtime.
type releasedChannelBalanceMonitor struct {
	model.ChannelRatioMonitor
	LastBalanceCostNanoCNY    *int64 `gorm:"bigint"`
	BalancePendingConsumption float64
}

func (releasedChannelBalanceMonitor) TableName() string { return "channel_ratio_monitors" }

func verifyChannelMonitorBalanceSnapshotUpgrade(t *testing.T, db *gorm.DB) {
	require.False(t, db.Migrator().HasColumn(&model.ChannelRatioMonitor{}, "last_balance_cost_nano_cny"))
	require.False(t, db.Migrator().HasColumn(&model.ChannelRatioMonitor{}, "balance_pending_consumption"))
	for n := 0; n < 2; n++ {
		require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}))
	}
	// Use only the disposable database owned by the matrix fixture.
	require.NoError(t, db.Migrator().DropTable(&model.ChannelRatioMonitor{}))
	require.NoError(t, db.AutoMigrate(&releasedChannelBalanceMonitor{}))
	old := releasedChannelBalanceMonitor{ChannelRatioMonitor: model.ChannelRatioMonitor{
		ChannelId: 51, Ratio: 4, UpstreamRevision: 7, Remark: "升级保留数据", UpstreamBalance: common.GetPointer(24.374499),
	}, LastBalanceCostNanoCNY: common.GetPointer(int64(3_264_000_000)), BalancePendingConsumption: 120}
	require.NoError(t, db.Create(&old).Error)
	for n := 0; n < 2; n++ {
		require.NoError(t, db.AutoMigrate(&model.ChannelRatioMonitor{}))
	}
	stored, err := model.GetChannelRatioMonitor(51)
	require.NoError(t, err)
	assert.Equal(t, old.Remark, stored.Remark)
	assert.Equal(t, old.Ratio, stored.Ratio)
	assert.Equal(t, old.UpstreamBalance, stored.UpstreamBalance)
	require.Error(t, db.Create(&model.ChannelRatioMonitor{ChannelId: 51}).Error, "the channel uniqueness guarantee survives migration")
	newBalance := 22.374499
	applied, err := model.RecordChannelRatioMonitorBalanceIfRevision(t.Context(), 51, 7, &newBalance, "")
	require.NoError(t, err)
	require.True(t, applied)
	stored, err = model.GetChannelRatioMonitor(51)
	require.NoError(t, err)
	assert.Equal(t, newBalance, *stored.UpstreamBalance)
	serialized, err := common.Marshal(stored)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "pending_consumption")
	assert.NotContains(t, string(serialized), "cost_nano")
}

func verifyChannelMonitorBalanceSettlementOrder(t *testing.T, db *gorm.DB) {
	conversion, err := service.MarshalChannelMonitorCostConversion(service.ChannelMonitorCostConversion{
		Mode: service.ChannelMonitorCostConversionSubscription, SubscriptionPeriod: service.ChannelMonitorSubscriptionPeriodMonth,
		SubscriptionPriceCNY: 408, SubscriptionDailyUSD: 500,
	})
	require.NoError(t, err)
	channel := model.Channel{Id: 41, Name: "余额预估", Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "vip", Model: "model-a", ChannelId: channel.Id, Enabled: true}).Error)
	monitor := model.ChannelRatioMonitor{
		ChannelId: channel.Id, UpstreamRevision: 1, UpstreamType: service.NewAPIUpstreamType,
		Ratio: 4, UpdatedTime: 1, CostConversion: conversion,
		BalanceWarningThreshold: common.GetPointer(30.0), BalanceAutoDisableThreshold: common.GetPointer(5.0),
	}
	require.NoError(t, db.Create(&monitor).Error)
	balance := 24.374499
	evaluation, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, &balance, "")
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, evaluation.Complete)
	// Reproduces the original first-snapshot/late-ledger bug: the provider's
	// balance already includes 60 completed requests at 2 upstream credits each.
	require.NoError(t, model.AddChannelDailyCost(t.Context(), channel.Id, common.GetTimestamp(), 3_264_000_000, 60, 0))
	checked, err := evaluateChannelMonitorBalance(t.Context(), monitor, balance)
	require.NoError(t, err)
	assert.Equal(t, balance, checked.EffectiveBalance)
	assert.Zero(t, checked.EstimatedConsumption)
	disabled, err := autoDisableChannelMonitorForLowBalanceWithContext(t.Context(), monitor, &channel, balance)
	require.NoError(t, err)
	assert.False(t, disabled)

	// A new request really consumes 2 credits: 0.5 base dollars x upstream 4;
	// CNY cost is 0.0544 and must not be mistaken for the balance's credit unit.
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	service.CaptureChannelDailyCostSnapshot(ctx, channel.Id)
	service.BeginChannelDailyCostAttempt(ctx, channel.Id)
	service.MarkChannelDailyCostRequestDispatched(ctx)
	service.RecordPerCallChannelDailyCost(ctx, channel.Id, "model-a", types.PriceData{UsePrice: true, ModelPrice: 0.5})
	t.Cleanup(func() { assert.NoError(t, service.FlushChannelDailyCostEvents()) })
	estimate, err := service.GetChannelBalanceEstimate(t.Context(), service.ChannelBalanceConfigForMonitor(monitor))
	require.NoError(t, err)
	assert.Equal(t, 2.0, estimate.CompletedConsumption)
	assert.Zero(t, estimate.InFlightConsumption)
	assert.Equal(t, 22.374499, estimate.EstimatedBalance)

	balance = 22.374499
	evaluation, applied, err = recordChannelMonitorBalanceUpdate(t.Context(), monitor, &balance, "")
	require.NoError(t, err)
	require.True(t, applied)
	assert.Zero(t, evaluation.EstimatedConsumption)
	assert.Equal(t, balance, evaluation.EffectiveBalance)
	// Once the live state is ready, repeated reads/threshold checks do not
	// query the ledger, monitor table or channel table when status is unchanged.
	var queries atomic.Int32
	const callback = "balance_estimate:no_extra_reads"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(*gorm.DB) { queries.Add(1) }))
	t.Cleanup(func() { assert.NoError(t, db.Callback().Query().Remove(callback)) })
	_, err = evaluateChannelMonitorBalance(t.Context(), monitor, balance)
	require.NoError(t, err)
	disabled, err = autoDisableChannelMonitorForLowBalanceWithContext(t.Context(), monitor, &channel, balance)
	require.NoError(t, err)
	assert.False(t, disabled)
	assert.Zero(t, queries.Load())

	// Actual threshold transitions still update the persisted channel and its
	// abilities together. Manual disable remains protected by the status guard.
	disabled, err = autoDisableChannelMonitorAtEffectiveBalance(monitor, &channel, balance, 4, balance-4)
	require.NoError(t, err)
	assert.True(t, disabled)
	stored, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
	var ability model.Ability
	require.NoError(t, db.First(&ability, "channel_id = ?", channel.Id).Error)
	assert.False(t, ability.Enabled)
}

func verifyChannelMonitorBalanceDisableThresholdIndependentOfWarning(t *testing.T, db *gorm.DB) {
	useChannelMonitorOptionMap(t, map[string]string{})
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios)) })
	for index, test := range []struct {
		name    string
		warning *float64
	}{
		{"未设置预警值", nil},
		{"余额高于预警值", common.GetPointer(1.0)},
		{"余额等于预警值", common.GetPointer(24.0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := model.Channel{Id: 701 + index, Name: test.name, Group: "vip", Models: "model-a", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Group: "vip", Model: "model-a", Enabled: true}).Error)
			monitor := model.ChannelRatioMonitor{ChannelId: channel.Id, UpstreamRevision: 1, UpstreamType: service.NewAPIUpstreamType,
				Ratio: 1, UpdatedTime: 1, BalanceWarningThreshold: test.warning, BalanceAutoDisableThreshold: common.GetPointer(5.0)}
			require.NoError(t, db.Create(&monitor).Error)
			evaluation, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(24.0), "")
			require.NoError(t, err)
			require.True(t, applied)
			require.True(t, evaluation.Complete)
			t.Cleanup(func() { assert.NoError(t, service.FlushChannelDailyCostEvents()) })

			var crossing *gin.Context
			for _, price := range []float64{19, 1} {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				service.CaptureChannelDailyCostSnapshot(ctx, channel.Id)
				service.BeginChannelDailyCostAttempt(ctx, channel.Id)
				service.PrepareChannelBalanceAttempt(ctx, &relaycommon.RelayInfo{
					ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}, OriginModelName: "model-a",
					PriceData: types.PriceData{UsePrice: true, ModelPrice: price},
				})
				service.MarkChannelDailyCostRequestDispatched(ctx)
				if price == 19 {
					evaluation, err := evaluateChannelMonitorBalance(t.Context(), monitor, 24)
					require.NoError(t, err)
					assert.Equal(t, 5.0, evaluation.EffectiveBalance)
					disabled, err := autoDisableChannelMonitorForLowBalanceWithContext(t.Context(), monitor, &channel, 24)
					require.NoError(t, err)
					assert.False(t, disabled, "等于禁用阈值时保持启用")
				}
				crossing = ctx
			}
			config := service.ChannelBalanceConfigForMonitor(monitor)
			estimate, err := service.GetChannelBalanceEstimate(t.Context(), config)
			require.NoError(t, err)
			assert.Equal(t, 4.0, estimate.EstimatedBalance)
			assert.Equal(t, "low", estimate.Decision)
			handled, err := applyChannelBalanceRealtimePolicy(t.Context(), config, estimate)
			require.NoError(t, err)
			require.True(t, handled)
			stored, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
			assert.Contains(t, stored.GetOtherInfo()["status_reason"], "估算余额 4")
			var ability model.Ability
			require.NoError(t, db.First(&ability, "channel_id = ?", channel.Id).Error)
			assert.False(t, ability.Enabled)
			allowed, err := channelMonitorAllowsHealthCheckAutoEnable(channel.Id)
			require.NoError(t, err)
			assert.False(t, allowed, "健康检查不能绕过进行中费用重新启用渠道")

			service.RecordPerCallChannelDailyCost(crossing, channel.Id, "model-a", types.PriceData{UsePrice: true, ModelPrice: 0})
			estimate, err = service.GetChannelBalanceEstimate(t.Context(), config)
			require.NoError(t, err)
			assert.Equal(t, 5.0, estimate.EstimatedBalance)
			allowed, err = channelMonitorAllowsHealthCheckAutoEnable(channel.Id)
			require.NoError(t, err)
			assert.True(t, allowed)
			handled, err = applyChannelBalanceRealtimePolicy(t.Context(), config, estimate)
			require.NoError(t, err)
			require.True(t, handled)
			stored, err = model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
			require.NoError(t, db.First(&ability, "channel_id = ?", channel.Id).Error)
			assert.True(t, ability.Enabled)
		})
	}
}

func TestChannelMonitorBalanceEstimateUnavailableBlocksRecovery(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{Id: 41, Name: "未知预估", Group: "vip", Status: common.ChannelStatusAutoDisabled}
	channel.SetOtherInfo(map[string]interface{}{"status_reason": channelMonitorBalancePolicyDisableReasonPrefix + "3" + channelMonitorBalancePolicyDisableThresholdMarker + "5"})
	require.NoError(t, db.Create(&channel).Error)
	monitor := model.ChannelRatioMonitor{ChannelId: channel.Id, UpstreamType: service.NewAPIUpstreamType, UpstreamRevision: 1,
		UpstreamBalance: common.GetPointer(10.0), BalanceAutoDisableThreshold: common.GetPointer(5.0)}
	require.NoError(t, db.Create(&monitor).Error)
	allowed, err := channelMonitorAllowsHealthCheckAutoEnable(channel.Id)
	require.ErrorIs(t, err, service.ErrChannelBalanceUnavailable)
	assert.False(t, allowed)
}

func TestChannelMonitorBalanceResponseOrderingIncludesFailures(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	monitor := model.ChannelRatioMonitor{ChannelId: 52, UpstreamType: service.NewAPIUpstreamType, UpstreamRevision: 1,
		BalanceWarningThreshold: common.GetPointer(30.0), BalanceAutoDisableThreshold: common.GetPointer(5.0)}
	require.NoError(t, db.Create(&monitor).Error)
	older, err := service.BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	newer, err := service.BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	_, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(20.0), "", newer)
	require.NoError(t, err)
	require.True(t, applied)
	_, applied, err = recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(100.0), "", older)
	require.NoError(t, err)
	assert.False(t, applied)
	_, applied, err = recordChannelMonitorBalanceUpdate(t.Context(), monitor, nil, "迟到的超时", older)
	require.NoError(t, err)
	assert.False(t, applied)
	stored, err := model.GetChannelRatioMonitor(monitor.ChannelId)
	require.NoError(t, err)
	assert.Equal(t, 20.0, *stored.UpstreamBalance)
	assert.Empty(t, stored.LastBalanceError)
	assert.Zero(t, stored.BalanceConsecutiveFailures)
	failed, err := service.BeginChannelBalanceSync(t.Context(), monitor)
	require.NoError(t, err)
	_, applied, err = recordChannelMonitorBalanceUpdate(t.Context(), monitor, nil, "当前超时", failed)
	require.NoError(t, err)
	require.True(t, applied)
	evaluation, err := evaluateChannelMonitorBalance(t.Context(), monitor, 20)
	require.NoError(t, err)
	assert.False(t, evaluation.Complete)
	stored, err = model.GetChannelRatioMonitor(monitor.ChannelId)
	require.NoError(t, err)
	assert.Equal(t, 20.0, *stored.UpstreamBalance)
	assert.Equal(t, "当前超时", stored.LastBalanceError)
}

func TestChannelMonitorBalanceWithoutRedisStillSavesRawSnapshot(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	monitor := model.ChannelRatioMonitor{ChannelId: 53, UpstreamType: service.NewAPIUpstreamType, UpstreamRevision: 1,
		BalanceWarningThreshold: common.GetPointer(30.0), BalanceAutoDisableThreshold: common.GetPointer(5.0)}
	require.NoError(t, db.Create(&monitor).Error)
	previousRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previousRedisEnabled })
	evaluation, applied, err := recordChannelMonitorBalanceUpdate(t.Context(), monitor, common.GetPointer(24.0), "")
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, 24.0, evaluation.EffectiveBalance)
	assert.False(t, evaluation.Complete)
	stored, err := model.GetChannelRatioMonitor(monitor.ChannelId)
	require.NoError(t, err)
	assert.Equal(t, 24.0, *stored.UpstreamBalance)
}
