package controller

import (
	"context"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
)

type channelMonitorCostReadSource struct {
	today   int64
	current *service.ChannelMonitorRedisSharedDailyCostView
}

func loadChannelMonitorCostReadSource(ctx context.Context, now int64, summaryOnly bool) (channelMonitorCostReadSource, error) {
	source := channelMonitorCostReadSource{today: channelMonitorCostDayStart(now)}
	if !common.RedisEnabled || source.today != channelMonitorCostDayStart(common.GetTimestamp()) {
		return source, nil
	}
	query := service.QueryChannelMonitorRedisDailyCosts
	if summaryOnly {
		query = service.QueryChannelMonitorRedisDailyCostTotals
	}
	view, err := query(ctx, source.today)
	if err != nil {
		return source, err
	}
	source.current = &view
	return source, nil
}

func (source channelMonitorCostReadSource) databaseEnd(end int64) int64 {
	if source.current != nil {
		return min(end, source.today)
	}
	return end
}

func (source channelMonitorCostReadSource) includesToday(start, end int64) bool {
	return source.current != nil && start <= source.today && end > source.today
}

func (source channelMonitorCostReadSource) dayTotals(ctx context.Context, db *gorm.DB, start, end int64, channelID int) ([]model.ChannelDailyCostDayTotal, error) {
	rows, err := model.GetChannelDailyCostDayTotalsWithDB(ctx, db, start, source.databaseEnd(end), channelID)
	if err != nil || !source.includesToday(start, end) {
		return rows, err
	}
	total := source.current.Global
	if channelID > 0 {
		total = source.current.Channels[channelID]
	}
	return append(rows, model.ChannelDailyCostDayTotal{
		DayStart: source.today, CostNanoCNY: total.SettledCostNanoCNY,
		ProbeCostNanoCNY: total.ProbeSettledCostNanoCNY, GroupProbeCostNanoCNY: total.GroupProbeSettledCostNanoCNY,
		ModelDetectionCostNanoCNY: total.ModelDetectionSettledCostNanoCNY,
		SettledCount:              total.SettledRequestCount, UnresolvedCount: total.UnresolvedRequestCount,
	}), nil
}

func (source channelMonitorCostReadSource) channelTotals(ctx context.Context, db *gorm.DB, start, end int64, channelID int, detailDay int64) ([]model.ChannelDailyCostChannelTotal, error) {
	rows, err := model.GetChannelDailyCostChannelTotalsWithDetailAndDB(ctx, db, start, source.databaseEnd(end), channelID, detailDay)
	if err != nil || !source.includesToday(start, end) {
		return rows, err
	}
	byChannel := make(map[int]int, len(rows))
	for index, row := range rows {
		byChannel[row.ChannelId] = index
	}
	for id, aggregate := range source.current.Channels {
		if channelID > 0 && channelID != id {
			continue
		}
		index, exists := byChannel[id]
		if !exists {
			index = len(rows)
			rows = append(rows, model.ChannelDailyCostChannelTotal{ChannelId: id})
		}
		row := &rows[index]
		for _, value := range []struct {
			target *int64
			amount int64
		}{
			{&row.CostNanoCNY, aggregate.SettledCostNanoCNY},
			{&row.ProbeCostNanoCNY, aggregate.ProbeSettledCostNanoCNY},
			{&row.GroupProbeCostNanoCNY, aggregate.GroupProbeSettledCostNanoCNY},
			{&row.ModelDetectionCostNanoCNY, aggregate.ModelDetectionSettledCostNanoCNY},
			{&row.SettledCount, aggregate.SettledRequestCount},
			{&row.UnresolvedCount, aggregate.UnresolvedRequestCount},
		} {
			if err := channelMonitorAddNonNegativeInt64(value.target, value.amount); err != nil {
				return nil, err
			}
		}
		if detailDay == source.today {
			row.DetailCostNanoCNY = aggregate.SettledCostNanoCNY
			row.DetailProbeCostNanoCNY = aggregate.ProbeSettledCostNanoCNY
			row.DetailGroupProbeCostNanoCNY = aggregate.GroupProbeSettledCostNanoCNY
			row.DetailModelDetectionCostNanoCNY = aggregate.ModelDetectionSettledCostNanoCNY
			row.DetailSettledCount = aggregate.SettledRequestCount
			row.DetailUnresolvedCount = aggregate.UnresolvedRequestCount
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ChannelId < rows[j].ChannelId })
	return rows, nil
}

