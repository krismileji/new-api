package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func resetChannelRatioMonitorTables(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Channel{}, &ChannelRatioMonitor{}, &ChannelRatioHistory{}))
	for _, value := range []interface{}{&ChannelRatioHistory{}, &ChannelRatioMonitor{}, &Channel{}} {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(value).Error)
	}
	t.Cleanup(func() {
		for _, value := range []interface{}{&ChannelRatioHistory{}, &ChannelRatioMonitor{}, &Channel{}} {
			require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(value).Error)
		}
	})
}

func TestUpdateChannelRatioMonitorTracksOnlyRatioChanges(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	monitor, created, changed, err := UpdateChannelRatioMonitor(10, 1.1, "baseline", 1, "root")
	require.NoError(t, err)
	assert.True(t, created)
	assert.False(t, changed)
	assert.Equal(t, 1.1, monitor.Ratio)
	assert.Nil(t, monitor.PreviousRatio)

	monitor, created, changed, err = UpdateChannelRatioMonitor(10, 1.1, "remark only", 1, "root")
	require.NoError(t, err)
	assert.False(t, created)
	assert.False(t, changed)
	assert.Equal(t, "remark only", monitor.Remark)
	assert.Nil(t, monitor.PreviousRatio)

	monitor, created, changed, err = UpdateChannelRatioMonitor(10, 1.25, "upstream changed", 2, "operator")
	require.NoError(t, err)
	assert.False(t, created)
	assert.True(t, changed)
	require.NotNil(t, monitor.PreviousRatio)
	assert.Equal(t, 1.1, *monitor.PreviousRatio)
	assert.Equal(t, 1.25, monitor.Ratio)

	monitor, created, changed, err = UpdateChannelRatioMonitor(10, 1.25, "confirmed", 2, "operator")
	require.NoError(t, err)
	assert.False(t, created)
	assert.False(t, changed)
	require.NotNil(t, monitor.PreviousRatio)
	assert.Equal(t, 1.1, *monitor.PreviousRatio)

	history, total, err := GetChannelRatioHistory(10, 0, 100)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, 1.1, history[0].OldRatio)
	assert.Equal(t, 1.25, history[0].NewRatio)
	assert.Equal(t, "upstream changed", history[0].Remark)
	assert.Equal(t, 2, history[0].OperatorId)
}

func TestChannelRatioMonitorFetchStatusTracksFailureAndRecovery(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	_, _, _, err := UpdateChannelRatioMonitor(10, 1.1, "manual baseline", 1, "root")
	require.NoError(t, err)
	require.NoError(t, RecordChannelRatioMonitorFetchFailure(10, "upstream timeout"))

	monitor, err := GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, ChannelRatioFetchStatusFailed, monitor.LastFetchStatus)
	assert.Equal(t, "upstream timeout", monitor.LastFetchError)
	assert.NotZero(t, monitor.LastFetchTime)
	assert.Equal(t, 1, monitor.ConsecutiveFailures)

	require.NoError(t, RecordChannelRatioMonitorFetchFailure(10, "upstream returned 502"))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, 2, monitor.ConsecutiveFailures)
	assert.Equal(t, "upstream returned 502", monitor.LastFetchError)

	_, _, _, err = UpdateChannelRatioMonitor(10, 1.2, "manual correction", 1, "root")
	require.NoError(t, err)
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, ChannelRatioFetchStatusFailed, monitor.LastFetchStatus)
	assert.Equal(t, 2, monitor.ConsecutiveFailures)

	monitor, _, changed, err := UpdateChannelRatioMonitorFromUpstream(10, 1.2, "upstream recovered", 0, "系统自动更新")
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, ChannelRatioFetchStatusSucceeded, monitor.LastFetchStatus)
	assert.Empty(t, monitor.LastFetchError)
	assert.NotZero(t, monitor.LastFetchTime)
	assert.Zero(t, monitor.ConsecutiveFailures)
}

