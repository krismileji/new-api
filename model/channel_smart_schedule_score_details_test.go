package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyChannelSmartScheduleRouteResultPersistsStructuredScoreSnapshot(t *testing.T) {
	db := setupChannelSmartScheduleRouteTestDB(t)
	priority := int64(80)
	weight := uint(50)
	ratio := 1.25
	firstToken := 420.0
	tps := 36.0
	stability := 0.96
	businessScore := 0.72
	finalScore := 0.84
	require.NoError(t, db.Create(&Channel{
		Id: 3201, Name: "score snapshot", Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "model-a", Priority: &priority, Weight: &weight,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		ChannelId: 3201, Group: "vip", Model: "model-a", Enabled: true,
		Priority: &priority, Weight: weight,
	}).Error)
	require.NoError(t, db.Create(&ChannelSmartScheduleRouteState{
		ChannelId: 3201, GroupName: "vip", ModelName: "model-a",
		ParticipationSet: true, Revision: 1,
	}).Error)

	details := &ChannelSmartScheduleScoreDetails{
		Version:               ChannelSmartScheduleScoreDetailsVersion,
		Strategy:              "smart",
		MinSamples:            20,
		MinComparableChannels: 2,
		ComparisonState:       ChannelSmartScheduleComparisonComparable,
		SampleScope:           ChannelSmartScheduleSampleScopeChannelModel,
		SampleGroupCount:      2,
		Inputs: ChannelSmartScheduleScoreInputs{
			CostRatio:    ChannelSmartScheduleScoreInput{Value: &ratio, SampleCount: 1},
			FirstTokenMs: ChannelSmartScheduleScoreInput{Value: &firstToken, SampleCount: 24},
			TPS:          ChannelSmartScheduleScoreInput{Value: &tps, SampleCount: 23},
			Stability:    ChannelSmartScheduleScoreInput{Value: &stability, SampleCount: 25},
		},
		BusinessScore: &businessScore,
		FinalScore:    &finalScore,
		Health: ChannelSmartScheduleHealthDetails{
			ErrorRequestPercent:             6,
			RiskRequestPercent:              10,
			FirstTokenWarningRequestPercent: 4,
			HealthyRequestPercent:           90,
		},
		Decision: ChannelSmartScheduleScoreDecision{
			SelectedPrimaryChannelId: 3201,
			SelectedPrimary:          true,
			SelectionReason:          "选择本轮评分最高的渠道",
			AdjustmentReason:         "权重调整为 900",
		},
	}
	outcomes, err := ApplyChannelSmartScheduleRouteResults([]ChannelSmartScheduleRouteResultUpdate{{
		ChannelId: 3201, Group: "vip", Model: "model-a",
		Status: ChannelSmartScheduleStatusSucceeded, Score: &finalScore, ScoreDetails: details,
		Priority: priority, Weight: 900, ExpectedPriority: priority, ExpectedWeight: weight,
		ApplyPriorityWeight: true,
	}})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Applied)

	// The persisted JSON must remain the execution-time snapshot even if the
	// caller later mutates its in-memory metrics.
	ratio = 9.99
	firstToken = 9999
	details.Decision.AdjustmentReason = "调用方后续修改"
	var stored ChannelSmartScheduleRouteState
	require.NoError(t, db.Where(
		"channel_id = ? AND group_name = ? AND model_name = ?", 3201, "vip", "model-a",
	).First(&stored).Error)
	decoded, err := stored.LastScheduleScoreDetails.Decode()
	require.NoError(t, err)
	require.NotNil(t, decoded)
	require.NotNil(t, decoded.Inputs.CostRatio.Value)
	assert.InDelta(t, 1.25, *decoded.Inputs.CostRatio.Value, 1e-9)
	require.NotNil(t, decoded.Inputs.FirstTokenMs.Value)
	assert.InDelta(t, 420, *decoded.Inputs.FirstTokenMs.Value, 1e-9)
	assert.Equal(t, "权重调整为 900", decoded.Decision.AdjustmentReason)

	routes, err := GetChannelSmartScheduleRoutes()
	require.NoError(t, err)
	require.Len(t, routes, 1)
	raw, err := common.Marshal(routes[0])
	require.NoError(t, err)
	serialized := string(raw)
	assert.Contains(t, serialized, `"last_schedule_score_details":{"version":8`)
	assert.Contains(t, serialized, `"error_request_percent":6`)
	assert.Contains(t, serialized, `"first_token_warning_request_percent":4`)
	assert.Contains(t, serialized, `"minimum_comparable_channels":2`)
	assert.Contains(t, serialized, `"comparison_state":"comparable"`)
	assert.Contains(t, serialized, `"sample_scope":"channel_model"`)
	assert.False(t, strings.Contains(serialized, `"last_schedule_score_details":"{`))
}

func TestChannelSmartScheduleScoreDetailsDoesNotBackfillOlderSnapshots(t *testing.T) {
	raw := ChannelSmartScheduleScoreDetailsJSON(`{"version":5,"strategy":"smart"}`)

	decoded, err := raw.Decode()
	require.NoError(t, err)
	assert.Nil(t, decoded)

	serialized, err := raw.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, "null", string(serialized))
}
