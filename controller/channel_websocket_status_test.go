package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A disable may commit before a WebSocket registers for notifications. The
// registration recheck must consult the status persisted after selection.
func TestChannelWebSocketStatusRechecksPersistedDisable(t *testing.T) {
	setupResponsesWSRequestTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}))
	for _, tc := range []struct {
		name                 string
		status               int
		reason               string
		wantDrain, wantClose bool
	}{
		{"enabled", common.ChannelStatusEnabled, "", false, false},
		{"monitor", common.ChannelStatusAutoDisabled, "渠道监控：上游余额 0", true, false},
		{"manual", common.ChannelStatusManuallyDisabled, "管理员禁用", false, true},
		{"upstream-error", common.ChannelStatusAutoDisabled, "invalid API key", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			channel := &model.Channel{Name: "status-recheck", Key: "test", Status: common.ChannelStatusEnabled}
			require.NoError(t, model.DB.Create(channel).Error)
			t.Cleanup(func() { require.NoError(t, model.DB.Delete(channel).Error) })
			channel.SetOtherInfo(map[string]interface{}{"status_reason": tc.reason})
			require.NoError(t, model.DB.Model(channel).Updates(map[string]interface{}{"status": tc.status, "other_info": channel.OtherInfo}).Error)
			var drained, closed bool
			service.EnforceChannelWebSocketStatus(channel.Id, func(string) { drained = true }, func(string) { closed = true })
			assert.Equal(t, tc.wantDrain, drained)
			assert.Equal(t, tc.wantClose, closed)
		})
	}
	var closed bool
	service.EnforceChannelWebSocketStatus(999999, func(string) { t.Error("missing channel must not drain") }, func(string) { closed = true })
	assert.True(t, closed, "a missing channel must fail closed")
}