func TestChannelRatioMonitorBalanceKeepsLastValueWhenRefreshFails(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	balance := 12.75
	require.NoError(t, RecordChannelRatioMonitorBalance(10, &balance, ""))
	monitor, err := GetChannelRatioMonitor(10)
	require.NoError(t, err)
	require.NotNil(t, monitor.UpstreamBalance)
	assert.InDelta(t, balance, *monitor.UpstreamBalance, 1e-9)
	assert.NotZero(t, monitor.LastBalanceTime)
	assert.Empty(t, monitor.LastBalanceError)
	assert.Zero(t, monitor.BalanceConsecutiveFailures)
	lastBalanceTime := monitor.LastBalanceTime

	require.NoError(t, RecordChannelRatioMonitorBalance(10, nil, "upstream timeout"))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	require.NotNil(t, monitor.UpstreamBalance)
	assert.InDelta(t, balance, *monitor.UpstreamBalance, 1e-9)
	assert.Equal(t, lastBalanceTime, monitor.LastBalanceTime)
	assert.Equal(t, "upstream timeout", monitor.LastBalanceError)
	assert.Equal(t, 1, monitor.BalanceConsecutiveFailures)

	require.NoError(t, RecordChannelRatioMonitorBalance(10, nil, "upstream returned 502"))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, 2, monitor.BalanceConsecutiveFailures)

	recoveredBalance := 15.0
	require.NoError(t, RecordChannelRatioMonitorBalance(10, &recoveredBalance, ""))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Empty(t, monitor.LastBalanceError)
	assert.Zero(t, monitor.BalanceConsecutiveFailures)
}

func TestChannelRatioUpstreamConfigChangeResetsRelatedFetchFailures(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId:                  10,
		UpstreamType:               "new_api",
		UpstreamBaseURL:            "https://upstream.example",
		UpstreamGroup:              "default",
		UpstreamAuthType:           "user",
		UpstreamUserId:             7,
		UpstreamAccessToken:        "old-token",
		LastFetchStatus:            ChannelRatioFetchStatusFailed,
		LastFetchError:             "倍率失败",
		LastFetchTime:              100,
		ConsecutiveFailures:        2,
		LastBalanceError:           "余额失败",
		BalanceConsecutiveFailures: 2,
	}).Error)

	monitor, err := SaveChannelRatioUpstreamConfig(
		10,
		"new_api",
		"https://upstream.example",
		"vip",
		"user",
		7,
		"old-token",
		ChannelRatioUpstreamOptions{RatioSyncEnabled: true, BalanceSyncEnabled: true},
	)
	require.NoError(t, err)
	assert.Zero(t, monitor.ConsecutiveFailures)
	assert.Empty(t, monitor.LastFetchStatus)
	assert.Empty(t, monitor.LastFetchError)
	assert.Zero(t, monitor.LastFetchTime)
	assert.Equal(t, 2, monitor.BalanceConsecutiveFailures)
	assert.Equal(t, "余额失败", monitor.LastBalanceError)

	monitor, err = SaveChannelRatioUpstreamConfig(
		10,
		"new_api",
		"https://upstream.example",
		"vip",
		"user",
		7,
		"new-token",
		ChannelRatioUpstreamOptions{RatioSyncEnabled: true, BalanceSyncEnabled: true},
	)
	require.NoError(t, err)
	assert.Zero(t, monitor.BalanceConsecutiveFailures)
	assert.Empty(t, monitor.LastBalanceError)
}

