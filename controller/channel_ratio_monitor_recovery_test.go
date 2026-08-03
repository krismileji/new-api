package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelMonitorSettingsPersistsCostRatioRecoverySwitch(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"auto_enable_on_cost_ratio_recovery": true,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			AutoEnableOnCostRatioRecovery bool `json:"auto_enable_on_cost_ratio_recovery"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.True(t, response.Data.AutoEnableOnCostRatioRecovery)

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorAutoEnableOnCostRatioRecoveryOption).First(&option).Error)
	assert.Equal(t, "true", option.Value)
}

func TestChannelMonitorCostRatioRecoverySettingDefaultsToDisabled(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		expects bool
	}{
		{name: "missing option"},
		{name: "invalid option", value: "invalid"},
		{name: "enabled option", value: "true", expects: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := map[string]string{}
			if test.value != "" {
				options[channelMonitorAutoEnableOnCostRatioRecoveryOption] = test.value
			}
			useChannelMonitorOptionMap(t, options)
			assert.Equal(t, test.expects, getChannelMonitorSettings().AutoEnableOnCostRatioRecovery)
		})
	}
}

func TestUpdateChannelMonitorSettingsPersistsBalanceRecoverySwitch(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})

	ctx, recorder := newChannelMonitorControllerContext(t, http.MethodPut, "/api/channel_monitor/settings", map[string]any{
		"auto_enable_on_balance_recovery": true,
	})
	UpdateChannelMonitorSettings(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			AutoEnableOnBalanceRecovery bool `json:"auto_enable_on_balance_recovery"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.True(t, response.Data.AutoEnableOnBalanceRecovery)

	var option model.Option
	require.NoError(t, db.Where("key = ?", channelMonitorAutoEnableOnBalanceRecoveryOption).First(&option).Error)
	assert.Equal(t, "true", option.Value)
}

func TestChannelMonitorBalanceRecoverySettingDefaultsToDisabled(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		expects bool
	}{
		{name: "missing option"},
		{name: "invalid option", value: "invalid"},
		{name: "enabled option", value: "true", expects: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := map[string]string{}
			if test.value != "" {
				options[channelMonitorAutoEnableOnBalanceRecoveryOption] = test.value
			}
			useChannelMonitorOptionMap(t, options)
			assert.Equal(t, test.expects, getChannelMonitorSettings().AutoEnableOnBalanceRecovery)
		})
	}
}

func TestRunChannelRatioMonitorTaskAutoEnablesChannelAfterRatioUpdateRecovers(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption:       "0",
		channelMonitorAutoDisableOnUpdateFailureOption: "true",
	})
	disableChannelMonitorSSRFProtection(t)

	var upstreamFailing atomic.Bool
	upstreamFailing.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if upstreamFailing.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":0.8}}`))
	}))
	defer server.Close()

	require.NoError(t, db.Create(&model.Channel{
		Id: 1, Name: "ratio recovery", Key: "test-key", Group: "vip", Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
	}).Error)

	firstSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, firstSummary.ChannelsDisabled)
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
	assert.Equal(t, "渠道监控：上游倍率或余额更新失败", channel.GetOtherInfo()["status_reason"])

	upstreamFailing.Store(false)
	secondSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Zero(t, secondSummary.Failed)
	assert.Equal(t, 1, secondSummary.ChannelsEnabled)
	channel, err = model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, channel.Status)
	assert.Empty(t, channel.GetOtherInfo()["status_reason"])
}

