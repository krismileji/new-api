package controller

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type channelMonitorAnalyticsQuery struct {
	Metric    string
	GroupBy   string
	From      int64
	To        int64
	Channel   int
	User      int
	APIKey    int
	Model     string
	Search    string
	Sort      string
	Direction string
	Page      int
	PageSize  int
}

type channelMonitorAnalyticsResponse struct {
	Source       string                         `json:"source"`
	GroupBy      string                         `json:"group_by"`
	Coverage     service.ChannelMonitorCoverage `json:"coverage"`
	Summary      map[string]any                 `json:"summary"`
	ScopeSummary map[string]any                 `json:"scope_summary"`
	Items        []map[string]any               `json:"items"`
	Page         int                            `json:"page"`
	PageSize     int                            `json:"page_size"`
	Total        int64                          `json:"total"`
	GeneratedAt  int64                          `json:"generated_at"`
}

func GetChannelMonitorAnalyticsSummary(c *gin.Context) {
	query, err := parseChannelMonitorAnalyticsQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if query.From == 0 {
		query.From = model.ChannelDailyCostDayStart(common.GetTimestamp()) - 29*24*60*60
	}
	if query.To == 0 {
		query.To = model.ChannelDailyCostDayStart(common.GetTimestamp()) + 24*60*60
	}
	response, err := queryChannelMonitorHistoricalAnalytics(c.Request.Context(), query)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func GetChannelMonitorAnalyticsTrend(c *gin.Context) {
	query, err := parseChannelMonitorAnalyticsQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	query.GroupBy = "day"
	response, err := queryChannelMonitorHistoricalAnalytics(c.Request.Context(), query)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func GetChannelMonitorAnalyticsRows(c *gin.Context) {
	GetChannelMonitorAnalyticsSummary(c)
}

func GetChannelMonitorAnalyticsOptions(c *gin.Context) {
	channels, err := model.GetAllChannelsForMonitorWithContext(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]map[string]any, 0, len(channels))
	for _, channel := range channels {
		items = append(items, map[string]any{"id": channel.Id, "name": channel.Name})
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

// RunChannelMonitorAnalyticsBackfill runs a bounded, idempotent historical
// cost projection rebuild. It only fills the drill-down table; authoritative
// daily ledgers are never replayed.
func RunChannelMonitorAnalyticsBackfill(c *gin.Context) {
	batchID := strings.TrimSpace(c.Query("batch_id"))
	from, err := channelMonitorAnalyticsTimestamp(c.Query("from"))
	if err != nil || from == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "from 必须为 YYYY-MM-DD"})
		return
	}
	to, err := channelMonitorAnalyticsTimestamp(c.Query("to"))
	if err != nil || to == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "to 必须为 YYYY-MM-DD 且为结束日期"})
		return
	}
	maxDays := 0
	if raw := strings.TrimSpace(c.Query("max_days")); raw != "" {
		maxDays, err = strconv.Atoi(raw)
		if err != nil || maxDays < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "max_days 必须为正整数"})
			return
		}
	}
	result, err := model.BackfillChannelMonitorCostDetails(c.Request.Context(), batchID, from, to, maxDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func parseChannelMonitorAnalyticsQuery(c *gin.Context) (channelMonitorAnalyticsQuery, error) {
	query := channelMonitorAnalyticsQuery{
		Metric:    strings.TrimSpace(c.DefaultQuery("metric", "success")),
		GroupBy:   strings.TrimSpace(c.DefaultQuery("group_by", "channel")),
		Sort:      strings.TrimSpace(c.DefaultQuery("sort", "samples")),
		Direction: strings.TrimSpace(c.DefaultQuery("direction", "desc")),
		Search:    strings.TrimSpace(c.Query("search")),
		Page:      1,
		PageSize:  50,
	}
	if query.Metric != "success" && query.Metric != "cost" {
		return query, &channelMonitorAnalyticsQueryError{"metric 必须为 success 或 cost"}
	}
	if !channelMonitorAnalyticsGroupByAllowed(query.GroupBy) {
		return query, &channelMonitorAnalyticsQueryError{"group_by 参数不受支持"}
	}
	if query.Direction != "asc" && query.Direction != "desc" {
		return query, &channelMonitorAnalyticsQueryError{"direction 必须为 asc 或 desc"}
	}
	if query.Metric == "cost" && query.GroupBy != "day" && query.GroupBy != "channel" && query.GroupBy != "user" && query.GroupBy != "api_key" && query.GroupBy != "api_key_channel_model" {
		return query, &channelMonitorAnalyticsQueryError{"成本指标目前只支持 day、channel、user、api_key、api_key_channel_model 分组"}
	}
	var err error
	query.From, err = channelMonitorAnalyticsTimestamp(c.Query("from"))
	if err != nil {
		return query, err
	}
	query.To, err = channelMonitorAnalyticsTimestamp(c.Query("to"))
	if err != nil {
		return query, err
	}
	query.Channel, err = channelMonitorAnalyticsPositiveInt(c.Query("channel_id"))
	if err != nil {
		return query, err
	}
	query.User, err = channelMonitorAnalyticsPositiveInt(c.Query("user_id"))
	if err != nil {
		return query, err
	}
	query.APIKey, err = channelMonitorAnalyticsPositiveInt(c.Query("api_key_id"))
	if err != nil {
		return query, err
	}
	query.Model = strings.TrimSpace(c.Query("model"))
	if raw := c.Query("page"); raw != "" {
		query.Page, err = strconv.Atoi(raw)
		if err != nil || query.Page < 1 {
			return query, &channelMonitorAnalyticsQueryError{"page 必须为正整数"}
		}
	}
	if raw := c.Query("page_size"); raw != "" {
		query.PageSize, err = strconv.Atoi(raw)
		if err != nil || query.PageSize < 1 || query.PageSize > 200 {
			return query, &channelMonitorAnalyticsQueryError{"page_size 必须在 1 到 200 之间"}
		}
	}
	return query, nil
}

func channelMonitorAnalyticsGroupByAllowed(groupBy string) bool {
	switch groupBy {
	case "day", "channel", "user", "api_key", "model", "channel_model", "api_key_channel_model":
		return true
	default:
		return false
	}
}

type channelMonitorAnalyticsQueryError struct{ message string }

func (err *channelMonitorAnalyticsQueryError) Error() string { return err.message }

func channelMonitorAnalyticsTimestamp(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.FixedZone("UTC+8", 8*60*60))
	if err != nil {
		return 0, &channelMonitorAnalyticsQueryError{"日期必须为 YYYY-MM-DD"}
	}
	return parsed.Unix(), nil
}