func TestChannelRatioMonitorBalanceAlertResetsAfterRecoveryOrThresholdChange(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	threshold := 10.0
	autoDisableThreshold := 5.0
	_, err := SaveChannelRatioUpstreamConfig(
		10,
		"new_api",
		"https://upstream.example",
		"vip",
		"user",
		7,
		"dashboard-token",
		ChannelRatioUpstreamOptions{
			SingleChannelAction:         "none",
			MultipleChannelsAction:      "none",
			BalanceWarningThreshold:     &threshold,
			BalanceAutoDisableThreshold: &autoDisableThreshold,
			RatioSyncEnabled:            true,
			BalanceSyncEnabled:          true,
		},
	)
	require.NoError(t, err)

	lowBalance := 5.0
	require.NoError(t, RecordChannelRatioMonitorBalance(10, &lowBalance, ""))
	require.NoError(t, MarkChannelRatioMonitorBalanceAlertsNotified([]int{10}))

	monitor, err := GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.True(t, monitor.BalanceAlertNotified)
	require.NotNil(t, monitor.BalanceAutoDisableThreshold)
	assert.Equal(t, autoDisableThreshold, *monitor.BalanceAutoDisableThreshold)

	stillLowBalance := 9.99
	require.NoError(t, RecordChannelRatioMonitorBalance(10, &stillLowBalance, ""))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.True(t, monitor.BalanceAlertNotified)

	recoveredBalance := threshold
	require.NoError(t, RecordChannelRatioMonitorBalance(10, &recoveredBalance, ""))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.False(t, monitor.BalanceAlertNotified)

	require.NoError(t, MarkChannelRatioMonitorBalanceAlertsNotified([]int{10}))
	newThreshold := 12.0
	monitor, err = SaveChannelRatioUpstreamConfig(
		10,
		"new_api",
		"https://upstream.example",
		"vip",
		"user",
		7,
		"dashboard-token",
		ChannelRatioUpstreamOptions{
			SingleChannelAction:         "none",
			MultipleChannelsAction:      "none",
			BalanceWarningThreshold:     &newThreshold,
			BalanceAutoDisableThreshold: &autoDisableThreshold,
			RatioSyncEnabled:            true,
			BalanceSyncEnabled:          true,
		},
	)
	require.NoError(t, err)
	assert.False(t, monitor.BalanceAlertNotified)
	require.NotNil(t, monitor.BalanceWarningThreshold)
	assert.Equal(t, newThreshold, *monitor.BalanceWarningThreshold)
}

func TestChannelRatioUpstreamConfigDoesNotCreateFalseBaseline(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	monitor, err := SaveChannelRatioUpstreamConfig(11, "new_api", "https://upstream.example", "vip", "user", 7, "dashboard-token", ChannelRatioUpstreamOptions{
		SingleChannelAction:    "update_group_ratio",
		MultipleChannelsAction: "disable_channel",
		RatioSyncEnabled:       true,
		BalanceSyncEnabled:     true,
		CostConversion:         `{"mode":"recharge","paid_cny":100,"credited_usd":200}`,
	})
	require.NoError(t, err)
	assert.Zero(t, monitor.UpdatedTime)
	assert.Equal(t, "dashboard-token", monitor.UpstreamAccessToken)
	assert.Equal(t, "update_group_ratio", monitor.SingleChannelAction)
	assert.Equal(t, "disable_channel", monitor.MultipleChannelsAction)
	assert.JSONEq(t, `{"mode":"recharge","paid_cny":100,"credited_usd":200}`, monitor.CostConversion)
	serialized, err := common.Marshal(monitor)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "dashboard-token")

	monitor, created, changed, err := UpdateChannelRatioMonitor(11, 0.8, "first fetch", 1, "root")
	require.NoError(t, err)
	assert.False(t, created)
	assert.False(t, changed)
	assert.Equal(t, 0.8, monitor.Ratio)
	assert.Nil(t, monitor.PreviousRatio)
	assert.Equal(t, "vip", monitor.UpstreamGroup)
	assert.Equal(t, "dashboard-token", monitor.UpstreamAccessToken)
	assert.JSONEq(t, `{"mode":"recharge","paid_cny":100,"credited_usd":200}`, monitor.CostConversion)

	history, total, err := GetChannelRatioHistory(11, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, history)
	assert.Zero(t, total)

	upstreamBalance := 9.5
	require.NoError(t, RecordChannelRatioMonitorBalance(11, &upstreamBalance, ""))
	monitor, err = SaveChannelRatioUpstreamConfig(11, "new_api", "https://upstream.example", "public", "public", 0, "", ChannelRatioUpstreamOptions{
		SingleChannelAction:    "update_group_ratio",
		MultipleChannelsAction: "disable_channel",
		RatioSyncEnabled:       true,
		BalanceSyncEnabled:     true,
	})
	require.NoError(t, err)
	assert.Equal(t, 0.8, monitor.Ratio)
	assert.NotZero(t, monitor.UpdatedTime)
	assert.Empty(t, monitor.UpstreamAccessToken)
	assert.Nil(t, monitor.UpstreamBalance)
	assert.Zero(t, monitor.LastBalanceTime)
	assert.Empty(t, monitor.LastBalanceError)
}

