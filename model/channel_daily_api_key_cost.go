package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

var ErrChannelDailyCostLedgerOverflow = errors.New("channel daily cost ledger exceeds int64")

// ErrChannelDailyCostEventIDRequiresOutbox marks direct ledger writes that
// carry an event id. Event ids are the durable idempotency boundary owned by
// the cost outbox; accepting one here would make a replay indistinguishable
// from a new charge and could double-count the ledger.
var ErrChannelDailyCostEventIDRequiresOutbox = errors.New("带 EventId 的渠道日成本批次必须通过 outbox 写入")

// ChannelDailyAPIKeyCost stores the daily settled cost attributed to one
// inbound API Key and its selected upstream credential. Only API Key metadata,
// a fingerprint, and a masked display value are persisted.
type ChannelDailyAPIKeyCost struct {
	Id              int64  `gorm:"primaryKey"`
	ChannelId       int    `gorm:"not null;uniqueIndex:idx_channel_daily_api_key_cost_key"`
	DayStart        int64  `gorm:"not null;uniqueIndex:idx_channel_daily_api_key_cost_key;index:idx_channel_daily_api_key_cost_day_start"`
	APIKeyId        int    `gorm:"not null;default:0;index:idx_channel_daily_api_key_cost_api_key"`
	APIKeyName      string `gorm:"size:255;not null;default:''"`
	KeyFingerprint  string `gorm:"size:64;not null;uniqueIndex:idx_channel_daily_api_key_cost_key"`
	KeyDisplay      string `gorm:"size:64;not null"`
	CostNanoCNY     int64  `gorm:"not null"`
	SettledCount    int64  `gorm:"not null"`
	UnresolvedCount int64  `gorm:"not null"`
	CreatedAt       int64  `gorm:"not null"`
	UpdatedAt       int64  `gorm:"not null"`
}

// ChannelDailyCostDelta is one already-aggregated channel cost update. A
// batch can contain different channels, days, and API Keys.
type ChannelDailyCostDelta struct {
	EventId               string
	ChannelId             int
	OccurredAt            int64
	CostNanoCNY           int64
	ProbeCostNanoCNY      int64
	GroupProbeCostNanoCNY int64
	SettledDelta          int64
	UnresolvedDelta       int64
	APIKeyId              int
	APIKeyName            string
	KeyFingerprint        string
	KeyDisplay            string
	UserId                int
	UserAttribution       string
	ModelName             string
	SourceKind            string
}

// ValidateChannelDailyCostDelta checks the ledger invariants without mutating
// the caller's value. Reliable consumers use it to quarantine malformed
// events before they can block a durable batch.
func ValidateChannelDailyCostDelta(delta ChannelDailyCostDelta) error {
	return normalizeChannelDailyCostDelta(&delta)
}

// ChannelDailyCostAPIKeyIdentity returns stable, non-reversible identity data
// for an upstream key. The full credential must never be persisted or returned.
func ChannelDailyCostAPIKeyIdentity(key string) (string, string) {
	normalized := strings.TrimSpace(key)
	if normalized == "" {
		return "", ""
	}

	digest := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(digest[:]), channelDailyCostAPIKeyDisplay(normalized)
}

// ChannelDailyCostAPIKeyIdentityForToken keeps different inbound API Keys
// separate even when they happen to select the same upstream credential.
func ChannelDailyCostAPIKeyIdentityForToken(tokenId int, key string) (string, string) {
	normalized := strings.TrimSpace(key)
	if tokenId <= 0 {
		return ChannelDailyCostAPIKeyIdentity(normalized)
	}
	if normalized == "" {
		normalized = "<empty>"
	}
	return channelDailyCostAPIKeyIdentity(strconv.Itoa(tokenId), normalized),
		channelDailyCostAPIKeyDisplay(strings.TrimSpace(key))
}

func channelDailyCostAPIKeyIdentity(tokenId string, key string) string {
	digest := sha256.Sum256([]byte(tokenId + ":" + key))
	return hex.EncodeToString(digest[:])
}

func channelDailyCostAPIKeyDisplay(normalized string) string {
	if normalized == "" {
		return ""
	}
	display := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, normalized)
	display = strings.Join(strings.Fields(display), " ")
	runes := []rune(display)
	switch {
	case len(runes) <= 4:
		display = strings.Repeat("*", len(runes))
	case len(runes) <= 8:
		display = string(runes[:2]) + "****" + string(runes[len(runes)-2:])
	default:
		display = string(runes[:4]) + "**********" + string(runes[len(runes)-4:])
	}
	return display
}

