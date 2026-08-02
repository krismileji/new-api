package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateChannelMonitorSmartScheduleManualRoutingPersistsValues(t *testing.T) {
	db := setupChannelMonitorControllerTestDB(t)
	priority := int64(5)
	weight := uint(10)
	require.NoError(t, db.Create(&model.Channel{
		Id: 1410, Name: "人工路由", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		ChannelId: 1410, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelSmartScheduleRouteState{
		ChannelId: 1410, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Excluded: true, Revision: 1,
	}).Error)

	ctx, recorder := newChannelMonitorControllerContext(
		t,
		http.MethodPut,
		"/api/channel_monitor/channel/1410/schedule/route/routing",
		map[string]any{
			"group": "vip", "model": "model-a", "priority": 30, "weight": 600,
		},
	)
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: "1410"})
	UpdateChannelMonitorSmartScheduleManualRouting(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	var ability model.Ability
	require.NoError(t, db.Where(&model.Ability{ChannelId: 1410, Group: "vip", Model: "model-a"}).First(&ability).Error)
	require.NotNil(t, ability.Priority)
	assert.Equal(t, int64(30), *ability.Priority)
	assert.Equal(t, uint(600), ability.Weight)
}