func TestChannelRatioUpstreamConfigStoresCustomConfig(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	customConfig := `{"version":1,"ratio":{"source":"fixed","fixed_value":0.8},"balance":{"source":"fixed","fixed_value":20}}`

	monitor, err := SaveChannelRatioUpstreamConfig(12, "custom", "https://custom.example", "", "custom", 0, "", ChannelRatioUpstreamOptions{
		RatioSyncEnabled:     true,
		BalanceSyncEnabled:   true,
		CustomUpstreamConfig: customConfig,
	})
	require.NoError(t, err)
	assert.JSONEq(t, customConfig, monitor.CustomUpstreamConfig)

	serialized, err := common.Marshal(monitor)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "custom_upstream_config")
}

func TestChannelRatioUpstreamTokenIsNotSerialized(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	monitor, err := SaveChannelRatioUpstreamConfig(
		12,
		"sub2api",
		"https://upstream.example",
		"vip",
		"token",
		0,
		"stored-access-token",
		ChannelRatioUpstreamOptions{
			SingleChannelAction:    "none",
			MultipleChannelsAction: "none",
			RatioSyncEnabled:       true,
			BalanceSyncEnabled:     true,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "stored-access-token", monitor.UpstreamAccessToken)

	serialized, err := common.Marshal(monitor)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "stored-access-token")

	monitor, err = SaveChannelRatioUpstreamConfig(12, "new_api", "https://upstream.example", "public", "public", 0, "", ChannelRatioUpstreamOptions{
		SingleChannelAction:    "none",
		MultipleChannelsAction: "none",
		RatioSyncEnabled:       true,
		BalanceSyncEnabled:     true,
	})
	require.NoError(t, err)
	assert.Empty(t, monitor.UpstreamAccessToken)
}

func TestGetAllChannelsForMonitorIncludesDisabledChannelsWithoutKeys(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	highPriority := int64(10)
	lowPriority := int64(5)
	channels := []Channel{
		{Id: 21, Name: "enabled", Key: "enabled-secret", Status: common.ChannelStatusEnabled, Priority: &highPriority},
		{Id: 22, Name: "disabled", Key: "disabled-secret", Status: common.ChannelStatusManuallyDisabled, Priority: &lowPriority},
	}
	require.NoError(t, DB.Create(&channels).Error)

	result, err := GetAllChannelsForMonitor()
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, 21, result[0].Id)
	assert.Equal(t, common.ChannelStatusEnabled, result[0].Status)
	assert.Empty(t, result[0].Key)
	assert.Equal(t, 22, result[1].Id)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, result[1].Status)
	assert.Empty(t, result[1].Key)
}

func TestGetChannelRatioMonitorTasksFiltersOrdersAndPaginatesRuns(t *testing.T) {
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&SystemTask{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&SystemTask{}).Error)
	})

	tasks := []SystemTask{
		{TaskID: "other-task", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusSucceeded},
		{TaskID: "monitor-oldest", Type: SystemTaskTypeChannelRatioMonitor, Status: SystemTaskStatusSucceeded},
		{TaskID: "monitor-middle", Type: SystemTaskTypeChannelRatioMonitor, Status: SystemTaskStatusFailed},
		{TaskID: "monitor-newest", Type: SystemTaskTypeChannelRatioMonitor, Status: SystemTaskStatusSucceeded},
	}
	require.NoError(t, DB.Create(&tasks).Error)

	result, total, err := GetChannelRatioMonitorTasks(0, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, result, 2)
	assert.Equal(t, "monitor-newest", result[0].TaskID)
	assert.Equal(t, "monitor-middle", result[1].TaskID)

	result, total, err = GetChannelRatioMonitorTasks(2, 2)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, result, 1)
	assert.Equal(t, "monitor-oldest", result[0].TaskID)
}

