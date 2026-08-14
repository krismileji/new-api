package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

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
	ChannelId        int
	OccurredAt       int64
	CostNanoCNY      int64
	ProbeCostNanoCNY int64
	SettledDelta     int64
	UnresolvedDelta  int64
	APIKeyId         int
	APIKeyName       string
	KeyFingerprint   string
	KeyDisplay       string
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
	normalized := make([]ChannelDailyCostDelta, len(deltas))
	copy(normalized, deltas)
	for index := range normalized {
		delta := &normalized[index]
		if delta.ChannelId <= 0 {
			return errors.New("channel id must be positive")
		}
		if delta.CostNanoCNY < 0 {
			return errors.New("daily cost must not be negative")
		}
		if delta.ProbeCostNanoCNY < 0 || delta.ProbeCostNanoCNY > delta.CostNanoCNY {
			return errors.New("daily probe cost must be between zero and total cost")
		}
		if delta.SettledDelta < 0 || delta.UnresolvedDelta < 0 || (delta.SettledDelta == 0 && delta.UnresolvedDelta == 0) {
			return errors.New("daily cost event count must be positive")
		}
		if delta.KeyFingerprint == "" {
			continue
		}
		if len(delta.KeyFingerprint) != sha256.Size*2 {
			return errors.New("API key fingerprint must be a SHA-256 hex digest")
		}
		if _, err := hex.DecodeString(delta.KeyFingerprint); err != nil {
			return errors.New("API key fingerprint must be a SHA-256 hex digest")
		}
		if len(delta.KeyDisplay) > 64 {
			return errors.New("API key display must contain at most 64 bytes")
		}
		if delta.APIKeyId < 0 {
			return errors.New("API key id must not be negative")
		}
		delta.APIKeyName = strings.TrimSpace(delta.APIKeyName)
		if len(delta.APIKeyName) > 255 {
			return errors.New("API key name must contain at most 255 bytes")
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
		if total.CostNanoCNY > math.MaxInt64-delta.CostNanoCNY || total.ProbeCostNanoCNY > math.MaxInt64-delta.ProbeCostNanoCNY || total.SettledDelta > math.MaxInt64-delta.SettledDelta || total.UnresolvedDelta > math.MaxInt64-delta.UnresolvedDelta {
			return errors.New("daily cost batch total exceeds int64")
		}
		total.CostNanoCNY += delta.CostNanoCNY
		total.ProbeCostNanoCNY += delta.ProbeCostNanoCNY
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

	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, total := range orderedChannelTotals {
			if err := addChannelDailyCost(tx, total.ChannelId, total.OccurredAt, total.CostNanoCNY, total.ProbeCostNanoCNY, total.SettledDelta, total.UnresolvedDelta); err != nil {
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
		return nil
	})
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
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}, {Name: "day_start"}, {Name: "key_fingerprint"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"api_key_id":       apiKeyId,
			"api_key_name":     apiKeyName,
			"key_display":      keyDisplay,
			"cost_nano_cny":    gorm.Expr("cost_nano_cny + ?", costNanoCNY),
			"settled_count":    gorm.Expr("settled_count + ?", settledDelta),
			"unresolved_count": gorm.Expr("unresolved_count + ?", unresolvedDelta),
			"updated_at":       occurredAt,
		}),
	}).Create(&record).Error
}

func GetChannelDailyAPIKeyCosts(ctx context.Context, startTimestamp int64, endTimestamp int64) ([]ChannelDailyAPIKeyCost, error) {
	return getChannelDailyAPIKeyCosts(ctx, startTimestamp, endTimestamp, 0)
}

func GetChannelDailyAPIKeyCostsForChannel(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyAPIKeyCost, error) {
	return getChannelDailyAPIKeyCosts(ctx, startTimestamp, endTimestamp, channelId)
}

// GetChannelDailyAPIKeyCostTotalsForChannel aggregates the requested range in
// the database so overview pages do not load one row per API Key and day.
func GetChannelDailyAPIKeyCostTotalsForChannel(ctx context.Context, startTimestamp int64, endTimestamp int64, channelId int) ([]ChannelDailyAPIKeyCost, error) {
	if startTimestamp >= endTimestamp {
		return []ChannelDailyAPIKeyCost{}, nil
	}
	query := DB.WithContext(ctx).
		Model(&ChannelDailyAPIKeyCost{}).
		Select("MIN(id) AS id, channel_id, api_key_id, api_key_name, key_fingerprint, key_display, SUM(cost_nano_cny) AS cost_nano_cny, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count").
		Where("day_start >= ? AND day_start < ?", startTimestamp, endTimestamp)
	if channelId > 0 {
		query = query.Where("channel_id = ?", channelId)
	}
	var costs []ChannelDailyAPIKeyCost
	err := query.
		Group("channel_id, api_key_id, api_key_name, key_fingerprint, key_display").
		Order("channel_id ASC, api_key_id ASC, key_fingerprint ASC").
		Find(&costs).Error
	if err != nil {
		return nil, err
	}
	return resolveChannelDailyAPIKeyCostNames(ctx, costs)
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
	return resolveChannelDailyAPIKeyCostNames(ctx, costs)
}

func resolveChannelDailyAPIKeyCostNames(ctx context.Context, costs []ChannelDailyAPIKeyCost) ([]ChannelDailyAPIKeyCost, error) {
	if len(costs) == 0 {
		return costs, nil
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
	if err := DB.WithContext(ctx).Model(&Token{}).Select("id, name").Where("id IN ?", ids).Find(&tokens).Error; err != nil {
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
