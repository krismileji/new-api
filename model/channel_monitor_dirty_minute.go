package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	channelMonitorDirtyMinuteTable        = "channel_monitor_dirty_minutes"
	channelMonitorDirtyMinutePendingTable = "channel_monitor_dirty_minute_pending"
	channelMonitorDirtyReasonMax          = 64
	channelMonitorDirtyClaimMax           = 100
	channelMonitorDirtyPendingMaxAttempts = 12
)

const (
	ChannelMonitorDirtyReasonLateLog          = "late_log"
	ChannelMonitorDirtyReasonCrossMinuteRetry = "cross_minute_retry"
)

type channelMonitorDirtyMinuteDatabaseState struct {
	mu                  sync.Mutex
	tableChecked        bool
	tableExists         bool
	pendingTableChecked bool
	pendingTableExists  bool
}

var channelMonitorDirtyMinuteDatabaseStates sync.Map

func channelMonitorDirtyMinuteDatabaseStateFor(db *gorm.DB) *channelMonitorDirtyMinuteDatabaseState {
	if state, exists := channelMonitorDirtyMinuteDatabaseStates.Load(db); exists {
		return state.(*channelMonitorDirtyMinuteDatabaseState)
	}
	state := &channelMonitorDirtyMinuteDatabaseState{}
	actual, _ := channelMonitorDirtyMinuteDatabaseStates.LoadOrStore(db, state)
	return actual.(*channelMonitorDirtyMinuteDatabaseState)
}

func resetChannelMonitorDirtyMinuteDatabaseState(db *gorm.DB) {
	if db != nil {
		channelMonitorDirtyMinuteDatabaseStates.Delete(db)
	}
}

func channelMonitorDirtyMinuteTableExists(db *gorm.DB) bool {
	state := channelMonitorDirtyMinuteDatabaseStateFor(db)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.tableChecked {
		return state.tableExists
	}
	state.tableExists = db.Migrator().HasTable(&ChannelMonitorDirtyMinute{})
	state.tableChecked = true
	return state.tableExists
}

func channelMonitorDirtyMinutePendingTableExists(db *gorm.DB) bool {
	state := channelMonitorDirtyMinuteDatabaseStateFor(db)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.pendingTableChecked {
		return state.pendingTableExists
	}
	state.pendingTableExists = db.Migrator().HasTable(&ChannelMonitorDirtyMinutePending{})
	state.pendingTableChecked = true
	return state.pendingTableExists
}

// ChannelMonitorDirtyMinute records a minute whose persisted aggregate may no
// longer match the source logs. One row covers the whole minute because the
// repair worker rebuilds all route dimensions for its claimed minute.
type ChannelMonitorDirtyMinute struct {
	Id            int64  `gorm:"primaryKey"`
	MinuteStart   int64  `gorm:"not null;uniqueIndex:idx_channel_monitor_dirty_minute_start"`
	DirtyReason   string `gorm:"size:64;not null"`
	FirstMarkedAt int64  `gorm:"not null;index:idx_channel_monitor_dirty_minute_marked"`
	LastMarkedAt  int64  `gorm:"not null;index:idx_channel_monitor_dirty_minute_marked"`
	MarkCount     int64  `gorm:"not null;default:1"`
	ClaimedBy     string `gorm:"size:128;not null;default:''"`
	ClaimedAt     int64  `gorm:"not null;default:0"`
	ClaimedUntil  int64  `gorm:"not null;default:0;index:idx_channel_monitor_dirty_minute_claim"`
}

func (ChannelMonitorDirtyMinute) TableName() string {
	return channelMonitorDirtyMinuteTable
}