func TestChannelSmartScheduleConfigAndResultPersistWithoutRatioBaseline(t *testing.T) {
	resetChannelRatioMonitorTables(t)

	monitor, err := SaveChannelSmartScheduleConfig(31, ChannelSmartScheduleConfigOptions{Excluded: false})
	require.NoError(t, err)
	assert.Zero(t, monitor.UpdatedTime)
	assert.False(t, monitor.SmartScheduleExcluded)

	score := 0.82
	require.NoError(t, SaveChannelSmartScheduleResults([]ChannelSmartScheduleResultUpdate{
		{
			ChannelId: 31,
			Status:    ChannelSmartScheduleStatusSucceeded,
			Score:     &score,
			Priority:  100,
			Weight:    80,
			Time:      123,
			Stability: &ChannelSmartScheduleStabilityUpdate{
				State:         ChannelSmartScheduleStabilityDegraded,
				Until:         456,
				SavedPriority: 90,
				SavedWeight:   70,
			},
		},
	}))

	monitor, err = GetChannelRatioMonitor(31)
	require.NoError(t, err)
	assert.Zero(t, monitor.UpdatedTime)
	assert.Equal(t, ChannelSmartScheduleStatusSucceeded, monitor.LastScheduleStatus)
	require.NotNil(t, monitor.LastScheduleScore)
	assert.InDelta(t, score, *monitor.LastScheduleScore, 1e-9)
	assert.Equal(t, int64(100), monitor.LastSchedulePriority)
	assert.Equal(t, uint(80), monitor.LastScheduleWeight)
	assert.Equal(t, int64(123), monitor.LastScheduleTime)
	assert.Equal(t, ChannelSmartScheduleStabilityDegraded, monitor.SmartScheduleStabilityState)
	assert.Equal(t, int64(456), monitor.SmartScheduleStabilityUntil)
	assert.Equal(t, int64(90), monitor.SmartScheduleSavedPriority)
	assert.Equal(t, uint(70), monitor.SmartScheduleSavedWeight)
}

func TestChannelSmartScheduleParticipationChangesFreezeRoutingAndRestartProbeSampling(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id = ?", 36).Delete(&Ability{}).Error)
	})

	priority := int64(90)
	weight := uint(40)
	require.NoError(t, DB.Create(&Channel{
		Id: 36, Name: "probing", Status: common.ChannelStatusEnabled, Group: "vip",
		Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "vip", Model: "model-a", ChannelId: 36, Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId: 36, SmartScheduleParticipationSet: true,
		SmartScheduleStabilityState: ChannelSmartScheduleStabilityProbing,
		SmartScheduleStabilitySince: 1, SmartScheduleSavedPriority: 90, SmartScheduleSavedWeight: 75,
	}).Error)

	monitor, err := SaveChannelSmartScheduleConfig(36, ChannelSmartScheduleConfigOptions{Excluded: true})
	require.NoError(t, err)
	assert.False(t, monitor.ParticipatesInSmartSchedule())
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, monitor.SmartScheduleStabilityState)
	assert.Equal(t, int64(1), monitor.SmartScheduleStabilitySince)

	monitor, err = SaveChannelSmartScheduleConfig(36, ChannelSmartScheduleConfigOptions{Excluded: false})
	require.NoError(t, err)
	assert.True(t, monitor.ParticipatesInSmartSchedule())
	assert.Equal(t, ChannelSmartScheduleStabilityProbing, monitor.SmartScheduleStabilityState)
	assert.Greater(t, monitor.SmartScheduleStabilitySince, int64(1))
	assert.Equal(t, int64(90), monitor.SmartScheduleSavedPriority)
	assert.Equal(t, uint(75), monitor.SmartScheduleSavedWeight)

	var channel Channel
	require.NoError(t, DB.First(&channel, "id = ?", 36).Error)
	assert.Equal(t, priority, channel.GetPriority())
	assert.Equal(t, int(weight), channel.GetWeight())
	var ability Ability
	require.NoError(t, DB.First(&ability, "channel_id = ?", 36).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, priority, *ability.Priority)
	assert.Equal(t, weight, ability.Weight)
}

