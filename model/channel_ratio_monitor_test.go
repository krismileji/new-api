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

func seedChannelRatioMonitorTestChannels(t *testing.T, channelIds ...int) {
	t.Helper()
	channels := make([]Channel, 0, len(channelIds))
	for _, channelId := range channelIds {
		channels = append(channels, Channel{Id: channelId, Name: "monitor channel"})
	}
	require.NoError(t, DB.Create(&channels).Error)
}

func TestUpdateChannelRatioMonitorTracksOnlyRatioChanges(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 10)

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
	seedChannelRatioMonitorTestChannels(t, 10)

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

func TestChannelRatioMonitorBalancePersistsAndClearsEstimateState(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{ChannelId: 10, UpstreamRevision: 2}).Error)

	balance := 12.75
	estimateState := ChannelRatioMonitorBalanceEstimateState{
		CostBaseline:       ChannelDailyCostBaseline{Timestamp: 123, CostNanoCNY: 456},
		PendingConsumption: 1.25,
	}
	applied, err := RecordChannelRatioMonitorBalanceWithEstimateIfRevision(10, 2, &balance, "", &estimateState)
	require.NoError(t, err)
	require.True(t, applied)
	monitor, err := GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Equal(t, int64(123), monitor.LastBalanceTime)
	require.NotNil(t, monitor.LastBalanceCostNanoCNY)
	assert.Equal(t, int64(456), *monitor.LastBalanceCostNanoCNY)
	assert.Equal(t, 1.25, monitor.BalancePendingConsumption)

	require.NoError(t, RecordChannelRatioMonitorBalance(10, nil, "upstream timeout"))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	require.NotNil(t, monitor.LastBalanceCostNanoCNY)
	assert.Equal(t, int64(456), *monitor.LastBalanceCostNanoCNY)
	assert.Equal(t, 1.25, monitor.BalancePendingConsumption)

	recoveredBalance := 20.0
	require.NoError(t, RecordChannelRatioMonitorBalance(10, &recoveredBalance, ""))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Nil(t, monitor.LastBalanceCostNanoCNY)
	assert.Zero(t, monitor.BalancePendingConsumption)
}

func TestChannelRatioMonitorFailureAlertsMarkOnceAndResetOnRecovery(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 10)

	lowBalance := 5.0
	threshold := 10.0
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId:                   10,
		Ratio:                       1,
		UpdatedTime:                 1,
		UpstreamRevision:            4,
		LastFetchStatus:             ChannelRatioFetchStatusFailed,
		LastFetchError:              "倍率上游超时",
		ConsecutiveFailures:         2,
		UpstreamBalance:             &lowBalance,
		LastBalanceError:            "余额上游超时",
		BalanceConsecutiveFailures:  2,
		BalanceWarningThreshold:     &threshold,
		FetchFailureAlertNotified:   false,
		BalanceFailureAlertNotified: false,
	}).Error)

	require.NoError(t, MarkChannelRatioMonitorFailureAlertsNotified([]ChannelRatioMonitorFailureAlertGuard{
		{ChannelId: 10, UpstreamRevision: 4, FailureType: ChannelRatioFailureAlertRatio, FailureCount: 2},
		{ChannelId: 10, UpstreamRevision: 4, FailureType: ChannelRatioFailureAlertBalance, FailureCount: 2},
	}))
	monitor, err := GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.True(t, monitor.FetchFailureAlertNotified)
	assert.True(t, monitor.BalanceFailureAlertNotified)

	require.NoError(t, MarkChannelRatioMonitorFailureAlertsNotified([]ChannelRatioMonitorFailureAlertGuard{
		{ChannelId: 10, UpstreamRevision: 3, FailureType: ChannelRatioFailureAlertRatio, FailureCount: 2},
	}))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.True(t, monitor.FetchFailureAlertNotified)

	monitor, _, _, err = UpdateChannelRatioMonitorFromUpstream(10, 1.1, "恢复", 0, "系统自动更新")
	require.NoError(t, err)
	assert.False(t, monitor.FetchFailureAlertNotified)
	assert.Zero(t, monitor.ConsecutiveFailures)

	recoveredBalance := 12.0
	require.NoError(t, RecordChannelRatioMonitorBalance(10, &recoveredBalance, ""))
	monitor, err = GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.False(t, monitor.BalanceFailureAlertNotified)
	assert.Zero(t, monitor.BalanceConsecutiveFailures)
}

