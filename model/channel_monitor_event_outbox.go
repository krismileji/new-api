package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	ChannelMonitorEventOutboxEventIDMaxLength = 128
	channelMonitorEventOutboxOwnerMaxLength   = 128
	channelMonitorEventOutboxPayloadMaxBytes  = 128 * 1024
	channelMonitorEventOutboxErrorMaxLength   = 512
	channelMonitorEventOutboxMaxClaimSize     = 256
)

var ErrChannelMonitorEventOutboxEventIDCollision = errors.New("channel monitor event outbox event id collision")

// ChannelMonitorEventOutbox is the durable fallback for monitor events that
// cannot be handed to Redis immediately. EventId is the idempotency boundary;
// replaying a processed row is safe because the Redis consumer deduplicates by
// the same event id before applying projections.
type ChannelMonitorEventOutbox struct {
	Id            int64  `gorm:"primaryKey"`
	EventId       string `gorm:"size:128;not null;uniqueIndex"`
	Payload       string `gorm:"type:text;not null"`
	AttemptCount  int64  `gorm:"not null"`
	NextAttemptAt int64  `gorm:"not null;index:idx_channel_monitor_event_outbox_pending,priority:2"`
	LeaseOwner    string `gorm:"size:128;not null;index"`
	LeaseUntil    int64  `gorm:"not null;index:idx_channel_monitor_event_outbox_pending,priority:3"`
	ProcessedAt   int64  `gorm:"not null;index:idx_channel_monitor_event_outbox_pending,priority:1"`
	LastError     string `gorm:"size:512;not null"`
	CreatedAt     int64  `gorm:"not null"`
	UpdatedAt     int64  `gorm:"not null"`
}

type ChannelMonitorEventOutboxStats struct {
	PendingCount  int64
	OldestPending int64
	RetryCount    int64
}

func StoreChannelMonitorEventOutbox(
	ctx context.Context,
	eventID string,
	payload []byte,
) (bool, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || len(eventID) > ChannelMonitorEventOutboxEventIDMaxLength {
		return false, errors.New("channel monitor event outbox event id is invalid")
	}
	if len(payload) == 0 || len(payload) > channelMonitorEventOutboxPayloadMaxBytes {
		return false, errors.New("channel monitor event outbox payload is invalid")
	}
	if DB == nil {
		return false, errors.New("channel monitor event outbox database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().Unix()
	record := ChannelMonitorEventOutbox{
		EventId: eventID, Payload: string(payload), NextAttemptAt: now,
		CreatedAt: now, UpdatedAt: now,
	}
	var inserted bool
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		const savepoint = "channel_monitor_event_outbox_insert"
		if err := tx.SavePoint(savepoint).Error; err != nil {
			return err
		}
		createErr := tx.Create(&record).Error
		if createErr == nil {
			inserted = true
			return nil
		}
		if rollbackErr := tx.RollbackTo(savepoint).Error; rollbackErr != nil {
			return rollbackErr
		}
		var existing ChannelMonitorEventOutbox
		if err := tx.Where("event_id = ?", eventID).First(&existing).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Preserve the original insert failure. Returning a synthetic
				// not-found error here hides transient database failures and makes
				// callers believe the event was simply absent, preventing the
				// outbox retry path from reporting the real cause.
				return createErr
			}
			return err
		}
		if existing.Payload != record.Payload {
			return fmt.Errorf("%w: %s", ErrChannelMonitorEventOutboxEventIDCollision, eventID)
		}
		return nil
	})
	return inserted, err
}