// AddChannelDailyCostWithAPIKey atomically updates the channel total and, when
// a key is available, the corresponding per-key total.
func AddChannelDailyCostWithAPIKey(ctx context.Context, channelId int, occurredAt int64, costNanoCNY int64, settledDelta int64, unresolvedDelta int64, keyFingerprint string, keyDisplay string) error {
	return AddChannelDailyCostWithAPIKeyAndToken(ctx, channelId, occurredAt, costNanoCNY, settledDelta, unresolvedDelta, 0, "", keyFingerprint, keyDisplay)
}

// AddChannelDailyCostWithAPIKeyAndToken atomically updates the channel total
// and the cost attributed to one inbound API Key.
func AddChannelDailyCostWithAPIKeyAndToken(ctx context.Context, channelId int, occurredAt int64, costNanoCNY int64, settledDelta int64, unresolvedDelta int64, apiKeyId int, apiKeyName string, keyFingerprint string, keyDisplay string) error {
	return AddChannelDailyCostBatch(ctx, []ChannelDailyCostDelta{{
		ChannelId:       channelId,
		OccurredAt:      occurredAt,
		CostNanoCNY:     costNanoCNY,
		SettledDelta:    settledDelta,
		UnresolvedDelta: unresolvedDelta,
		APIKeyId:        apiKeyId,
		APIKeyName:      apiKeyName,
		KeyFingerprint:  keyFingerprint,
		KeyDisplay:      keyDisplay,
	}})
}

// AddChannelDailyCostBatch atomically applies a bounded in-process batch. The
// conflict updates remain atomic across multiple application instances.
func AddChannelDailyCostBatch(ctx context.Context, deltas []ChannelDailyCostDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	for _, delta := range deltas {
		if strings.TrimSpace(delta.EventId) != "" {
			return ErrChannelDailyCostEventIDRequiresOutbox
		}
	}
	if DB == nil {
		return errors.New("channel daily cost database is unavailable")
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return addChannelDailyCostBatch(tx, deltas)
	})
}

