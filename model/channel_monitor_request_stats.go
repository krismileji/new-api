package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRequestStatsConfigConflict = errors.New("配置已被其他管理员更新，请刷新后重试")

type ChannelMonitorRequestStatsConfig struct {
	Id                        int   `json:"-" gorm:"primaryKey;autoIncrement:false"`
	Revision                  int64 `json:"revision"`
	LowInputThresholdK        int   `json:"low_input_threshold_k"`
	Enabled                   bool  `json:"enabled"`
	DisableRatePercent        int   `json:"disable_rate_percent"`
	MinimumKnownInputRequests int64 `json:"minimum_known_input_requests"`
	StartedAt                 int64 `json:"started_at"`
}

type ChannelMonitorRequestStatsDay struct {
	DayStart     int64  `gorm:"primaryKey;autoIncrement:false"`
	ConfigJSON   string `gorm:"type:text"`
	SnapshotJSON string `gorm:"type:text"`
	Status       string `gorm:"size:24;not null"`
	LastError    string `gorm:"size:255"`
	UpdatedAt    int64
}

// One committed enabled -> disabled transition is one lifetime count.
// This ledger is deliberately independent of daily statistics retention.
type ChannelMonitorRequestStatsDisable struct {
	Id                        int64  `json:"id" gorm:"primaryKey"`
	DayStart                  int64  `json:"day_start" gorm:"not null;uniqueIndex:idx_request_stats_key_day"`
	APIKeyId                  int    `json:"api_key_id" gorm:"not null;uniqueIndex:idx_request_stats_key_day;index"`
	UserId                    int    `json:"user_id" gorm:"not null;index"`
	APIKeyName                string `json:"api_key_name" gorm:"size:255"`
	Reason                    string `json:"reason" gorm:"size:32"`
	ConfigRevision            int64  `json:"config_revision"`
	ThresholdK                int    `json:"low_input_threshold_k"`
	DisableRatePercent        int    `json:"disable_rate_percent"`
	MinimumKnownInputRequests int64  `json:"minimum_known_input_requests"`
	KnownInputRequests        int64  `json:"known_input_request_count"`
	LowInputCount             int64  `json:"low_input_count"`
	CreatedAt                 int64  `json:"created_at"`
	RestoredAt                int64  `json:"restored_at"`
	RestoredBy                string `json:"restored_by" gorm:"size:16"`
	MailStatus                string `json:"mail_status" gorm:"size:24;index:idx_request_stats_mail"`
	MailAttempts              int    `json:"mail_attempts"`
	MailLastError             string `json:"mail_last_error" gorm:"size:255"`
	MailNextAt                int64  `json:"-" gorm:"index:idx_request_stats_mail"`
	MailLeaseUntil            int64  `json:"-"`
	MailLeaseOwner            string `json:"-" gorm:"size:64"`
}

// A receipt records only an address whose ownership was actually verified.
// Matching the current account address prevents a stale receipt authorizing
// mail to a subsequently edited, unverified address.
type VerifiedNotificationEmail struct {
	UserId     int    `gorm:"primaryKey;autoIncrement:false"`
	Email      string `gorm:"size:255;not null"`
	VerifiedAt int64
}

func RecordVerifiedNotificationEmail(tx *gorm.DB, userID int, email string) error {
	if userID <= 0 || NormalizeEmail(email) == "" {
		return nil
	}
	receipt := VerifiedNotificationEmail{UserId: userID, Email: NormalizeEmail(email), VerifiedAt: time.Now().Unix()}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"email", "verified_at"})}).Create(&receipt).Error
}

func GetChannelMonitorRequestStatsConfig(ctx context.Context) (ChannelMonitorRequestStatsConfig, error) {
	seed := ChannelMonitorRequestStatsConfig{Id: 1, Revision: 1, LowInputThresholdK: 10, DisableRatePercent: 80, MinimumKnownInputRequests: 100, StartedAt: time.Now().Unix()}
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
		return seed, err
	}
	var result ChannelMonitorRequestStatsConfig
	err := DB.WithContext(ctx).First(&result, 1).Error
	return result, err
}

func SaveChannelMonitorRequestStatsConfig(ctx context.Context, revision int64, values map[string]any) (ChannelMonitorRequestStatsConfig, error) {
	values["revision"] = revision + 1
	result := DB.WithContext(ctx).Model(&ChannelMonitorRequestStatsConfig{}).Where("id = ? AND revision = ?", 1, revision).Updates(values)
	if result.Error != nil {
		return ChannelMonitorRequestStatsConfig{}, result.Error
	}
	if result.RowsAffected != 1 {
		return ChannelMonitorRequestStatsConfig{}, ErrRequestStatsConfigConflict
	}
	return GetChannelMonitorRequestStatsConfig(ctx)
}