func ClaimChannelMonitorEventOutbox(
	ctx context.Context,
	owner string,
	now int64,
	leaseDuration time.Duration,
	limit int,
) ([]ChannelMonitorEventOutbox, error) {
	if DB == nil {
		return nil, errors.New("channel monitor event outbox database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > channelMonitorEventOutboxOwnerMaxLength {
		return nil, errors.New("channel monitor event outbox owner is invalid")
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	if limit <= 0 || limit > channelMonitorEventOutboxMaxClaimSize {
		limit = channelMonitorEventOutboxMaxClaimSize
	}
	leaseSeconds := int64(leaseDuration / time.Second)
	if leaseSeconds <= 0 {
		leaseSeconds = 1
	}
	leaseUntil := now + leaseSeconds
	if leaseUntil < now {
		leaseUntil = math.MaxInt64
	}
	claimed := make([]ChannelMonitorEventOutbox, 0, limit)
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := lockForUpdate(tx.Model(&ChannelMonitorEventOutbox{})).
			Where("processed_at = ? AND next_attempt_at <= ? AND (lease_until <= ? OR lease_owner = ?)", 0, now, now, owner).
			Order("next_attempt_at ASC, id ASC").Limit(limit)
		var candidates []ChannelMonitorEventOutbox
		if err := query.Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		for index := range candidates {
			result := tx.Model(&ChannelMonitorEventOutbox{}).
				Where("id = ? AND processed_at = ? AND (lease_until <= ? OR lease_owner = ?)", candidates[index].Id, 0, now, owner).
				Updates(map[string]interface{}{"lease_owner": owner, "lease_until": leaseUntil, "updated_at": now})
			if result.Error != nil {
				return result.Error
			}
			// SQLite has no FOR UPDATE clause, so another worker may win the
			// lease between the candidate read and this conditional update. A
			// zero-row update means this candidate was not actually claimed.
			if result.RowsAffected != 1 {
				continue
			}
			candidates[index].LeaseOwner = owner
			candidates[index].LeaseUntil = leaseUntil
			candidates[index].UpdatedAt = now
			claimed = append(claimed, candidates[index])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func MarkChannelMonitorEventOutboxProcessed(
	ctx context.Context,
	owner string,
	ids []int64,
	processedAt int64,
) error {
	if DB == nil {
		return errors.New("channel monitor event outbox database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(ids) == 0 {
		return nil
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > channelMonitorEventOutboxOwnerMaxLength {
		return errors.New("channel monitor event outbox owner is invalid")
	}
	if processedAt <= 0 {
		processedAt = time.Now().Unix()
	}
	return DB.WithContext(ctx).Model(&ChannelMonitorEventOutbox{}).
		Where("id IN ? AND lease_owner = ? AND processed_at = ?", ids, owner, 0).
		Updates(map[string]interface{}{"processed_at": processedAt, "lease_owner": "", "lease_until": 0, "updated_at": processedAt}).Error
}

func FailChannelMonitorEventOutbox(
	ctx context.Context,
	owner string,
	ids []int64,
	nextAttemptAt int64,
	failure error,
) error {
	if DB == nil {
		return errors.New("channel monitor event outbox database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(ids) == 0 {
		return nil
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > channelMonitorEventOutboxOwnerMaxLength {
		return errors.New("channel monitor event outbox owner is invalid")
	}
	if nextAttemptAt <= 0 {
		nextAttemptAt = time.Now().Unix() + 1
	}
	errorText := ""
	if failure != nil {
		errorText = failure.Error()
		if len(errorText) > channelMonitorEventOutboxErrorMaxLength {
			errorText = errorText[:channelMonitorEventOutboxErrorMaxLength]
		}
	}
	now := time.Now().Unix()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []ChannelMonitorEventOutbox
		if err := lockForUpdate(tx.Model(&ChannelMonitorEventOutbox{})).
			Where("id IN ? AND lease_owner = ? AND processed_at = ?", ids, owner, 0).
			Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			attempts := row.AttemptCount
			if attempts < math.MaxInt64 {
				attempts++
			}
			if err := tx.Model(&ChannelMonitorEventOutbox{}).Where("id = ?", row.Id).
				Updates(map[string]interface{}{"attempt_count": attempts, "next_attempt_at": nextAttemptAt, "lease_owner": "", "lease_until": 0, "last_error": errorText, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func GetChannelMonitorEventOutboxStats(ctx context.Context) (ChannelMonitorEventOutboxStats, error) {
	if DB == nil {
		return ChannelMonitorEventOutboxStats{}, errors.New("channel monitor event outbox database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var stats ChannelMonitorEventOutboxStats
	if err := DB.WithContext(ctx).Model(&ChannelMonitorEventOutbox{}).
		Where("processed_at = ?", 0).Count(&stats.PendingCount).Error; err != nil {
		return stats, err
	}
	var oldest struct{ Value int64 }
	if err := DB.WithContext(ctx).Model(&ChannelMonitorEventOutbox{}).
		Select("MIN(created_at) AS value").Where("processed_at = ?", 0).Scan(&oldest).Error; err != nil {
		return stats, err
	}
	stats.OldestPending = oldest.Value
	if err := DB.WithContext(ctx).Model(&ChannelMonitorEventOutbox{}).
		Where("processed_at = ? AND attempt_count > ?", 0, 0).
		Select("COALESCE(SUM(attempt_count), 0)").Scan(&stats.RetryCount).Error; err != nil {
		return stats, err
	}
	return stats, nil
}