func channelMonitorAnalyticsPositiveInt(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, &channelMonitorAnalyticsQueryError{"筛选 ID 必须为正整数"}
	}
	return value, nil
}

func queryChannelMonitorHistoricalAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	if query.To <= query.From {
		return channelMonitorAnalyticsResponse{}, &channelMonitorAnalyticsQueryError{"时间范围无效"}
	}
	if query.To-query.From > 90*24*60*60 {
		return channelMonitorAnalyticsResponse{}, &channelMonitorAnalyticsQueryError{"时间范围不能超过 90 天"}
	}
	if query.Metric == "success" && query.From == model.ChannelDailyCostDayStart(common.GetTimestamp()) {
		return queryChannelMonitorCurrentSuccessAnalytics(ctx, query)
	}
	if query.Metric == "cost" {
		return queryChannelMonitorHistoricalCostAnalytics(ctx, query)
	}
	return queryChannelMonitorHistoricalSuccessAnalytics(ctx, query)
}

func queryChannelMonitorCurrentSuccessAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	view, err := service.QueryChannelMonitorRedisDailySuccessAnalytics(ctx, query.From)
	if err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	rows := make([]channelMonitorAnalyticsSuccessRow, 0, len(view.Rows))
	var summaryRow channelMonitorAnalyticsSuccessRow
	userScopedRoutes := make(map[string]bool)
	for _, redisRow := range view.Rows {
		if redisRow.UserID > 0 && redisRow.APIKeyID > 0 && redisRow.ChannelID > 0 && redisRow.ModelName != "" {
			userScopedRoutes[strconv.Itoa(redisRow.APIKeyID)+":"+strconv.Itoa(redisRow.ChannelID)+":"+redisRow.ModelName] = true
		}
	}
	for _, redisRow := range view.Rows {
		if query.GroupBy == "api_key_channel_model" && redisRow.UserID == 0 && userScopedRoutes[strconv.Itoa(redisRow.APIKeyID)+":"+strconv.Itoa(redisRow.ChannelID)+":"+redisRow.ModelName] {
			continue
		}
		if !channelMonitorAnalyticsCurrentRowMatches(query, redisRow) {
			continue
		}
		row := channelMonitorAnalyticsSuccessRow{
			DayStart: query.From, ChannelID: redisRow.ChannelID, UserID: redisRow.UserID,
			UserAttribution: redisRow.UserAttribution, APIKeyID: redisRow.APIKeyID,
			APIKeyKey: redisRow.APIKeyKey, APIKeyName: redisRow.APIKeyName,
			ModelKey: redisRow.ModelKey, ModelName: redisRow.ModelName,
			ActualSuccess: redisRow.Aggregate.ActualSuccessCount, ActualFailure: redisRow.Aggregate.ActualFailureCount,
			FinalSuccess: redisRow.Aggregate.FinalSuccessCount, FinalFailure: redisRow.Aggregate.FinalFailureCount,
			CacheHit: redisRow.Aggregate.CacheHitCount, CacheSample: redisRow.Aggregate.CacheSampleCount,
			CacheReadTokens: redisRow.Aggregate.CacheReadTokens, InputTokens: redisRow.Aggregate.InputTokens,
			CacheWriteCount: redisRow.Aggregate.CacheWriteRequestCount,
		}
		rows = append(rows, row)
		channelMonitorAnalyticsAddSuccessRow(&summaryRow, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left := channelMonitorAnalyticsSuccessSortValue(rows[i], query.Sort)
		right := channelMonitorAnalyticsSuccessSortValue(rows[j], query.Sort)
		if left != right {
			if query.Direction == "asc" {
				return left < right
			}
			return left > right
		}
		return channelMonitorAnalyticsSuccessKey(query.GroupBy, rows[i]) < channelMonitorAnalyticsSuccessKey(query.GroupBy, rows[j])
	})
	total := int64(len(rows))
	start := (query.Page - 1) * query.PageSize
	if start > len(rows) {
		start = len(rows)
	}
	end := start + query.PageSize
	if end > len(rows) {
		end = len(rows)
	}
	items := make([]map[string]any, 0, end-start)
	for _, row := range rows[start:end] {
		items = append(items, channelMonitorAnalyticsSuccessItem(query.GroupBy, row))
	}
	if err := attachChannelMonitorAnalyticsUserNames(ctx, items); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	coverage := channelMonitorCurrentDayCoverage(ctx, query.From, view.DataCutoffAt)
	summary := channelMonitorAnalyticsSuccessSummary(summaryRow)
	return channelMonitorAnalyticsResponse{
		Source: "redis_daily", GroupBy: query.GroupBy, Coverage: coverage, Summary: summary, ScopeSummary: summary,
		Items: items, Page: query.Page, PageSize: query.PageSize, Total: total,
		GeneratedAt: common.GetTimestamp(),
	}, nil
}

