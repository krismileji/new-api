package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

// ChannelMonitorRedisDailySuccessAnalyticsRow is one dimension from the
// dedicated current-day hash. The row is already aggregated by the Stream
// consumer; callers only filter and page these bounded rows.
type ChannelMonitorRedisDailySuccessAnalyticsRow struct {
	ChannelID       int
	UserID          int
	UserAttribution string
	APIKeyID        int
	APIKeyKey       string
	APIKeyName      string
	ModelKey        string
	ModelName       string
	Aggregate       ChannelMonitorRedisSharedAggregate
}

type ChannelMonitorRedisDailySuccessAnalyticsView struct {
	DayStart       int64
	Summary        ChannelMonitorRedisSharedAggregate
	Rows           []ChannelMonitorRedisDailySuccessAnalyticsRow
	DataCutoffAt   int64
	ProcessedAt    int64
	EventWatermark uint64
}

// QueryChannelMonitorRedisDailySuccessAnalytics reads one current-day hash.
// It does not inspect minute keys or raw request logs.
func QueryChannelMonitorRedisDailySuccessAnalytics(
	ctx context.Context,
	dayStart int64,
) (ChannelMonitorRedisDailySuccessAnalyticsView, error) {
	daily, err := QueryChannelMonitorRedisDailySuccess(ctx, dayStart, []string{"*"})
	if err != nil {
		return ChannelMonitorRedisDailySuccessAnalyticsView{}, err
	}
	return channelMonitorRedisDailySuccessAnalyticsFromView(daily)
}

func queryChannelMonitorRedisDailySuccessAnalyticsWithClient(
	ctx context.Context,
	client *redis.Client,
	dayStart int64,
) (ChannelMonitorRedisDailySuccessAnalyticsView, error) {
	daily, err := queryChannelMonitorRedisDailySuccessWithClient(ctx, client, dayStart, []string{"*"})
	if err != nil {
		return ChannelMonitorRedisDailySuccessAnalyticsView{}, err
	}
	return channelMonitorRedisDailySuccessAnalyticsFromView(daily)
}