// ChannelMonitorDirtyMinutePending is a durable compensation record for a
// source log whose dirty marker could not be written. The table is lazily
// migrated so older installations can recover marker failures without a
// restart or a hand-written migration.
type ChannelMonitorDirtyMinutePending struct {
	Id           int64  `gorm:"primaryKey"`
	MinuteStart  int64  `gorm:"not null;uniqueIndex:idx_channel_monitor_dirty_pending_minute"`
	DirtyReason  string `gorm:"size:64;not null"`
	Attempts     int64  `gorm:"not null;default:0"`
	NextRetryAt  int64  `gorm:"not null;default:0;index:idx_channel_monitor_dirty_pending_retry"`
	LastError    string `gorm:"size:512;not null;default:''"`
	DeadLetterAt int64  `gorm:"not null;default:0;index:idx_channel_monitor_dirty_pending_dead"`
	CreatedAt    int64  `gorm:"not null"`
	UpdatedAt    int64  `gorm:"not null"`
}

func (ChannelMonitorDirtyMinutePending) TableName() string {
	return channelMonitorDirtyMinutePendingTable
}

var (
	// ErrChannelMonitorDirtyMinuteLeaseLost is returned by lease renewal when a
	// different worker already reclaimed a row. Complete intentionally treats
	// the same condition as a no-op so stale workers cannot delete a marker.
	ErrChannelMonitorDirtyMinuteLeaseLost = errors.New("channel monitor dirty minute lease lost")
)

func isChannelMonitorDirtyMinuteSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked")
}

func waitChannelMonitorDirtyMinuteRetry(ctx context.Context, attempt int) error {
	delay := 10 * time.Millisecond * time.Duration(1<<attempt)
	if delay > 250*time.Millisecond {
		delay = 250 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withChannelMonitorDirtyMinuteRetry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		err = fn()
		if err == nil || !isChannelMonitorDirtyMinuteSQLiteBusy(err) {
			return err
		}
		if attempt+1 < 6 {
			if waitErr := waitChannelMonitorDirtyMinuteRetry(ctx, attempt); waitErr != nil {
				return waitErr
			}
		}
	}
	return err
}

func ensureChannelMonitorDirtyMinutePendingTable(db *gorm.DB) error {
	if db == nil {
		return errors.New("channel monitor database is not initialized")
	}
	if channelMonitorDirtyMinutePendingTableExists(db) {
		return nil
	}
	if err := withChannelMonitorDirtyMinuteRetry(context.Background(), func() error {
		return db.AutoMigrate(&ChannelMonitorDirtyMinutePending{})
	}); err != nil {
		return err
	}
	state := channelMonitorDirtyMinuteDatabaseStateFor(db)
	state.mu.Lock()
	state.pendingTableChecked = true
	state.pendingTableExists = true
	state.mu.Unlock()
	return nil
}

func normalizeChannelMonitorDirtyReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = ChannelMonitorDirtyReasonLateLog
	}
	if len(reason) > channelMonitorDirtyReasonMax {
		reason = reason[:channelMonitorDirtyReasonMax]
	}
	return reason
}

func normalizeChannelMonitorDirtyMinute(minuteStart int64) int64 {
	if minuteStart <= 0 {
		return 0
	}
	return channelMonitorMinuteStart(minuteStart)
}

func MarkChannelMonitorDirtyMinute(ctx context.Context, minuteStart int64, reason string) error {
	return MarkChannelMonitorDirtyMinutes(ctx, []int64{minuteStart}, reason)
}

// MarkChannelMonitorDirtyMinutes is idempotent on minute_start. Re-marking a
// claimed row updates its timestamp and count without deleting or replacing
// the row, so a worker that already claimed it cannot lose a later signal.
func MarkChannelMonitorDirtyMinutes(ctx context.Context, minuteStarts []int64, reason string) error {
	return markChannelMonitorDirtyMinutes(ctx, minuteStarts, reason, true)
}

