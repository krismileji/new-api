package model

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelModelDetectionLogicalConfig stores the shared model-detection
// configuration independently from every member's physical fallback config.
type ChannelModelDetectionLogicalConfig struct {
	Id                int64  `json:"id" gorm:"primaryKey"`
	LogicalChannelId  int64  `json:"logical_channel_id" gorm:"not null;uniqueIndex;index:idx_channel_model_detection_logical_config_due,priority:1"`
	ScheduleEnabled   bool   `json:"schedule_enabled" gorm:"not null;index:idx_channel_model_detection_logical_config_due,priority:2"`
	ManualRequestId   string `json:"manual_request_id" gorm:"type:varchar(64);index:idx_channel_model_detection_logical_config_manual_due,priority:1"`
	ManualRequestedAt int64  `json:"manual_requested_at" gorm:"bigint;index:idx_channel_model_detection_logical_config_manual_due,priority:2,sort:desc"`
	Revision          int64  `json:"revision" gorm:"bigint;not null"`
	RunningRunId      string `json:"running_run_id" gorm:"type:varchar(64);index"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (config *ChannelModelDetectionLogicalConfig) BeforeCreate(_ *gorm.DB) error {
	if config.LogicalChannelId <= 0 {
		return errors.New("逻辑渠道 ID 必须为正数")
	}
	if config.Revision == 0 {
		config.Revision = 1
	}
	now := channelModelDetectionNow()
	if config.CreatedAt == 0 {
		config.CreatedAt = now
	}
	if config.UpdatedAt == 0 {
		config.UpdatedAt = config.CreatedAt
	}
	return nil
}

type ChannelModelDetectionLogicalTarget struct {
	Id               int64  `json:"id" gorm:"primaryKey"`
	ConfigId         int64  `json:"config_id" gorm:"not null;index"`
	LogicalChannelId int64  `json:"logical_channel_id" gorm:"not null;index;uniqueIndex:idx_channel_model_detection_logical_target_identity"`
	TargetKey        string `json:"target_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	RequestModel     string `json:"request_model" gorm:"type:varchar(255);not null;uniqueIndex:idx_channel_model_detection_logical_target_identity"`
	ClaimedModel     string `json:"claimed_model" gorm:"type:varchar(32);not null;uniqueIndex:idx_channel_model_detection_logical_target_identity"`
	Position         int    `json:"position" gorm:"not null"`
	Enabled          bool   `json:"enabled" gorm:"not null;index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (target *ChannelModelDetectionLogicalTarget) BeforeCreate(_ *gorm.DB) error {
	if target.TargetKey == "" {
		target.TargetKey = common.GetUUID()
	}
	now := channelModelDetectionNow()
	if target.CreatedAt == 0 {
		target.CreatedAt = now
	}
	if target.UpdatedAt == 0 {
		target.UpdatedAt = target.CreatedAt
	}
	return target.Validate()
}

func (target ChannelModelDetectionLogicalTarget) Validate() error {
	if target.LogicalChannelId <= 0 || target.ConfigId <= 0 || strings.TrimSpace(target.RequestModel) == "" {
		return errors.New("逻辑渠道模型检测目标配置无效")
	}
	if !IsChannelModelDetectionClaimedModel(target.ClaimedModel) {
		return ErrChannelModelDetectionInvalidClaimedModel
	}
	if target.Position < 0 {
		return errors.New("模型检测目标顺序无效")
	}
	return nil
}

// EnsureChannelModelDetectionLogicalConfigTx materializes a group's shared
// config once. Existing shared data is never overwritten, and source physical
// configs remain untouched for grouping fallback.
func EnsureChannelModelDetectionLogicalConfigTx(tx *gorm.DB, logicalChannelID int64, memberIDs []int) error {
	if tx == nil {
		return gorm.ErrInvalidDB
	}
	if logicalChannelID <= 0 {
		return ErrChannelLogicalGroupInvalidRevision
	}
	var existing int64
	if err := tx.Model(&ChannelModelDetectionLogicalConfig{}).Where("logical_channel_id = ?", logicalChannelID).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	ids := append([]int(nil), memberIDs...)
	sort.Ints(ids)
	for _, channelID := range ids {
		if channelID <= 0 {
			continue
		}
		var source ChannelModelDetectionConfig
		result := tx.Where("channel_id = ?", channelID).Limit(1).Find(&source)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		var sourceTargets []ChannelModelDetectionTarget
		if err := tx.Where("config_id = ? AND channel_id = ?", source.Id, channelID).Order("position ASC, id ASC").Find(&sourceTargets).Error; err != nil {
			return err
		}
		if len(sourceTargets) == 0 {
			continue
		}
		logicalConfig := ChannelModelDetectionLogicalConfig{
			LogicalChannelId: logicalChannelID, ScheduleEnabled: source.ScheduleEnabled,
			Revision: source.Revision, CreatedAt: source.CreatedAt, UpdatedAt: source.UpdatedAt,
		}
		if logicalConfig.Revision <= 0 {
			logicalConfig.Revision = 1
		}
		if err := tx.Create(&logicalConfig).Error; err != nil {
			return err
		}
		for _, sourceTarget := range sourceTargets {
			target := ChannelModelDetectionLogicalTarget{
				ConfigId: logicalConfig.Id, LogicalChannelId: logicalChannelID, TargetKey: common.GetUUID(),
				RequestModel: sourceTarget.RequestModel, ClaimedModel: sourceTarget.ClaimedModel,
				Position: sourceTarget.Position, Enabled: sourceTarget.Enabled,
				CreatedAt: sourceTarget.CreatedAt, UpdatedAt: sourceTarget.UpdatedAt,
			}
			if err := tx.Create(&target).Error; err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func claimChannelModelDetectionLogicalRun(tx *gorm.DB, run *ChannelModelDetectionRun) (bool, error) {
	if run.LogicalChannelID <= 0 || run.LogicalRevision <= 0 {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	claimed := tx.Model(&ChannelModelDetectionLogicalConfig{}).
		Where("logical_channel_id = ? AND running_run_id = ?", run.LogicalChannelID, "").
		Updates(map[string]any{"running_run_id": run.RunId, "updated_at": channelModelDetectionNow()})
	if claimed.Error != nil || claimed.RowsAffected != 1 {
		return false, claimed.Error
	}
	return true, nil
}

func releaseChannelModelDetectionLogicalRun(tx *gorm.DB, logicalChannelID int64, runID string, now int64) (bool, error) {
	if logicalChannelID <= 0 || strings.TrimSpace(runID) == "" {
		return false, ErrChannelModelDetectionInvalidSchedule
	}
	updated := tx.Model(&ChannelModelDetectionLogicalConfig{}).
		Where("logical_channel_id = ? AND running_run_id = ?", logicalChannelID, runID).
		Updates(map[string]any{"running_run_id": "", "updated_at": now})
	return updated.RowsAffected == 1, updated.Error
}
