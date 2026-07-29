package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLookupChannelSmartScheduleProbeRouteLoadsOnlyConfiguredRoute(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(50)
	const targetChannelID = 2101
	require.NoError(t, db.Create(&Channel{
		Id: targetChannelID, Name: "target", Status: common.ChannelStatusEnabled,
		Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: targetChannelID, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: targetChannelID, GroupName: "vip", ModelName: "model-a", ParticipationSet: true,
	}).Error)
	for index := 0; index < 25; index++ {
		channelID := 2200 + index
		require.NoError(t, db.Create(&Channel{
			Id: channelID, Name: fmt.Sprintf("other-%d", index), Status: common.ChannelStatusEnabled,
			Priority: &priority, Weight: &weight,
		}).Error)
		require.NoError(t, db.Create(&Ability{
			ChannelId: channelID, Group: "other", Model: fmt.Sprintf("model-%d", index), Enabled: true,
			Priority: &priority, Weight: weight,
		}).Error)
		require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
			ChannelId: channelID, GroupName: "other", ModelName: fmt.Sprintf("model-%d", index), ParticipationSet: true,
		}).Error)
	}

	queries := make([]string, 0, 3)
	callbackName := "test:smart_schedule_probe_lookup_bounded_queries"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		queries = append(queries, tx.Statement.SQL.String())
	}))
	t.Cleanup(func() {
		require.NoError(t, db.Callback().Query().Remove(callbackName))
	})

	route, channel, found, err := LookupChannelSmartScheduleProbeRoute(targetChannelID, "vip", "model-a")
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, channel)
	assert.Equal(t, targetChannelID, channel.Id)
	assert.Equal(t, targetChannelID, route.ChannelId)
	assert.Equal(t, "vip", route.Group)
	assert.Equal(t, "model-a", route.Model)
	assert.True(t, route.State.Participates())
	require.Len(t, queries, 3)
	for _, query := range queries {
		assert.Contains(t, strings.ToUpper(query), "LIMIT 1")
	}
}

func TestLookupChannelSmartScheduleProbeRouteRequiresAbilityChannelAndState(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(50)
	require.NoError(t, db.Create(&Channel{
		Id: 2301, Name: "without state", Status: common.ChannelStatusEnabled,
		Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 2301, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)

	_, channel, found, err := LookupChannelSmartScheduleProbeRoute(2301, "vip", "model-a")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, channel)

	_, channel, found, err = LookupChannelSmartScheduleProbeRoute(2301, "vip", "missing")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, channel)
}
