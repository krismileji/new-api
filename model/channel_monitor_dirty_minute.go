package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	channelMonitorDirtyMinuteTable = "channel_monitor_dirty_minutes"
	channelMonitorDirtyReasonMax   = 64
	channelMonitorDirtyClaimMax    = 100
)

const (
	ChannelMonitorDirtyReasonLateLog          = "late_log"
	ChannelMonitorDirtyReasonCrossMinuteRetry = "cross_minute_retry"
)

type channelMonitorDirtyMinuteDatabaseState struct {
	mu           sync.Mutex
	tableChecked bool
	tableExists  bool
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
		return fmt.Errorf("channel monitor dirty minute table does not exist")
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
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "minute_start"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_marked_at": now,
			"mark_count": gorm.Expr("? + ?",
				clause.Column{Table: clause.CurrentTable, Name: "mark_count"}, 1,
			),
		}),
	}).Create(&rows).Error
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
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, claim := range claims {
			deleted := tx.Where(
				"id = ? AND claimed_by = ? AND claimed_at = ? AND mark_count = ?",
				claim.Id, claimer, claim.ClaimedAt, claim.MarkCount,
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
		return nil
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