func TestClearChannelSmartScheduleStabilityRestoresRoutingAndInvalidatesStaleResults(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}, &Option{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id IN ?", []int{37, 38}).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("key = ?", ChannelSmartScheduleControlRevisionOption).Delete(&Option{}).Error)
	})

	zeroPriority := int64(0)
	zeroWeight := uint(0)
	require.NoError(t, DB.Create(&[]Channel{
		{Id: 37, Name: "degraded", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Priority: &zeroPriority, Weight: &zeroWeight},
		{Id: 38, Name: "probing", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Priority: &zeroPriority, Weight: &zeroWeight},
	}).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "vip", Model: "model-a", ChannelId: 37, Enabled: true, Priority: &zeroPriority, Weight: zeroWeight},
		{Group: "vip", Model: "model-a", ChannelId: 38, Enabled: true, Priority: &zeroPriority, Weight: zeroWeight},
	}).Error)
	require.NoError(t, DB.Create(&[]ChannelRatioMonitor{
		{
			ChannelId: 37, SmartScheduleParticipationSet: true, SmartScheduleRevision: 4,
			SmartScheduleStabilityState: ChannelSmartScheduleStabilityDegraded,
			SmartScheduleStabilityUntil: 999, SmartScheduleSavedPriority: 90, SmartScheduleSavedWeight: 40,
		},
		{
			ChannelId: 38, SmartScheduleParticipationSet: true, SmartScheduleRevision: 8,
			SmartScheduleStabilityState: ChannelSmartScheduleStabilityProbing,
			SmartScheduleStabilitySince: 888, SmartScheduleSavedPriority: 100, SmartScheduleSavedWeight: 60,
		},
	}).Error)

	for channelId, expected := range map[int]struct {
		state    string
		priority int64
		weight   uint
	}{
		37: {state: ChannelSmartScheduleStabilityDegraded, priority: 90, weight: 40},
		38: {state: ChannelSmartScheduleStabilityProbing, priority: 100, weight: 60},
	} {
		clearStartedAt := common.GetTimestamp()
		result, err := ClearChannelSmartScheduleStability(channelId, 80, 10)
		require.NoError(t, err)
		clearFinishedAt := common.GetTimestamp()
		assert.True(t, result.Cleared)
		assert.Equal(t, expected.state, result.PreviousState)
		assert.Equal(t, expected.priority, result.Priority)
		assert.Equal(t, expected.weight, result.Weight)

		var channel Channel
		require.NoError(t, DB.First(&channel, "id = ?", channelId).Error)
		assert.Equal(t, expected.priority, channel.GetPriority())
		assert.Equal(t, int(expected.weight), channel.GetWeight())
		var ability Ability
		require.NoError(t, DB.First(&ability, "channel_id = ?", channelId).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, expected.priority, *ability.Priority)
		assert.Equal(t, expected.weight, ability.Weight)
		monitor, monitorErr := GetChannelRatioMonitor(channelId)
		require.NoError(t, monitorErr)
		assert.Empty(t, monitor.SmartScheduleStabilityState)
		assert.Zero(t, monitor.SmartScheduleStabilityUntil)
		assert.GreaterOrEqual(t, monitor.SmartScheduleStabilitySince, clearStartedAt)
		assert.LessOrEqual(t, monitor.SmartScheduleStabilitySince, clearFinishedAt)
		assert.Zero(t, monitor.SmartScheduleSavedPriority)
		assert.Zero(t, monitor.SmartScheduleSavedWeight)
	}

	secondClear, err := ClearChannelSmartScheduleStability(37, 80, 10)
	require.NoError(t, err)
	assert.False(t, secondClear.Cleared)
	assert.Empty(t, secondClear.PreviousState)
	assert.Equal(t, int64(90), secondClear.Priority)
	assert.Equal(t, uint(40), secondClear.Weight)

	outcomes, err := ApplyChannelSmartScheduleResults([]ChannelSmartScheduleResultUpdate{{
		ChannelId: 37, Status: ChannelSmartScheduleStatusSucceeded, Priority: 0, Weight: 0,
		GuardCurrent: true, ExpectedRevision: 4, ExpectedControlRevision: "",
		ExpectedPriority: 0, ExpectedWeight: 0, ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Applied)
	var channel Channel
	require.NoError(t, DB.First(&channel, "id = ?", 37).Error)
	assert.Equal(t, int64(90), channel.GetPriority())
	assert.Equal(t, 40, channel.GetWeight())
}

func TestApplyChannelSmartScheduleResultsRejectsStaleGlobalSettings(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}, &Option{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id = ?", 39).Delete(&Ability{}).Error)
		require.NoError(t, DB.Where("key = ?", ChannelSmartScheduleControlRevisionOption).Delete(&Option{}).Error)
	})

	priority := int64(80)
	weight := uint(10)
	require.NoError(t, DB.Create(&Channel{
		Id: 39, Name: "guarded", Status: common.ChannelStatusEnabled, Group: "vip",
		Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group: "vip", Model: "model-a", ChannelId: 39, Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId: 39, SmartScheduleParticipationSet: true, SmartScheduleRevision: 3,
	}).Error)
	require.NoError(t, DB.Where("key = ?", ChannelSmartScheduleControlRevisionOption).Delete(&Option{}).Error)
	require.NoError(t, DB.Create(&Option{Key: ChannelSmartScheduleControlRevisionOption, Value: "current"}).Error)

	update := ChannelSmartScheduleResultUpdate{
		ChannelId: 39, Status: ChannelSmartScheduleStatusSucceeded, Priority: 90, Weight: 30,
		GuardCurrent: true, ExpectedRevision: 3, ExpectedControlRevision: "stale",
		ExpectedPriority: 80, ExpectedWeight: 10, ApplyPriorityWeight: true,
	}
	outcomes, err := ApplyChannelSmartScheduleResults([]ChannelSmartScheduleResultUpdate{update})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Applied)

	update.ExpectedControlRevision = "current"
	outcomes, err = ApplyChannelSmartScheduleResults([]ChannelSmartScheduleResultUpdate{update})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)
	assert.True(t, outcomes[0].RoutingChanged)
	var channel Channel
	require.NoError(t, DB.First(&channel, "id = ?", 39).Error)
	assert.Equal(t, int64(90), channel.GetPriority())
	assert.Equal(t, 30, channel.GetWeight())
}