func markChannelMonitorDirtyMinutes(
	ctx context.Context,
	minuteStarts []int64,
	reason string,
	enqueueOnFailure bool,
) error {
	if DB == nil {
		return errors.New("channel monitor database is not initialized")
	}
	starts := make([]int64, 0, len(minuteStarts))
	seen := make(map[int64]struct{}, len(minuteStarts))
	for _, minuteStart := range minuteStarts {
		minuteStart = normalizeChannelMonitorDirtyMinute(minuteStart)
		if minuteStart <= 0 {
			continue
		}
		if _, exists := seen[minuteStart]; exists {
			continue
		}
		seen[minuteStart] = struct{}{}
		starts = append(starts, minuteStart)
	}
	if len(starts) == 0 {
		return nil
	}
	if !channelMonitorDirtyMinuteTableExists(DB) {
		err := fmt.Errorf("channel monitor dirty minute table does not exist")
		if enqueueOnFailure {
			if pendingErr := enqueueChannelMonitorDirtyMinutePending(ctx, starts, reason, err); pendingErr != nil {
				return errors.Join(err, pendingErr)
			}
		}
		return err
	}
	now := common.GetTimestamp()
	reason = normalizeChannelMonitorDirtyReason(reason)
	rows := make([]ChannelMonitorDirtyMinute, 0, len(starts))
	for _, minuteStart := range starts {
		rows = append(rows, ChannelMonitorDirtyMinute{
			MinuteStart:   minuteStart,
			DirtyReason:   reason,
			FirstMarkedAt: now,
			LastMarkedAt:  now,
			MarkCount:     1,
		})
	}
	err := withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		return DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "minute_start"}},
			DoUpdates: clause.Assignments(map[string]any{
				"last_marked_at": now,
				"mark_count": gorm.Expr("? + ?",
					clause.Column{Table: clause.CurrentTable, Name: "mark_count"}, 1,
				),
			}),
		}).Create(&rows).Error
	})
	if err == nil {
		if clearErr := clearChannelMonitorDirtyMinutePending(ctx, starts); clearErr != nil {
			return fmt.Errorf("脏分钟标记已写入但待处理清理失败: %w", clearErr)
		}
		return nil
	}
	if !enqueueOnFailure {
		return err
	}
	if pendingErr := enqueueChannelMonitorDirtyMinutePending(ctx, starts, reason, err); pendingErr != nil {
		return errors.Join(err, pendingErr)
	}
	return err
}

func enqueueChannelMonitorDirtyMinutePending(
	ctx context.Context,
	minuteStarts []int64,
	reason string,
	markerErr error,
) error {
	if DB == nil {
		return errors.New("channel monitor database is not initialized")
	}
	if err := ensureChannelMonitorDirtyMinutePendingTable(DB); err != nil {
		return err
	}
	now := common.GetTimestamp()
	reason = normalizeChannelMonitorDirtyReason(reason)
	lastError := "dirty marker write failed"
	if markerErr != nil {
		lastError = markerErr.Error()
	}
	if len(lastError) > 512 {
		lastError = lastError[:512]
	}
	rows := make([]ChannelMonitorDirtyMinutePending, 0, len(minuteStarts))
	for _, minuteStart := range minuteStarts {
		minuteStart = normalizeChannelMonitorDirtyMinute(minuteStart)
		if minuteStart <= 0 {
			continue
		}
		rows = append(rows, ChannelMonitorDirtyMinutePending{
			MinuteStart: minuteStart,
			DirtyReason: reason,
			Attempts:    1,
			NextRetryAt: now,
			LastError:   lastError,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		return DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "minute_start"}},
			DoUpdates: clause.Assignments(map[string]any{
				"dirty_reason":  reason,
				"attempts":      gorm.Expr("? + ?", clause.Column{Table: clause.CurrentTable, Name: "attempts"}, 1),
				"next_retry_at": now,
				"last_error":    lastError,
				"updated_at":    now,
			}),
		}).Create(&rows).Error
	})
}

