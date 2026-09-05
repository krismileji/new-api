package model

import (
	"context"
	"errors"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelMonitorDailySuccessLedger is the historical day read model. The
// API-key grain keeps channel/user/key/model drill-down possible without
// scanning minute rows during a page request.
type ChannelMonitorDailySuccessLedger struct {
	Id              int64  `gorm:"primaryKey"`
	DayStart        int64  `gorm:"not null;uniqueIndex:idx_cm_daily_success_dim;index:idx_cm_daily_success_day"`
	ChannelId       int    `gorm:"not null;uniqueIndex:idx_cm_daily_success_dim;index:idx_cm_daily_success_channel"`
	UserId          int    `gorm:"not null;default:0;uniqueIndex:idx_cm_daily_success_dim;index:idx_cm_daily_success_user"`
	UserAttribution string `gorm:"size:16;not null"`
	APIKeyId        int    `gorm:"not null;default:0;uniqueIndex:idx_cm_daily_success_dim;index:idx_cm_daily_success_api_key"`
	APIKeyKey       string `gorm:"size:32;not null;uniqueIndex:idx_cm_daily_success_dim"`
	APIKeyName      string `gorm:"size:255;not null;default:''"`
	ModelKey        string `gorm:"size:32;not null;uniqueIndex:idx_cm_daily_success_dim"`
	ModelName       string `gorm:"size:255;not null;default:''"`
	GroupKey        string `gorm:"size:32;not null;uniqueIndex:idx_cm_daily_success_dim"`
	GroupName       string `gorm:"size:255;not null;default:''"`

	ActualSuccessCount int64 `gorm:"not null"`
	ActualFailureCount int64 `gorm:"not null"`
	FinalSuccessCount  int64 `gorm:"not null"`
	FinalFailureCount  int64 `gorm:"not null"`
	CacheHitCount      int64 `gorm:"not null"`
	CacheSampleCount   int64 `gorm:"not null"`
	CacheReadTokens    int64 `gorm:"not null;default:0"`
	InputTokens        int64 `gorm:"not null;default:0"`
	CacheWriteCount    int64 `gorm:"not null"`
	CreatedAt          int64 `gorm:"not null"`
	UpdatedAt          int64 `gorm:"not null"`
}

// ChannelMonitorDailySuccessMinute is the per-minute contribution ledger.
// Replacing one minute is idempotent and lets late log repair subtract the old
// contribution before adding the corrected contribution.
type ChannelMonitorDailySuccessMinute struct {
	Id              int64  `gorm:"primaryKey"`
	MinuteStart     int64  `gorm:"not null;uniqueIndex:idx_cm_daily_success_minute_dim;index:idx_cm_daily_success_minute_day"`
	DayStart        int64  `gorm:"not null;uniqueIndex:idx_cm_daily_success_minute_dim"`
	ChannelId       int    `gorm:"not null;uniqueIndex:idx_cm_daily_success_minute_dim"`
	UserId          int    `gorm:"not null;default:0;uniqueIndex:idx_cm_daily_success_minute_dim"`
	UserAttribution string `gorm:"size:16;not null"`
	APIKeyId        int    `gorm:"not null;default:0;uniqueIndex:idx_cm_daily_success_minute_dim"`
	APIKeyKey       string `gorm:"size:32;not null;uniqueIndex:idx_cm_daily_success_minute_dim"`
	APIKeyName      string `gorm:"size:255;not null;default:''"`
	ModelKey        string `gorm:"size:32;not null;uniqueIndex:idx_cm_daily_success_minute_dim"`
	ModelName       string `gorm:"size:255;not null;default:''"`
	GroupKey        string `gorm:"size:32;not null;uniqueIndex:idx_cm_daily_success_minute_dim"`
	GroupName       string `gorm:"size:255;not null;default:''"`

	ActualSuccessCount int64 `gorm:"not null"`
	ActualFailureCount int64 `gorm:"not null"`
	FinalSuccessCount  int64 `gorm:"not null"`
	FinalFailureCount  int64 `gorm:"not null"`
	CacheHitCount      int64 `gorm:"not null"`
	CacheSampleCount   int64 `gorm:"not null"`
	CacheReadTokens    int64 `gorm:"not null;default:0"`
	InputTokens        int64 `gorm:"not null;default:0"`
	CacheWriteCount    int64 `gorm:"not null"`
	CreatedAt          int64 `gorm:"not null"`
	UpdatedAt          int64 `gorm:"not null"`
}

func (ChannelMonitorDailySuccessLedger) TableName() string {
	return "channel_monitor_daily_success_metrics"
}

func (ChannelMonitorDailySuccessMinute) TableName() string {
	return "channel_monitor_daily_success_minutes"
}

type channelMonitorDailySuccessDimension struct {
	DayStart        int64
	MinuteStart     int64
	ChannelId       int
	UserId          int
	UserAttribution string
	APIKeyId        int
	APIKeyKey       string
	APIKeyName      string
	ModelKey        string
	ModelName       string
	GroupKey        string
	GroupName       string
}

type channelMonitorDailySuccessValues struct {
	ActualSuccessCount int64
	ActualFailureCount int64
	FinalSuccessCount  int64
	FinalFailureCount  int64
	CacheHitCount      int64
	CacheSampleCount   int64
	CacheReadTokens    int64
	InputTokens        int64
	CacheWriteCount    int64
}

func (values channelMonitorDailySuccessValues) add(other channelMonitorDailySuccessValues, sign int64) (channelMonitorDailySuccessValues, error) {
	result := values
	fields := []*int64{
		&result.ActualSuccessCount, &result.ActualFailureCount, &result.FinalSuccessCount,
		&result.FinalFailureCount, &result.CacheHitCount, &result.CacheSampleCount,
		&result.CacheReadTokens, &result.InputTokens, &result.CacheWriteCount,
	}
	deltas := []int64{
		other.ActualSuccessCount, other.ActualFailureCount, other.FinalSuccessCount,
		other.FinalFailureCount, other.CacheHitCount, other.CacheSampleCount,
		other.CacheReadTokens, other.InputTokens, other.CacheWriteCount,
	}
	for index := range fields {
		if deltas[index] > 0 && sign > 0 && *fields[index] > math.MaxInt64-deltas[index] {
			return channelMonitorDailySuccessValues{}, errors.New("渠道监控日成功率汇总超过 int64 范围")
		}
		if sign < 0 && *fields[index] < deltas[index] {
			return channelMonitorDailySuccessValues{}, errors.New("渠道监控日成功率汇总出现负数")
		}
		if sign > 0 {
			*fields[index] += deltas[index]
		} else {
			*fields[index] -= deltas[index]
		}
	}
	return result, nil
}

func (metric ChannelMonitorDailySuccessLedger) values() channelMonitorDailySuccessValues {
	return channelMonitorDailySuccessValues{
		ActualSuccessCount: metric.ActualSuccessCount, ActualFailureCount: metric.ActualFailureCount,
		FinalSuccessCount: metric.FinalSuccessCount, FinalFailureCount: metric.FinalFailureCount,
		CacheHitCount: metric.CacheHitCount, CacheSampleCount: metric.CacheSampleCount,
		CacheReadTokens: metric.CacheReadTokens, InputTokens: metric.InputTokens,
		CacheWriteCount: metric.CacheWriteCount,
	}
}

func (minute ChannelMonitorDailySuccessMinute) values() channelMonitorDailySuccessValues {
	return channelMonitorDailySuccessValues{
		ActualSuccessCount: minute.ActualSuccessCount, ActualFailureCount: minute.ActualFailureCount,
		FinalSuccessCount: minute.FinalSuccessCount, FinalFailureCount: minute.FinalFailureCount,
		CacheHitCount: minute.CacheHitCount, CacheSampleCount: minute.CacheSampleCount,
		CacheReadTokens: minute.CacheReadTokens, InputTokens: minute.InputTokens,
		CacheWriteCount: minute.CacheWriteCount,
	}
}

// UpdateChannelMonitorDailySuccessForMinuteRange replaces each closed-minute
// contribution and updates the day table in the same transaction. Replaying a
// minute therefore subtracts its previous contribution before adding the new
// one and cannot double count.
func UpdateChannelMonitorDailySuccessForMinuteRange(ctx context.Context, startTimestamp, endTimestamp int64) error {
	startTimestamp = channelMonitorMinuteStart(startTimestamp)
	endTimestamp = channelMonitorMinuteStart(endTimestamp)
	if startTimestamp >= endTimestamp || DB == nil {
		return nil
	}
	for minuteStart := startTimestamp; minuteStart < endTimestamp; minuteStart += channelMonitorMinuteSeconds {
		if err := updateChannelMonitorDailySuccessForMinute(ctx, minuteStart); err != nil {
			return err
		}
	}
	return nil
}

func updateChannelMonitorDailySuccessForMinute(ctx context.Context, minuteStart int64) error {
	var minuteRows []ChannelMonitorMinuteAPIKeyMetric
	if err := DB.WithContext(ctx).Where("minute_start = ?", minuteStart).Find(&minuteRows).Error; err != nil {
		return err
	}
	userIdsByToken, err := channelMonitorDailySuccessTokenOwners(ctx, minuteRows)
	if err != nil {
		return err
	}
	newRows := make([]ChannelMonitorDailySuccessMinute, 0, len(minuteRows))
	for _, row := range minuteRows {
		userID := userIdsByToken[row.APIKeyId]
		attribution := string(ChannelMonitorEventUserAttributionUnknown)
		if userID > 0 {
			attribution = string(ChannelMonitorEventUserAttributionInferred)
		}
		newRows = append(newRows, ChannelMonitorDailySuccessMinute{
			MinuteStart: minuteStart, DayStart: ChannelDailyCostDayStart(minuteStart),
			ChannelId: row.ChannelId, UserId: userID, UserAttribution: attribution,
			APIKeyId: row.APIKeyId, APIKeyKey: row.APIKeyKey, APIKeyName: row.APIKeyName,
			ModelKey: row.ModelKey, ModelName: row.ModelName, GroupKey: row.GroupKey, GroupName: row.GroupName,
			ActualSuccessCount: row.ActualSuccessCount, ActualFailureCount: row.ActualFailureCount,
			FinalSuccessCount: row.FinalSuccessCount, FinalFailureCount: row.FinalFailureCount,
			CacheHitCount: row.CacheHitCount, CacheSampleCount: row.CacheSampleCount,
			CacheReadTokens: row.CacheReadTokens, InputTokens: row.InputTokens, CacheWriteCount: row.CacheWriteCount,
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		})
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var oldRows []ChannelMonitorDailySuccessMinute
		if err := tx.Where("minute_start = ?", minuteStart).Find(&oldRows).Error; err != nil {
			return err
		}
		for _, oldRow := range oldRows {
			if err := applyChannelMonitorDailySuccessDelta(tx, oldRow.dimension(), oldRow.values(), -1); err != nil {
				return err
			}
		}
		if err := tx.Where("minute_start = ?", minuteStart).Delete(&ChannelMonitorDailySuccessMinute{}).Error; err != nil {
			return err
		}
		if len(newRows) > 0 {
			if err := tx.Create(&newRows).Error; err != nil {
				return err
			}
		}
		for _, newRow := range newRows {
			if err := applyChannelMonitorDailySuccessDelta(tx, newRow.dimension(), newRow.values(), 1); err != nil {
				return err
			}
		}
		return nil
	})
}

