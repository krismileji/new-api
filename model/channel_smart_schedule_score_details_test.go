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
	assert.Contains(t, serialized, `"last_schedule_score_details":{"version":9`)
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

func TestChannelSmartScheduleScoreDetailsJSONReadsStoredFormats(t *testing.T) {
	snapshot := `{"version":9,"event_watermark":9007199254740993,"final_score":0,"decision":{"selected_primary":false},"future_field":{"value":1}}`
	legacy, err := common.Marshal(snapshot)
	require.NoError(t, err)
	for _, tt := range []struct {
		name     string
		payload  string
		expected string
	}{
		{name: "structured snapshot", payload: snapshot, expected: snapshot},
		{name: "legacy JSON string", payload: string(legacy), expected: snapshot},
		{name: "null clears previous snapshot", payload: "null"},
		{name: "empty legacy string", payload: `""`},
		{name: "older version stays unchanged", payload: `{"version":5,"strategy":"smart"}`, expected: `{"version":5,"strategy":"smart"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := ChannelSmartScheduleScoreDetailsJSON(`{"version":9,"strategy":"previous"}`)
			require.NoError(t, common.UnmarshalJsonStr(tt.payload, &raw))
			assert.Equal(t, tt.expected, string(raw))
		})
	}
}

func TestChannelSmartScheduleScoreDetailsJSONRejectsInvalidSnapshotsWithoutOverwriting(t *testing.T) {
	for _, payload := range []string{`[]`, `true`, `42`, `"invalid"`, `{"version":"invalid"}`, `{"version":9`} {
		t.Run(payload, func(t *testing.T) {
			previous := ChannelSmartScheduleScoreDetailsJSON(`{"version":9,"strategy":"previous"}`)
			raw := previous
			require.Error(t, common.UnmarshalJsonStr(payload, &raw))
			assert.Equal(t, previous, raw)
		})
	}
}
