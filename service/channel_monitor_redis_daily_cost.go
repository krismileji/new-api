package service

import (
	"context"
)

// QueryChannelMonitorRedisDailyCosts reads only the current day's cost hash.
// It deliberately avoids the generic minute-window projection query.
func QueryChannelMonitorRedisDailyCosts(
	ctx context.Context,
	dayStart int64,
) (ChannelMonitorRedisSharedDailyCostView, error) {
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
	key := ChannelMonitorRedisCostDayKey(dayStart)
	opCtx, cancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
	values, err := projection.client.HGetAll(opCtx, key).Result()
	cancel()
	if err != nil {
		return ChannelMonitorRedisSharedDailyCostView{}, err
	}
	if len(values) == 0 {
		existsCtx, existsCancel := context.WithTimeout(ctx, channelMonitorRedisSharedOperationTimeout)
		exists, existsErr := projection.client.Exists(existsCtx, key).Result()
		existsCancel()
		if existsErr != nil {
			return ChannelMonitorRedisSharedDailyCostView{}, existsErr
		}
		if exists == 0 {
			return ChannelMonitorRedisSharedDailyCostView{}, ErrChannelMonitorRedisSharedProjectionUnavailable
		}
		return ChannelMonitorRedisSharedDailyCostView{Channels: make(map[int]ChannelMonitorRedisSharedAggregate)}, nil
	}
	limits := normalizeChannelMonitorRedisSharedProjectionLimits(projection.limits)
	if len(values) > limits.MaxHashFields*2 {
		return ChannelMonitorRedisSharedDailyCostView{}, &ChannelMonitorRedisSharedProjectionLimitError{
			Resource: "hash_fields", Limit: int64(limits.MaxHashFields), Actual: int64(len(values) / 2),
		}
	}
	view := ChannelMonitorRedisSharedDailyCostView{
		Channels: make(map[int]ChannelMonitorRedisSharedAggregate),
		Models:   make(map[string]ChannelMonitorRedisSharedAggregate),
		Groups:   make(map[string]ChannelMonitorRedisSharedAggregate),
		APIKeys:  make(map[int]ChannelMonitorRedisSharedAggregate),
	}
	entries := make(map[string]map[string]string)
	for field, raw := range values {
		parts := splitChannelMonitorRedisDailyField(field)
		if len(parts) != 3 {
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