func (minute ChannelMonitorDailySuccessMinute) dimension() channelMonitorDailySuccessDimension {
	return channelMonitorDailySuccessDimension{
		DayStart: minute.DayStart, MinuteStart: minute.MinuteStart, ChannelId: minute.ChannelId,
		UserId: minute.UserId, UserAttribution: minute.UserAttribution, APIKeyId: minute.APIKeyId,
		APIKeyKey: minute.APIKeyKey, APIKeyName: minute.APIKeyName, ModelKey: minute.ModelKey,
		ModelName: minute.ModelName, GroupKey: minute.GroupKey, GroupName: minute.GroupName,
	}
}

func channelMonitorDailySuccessTokenOwners(ctx context.Context, rows []ChannelMonitorMinuteAPIKeyMetric) (map[int]int, error) {
	ids := make([]int, 0)
	seen := make(map[int]struct{})
	for _, row := range rows {
		if row.APIKeyId > 0 {
			if _, exists := seen[row.APIKeyId]; !exists {
				seen[row.APIKeyId] = struct{}{}
				ids = append(ids, row.APIKeyId)
			}
		}
	}
	owners := make(map[int]int, len(ids))
	if len(ids) == 0 {
		return owners, nil
	}
	var tokens []Token
	if err := DB.WithContext(ctx).Model(&Token{}).Select("id", "user_id").Where("id IN ?", ids).Find(&tokens).Error; err != nil {
		return nil, err
	}
	for _, token := range tokens {
		if token.Id > 0 && token.UserId > 0 {
			owners[token.Id] = token.UserId
		}
	}
	return owners, nil
}

