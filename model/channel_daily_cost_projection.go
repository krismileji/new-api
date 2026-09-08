package model

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// appendChannelDailyCostProjectionTx records delivery of an already committed
// ledger change in the existing outbox, in that ledger's transaction. It must
// never be called by the outbox ledger applier itself.
func appendChannelDailyCostProjectionTx(tx *gorm.DB, delta ChannelDailyCostDelta, detectionCost int64, identity string) (int64, error) {
	if !tx.Migrator().HasTable(&ChannelDailyCostOutbox{}) {
		return 0, nil // Older schema fixtures and pre-migration installations.
	}
	now := time.Now().Unix()
	delta.EventId = uuid.NewString()
	row := channelDailyCostOutboxFromDelta(delta, now)
	row.ProcessedAt = now
	row.ProjectionEventId = identity
	row.ModelDetectionCostNanoCNY = detectionCost
	if err := tx.Create(&row).Error; err != nil {
		return 0, err
	}
	return row.Id, nil
}

// PendingChannelDailyCostProjections returns a bounded, ordered delivery batch.
// A row may be projected before its minute ledger transaction has run.
func PendingChannelDailyCostProjections(ctx context.Context, limit int) ([]ChannelDailyCostOutbox, error) {
	if DB == nil {
		return nil, errors.New("渠道成本投影数据库不可用")
	}
	if limit <= 0 || limit > channelDailyCostOutboxMaxClaimSize {
		limit = channelDailyCostOutboxMaxClaimSize
	}
	var rows []ChannelDailyCostOutbox
	// Historical queries read the daily ledger. Do not let an upgrade's old
	// processed outbox backlog delay today's live projection or recreate
	// expired hashes for every historical day.
	retainedFrom := ChannelDailyCostDayStart(time.Now().Unix()) - 24*60*60
	err := DB.WithContext(ctx).Where("redis_projected_at = ? AND occurred_at >= ?", 0, retainedFrom).
		Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func MarkChannelDailyCostProjectionsApplied(ctx context.Context, ids []int64, appliedAt int64) error {
	if len(ids) == 0 {
		return nil
	}
	if DB == nil || appliedAt <= 0 {
		return errors.New("渠道成本投影确认参数无效")
	}
	return DB.WithContext(ctx).Model(&ChannelDailyCostOutbox{}).
		Where("id IN ? AND redis_projected_at = ?", ids, 0).
		Update("redis_projected_at", appliedAt).Error
}

func channelTaskCostProjectionDelta(event ChannelTaskCostEvent, cost int64) ChannelDailyCostDelta {
	return ChannelDailyCostDelta{
		ChannelId: event.ChannelId, OccurredAt: event.OccurredAt,
		CostNanoCNY: cost, SettledDelta: 1, APIKeyId: event.APIKeyId,
		APIKeyName: event.APIKeyName, KeyFingerprint: event.KeyFingerprint,
		KeyDisplay: event.KeyDisplay, UserId: event.UserId,
		UserAttribution: event.UserAttribution, ModelName: event.ModelName, SourceKind: "business",
	}
}