func TestChannelRatioUpstreamConfigChangeResetsRelatedFetchFailures(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 10)

	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId:                   10,
		UpstreamType:                "new_api",
		UpstreamBaseURL:             "https://upstream.example",
		UpstreamGroup:               "default",
		UpstreamAuthType:            "user",
		UpstreamUserId:              7,
		UpstreamAccessToken:         "old-token",
		LastFetchStatus:             ChannelRatioFetchStatusFailed,
		LastFetchError:              "倍率失败",
		LastFetchTime:               100,
		ConsecutiveFailures:         2,
		FetchFailureAlertNotified:   true,
		LastBalanceError:            "余额失败",
		BalanceConsecutiveFailures:  2,
		BalanceFailureAlertNotified: true,
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
	assert.False(t, monitor.FetchFailureAlertNotified)
	assert.Equal(t, 2, monitor.BalanceConsecutiveFailures)
	assert.Equal(t, "余额失败", monitor.LastBalanceError)
	assert.True(t, monitor.BalanceFailureAlertNotified)

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
	assert.False(t, monitor.BalanceFailureAlertNotified)
}

func TestChannelRatioMonitorRejectsResultsFromStaleUpstreamConfig(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 10)

	options := ChannelRatioUpstreamOptions{
		RatioSyncEnabled:   true,
		BalanceSyncEnabled: true,
	}
	monitor, err := SaveChannelRatioUpstreamConfig(
		10,
		"new_api",
		"https://upstream.example",
		"default",
		"public",
		0,
		"",
		options,
	)
	require.NoError(t, err)
	require.NotZero(t, monitor.UpstreamRevision)
	staleRevision := monitor.UpstreamRevision

	monitor, err = SaveChannelRatioUpstreamConfig(
		10,
		"new_api",
		"https://upstream.example",
		"vip",
		"public",
		0,
		"",
		options,
	)
	require.NoError(t, err)
	assert.Greater(t, monitor.UpstreamRevision, staleRevision)
	currentRevision := monitor.UpstreamRevision

	_, _, _, applied, err := UpdateChannelRatioMonitorFromUpstreamIfRevision(
		10,
		staleRevision,
		9.9,
		"stale ratio",
		0,
		"system",
	)
	require.NoError(t, err)
	assert.False(t, applied)

	staleBalance := 3.5
	applied, err = RecordChannelRatioMonitorBalanceIfRevision(10, staleRevision, &staleBalance, "")
	require.NoError(t, err)
	assert.False(t, applied)
	applied, err = RecordChannelRatioMonitorFetchFailureIfRevision(10, staleRevision, "stale failure")
	require.NoError(t, err)
	assert.False(t, applied)

	stored, err := GetChannelRatioMonitor(10)
	require.NoError(t, err)
	assert.Zero(t, stored.UpdatedTime)
	assert.Nil(t, stored.UpstreamBalance)
	assert.Empty(t, stored.LastFetchError)
	assert.Zero(t, stored.ConsecutiveFailures)

	stored, _, _, applied, err = UpdateChannelRatioMonitorFromUpstreamIfRevision(
		10,
		currentRevision,
		0.8,
		"current ratio",
		0,
		"system",
	)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Equal(t, 0.8, stored.Ratio)
}