// channelMonitorCurrentDayCoverage describes the health of the live snapshot.
// data_cutoff_at is the timestamp of the latest business sample, so it must
// not be compared with wall-clock time: an idle channel is not a lagging
// channel. Consumer health and backlog are the completeness signals here.
func channelMonitorCurrentDayCoverage(ctx context.Context, requestedFrom, dataCutoffAt int64) service.ChannelMonitorCoverage {
	status := service.GetChannelMonitorRedisRealtimeStatus(ctx)
	coveredThrough := dataCutoffAt
	if coveredThrough < requestedFrom {
		coveredThrough = requestedFrom
	}
	reasons := append([]string(nil), status.DegradedReasons...)
	return service.DeriveChannelMonitorCoverage(
		status.RedisAvailable,
		requestedFrom,
		coveredThrough,
		requestedFrom,
		coveredThrough,
		reasons,
	)
}

func channelMonitorAnalyticsCurrentRowMatches(query channelMonitorAnalyticsQuery, row service.ChannelMonitorRedisDailySuccessAnalyticsRow) bool {
	if query.Channel > 0 && row.ChannelID != query.Channel {
		return false
	}
	if query.User > 0 && row.UserID != query.User {
		return false
	}
	if query.APIKey > 0 && row.APIKeyID != query.APIKey {
		return false
	}
	if query.Model != "" && row.ModelName != query.Model && row.ModelKey != query.Model {
		return false
	}
	if query.Search != "" {
		needle := strings.ToLower(query.Search)
		values := []string{row.APIKeyName, row.ModelName, row.ModelKey, strconv.Itoa(row.ChannelID), strconv.Itoa(row.UserID), strconv.Itoa(row.APIKeyID)}
		matched := false
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), needle) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	switch query.GroupBy {
	case "channel":
		return row.ChannelID > 0 && row.UserID == 0 && row.APIKeyID == 0 && row.ModelName == ""
	case "user":
		if query.Channel > 0 {
			return row.ChannelID == query.Channel && row.UserID > 0 && row.APIKeyID == 0 && row.ModelName == ""
		}
		return row.ChannelID == 0 && row.UserID > 0 && row.APIKeyID == 0 && row.ModelName == ""
	case "api_key":
		if query.Channel > 0 || query.User > 0 {
			return row.ChannelID > 0 && row.UserID > 0 && row.APIKeyID > 0 && row.ModelName == ""
		}
		return row.ChannelID == 0 && row.UserID == 0 && row.APIKeyID > 0 && row.ModelName == ""
	case "model":
		return row.ChannelID == 0 && row.UserID == 0 && row.APIKeyID == 0 && row.ModelName != ""
	case "channel_model":
		if query.APIKey > 0 {
			return row.ChannelID > 0 && row.UserID > 0 && row.APIKeyID == query.APIKey && row.ModelName != ""
		}
		return row.ChannelID > 0 && row.UserID == 0 && row.APIKeyID == 0 && row.ModelName != ""
	case "api_key_channel_model":
		return row.ChannelID > 0 && row.APIKeyID > 0 && row.ModelName != ""
	default:
		return false
	}
}

func channelMonitorAnalyticsAddSuccessRow(target *channelMonitorAnalyticsSuccessRow, source channelMonitorAnalyticsSuccessRow) {
	target.ActualSuccess += source.ActualSuccess
	target.ActualFailure += source.ActualFailure
	target.FinalSuccess += source.FinalSuccess
	target.FinalFailure += source.FinalFailure
	target.CacheHit += source.CacheHit
	target.CacheSample += source.CacheSample
	target.CacheReadTokens += source.CacheReadTokens
	target.InputTokens += source.InputTokens
	target.CacheWriteCount += source.CacheWriteCount
}

