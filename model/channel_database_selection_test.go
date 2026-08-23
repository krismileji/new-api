package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

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

func TestDatabaseSmartScheduleLogicalGroupSelectsRemainingMemberAfterExclusion(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	require.NoError(t, db.AutoMigrate(&ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}))

	logicalID := int64(9300)
	priority := int64(100)
	weight := uint(100)
	require.NoError(t, db.Create(&ChannelLogicalGroup{
		Id: logicalID, Name: "同一上游", Status: ChannelLogicalGroupStatusEnabled, Revision: 3,
	}).Error)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9301, Name: "key-a", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
		{Id: 9302, Name: "key-b", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelLogicalGroupMember{
		{LogicalGroupID: logicalID, ChannelID: 9301, Weight: 100, AddressFingerprint: strings.Repeat("a", 64)},
		{LogicalGroupID: logicalID, ChannelID: 9302, Weight: 0, AddressFingerprint: strings.Repeat("a", 64)},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9301, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
		{ChannelId: 9302, Group: "vip", Model: "model-a", Enabled: true, Priority: &priority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 9301, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 9302, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	policy := &channelSmartScheduleTrafficPolicy{
		enabled: true, allModels: map[string]struct{}{"vip": {}},
		modelsByGroup: map[string]map[string]struct{}{},
	}

	channel, err := getChannelFromDatabasePoolWithTrafficPolicy(
		"vip", "model-a", "model-a", 0, "",
		ChannelSelectionOptions{ExcludedChannelIds: []int{9301}}, policy,
	)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 9302, channel.Id)
}

func TestDatabaseLogicalGroupSelectionDisabledKeepsPhysicalRouting(t *testing.T) {
	tests := []struct {
		name          string
		globalEnabled string
		policy        *channelSmartScheduleTrafficPolicy
	}{
		{
			name: "全局关闭", globalEnabled: "false",
			policy: &channelSmartScheduleTrafficPolicy{
				enabled: true, allModels: map[string]struct{}{"vip": {}},
				modelsByGroup: map[string]map[string]struct{}{},
			},
		},
		{
			name: "单组未托管", globalEnabled: "true",
			policy: &channelSmartScheduleTrafficPolicy{
				enabled: true, allModels: map[string]struct{}{"other": {}},
				modelsByGroup: map[string]map[string]struct{}{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupChannelSmartScheduleRouteTestDB(t)
			useDatabaseChannelSelection(t)
			t.Setenv(ChannelLogicalGroupGlobalEnableEnv, test.globalEnabled)
			require.NoError(t, db.AutoMigrate(&ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}))
			logicalID := int64(9400)
			highPriority := int64(100)
			lowPriority := int64(10)
			weight := uint(100)
			require.NoError(t, db.Create(&ChannelLogicalGroup{
				Id: logicalID, Name: "关闭回退", Status: ChannelLogicalGroupStatusEnabled, Revision: 1,
			}).Error)
			require.NoError(t, db.Create(&[]Channel{
				{Id: 9401, Name: "physical-primary", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
				{Id: 9402, Name: "logical-weight-primary", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
			}).Error)
			require.NoError(t, db.Create(&[]ChannelLogicalGroupMember{
				{LogicalGroupID: logicalID, ChannelID: 9401, Weight: 0, AddressFingerprint: strings.Repeat("b", 64)},
				{LogicalGroupID: logicalID, ChannelID: 9402, Weight: 100, AddressFingerprint: strings.Repeat("b", 64)},
			}).Error)
			require.NoError(t, db.Create(&[]Ability{
				{ChannelId: 9401, Group: "vip", Model: "model-a", Enabled: true, Priority: &highPriority, Weight: weight},
				{ChannelId: 9402, Group: "vip", Model: "model-a", Enabled: true, Priority: &lowPriority, Weight: weight},
			}).Error)
			if test.policy.managesPool("vip", "model-a") {
				require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
					{ChannelId: 9401, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
					{ChannelId: 9402, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
				}).Error)
			}

			channel, err := getChannelFromDatabasePoolWithTrafficPolicy(
				"vip", "model-a", "model-a", 0, "", ChannelSelectionOptions{}, test.policy,
			)
			require.NoError(t, err)
			require.NotNil(t, channel)
			assert.Equal(t, 9401, channel.Id)
		})
	}
}

func TestDatabaseSmartScheduleLogicalGroupUsesLogicalRouteStateOverlay(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	useDatabaseChannelSelection(t)
	t.Setenv(ChannelLogicalGroupGlobalEnableEnv, "true")
	require.NoError(t, db.AutoMigrate(
		&ChannelLogicalGroup{}, &ChannelLogicalGroupMember{}, &ChannelLogicalSmartScheduleRouteState{},
	))
	logicalID := int64(9450)
	physicalPriority := int64(100)
	standalonePriority := int64(50)
	weight := uint(100)
	require.NoError(t, db.Create(&ChannelLogicalGroup{
		Id: logicalID, Name: "逻辑状态覆盖", Status: ChannelLogicalGroupStatusEnabled, Revision: 2,
	}).Error)
	require.NoError(t, db.Create(&[]Channel{
		{Id: 9451, Name: "key-a", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
		{Id: 9452, Name: "key-b", Status: common.ChannelStatusEnabled, LogicalChannelID: &logicalID},
		{Id: 9453, Name: "standalone", Status: common.ChannelStatusEnabled},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelLogicalGroupMember{
		{LogicalGroupID: logicalID, ChannelID: 9451, Weight: 1, AddressFingerprint: strings.Repeat("d", 64)},
		{LogicalGroupID: logicalID, ChannelID: 9452, Weight: 1, AddressFingerprint: strings.Repeat("d", 64)},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		{ChannelId: 9451, Group: "vip", Model: "model-a", Enabled: true, Priority: &physicalPriority, Weight: weight},
		{ChannelId: 9452, Group: "vip", Model: "model-a", Enabled: true, Priority: &physicalPriority, Weight: weight},
		{ChannelId: 9453, Group: "vip", Model: "model-a", Enabled: true, Priority: &standalonePriority, Weight: weight},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelSmartScheduleRouteState{
		{ChannelId: 9451, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 9452, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
		{ChannelId: 9453, GroupName: "vip", ModelName: "model-a", ParticipationSet: true},
	}).Error)
	payload, err := encodeLogicalSmartScheduleRouteStateWithRouting(
		ChannelSmartScheduleRouteState{ParticipationSet: true, Revision: 1}, 10, weight,
	)
	require.NoError(t, err)
	require.NoError(t, db.Create(&ChannelLogicalSmartScheduleRouteState{
		LogicalGroupID: logicalID, LogicalRevision: 2, GroupName: "vip", ModelName: "model-a",
		StateRevision: 1, StateJSON: payload, UpdatedAt: common.GetTimestamp(),
	}).Error)
	policy := &channelSmartScheduleTrafficPolicy{
		enabled: true, allModels: map[string]struct{}{"vip": {}},
		modelsByGroup: map[string]map[string]struct{}{},
	}

	channel, err := getChannelFromDatabasePoolWithTrafficPolicy(
		"vip", "model-a", "model-a", 0, "", ChannelSelectionOptions{}, policy,
	)
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, 9453, channel.Id)
}
