package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type channelSmartScheduleExecutionDetailFixture struct {
	ChannelId int                               `json:"channel_id"`
	Reason    string                            `json:"reason"`
	Details   *ChannelSmartScheduleScoreDetails `json:"score_details"`
}

func TestChannelSmartScheduleExecutionDetailsPreserveOrderedRuntimePayloads(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))

	firstScore := 0.92
	secondScore := 0.61
	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		"schedule-task-1",
		[]ChannelSmartScheduleExecutionDetailInput{
			{
				AdjustmentIndex: 0,
				Payload: channelSmartScheduleExecutionDetailFixture{
					ChannelId: 11,
					Reason:    "评分最高，设为主渠道",
					Details: &ChannelSmartScheduleScoreDetails{
						Version:    ChannelSmartScheduleScoreDetailsVersion,
						Strategy:   "smart",
						FinalScore: &firstScore,
					},
				},
			},
			{
				AdjustmentIndex: 1,
				Payload: channelSmartScheduleExecutionDetailFixture{
					ChannelId: 12,
					Reason:    "评分较低，保留备用流量",
					Details: &ChannelSmartScheduleScoreDetails{
						Version:    ChannelSmartScheduleScoreDetailsVersion,
						Strategy:   "smart",
						FinalScore: &secondScore,
					},
				},
			},
		},
	))

	loaded, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-task-1", "missing"})
	require.NoError(t, err)
	require.Len(t, loaded["schedule-task-1"], 2)
	assert.Equal(t, 0, loaded["schedule-task-1"][0].AdjustmentIndex)
	assert.Equal(t, 1, loaded["schedule-task-1"][1].AdjustmentIndex)
	assert.NotContains(t, loaded, "missing")

	var first channelSmartScheduleExecutionDetailFixture
	require.NoError(t, common.UnmarshalJsonStr(loaded["schedule-task-1"][0].Payload, &first))
	assert.Equal(t, 11, first.ChannelId)
	assert.Equal(t, "评分最高，设为主渠道", first.Reason)
	require.NotNil(t, first.Details)
	assert.InDelta(t, firstScore, *first.Details.FinalScore, 1e-9)
}

func TestChannelSmartScheduleExecutionDetailsReplaceARepeatedTaskSnapshot(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	require.NoError(t, db.AutoMigrate(&ChannelSmartScheduleExecutionDetail{}))

	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		"schedule-task-2",
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleExecutionDetailFixture{
				ChannelId: 21,
				Reason:    "旧快照",
			},
		}},
	))
	require.NoError(t, SaveChannelSmartScheduleExecutionDetails(
		"schedule-task-2",
		[]ChannelSmartScheduleExecutionDetailInput{{
			AdjustmentIndex: 0,
			Payload: channelSmartScheduleExecutionDetailFixture{
				ChannelId: 22,
				Reason:    "新快照",
			},
		}},
	))

	loaded, err := GetChannelSmartScheduleExecutionDetails([]string{"schedule-task-2"})
	require.NoError(t, err)
	require.Len(t, loaded["schedule-task-2"], 1)
	var replacement channelSmartScheduleExecutionDetailFixture
	require.NoError(t, common.UnmarshalJsonStr(loaded["schedule-task-2"][0].Payload, &replacement))
	assert.Equal(t, 22, replacement.ChannelId)
	assert.Equal(t, "新快照", replacement.Reason)

	var rows []ChannelSmartScheduleExecutionDetail
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
}