func channelMonitorAnalyticsSuccessSortValue(row channelMonitorAnalyticsSuccessRow, sortKey string) int64 {
	switch sortKey {
	case "success":
		return row.ActualSuccess
	case "failure":
		return row.ActualFailure
	case "cache_tokens":
		return row.CacheReadTokens
	default:
		return row.ActualSuccess + row.ActualFailure
	}
}

func channelMonitorAnalyticsMinInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

type channelMonitorAnalyticsSuccessRow struct {
	DayStart        int64  `gorm:"column:day_start"`
	ChannelID       int    `gorm:"column:channel_id"`
	UserID          int    `gorm:"column:user_id"`
	UserAttribution string `gorm:"column:user_attribution"`
	APIKeyID        int    `gorm:"column:api_key_id"`
	APIKeyKey       string `gorm:"column:api_key_key"`
	APIKeyName      string `gorm:"column:api_key_name"`
	ModelKey        string `gorm:"column:model_key"`
	ModelName       string `gorm:"column:model_name"`
	ActualSuccess   int64  `gorm:"column:actual_success_count"`
	ActualFailure   int64  `gorm:"column:actual_failure_count"`
	FinalSuccess    int64  `gorm:"column:final_success_count"`
	FinalFailure    int64  `gorm:"column:final_failure_count"`
	CacheHit        int64  `gorm:"column:cache_hit_count"`
	CacheSample     int64  `gorm:"column:cache_sample_count"`
	CacheReadTokens int64  `gorm:"column:cache_read_tokens"`
	InputTokens     int64  `gorm:"column:input_tokens"`
	CacheWriteCount int64  `gorm:"column:cache_write_count"`
}

func queryChannelMonitorHistoricalSuccessAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	base := channelMonitorAnalyticsSuccessBaseQuery(ctx, query)
	summaryBase := channelMonitorAnalyticsSuccessBaseQuery(ctx, query)
	groupColumns := channelMonitorAnalyticsSuccessGroupColumns(query.GroupBy)
	selectColumns := []string{
		channelMonitorAnalyticsSelectDimension("day_start", groupColumns),
		channelMonitorAnalyticsSelectDimension("channel_id", groupColumns),
		channelMonitorAnalyticsSelectDimension("user_id", groupColumns),
		"MAX(user_attribution) AS user_attribution",
		channelMonitorAnalyticsSelectDimension("api_key_id", groupColumns),
		channelMonitorAnalyticsSelectDimension("api_key_key", groupColumns),
		"MAX(api_key_name) AS api_key_name",
		channelMonitorAnalyticsSelectDimension("model_key", groupColumns),
		"MAX(model_name) AS model_name",
		"SUM(actual_success_count) AS actual_success_count",
		"SUM(actual_failure_count) AS actual_failure_count",
		"SUM(final_success_count) AS final_success_count",
		"SUM(final_failure_count) AS final_failure_count",
		"SUM(cache_hit_count) AS cache_hit_count",
		"SUM(cache_sample_count) AS cache_sample_count",
		"SUM(cache_read_tokens) AS cache_read_tokens",
		"SUM(input_tokens) AS input_tokens",
		"SUM(cache_write_count) AS cache_write_count",
	}
	groupSQL := strings.Join(groupColumns, ", ")
	countGrouped := base.Session(&gorm.Session{}).Select(groupSQL).Group(groupSQL)
	grouped := base.Select(strings.Join(selectColumns, ", ")).Group(groupSQL)
	grouped = grouped.Order(channelMonitorAnalyticsSuccessOrder(query.Sort, query.Direction, groupColumns))
	var total int64
	countQuery := model.DB.WithContext(ctx).Table("(?) AS grouped_rows", countGrouped)
	if err := countQuery.Count(&total).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	var rows []channelMonitorAnalyticsSuccessRow
	if err := grouped.Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&rows).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	var summaryRow channelMonitorAnalyticsSuccessRow
	if err := summaryBase.Select(
		"COALESCE(SUM(actual_success_count), 0) AS actual_success_count",
		"COALESCE(SUM(actual_failure_count), 0) AS actual_failure_count",
		"COALESCE(SUM(final_success_count), 0) AS final_success_count",
		"COALESCE(SUM(final_failure_count), 0) AS final_failure_count",
		"COALESCE(SUM(cache_hit_count), 0) AS cache_hit_count",
		"COALESCE(SUM(cache_sample_count), 0) AS cache_sample_count",
		"COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens",
		"COALESCE(SUM(input_tokens), 0) AS input_tokens",
		"COALESCE(SUM(cache_write_count), 0) AS cache_write_count",
	).Scan(&summaryRow).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	summary := channelMonitorAnalyticsSuccessSummary(summaryRow)
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, channelMonitorAnalyticsSuccessItem(query.GroupBy, row))
	}
	if err := attachChannelMonitorAnalyticsUserNames(ctx, items); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	return channelMonitorAnalyticsResponse{
		Source: "database_daily", GroupBy: query.GroupBy, Coverage: service.DeriveChannelMonitorCoverage(true, query.From, query.To, query.From, query.To, nil),
		Summary: summary, ScopeSummary: summary, Items: items, Page: query.Page, PageSize: query.PageSize,
		Total: total, GeneratedAt: common.GetTimestamp(),
	}, nil
}