func TestRunChannelRatioMonitorTaskDoesNotOverrideManualDisableDuringRecovery(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoUpdateRetryCountOption: "0",
	})
	disableChannelMonitorSSRFProtection(t)

	channel := model.Channel{
		Id: 1, Name: "manual during recovery", Key: "test-key", Group: "vip", Status: common.ChannelStatusAutoDisabled,
	}
	channel.SetOtherInfo(map[string]interface{}{
		"status_reason": channelMonitorUpdateFailureDisableReason,
	})
	require.NoError(t, db.Create(&channel).Error)
	manualState := model.Channel{}
	manualState.SetOtherInfo(map[string]interface{}{
		"status_reason": "管理员手动禁用",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := db.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]interface{}{
			"status":     common.ChannelStatusManuallyDisabled,
			"other_info": manualState.OtherInfo,
		}).Error; err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"group_ratio":{"vip":0.8}}`))
	}))
	defer server.Close()
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.NewAPIUpstreamType, UpstreamBaseURL: server.URL,
		UpstreamGroup: "vip", UpstreamAuthType: service.NewAPIUpstreamAuthPublic,
		UpstreamBalanceSyncDisabled: true,
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Zero(t, summary.ChannelsEnabled)
	storedChannel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, storedChannel.Status)
	assert.Equal(t, "管理员手动禁用", storedChannel.GetOtherInfo()["status_reason"])
}

func TestRunChannelRatioMonitorTaskAutoEnablesChannelAfterBalanceUpdateRecovers(t *testing.T) {
	tests := []struct {
		name          string
		balance       float64
		wantStatus    int
		wantReasonSet bool
	}{
		{name: "balance above threshold enables channel", balance: 5, wantStatus: common.ChannelStatusEnabled},
		{name: "balance below threshold keeps channel disabled", balance: 3, wantStatus: common.ChannelStatusAutoDisabled, wantReasonSet: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorAutoUpdateRetryCountOption: "0",
			})
			disableChannelMonitorSSRFProtection(t)

			ratio := 1.0
			customConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
				Ratio: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &ratio,
				},
				Balance: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &test.balance,
				},
			})
			require.NoError(t, err)
			channel := model.Channel{
				Id: 1, Name: "balance recovery", Group: "vip", Status: common.ChannelStatusAutoDisabled,
			}
			channel.SetOtherInfo(map[string]interface{}{
				"status_reason": "渠道监控：上游倍率或余额更新失败",
			})
			require.NoError(t, db.Create(&channel).Error)
			threshold := 4.0
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: 1, UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: "https://custom.example",
				UpstreamAuthType: service.CustomUpstreamAuthType, CustomUpstreamConfig: customConfig,
				UpstreamRatioSyncDisabled: true, BalanceAutoDisableThreshold: &threshold,
			}).Error)

			summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Equal(t, 1, summary.BalanceUpdated)
			if test.wantStatus == common.ChannelStatusEnabled {
				assert.Equal(t, 1, summary.ChannelsEnabled)
			} else {
				assert.Zero(t, summary.ChannelsEnabled)
			}
			storedChannel, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, storedChannel.Status)
			if test.wantReasonSet {
				assert.Equal(t, "渠道监控：上游倍率或余额更新失败", storedChannel.GetOtherInfo()["status_reason"])
			} else {
				assert.Empty(t, storedChannel.GetOtherInfo()["status_reason"])
			}
		})
	}
}

func TestRunChannelRatioMonitorTaskBalanceRecoveryRespectsSwitchAndGroupRatios(t *testing.T) {
	tests := []struct {
		name        string
		switchOn    bool
		balance     float64
		costRatio   float64
		groups      string
		status      int
		reason      string
		wantStatus  int
		wantEnabled int
	}{
		{
			name: "switch disabled leaves channel disabled", balance: 5, costRatio: 0.8,
			groups: "vip", status: common.ChannelStatusAutoDisabled, wantStatus: common.ChannelStatusAutoDisabled,
		},
		{
			name: "balance below threshold leaves channel disabled", switchOn: true, balance: 3, costRatio: 0.8,
			groups: "vip", status: common.ChannelStatusAutoDisabled, wantStatus: common.ChannelStatusAutoDisabled,
		},
		{
			name: "ratio equal to group ratio enables channel", switchOn: true, balance: 5, costRatio: 1,
			groups: "vip", status: common.ChannelStatusAutoDisabled, wantStatus: common.ChannelStatusEnabled, wantEnabled: 1,
		},
		{
			name: "ratio above group ratio leaves channel disabled", switchOn: true, balance: 5, costRatio: 1.01,
			groups: "vip", status: common.ChannelStatusAutoDisabled, wantStatus: common.ChannelStatusAutoDisabled,
		},
		{
			name: "every group must allow recovery", switchOn: true, balance: 5, costRatio: 0.8,
			groups: "vip,discount", status: common.ChannelStatusAutoDisabled, wantStatus: common.ChannelStatusAutoDisabled,
		},
		{
			name: "other automatic disable reason is untouched", switchOn: true, balance: 5, costRatio: 0.8,
			groups: "vip", status: common.ChannelStatusAutoDisabled, reason: "其他系统自动禁用原因",
			wantStatus: common.ChannelStatusAutoDisabled,
		},
		{
			name: "manual disable is untouched", switchOn: true, balance: 5, costRatio: 0.8,
			groups: "vip", status: common.ChannelStatusManuallyDisabled, wantStatus: common.ChannelStatusManuallyDisabled,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			useChannelMonitorOptionMap(t, map[string]string{
				channelMonitorAutoEnableOnBalanceRecoveryOption: strconv.FormatBool(test.switchOn),
				channelMonitorAutoUpdateRetryCountOption:        "0",
			})
			disableChannelMonitorSSRFProtection(t)
			originalGroupRatios := ratio_setting.GroupRatio2JSONString()
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1,"discount":0.75}`))
			t.Cleanup(func() {
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
			})

			customConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
				Ratio: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &test.costRatio,
				},
				Balance: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &test.balance,
				},
			})
			require.NoError(t, err)
			reason := test.reason
			if reason == "" {
				reason = channelMonitorBalancePolicyDisableReasonPrefix + "3" +
					channelMonitorBalancePolicyDisableThresholdMarker + "4"
			}
			channel := model.Channel{
				Id: 1, Name: "balance policy recovery", Group: test.groups, Status: test.status,
			}
			channel.SetOtherInfo(map[string]interface{}{"status_reason": reason})
			require.NoError(t, db.Create(&channel).Error)
			threshold := 4.0
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: 1, Ratio: test.costRatio, UpdatedTime: 1,
				UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: "https://custom.example",
				UpstreamAuthType: service.CustomUpstreamAuthType, CustomUpstreamConfig: customConfig,
				UpstreamRatioSyncDisabled: true, BalanceAutoDisableThreshold: &threshold,
			}).Error)

			summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Equal(t, 1, summary.BalanceUpdated)
			assert.Equal(t, test.wantEnabled, summary.ChannelsEnabled)
			storedChannel, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, storedChannel.Status)
			if test.wantStatus == common.ChannelStatusEnabled {
				assert.Empty(t, storedChannel.GetOtherInfo()["status_reason"])
			} else {
				assert.Equal(t, reason, storedChannel.GetOtherInfo()["status_reason"])
			}
		})
	}
}

