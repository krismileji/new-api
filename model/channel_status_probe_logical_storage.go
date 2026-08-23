package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ChannelStatusProbeLogicalConfig stores the effective shared schedule without
// mutating any member's physical configuration. Disabling logical grouping
// therefore restores every physical channel's original schedule and models.
type ChannelStatusProbeLogicalConfig struct {
	Id                int64  `json:"id"`
	LogicalChannelId  int64  `json:"logical_channel_id" gorm:"bigint;not null;uniqueIndex"`
	LogicalRevision   int64  `json:"logical_revision" gorm:"bigint"`
	OwnerChannelId    int    `json:"owner_channel_id" gorm:"not null"`
	Enabled           bool   `json:"enabled"`
	ModelsJSON        string `json:"-" gorm:"type:text;not null"`
	IntervalSeconds   int    `json:"interval_seconds"`
	DisplayValue      int    `json:"display_value"`
	DisplayUnit       string `json:"display_unit" gorm:"type:varchar(16)"`
	RecordSample      bool   `json:"record_sample"`
	NextRunAt         int64  `json:"next_run_at" gorm:"bigint;index:idx_channel_status_probe_logical_due,priority:2"`
	ManualRequestId   string `json:"manual_request_id" gorm:"type:varchar(64);index:idx_channel_status_probe_logical_manual_due,priority:1"`
	ManualRequestedAt int64  `json:"manual_requested_at" gorm:"bigint;index:idx_channel_status_probe_logical_manual_due,priority:2,sort:desc"`
	Revision          int64  `json:"revision" gorm:"bigint"`
	LeaseToken        string `json:"-" gorm:"type:varchar(64)"`
	LeaseUntil        int64  `json:"lease_until" gorm:"bigint;index:idx_channel_status_probe_logical_due,priority:3;index:idx_channel_status_probe_logical_manual_due,priority:3"`
	RunningTrigger    string `json:"running_trigger" gorm:"type:varchar(16)"`
	RunningRunId      string `json:"running_run_id" gorm:"type:varchar(64)"`
	RunningStartedAt  int64  `json:"running_started_at" gorm:"bigint"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint"`
}

func (config ChannelStatusProbeLogicalConfig) Models() ([]string, error) {
	models := []string{}
	if strings.TrimSpace(config.ModelsJSON) == "" {
		return models, nil
	}
	if err := common.UnmarshalJsonStr(config.ModelsJSON, &models); err != nil {
		return nil, fmt.Errorf("解析逻辑渠道状态探测模型失败: %w", err)
	}
	return models, nil
}

func (config ChannelStatusProbeLogicalConfig) Project(channelID int) ChannelStatusProbeConfig {
	return ChannelStatusProbeConfig{
		Id: config.Id, ChannelId: channelID, LogicalChannelId: config.LogicalChannelId, LogicalRevision: config.LogicalRevision,
		Enabled: config.Enabled, ModelsJSON: config.ModelsJSON, IntervalSeconds: config.IntervalSeconds,
		DisplayValue: config.DisplayValue, DisplayUnit: config.DisplayUnit, RecordSample: config.RecordSample,
		NextRunAt: config.NextRunAt, ManualRequestId: config.ManualRequestId, ManualRequestedAt: config.ManualRequestedAt,
		Revision: config.Revision, LeaseToken: config.LeaseToken, LeaseUntil: config.LeaseUntil,
		RunningTrigger: config.RunningTrigger, RunningRunId: config.RunningRunId, RunningStartedAt: config.RunningStartedAt,
		CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
	}
}

// ChannelStatusProbeLogicalState keeps the shared aggregate separate from the
// physical state rows. StateJSON uses the stable public state representation
// and avoids a second large, drift-prone set of aggregate columns.
type ChannelStatusProbeLogicalState struct {
	Id               int64  `json:"id"`
	LogicalChannelId int64  `json:"logical_channel_id" gorm:"bigint;not null;uniqueIndex:idx_channel_status_probe_logical_state_model"`
	LogicalRevision  int64  `json:"logical_revision" gorm:"bigint"`
	ModelName        string `json:"model_name" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_status_probe_logical_state_model"`
	ExecutionId      int64  `json:"execution_id" gorm:"bigint;index"`
	StateJSON        string `json:"-" gorm:"type:text;not null"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint"`
}

type channelStatusProbeLogicalStatePayload struct {
	ChannelStatusProbeState
	MinuteBucketsJSON string `json:"minute_buckets_json"`
	HourBucketsJSON   string `json:"hour_buckets_json"`
	DayBucketsJSON    string `json:"day_buckets_json"`
}

func (row ChannelStatusProbeLogicalState) State(ownerChannelID int) (ChannelStatusProbeState, error) {
	payload := channelStatusProbeLogicalStatePayload{}
	if strings.TrimSpace(row.StateJSON) != "" {
		if err := common.UnmarshalJsonStr(row.StateJSON, &payload); err != nil {
			return ChannelStatusProbeState{}, fmt.Errorf("解析逻辑渠道状态探测状态失败: %w", err)
		}
	}
	state := payload.ChannelStatusProbeState
	state.MinuteBucketsJSON = payload.MinuteBucketsJSON
	state.HourBucketsJSON = payload.HourBucketsJSON
	state.DayBucketsJSON = payload.DayBucketsJSON
	state.ChannelId = ownerChannelID
	state.LogicalChannelId = row.LogicalChannelId
	state.LogicalRevision = row.LogicalRevision
	state.ModelName = row.ModelName
	return state, nil
}

func newChannelStatusProbeLogicalStateRow(state ChannelStatusProbeState) (ChannelStatusProbeLogicalState, error) {
	payload := channelStatusProbeLogicalStatePayload{
		ChannelStatusProbeState: state,
		MinuteBucketsJSON:       state.MinuteBucketsJSON,
		HourBucketsJSON:         state.HourBucketsJSON,
		DayBucketsJSON:          state.DayBucketsJSON,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return ChannelStatusProbeLogicalState{}, err
	}
	return ChannelStatusProbeLogicalState{
		LogicalChannelId: state.LogicalChannelId, LogicalRevision: state.LogicalRevision, ModelName: state.ModelName,
		ExecutionId: state.ExecutionId, StateJSON: string(encoded), CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt,
	}, nil
}
