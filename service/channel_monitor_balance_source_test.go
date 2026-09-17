package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeChannelMonitorBalanceAccountSource(t *testing.T) {
	config := ChannelMonitorCustomUpstreamConfig{
		Ratio:   ChannelMonitorCustomMetricConfig{Source: "fixed", FixedValue: common.GetPointer(2.0)},
		Balance: ChannelMonitorCustomMetricConfig{Source: "account", AccountID: 9, FixedValue: common.GetPointer(999.0), Request: &ChannelMonitorCustomRequestConfig{Path: "/old-balance"}},
	}
	normalized, err := NormalizeChannelMonitorCustomUpstreamConfig(config)
	require.NoError(t, err)
	assert.Equal(t, ChannelMonitorCustomMetricConfig{Source: "account", AccountID: 9}, normalized.Balance, "账户余额引用不保留另一份接口或固定余额")
	for _, tc := range []struct {
		name      string
		ratio     bool
		accountID int
		reuse     bool
	}{
		{name: "未选择账户", accountID: 0},
		{name: "负账户编号", accountID: -1},
		{name: "倍率不能来自余额账户", ratio: true, accountID: 9},
		{name: "账户余额不能复用倍率请求", accountID: 9, reuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := config
			invalid.Balance.AccountID = tc.accountID
			invalid.BalanceReuseRatioRequest = tc.reuse
			if tc.ratio {
				invalid.Ratio = invalid.Balance
			}
			_, err := NormalizeChannelMonitorCustomUpstreamConfig(invalid)
			require.Error(t, err)
		})
	}
}