func TestRunChannelRatioMonitorTaskCostRatioRecoveryRespectsSwitch(t *testing.T) {
	tests := []struct {
		name       string
		switchOn   bool
		wantStatus int
	}{
		{name: "switch enabled restores recovered channel", switchOn: true, wantStatus: common.ChannelStatusEnabled},
		{name: "switch disabled leaves recovered channel disabled", switchOn: false, wantStatus: common.ChannelStatusAutoDisabled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelMonitorControllerTestDB(t)
			useChannelMonitorOptionMap(t, map[string]string{
				"GroupRatio": `{"vip":1}`,
				channelMonitorAutoEnableOnCostRatioRecoveryOption: strconv.FormatBool(test.switchOn),
				channelMonitorAutoUpdateRetryCountOption:          "0",
			})
			disableChannelMonitorSSRFProtection(t)
			originalGroupRatios := ratio_setting.GroupRatio2JSONString()
			require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
			t.Cleanup(func() {
				require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
			})

			highRatio := 1.25
			balance := 100.0
			highConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
				Ratio: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &highRatio,
				},
				Balance: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &balance,
				},
			})
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Channel{
				Id: 1, Name: "cost recovery", Group: "vip", Status: common.ChannelStatusEnabled,
			}).Error)
			require.NoError(t, db.Create(&model.ChannelRatioMonitor{
				ChannelId: 1, Ratio: 1, UpstreamType: service.CustomUpstreamType,
				UpstreamBaseURL: "https://custom.example", UpstreamAuthType: service.CustomUpstreamAuthType,
				CustomUpstreamConfig: highConfig, UpstreamBalanceSyncDisabled: true,
				SingleChannelAction: channelMonitorPolicyActionDisableChannel,
			}).Error)

			firstSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Equal(t, 1, firstSummary.ChannelsDisabled)
			channel, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			require.Equal(t, common.ChannelStatusAutoDisabled, channel.Status)
			assert.Equal(t, "渠道监控：成本倍率高于分组倍率", channel.GetOtherInfo()["status_reason"])

			lowRatio := 0.8
			lowConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
				Ratio: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &lowRatio,
				},
				Balance: service.ChannelMonitorCustomMetricConfig{
					Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &balance,
				},
			})
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.ChannelRatioMonitor{}).
				Where("channel_id = ?", 1).
				Update("custom_upstream_config", lowConfig).Error)

			secondSummary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
			require.NoError(t, err)
			assert.Zero(t, secondSummary.ChannelsDisabled)
			if test.wantStatus == common.ChannelStatusEnabled {
				assert.Equal(t, 1, secondSummary.ChannelsEnabled)
			} else {
				assert.Zero(t, secondSummary.ChannelsEnabled)
			}
			channel, err = model.GetChannelById(1, true)
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, channel.Status)
			if test.wantStatus == common.ChannelStatusEnabled {
				assert.Empty(t, channel.GetOtherInfo()["status_reason"])
			}
		})
	}
}