// Freeze policy once per decision day, even when projection readiness requires
// retries. The scheduler lease and key/day uniqueness are separate safeguards.
func FreezeRequestStatsDay(ctx context.Context, day int64, config ChannelMonitorRequestStatsConfig) (ChannelMonitorRequestStatsDay, ChannelMonitorRequestStatsConfig, error) {
	payload, err := common.Marshal(config)
	if err != nil {
		return ChannelMonitorRequestStatsDay{}, config, err
	}
	row := ChannelMonitorRequestStatsDay{DayStart: day, ConfigJSON: string(payload), Status: "pending", UpdatedAt: time.Now().Unix()}
	if !config.Enabled || config.StartedAt > day {
		row.Status = "skipped"
	}
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return row, config, err
	}
	err = DB.WithContext(ctx).First(&row, "day_start = ?", day).Error
	if err == nil {
		err = common.UnmarshalJsonStr(row.ConfigJSON, &config)
	}
	return row, config, err
}

func DisableRequestStatsToken(ctx context.Context, record ChannelMonitorRequestStatsDisable) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	changed := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The first statement writes: SQLite cannot upgrade a read snapshot
		// under concurrent restoration; MySQL/PostgreSQL also acquire a lock.
		if err := tx.Model(&Token{}).Where("id = ? AND user_id = ?", record.APIKeyId, record.UserId).
			UpdateColumn("status", gorm.Expr("status")).Error; err != nil {
			return err
		}
		var token Token
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", record.APIKeyId, record.UserId).First(&token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if token.Status != common.TokenStatusEnabled || token.ExpiredTime != -1 && token.ExpiredTime <= time.Now().Unix() || !token.UnlimitedQuota && token.RemainQuota <= 0 {
			return nil
		}
		var protected int64
		if err := tx.Model(&TokenAutoDisableRecord{}).Where("token_id = ? AND released_at = ?", token.Id, 0).Count(&protected).Error; err != nil {
			return err
		}
		if protected > 0 {
			return nil
		}
		var existing int64
		if err := tx.Model(&ChannelMonitorRequestStatsDisable{}).Where("day_start = ? AND api_key_id = ?", record.DayStart, token.Id).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		// Fail closed if the shared authentication cache cannot be invalidated.
		if err := invalidateTokenCacheForMutation(token.Key, ctx); err != nil {
			return err
		}
		record.APIKeyName = token.Name
		record.Reason = "low_input_rate"
		record.CreatedAt = time.Now().Unix()
		record.MailStatus = "pending"
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&token).Update("status", common.TokenStatusDisabled).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed && err == nil, err
}

type RequestStatsTokenMutation struct {
	StatusOnly    bool
	RestoreSource string
}

// Serialize ordinary key edits with automatic disable. A metadata edit must
// never restore a stale enabled status. Status changes only update status.
func UpdateTokenWithRequestStats(token *Token, intent RequestStatsTokenMutation) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Token{}).Where("id = ? AND user_id = ?", token.Id, token.UserId).UpdateColumn("status", gorm.Expr("status")).Error; err != nil {
			return err
		}
		var current Token
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", token.Id, token.UserId).First(&current).Error; err != nil {
			return err
		}
		columns := []string{"name", "expired_time", "remain_quota", "unlimited_quota", "model_limits_enabled", "model_limits", "allow_ips", "group", "cross_group_retry", "auto_groups"}
		if intent.StatusOnly {
			columns = []string{"status"}
			if token.Status != common.TokenStatusEnabled && token.Status != common.TokenStatusDisabled {
				return errors.New("API Key 状态无效")
			}
			if token.Status == common.TokenStatusEnabled {
				if current.ExpiredTime != -1 && current.ExpiredTime <= time.Now().Unix() || !current.UnlimitedQuota && current.RemainQuota <= 0 {
					return errors.New("API Key 已过期或额度不足，无法启用")
				}
				var protected int64
				if err := tx.Model(&TokenAutoDisableRecord{}).Where("token_id = ? AND released_at = ?", token.Id, 0).Count(&protected).Error; err != nil {
					return err
				}
				if protected > 0 {
					return errors.New("此 API Key 已被自动禁用，仅管理员可以解除")
				}
				if current.Status != common.TokenStatusEnabled {
					source := intent.RestoreSource
					if source != "admin" {
						source = "user"
					}
					if err := tx.Model(&ChannelMonitorRequestStatsDisable{}).Where("api_key_id = ? AND restored_at = ?", token.Id, 0).
						Updates(map[string]any{"restored_at": time.Now().Unix(), "restored_by": source}).Error; err != nil {
						return err
					}
				}
			}
		} else {
			token.Status = current.Status
		}
		if err := invalidateTokenCacheForMutation(current.Key, ctx); err != nil {
			return fmt.Errorf("刷新 API Key 状态缓存失败: %w", err)
		}
		return tx.Model(token).Select(columns).Updates(token).Error
	})
}

func ListRequestStatsDisableHistory(ctx context.Context, userID, keyID, offset, limit int) ([]ChannelMonitorRequestStatsDisable, int64, error) {
	query := DB.WithContext(ctx).Model(&ChannelMonitorRequestStatsDisable{})
	if userID >= 0 {
		query = query.Where("user_id = ?", userID)
	}
	if keyID >= 0 {
		query = query.Where("api_key_id = ?", keyID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := []ChannelMonitorRequestStatsDisable{}
	err := query.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}
