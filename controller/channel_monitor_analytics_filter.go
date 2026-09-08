package controller

import (
	"context"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// An omitted ID selects every owner/key; an explicit zero selects only
// unattributed history. Positive programmatic filters remain compatible.
func channelMonitorAnalyticsOptionalID(raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, false, &channelMonitorAnalyticsQueryError{"筛选 ID 必须为非负整数"}
	}
	return value, true, nil
}

func (query channelMonitorAnalyticsQuery) hasUserFilter() bool {
	return query.UserSet || query.User > 0
}

func (query channelMonitorAnalyticsQuery) hasAPIKeyFilter() bool {
	return query.APIKeySet || query.APIKey > 0
}

func (query channelMonitorAnalyticsQuery) hasCostDetailFilter() bool {
	return query.hasUserFilter() || query.hasAPIKeyFilter() || query.APIKeyKey != nil || query.ModelKey != nil || query.Model != "" || query.Search != ""
}

func channelMonitorAnalyticsSearchPattern(search string) string {
	return "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search)) + "%"
}

// Resolve display names before grouping/pagination, once for both sides of a
// mixed-day query. Bound name matches rather than silently truncating them.
func prepareChannelMonitorAnalyticsSearch(ctx context.Context, query channelMonitorAnalyticsQuery) (channelMonitorAnalyticsQuery, error) {
	if query.Search == "" {
		return query, nil
	}
	if model.DB == nil {
		return query, &channelMonitorAnalyticsQueryError{"暂时无法查询统计名称"}
	}
	const identityLimit = 2000
	pattern := channelMonitorAnalyticsSearchPattern(query.Search)
	if err := model.DB.WithContext(ctx).Unscoped().Model(&model.User{}).
		Where("LOWER(username) LIKE ? ESCAPE '!' OR LOWER(display_name) LIKE ? ESCAPE '!'", pattern, pattern).
		Order("id").Limit(identityLimit+1).Pluck("id", &query.SearchUserIDs).Error; err != nil {
		return query, err
	}
	if err := model.DB.WithContext(ctx).Model(&model.Channel{}).
		Where("LOWER(name) LIKE ? ESCAPE '!'", pattern).
		Order("id").Limit(identityLimit+1).Pluck("id", &query.SearchChannelIDs).Error; err != nil {
		return query, err
	}
	if len(query.SearchUserIDs) > identityLimit || len(query.SearchChannelIDs) > identityLimit {
		return query, &channelMonitorAnalyticsQueryError{"名称匹配结果较多，请缩小搜索范围"}
	}
	return query, nil
}

func (query channelMonitorAnalyticsQuery) applySearch(base *gorm.DB) *gorm.DB {
	if query.Search == "" {
		return base
	}
	pattern := channelMonitorAnalyticsSearchPattern(query.Search)
	condition := "LOWER(api_key_name) LIKE ? ESCAPE '!' OR LOWER(api_key_key) LIKE ? ESCAPE '!' OR LOWER(model_name) LIKE ? ESCAPE '!'"
	args := []any{pattern, pattern, pattern}
	if exact, err := strconv.Atoi(query.Search); err == nil && exact >= 0 {
		condition += " OR channel_id = ? OR user_id = ? OR api_key_id = ?"
		args = append(args, exact, exact, exact)
	}
	if len(query.SearchUserIDs) > 0 {
		condition += " OR user_id IN ?"
		args = append(args, query.SearchUserIDs)
	}
	if len(query.SearchChannelIDs) > 0 {
		condition += " OR channel_id IN ?"
		args = append(args, query.SearchChannelIDs)
	}
	return base.Where(condition, args...)
}
