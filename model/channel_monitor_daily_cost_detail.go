package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelMonitorDailyCostDetail stores the cost dimensions needed by the
// monitor drill-down. ChannelDailyCost remains the authoritative channel
// ledger; this table is a separately replaceable query projection.
type ChannelMonitorDailyCostDetail struct {
	Id              int64  `gorm:"primaryKey"`
	DayStart        int64  `gorm:"not null;uniqueIndex:idx_cm_daily_cost_detail_dim;index:idx_cm_daily_cost_detail_day"`
	ChannelId       int    `gorm:"not null;uniqueIndex:idx_cm_daily_cost_detail_dim;index:idx_cm_daily_cost_detail_channel"`
	UserId          int    `gorm:"not null;default:0;uniqueIndex:idx_cm_daily_cost_detail_dim;index:idx_cm_daily_cost_detail_user"`
	UserAttribution string `gorm:"size:16;not null;default:''"`
	APIKeyId        int    `gorm:"not null;default:0;uniqueIndex:idx_cm_daily_cost_detail_dim;index:idx_cm_daily_cost_detail_api_key"`
	APIKeyKey       string `gorm:"size:64;not null;default:'';uniqueIndex:idx_cm_daily_cost_detail_dim"`
	APIKeyName      string `gorm:"size:255;not null;default:''"`
	ModelKey        string `gorm:"size:64;not null;default:'';uniqueIndex:idx_cm_daily_cost_detail_dim"`
	ModelName       string `gorm:"size:255;not null;default:''"`
	SourceKind      string `gorm:"size:32;not null;default:'';uniqueIndex:idx_cm_daily_cost_detail_dim"`

	CostNanoCNY           int64 `gorm:"not null"`
	ProbeCostNanoCNY      int64 `gorm:"not null;default:0"`
	GroupProbeCostNanoCNY int64 `gorm:"not null;default:0"`
	SettledCount          int64 `gorm:"not null"`
	UnresolvedCount       int64 `gorm:"not null"`
	CreatedAt             int64 `gorm:"not null"`
	UpdatedAt             int64 `gorm:"not null"`
}

func (ChannelMonitorDailyCostDetail) TableName() string {
	return "channel_monitor_daily_cost_details"
}

func ChannelMonitorDailyCostModelKey(modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return "unknown"
	}
	digest := sha256.Sum256([]byte(modelName))
	return hex.EncodeToString(digest[:])
}

func normalizeChannelMonitorDailyCostDetail(delta ChannelDailyCostDelta) (ChannelMonitorDailyCostDetail, error) {
	if err := normalizeChannelDailyCostDelta(&delta); err != nil {
		return ChannelMonitorDailyCostDetail{}, err
	}
	modelName := strings.TrimSpace(delta.ModelName)
	if len(modelName) > 255 {
		return ChannelMonitorDailyCostDetail{}, errors.New("成本明细模型名称过长")
	}
	userAttribution := strings.TrimSpace(delta.UserAttribution)
	if userAttribution == "" {
		userAttribution = string(ChannelMonitorEventUserAttributionUnknown)
	}
	if delta.UserId > 0 && userAttribution == string(ChannelMonitorEventUserAttributionUnknown) {
		userAttribution = string(ChannelMonitorEventUserAttributionRequest)
	}
	sourceKind := strings.TrimSpace(delta.SourceKind)
	if sourceKind == "" {
		sourceKind = "unknown"
	}
	return ChannelMonitorDailyCostDetail{
		DayStart: deltaDayStart(delta.OccurredAt), ChannelId: delta.ChannelId, UserId: delta.UserId,
		UserAttribution: userAttribution, APIKeyId: delta.APIKeyId, APIKeyKey: delta.KeyFingerprint,
		APIKeyName: delta.APIKeyName, ModelKey: ChannelMonitorDailyCostModelKey(modelName), ModelName: modelName,
		SourceKind: sourceKind, CostNanoCNY: delta.CostNanoCNY, ProbeCostNanoCNY: delta.ProbeCostNanoCNY,
		GroupProbeCostNanoCNY: delta.GroupProbeCostNanoCNY, SettledCount: delta.SettledDelta,
		UnresolvedCount: delta.UnresolvedDelta, CreatedAt: delta.OccurredAt, UpdatedAt: delta.OccurredAt,
	}, nil
}