func TestSaveChannelRatioUpstreamConfigClearsPreviousRatioWhenCostConversionChanges(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 11)
	previousRatio := 0.8
	originalConversion := `{"mode":"recharge","paid_cny":100,"credited_usd":200}`
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId:        11,
		Ratio:            1,
		PreviousRatio:    &previousRatio,
		UpdatedTime:      1,
		UpstreamType:     "new_api",
		UpstreamBaseURL:  "https://upstream.example",
		UpstreamAuthType: "public",
		CostConversion:   originalConversion,
		UpstreamRevision: 1,
	}).Error)

	monitor, err := SaveChannelRatioUpstreamConfig(
		11, "new_api", "https://upstream.example", "", "public", 0, "",
		ChannelRatioUpstreamOptions{
			RatioSyncEnabled:   true,
			BalanceSyncEnabled: true,
			CostConversion:     originalConversion,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, monitor.PreviousRatio)
	assert.Equal(t, previousRatio, *monitor.PreviousRatio)

	monitor, err = SaveChannelRatioUpstreamConfig(
		11, "new_api", "https://upstream.example", "", "public", 0, "",
		ChannelRatioUpstreamOptions{
			RatioSyncEnabled:   true,
			BalanceSyncEnabled: true,
			CostConversion:     `{"mode":"none"}`,
		},
	)
	require.NoError(t, err)
	assert.Nil(t, monitor.PreviousRatio)
}

