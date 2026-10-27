package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
)

var requestStatsLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type RequestStatsQuery struct {
	Date      string `form:"date"`
	Level     string `form:"level"`
	UserID    int    `form:"user_id"`
	APIKeyID  int    `form:"api_key_id"`
	Search    string `form:"search"`
	Sort      string `form:"sort"`
	Direction string `form:"direction"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}

func (query *RequestStatsQuery) Validate(now time.Time) (int64, error) {
	if query.Date == "" {
		query.Date = now.In(requestStatsLocation).Format(time.DateOnly)
	}
	day, err := time.ParseInLocation(time.DateOnly, query.Date, requestStatsLocation)
	if err != nil || day.Unix() < 0 || day.After(now) {
		return 0, fmt.Errorf("统计日期无效或晚于今天")
	}
	if query.Level == "" {
		query.Level = "user"
	}
	if query.Level != "user" && query.Level != "api_key" && query.Level != "channel" {
		return 0, fmt.Errorf("统计层级无效")
	}
	if query.UserID < 0 || query.APIKeyID < 0 || query.Level == "user" && (query.UserID != 0 || query.APIKeyID != 0) || query.Level == "api_key" && query.APIKeyID != 0 {
		return 0, fmt.Errorf("统计父级筛选无效")
	}
	if query.Sort == "" {
		query.Sort = "low_input_rate"
	}
	if query.Sort != "low_input_rate" && query.Sort != "request_count" && query.Sort != "low_input_auto_disabled_count" {
		return 0, fmt.Errorf("排序字段无效")
	}
	if query.Direction == "" {
		query.Direction = "desc"
	}
	if query.Direction != "asc" && query.Direction != "desc" {
		return 0, fmt.Errorf("排序方向无效")
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	if query.Page < 1 || query.Page > 1000000 || query.PageSize < 1 || query.PageSize > 100 || len(query.Search) > 255 {
		return 0, fmt.Errorf("分页或搜索参数无效")
	}
	return day.Unix(), nil
}

type RequestStatsRow struct {
	ID                             int      `json:"id"`
	Name                           string   `json:"name"`
	UserID                         int      `json:"user_id"`
	APIKeyID                       int      `json:"api_key_id"`
	RequestCount                   int64    `json:"request_count"`
	StreamRequestCount             int64    `json:"stream_request_count"`
	NonStreamRequestCount          int64    `json:"non_stream_request_count"`
	UnclassifiedStreamRequestCount int64    `json:"unclassified_stream_request_count"`
	KnownInputRequestCount         int64    `json:"known_input_request_count"`
	UnknownInputRequestCount       int64    `json:"unknown_input_request_count"`
	LowInputCount                  int64    `json:"low_input_count"`
	LowInputRate                   *float64 `json:"low_input_rate"`
	LowInputAutoDisabledCount      int64    `json:"low_input_auto_disabled_count"`
}

type RequestStatsResult struct {
	Date               string            `json:"date"`
	Level              string            `json:"level"`
	LowInputThresholdK int               `json:"low_input_threshold_k"`
	Revision           int64             `json:"revision"`
	StartedAt          int64             `json:"started_at"`
	Source             string            `json:"source"`
	CoveragePartial    bool              `json:"coverage_partial"`
	DataCutoffAt       int64             `json:"data_cutoff_at"`
	ProcessedAt        int64             `json:"processed_at"`
	Summary            RequestStatsRow   `json:"summary"`
	Items              []RequestStatsRow `json:"items"`
	Total              int               `json:"total"`
	Page               int               `json:"page"`
	PageSize           int               `json:"page_size"`
}

func QueryChannelMonitorRequestStats(ctx context.Context, query RequestStatsQuery) (RequestStatsResult, error) {
	day, err := query.Validate(time.Now())
	if err != nil {
		return RequestStatsResult{}, err
	}
	config, err := model.GetChannelMonitorRequestStatsConfig(ctx)
	if err != nil {
		return RequestStatsResult{}, err
	}
	view, err := QueryChannelMonitorRedisDailySuccess(ctx, day, []string{"fact:*", "meta:*"})
	if err != nil {
		return RequestStatsResult{}, err
	}
	rows := make(map[int]*RequestStatsRow)
	for _, entry := range view.Entries {
		if entry.Scope != "fact" {
			continue
		}
		identity, err := channelMonitorDailyMetricIdentity(entry.Identity)
		if err != nil {
			return RequestStatsResult{}, err
		}
		if query.Level != "user" && identity.UserID != query.UserID || query.Level == "channel" && identity.APIKeyID != query.APIKeyID {
			continue
		}
		id := identity.UserID
		if query.Level == "api_key" {
			id = identity.APIKeyID
		}
		if query.Level == "channel" {
			id = identity.ChannelID
		}
		row := rows[id]
		if row == nil {
			row = &RequestStatsRow{ID: id, UserID: query.UserID, APIKeyID: query.APIKeyID}
			if query.Level == "user" {
				row.UserID = id
			}
			if query.Level == "api_key" {
				row.APIKeyID = id
				row.Name = entry.Aggregate.APIKeyName
			}
			rows[id] = row
		}
		distribution := entry.Aggregate.RequestStats
		if err := validateRequestStatsDistribution(distribution); err != nil {
			return RequestStatsResult{}, err
		}
		actual, err := channelMonitorRedisSharedCheckedAddInt64(entry.Aggregate.ActualSuccessCount, entry.Aggregate.ActualFailureCount)
		if err != nil {
			return RequestStatsResult{}, err
		}
		total := max(distribution["total"], actual)
		if err := row.merge(RequestStatsRow{RequestCount: total, StreamRequestCount: distribution["stream"],
			NonStreamRequestCount: distribution["total"] - distribution["stream"], UnclassifiedStreamRequestCount: total - distribution["total"],
			KnownInputRequestCount: distribution["known"], UnknownInputRequestCount: total - distribution["known"],
			LowInputCount: requestStatsLowCount(distribution, config.LowInputThresholdK)}); err != nil {
			return RequestStatsResult{}, err
		}
	}
	if err := hydrateRequestStatsRows(ctx, query.Level, rows); err != nil {
		return RequestStatsResult{}, err
	}
	result := RequestStatsResult{Date: query.Date, Level: query.Level, LowInputThresholdK: config.LowInputThresholdK, Revision: config.Revision, StartedAt: config.StartedAt,
		Source: view.Source, CoveragePartial: view.CoveragePartial || config.StartedAt > day, DataCutoffAt: view.DataCutoffAt, ProcessedAt: view.ProcessedAt,
		Items: []RequestStatsRow{}, Page: query.Page, PageSize: query.PageSize}
	filtered := make([]RequestStatsRow, 0, len(rows))
	search := strings.ToLower(strings.TrimSpace(query.Search))
	for _, row := range rows {
		if search != "" && !strings.Contains(strings.ToLower(row.Name), search) && !strings.Contains(strconv.Itoa(row.ID), search) {
			continue
		}
		if row.KnownInputRequestCount > 0 {
			rate := float64(row.LowInputCount) / float64(row.KnownInputRequestCount) * 100
			row.LowInputRate = &rate
		}
		if row.UnclassifiedStreamRequestCount > 0 {
			result.CoveragePartial = true
		}
		filtered = append(filtered, *row)
		if err := result.Summary.merge(*row); err != nil {
			return RequestStatsResult{}, err
		}
	}
	if result.Summary.KnownInputRequestCount > 0 {
		rate := float64(result.Summary.LowInputCount) / float64(result.Summary.KnownInputRequestCount) * 100
		result.Summary.LowInputRate = &rate
	}
	slices.SortFunc(filtered, func(a, b RequestStatsRow) int {
		if query.Sort == "low_input_rate" {
			if a.LowInputRate == nil && b.LowInputRate != nil {
				return 1
			}
			if a.LowInputRate != nil && b.LowInputRate == nil {
				return -1
			}
		}
		comparison := 0
		switch query.Sort {
		case "request_count":
			comparison = cmp.Compare(a.RequestCount, b.RequestCount)
		case "low_input_auto_disabled_count":
			comparison = cmp.Compare(a.LowInputAutoDisabledCount, b.LowInputAutoDisabledCount)
		default:
			if a.LowInputRate != nil && b.LowInputRate != nil {
				if *a.LowInputRate < *b.LowInputRate {
					comparison = -1
				} else if *a.LowInputRate > *b.LowInputRate {
					comparison = 1
				}
			}
		}
		if query.Direction == "desc" {
			comparison = -comparison
		}
		if comparison != 0 {
			return comparison
		}
		if comparison = cmp.Compare(b.RequestCount, a.RequestCount); comparison != 0 {
			return comparison
		}
		return cmp.Compare(a.ID, b.ID)
	})
	result.Total = len(filtered)
	start := min((query.Page-1)*query.PageSize, len(filtered))
	result.Items = append(result.Items, filtered[start:min(start+query.PageSize, len(filtered))]...)
	return result, nil
}

func (row *RequestStatsRow) merge(source RequestStatsRow) error {
	for _, counter := range []struct {
		target *int64
		value  int64
	}{
		{&row.RequestCount, source.RequestCount}, {&row.StreamRequestCount, source.StreamRequestCount},
		{&row.NonStreamRequestCount, source.NonStreamRequestCount}, {&row.UnclassifiedStreamRequestCount, source.UnclassifiedStreamRequestCount},
		{&row.KnownInputRequestCount, source.KnownInputRequestCount}, {&row.UnknownInputRequestCount, source.UnknownInputRequestCount},
		{&row.LowInputCount, source.LowInputCount}, {&row.LowInputAutoDisabledCount, source.LowInputAutoDisabledCount},
	} {
		value, err := channelMonitorRedisSharedCheckedAddInt64(*counter.target, counter.value)
		if err != nil {
			return err
		}
		*counter.target = value
	}
	return nil
}

func hydrateRequestStatsRows(ctx context.Context, level string, rows map[int]*RequestStatsRow) error {
	ids := make([]int, 0, len(rows))
	for id, row := range rows {
		if row.Name == "" {
			row.Name = "#" + strconv.Itoa(id)
		}
		if id == 0 {
			row.Name = "未知归属"
		}
		ids = append(ids, id)
	}
	// Keep queries within SQLite/PostgreSQL bind-parameter limits.
	for batch := range slices.Chunk(ids, 500) {
		switch level {
		case "user":
			var users []struct {
				Id          int
				Username    string
				DisplayName string
			}
			if err := model.DB.WithContext(ctx).Model(&model.User{}).Select("id", "username", "display_name").Where("id IN ?", batch).Find(&users).Error; err != nil {
				return err
			}
			for _, user := range users {
				rows[user.Id].Name = user.Username
				if user.DisplayName != "" {
					rows[user.Id].Name += " · " + user.DisplayName
				}
			}
		case "api_key":
			var keys []struct {
				Id   int
				Name string
			}
			if err := model.DB.WithContext(ctx).Model(&model.Token{}).Select("id", "name").Where("id IN ?", batch).Find(&keys).Error; err != nil {
				return err
			}
			for _, key := range keys {
				if key.Name != "" {
					rows[key.Id].Name = key.Name
				}
			}
		case "channel":
			var channels []struct {
				Id   int
				Name string
			}
			if err := model.DB.WithContext(ctx).Model(&model.Channel{}).Select("id", "name").Where("id IN ?", batch).Find(&channels).Error; err != nil {
				return err
			}
			for _, channel := range channels {
				if channel.Name != "" {
					rows[channel.Id].Name = channel.Name
				}
			}
		}
		if level == "channel" {
			continue
		}
		column := "user_id"
		if level == "api_key" {
			column = "api_key_id"
		}
		var counts []struct {
			Identity int
			Total    int64
		}
		if err := model.DB.WithContext(ctx).Model(&model.ChannelMonitorRequestStatsDisable{}).Select(column+" AS identity, COUNT(*) AS total").Where(column+" IN ?", batch).Group(column).Scan(&counts).Error; err != nil {
			return err
		}
		for _, count := range counts {
			rows[count.Identity].LowInputAutoDisabledCount = count.Total
		}
	}
	return nil
}