func addChannelDailyCostBatch(tx *gorm.DB, deltas []ChannelDailyCostDelta) error {
	if tx == nil {
		return errors.New("channel daily cost database is unavailable")
	}
	normalized := make([]ChannelDailyCostDelta, len(deltas))
	copy(normalized, deltas)
	for index := range normalized {
		if err := normalizeChannelDailyCostDelta(&normalized[index]); err != nil {
			return err
		}
	}
	type channelDayKey struct {
		ChannelId int
		DayStart  int64
	}
	channelTotals := make(map[channelDayKey]ChannelDailyCostDelta)
	for _, delta := range normalized {
		key := channelDayKey{ChannelId: delta.ChannelId, DayStart: ChannelDailyCostDayStart(delta.OccurredAt)}
		total, exists := channelTotals[key]
		if !exists {
			channelTotals[key] = delta
			continue
		}
		if total.CostNanoCNY > math.MaxInt64-delta.CostNanoCNY || total.ProbeCostNanoCNY > math.MaxInt64-delta.ProbeCostNanoCNY || total.GroupProbeCostNanoCNY > math.MaxInt64-delta.GroupProbeCostNanoCNY || total.SettledDelta > math.MaxInt64-delta.SettledDelta || total.UnresolvedDelta > math.MaxInt64-delta.UnresolvedDelta {
			return fmt.Errorf("%w: daily cost batch total exceeds int64", ErrChannelDailyCostLedgerOverflow)
		}
		total.CostNanoCNY += delta.CostNanoCNY
		total.ProbeCostNanoCNY += delta.ProbeCostNanoCNY
		total.GroupProbeCostNanoCNY += delta.GroupProbeCostNanoCNY
		total.SettledDelta += delta.SettledDelta
		total.UnresolvedDelta += delta.UnresolvedDelta
		if delta.OccurredAt > total.OccurredAt {
			total.OccurredAt = delta.OccurredAt
		}
		channelTotals[key] = total
	}
	orderedChannelTotals := make([]ChannelDailyCostDelta, 0, len(channelTotals))
	for _, total := range channelTotals {
		orderedChannelTotals = append(orderedChannelTotals, total)
	}
	sort.Slice(orderedChannelTotals, func(i int, j int) bool {
		if orderedChannelTotals[i].ChannelId != orderedChannelTotals[j].ChannelId {
			return orderedChannelTotals[i].ChannelId < orderedChannelTotals[j].ChannelId
		}
		return ChannelDailyCostDayStart(orderedChannelTotals[i].OccurredAt) < ChannelDailyCostDayStart(orderedChannelTotals[j].OccurredAt)
	})
	sort.SliceStable(normalized, func(i int, j int) bool {
		if normalized[i].ChannelId != normalized[j].ChannelId {
			return normalized[i].ChannelId < normalized[j].ChannelId
		}
		iDayStart := ChannelDailyCostDayStart(normalized[i].OccurredAt)
		jDayStart := ChannelDailyCostDayStart(normalized[j].OccurredAt)
		if iDayStart != jDayStart {
			return iDayStart < jDayStart
		}
		if normalized[i].KeyFingerprint != normalized[j].KeyFingerprint {
			return normalized[i].KeyFingerprint < normalized[j].KeyFingerprint
		}
		return normalized[i].OccurredAt < normalized[j].OccurredAt
	})

	for _, total := range orderedChannelTotals {
		if err := addChannelDailyCost(tx, total.ChannelId, total.OccurredAt, total.CostNanoCNY, total.ProbeCostNanoCNY, total.GroupProbeCostNanoCNY, total.SettledDelta, total.UnresolvedDelta); err != nil {
			return err
		}
	}
	for _, delta := range normalized {
		if delta.KeyFingerprint == "" {
			continue
		}
		if err := addChannelDailyAPIKeyCost(tx, delta.ChannelId, delta.OccurredAt, delta.CostNanoCNY, delta.SettledDelta, delta.UnresolvedDelta, delta.APIKeyId, delta.APIKeyName, delta.KeyFingerprint, delta.KeyDisplay); err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&ChannelMonitorDailyCostDetail{}) {
		for _, delta := range normalized {
			if err := addChannelMonitorDailyCostDetail(tx, delta); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeChannelDailyCostDelta(delta *ChannelDailyCostDelta) error {
	if delta == nil || delta.ChannelId <= 0 {
		return errors.New("channel id must be positive")
	}
	if delta.OccurredAt <= 0 {
		return errors.New("cost event timestamp must be positive")
	}
	if delta.OccurredAt > math.MaxInt64-channelDailyCostUTC8Offset {
		return errors.New("cost event timestamp exceeds supported range")
	}
	if delta.CostNanoCNY < 0 {
		return errors.New("daily cost must not be negative")
	}
	if delta.ProbeCostNanoCNY < 0 || delta.ProbeCostNanoCNY > delta.CostNanoCNY {
		return errors.New("daily probe cost must be between zero and total cost")
	}
	if delta.GroupProbeCostNanoCNY < 0 || delta.GroupProbeCostNanoCNY > delta.ProbeCostNanoCNY {
		return errors.New("daily group probe cost must be between zero and probe cost")
	}
	if delta.SettledDelta < 0 || delta.UnresolvedDelta < 0 || (delta.SettledDelta == 0 && delta.UnresolvedDelta == 0) {
		return errors.New("daily cost event count must be positive")
	}
	if len(delta.KeyDisplay) > 64 {
		return errors.New("API key display must contain at most 64 bytes")
	}
	if delta.APIKeyId < 0 {
		return errors.New("API key id must not be negative")
	}
	if delta.UserId < 0 {
		return errors.New("user id must not be negative")
	}
	delta.UserAttribution = strings.TrimSpace(delta.UserAttribution)
	if delta.UserAttribution != "" && delta.UserAttribution != string(ChannelMonitorEventUserAttributionRequest) && delta.UserAttribution != string(ChannelMonitorEventUserAttributionInferred) && delta.UserAttribution != string(ChannelMonitorEventUserAttributionUnknown) {
		return errors.New("user attribution is invalid")
	}
	delta.APIKeyName = strings.TrimSpace(delta.APIKeyName)
	if len(delta.APIKeyName) > 255 {
		return errors.New("API key name must contain at most 255 bytes")
	}
	delta.ModelName = strings.TrimSpace(delta.ModelName)
	if len(delta.ModelName) > ChannelMonitorEventMaxNameLength {
		return errors.New("model name must contain at most 255 bytes")
	}
	delta.SourceKind = strings.TrimSpace(delta.SourceKind)
	if len(delta.SourceKind) > 32 {
		return errors.New("cost source kind must contain at most 32 bytes")
	}
	if delta.KeyFingerprint == "" {
		return nil
	}
	if len(delta.KeyFingerprint) != sha256.Size*2 {
		return errors.New("API key fingerprint must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(delta.KeyFingerprint); err != nil {
		return errors.New("API key fingerprint must be a SHA-256 hex digest")
	}
	return nil
}

func addChannelDailyAPIKeyCost(tx *gorm.DB, channelId int, occurredAt int64, costNanoCNY int64, settledDelta int64, unresolvedDelta int64, apiKeyId int, apiKeyName string, keyFingerprint string, keyDisplay string) error {
	if channelId <= 0 {
		return errors.New("channel id must be positive")
	}
	if costNanoCNY < 0 {
		return errors.New("daily API key cost must not be negative")
	}
	if settledDelta < 0 || unresolvedDelta < 0 || (settledDelta == 0 && unresolvedDelta == 0) {
		return errors.New("daily API key cost event count must be positive")
	}

	record := ChannelDailyAPIKeyCost{
		ChannelId:       channelId,
		DayStart:        ChannelDailyCostDayStart(occurredAt),
		APIKeyId:        apiKeyId,
		APIKeyName:      apiKeyName,
		KeyFingerprint:  keyFingerprint,
		KeyDisplay:      keyDisplay,
		CostNanoCNY:     costNanoCNY,
		SettledCount:    settledDelta,
		UnresolvedCount: unresolvedDelta,
		CreatedAt:       occurredAt,
		UpdatedAt:       occurredAt,
	}
	updated, err := updateChannelDailyAPIKeyCostIfWithinBounds(
		tx, record, costNanoCNY, settledDelta, unresolvedDelta,
	)
	if err != nil || updated {
		return err
	}

	var existingId int64
	err = tx.Model(&ChannelDailyAPIKeyCost{}).
		Select("id").
		Where("channel_id = ? AND day_start = ? AND key_fingerprint = ?", channelId, record.DayStart, keyFingerprint).
		Take(&existingId).Error
	if err == nil {
		return fmt.Errorf("%w: 渠道 API Key 日成本累计超过 int64 范围", ErrChannelDailyCostLedgerOverflow)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	const savepoint = "channel_daily_api_key_cost_insert"
	if err := tx.SavePoint(savepoint).Error; err != nil {
		return err
	}
	createErr := tx.Create(&record).Error
	if createErr == nil {
		return nil
	}
	if err := tx.RollbackTo(savepoint).Error; err != nil {
		return err
	}

	updated, err = updateChannelDailyAPIKeyCostIfWithinBounds(
		tx, record, costNanoCNY, settledDelta, unresolvedDelta,
	)
	if err != nil || updated {
		return err
	}
	err = tx.Model(&ChannelDailyAPIKeyCost{}).
		Select("id").
		Where("channel_id = ? AND day_start = ? AND key_fingerprint = ?", channelId, record.DayStart, keyFingerprint).
		Take(&existingId).Error
	if err == nil {
		return fmt.Errorf("%w: 渠道 API Key 日成本累计超过 int64 范围", ErrChannelDailyCostLedgerOverflow)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return createErr
}

func updateChannelDailyAPIKeyCostIfWithinBounds(tx *gorm.DB, record ChannelDailyAPIKeyCost, costNanoCNY int64, settledDelta int64, unresolvedDelta int64) (bool, error) {
	update := tx.Model(&ChannelDailyAPIKeyCost{}).
		Where("channel_id = ? AND day_start = ? AND key_fingerprint = ?", record.ChannelId, record.DayStart, record.KeyFingerprint).
		Where("cost_nano_cny >= 0 AND settled_count >= 0 AND unresolved_count >= 0").
		Where("cost_nano_cny <= ?", math.MaxInt64-costNanoCNY).
		Where("settled_count <= ?", math.MaxInt64-settledDelta).
		Where("unresolved_count <= ?", math.MaxInt64-unresolvedDelta).
		Updates(map[string]interface{}{
			"api_key_id":       record.APIKeyId,
			"api_key_name":     record.APIKeyName,
			"key_display":      record.KeyDisplay,
			"cost_nano_cny":    gorm.Expr("cost_nano_cny + ?", costNanoCNY),
			"settled_count":    gorm.Expr("settled_count + ?", settledDelta),
			"unresolved_count": gorm.Expr("unresolved_count + ?", unresolvedDelta),
			"updated_at":       record.UpdatedAt,
		})
	if update.Error != nil {
		return false, update.Error
	}
	return update.RowsAffected == 1, nil
}

func GetChannelDailyAPIKeyCosts(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelDailyAPIKeyCost, error) {
	return getChannelDailyAPIKeyCosts(ctx, startTimestamp, endTimestamp, 0)
}

func GetChannelDailyAPIKeyCostsForChannel(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyAPIKeyCost, error) {
	return getChannelDailyAPIKeyCosts(ctx, startTimestamp, endTimestamp, channelId)
}

// GetChannelDailyAPIKeyCostTotalsForChannel aggregates the requested range
// with checked arithmetic so no database-specific SUM behavior can wrap a
// monetary or count total.
func GetChannelDailyAPIKeyCostTotalsForChannel(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyAPIKeyCost, error) {
	costs, err := getChannelDailyAPIKeyCostTotalsForChannel(ctx, DB, startTimestamp, endTimestamp, channelId, 0, 0)
	if err != nil {
		return nil, err
	}
	return resolveChannelDailyAPIKeyCostNames(ctx, DB, costs)
}

// GetChannelDailyAPIKeyCostTotalsForChannelBounded returns at most limit
// channel/key groups. Groups are ordered by descending cost so a bounded
// monitor response keeps the most material identities first. The boolean
// result reports whether additional groups exist after the requested page.
// A non-positive limit preserves the unbounded behavior of the legacy query.
//
// The channel monitor uses this bounded variant for user-facing overviews;
// omitted groups are represented by its existing unattributed residual so
// channel totals remain conserved.
func GetChannelDailyAPIKeyCostTotalsForChannelBounded(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int, limit int) ([]ChannelDailyAPIKeyCost, bool, error) {
	costs, truncated, err := getChannelDailyAPIKeyCostTotalsForChannelBounded(ctx, DB, startTimestamp, endTimestamp, channelId, limit)
	if err != nil {
		return nil, false, err
	}
	costs, err = resolveChannelDailyAPIKeyCostNames(ctx, DB, costs)
	return costs, truncated, err
}

// GetChannelDailyAPIKeyCostTotalsForMonitor returns bounded ledger rows from a
// caller-provided consistent transaction. API Key names are left as recorded
// in the ledger because the monitor controller resolves the current token name
// and owner in one batched lookup.
func GetChannelDailyAPIKeyCostTotalsForMonitor(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int, limit int) ([]ChannelDailyAPIKeyCost, bool, error) {
	return getChannelDailyAPIKeyCostTotalsForChannelBounded(ctx, db, startTimestamp, endTimestamp, channelId, limit)
}

func getChannelDailyAPIKeyCostTotalsForChannelBounded(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int, limit int) ([]ChannelDailyAPIKeyCost, bool, error) {
	if limit <= 0 {
		costs, err := getChannelDailyAPIKeyCostTotalsForChannel(ctx, db, startTimestamp, endTimestamp, channelId, 0, 0)
		return costs, false, err
	}
	// The sentinel row below uses limit+1. Reject the largest int value so a
	// caller cannot wrap that expression negative and trigger an invalid slice
	// bound (or accidentally turn a bounded request into an unbounded scan).
	if limit == math.MaxInt {
		return nil, false, errors.New("渠道 API Key 成本汇总 limit 超出支持范围")
	}
	// Fetch one sentinel row so callers can distinguish an exact-limit result
	// from a truncated result without a second COUNT query over the aggregate.
	costs, err := getChannelDailyAPIKeyCostTotalsForChannel(ctx, db, startTimestamp, endTimestamp, channelId, limit+1, 0)
	if err != nil {
		return nil, false, err
	}
	// The legacy helper sorts by channel/key for compatibility. Reorder before
	// trimming so the sentinel query cannot discard a more expensive group
	// that happened to sort later in that legacy order.
	sort.SliceStable(costs, func(i, j int) bool {
		if costs[i].CostNanoCNY != costs[j].CostNanoCNY {
			return costs[i].CostNanoCNY > costs[j].CostNanoCNY
		}
		if costs[i].ChannelId != costs[j].ChannelId {
			return costs[i].ChannelId < costs[j].ChannelId
		}
		if costs[i].APIKeyId != costs[j].APIKeyId {
			return costs[i].APIKeyId < costs[j].APIKeyId
		}
		return costs[i].KeyFingerprint < costs[j].KeyFingerprint
	})
	truncated := len(costs) > limit
	if truncated {
		costs = costs[:limit]
	}
	return costs, truncated, nil
}

func getChannelDailyAPIKeyCostTotalsForChannel(ctx context.Context, db *gorm.DB, startTimestamp int64, endTimestamp int64, channelId int, limit int, offset int) ([]ChannelDailyAPIKeyCost, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyAPIKeyCost{}, nil
	}
	queryDB, err := channelMonitorCostQueryDB(ctx, db)
	if err != nil {
		return nil, err
	}
	query := queryDB.Model(&ChannelDailyAPIKeyCost{}).
		Select("MIN(id) AS id, channel_id, api_key_id, MAX(api_key_name) AS api_key_name, MAX(key_fingerprint) AS key_fingerprint, MAX(key_display) AS key_display, SUM(cost_nano_cny) AS cost_nano_cny, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count").
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp).
		Group("channel_id").Group("api_key_id").Group("CASE WHEN api_key_id = 0 THEN key_fingerprint ELSE '' END").
		Order("SUM(cost_nano_cny) DESC, channel_id ASC, api_key_id ASC, CASE WHEN api_key_id = 0 THEN key_fingerprint ELSE '' END ASC")
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	var costs []ChannelDailyAPIKeyCost
	if err := query.Scan(&costs).Error; err != nil {
		return nil, fmt.Errorf("渠道 API Key 成本汇总超过 int64 范围或查询失败: %w", err)
	}
	for _, cost := range costs {
		if cost.CostNanoCNY < 0 || cost.SettledCount < 0 || cost.UnresolvedCount < 0 {
			return nil, errors.New("渠道 API Key 成本汇总超过 int64 范围")
		}
	}
	sort.Slice(costs, func(i, j int) bool {
		if costs[i].ChannelId != costs[j].ChannelId {
			return costs[i].ChannelId < costs[j].ChannelId
		}
		if costs[i].APIKeyId != costs[j].APIKeyId {
			return costs[i].APIKeyId < costs[j].APIKeyId
		}
		return costs[i].KeyFingerprint < costs[j].KeyFingerprint
	})
	return costs, nil
}

func getChannelDailyAPIKeyCosts(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyAPIKeyCost, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyAPIKeyCost{}, nil
	}
	query := DB.WithContext(ctx).
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp)
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	var costs []ChannelDailyAPIKeyCost
	err := query.Order("day_start ASC, channel_id ASC, key_fingerprint ASC").Find(&costs).Error
	if err != nil {
		return costs, err
	}
	return resolveChannelDailyAPIKeyCostNames(ctx, DB, costs)
}

func resolveChannelDailyAPIKeyCostNames(ctx context.Context, db *gorm.DB, costs []ChannelDailyAPIKeyCost) ([]ChannelDailyAPIKeyCost, error) {
	if len(costs) == 0 {
		return costs, nil
	}
	queryDB, err := channelMonitorCostQueryDB(ctx, db)
	if err != nil {
		return nil, err
	}
	missingNameIDs := make(map[int]struct{})
	for _, cost := range costs {
		if cost.APIKeyId > 0 && strings.TrimSpace(cost.APIKeyName) == "" {
			missingNameIDs[cost.APIKeyId] = struct{}{}
		}
	}
	if len(missingNameIDs) == 0 {
		return costs, nil
	}
	ids := make([]int, 0, len(missingNameIDs))
	for id := range missingNameIDs {
		ids = append(ids, id)
	}
	var tokens []Token
	if err := queryDB.Model(&Token{}).Select("id, name").Where("id IN ?", ids).Find(&tokens).Error; err != nil {
		return nil, err
	}
	tokenNames := make(map[int]string, len(tokens))
	for _, token := range tokens {
		tokenNames[token.Id] = strings.TrimSpace(token.Name)
	}
	for index := range costs {
		if costs[index].APIKeyName == "" {
			costs[index].APIKeyName = tokenNames[costs[index].APIKeyId]
		}
	}
	return costs, nil
}