func applyChannelMonitorDailySuccessDelta(tx *gorm.DB, dimension channelMonitorDailySuccessDimension, delta channelMonitorDailySuccessValues, sign int64) error {
	var row ChannelMonitorDailySuccessLedger
	query := tx.Where("day_start = ? AND channel_id = ? AND user_id = ? AND api_key_id = ? AND api_key_key = ? AND model_key = ? AND group_key = ?",
		dimension.DayStart, dimension.ChannelId, dimension.UserId, dimension.APIKeyId, dimension.APIKeyKey, dimension.ModelKey, dimension.GroupKey)
	err := query.First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if sign < 0 {
			return nil
		}
		row = ChannelMonitorDailySuccessLedger{
			DayStart: dimension.DayStart, ChannelId: dimension.ChannelId, UserId: dimension.UserId,
			UserAttribution: dimension.UserAttribution, APIKeyId: dimension.APIKeyId, APIKeyKey: dimension.APIKeyKey,
			APIKeyName: dimension.APIKeyName, ModelKey: dimension.ModelKey, ModelName: dimension.ModelName,
			GroupKey: dimension.GroupKey, GroupName: dimension.GroupName, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		}
	} else if err != nil {
		return err
	}
	values, err := row.values().add(delta, sign)
	if err != nil {
		return err
	}
	row.ActualSuccessCount, row.ActualFailureCount = values.ActualSuccessCount, values.ActualFailureCount
	row.FinalSuccessCount, row.FinalFailureCount = values.FinalSuccessCount, values.FinalFailureCount
	row.CacheHitCount, row.CacheSampleCount = values.CacheHitCount, values.CacheSampleCount
	row.CacheReadTokens, row.InputTokens, row.CacheWriteCount = values.CacheReadTokens, values.InputTokens, values.CacheWriteCount
	row.UpdatedAt = time.Now().Unix()
	if row.UserAttribution == "" {
		row.UserAttribution = dimension.UserAttribution
	}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "day_start"}, {Name: "channel_id"}, {Name: "user_id"}, {Name: "api_key_id"}, {Name: "api_key_key"}, {Name: "model_key"}, {Name: "group_key"}}, DoUpdates: clause.AssignmentColumns([]string{
		"user_attribution", "api_key_name", "model_name", "group_name", "actual_success_count", "actual_failure_count", "final_success_count", "final_failure_count", "cache_hit_count", "cache_sample_count", "cache_read_tokens", "input_tokens", "cache_write_count", "updated_at",
	})}).Create(&row).Error
}