func TestRunChannelRatioMonitorTaskDoesNotRestoreCostRatioChannelWithLowBalance(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		"GroupRatio": `{"vip":1}`,
		channelMonitorAutoEnableOnCostRatioRecoveryOption: "true",
		channelMonitorAutoUpdateRetryCountOption:          "0",
	})
	disableChannelMonitorSSRFProtection(t)
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.True(t, getChannelMonitorSettings().AutoEnableOnCostRatioRecovery)

	ratio := 0.8
	balance := 3.0
	customConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
		Ratio: service.ChannelMonitorCustomMetricConfig{
			Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &ratio,
		},
		Balance: service.ChannelMonitorCustomMetricConfig{
			Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &balance,
		},
	})
	require.NoError(t, err)
	channel := model.Channel{
		Id: 1, Name: "low balance", Group: "vip", Status: common.ChannelStatusAutoDisabled,
	}
	channel.SetOtherInfo(map[string]interface{}{
		"status_reason": channelMonitorCostRatioPolicyDisableReason,
	})
	require.NoError(t, db.Create(&channel).Error)
	threshold := 4.0
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: "https://custom.example",
		UpstreamAuthType: service.CustomUpstreamAuthType, CustomUpstreamConfig: customConfig,
		BalanceAutoDisableThreshold: &threshold,
		SingleChannelAction:         channelMonitorPolicyActionDisableChannel,
	}).Error)

	summary, err := runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Updated)
	assert.Equal(t, 1, summary.BalanceUpdated)
	assert.Zero(t, summary.ChannelsEnabled)
	storedChannel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
	assert.Equal(t, channelMonitorCostRatioPolicyDisableReason, storedChannel.GetOtherInfo()["status_reason"])
}

func TestRunChannelRatioMonitorTaskDoesNotEnableOtherDisabledChannels(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{
		channelMonitorAutoEnableOnCostRatioRecoveryOption: "true",
		channelMonitorAutoUpdateRetryCountOption:          "0",
	})
	disableChannelMonitorSSRFProtection(t)

	ratio := 0.5
	balance := 100.0
	customConfig, err := service.MarshalChannelMonitorCustomUpstreamConfig(service.ChannelMonitorCustomUpstreamConfig{
		Ratio: service.ChannelMonitorCustomMetricConfig{
			Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &ratio,
		},
		Balance: service.ChannelMonitorCustomMetricConfig{
			Source: service.ChannelMonitorCustomSourceFixed, FixedValue: &balance,
		},
	})
	require.NoError(t, err)
	manualChannel := model.Channel{Id: 1, Name: "manual", Group: "vip", Status: common.ChannelStatusManuallyDisabled}
	manualChannel.SetOtherInfo(map[string]interface{}{
		"status_reason": "渠道监控：上游倍率或余额更新失败",
	})
	unrelatedChannel := model.Channel{Id: 2, Name: "unrelated", Group: "vip", Status: common.ChannelStatusAutoDisabled}
	unrelatedChannel.SetOtherInfo(map[string]interface{}{
		"status_reason": "其他系统自动禁用原因",
	})
	require.NoError(t, db.Create([]model.Channel{manualChannel, unrelatedChannel}).Error)
	require.NoError(t, db.Create([]model.ChannelRatioMonitor{
		{
			ChannelId: 1, UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: "https://custom.example",
			UpstreamAuthType: service.CustomUpstreamAuthType, CustomUpstreamConfig: customConfig,
			UpstreamBalanceSyncDisabled: true,
		},
		{
			ChannelId: 2, UpstreamType: service.CustomUpstreamType, UpstreamBaseURL: "https://custom.example",
			UpstreamAuthType: service.CustomUpstreamAuthType, CustomUpstreamConfig: customConfig,
			UpstreamBalanceSyncDisabled: true,
		},
	}).Error)

	_, err = runChannelRatioMonitorTaskOnce(context.Background(), nil, nil)
	require.NoError(t, err)
	storedManual, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, storedManual.Status)
	storedUnrelated, err := model.GetChannelById(2, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedUnrelated.Status)
}

