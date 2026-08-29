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
	ChannelDailyCostOutboxEventIDMaxLength = 64
	channelDailyCostOutboxOwnerMaxLength   = 128
	channelDailyCostOutboxErrorMaxLength   = 512
	channelDailyCostOutboxMaxClaimSize     = 256
	channelDailyCostOutboxMaxCleanupSize   = 1_000
)

var ErrChannelDailyCostOutboxEventIDCollision = errors.New("channel daily cost outbox event id collision")

// ChannelDailyCostOutbox is the durable idempotency boundary between the
// cost Stream and the daily ledger. Applying the ledger update and setting
// ProcessedAt happen in one database transaction.
type ChannelDailyCostOutbox struct {
	Id                    int64  `gorm:"primaryKey"`
	EventId               string `gorm:"size:64;not null;uniqueIndex"`
	ChannelId             int    `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:2"`
	OccurredAt            int64  `gorm:"not null"`
	CostNanoCNY           int64  `gorm:"not null"`
	ProbeCostNanoCNY      int64  `gorm:"not null"`
	GroupProbeCostNanoCNY int64  `gorm:"not null"`
	SettledDelta          int64  `gorm:"not null"`
	UnresolvedDelta       int64  `gorm:"not null"`
	APIKeyId              int    `gorm:"not null"`
	APIKeyName            string `gorm:"size:255;not null"`
	KeyFingerprint        string `gorm:"size:64;not null"`
	KeyDisplay            string `gorm:"size:64;not null"`
	AttemptCount          int64  `gorm:"not null"`
	NextAttemptAt         int64  `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:3"`
	LeaseOwner            string `gorm:"size:128;not null;index"`
	LeaseUntil            int64  `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:4"`
	ProcessedAt           int64  `gorm:"not null;index:idx_channel_daily_cost_outbox_pending,priority:1"`
	LastError             string `gorm:"size:512;not null"`
	CreatedAt             int64  `gorm:"not null"`
	UpdatedAt             int64  `gorm:"not null"`
}

type ChannelDailyCostOutboxStats struct {
	PendingCount  int64
	OldestPending int64
	RetryCount    int64
}

func StoreChannelDailyCostOutboxEvents(ctx context.Context, deltas []ChannelDailyCostDelta) error {
	_, err := StoreChannelDailyCostOutboxEventsWithResult(ctx, deltas)
	return err
}

// StoreChannelDailyCostOutboxEventsWithResult stores a batch idempotently and
// returns the number of rows newly accepted by this call.
func StoreChannelDailyCostOutboxEventsWithResult(ctx context.Context, deltas []ChannelDailyCostDelta) (int64, error) {
	if len(deltas) == 0 {
		return 0, nil
	}
	if DB == nil {
		return 0, errors.New("channel daily cost outbox database is unavailable")
	}
	normalized := make([]ChannelDailyCostDelta, len(deltas))
	copy(normalized, deltas)
	for index := range normalized {
		normalized[index].EventId = strings.TrimSpace(normalized[index].EventId)
		if normalized[index].EventId == "" || len(normalized[index].EventId) > ChannelDailyCostOutboxEventIDMaxLength {
			return 0, errors.New("channel daily cost outbox event id is invalid")
		}
		if err := normalizeChannelDailyCostDelta(&normalized[index]); err != nil {
			return 0, err
		}
	}
	now := time.Now().Unix()
	var inserted int64
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, delta := range normalized {
			record := channelDailyCostOutboxFromDelta(delta, now)
			const savepoint = "channel_daily_cost_outbox_insert"
			if err := tx.SavePoint(savepoint).Error; err != nil {
				return err
			}
			createErr := tx.Create(&record).Error
			if createErr == nil {
				inserted++
				continue
			}
			// MySQL implements GORM's OnConflict DoNothing as a no-op UPDATE.
			// With clientFoundRows enabled that reports one affected row, so it
			// cannot distinguish a new insert from an idempotent replay or a
			// payload collision. Recover the transaction after the constraint
			// error and compare the durable row explicitly on every dialect.
			if err := tx.RollbackTo(savepoint).Error; err != nil {
				return err
			}
			var existing ChannelDailyCostOutbox
			if err := tx.Where("event_id = ?", delta.EventId).First(&existing).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return createErr
				}
				return err
			}
			if !channelDailyCostOutboxMatchesDelta(existing, delta) {
				return fmt.Errorf("%w: %s", ErrChannelDailyCostOutboxEventIDCollision, delta.EventId)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}

