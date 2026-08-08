package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshLockedTaskChannelReloadsCurrentChannelAndRejectsDisabledChannel(t *testing.T) {
	t.Cleanup(model.InitChannelCache)
	db := setupChannelMonitorControllerTestDB(t)
	baseURL := "https://current.example"
	priority := int64(100)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 65, Type: constant.ChannelTypeOpenAI, Name: "origin", Key: "current-key",
		Status: common.ChannelStatusEnabled, BaseURL: &baseURL, Group: "vip", Models: "model-a",
		Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "vip", Model: "model-a", ChannelId: 65, Enabled: true, Priority: &priority, Weight: weight,
	}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()

	staleBaseURL := "https://stale.example"
	stale := &model.Channel{Id: 65, Key: "stale-key", BaseURL: &staleBaseURL}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{LockedChannel: stale}}

	selected, taskErr := refreshLockedTaskChannel(info, stale)

	require.Nil(t, taskErr)
	require.NotNil(t, selected)
	assert.Equal(t, "current-key", selected.Key)
	assert.Equal(t, baseURL, selected.GetBaseURL())
	assert.Same(t, selected, info.LockedChannel)

	model.CacheUpdateChannelStatus(65, common.ChannelStatusAutoDisabled)
	selected, taskErr = refreshLockedTaskChannel(info, selected)

	assert.Nil(t, selected)
	require.NotNil(t, taskErr)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode)
}