func deltaDayStart(occurredAt int64) int64 { return ChannelDailyCostDayStart(occurredAt) }

func addChannelMonitorDailyCostDetail(tx *gorm.DB, delta ChannelDailyCostDelta) error {
	if tx == nil {
		return errors.New("成本明细数据库不可用")
	}
	detail, err := normalizeChannelMonitorDailyCostDetail(delta)
	if err != nil {
		return err
	}
	keyWhere := "day_start = ? AND channel_id = ? AND user_id = ? AND api_key_id = ? AND api_key_key = ? AND model_key = ? AND source_kind = ?"
	keyArgs := []any{detail.DayStart, detail.ChannelId, detail.UserId, detail.APIKeyId, detail.APIKeyKey, detail.ModelKey, detail.SourceKind}
	update := func() (bool, error) {
		updated := tx.Model(&ChannelMonitorDailyCostDetail{}).Where(keyWhere, keyArgs...).
			Where("cost_nano_cny >= 0 AND cost_nano_cny <= ?", math.MaxInt64-detail.CostNanoCNY).
			Where("probe_cost_nano_cny >= 0 AND probe_cost_nano_cny <= ?", math.MaxInt64-detail.ProbeCostNanoCNY).
			Where("group_probe_cost_nano_cny >= 0 AND group_probe_cost_nano_cny <= ?", math.MaxInt64-detail.GroupProbeCostNanoCNY).
			Where("settled_count >= 0 AND settled_count <= ?", math.MaxInt64-detail.SettledCount).
			Where("unresolved_count >= 0 AND unresolved_count <= ?", math.MaxInt64-detail.UnresolvedCount).
			Updates(map[string]any{
				"user_attribution": detail.UserAttribution, "api_key_name": detail.APIKeyName, "model_name": detail.ModelName,
				"cost_nano_cny":             gorm.Expr("cost_nano_cny + ?", detail.CostNanoCNY),
				"probe_cost_nano_cny":       gorm.Expr("probe_cost_nano_cny + ?", detail.ProbeCostNanoCNY),
				"group_probe_cost_nano_cny": gorm.Expr("group_probe_cost_nano_cny + ?", detail.GroupProbeCostNanoCNY),
				"settled_count":             gorm.Expr("settled_count + ?", detail.SettledCount),
				"unresolved_count":          gorm.Expr("unresolved_count + ?", detail.UnresolvedCount),
				"updated_at":                detail.UpdatedAt,
			})
		return updated.RowsAffected == 1, updated.Error
	}
	if updated, err := update(); err != nil || updated {
		return err
	}
	create := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&detail)
	if create.Error != nil {
		return create.Error
	}
	if create.RowsAffected == 1 {
		return nil
	}
	updated, err := update()
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("成本明细写入失败或累计超过 int64 范围")
	}
	return nil
}

type ChannelMonitorDailyCostDetailQuery struct {
	From      int64
	To        int64
	ChannelId int
	UserId    int
	APIKeyId  int
	Model     string
}

func GetChannelMonitorDailyCostDetails(ctx context.Context, query ChannelMonitorDailyCostDetailQuery) ([]ChannelMonitorDailyCostDetail, error) {
	db := DB.WithContext(ctx).Model(&ChannelMonitorDailyCostDetail{}).
		Where("day_start >= ? AND day_start < ?", query.From, query.To)
	if query.ChannelId > 0 {
		db = db.Where("channel_id = ?", query.ChannelId)
	}
	if query.UserId > 0 {
		db = db.Where("user_id = ?", query.UserId)
	}
	if query.APIKeyId > 0 {
		db = db.Where("api_key_id = ?", query.APIKeyId)
	}
	if strings.TrimSpace(query.Model) != "" {
		db = db.Where("model_name = ? OR model_key = ?", query.Model, query.Model)
	}
	var rows []ChannelMonitorDailyCostDetail
	if err := db.Order("day_start ASC, channel_id ASC, api_key_id ASC, model_key ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