func channelMonitorAnalyticsSuccessBaseQuery(ctx context.Context, query channelMonitorAnalyticsQuery) *gorm.DB {
	base := model.DB.WithContext(ctx).Model(&model.ChannelMonitorDailySuccessLedger{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To)
	if query.Channel > 0 {
		base = base.Where("channel_id = ?", query.Channel)
	}
	if query.User > 0 {
		base = base.Where("user_id = ?", query.User)
	}
	if query.APIKey > 0 {
		base = base.Where("api_key_id = ?", query.APIKey)
	}
	if query.Model != "" {
		base = base.Where("model_name = ? OR model_key = ?", query.Model, query.Model)
	}
	if query.Search != "" {
		search := "%" + query.Search + "%"
		condition := "api_key_name LIKE ? OR api_key_key LIKE ? OR model_name LIKE ?"
		args := []any{search, search, search}
		if exact, parseErr := strconv.Atoi(query.Search); parseErr == nil && exact > 0 {
			condition += " OR channel_id = ? OR user_id = ? OR api_key_id = ?"
			args = append(args, exact, exact, exact)
		}
		base = base.Where(condition, args...)
	}
	return base
}

func channelMonitorAnalyticsSuccessGroupColumns(groupBy string) []string {
	switch groupBy {
	case "day":
		return []string{"day_start"}
	case "channel":
		return []string{"channel_id"}
	case "user":
		return []string{"user_id"}
	case "api_key":
		return []string{"api_key_id", "api_key_key", "user_id"}
	case "model":
		return []string{"model_key"}
	case "channel_model":
		return []string{"channel_id", "model_key"}
	case "api_key_channel_model":
		return []string{"api_key_id", "api_key_key", "user_id", "channel_id", "model_key"}
	default:
		return []string{"channel_id"}
	}
}

func channelMonitorAnalyticsSelectDimension(column string, groupColumns []string) string {
	for _, groupColumn := range groupColumns {
		if column == groupColumn {
			return column
		}
	}
	return "MAX(" + column + ") AS " + column
}

func channelMonitorAnalyticsSuccessOrder(sortKey, direction string, groupColumns []string) string {
	order := "SUM(actual_success_count) + SUM(actual_failure_count)"
	switch sortKey {
	case "success":
		order = "SUM(actual_success_count)"
	case "failure":
		order = "SUM(actual_failure_count)"
	case "cache_tokens":
		order = "SUM(cache_read_tokens)"
	}
	return order + " " + direction + ", " + groupColumns[0] + " ASC"
}

func channelMonitorAnalyticsSuccessSummary(row channelMonitorAnalyticsSuccessRow) map[string]any {
	actualSample := row.ActualSuccess + row.ActualFailure
	finalSample := row.FinalSuccess + row.FinalFailure
	return map[string]any{
		"actual_success_count": row.ActualSuccess, "actual_failure_count": row.ActualFailure,
		"actual_sample_count": actualSample, "actual_success_rate": channelMonitorAnalyticsRate(row.ActualSuccess, actualSample),
		"final_success_count": row.FinalSuccess, "final_failure_count": row.FinalFailure,
		"final_sample_count": finalSample, "final_success_rate": channelMonitorAnalyticsRate(row.FinalSuccess, finalSample),
		"cache_hit_count": row.CacheHit, "cache_sample_count": row.CacheSample,
		"cache_hit_rate":    channelMonitorAnalyticsRate(row.CacheHit, row.CacheSample),
		"cache_read_tokens": row.CacheReadTokens, "input_tokens": row.InputTokens,
		"cache_utilization_rate":    channelMonitorAnalyticsRate(row.CacheReadTokens, row.InputTokens),
		"cache_write_request_count": row.CacheWriteCount,
	}
}

func channelMonitorAnalyticsSuccessItem(groupBy string, row channelMonitorAnalyticsSuccessRow) map[string]any {
	item := channelMonitorAnalyticsSuccessSummary(row)
	item["day_start"] = row.DayStart
	item["channel_id"] = row.ChannelID
	item["user_id"] = row.UserID
	item["user_attribution"] = row.UserAttribution
	item["api_key_id"] = row.APIKeyID
	item["api_key_key"] = row.APIKeyKey
	item["api_key_name"] = row.APIKeyName
	item["model_key"] = row.ModelKey
	item["model_name"] = row.ModelName
	item["group_by"] = groupBy
	item["key"] = channelMonitorAnalyticsSuccessKey(groupBy, row)
	return item
}

func channelMonitorAnalyticsSuccessKey(groupBy string, row channelMonitorAnalyticsSuccessRow) string {
	switch groupBy {
	case "day":
		return strconv.FormatInt(row.DayStart, 10)
	case "channel":
		return strconv.Itoa(row.ChannelID)
	case "user":
		return strconv.Itoa(row.UserID)
	case "api_key":
		if row.APIKeyID > 0 {
			return strconv.Itoa(row.APIKeyID)
		}
		return row.APIKeyKey
	case "model":
		return row.ModelName
	case "channel_model":
		return strconv.Itoa(row.ChannelID) + ":" + row.ModelName
	case "api_key_channel_model":
		return strconv.Itoa(row.APIKeyID) + ":" + strconv.Itoa(row.ChannelID) + ":" + row.ModelName
	default:
		return strconv.Itoa(row.ChannelID)
	}
}

func channelMonitorAnalyticsRate(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func queryChannelMonitorHistoricalCostAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	if query.GroupBy != "day" && query.GroupBy != "channel" && model.DB != nil && model.DB.Migrator().HasTable(&model.ChannelMonitorDailyCostDetail{}) {
		return queryChannelMonitorHistoricalCostDetailAnalytics(ctx, query)
	}
	type row struct {
		Key             string `gorm:"column:analytics_key"`
		ChannelID       int    `gorm:"column:channel_id"`
		UserID          int    `gorm:"column:user_id"`
		APIKeyID        int    `gorm:"column:api_key_id"`
		APIKeyName      string `gorm:"column:api_key_name"`
		Cost            int64  `gorm:"column:cost"`
		SettledCount    int64  `gorm:"column:settled_count"`
		UnresolvedCount int64  `gorm:"column:unresolved_count"`
	}
	base := model.DB.WithContext(ctx).Model(&model.ChannelDailyAPIKeyCost{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To)
	if query.GroupBy == "user" {
		base = base.Joins("LEFT JOIN tokens ON tokens.id = channel_daily_api_key_costs.api_key_id")
	}
	if query.User > 0 {
		base = base.Joins("LEFT JOIN tokens ON tokens.id = channel_daily_api_key_costs.api_key_id")
	}
	if query.Channel > 0 {
		base = base.Where("channel_id = ?", query.Channel)
	}
	if query.APIKey > 0 {
		base = base.Where("api_key_id = ?", query.APIKey)
	}
	if query.User > 0 {
		base = base.Where("COALESCE(tokens.user_id, 0) = ?", query.User)
	}
	if query.Search != "" {
		search := "%" + query.Search + "%"
		condition := "api_key_name LIKE ?"
		args := []any{search}
		if exact, parseErr := strconv.Atoi(query.Search); parseErr == nil && exact > 0 {
			condition += " OR api_key_id = ?"
			args = append(args, exact)
		}
		base = base.Where(condition, args...)
	}
	if query.GroupBy == "day" || query.GroupBy == "channel" {
		base = model.DB.WithContext(ctx).Model(&model.ChannelDailyCost{}).
			Where("day_start >= ? AND day_start < ?", query.From, query.To)
		if query.Channel > 0 {
			base = base.Where("channel_id = ?", query.Channel)
		}
	}
	selectSQL := "channel_id AS analytics_key, channel_id, 0 AS api_key_id, '' AS api_key_name, SUM(cost_nano_cny) AS cost, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count"
	groupSQL := "channel_id"
	if query.GroupBy == "day" {
		selectSQL = "day_start AS analytics_key, 0 AS channel_id, 0 AS api_key_id, '' AS api_key_name, SUM(cost_nano_cny) AS cost, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count"
		groupSQL = "day_start"
	} else if query.GroupBy == "api_key" {
		selectSQL = "api_key_id AS analytics_key, channel_id, api_key_id, MAX(api_key_name) AS api_key_name, SUM(cost_nano_cny) AS cost, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count"
		groupSQL = "channel_id, api_key_id, key_fingerprint"
	} else if query.GroupBy == "user" {
		selectSQL = "COALESCE(tokens.user_id, 0) AS analytics_key, 0 AS channel_id, COALESCE(tokens.user_id, 0) AS user_id, 0 AS api_key_id, MAX(api_key_name) AS api_key_name, SUM(cost_nano_cny) AS cost, SUM(settled_count) AS settled_count, SUM(unresolved_count) AS unresolved_count"
		groupSQL = "COALESCE(tokens.user_id, 0)"
	}
	orderKey := "channel_id"
	if query.GroupBy == "day" {
		orderKey = "day_start"
	} else if query.GroupBy == "api_key" {
		orderKey = "api_key_id"
	} else if query.GroupBy == "user" {
		orderKey = "COALESCE(tokens.user_id, 0)"
	}
	countGrouped := base.Session(&gorm.Session{}).Select(groupSQL).Group(groupSQL)
	grouped := base.Select(selectSQL).Group(groupSQL)
	grouped = grouped.Order("cost " + query.Direction + ", " + orderKey + " ASC")
	var total int64
	countQuery := model.DB.WithContext(ctx).Table("(?) AS grouped_rows", countGrouped)
	if err := countQuery.Count(&total).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	var rows []row
	if err := grouped.Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&rows).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	var summary struct {
		Cost            int64 `gorm:"column:cost"`
		SettledCount    int64 `gorm:"column:settled_count"`
		UnresolvedCount int64 `gorm:"column:unresolved_count"`
	}
	if err := base.Select("COALESCE(SUM(cost_nano_cny), 0) AS cost, COALESCE(SUM(settled_count), 0) AS settled_count, COALESCE(SUM(unresolved_count), 0) AS unresolved_count").Scan(&summary).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, item := range rows {
		items = append(items, map[string]any{
			"key": item.Key, "channel_id": item.ChannelID, "user_id": item.UserID, "api_key_id": item.APIKeyID,
			"api_key_name": item.APIKeyName, "cost_nano_cny": item.Cost,
			"settled_count": item.SettledCount, "unresolved_count": item.UnresolvedCount,
		})
	}
	if err := attachChannelMonitorAnalyticsUserNames(ctx, items); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	summaryMap := map[string]any{"cost_nano_cny": summary.Cost, "settled_count": summary.SettledCount, "unresolved_count": summary.UnresolvedCount}
	return channelMonitorAnalyticsResponse{
		Source: "database_daily", GroupBy: query.GroupBy, Coverage: service.DeriveChannelMonitorCoverage(true, query.From, query.To, query.From, query.To, nil),
		Summary: summaryMap, ScopeSummary: summaryMap, Items: items, Page: query.Page, PageSize: query.PageSize,
		Total: total, GeneratedAt: common.GetTimestamp(),
	}, nil
}

type channelMonitorAnalyticsCostDetailRow struct {
	DayStart        int64  `gorm:"column:day_start"`
	ChannelID       int    `gorm:"column:channel_id"`
	UserID          int    `gorm:"column:user_id"`
	UserAttribution string `gorm:"column:user_attribution"`
	APIKeyID        int    `gorm:"column:api_key_id"`
	APIKeyKey       string `gorm:"column:api_key_key"`
	APIKeyName      string `gorm:"column:api_key_name"`
	ModelKey        string `gorm:"column:model_key"`
	ModelName       string `gorm:"column:model_name"`
	Cost            int64  `gorm:"column:cost_nano_cny"`
	ProbeCost       int64  `gorm:"column:probe_cost_nano_cny"`
	GroupProbeCost  int64  `gorm:"column:group_probe_cost_nano_cny"`
	SettledCount    int64  `gorm:"column:settled_count"`
	UnresolvedCount int64  `gorm:"column:unresolved_count"`
}

type channelMonitorAnalyticsUserIdentity struct {
	ID          int    `gorm:"column:id"`
	Username    string `gorm:"column:username"`
	DisplayName string `gorm:"column:display_name"`
}

func attachChannelMonitorAnalyticsUserNames(ctx context.Context, items []map[string]any) error {
	userIDs := make([]int, 0)
	seenUserIDs := make(map[int]struct{})
	for _, item := range items {
		userID, ok := item["user_id"].(int)
		if !ok || userID <= 0 {
			continue
		}
		if _, exists := seenUserIDs[userID]; exists {
			continue
		}
		seenUserIDs[userID] = struct{}{}
		userIDs = append(userIDs, userID)
	}
	if len(userIDs) == 0 || model.DB == nil {
		return nil
	}
	sort.Ints(userIDs)
	var users []channelMonitorAnalyticsUserIdentity
	if err := model.DB.WithContext(ctx).Unscoped().Model(&model.User{}).
		Select("id", "username", "display_name").
		Where("id IN ?", userIDs).
		Find(&users).Error; err != nil {
		return err
	}
	userByID := make(map[int]channelMonitorAnalyticsUserIdentity, len(users))
	for _, user := range users {
		userByID[user.ID] = user
	}
	for _, item := range items {
		userID, ok := item["user_id"].(int)
		if !ok || userID <= 0 {
			continue
		}
		user, exists := userByID[userID]
		if !exists {
			continue
		}
		item["user_name"] = strings.TrimSpace(user.Username)
		item["user_display_name"] = strings.TrimSpace(user.DisplayName)
	}
	return nil
}

func queryChannelMonitorHistoricalCostDetailAnalytics(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsResponse, error) {
	base := channelMonitorAnalyticsCostDetailBaseQuery(ctx, query)
	summaryBase := channelMonitorAnalyticsCostDetailBaseQuery(ctx, query)
	groupColumns := channelMonitorAnalyticsCostGroupColumns(query.GroupBy)
	selectColumns := []string{
		channelMonitorAnalyticsSelectDimension("day_start", groupColumns),
		channelMonitorAnalyticsSelectDimension("channel_id", groupColumns),
		channelMonitorAnalyticsSelectDimension("user_id", groupColumns),
		"MAX(user_attribution) AS user_attribution",
		channelMonitorAnalyticsSelectDimension("api_key_id", groupColumns),
		channelMonitorAnalyticsSelectDimension("api_key_key", groupColumns),
		"MAX(api_key_name) AS api_key_name",
		channelMonitorAnalyticsSelectDimension("model_key", groupColumns),
		"MAX(model_name) AS model_name",
		"SUM(cost_nano_cny) AS cost_nano_cny",
		"SUM(probe_cost_nano_cny) AS probe_cost_nano_cny",
		"SUM(group_probe_cost_nano_cny) AS group_probe_cost_nano_cny",
		"SUM(settled_count) AS settled_count",
		"SUM(unresolved_count) AS unresolved_count",
	}
	groupSQL := strings.Join(groupColumns, ", ")
	countGrouped := base.Session(&gorm.Session{}).Select(groupSQL).Group(groupSQL)
	grouped := base.Select(strings.Join(selectColumns, ", ")).Group(groupSQL)
	grouped = grouped.Order("cost_nano_cny " + query.Direction + ", " + groupColumns[0] + " ASC")
	var total int64
	countQuery := model.DB.WithContext(ctx).Table("(?) AS grouped_rows", countGrouped)
	if err := countQuery.Count(&total).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	var rows []channelMonitorAnalyticsCostDetailRow
	if err := grouped.Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Scan(&rows).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	var summary channelMonitorAnalyticsCostDetailRow
	if err := summaryBase.Select(
		"COALESCE(SUM(cost_nano_cny), 0) AS cost_nano_cny",
		"COALESCE(SUM(probe_cost_nano_cny), 0) AS probe_cost_nano_cny",
		"COALESCE(SUM(group_probe_cost_nano_cny), 0) AS group_probe_cost_nano_cny",
		"COALESCE(SUM(settled_count), 0) AS settled_count",
		"COALESCE(SUM(unresolved_count), 0) AS unresolved_count",
	).Scan(&summary).Error; err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"key":       channelMonitorAnalyticsCostDetailKey(query.GroupBy, row),
			"day_start": row.DayStart, "channel_id": row.ChannelID, "user_id": row.UserID,
			"user_attribution": row.UserAttribution, "api_key_id": row.APIKeyID,
			"api_key_key": row.APIKeyKey, "api_key_name": row.APIKeyName,
			"model_key": row.ModelKey, "model_name": row.ModelName,
			"cost_nano_cny": row.Cost, "probe_cost_nano_cny": row.ProbeCost,
			"group_probe_cost_nano_cny": row.GroupProbeCost, "settled_count": row.SettledCount,
			"unresolved_count": row.UnresolvedCount,
		})
	}
	if err := attachChannelMonitorAnalyticsUserNames(ctx, items); err != nil {
		return channelMonitorAnalyticsResponse{}, err
	}
	summaryMap := map[string]any{
		"cost_nano_cny": summary.Cost, "probe_cost_nano_cny": summary.ProbeCost,
		"group_probe_cost_nano_cny": summary.GroupProbeCost, "settled_count": summary.SettledCount,
		"unresolved_count": summary.UnresolvedCount,
	}
	return channelMonitorAnalyticsResponse{
		Source: "database_daily", GroupBy: query.GroupBy, Coverage: service.DeriveChannelMonitorCoverage(true, query.From, query.To, query.From, query.To, nil),
		Summary: summaryMap, ScopeSummary: summaryMap, Items: items, Page: query.Page, PageSize: query.PageSize,
		Total: total, GeneratedAt: common.GetTimestamp(),
	}, nil
}

