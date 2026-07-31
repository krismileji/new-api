package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const ChannelSmartScheduleScoreDetailsVersion = 2

type ChannelSmartScheduleScoreInput struct {
	Value       *float64 `json:"value"`
	SampleCount int64    `json:"sample_count"`
}

type ChannelSmartScheduleScoreInputs struct {
	CostRatio    ChannelSmartScheduleScoreInput `json:"cost_ratio"`
	FirstTokenMs ChannelSmartScheduleScoreInput `json:"first_token_ms"`
	TPS          ChannelSmartScheduleScoreInput `json:"tps"`
	Stability    ChannelSmartScheduleScoreInput `json:"stability"`
}

type ChannelSmartScheduleScoreRange struct {
	Minimum        *float64 `json:"minimum"`
	Maximum        *float64 `json:"maximum"`
	AvailableCount int      `json:"available_count"`
}

type ChannelSmartScheduleScoreCohort struct {
	Priority     *int64                         `json:"priority,omitempty"`
	CostRatio    ChannelSmartScheduleScoreRange `json:"cost_ratio"`
	FirstTokenMs ChannelSmartScheduleScoreRange `json:"first_token_ms"`
	TPS          ChannelSmartScheduleScoreRange `json:"tps"`
}

type ChannelSmartScheduleScoreComponent struct {
	Available               bool     `json:"available"`
	RawValue                *float64 `json:"raw_value"`
	NormalizedScore         *float64 `json:"normalized_score"`
	ConfiguredWeightPercent float64  `json:"configured_weight_percent"`
	EffectiveWeightPercent  float64  `json:"effective_weight_percent"`
}

type ChannelSmartScheduleScoreComponents struct {
	CostRatio    ChannelSmartScheduleScoreComponent `json:"cost_ratio"`
	FirstTokenMs ChannelSmartScheduleScoreComponent `json:"first_token_ms"`
	TPS          ChannelSmartScheduleScoreComponent `json:"tps"`
}

type ChannelSmartScheduleStabilityScoreDetails struct {
	Enabled                 bool     `json:"enabled"`
	Available               bool     `json:"available"`
	Applied                 bool     `json:"applied"`
	RawScore                *float64 `json:"raw_score"`
	ConfiguredWeightPercent float64  `json:"configured_weight_percent"`
	EffectiveWeightPercent  float64  `json:"effective_weight_percent"`
	BusinessContribution    float64  `json:"business_contribution"`
	Contribution            float64  `json:"contribution"`
}

type ChannelSmartScheduleScoreDecision struct {
	ApplyMode                     string  `json:"apply_mode"`
	CurrentPrimaryChannelId       int     `json:"current_primary_channel_id"`
	RawWinnerChannelId            int     `json:"raw_winner_channel_id"`
	SelectedPrimaryChannelId      int     `json:"selected_primary_channel_id"`
	SelectedPrimary               bool    `json:"selected_primary"`
	ManualPrimaryChannelId        int     `json:"manual_primary_channel_id"`
	PrimarySwitchThresholdPercent float64 `json:"switch_threshold_percent"`
	PrimaryTrafficPercent         float64 `json:"primary_traffic_percent"`
	ForceReset                    bool    `json:"force_reset"`
	ManualPrimary                 bool    `json:"manual_primary"`
	SelectionReason               string  `json:"selection_reason"`
	AdjustmentReason              string  `json:"adjustment_reason"`
	Reason                        string  `json:"reason"`
}

type ChannelSmartScheduleScoreDetails struct {
	Version          int                                       `json:"version"`
	Strategy         string                                    `json:"strategy"`
	MinSamples       int                                       `json:"minimum_samples"`
	SampleScope      string                                    `json:"sample_scope"`
	SampleGroupCount int                                       `json:"sample_group_count"`
	Inputs           ChannelSmartScheduleScoreInputs           `json:"inputs"`
	Cohort           ChannelSmartScheduleScoreCohort           `json:"cohort"`
	Components       ChannelSmartScheduleScoreComponents       `json:"components"`
	BusinessScore    *float64                                  `json:"business_score"`
	Stability        ChannelSmartScheduleStabilityScoreDetails `json:"stability"`
	FinalScore       *float64                                  `json:"final_score"`
	Decision         ChannelSmartScheduleScoreDecision         `json:"decision"`
}

// ChannelSmartScheduleScoreDetailsJSON is persisted as portable TEXT while
// marshaling to API consumers as the original structured snapshot.
type ChannelSmartScheduleScoreDetailsJSON string

func EncodeChannelSmartScheduleScoreDetails(
	details *ChannelSmartScheduleScoreDetails,
) (ChannelSmartScheduleScoreDetailsJSON, error) {
	if details == nil {
		return "", nil
	}
	raw, err := common.Marshal(details)
	if err != nil {
		return "", err
	}
	return ChannelSmartScheduleScoreDetailsJSON(raw), nil
}

func (raw ChannelSmartScheduleScoreDetailsJSON) Decode() (*ChannelSmartScheduleScoreDetails, error) {
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	details := &ChannelSmartScheduleScoreDetails{}
	if err := common.UnmarshalJsonStr(string(raw), details); err != nil {
		return nil, err
	}
	return details, nil
}

func (raw ChannelSmartScheduleScoreDetailsJSON) MarshalJSON() ([]byte, error) {
	details, err := raw.Decode()
	if err != nil {
		return nil, err
	}
	if details == nil {
		return []byte("null"), nil
	}
	return common.Marshal(details)
}