func clearChannelMonitorDirtyMinutePending(ctx context.Context, minuteStarts []int64) error {
	if DB == nil || DB.Config.DryRun || !channelMonitorDirtyMinutePendingTableExists(DB) {
		return nil
	}
	starts := make([]int64, 0, len(minuteStarts))
	for _, minuteStart := range minuteStarts {
		if minuteStart = normalizeChannelMonitorDirtyMinute(minuteStart); minuteStart > 0 {
			starts = append(starts, minuteStart)
		}
	}
	if len(starts) == 0 {
		return nil
	}
	return withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		return DB.WithContext(ctx).
			Where("minute_start IN ?", starts).
			Delete(&ChannelMonitorDirtyMinutePending{}).Error
	})
}

// RetryChannelMonitorDirtyMinutePending replays durable marker failures. A
// failed replay remains pending with bounded exponential backoff; after the
// limit it is retained as a dead-letter row for operator inspection.
func RetryChannelMonitorDirtyMinutePending(ctx context.Context, limit int) error {
	if DB == nil || !channelMonitorDirtyMinutePendingTableExists(DB) {
		return nil
	}
	if limit <= 0 {
		limit = channelMonitorDirtyClaimMax
	}
	if limit > channelMonitorDirtyClaimMax {
		limit = channelMonitorDirtyClaimMax
	}
	now := common.GetTimestamp()
	var pending []ChannelMonitorDirtyMinutePending
	if err := withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		return DB.WithContext(ctx).
			Where("next_retry_at <= ? AND dead_letter_at = 0", now).
			Order("next_retry_at ASC, id ASC").Limit(limit).
			Find(&pending).Error
	}); err != nil {
		return err
	}
	for _, row := range pending {
		var existing ChannelMonitorDirtyMinute
		existingErr := withChannelMonitorDirtyMinuteRetry(ctx, func() error {
			return DB.WithContext(ctx).
				Where("minute_start = ?", row.MinuteStart).
				First(&existing).Error
		})
		if existingErr == nil {
			if clearErr := clearChannelMonitorDirtyMinutePending(ctx, []int64{row.MinuteStart}); clearErr != nil {
				return clearErr
			}
			continue
		}
		if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			if !strings.Contains(strings.ToLower(existingErr.Error()), "no such table") &&
				!strings.Contains(strings.ToLower(existingErr.Error()), "doesn't exist") {
				return existingErr
			}
		}
		err := markChannelMonitorDirtyMinutes(ctx, []int64{row.MinuteStart}, row.DirtyReason, false)
		if err == nil {
			continue
		}
		attempts := row.Attempts + 1
		updates := map[string]any{
			"attempts":   attempts,
			"last_error": err.Error(),
			"updated_at": common.GetTimestamp(),
		}
		if attempts >= channelMonitorDirtyPendingMaxAttempts {
			updates["dead_letter_at"] = common.GetTimestamp()
			updates["next_retry_at"] = 0
		} else {
			backoff := int64(1 << min(int(attempts), 10))
			if backoff > 3600 {
				backoff = 3600
			}
			updates["next_retry_at"] = common.GetTimestamp() + backoff
		}
		if updateErr := withChannelMonitorDirtyMinuteRetry(ctx, func() error {
			return DB.WithContext(ctx).Model(&ChannelMonitorDirtyMinutePending{}).
				Where("id = ? AND dead_letter_at = 0", row.Id).Updates(updates).Error
		}); updateErr != nil {
			return updateErr
		}
	}
	return nil
}