func TestDropLegacyChannelSmartScheduleGroupColumnPreservesMonitorData(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, dropLegacyChannelSmartScheduleGroupColumn())
	t.Cleanup(func() {
		require.NoError(t, dropLegacyChannelSmartScheduleGroupColumn())
	})

	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId:   33,
		Ratio:       1.25,
		UpdatedTime: 100,
	}).Error)
	legacyModel := &channelRatioMonitorLegacyScheduleGroup{}
	require.NoError(t, DB.Migrator().AddColumn(legacyModel, "SmartScheduleGroup"))
	require.True(t, DB.Migrator().HasColumn(legacyModel, "SmartScheduleGroup"))
	require.NoError(t, DB.Table("channel_ratio_monitors").
		Where("channel_id = ?", 33).
		Update("smart_schedule_group", "vip").Error)

	require.NoError(t, dropLegacyChannelSmartScheduleGroupColumn())
	assert.False(t, DB.Migrator().HasColumn(legacyModel, "SmartScheduleGroup"))
	monitor, err := GetChannelRatioMonitor(33)
	require.NoError(t, err)
	assert.Equal(t, 1.25, monitor.Ratio)
	assert.Equal(t, int64(100), monitor.UpdatedTime)
}

func TestChannelSmartSchedulePriorityWeightUpdatesKeepAbilitiesInSync(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id = ?", 32).Delete(&Ability{}).Error)
	})

	priority := int64(0)
	weight := uint(0)
	channel := Channel{
		Id:       32,
		Name:     "scheduled-channel",
		Key:      "secret",
		Status:   common.ChannelStatusEnabled,
		Group:    "vip",
		Models:   "model-a",
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "vip",
		Model:     "model-a",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
		Weight:    weight,
	}).Error)

	targetPriority := int64(100)
	targetWeight := uint(75)
	require.NoError(t, UpdateChannelSmartSchedulePriorityWeight(channel.Id, &targetPriority, &targetWeight))

	var storedChannel Channel
	require.NoError(t, DB.Where("id = ?", channel.Id).First(&storedChannel).Error)
	assert.Equal(t, targetPriority, storedChannel.GetPriority())
	assert.Equal(t, int(targetWeight), storedChannel.GetWeight())

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, targetPriority, *ability.Priority)
	assert.Equal(t, targetWeight, ability.Weight)

	require.NoError(t, ResetChannelSmartSchedulePriorityWeight([]int{channel.Id}, 80, 10))
	require.NoError(t, DB.Where("id = ?", channel.Id).First(&storedChannel).Error)
	assert.Equal(t, int64(80), storedChannel.GetPriority())
	assert.Equal(t, 10, storedChannel.GetWeight())
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(80), *ability.Priority)
	assert.Equal(t, uint(10), ability.Weight)
}

