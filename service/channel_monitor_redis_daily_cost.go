package service

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

// QueryChannelMonitorRedisDailyCosts reads only the current day's cost hash.
// It deliberately avoids the generic minute-window projection query.
func QueryChannelMonitorRedisDailyCosts(
	ctx context.Context,
	dayStart int64,
) (ChannelMonitorRedisSharedDailyCostView, error) {
	return queryChannelMonitorRedisDailyCosts(ctx, dayStart, false)
}

// QueryChannelMonitorRedisDailyCostTotals keeps frequent page refreshes from
// transferring every user/key/model detail when only channel totals are used.
func QueryChannelMonitorRedisDailyCostTotals(ctx context.Context, dayStart int64) (ChannelMonitorRedisSharedDailyCostView, error) {
	return queryChannelMonitorRedisDailyCosts(ctx, dayStart, true)
}

func queryChannelMonitorRedisDailyCosts(ctx context.Context, dayStart int64, summaryOnly bool) (ChannelMonitorRedisSharedDailyCostView, error) {
	projection, err := NewChannelMonitorRedisSharedProjection()
	if err != nil {
		return ChannelMonitorRedisSharedDailyCostView{}, err
	}
	if projection == nil || projection.client == nil {
		return ChannelMonitorRedisSharedDailyCostView{}, ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limits := normalizeChannelMonitorRedisSharedProjectionLimits(projection.limits)
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	defer cancel()
	patterns := []string{"*"}
	if summaryOnly {
		patterns = []string{"meta:*", "global:*", "channel:*"}
	}
	values, err := readChannelMonitorRedisDailyHash(opCtx, projection.client, ChannelMonitorRedisCostDayKey(dayStart), patterns, limits.MaxHashFields)
	if err != nil {
		return ChannelMonitorRedisSharedDailyCostView{}, err
	}
	if values[channelMonitorReliableCostVersionField] != "1" {
		return ChannelMonitorRedisSharedDailyCostView{}, ErrChannelMonitorRedisSharedProjectionUnavailable
	}
	view := ChannelMonitorRedisSharedDailyCostView{
		DayStart:     dayStart,
		Revision:     parseDailySuccessInt64(values["meta:revision"]),
		ProcessedAt:  parseDailySuccessInt64(values["meta:processed_at"]),
		DataCutoffAt: parseDailySuccessInt64(values["meta:data_cutoff_at"]),
		Channels:     make(map[int]ChannelMonitorRedisSharedAggregate),
		Models:       make(map[string]ChannelMonitorRedisSharedAggregate),
		Groups:       make(map[string]ChannelMonitorRedisSharedAggregate),
		APIKeys:      make(map[int]ChannelMonitorRedisSharedAggregate),
	}
	status, err := projection.client.Get(opCtx, channelMonitorReliableCostStatusKey).Bytes()
	if err != nil && !errors.Is(err, redis.Nil) {
		return ChannelMonitorRedisSharedDailyCostView{}, err
	}
	if len(status) > 0 {
		if err := common.Unmarshal(status, &view.Projection); err != nil {
			return ChannelMonitorRedisSharedDailyCostView{}, err
		}
	}
	entries := make(map[string]map[string]string)
	for field, raw := range values {
		parts := splitChannelMonitorRedisDailyField(field)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == channelMonitorRedisSharedScopeMetadata {
			continue
		}
		entryKey := parts[0] + "\x00" + parts[1]
		entry := entries[entryKey]
		if entry == nil {
			entry = make(map[string]string)
			entries[entryKey] = entry
		}
		entry[parts[2]] = raw
	}
	for entryKey, fields := range entries {
		parts := splitChannelMonitorRedisDailyEntryKey(entryKey)
		if len(parts) != 2 {
			continue
		}
		aggregate := ChannelMonitorRedisSharedAggregate{}
		for metric, raw := range fields {
			if err := addChannelMonitorRedisAggregateField(&aggregate, metric, raw); err != nil {
				return ChannelMonitorRedisSharedDailyCostView{}, err
			}
		}
		switch parts[0] {
		case "cost_detail", "cost_key":
			encoded, err := base64.RawURLEncoding.DecodeString(parts[1])
			if err != nil {
				return ChannelMonitorRedisSharedDailyCostView{}, err
			}
			var id channelMonitorReliableCostIdentity
			if err := common.Unmarshal(encoded, &id); err != nil {
				return ChannelMonitorRedisSharedDailyCostView{}, err
			}
			if parts[0] == "cost_key" {
				view.KeyCosts = append(view.KeyCosts, model.ChannelDailyAPIKeyCost{
					DayStart: dayStart, ChannelId: id.ChannelID, APIKeyId: id.APIKeyID, APIKeyName: aggregate.APIKeyName,
					KeyFingerprint: id.Fingerprint, CostNanoCNY: aggregate.SettledCostNanoCNY,
					KeyDisplay:   fields["key_display"],
					SettledCount: aggregate.SettledRequestCount, UnresolvedCount: aggregate.UnresolvedRequestCount,
				})
				continue
			}
			attribution := "unknown"
			if id.UserID > 0 {
				attribution = "request"
			}
			view.Details = append(view.Details, model.ChannelMonitorDailyCostDetail{
				DayStart: dayStart, ChannelId: id.ChannelID, UserId: id.UserID, UserAttribution: attribution,
				APIKeyId: id.APIKeyID, APIKeyKey: id.Fingerprint, APIKeyName: aggregate.APIKeyName,
				ModelKey: model.ChannelMonitorDailyCostModelKey(id.Model), ModelName: id.Model, SourceKind: id.Source,
				CostNanoCNY: aggregate.SettledCostNanoCNY, ProbeCostNanoCNY: aggregate.ProbeSettledCostNanoCNY,
				GroupProbeCostNanoCNY: aggregate.GroupProbeSettledCostNanoCNY,
				SettledCount:          aggregate.SettledRequestCount, UnresolvedCount: aggregate.UnresolvedRequestCount,
			})
		case channelMonitorRedisSharedScopeChannel:
			channelID := parseDailySuccessPositiveInt(parts[1])
			if channelID > 0 {
				view.Channels[channelID] = aggregate
			}
		case channelMonitorRedisSharedScopeModel:
			if modelName, ok := decodeDailySuccessDimension(parts[1]); ok {
				view.Models[modelName] = aggregate
			}
		case channelMonitorRedisSharedScopeGroup:
			if group, ok := decodeDailySuccessDimension(parts[1]); ok {
				view.Groups[group] = aggregate
			}
		case channelMonitorRedisSharedScopeAPIKey:
			apiKeyID := parseDailySuccessPositiveInt(parts[1])
			if apiKeyID > 0 {
				view.APIKeys[apiKeyID] = aggregate
			}
		case channelMonitorRedisSharedScopeGlobal:
			view.Global = aggregate
		}
	}
	if summaryOnly {
		return view, nil
	}
	// Older daily ledgers may contain costs without frozen user/model detail.
	// Preserve those amounts as an explicit unknown bucket in drill-downs.
	attributed := make(map[int]ChannelMonitorRedisSharedAggregate)
	for _, row := range view.Details {
		total := attributed[row.ChannelId]
		if err := mergeChannelMonitorRedisSharedAggregate(&total, channelMonitorRedisDailyCostDetailAggregate(row)); err != nil {
			return ChannelMonitorRedisSharedDailyCostView{}, err
		}
		attributed[row.ChannelId] = total
	}
	for id, total := range view.Channels {
		known := attributed[id]
		if total.SettledCostNanoCNY < known.SettledCostNanoCNY || total.SettledRequestCount < known.SettledRequestCount || total.UnresolvedRequestCount < known.UnresolvedRequestCount || total.ProbeSettledCostNanoCNY < known.ProbeSettledCostNanoCNY || total.GroupProbeSettledCostNanoCNY < known.GroupProbeSettledCostNanoCNY {
			return ChannelMonitorRedisSharedDailyCostView{}, errors.New("渠道日成本明细超过渠道汇总，等待重建")
		}
		remaining := model.ChannelMonitorDailyCostDetail{
			DayStart: dayStart, ChannelId: id, UserAttribution: "unknown", SourceKind: "unknown",
			CostNanoCNY:           total.SettledCostNanoCNY - known.SettledCostNanoCNY,
			ProbeCostNanoCNY:      total.ProbeSettledCostNanoCNY - known.ProbeSettledCostNanoCNY,
			GroupProbeCostNanoCNY: total.GroupProbeSettledCostNanoCNY - known.GroupProbeSettledCostNanoCNY,
			SettledCount:          total.SettledRequestCount - known.SettledRequestCount,
			UnresolvedCount:       total.UnresolvedRequestCount - known.UnresolvedRequestCount,
		}
		if remaining.CostNanoCNY != 0 || remaining.SettledCount != 0 || remaining.UnresolvedCount != 0 {
			view.AttributionPartial = true
			view.Details = append(view.Details, remaining)
		}
	}
	return view, nil
}

func splitChannelMonitorRedisDailyField(field string) []string {
	last := -1
	for index := len(field) - 1; index >= 0; index-- {
		if field[index] == ':' {
			last = index
			break
		}
	}
	if last <= 0 || last >= len(field)-1 {
		return nil
	}
	prefix := field[:last]
	separator := -1
	for index := range prefix {
		if prefix[index] == ':' {
			separator = index
			break
		}
	}
	if separator < 0 {
		return []string{prefix, "", field[last+1:]}
	}
	if separator == 0 || separator >= len(prefix)-1 {
		return nil
	}
	return []string{prefix[:separator], prefix[separator+1:], field[last+1:]}
}

func splitChannelMonitorRedisDailyEntryKey(value string) []string {
	for index := range value {
		if value[index] == '\x00' {
			return []string{value[:index], value[index+1:]}
		}
	}
	return nil
}