func channelMonitorAnalyticsCostDetailBaseQuery(ctx context.Context, query channelMonitorAnalyticsQuery) *gorm.DB {
	base := model.DB.WithContext(ctx).Model(&model.ChannelMonitorDailyCostDetail{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To)
	if query.Channel > 0 {
		base = base.Where("channel_id = ?", query.Channel)
	}
	if query.User > 0 {
		base = base.Where("user_id = ?", query.User)
	}
	if query.APIKey > 0 {
		base = base.Where("api_key_id = ?", query.APIKey)
	}
	if query.Model != "" {
		base = base.Where("model_name = ? OR model_key = ?", query.Model, query.Model)
	}
	if query.Search != "" {
		search := "%" + query.Search + "%"
		condition := "api_key_name LIKE ? OR model_name LIKE ? OR api_key_key LIKE ?"
		args := []any{search, search, search}
		if exact, parseErr := strconv.Atoi(query.Search); parseErr == nil && exact > 0 {
			condition += " OR channel_id = ? OR user_id = ? OR api_key_id = ?"
			args = append(args, exact, exact, exact)
		}
		base = base.Where(condition, args...)
	}
	return base
}

func channelMonitorAnalyticsCostGroupColumns(groupBy string) []string {
	switch groupBy {
	case "user":
		return []string{"user_id"}
	case "api_key":
		return []string{"api_key_id", "api_key_key", "user_id"}
	case "model":
		return []string{"model_key"}
	case "channel_model":
		return []string{"channel_id", "model_key"}
	case "api_key_channel_model":
		return []string{"api_key_id", "api_key_key", "user_id", "channel_id", "model_key"}
	default:
		return []string{"channel_id"}
	}
}

func channelMonitorAnalyticsCostDetailKey(groupBy string, row channelMonitorAnalyticsCostDetailRow) string {
	switch groupBy {
	case "user":
		return strconv.Itoa(row.UserID)
	case "api_key":
		return strconv.Itoa(row.APIKeyID) + ":" + row.APIKeyKey
	case "model":
		return row.ModelName
	case "channel_model":
		return strconv.Itoa(row.ChannelID) + ":" + row.ModelName
	case "api_key_channel_model":
		return strconv.Itoa(row.APIKeyID) + ":" + strconv.Itoa(row.ChannelID) + ":" + row.ModelName
	default:
		return strconv.Itoa(row.ChannelID)
	}
}
