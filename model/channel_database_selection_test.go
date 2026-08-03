package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func useDatabaseChannelSelection(t *testing.T) {
	t.Helper()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })
}

func TestDatabaseChannelSelectionPrefersAvailableExactModelPool(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)

	requestModel := "gpt-4o-gizmo-production"
	wildcardModel := "gpt-4o-gizmo-*"
	exactPriority := int64(10)
	wildcardPriority := int64(100)
	weight := uint(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9201, Name: "exact", Status: common.ChannelStatusEnabled},
		{Id: 9202, Name: "wildcard", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9201, Group: "vip", Model: requestModel, Enabled: true, Priority: &exactPriority, Weight: weight},
		{ChannelId: 9202, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &wildcardPriority, Weight: weight},
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", requestModel, 0, "")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 9201, channel.Id)
}

func TestDatabaseChannelSelectionFallsBackAfterExactPoolFilters(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)

	requestModel := "gpt-4o-gizmo-production"
	wildcardModel := "gpt-4o-gizmo-*"
	priority := int64(100)
	weight := uint(100)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9211, Name: "disabled channel", Status: common.ChannelStatusManuallyDisabled},
		{Id: 9212, Name: "disabled ability", Status: common.ChannelStatusEnabled},
		{Id: 9213, Name: "excluded channel", Status: common.ChannelStatusEnabled},
		{Id: 9214, Name: "wildcard", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9211, Group: "vip", Model: requestModel, Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 9212, Group: "vip", Model: requestModel, Enabled: false, Priority: &priority, Weight: weight},
		{ChannelId: 9213, Group: "vip", Model: requestModel, Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 9214, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)

	channel, err := GetRandomSatisfiedChannel(
		"vip",
		requestModel,
		3,
		"",
		ChannelSelectionOptions{ExcludedChannelIds: []int{9213}},
	)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 9214, channel.Id)
}

func TestDatabaseChannelSelectionFallsBackAfterRequestPathFilter(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)

	requestModel := "gpt-4o-gizmo-production"
	wildcardModel := "gpt-4o-gizmo-*"
	priority := int64(100)
	weight := uint(100)
	exact := Channel{Id: 9221, Name: "exact wrong path", Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	exact.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/chat/completions", Models: []string{requestModel}},
	}}})
	wildcard := Channel{Id: 9222, Name: "wildcard response path", Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	wildcard.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: "/v1/responses", UpstreamPath: "/v1/responses", Models: []string{requestModel}},
	}}})
	require.NoError(t, db.Create(&[]Channel{exact, wildcard}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: exact.Id, Group: "vip", Model: requestModel, Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: wildcard.Id, Group: "vip", Model: wildcardModel, Enabled: true, Priority: &priority, Weight: weight},
	}).Error)

	channel, err := GetRandomSatisfiedChannel("vip", requestModel, 0, "/v1/responses")
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, wildcard.Id, channel.Id)
}