func channelMonitorRedisDailySuccessAnalyticsFromView(
	daily ChannelMonitorRedisDailySuccessView,
) (ChannelMonitorRedisDailySuccessAnalyticsView, error) {
	view := ChannelMonitorRedisDailySuccessAnalyticsView{
		DayStart:       daily.DayStart,
		Rows:           make([]ChannelMonitorRedisDailySuccessAnalyticsRow, 0),
		DataCutoffAt:   daily.DataCutoffAt,
		ProcessedAt:    daily.ProcessedAt,
		EventWatermark: daily.EventWatermark,
	}
	rows := make(map[string]*ChannelMonitorRedisDailySuccessAnalyticsRow)
	userByChannelKey := make(map[string]int)

	for _, entry := range daily.Entries {
		row := ChannelMonitorRedisDailySuccessAnalyticsRow{Aggregate: entry.Aggregate}
		switch entry.Scope {
		case channelMonitorRedisSharedScopeGlobal:
			view.Summary = entry.Aggregate
			continue
		case channelMonitorRedisSharedScopeChannel:
			row.ChannelID = parseDailySuccessPositiveInt(entry.Identity)
		case "user":
			row.UserID = parseDailySuccessPositiveInt(entry.Identity)
			row.UserAttribution = string(model.ChannelMonitorEventUserAttributionRequest)
		case "channel_user":
			parts := strings.Split(entry.Identity, ".")
			if len(parts) != 2 {
				continue
			}
			row.ChannelID = parseDailySuccessPositiveInt(parts[0])
			row.UserID = parseDailySuccessPositiveInt(parts[1])
			row.UserAttribution = string(model.ChannelMonitorEventUserAttributionRequest)
		case channelMonitorRedisSharedScopeAPIKey:
			row.APIKeyID = parseDailySuccessPositiveInt(entry.Identity)
			row.APIKeyName = entry.Aggregate.APIKeyName
		case "user_api_key":
			parts := strings.Split(entry.Identity, ".")
			if len(parts) != 2 {
				continue
			}
			row.UserID = parseDailySuccessPositiveInt(parts[0])
			row.APIKeyID = parseDailySuccessPositiveInt(parts[1])
			row.APIKeyName = entry.Aggregate.APIKeyName
			row.UserAttribution = string(model.ChannelMonitorEventUserAttributionRequest)
		case "channel_user_api_key":
			parts := strings.Split(entry.Identity, ".")
			if len(parts) != 3 {
				continue
			}
			row.ChannelID = parseDailySuccessPositiveInt(parts[0])
			row.UserID = parseDailySuccessPositiveInt(parts[1])
			row.APIKeyID = parseDailySuccessPositiveInt(parts[2])
			row.APIKeyName = entry.Aggregate.APIKeyName
			row.UserAttribution = string(model.ChannelMonitorEventUserAttributionRequest)
		case "user_api_key_route":
			parts := strings.Split(entry.Identity, ".")
			if len(parts) != 4 {
				continue
			}
			modelName, ok := decodeDailySuccessDimension(parts[3])
			if !ok {
				continue
			}
			row.UserID = parseDailySuccessPositiveInt(parts[0])
			row.APIKeyID = parseDailySuccessPositiveInt(parts[1])
			row.ChannelID = parseDailySuccessPositiveInt(parts[2])
			row.ModelName, row.ModelKey = modelName, parts[3]
			row.APIKeyName = entry.Aggregate.APIKeyName
			row.UserAttribution = string(model.ChannelMonitorEventUserAttributionRequest)
		case channelMonitorRedisSharedScopeModel:
			modelName, ok := decodeDailySuccessDimension(entry.Identity)
			if !ok {
				continue
			}
			row.ModelName, row.ModelKey = modelName, entry.Identity
		case channelMonitorRedisSharedScopeRoute:
			parts := strings.SplitN(entry.Identity, ".", 2)
			if len(parts) != 2 {
				continue
			}
			modelName, ok := decodeDailySuccessDimension(parts[1])
			if !ok {
				continue
			}
			row.ChannelID = parseDailySuccessPositiveInt(parts[0])
			row.ModelName, row.ModelKey = modelName, parts[1]
		case channelMonitorRedisSharedScopeAPIKeyRoute:
			parts := strings.Split(entry.Identity, ".")
			if len(parts) != 4 {
				continue
			}
			modelName, ok := decodeDailySuccessDimension(parts[2])
			if !ok {
				continue
			}
			row.APIKeyID = parseDailySuccessPositiveInt(parts[0])
			row.ChannelID = parseDailySuccessPositiveInt(parts[1])
			row.ModelName, row.ModelKey = modelName, parts[2]
			row.APIKeyName = entry.Aggregate.APIKeyName
		default:
			continue
		}
		if row.ChannelID <= 0 && row.UserID <= 0 && row.APIKeyID <= 0 && row.ModelName == "" {
			continue
		}
		if row.APIKeyID > 0 && row.APIKeyName == "" {
			row.APIKeyName = entry.Aggregate.APIKeyName
		}
		key := dailySuccessAnalyticsRowKey(row)
		if row.ChannelID > 0 && row.UserID > 0 && row.APIKeyID > 0 {
			ownerKey := strconv.Itoa(row.ChannelID) + "." + strconv.Itoa(row.APIKeyID)
			if previous, exists := userByChannelKey[ownerKey]; exists && previous != row.UserID {
				userByChannelKey[ownerKey] = 0
			} else {
				userByChannelKey[ownerKey] = row.UserID
			}
		}
		if existing := rows[key]; existing != nil {
			if err := mergeChannelMonitorRedisSharedAggregate(&existing.Aggregate, row.Aggregate); err != nil {
				return ChannelMonitorRedisDailySuccessAnalyticsView{}, err
			}
			if existing.APIKeyName == "" {
				existing.APIKeyName = row.APIKeyName
			}
			continue
		}
		copyRow := row
		rows[key] = &copyRow
	}

	for _, row := range rows {
		if row.APIKeyID > 0 && row.ChannelID > 0 && row.UserID == 0 {
			owner := userByChannelKey[strconv.Itoa(row.ChannelID)+"."+strconv.Itoa(row.APIKeyID)]
			if owner > 0 {
				row.UserID = owner
				row.UserAttribution = string(model.ChannelMonitorEventUserAttributionRequest)
			}
		}
		view.Rows = append(view.Rows, *row)
	}
	return view, nil
}

func parseDailySuccessPositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func decodeDailySuccessDimension(value string) (string, bool) {
	decoded, err := channelMonitorRedisSharedDimensionDecode(value)
	return decoded, err == nil
}

func dailySuccessAnalyticsRowKey(row ChannelMonitorRedisDailySuccessAnalyticsRow) string {
	return strconv.Itoa(row.ChannelID) + ":" + strconv.Itoa(row.UserID) + ":" +
		strconv.Itoa(row.APIKeyID) + ":" + row.APIKeyKey + ":" + row.ModelKey + ":" + row.ModelName
}
