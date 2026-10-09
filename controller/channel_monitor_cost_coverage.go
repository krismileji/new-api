package controller

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

// Unknown queue entries still block coverage. Identified costs must match the
// same parent filters and model normalization as the displayed statistics.
func channelMonitorAnalyticsCostBlockMatch(query channelMonitorAnalyticsQuery, block service.ChannelMonitorProfitBlock) bool {
	if block.To > 0 && block.To <= query.From || block.From >= query.To || query.Channel > 0 && block.ChannelID > 0 && query.Channel != block.ChannelID {
		return false
	}
	if block.CostEvent == nil {
		return true
	}
	event := block.CostEvent
	modelName := strings.TrimSpace(event.ModelName)
	source := strings.TrimSpace(event.SourceKind)
	if query.Metric == "cost" {
		modelName = ratio_setting.FormatMatchingModelName(modelName)
		if source == "" {
			source = string(model.ChannelMonitorEventSourceBusiness)
		}
	}
	if query.APIKeyKey != nil && channelMonitorAnalyticsSystemAPIKey(*query.APIKeyKey) {
		identity := channelMonitorAnalyticsCostAPIKeyIdentity(event.APIKeyId, event.KeyFingerprint, source, event.APIKeyName)
		if event.APIKeyId != 0 || identity != *query.APIKeyKey {
			return false
		}
		query.APIKeyKey = nil
	}
	return channelMonitorAnalyticsCurrentMatch(query, event.ChannelId, event.UserId, event.APIKeyId,
		event.APIKeyName, event.KeyFingerprint, modelName, model.ChannelMonitorDailyCostModelKey(modelName))
}

func channelMonitorAnalyticsPendingCosts(ctx context.Context, db *gorm.DB, query channelMonitorAnalyticsQuery) ([]service.ChannelMonitorProfitBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if db == nil {
		return nil, errors.New("渠道成本投影数据库不可用")
	}
	base := db.WithContext(ctx).Model(&model.ChannelDailyCostOutbox{}).
		Where("occurred_at >= ? AND occurred_at < ?", query.From, query.To)
	if query.Metric == "cost" {
		base = base.Where("redis_projected_at = ?", 0)
	} else {
		base = base.Where("processed_at = ?", 0)
	}
	if query.Channel > 0 {
		base = base.Where("channel_id = ?", query.Channel)
	}
	if query.hasUserFilter() {
		base = base.Where("user_id = ?", query.User)
	}
	if query.hasAPIKeyFilter() {
		base = base.Where("api_key_id = ?", query.APIKey)
	}
	const limit = 4096
	var rows []model.ChannelDailyCostOutbox
	err := base.Select("channel_id, occurred_at, user_id, api_key_id, api_key_name, key_fingerprint, model_name, source_kind").
		Order("id ASC").Limit(limit + 1).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > limit {
		return []service.ChannelMonitorProfitBlock{{Reason: "profit_cost_queue_unavailable"}}, nil
	}
	var blocks []service.ChannelMonitorProfitBlock
	for _, row := range rows {
		day := model.ChannelDailyCostDayStart(row.OccurredAt)
		block := service.ChannelMonitorProfitBlock{
			From: day, To: day + 86400, ChannelID: row.ChannelId, Reason: "cost_projection_pending",
			CostEvent: &model.ChannelDailyCostDelta{
				ChannelId: row.ChannelId, UserId: row.UserId, APIKeyId: row.APIKeyId,
				APIKeyName: row.APIKeyName, KeyFingerprint: row.KeyFingerprint,
				ModelName: row.ModelName, SourceKind: row.SourceKind, OccurredAt: row.OccurredAt,
			},
		}
		if channelMonitorAnalyticsCostBlockMatch(query, block) {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}
