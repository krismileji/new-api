package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelMonitorDailyCheckpoint is one replaceable checkpoint per day, never
// one row per minute. EventWatermark is accepted only after the Redis consumer
// has no unapplied holes through that position.
type ChannelMonitorDailyCheckpoint struct {
	Id              int64 `gorm:"primaryKey"`
	DayStart        int64 `gorm:"not null;uniqueIndex"`
	Revision        int64 `gorm:"not null"`
	EventWatermark  int64 `gorm:"not null"`
	DataCutoffAt    int64 `gorm:"not null"`
	ProcessedAt     int64 `gorm:"not null"`
	UpdatedAt       int64 `gorm:"not null"`
	CoveragePartial bool
}

// ChannelMonitorDailyMetricIdentity freezes the same dimensions used by the
// existing daily ledger. Display names are kept outside the identity.
type ChannelMonitorDailyMetricIdentity struct {
	ChannelID int
	UserID    int
	APIKeyID  int
	APIKeyKey string
	Model     string
	Group     string
}

func ChannelMonitorDailyMetricIdentityFromEvent(event ChannelMonitorEvent) ChannelMonitorDailyMetricIdentity {
	return ChannelMonitorDailyMetricIdentity{
		ChannelID: event.ChannelId, UserID: event.UserId, APIKeyID: event.APIKeyId,
		APIKeyKey: channelMonitorMinuteDimensionKey(channelMonitorMinuteAPIKeyIdentity(event.APIKeyId, event.APIKeyName)),
		Model:     strings.TrimSpace(event.ModelName), Group: strings.TrimSpace(event.GroupName),
	}
}

func ChannelMonitorDailyMetricIdentityFromRow(row ChannelMonitorDailySuccessLedger) ChannelMonitorDailyMetricIdentity {
	return ChannelMonitorDailyMetricIdentity{
		ChannelID: row.ChannelId, UserID: row.UserId, APIKeyID: row.APIKeyId,
		APIKeyKey: row.APIKeyKey, Model: row.ModelName, Group: row.GroupName,
	}
}

func (identity ChannelMonitorDailyMetricIdentity) LedgerRow(day int64) ChannelMonitorDailySuccessLedger {
	attribution := string(ChannelMonitorEventUserAttributionUnknown)
	if identity.UserID > 0 {
		attribution = string(ChannelMonitorEventUserAttributionRequest)
	}
	return ChannelMonitorDailySuccessLedger{
		DayStart: day, ChannelId: identity.ChannelID, UserId: identity.UserID,
		UserAttribution: attribution, APIKeyId: identity.APIKeyID, APIKeyKey: identity.APIKeyKey,
		ModelKey: channelMonitorMinuteDimensionKey(identity.Model), ModelName: identity.Model,
		GroupKey: channelMonitorMinuteDimensionKey(identity.Group), GroupName: identity.Group,
	}
}

// PersistChannelMonitorDailySnapshot replaces cumulative values under the day
// checkpoint lock. It preserves row identity and atomically commits the data
// and replay position. Retrying or finishing an older worker is a no-op.
func PersistChannelMonitorDailySnapshot(ctx context.Context, checkpoint ChannelMonitorDailyCheckpoint, rows []ChannelMonitorDailySuccessLedger) error {
	if DB == nil || checkpoint.DayStart <= 0 || checkpoint.Revision <= 0 || checkpoint.EventWatermark < 0 {
		return errors.New("渠道监控日汇总检查点无效")
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seed := ChannelMonitorDailyCheckpoint{DayStart: checkpoint.DayStart}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
			return err
		}
		var existing ChannelMonitorDailyCheckpoint
		if err := lockForUpdate(tx).Where("day_start = ?", checkpoint.DayStart).First(&existing).Error; err != nil {
			return err
		}
		if existing.Revision >= checkpoint.Revision {
			return nil
		}
		if checkpoint.EventWatermark < existing.EventWatermark {
			return errors.New("渠道监控日汇总处理位置不能回退")
		}
		now := time.Now().Unix()
		for index := range rows {
			row := &rows[index]
			if row.DayStart != checkpoint.DayStart || row.ChannelId <= 0 {
				return errors.New("渠道监控日汇总维度无效")
			}
			if _, err := (channelMonitorDailySuccessValues{}).add(row.values(), 1); err != nil {
				return err
			}
			row.Id = 0
			row.ProjectionRevision = checkpoint.Revision
			row.CreatedAt, row.UpdatedAt = now, now
		}
		if len(rows) > 0 {
			err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "day_start"}, {Name: "channel_id"}, {Name: "user_id"}, {Name: "api_key_id"}, {Name: "api_key_key"}, {Name: "model_key"}, {Name: "group_key"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"user_attribution", "api_key_name", "model_name", "group_name",
					"actual_success_count", "actual_failure_count", "final_success_count", "final_failure_count",
					"cache_hit_count", "cache_sample_count", "cache_read_tokens", "input_tokens", "cache_write_count",
					"aggregate_json", "projection_revision", "updated_at",
				}),
			}).CreateInBatches(&rows, 100).Error
			if err != nil {
				return err
			}
		}
		// Daily facts only accumulate. Recovery must never erase a previously
		// persisted dimension just because an incomplete Redis snapshot lacks it.
		return tx.Model(&ChannelMonitorDailyCheckpoint{}).Where("id = ?", existing.Id).Updates(map[string]any{
			"revision": checkpoint.Revision, "event_watermark": checkpoint.EventWatermark,
			"data_cutoff_at": checkpoint.DataCutoffAt, "processed_at": checkpoint.ProcessedAt, "updated_at": now,
			"coverage_partial": checkpoint.CoveragePartial || existing.CoveragePartial,
		}).Error
	})
}