func channelMonitorCostKeyIdentity(row model.ChannelDailyAPIKeyCost) string {
	key := strconv.Itoa(row.ChannelId) + ":" + strconv.Itoa(row.APIKeyId)
	if row.APIKeyId == 0 {
		key += ":" + row.KeyFingerprint
	}
	return key
}

// Candidates are the union of historical top rows and keys used today. Fetch
// historical contributions for today's keys before ranking the combined range.
func (source channelMonitorCostReadSource) apiKeyTotals(ctx context.Context, db *gorm.DB, start, end int64, channelID, limit int) ([]model.ChannelDailyAPIKeyCost, bool, error) {
	rows, truncated, err := model.GetChannelDailyAPIKeyCostTotalsForMonitor(ctx, db, start, source.databaseEnd(end), channelID, limit)
	if err != nil || !source.includesToday(start, end) {
		return rows, truncated, err
	}
	byKey := make(map[string]model.ChannelDailyAPIKeyCost, len(rows))
	for _, row := range rows {
		byKey[channelMonitorCostKeyIdentity(row)] = row
	}
	if truncated {
		ids := make(map[int]bool)
		fingerprints := make(map[string]bool)
		for _, row := range source.current.KeyCosts {
			if channelID > 0 && row.ChannelId != channelID {
				continue
			}
			if row.APIKeyId > 0 {
				ids[row.APIKeyId] = true
			} else {
				fingerprints[row.KeyFingerprint] = true
			}
		}
		keyIDs := make([]int, 0, len(ids))
		for id := range ids {
			keyIDs = append(keyIDs, id)
		}
		for offset := 0; offset < len(keyIDs); offset += channelMonitorOwnerLookupBatchSize {
			endOffset := min(offset+channelMonitorOwnerLookupBatchSize, len(keyIDs))
			candidates, _, err := model.GetChannelDailyAPIKeyCostTotalsForMonitor(ctx, db.Where("api_key_id IN ?", keyIDs[offset:endOffset]), start, source.today, channelID, 0)
			if err != nil {
				return nil, false, err
			}
			for _, row := range candidates {
				byKey[channelMonitorCostKeyIdentity(row)] = row
			}
		}
		keys := make([]string, 0, len(fingerprints))
		for key := range fingerprints {
			keys = append(keys, key)
		}
		for offset := 0; offset < len(keys); offset += channelMonitorOwnerLookupBatchSize {
			endOffset := min(offset+channelMonitorOwnerLookupBatchSize, len(keys))
			candidates, _, err := model.GetChannelDailyAPIKeyCostTotalsForMonitor(ctx, db.Where("api_key_id = ? AND key_fingerprint IN ?", 0, keys[offset:endOffset]), start, source.today, channelID, 0)
			if err != nil {
				return nil, false, err
			}
			for _, row := range candidates {
				byKey[channelMonitorCostKeyIdentity(row)] = row
			}
		}
	}
	for _, today := range source.current.KeyCosts {
		if channelID > 0 && today.ChannelId != channelID {
			continue
		}
		key := channelMonitorCostKeyIdentity(today)
		row, exists := byKey[key]
		if !exists {
			row = today
			row.CostNanoCNY, row.SettledCount, row.UnresolvedCount = 0, 0, 0
		}
		for _, value := range []struct {
			target *int64
			amount int64
		}{
			{&row.CostNanoCNY, today.CostNanoCNY}, {&row.SettledCount, today.SettledCount}, {&row.UnresolvedCount, today.UnresolvedCount},
		} {
			if err := channelMonitorAddNonNegativeInt64(value.target, value.amount); err != nil {
				return nil, false, err
			}
		}
		if today.APIKeyName != "" {
			row.APIKeyName = today.APIKeyName
		}
		byKey[key] = row
	}
	rows = make([]model.ChannelDailyAPIKeyCost, 0, len(byKey))
	for _, row := range byKey {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CostNanoCNY != rows[j].CostNanoCNY {
			return rows[i].CostNanoCNY > rows[j].CostNanoCNY
		}
		return channelMonitorCostKeyIdentity(rows[i]) < channelMonitorCostKeyIdentity(rows[j])
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
		truncated = true
	}
	return rows, truncated, nil
}

func (source channelMonitorCostReadSource) attach(overview *channelMonitorCostOverview) {
	if source.current == nil {
		overview.CostSource = "database_daily"
		return
	}
	overview.CostSource = "redis_daily"
	overview.CostRevision = source.current.Revision
	overview.CostProjection = source.current.Projection
	overview.DataCutoffAt = source.current.DataCutoffAt
	overview.ProcessedAt = source.current.ProcessedAt
}