func TestChannelRatioMonitorRevisionGuardDoesNotRecreateDeletedZeroRevisionConfig(t *testing.T) {
	tests := []struct {
		name  string
		apply func() (bool, error)
	}{
		{
			name: "ratio",
			apply: func() (bool, error) {
				_, _, _, applied, err := UpdateChannelRatioMonitorFromUpstreamIfRevision(
					10, 0, 1.25, "stale ratio", 0, "system",
				)
				return applied, err
			},
		},
		{
			name: "balance",
			apply: func() (bool, error) {
				balance := 10.0
				return RecordChannelRatioMonitorBalanceIfRevision(10, 0, &balance, "")
			},
		},
		{
			name: "failure",
			apply: func() (bool, error) {
				return RecordChannelRatioMonitorFetchFailureIfRevision(10, 0, "stale failure")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetChannelRatioMonitorTables(t)
			require.NoError(t, DB.Create(&ChannelRatioMonitor{ChannelId: 10}).Error)
			require.NoError(t, DB.Where("channel_id = ?", 10).Delete(&ChannelRatioMonitor{}).Error)

			applied, err := test.apply()
			require.NoError(t, err)
			assert.False(t, applied)

			var count int64
			require.NoError(t, DB.Model(&ChannelRatioMonitor{}).Where("channel_id = ?", 10).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestChannelRatioMonitorAdminWritesRequireExistingChannel(t *testing.T) {
	tests := []struct {
		name  string
		write func() error
	}{
		{
			name: "ratio",
			write: func() error {
				_, _, _, err := UpdateChannelRatioMonitor(10, 1.25, "manual ratio", 1, "admin")
				return err
			},
		},
		{
			name: "upstream config",
			write: func() error {
				_, err := SaveChannelRatioUpstreamConfig(
					10, "new_api", "https://upstream.example", "default", "public", 0, "",
					ChannelRatioUpstreamOptions{RatioSyncEnabled: true, BalanceSyncEnabled: true},
				)
				return err
			},
		},
		{
			name: "concurrency limit",
			write: func() error {
				_, err := SaveChannelConcurrencyLimit(10, 1)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetChannelRatioMonitorTables(t)
			seedChannelRatioMonitorTestChannels(t, 10)
			require.NoError(t, DB.Where("id = ?", 10).Delete(&Channel{}).Error)

			err := test.write()
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)

			var count int64
			require.NoError(t, DB.Model(&ChannelRatioMonitor{}).Where("channel_id = ?", 10).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestChannelRatioMonitorBalanceAlertResetsAfterRecoveryOrThresholdChange(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 10)

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
	monitor, err := GetChannelRatioMonitor(10)
	require.NoError(t, err)
	require.NoError(t, MarkChannelRatioMonitorBalanceAlertsNotified([]ChannelRatioMonitorBalanceAlertGuard{{
		ChannelId: 10, UpstreamRevision: monitor.UpstreamRevision, WarningThreshold: threshold,
	}}))

	monitor, err = GetChannelRatioMonitor(10)
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

	require.NoError(t, MarkChannelRatioMonitorBalanceAlertsNotified([]ChannelRatioMonitorBalanceAlertGuard{{
		ChannelId: 10, UpstreamRevision: monitor.UpstreamRevision, WarningThreshold: threshold,
	}}))
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

func TestMarkChannelRatioMonitorBalanceAlertsNotifiedSkipsStaleSnapshot(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	threshold := 10.0
	lowBalance := 5.0
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId: 20, UpstreamRevision: 2,
		BalanceWarningThreshold: &threshold,
		UpstreamBalance:         &lowBalance,
	}).Error)

	require.NoError(t, MarkChannelRatioMonitorBalanceAlertsNotified([]ChannelRatioMonitorBalanceAlertGuard{{
		ChannelId: 20, UpstreamRevision: 1, WarningThreshold: threshold,
	}}))
	monitor, err := GetChannelRatioMonitor(20)
	require.NoError(t, err)
	assert.False(t, monitor.BalanceAlertNotified)

	newThreshold := 6.0
	require.NoError(t, DB.Model(&ChannelRatioMonitor{}).
		Where("channel_id = ?", 20).
		Updates(map[string]any{
			"upstream_revision":         int64(3),
			"balance_warning_threshold": newThreshold,
		}).Error)
	require.NoError(t, MarkChannelRatioMonitorBalanceAlertsNotified([]ChannelRatioMonitorBalanceAlertGuard{{
		ChannelId: 20, UpstreamRevision: 2, WarningThreshold: threshold,
	}}))
	monitor, err = GetChannelRatioMonitor(20)
	require.NoError(t, err)
	assert.False(t, monitor.BalanceAlertNotified)

	require.NoError(t, MarkChannelRatioMonitorBalanceAlertsNotified([]ChannelRatioMonitorBalanceAlertGuard{{
		ChannelId: 20, UpstreamRevision: 3, WarningThreshold: newThreshold,
	}}))
	monitor, err = GetChannelRatioMonitor(20)
	require.NoError(t, err)
	assert.True(t, monitor.BalanceAlertNotified)
}

func TestChannelRatioUpstreamConfigDoesNotCreateFalseBaseline(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 11)

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
	seedChannelRatioMonitorTestChannels(t, 12)
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

func TestChannelRatioUpstreamConfigChangeResetsIncompatibleBalanceEstimate(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 13)
	baselineCost := int64(100)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId:                 13,
		UpstreamType:              "new_api",
		UpstreamBaseURL:           "https://upstream.example",
		UpstreamGroup:             "vip",
		UpstreamAuthType:          "public",
		LastBalanceTime:           100,
		LastBalanceCostNanoCNY:    &baselineCost,
		BalancePendingConsumption: 2,
		CostConversion:            `{"mode":"none"}`,
	}).Error)

	monitor, err := SaveChannelRatioUpstreamConfig(
		13,
		"new_api",
		"https://upstream.example",
		"vip",
		"public",
		0,
		"",
		ChannelRatioUpstreamOptions{
			RatioSyncEnabled:   true,
			BalanceSyncEnabled: true,
			CostConversion:     `{"mode":"recharge","paid_cny":100,"credited_usd":200}`,
		},
	)
	require.NoError(t, err)
	assert.Nil(t, monitor.LastBalanceCostNanoCNY)
	assert.Zero(t, monitor.BalancePendingConsumption)
}

func TestChannelRatioUpstreamTokenIsNotSerialized(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 12)

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

func TestRotateChannelRatioUpstreamCredentialRequiresMatchingSnapshot(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	seedChannelRatioMonitorTestChannels(t, 14)
	require.NoError(t, DB.Create(&ChannelRatioMonitor{
		ChannelId:           14,
		UpstreamType:        "sub2api",
		UpstreamAuthType:    "refresh_token",
		UpstreamAccessToken: "refresh-old",
		UpstreamRevision:    3,
	}).Error)

	rotated, err := RotateChannelRatioUpstreamCredential(14, "sub2api", "refresh_token", 2, "refresh-old", "refresh-stale")
	require.NoError(t, err)
	assert.False(t, rotated)
	rotated, err = RotateChannelRatioUpstreamCredential(14, "sub2api", "refresh_token", 3, "refresh-other", "refresh-stale")
	require.NoError(t, err)
	assert.False(t, rotated)

	monitor, err := GetChannelRatioMonitor(14)
	require.NoError(t, err)
	assert.Equal(t, "refresh-old", monitor.UpstreamAccessToken)

	rotated, err = RotateChannelRatioUpstreamCredential(14, "sub2api", "refresh_token", 3, "refresh-old", "refresh-new")
	require.NoError(t, err)
	assert.True(t, rotated)
	monitor, err = GetChannelRatioMonitor(14)
	require.NoError(t, err)
	assert.Equal(t, "refresh-new", monitor.UpstreamAccessToken)
	assert.Equal(t, int64(3), monitor.UpstreamRevision)
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

func TestChannelMonitorStatusTransitionDoesNotOverwriteNewerState(t *testing.T) {
	resetChannelRatioMonitorTables(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}, &Option{}))
	require.NoError(t, DB.Where("channel_id = ?", 9011).Delete(&Ability{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Where("channel_id = ?", 9011).Delete(&Ability{}).Error)
	})

	priority := int64(10)
	weight := uint(100)
	channel := Channel{
		Id:       9011,
		Name:     "monitor status cas",
		Key:      "secret",
		Status:   common.ChannelStatusManuallyDisabled,
		Priority: &priority,
		Weight:   &weight,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Create(&Ability{
		ChannelId: channel.Id,
		Group:     "default",
		Model:     "gpt-test",
		Enabled:   false,
		Priority:  &priority,
		Weight:    weight,
	}).Error)

	changed, err := UpdateChannelMonitorStatusIfCurrent(
		channel.Id,
		common.ChannelStatusEnabled,
		"",
		common.ChannelStatusAutoDisabled,
		"delayed automatic disable",
	)
	require.NoError(t, err)
	assert.False(t, changed)

	autoReason := "channel monitor automatic disable"
	encodedInfo, err := common.Marshal(map[string]any{"status_reason": autoReason})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"status":     common.ChannelStatusAutoDisabled,
		"other_info": string(encodedInfo),
	}).Error)

	changed, err = UpdateChannelMonitorStatusIfCurrent(
		channel.Id,
		common.ChannelStatusAutoDisabled,
		"a different reason",
		common.ChannelStatusEnabled,
		"",
	)
	require.NoError(t, err)
	assert.False(t, changed)

	changed, err = UpdateChannelMonitorStatusIfCurrent(
		channel.Id,
		common.ChannelStatusAutoDisabled,
		autoReason,
		common.ChannelStatusEnabled,
		"",
	)
	require.NoError(t, err)
	assert.True(t, changed)

	storedChannel, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, storedChannel.Status)
	assert.Empty(t, storedChannel.GetOtherInfo()["status_reason"])
	var storedAbility Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&storedAbility).Error)
	assert.True(t, storedAbility.Enabled)
}

func TestGetChannelRatioMonitorTasksFiltersOrdersAndPaginatesRuns(t *testing.T) {
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&SystemTask{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&SystemTask{}).Error)
	})

	tasks := []SystemTask{
		{TaskID: "other-task", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusSucceeded, CreatedAt: 400},
		{TaskID: "monitor-oldest", Type: SystemTaskTypeChannelRatioMonitor, Status: SystemTaskStatusSucceeded, CreatedAt: 100},
		{TaskID: "monitor-newest", Type: SystemTaskTypeChannelRatioMonitor, Status: SystemTaskStatusSucceeded, CreatedAt: 300},
		{TaskID: "monitor-middle", Type: SystemTaskTypeChannelRatioMonitor, Status: SystemTaskStatusFailed, CreatedAt: 200},
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