// ClaimChannelMonitorDirtyMinutes leases pending dirty rows. The lease is a
// claim state, not deletion; callers must complete rows explicitly after a
// successful rebuild. An error rolls back every claim in the transaction.
func ClaimChannelMonitorDirtyMinutes(
	ctx context.Context,
	limit int,
	claimer string,
	lockUntil int64,
) ([]ChannelMonitorDirtyMinute, error) {
	if DB == nil {
		return nil, errors.New("channel monitor database is not initialized")
	}
	claimer = strings.TrimSpace(claimer)
	if claimer == "" {
		return nil, errors.New("channel monitor dirty minute claimer is required")
	}
	if len(claimer) > 128 {
		return nil, errors.New("channel monitor dirty minute claimer is too long")
	}
	now := common.GetTimestamp()
	if lockUntil <= now {
		return nil, errors.New("channel monitor dirty minute lease must be in the future")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > channelMonitorDirtyClaimMax {
		limit = channelMonitorDirtyClaimMax
	}
	if !channelMonitorDirtyMinuteTableExists(DB) {
		return nil, fmt.Errorf("channel monitor dirty minute table does not exist")
	}

	claimed := make([]ChannelMonitorDirtyMinute, 0, limit)
	err := withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		claimed = claimed[:0]
		return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var candidates []ChannelMonitorDirtyMinute
			if err := lockForUpdate(tx).
				Where("claimed_until <= ?", now).
				Order("minute_start ASC, id ASC").
				Limit(limit).
				Find(&candidates).Error; err != nil {
				return err
			}
			for _, candidate := range candidates {
				result := tx.Model(&ChannelMonitorDirtyMinute{}).
					Where("id = ? AND claimed_until <= ?", candidate.Id, now).
					Updates(map[string]any{
						"claimed_by":    claimer,
						"claimed_at":    now,
						"claimed_until": lockUntil,
					})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					continue
				}
				candidate.ClaimedBy = claimer
				candidate.ClaimedAt = now
				candidate.ClaimedUntil = lockUntil
				claimed = append(claimed, candidate)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// CompleteChannelMonitorDirtyMinutes deletes only claims returned to the
// caller. If a row was re-marked after it was claimed, MarkCount changes and
// the row remains pending for another pass even when both writes share a
// one-second timestamp.
func CompleteChannelMonitorDirtyMinutes(ctx context.Context, claimer string, claims []ChannelMonitorDirtyMinute) error {
	if len(claims) == 0 {
		return nil
	}
	claimer = strings.TrimSpace(claimer)
	if claimer == "" {
		return errors.New("channel monitor dirty minute claimer is required")
	}
	now := common.GetTimestamp()
	return withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, claim := range claims {
				deleted := tx.Where(
					"id = ? AND claimed_by = ? AND claimed_at = ? AND mark_count = ? AND claimed_until > ?",
					claim.Id, claimer, claim.ClaimedAt, claim.MarkCount, now,
				).Delete(&ChannelMonitorDirtyMinute{})
				if deleted.Error != nil {
					return deleted.Error
				}
				if deleted.RowsAffected > 0 {
					continue
				}
				if err := tx.Model(&ChannelMonitorDirtyMinute{}).
					Where("id = ? AND claimed_by = ? AND claimed_at = ?", claim.Id, claimer, claim.ClaimedAt).
					Updates(map[string]any{
						"claimed_by":    "",
						"claimed_at":    0,
						"claimed_until": 0,
					}).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// ReleaseChannelMonitorDirtyMinutes makes claims immediately available after
// a failed rebuild. The marker itself is intentionally preserved.
func ReleaseChannelMonitorDirtyMinutes(ctx context.Context, claimer string, claims []ChannelMonitorDirtyMinute) error {
	if len(claims) == 0 {
		return nil
	}
	claimer = strings.TrimSpace(claimer)
	if claimer == "" {
		return errors.New("channel monitor dirty minute claimer is required")
	}
	return withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, claim := range claims {
				if err := tx.Model(&ChannelMonitorDirtyMinute{}).
					Where("id = ? AND claimed_by = ? AND claimed_at = ?", claim.Id, claimer, claim.ClaimedAt).
					Updates(map[string]any{
						"claimed_by":    "",
						"claimed_at":    0,
						"claimed_until": 0,
					}).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// RenewChannelMonitorDirtyMinutes extends a claim without changing the
// original claim timestamp. The timestamp remains the fencing token used by
// complete/release, so a stale worker can never renew or complete a newer
// worker's claim. Renewal is allowed after the nominal expiry until another
// worker successfully reclaims the row; this closes the SQLite writer-lock
// gap where a long rebuild can temporarily block the renewal transaction.
func RenewChannelMonitorDirtyMinutes(
	ctx context.Context,
	claimer string,
	claims []ChannelMonitorDirtyMinute,
	lockUntil int64,
) error {
	if len(claims) == 0 {
		return nil
	}
	if DB == nil {
		return errors.New("channel monitor database is not initialized")
	}
	claimer = strings.TrimSpace(claimer)
	if claimer == "" {
		return errors.New("channel monitor dirty minute claimer is required")
	}
	now := common.GetTimestamp()
	if lockUntil <= now {
		return errors.New("channel monitor dirty minute lease must be in the future")
	}
	return withChannelMonitorDirtyMinuteRetry(ctx, func() error {
		return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, claim := range claims {
				result := tx.Model(&ChannelMonitorDirtyMinute{}).
					Where("id = ? AND claimed_by = ? AND claimed_at = ?",
						claim.Id, claimer, claim.ClaimedAt).
					Updates(map[string]any{"claimed_until": lockUntil})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return ErrChannelMonitorDirtyMinuteLeaseLost
				}
			}
			return nil
		})
	})
}

type channelMonitorDirtyLogRow struct {
	CreatedAt      int64
	IsRetryAttempt bool
	Other          string
}

func markChannelMonitorDirtyMinutesForLog(log *Log) error {
	if log == nil || log.ChannelId <= 0 || (log.Type != LogTypeConsume && log.Type != LogTypeError) || log.CreatedAt <= 0 {
		return nil
	}
	if DB == nil {
		return errors.New("channel monitor database is not initialized")
	}
	parsedOther, parsed := channelMonitorMinuteOther(log.Other)
	if parsed && (parsedOther.SmartScheduleProbe || parsedOther.ChannelTest || parsedOther.GroupProbe || parsedOther.StatusProbe) {
		return nil
	}
	logMinute := normalizeChannelMonitorDirtyMinute(log.CreatedAt)
	currentMinute := channelMonitorMinuteStart(common.GetTimestamp())
	isRetryBoundary := log.IsRetryAttempt || parsed && parsedOther.FinalRetrySummary
	if logMinute >= currentMinute && !isRetryBoundary {
		return nil
	}
	candidateMinutes := make([]int64, 0, 4)
	appendCandidateMinute := func(createdAt int64) {
		minuteStart := normalizeChannelMonitorDirtyMinute(createdAt)
		if minuteStart > 0 && minuteStart < currentMinute {
			candidateMinutes = append(candidateMinutes, minuteStart)
		}
	}
	appendCandidateMinute(log.CreatedAt)

	if log.RequestId != "" && isRetryBoundary && LOG_DB != nil {
		var rows []channelMonitorDirtyLogRow
		if err := LOG_DB.WithContext(context.Background()).
			Model(&Log{}).
			Select("created_at, is_retry_attempt, other").
			Where("request_id = ? AND type = ?", log.RequestId, LogTypeError).
			Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			rowOther, rowParsed := channelMonitorMinuteOther(row.Other)
			if !row.IsRetryAttempt && (!rowParsed || !rowOther.FinalRetrySummary) {
				continue
			}
			appendCandidateMinute(row.CreatedAt)
		}
	}
	if len(candidateMinutes) == 0 {
		return nil
	}
	reason := ChannelMonitorDirtyReasonLateLog
	if isRetryBoundary {
		reason = ChannelMonitorDirtyReasonCrossMinuteRetry
	}
	// A closed-minute log can arrive after the aggregator scanned its source
	// range but before completed_through commits. Mark it unconditionally so the
	// later watermark update cannot make that log permanently invisible.
	return MarkChannelMonitorDirtyMinutes(context.Background(), candidateMinutes, reason)
}
