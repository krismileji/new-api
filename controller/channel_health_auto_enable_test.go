package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorAllowsHealthCheckAutoEnable(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	useChannelMonitorOptionMap(t, map[string]string{})
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	lowBalance := 3.0
	validBalance := 5.0
	balanceThreshold := 5.0
	tests := []struct {
		name         string
		statusReason string
		monitor      *model.ChannelRatioMonitor
		want         bool
	}{
		{
			name: "没有渠道监控配置时允许启用",
			want: true,
		},
		{
			name: "余额低于自动禁用阈值时拒绝启用",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamRatioSyncDisabled: true,
				UpstreamBalance: &lowBalance, BalanceAutoDisableThreshold: &balanceThreshold,
			},
		},
		{
			name: "余额等于自动禁用阈值时允许启用",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamRatioSyncDisabled: true,
				UpstreamBalance: &validBalance, BalanceAutoDisableThreshold: &balanceThreshold,
			},
			want: true,
		},
		{
			name: "关闭余额同步后不应用余额门槛",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamRatioSyncDisabled: true,
				UpstreamBalanceSyncDisabled: true, UpstreamBalance: &lowBalance,
				BalanceAutoDisableThreshold: &balanceThreshold,
			},
			want: true,
		},
		{
			name: "成本倍率高于分组倍率时拒绝启用",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamBalanceSyncDisabled: true,
				Ratio: 1.2, UpdatedTime: 1, SingleChannelAction: channelMonitorPolicyActionDisableChannel,
			},
		},
		{
			name: "余额和倍率同时开启时必须全部满足",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType:    service.CustomUpstreamType,
				UpstreamBalance: &validBalance, BalanceAutoDisableThreshold: &balanceThreshold,
				Ratio: 1.2, UpdatedTime: 1, SingleChannelAction: channelMonitorPolicyActionDisableChannel,
			},
		},
		{
			name: "余额和倍率同时满足时允许启用",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType:    service.CustomUpstreamType,
				UpstreamBalance: &validBalance, BalanceAutoDisableThreshold: &balanceThreshold,
				Ratio: 0.8, UpdatedTime: 1, SingleChannelAction: channelMonitorPolicyActionDisableChannel,
			},
			want: true,
		},
		{
			name:         "其他原因禁用且成本倍率等于分组倍率时允许启用",
			statusReason: "渠道健康检查：请求失败",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamBalanceSyncDisabled: true,
				Ratio: 1, UpdatedTime: 1, SingleChannelAction: channelMonitorPolicyActionDisableChannel,
			},
			want: true,
		},
		{
			name:         "成本倍率原因禁用时必须严格低于分组倍率",
			statusReason: channelMonitorCostRatioPolicyDisableReason,
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamBalanceSyncDisabled: true,
				Ratio: 1, UpdatedTime: 1, SingleChannelAction: channelMonitorPolicyActionDisableChannel,
			},
		},
		{
			name: "关闭倍率同步后不应用倍率门槛",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamRatioSyncDisabled: true,
				UpstreamBalanceSyncDisabled: true, Ratio: 1.2, UpdatedTime: 1,
				SingleChannelAction: channelMonitorPolicyActionDisableChannel,
			},
			want: true,
		},
		{
			name: "未配置禁用渠道策略时不应用倍率门槛",
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamBalanceSyncDisabled: true,
				Ratio: 1.2, UpdatedTime: 1, SingleChannelAction: channelMonitorPolicyActionUpdateGroupRatio,
			},
			want: true,
		},
		{
			name:         "渠道监控更新仍失败时拒绝启用",
			statusReason: channelMonitorUpdateFailureDisableReason,
			monitor: &model.ChannelRatioMonitor{
				UpstreamType: service.CustomUpstreamType, UpstreamBalanceSyncDisabled: true,
				Ratio: 0.8, UpdatedTime: 1, LastFetchStatus: model.ChannelRatioFetchStatusFailed,
				LastFetchError: "上游请求失败",
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := model.Channel{
				Id: index + 1, Name: test.name, Group: "vip", Status: common.ChannelStatusAutoDisabled,
			}
			channel.SetOtherInfo(map[string]interface{}{"status_reason": test.statusReason})
			require.NoError(t, db.Create(&channel).Error)
			if test.monitor != nil {
				monitor := *test.monitor
				monitor.ChannelId = channel.Id
				require.NoError(t, db.Create(&monitor).Error)
			}

			allowed, err := channelMonitorAllowsHealthCheckAutoEnable(channel.Id)
			require.NoError(t, err)
			assert.Equal(t, test.want, allowed)
		})
	}
}

func TestChannelMonitorAllowsHealthCheckAutoEnableDoesNotOverrideManualDisable(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	channel := model.Channel{
		Id: 1, Name: "管理员已禁用", Group: "vip", Status: common.ChannelStatusManuallyDisabled,
	}
	require.NoError(t, db.Create(&channel).Error)

	allowed, err := channelMonitorAllowsHealthCheckAutoEnable(channel.Id)
	require.NoError(t, err)
	assert.False(t, allowed)
}