func TestRestoreChannelSmartScheduleStabilityStatesRestoresSavedOrBaselineValues(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}))
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id IN ?", []int{34, 35}).Delete(&Ability{}).Error)
	})

	priority := int64(0)
	weight := uint(0)
	channels := []Channel{
		{Id: 34, Name: "saved", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight},
		{Id: 35, Name: "fallback", Status: common.ChannelStatusEnabled, Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight},
	}
	require.NoError(t, DB.Create(&channels).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "vip", Model: "model-a", ChannelId: 34, Enabled: true, Priority: &priority, Weight: weight},
		{Group: "vip", Model: "model-a", ChannelId: 35, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, DB.Create(&[]ChannelRatioMonitor{
		{
			ChannelId: 34, SmartScheduleStabilityState: ChannelSmartScheduleStabilityDegraded,
			SmartScheduleSavedPriority: 90, SmartScheduleSavedWeight: 40,
		},
		{
			ChannelId: 35, SmartScheduleStabilityState: ChannelSmartScheduleStabilityProbing,
		},
	}).Error)

	restored, err := RestoreChannelSmartScheduleStabilityStates(80, 10)
	require.NoError(t, err)
	assert.Equal(t, 2, restored)

	for channelId, expected := range map[int]struct {
		priority int64
		weight   int
	}{
		34: {priority: 90, weight: 40},
		35: {priority: 80, weight: 10},
	} {
		var channel Channel
		require.NoError(t, DB.First(&channel, "id = ?", channelId).Error)
		assert.Equal(t, expected.priority, channel.GetPriority())
		assert.Equal(t, expected.weight, channel.GetWeight())

		var ability Ability
		require.NoError(t, DB.First(&ability, "channel_id = ?", channelId).Error)
		require.NotNil(t, ability.Priority)
		assert.Equal(t, expected.priority, *ability.Priority)
		assert.Equal(t, uint(expected.weight), ability.Weight)

		monitor, monitorErr := GetChannelRatioMonitor(channelId)
		require.NoError(t, monitorErr)
		assert.Empty(t, monitor.SmartScheduleStabilityState)
		assert.Zero(t, monitor.SmartScheduleStabilityUntil)
		assert.Zero(t, monitor.SmartScheduleStabilitySince)
	}
}
