package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeduplicateChannelStatusProbeClaimsByIdentitySharesOneTargetPerLogicalGroup(t *testing.T) {
	claims := []model.ChannelStatusProbeClaim{
		{Config: model.ChannelStatusProbeConfig{ChannelId: 101}, Models: []string{"model-a"}},
		{Config: model.ChannelStatusProbeConfig{ChannelId: 102}, Models: []string{"model-a"}},
		{Config: model.ChannelStatusProbeConfig{ChannelId: 201}, Models: []string{"model-b"}},
	}
	identities := []model.LogicalChannelIdentity{
		{ChannelID: 101, LogicalChannelID: 9001, Revision: 2},
		{ChannelID: 102, LogicalChannelID: 9001, Revision: 2},
		{ChannelID: 201, LogicalChannelID: 201},
	}

	winners, duplicates := deduplicateChannelStatusProbeClaimsByIdentity(claims, identities)
	require.Len(t, winners, 2)
	require.Len(t, duplicates, 1)
	assert.Equal(t, 101, winners[0].Config.ChannelId)
	assert.Equal(t, int64(9001), winners[0].Config.LogicalChannelId)
	assert.Equal(t, 201, winners[1].Config.ChannelId)
	assert.Equal(t, []int{1}, duplicates)
}

func TestDeduplicateChannelStatusProbeClaimsPrefersConfiguredTarget(t *testing.T) {
	claims := []model.ChannelStatusProbeClaim{
		{Config: model.ChannelStatusProbeConfig{ChannelId: 301}},
		{Config: model.ChannelStatusProbeConfig{ChannelId: 302}, Models: []string{"model-a"}},
	}
	identities := []model.LogicalChannelIdentity{
		{ChannelID: 301, LogicalChannelID: 9100},
		{ChannelID: 302, LogicalChannelID: 9100},
	}

	winners, duplicates := deduplicateChannelStatusProbeClaimsByIdentity(claims, identities)
	require.Len(t, winners, 1)
	assert.Equal(t, 302, winners[0].Config.ChannelId)
	assert.Equal(t, int64(9100), winners[0].Config.LogicalChannelId)
	assert.Equal(t, []int{0}, duplicates)
}

func TestChannelStatusProbeOutcomeKeepsActualMemberForPersistence(t *testing.T) {
	outcome := channelStatusProbeOutcome{ActualChannelId: 402, TestExecuted: true}
	claim := model.ChannelStatusProbeClaim{
		Config: model.ChannelStatusProbeConfig{ChannelId: 401, LogicalChannelId: 9200, Revision: 3},
		RunId:  "run-1", Trigger: model.ChannelStatusProbeTriggerScheduled,
	}
	execution := model.ChannelStatusProbeExecution{
		ChannelId: claim.Config.ChannelId, LogicalChannelId: claim.Config.LogicalChannelId, ActualChannelId: outcome.ActualChannelId,
	}
	assert.Equal(t, 401, execution.ChannelId)
	assert.Equal(t, int64(9200), execution.LogicalChannelId)
	assert.Equal(t, 402, execution.ActualChannelId)
}

func TestChannelStatusProbeMemberSelectionExcludesOnlyUnavailableMember(t *testing.T) {
	snapshot := service.LogicalChannelSelectionSnapshot{
		LogicalChannelID: 9300,
		Revision:         1,
		Status:           model.ChannelLogicalGroupStatusEnabled,
		Members: []model.LogicalChannelMemberSnapshot{
			{ChannelID: 501, Weight: 3},
			{ChannelID: 502, Weight: 1},
		},
	}
	selected, err := service.SelectLogicalChannelMember(snapshot, []service.LogicalChannelMemberAvailability{
		{ChannelID: 501, Weight: 3, Available: false, Reason: "认证失败"},
		{ChannelID: 502, Weight: 1, Available: true},
	}, model.LogicalChannelRandomFunc(func(max uint64) uint64 { return 0 }))
	require.NoError(t, err)
	assert.Equal(t, 502, selected)
}

func TestChannelStatusProbeSelectsMemberPerModelFromFrozenSnapshot(t *testing.T) {
	snapshot := model.LogicalChannelGroupSnapshot{
		LogicalChannelID: 9400,
		Revision:         2,
		Status:           model.ChannelLogicalGroupStatusEnabled,
		Members: []model.LogicalChannelMemberSnapshot{
			{ChannelID: 601, Weight: 3},
			{ChannelID: 602, Weight: 1},
		},
	}
	members := map[int]*model.Channel{
		601: {Id: 601, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-a"},
		602: {Id: 602, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-b"},
	}
	rng := model.LogicalChannelRandomFunc(func(uint64) uint64 { return 0 })

	selectedA, err := selectChannelStatusProbeMemberForModel(snapshot, members, "gpt-a", nil, rng)
	require.NoError(t, err)
	assert.Equal(t, 601, selectedA.Id)

	selectedB, err := selectChannelStatusProbeMemberForModel(snapshot, members, "gpt-b", nil, rng)
	require.NoError(t, err)
	assert.Equal(t, 602, selectedB.Id)
}

func TestLogicalChannelStatusProbeConfigAcceptsUnionOfMemberModels(t *testing.T) {
	members := map[int]*model.Channel{
		601: {Id: 601, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-a"},
		602: {Id: 602, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-b"},
	}

	models, err := normalizeLogicalChannelStatusProbeModels(members, []string{"gpt-a", "gpt-b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-a", "gpt-b"}, models)
}

func TestChannelStatusProbeBusyMemberFallsBackToSibling(t *testing.T) {
	snapshot := model.LogicalChannelGroupSnapshot{
		LogicalChannelID: 9500,
		Revision:         3,
		Status:           model.ChannelLogicalGroupStatusEnabled,
		Members: []model.LogicalChannelMemberSnapshot{
			{ChannelID: 701, Weight: 10},
			{ChannelID: 702, Weight: 1},
		},
	}
	members := map[int]*model.Channel{
		701: {Id: 701, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-a"},
		702: {Id: 702, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "gpt-a"},
	}
	rng := model.LogicalChannelRandomFunc(func(uint64) uint64 { return 0 })
	selected := make([]int, 0, 2)

	outcome, err := executeChannelStatusProbeModelWithMemberFailover(
		snapshot, members, "gpt-a", rng,
		func(channel *model.Channel) channelStatusProbeOutcome {
			selected = append(selected, channel.Id)
			if channel.Id == 701 {
				return channelStatusProbeOutcome{Result: model.ChannelStatusProbeResultSkipped, ErrorCode: "channel_busy"}
			}
			return channelStatusProbeOutcome{Result: model.ChannelStatusProbeResultSuccess, TestExecuted: true}
		},
	)
	require.NoError(t, err)
	assert.Equal(t, []int{701, 702}, selected)
	assert.Equal(t, model.ChannelStatusProbeResultSuccess, outcome.Result)
	assert.Equal(t, 702, outcome.ActualChannelId)
}