func TestCostRatioRecoveryRequiresEveryGroupToRecover(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{
		Id: 1, Name: "multi group", Group: "vip,discount", Status: common.ChannelStatusAutoDisabled,
	}
	channel.SetOtherInfo(map[string]interface{}{
		"status_reason": channelMonitorCostRatioPolicyDisableReason,
	})
	require.NoError(t, db.Create(&channel).Error)

	channels := []*model.Channel{&channel}
	enabledChannelIds, err := autoEnableChannelsAfterCostRatioRecovery(
		context.Background(),
		channels,
		map[int]channelMonitorPolicyInput{1: {CostRatio: 0.8}},
		map[string]float64{"vip": 1, "discount": 0.75},
		map[string]float64{},
	)
	require.NoError(t, err)
	assert.Empty(t, enabledChannelIds)
	storedChannel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
}

func TestCostRatioRecoveryRequiresStrictlyLowerRatio(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{
		Id: 1, Name: "equal ratio", Group: "vip", Status: common.ChannelStatusAutoDisabled,
	}
	channel.SetOtherInfo(map[string]interface{}{
		"status_reason": channelMonitorCostRatioPolicyDisableReason,
	})
	require.NoError(t, db.Create(&channel).Error)

	enabledChannelIds, err := autoEnableChannelsAfterCostRatioRecovery(
		context.Background(),
		[]*model.Channel{&channel},
		map[int]channelMonitorPolicyInput{1: {CostRatio: 1}},
		map[string]float64{"vip": 1},
		map[string]float64{},
	)
	require.NoError(t, err)
	assert.Empty(t, enabledChannelIds)
	storedChannel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
}

func TestCostRatioRecoveryUsesGroupCoefficient(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{
		Id: 1, Name: "coefficient", Group: "vip", Status: common.ChannelStatusAutoDisabled,
	}
	channel.SetOtherInfo(map[string]interface{}{
		"status_reason": channelMonitorCostRatioPolicyDisableReason,
	})
	require.NoError(t, db.Create(&channel).Error)

	enabledChannelIds, err := autoEnableChannelsAfterCostRatioRecovery(
		context.Background(),
		[]*model.Channel{&channel},
		map[int]channelMonitorPolicyInput{1: {CostRatio: 0.8}},
		map[string]float64{"vip": 1},
		map[string]float64{"vip": 1.5},
	)
	require.NoError(t, err)
	assert.Empty(t, enabledChannelIds)
	storedChannel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
}

func TestCostRatioRecoverySkipsStaleMonitorRevision(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{
		Id: 1, Name: "stale recovery", Group: "vip", Status: common.ChannelStatusAutoDisabled,
	}
	channel.SetOtherInfo(map[string]interface{}{
		"status_reason": channelMonitorCostRatioPolicyDisableReason,
	})
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.ChannelRatioMonitor{
		ChannelId: 1, UpstreamRevision: 2,
	}).Error)

	enabledChannelIds, err := autoEnableChannelsAfterCostRatioRecovery(
		context.Background(),
		[]*model.Channel{&channel},
		map[int]channelMonitorPolicyInput{1: {UpstreamRevision: 1, CostRatio: 0.5}},
		map[string]float64{"vip": 1},
		map[string]float64{},
	)
	require.NoError(t, err)
	assert.Empty(t, enabledChannelIds)

	storedChannel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusAutoDisabled, storedChannel.Status)
}