func ClaimChannelDailyCostOutboxEvents(ctx context.Context, owner string, now int64, readyBefore int64, leaseDuration time.Duration, limit int) ([]ChannelDailyCostOutbox, error) {
	if DB == nil {
		return nil, errors.New("channel daily cost outbox database is unavailable")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > channelDailyCostOutboxOwnerMaxLength {
		return nil, errors.New("channel daily cost outbox lease owner is invalid")
	}
	if now <= 0 {
		return nil, errors.New("channel daily cost outbox claim timestamp must be positive")
	}
	if readyBefore <= 0 {
		return nil, errors.New("channel daily cost outbox ready cutoff must be positive")
	}
	if leaseDuration <= 0 {
		return nil, errors.New("channel daily cost outbox lease duration must be positive")
	}
	if limit <= 0 || limit > channelDailyCostOutboxMaxClaimSize {
		limit = channelDailyCostOutboxMaxClaimSize
	}
	leaseSeconds := max(int64(1), int64(leaseDuration/time.Second))
	if leaseSeconds > math.MaxInt64-now {
		return nil, errors.New("channel daily cost outbox lease timestamp exceeds int64")
	}
	leaseUntil := now + leaseSeconds
	claimed := make([]ChannelDailyCostOutbox, 0, limit)
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		if err := lockForUpdate(tx.Model(&ChannelDailyCostOutbox{})).
			Select("id").
			Where("processed_at = ? AND next_attempt_at <= ? AND lease_until <= ?", 0, readyBefore, now).
			Order("id ASC").
			Limit(limit).
			Find(&ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		updated := tx.Model(&ChannelDailyCostOutbox{}).
			Where("id IN ? AND processed_at = ? AND next_attempt_at <= ? AND lease_until <= ?", ids, 0, readyBefore, now).
			Where("attempt_count >= 0 AND attempt_count < ?", int64(math.MaxInt64)).
			Updates(map[string]interface{}{
				"lease_owner":   owner,
				"lease_until":   leaseUntil,
				"attempt_count": gorm.Expr("attempt_count + ?", 1),
				"updated_at":    now,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != int64(len(ids)) {
			return errors.New("channel daily cost outbox attempt count exhausted or lease changed")
		}
		return tx.Where("id IN ? AND lease_owner = ? AND lease_until = ? AND processed_at = ?", ids, owner, leaseUntil, 0).
			Order("id ASC").Find(&claimed).Error
	})
	return claimed, err
}

func ApplyClaimedChannelDailyCostOutboxEvents(ctx context.Context, owner string, ids []int64, processedAt int64) error {
	_, err := ApplyClaimedChannelDailyCostOutboxEventsWithResult(ctx, owner, ids, processedAt)
	return err
}

// ApplyClaimedChannelDailyCostOutboxEventsWithResult applies the rows still
// owned by owner and reports how many were finalized. A zero count is a valid
// result when another worker took over the lease between claim and finalize;
// callers must not count that as a second ledger application.
func ApplyClaimedChannelDailyCostOutboxEventsWithResult(ctx context.Context, owner string, ids []int64, processedAt int64) (int64, error) {
	if DB == nil {
		return 0, errors.New("channel daily cost outbox database is unavailable")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > channelDailyCostOutboxOwnerMaxLength || len(ids) == 0 {
		if owner == "" || len(owner) > channelDailyCostOutboxOwnerMaxLength {
			return 0, errors.New("channel daily cost outbox lease owner is invalid")
		}
		return 0, nil
	}
	if processedAt <= 0 {
		return 0, errors.New("channel daily cost outbox processed timestamp must be positive")
	}
	var applied int64
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []ChannelDailyCostOutbox
		if err := lockForUpdate(tx).
			Where("id IN ? AND lease_owner = ? AND processed_at = ?", ids, owner, 0).
			Order("id ASC").Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		deltas := make([]ChannelDailyCostDelta, 0, len(rows))
		rowIDs := make([]int64, 0, len(rows))
		for _, row := range rows {
			deltas = append(deltas, row.channelDailyCostDelta())
			rowIDs = append(rowIDs, row.Id)
		}
		if err := addChannelDailyCostBatch(tx, deltas); err != nil {
			return err
		}
		updated := tx.Model(&ChannelDailyCostOutbox{}).
			Where("id IN ? AND lease_owner = ? AND processed_at = ?", rowIDs, owner, 0).
			Updates(map[string]interface{}{
				"processed_at":    processedAt,
				"lease_owner":     "",
				"lease_until":     0,
				"next_attempt_at": 0,
				"last_error":      "",
				"updated_at":      processedAt,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != int64(len(rowIDs)) {
			return errors.New("channel daily cost outbox lease changed during apply")
		}
		applied = updated.RowsAffected
		return nil
	})
	return applied, err
}

func FailClaimedChannelDailyCostOutboxEvents(ctx context.Context, owner string, ids []int64, nextAttemptAt int64, failure error) error {
	_, err := FailClaimedChannelDailyCostOutboxEventsWithResult(ctx, owner, ids, nextAttemptAt, failure)
	return err
}

// FailClaimedChannelDailyCostOutboxEventsWithResult releases a lease and
// reports how many rows were released. A zero count means a different worker
// already finalized or took over the rows.
func FailClaimedChannelDailyCostOutboxEventsWithResult(ctx context.Context, owner string, ids []int64, nextAttemptAt int64, failure error) (int64, error) {
	if DB == nil {
		return 0, errors.New("channel daily cost outbox database is unavailable")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > channelDailyCostOutboxOwnerMaxLength || len(ids) == 0 {
		if owner == "" || len(owner) > channelDailyCostOutboxOwnerMaxLength {
			return 0, errors.New("channel daily cost outbox lease owner is invalid")
		}
		return 0, nil
	}
	if nextAttemptAt <= 0 {
		return 0, errors.New("channel daily cost outbox next attempt timestamp must be positive")
	}
	message := ""
	if failure != nil {
		message = failure.Error()
	}
	if len(message) > channelDailyCostOutboxErrorMaxLength {
		message = message[:channelDailyCostOutboxErrorMaxLength]
	}
	result := DB.WithContext(ctx).Model(&ChannelDailyCostOutbox{}).
		Where("id IN ? AND lease_owner = ? AND processed_at = ?", ids, owner, 0).
		Updates(map[string]interface{}{
			"lease_owner":     "",
			"lease_until":     0,
			"next_attempt_at": nextAttemptAt,
			"last_error":      message,
			"updated_at":      time.Now().Unix(),
		})
	return result.RowsAffected, result.Error
}

func GetChannelDailyCostOutboxStats(ctx context.Context) (ChannelDailyCostOutboxStats, error) {
	var stats ChannelDailyCostOutboxStats
	if DB == nil {
		return stats, errors.New("channel daily cost outbox database is unavailable")
	}
	rows, err := DB.WithContext(ctx).Model(&ChannelDailyCostOutbox{}).
		Select("created_at, attempt_count").Where("processed_at = ?", 0).Rows()
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var createdAt, attemptCount int64
		if err := rows.Scan(&createdAt, &attemptCount); err != nil {
			return stats, err
		}
		if stats.PendingCount < math.MaxInt64 {
			stats.PendingCount++
		}
		if createdAt > 0 && (stats.OldestPending == 0 || createdAt < stats.OldestPending) {
			stats.OldestPending = createdAt
		}
		if attemptCount > 0 && stats.RetryCount <= math.MaxInt64-attemptCount {
			stats.RetryCount += attemptCount
		} else if attemptCount > 0 {
			// Retry is an observability gauge; saturate rather than letting a
			// malformed or enormous pending set wrap it negative.
			stats.RetryCount = math.MaxInt64
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}
	return stats, nil
}

// DeleteProcessedChannelDailyCostOutboxEvents removes old idempotency records
// in bounded batches. Pending records are never eligible for cleanup.
func DeleteProcessedChannelDailyCostOutboxEvents(ctx context.Context, processedBefore int64, limit int) (int64, error) {
	if DB == nil {
		return 0, errors.New("channel daily cost outbox database is unavailable")
	}
	if processedBefore <= 0 {
		return 0, errors.New("channel daily cost outbox cleanup timestamp must be positive")
	}
	if limit <= 0 || limit > channelDailyCostOutboxMaxCleanupSize {
		limit = channelDailyCostOutboxMaxCleanupSize
	}
	var deleted int64
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []int64
		if err := tx.Model(&ChannelDailyCostOutbox{}).
			Select("id").
			Where("processed_at > ? AND processed_at < ?", 0, processedBefore).
			Order("id ASC").
			Limit(limit).
			Find(&ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Where("id IN ? AND processed_at > ? AND processed_at < ?", ids, 0, processedBefore).
			Delete(&ChannelDailyCostOutbox{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}

func channelDailyCostOutboxFromDelta(delta ChannelDailyCostDelta, now int64) ChannelDailyCostOutbox {
	return ChannelDailyCostOutbox{
		EventId: delta.EventId, ChannelId: delta.ChannelId, OccurredAt: delta.OccurredAt,
		CostNanoCNY: delta.CostNanoCNY, ProbeCostNanoCNY: delta.ProbeCostNanoCNY,
		GroupProbeCostNanoCNY: delta.GroupProbeCostNanoCNY, SettledDelta: delta.SettledDelta,
		UnresolvedDelta: delta.UnresolvedDelta, APIKeyId: delta.APIKeyId, APIKeyName: delta.APIKeyName,
		KeyFingerprint: delta.KeyFingerprint, KeyDisplay: delta.KeyDisplay, CreatedAt: now, UpdatedAt: now,
	}
}

func channelDailyCostOutboxMatchesDelta(row ChannelDailyCostOutbox, delta ChannelDailyCostDelta) bool {
	return row.EventId == delta.EventId && row.ChannelId == delta.ChannelId && row.OccurredAt == delta.OccurredAt &&
		row.CostNanoCNY == delta.CostNanoCNY && row.ProbeCostNanoCNY == delta.ProbeCostNanoCNY &&
		row.GroupProbeCostNanoCNY == delta.GroupProbeCostNanoCNY && row.SettledDelta == delta.SettledDelta &&
		row.UnresolvedDelta == delta.UnresolvedDelta && row.APIKeyId == delta.APIKeyId &&
		row.APIKeyName == delta.APIKeyName && row.KeyFingerprint == delta.KeyFingerprint && row.KeyDisplay == delta.KeyDisplay
}

func (row ChannelDailyCostOutbox) channelDailyCostDelta() ChannelDailyCostDelta {
	return ChannelDailyCostDelta{
		EventId: row.EventId, ChannelId: row.ChannelId, OccurredAt: row.OccurredAt,
		CostNanoCNY: row.CostNanoCNY, ProbeCostNanoCNY: row.ProbeCostNanoCNY,
		GroupProbeCostNanoCNY: row.GroupProbeCostNanoCNY, SettledDelta: row.SettledDelta,
		UnresolvedDelta: row.UnresolvedDelta, APIKeyId: row.APIKeyId, APIKeyName: row.APIKeyName,
		KeyFingerprint: row.KeyFingerprint, KeyDisplay: row.KeyDisplay,
	}
}
